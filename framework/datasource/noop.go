package datasource

import "context"

// noopDataSource 占位实现，用于单测或未实现类型的占位。
type noopDataSource struct {
	id   string
	typ  string
}

func (n *noopDataSource) ID() string { return n.id }
func (n *noopDataSource) Type() string {
	if n.typ != "" {
		return n.typ
	}
	return TypeNoop
}
func (n *noopDataSource) Ping(ctx context.Context) error { return nil }
func (n *noopDataSource) Close() error                   { return nil }

// RegisterNoop 在 r 上注册 "noop" 类型，便于单测或占位。
func RegisterNoop(r *Registry) {
	r.RegisterType("noop", func(cfg Config) (DataSource, error) {
		typ := cfg.Type
		if typ == "" {
			typ = TypeNoop
		}
		return &noopDataSource{id: cfg.ID, typ: typ}, nil
	})
}
