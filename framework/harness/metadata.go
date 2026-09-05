package harness

// Well-known metadata keys（与 map 扩展字段同步；优先使用 Request/Response 上的 typed 字段）。
const (
	MetaAgentName    = "agent_name"
	MetaUserID       = "user_id"
	MetaModelName    = "model"
	MetaSystem       = "system_prompt"
	MetaTemperature  = "temperature"
	MetaTokenInput   = "token_input"
	MetaTokenOutput  = "token_output"
	MetaSessionID    = "session_id"
	MetaDatasourceID = "datasource_id"
)
