package datasource

// 数据源类型常量，与 Config.Type / DataSource.Type() 一致。
const (
	TypeMySQL         = "mysql"
	TypeElasticsearch = "elasticsearch"
	TypeMongoDB       = "mongodb"
	TypeHive          = "hive"
	TypeNoop          = "noop"
)
