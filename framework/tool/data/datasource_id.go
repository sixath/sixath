package tooldata

import (
	"github.com/sixath/framework/datasource"
)

// ResolveDatasourceID 从工具参数解析 datasource_id，并与配置的默认 ID 合并。
//
// 模型常把字面量 "default" 当作「使用默认数据源」，但 Portal/配置里实际 id 多为 "main" 等。
// 若请求 id 在注册表中不存在，且为 "default" 且 cfgDefault 非空且能在注册表解析，则回退为 cfgDefault。
// reg 为 nil 时：若 id 为 "default" 且 cfgDefault 非空且与 "default" 不同，则直接使用 cfgDefault（无法校验注册表）。
func ResolveDatasourceID(params map[string]any, cfgDefault string, reg *datasource.Registry) string {
	id := cfgDefault
	if p := params["datasource_id"]; p != nil {
		if s, ok := p.(string); ok && s != "" {
			id = s
		}
	}
	if id == "" {
		return ""
	}
	if reg != nil {
		if _, err := reg.Get(id); err == nil {
			return id
		}
		if id == "default" && cfgDefault != "" {
			if _, err2 := reg.Get(cfgDefault); err2 == nil {
				return cfgDefault
			}
		}
		return id
	}
	if id == "default" && cfgDefault != "" && cfgDefault != "default" {
		return cfgDefault
	}
	return id
}
