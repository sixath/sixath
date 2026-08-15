package biz

import (
	"strings"

	kratosErrors "github.com/go-kratos/kratos/v2/errors"
	"google.golang.org/protobuf/types/known/structpb"
)

var (
	ErrRCAESLogMutualExclusive = kratosErrors.BadRequest(
		"RCA_ES_LOG_CONFIG",
		"endpoint and datasource_id are mutually exclusive; keep only one",
	)
	ErrRCAESLogMissingConn = kratosErrors.BadRequest(
		"RCA_ES_LOG_CONFIG",
		"require endpoint or datasource_id",
	)
)

// ValidateRCAESLogConfig enforces save-time mutual exclusion for es_log_query.
func ValidateRCAESLogConfig(endpoint, datasourceID string) error {
	ep := strings.TrimSpace(endpoint)
	ds := strings.TrimSpace(datasourceID)
	if ep != "" && ds != "" {
		return ErrRCAESLogMutualExclusive
	}
	if ep == "" && ds == "" {
		return ErrRCAESLogMissingConn
	}
	return nil
}

func validateRCAESLogConfigIfNeeded(tt ToolType, config *structpb.Struct) error {
	if tt != ToolTypeRCA {
		return nil
	}
	fp, ep, ds := rcaESLogFieldsFromConfig(config)
	if fp != "es_log_query" {
		return nil
	}
	return ValidateRCAESLogConfig(ep, ds)
}

func rcaESLogFieldsFromConfig(config *structpb.Struct) (funcPath, endpoint, dsID string) {
	if config == nil {
		return "", "", ""
	}
	m := config.AsMap()
	rca, _ := m["rca"].(map[string]any)
	if rca == nil {
		return "", "", ""
	}
	fp, _ := rca["func_path"].(string)
	ep, _ := rca["endpoint"].(string)
	ds, _ := rca["datasource_id"].(string)
	return fp, ep, ds
}
