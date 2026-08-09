package executor

import (
	"context"

	"github.com/sixath/framework/datasource"
)

// ExecutorRegistry 按 DataSource.Type() 路由到具体 Executor。
type ExecutorRegistry struct {
	d *datasource.TypedDispatcher[Executor]
}

// NewExecutorRegistry 创建执行器注册表。
func NewExecutorRegistry(dsReg *datasource.Registry) *ExecutorRegistry {
	return &ExecutorRegistry{d: datasource.NewTypedDispatcher[Executor](dsReg)}
}

// Register 为数据源类型注册 Executor（如 datasource.TypeMySQL）。
func (r *ExecutorRegistry) Register(typ string, exec Executor) {
	r.d.Register(typ, exec)
}

// Execute 在指定数据源上执行 dsl。
func (r *ExecutorRegistry) Execute(ctx context.Context, datasourceID string, dsl string, opts ExecuteOptions) (*Result, error) {
	exec, err := r.d.For(datasourceID)
	if err != nil {
		if err == datasource.ErrUnsupportedType {
			return nil, ErrUnsupportedDataSource
		}
		return nil, err
	}
	return exec.Execute(ctx, datasourceID, dsl, opts)
}

// ReaderRegistry 按类型路由 Reader。
type ReaderRegistry struct {
	d *datasource.TypedDispatcher[Reader]
}

func NewReaderRegistry(dsReg *datasource.Registry) *ReaderRegistry {
	return &ReaderRegistry{d: datasource.NewTypedDispatcher[Reader](dsReg)}
}

func (r *ReaderRegistry) Register(typ string, reader Reader) { r.d.Register(typ, reader) }

// MultiReader 实现 Reader，按 datasource ID 分发。
type MultiReader struct {
	reg *ReaderRegistry
}

func NewMultiReader(reg *ReaderRegistry) *MultiReader {
	return &MultiReader{reg: reg}
}

func (m *MultiReader) Query(ctx context.Context, datasourceID string, dsl string, opts QueryOptions) (*QueryResult, error) {
	rd, err := m.reg.d.For(datasourceID)
	if err != nil {
		if err == datasource.ErrUnsupportedType {
			return nil, ErrUnsupportedDataSource
		}
		return nil, err
	}
	return rd.Query(ctx, datasourceID, dsl, opts)
}

// WriterRegistry 按类型路由 Writer。
type WriterRegistry struct {
	d *datasource.TypedDispatcher[Writer]
}

func NewWriterRegistry(dsReg *datasource.Registry) *WriterRegistry {
	return &WriterRegistry{d: datasource.NewTypedDispatcher[Writer](dsReg)}
}

func (r *WriterRegistry) Register(typ string, writer Writer) { r.d.Register(typ, writer) }

// MultiWriter 实现 Writer，按 datasource ID 分发。
type MultiWriter struct {
	reg *WriterRegistry
}

func NewMultiWriter(reg *WriterRegistry) *MultiWriter {
	return &MultiWriter{reg: reg}
}

func (m *MultiWriter) Exec(ctx context.Context, datasourceID string, dsl string, opts ExecOptions) (*ExecResult, error) {
	wr, err := m.reg.d.For(datasourceID)
	if err != nil {
		if err == datasource.ErrUnsupportedType {
			return nil, ErrUnsupportedDataSource
		}
		return nil, err
	}
	return wr.Exec(ctx, datasourceID, dsl, opts)
}

// Bundle 聚合默认 Executor / Reader / Writer 注册，便于应用启动时一次配置。
type Bundle struct {
	ExecutorRegistry *ExecutorRegistry
	Reader           Reader
	Writer           Writer
	Executor         Executor // 与 ExecutorRegistry 等价门面
	stopPoolSampler  context.CancelFunc
}

// NewBundle 注册 MySQL / Elasticsearch / MongoDB 的默认执行器并返回 Multi 门面。
func NewBundle(dsReg *datasource.Registry) *Bundle {
	ctx, stopPool := context.WithCancel(context.Background())
	datasource.StartPoolSampler(ctx, dsReg, 0)

	execReg := NewExecutorRegistry(dsReg)
	readReg := NewReaderRegistry(dsReg)
	writeReg := NewWriterRegistry(dsReg)

	mysql := NewMySQLExecutor(dsReg)
	es := NewESExecutor(dsReg)
	mongo := NewMongoExecutor(dsReg)

	execReg.Register(datasource.TypeMySQL, mysql)
	execReg.Register(datasource.TypeElasticsearch, es)
	execReg.Register(datasource.TypeMongoDB, mongo)

	readReg.Register(datasource.TypeMySQL, mysql)
	readReg.Register(datasource.TypeElasticsearch, es)
	readReg.Register(datasource.TypeMongoDB, mongo)

	writeReg.Register(datasource.TypeMySQL, mysql)

	return &Bundle{
		ExecutorRegistry: execReg,
		Reader:           NewMultiReader(readReg),
		Writer:           NewMultiWriter(writeReg),
		Executor:         execReg,
		stopPoolSampler:  stopPool,
	}
}

// Stop 停止 Bundle 启动的后台任务（如连接池采样）。
func (b *Bundle) Stop() {
	if b != nil && b.stopPoolSampler != nil {
		b.stopPoolSampler()
	}
}
