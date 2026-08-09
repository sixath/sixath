package chat

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sixath/framework/model"
)

type stubClassifyModel struct {
	text  string
	err   error
	delay time.Duration
}

func (s stubClassifyModel) Generate(ctx context.Context, prompt string, opts ...model.Option) (*model.Generation, error) {
	if s.delay > 0 {
		select {
		case <-time.After(s.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if s.err != nil {
		return nil, s.err
	}
	return &model.Generation{Text: s.text}, nil
}

func (s stubClassifyModel) Chat(ctx context.Context, messages []model.Message, opts ...model.Option) (*model.Generation, error) {
	return s.Generate(ctx, "")
}

func (s stubClassifyModel) Embed(ctx context.Context, texts []string, opts ...model.Option) ([]model.Embedding, error) {
	return nil, nil
}

func TestParseClassifierJSON(t *testing.T) {
	fams, conf, err := parseClassifierJSON(`{"families":["mcp:gitlab","rca"],"confidence":"high"}`)
	if err != nil || conf != "high" || len(fams) != 2 {
		t.Fatalf("%v %v %v", fams, conf, err)
	}
}

func TestModelFamilyClassifier_High(t *testing.T) {
	c := ModelFamilyClassifier{Model: stubClassifyModel{text: `{"families":["mcp:gitlab"],"confidence":"high"}`}, Timeout: time.Second}
	sel, conf, err := c.Classify(context.Background(), "gitlab projects", []string{FamilyCore, "mcp:gitlab", FamilyRCA}, nil)
	if err != nil || conf != "high" || len(sel) != 1 || sel[0] != "mcp:gitlab" {
		t.Fatalf("%v %s %v", sel, conf, err)
	}
}

func TestModelFamilyClassifier_BadJSON(t *testing.T) {
	c := ModelFamilyClassifier{Model: stubClassifyModel{text: "not-json"}, Timeout: time.Second}
	_, _, err := c.Classify(context.Background(), "x", []string{FamilyCore}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestModelFamilyClassifier_Timeout(t *testing.T) {
	c := ModelFamilyClassifier{Model: stubClassifyModel{delay: 200 * time.Millisecond}, Timeout: 20 * time.Millisecond}
	_, _, err := c.Classify(context.Background(), "x", []string{FamilyCore}, nil)
	if err == nil {
		t.Fatal("expected timeout")
	}
}

func TestIntentResolver_ClassifierErrorFailNarrow(t *testing.T) {
	r := IntentResolver{Classifier: ModelFamilyClassifier{Model: stubClassifyModel{err: errors.New("boom")}, Timeout: time.Second}}
	res := r.Resolve(context.Background(), IntentResolveInput{
		UserText:      "你好",
		BoundFamilies: []string{FamilyCore, FamilyRCA},
	})
	if res.Source != "fail_narrow" || FamilyActive(familySet(res.ActiveFamilies), FamilyRCA) {
		t.Fatalf("%+v", res)
	}
}
