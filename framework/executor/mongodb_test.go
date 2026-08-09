package executor

import (
	"context"
	"errors"
	"testing"

	"github.com/sixath/framework/datasource"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/integration/mtest"
)

type mongoStubDS struct {
	id string
	db *mongo.Database
}

func (m *mongoStubDS) ID() string                     { return m.id }
func (m *mongoStubDS) Type() string                   { return datasource.TypeMongoDB }
func (m *mongoStubDS) Ping(ctx context.Context) error { return nil }
func (m *mongoStubDS) Close() error                   { return nil }
func (m *mongoStubDS) MongoDatabase() *mongo.Database { return m.db }

func registerMongoExecutor(t *testing.T, mt *mtest.T) *MongoExecutor {
	t.Helper()
	reg := datasource.NewRegistry()
	reg.RegisterType(datasource.TypeMongoDB, func(cfg datasource.Config) (datasource.DataSource, error) {
		return &mongoStubDS{id: cfg.ID, db: mt.DB}, nil
	})
	if _, err := reg.Register(datasource.Config{ID: "ds1", Type: datasource.TypeMongoDB}); err != nil {
		mt.Fatalf("Register: %v", err)
	}
	return NewMongoExecutor(reg)
}

func TestMongoExecutor_BasicFind(t *testing.T) {
	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))
	mt.Run("basic", func(mt *mtest.T) {
		mt.AddMockResponses(
			mtest.CreateCursorResponse(1, "test.users", mtest.FirstBatch, bson.D{{Key: "name", Value: "alice"}}),
			mtest.CreateCursorResponse(0, "test.users", mtest.NextBatch),
		)
		ex := registerMongoExecutor(t, mt)
		res, err := ex.Execute(context.Background(), "ds1", `{"collection":"users"}`, ExecuteOptions{})
		if err != nil {
			mt.Fatalf("Execute: %v", err)
		}
		if len(res.Columns) != 1 || res.Columns[0] != "name" {
			mt.Fatalf("columns = %v", res.Columns)
		}
		if len(res.Rows) != 1 || res.Rows[0][0] != "alice" {
			mt.Fatalf("rows = %v", res.Rows)
		}
	})
}

func TestMongoExecutor_MissingCollection(t *testing.T) {
	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))
	mt.Run("missing", func(mt *mtest.T) {
		ex := registerMongoExecutor(t, mt)
		_, err := ex.Execute(context.Background(), "ds1", `{"filter":{}}`, ExecuteOptions{})
		if err == nil {
			mt.Fatal("expected error")
		}
	})
}

func TestMongoExecutor_InvalidJSON(t *testing.T) {
	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))
	mt.Run("bad json", func(mt *mtest.T) {
		ex := registerMongoExecutor(t, mt)
		_, err := ex.Execute(context.Background(), "ds1", `{not json`, ExecuteOptions{})
		if err == nil {
			mt.Fatal("expected error")
		}
	})
}

func TestMongoExecutor_LimitPushdown(t *testing.T) {
	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))
	mt.Run("limit", func(mt *mtest.T) {
		mt.AddMockResponses(
			mtest.CreateCursorResponse(0, "test.users", mtest.FirstBatch),
		)
		ex := registerMongoExecutor(t, mt)
		_, err := ex.Execute(context.Background(), "ds1", `{"collection":"users"}`, ExecuteOptions{MaxRows: 5})
		if err != nil {
			mt.Fatalf("Execute: %v", err)
		}
		events := mt.GetAllStartedEvents()
		var found bool
		for _, e := range events {
			if e.CommandName != "find" {
				continue
			}
			limit, ok := e.Command.Lookup("limit").Int64OK()
			if ok && limit == 5 {
				found = true
			}
		}
		if !found {
			mt.Fatalf("find command limit=5 not seen in %+v", events)
		}
	})
}

func TestMongoExecutor_SortPushdown(t *testing.T) {
	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))
	mt.Run("sort", func(mt *mtest.T) {
		mt.AddMockResponses(
			mtest.CreateCursorResponse(0, "test.users", mtest.FirstBatch),
		)
		ex := registerMongoExecutor(t, mt)
		_, err := ex.Execute(context.Background(), "ds1", `{"collection":"users","sort":{"age":-1}}`, ExecuteOptions{})
		if err != nil {
			mt.Fatalf("Execute: %v", err)
		}
		events := mt.GetAllStartedEvents()
		var found bool
		for _, e := range events {
			if e.CommandName != "find" {
				continue
			}
			sortDoc := e.Command.Lookup("sort").Document()
			if sortDoc != nil {
				if v, ok := sortDoc.Lookup("age").Int32OK(); ok && v == -1 {
					found = true
				}
			}
		}
		if !found {
			mt.Fatalf("find sort not seen in %+v", events)
		}
	})
}

func TestMongoExecutor_HeterogeneousColumns(t *testing.T) {
	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))
	mt.Run("hetero", func(mt *mtest.T) {
		mt.AddMockResponses(
			mtest.CreateCursorResponse(1, "test.items", mtest.FirstBatch,
				bson.D{{Key: "name", Value: "a"}},
				bson.D{{Key: "name", Value: "b"}, {Key: "extra", Value: 1}},
			),
			mtest.CreateCursorResponse(0, "test.items", mtest.NextBatch),
		)
		ex := registerMongoExecutor(t, mt)
		res, err := ex.Execute(context.Background(), "ds1", `{"collection":"items"}`, ExecuteOptions{})
		if err != nil {
			mt.Fatalf("Execute: %v", err)
		}
		want := []string{"extra", "name"}
		if len(res.Columns) != len(want) {
			mt.Fatalf("columns = %v, want %v", res.Columns, want)
		}
		for i := range want {
			if res.Columns[i] != want[i] {
				mt.Fatalf("columns = %v, want %v", res.Columns, want)
			}
		}
	})
}

func TestMongoExecutor_UnsupportedDataSource(t *testing.T) {
	reg := datasource.NewRegistry()
	datasource.RegisterNoop(reg)
	if _, err := reg.Register(datasource.Config{ID: "noop", Type: "noop"}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	ex := NewMongoExecutor(reg)
	_, err := ex.Execute(context.Background(), "noop", `{"collection":"x"}`, ExecuteOptions{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrUnsupportedDataSource) {
		t.Fatalf("expected ErrUnsupportedDataSource, got %v", err)
	}
}

func TestMongoExecutor_Truncated(t *testing.T) {
	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))
	mt.Run("truncated", func(mt *mtest.T) {
		docs := make([]bson.D, 5)
		for i := range docs {
			docs[i] = bson.D{{Key: "n", Value: i}}
		}
		mt.AddMockResponses(
			mtest.CreateCursorResponse(1, "test.t", mtest.FirstBatch, docs...),
			mtest.CreateCursorResponse(0, "test.t", mtest.NextBatch),
		)
		ex := registerMongoExecutor(t, mt)
		res, err := ex.Execute(context.Background(), "ds1", `{"collection":"t"}`, ExecuteOptions{MaxRows: 5})
		if err != nil {
			mt.Fatalf("Execute: %v", err)
		}
		if len(res.Rows) != 5 {
			mt.Fatalf("rows = %d, want 5", len(res.Rows))
		}
		if !res.Truncated {
			mt.Fatal("expected Truncated=true when rows == MaxRows")
		}
	})
}
