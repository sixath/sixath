package datasource

import "sync"

// TypedDispatcher 按 DataSource.Type() 将 datasource ID 路由到对应实现。
type TypedDispatcher[T any] struct {
	mu     sync.RWMutex
	reg    *Registry
	byType map[string]T
}

// NewTypedDispatcher 创建与 reg 绑定的按类型路由器。
func NewTypedDispatcher[T any](reg *Registry) *TypedDispatcher[T] {
	return &TypedDispatcher[T]{
		reg:    reg,
		byType: make(map[string]T),
	}
}

// Register 为 typ 注册实现（如 TypeMySQL）。
func (d *TypedDispatcher[T]) Register(typ string, impl T) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.byType[typ] = impl
}

// For 根据 datasourceID 查找已注册数据源并返回对应类型的实现。
func (d *TypedDispatcher[T]) For(datasourceID string) (T, error) {
	var zero T
	ds, err := d.reg.Get(datasourceID)
	if err != nil {
		return zero, err
	}
	d.mu.RLock()
	impl, ok := d.byType[ds.Type()]
	d.mu.RUnlock()
	if !ok {
		return zero, ErrUnsupportedType
	}
	return impl, nil
}

// RegistryRef 返回绑定的数据源 Registry（供需要直接 Get 的调用方使用）。
func (d *TypedDispatcher[T]) RegistryRef() *Registry {
	return d.reg
}

// Lookup 按类型字符串查找已注册实现（不校验 datasource ID）。
func (d *TypedDispatcher[T]) Lookup(typ string) (T, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	v, ok := d.byType[typ]
	return v, ok
}
