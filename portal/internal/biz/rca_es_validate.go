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
	ErrRCAESLogUseDatasource = kratosErrors.BadRequest(
		"RCA_ES_LOG_USE_DATASOURCE",
		"create an elasticsearch datasource instead",
	)
	ErrESDatasourceMissingMeta = kratosErrors.BadRequest(
		"ES_DATASOURCE_CONFIG",
		"elasticsearch datasource requires default_index and purpose",
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

// ValidateCreateRCAESLog rejects creating a new RCA es_log_query tool.
// Existing tools may still be updated via ValidateUpdateRCAESLog.
func ValidateCreateRCAESLog(tt ToolType, config *structpb.Struct) error {
	if tt != ToolTypeRCA {
		return nil
	}
	fp, _, _ := rcaESLogFieldsFromConfig(config)
	if isRCAESLogQuery(fp) {
		return ErrRCAESLogUseDatasource
	}
	return nil
}

// ValidateUpdateRCAESLog allows connection edits on an existing RCA es_log_query
// tool and rejects minting a new one by changing func_path.
func ValidateUpdateRCAESLog(existingConfig *structpb.Struct, newType ToolType, newConfig *structpb.Struct) error {
	if newType != ToolTypeRCA {
		return nil
	}
	newFP, ep, ds := rcaESLogFieldsFromConfig(newConfig)
	if !isRCAESLogQuery(newFP) {
		return nil
	}
	oldFP, _, _ := rcaESLogFieldsFromConfig(existingConfig)
	if !isRCAESLogQuery(oldFP) {
		return ErrRCAESLogUseDatasource
	}
	return ValidateRCAESLogConfig(ep, ds)
}

// ValidateElasticsearchDatasource requires default_index and purpose for ES datasources.
// MySQL and other datasource types are unchanged.
func ValidateElasticsearchDatasource(tt ToolType, config *structpb.Struct) error {
	if tt != ToolTypeDatasource {
		return nil
	}
	typ, index, purpose := datasourceESFieldsFromConfig(config)
	if !isElasticsearchDatasourceType(typ) {
		return nil
	}
	if strings.TrimSpace(index) == "" || strings.TrimSpace(purpose) == "" {
		return ErrESDatasourceMissingMeta
	}
	return nil
}

func isRCAESLogQuery(funcPath string) bool {
	return strings.TrimSpace(funcPath) == "es_log_query"
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

func datasourceESFieldsFromConfig(config *structpb.Struct) (typ, defaultIndex, purpose string) {
	if config == nil {
		return "", "", ""
	}
	m := config.AsMap()
	ds, _ := m["datasource"].(map[string]any)
	if ds == nil {
		return "", "", ""
	}
	typ, _ = ds["type"].(string)
	defaultIndex, _ = ds["default_index"].(string)
	if defaultIndex == "" {
		defaultIndex, _ = ds["defaultIndex"].(string)
	}
	purpose, _ = ds["purpose"].(string)
	return typ, defaultIndex, purpose
}

func isElasticsearchDatasourceType(typ string) bool {
	switch strings.ToLower(strings.TrimSpace(typ)) {
	case "elasticsearch", "es":
		return true
	default:
		return false
	}
}
