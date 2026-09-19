package biz

import (
	"context"
	"strings"

	kratosErrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/sixath/framework/netx"
	"google.golang.org/protobuf/types/known/structpb"
)

var (
	ErrToolEgressInvalidMode = kratosErrors.BadRequest("TOOL_EGRESS_CONFIG", "invalid egress_mode")
	ErrToolEgressProxyID     = kratosErrors.BadRequest("TOOL_EGRESS_CONFIG", "egress_mode=proxy requires proxy_id")
	ErrToolEgressHiveMongo   = kratosErrors.BadRequest("TOOL_EGRESS_CONFIG", "hive cannot use egress_mode=proxy")
	ErrToolEgressMySQLHTTP   = kratosErrors.BadRequest("TOOL_EGRESS_CONFIG", "mysql/mongodb cannot use an http proxy")
)

func requireProxyUse(ctx context.Context, caller, proxyID string, proxies ProxyRepo, resources ResourceRepo, access *AccessChecker) error {
	id := strings.TrimSpace(proxyID)
	if id == "" {
		return nil
	}
	if proxies == nil {
		return ErrProxyNotFound
	}
	if _, err := proxies.GetByID(ctx, id); err != nil {
		return ErrProxyNotFound
	}
	if resources == nil || access == nil {
		return ErrProxyNotFound
	}
	resource, err := resources.GetByPayload(ctx, ResourceTypeProxy, id)
	if err != nil {
		return ErrProxyNotFound
	}
	canView, err := access.Can(ctx, caller, resource.ID, PermView, "")
	if err != nil {
		return err
	}
	if !canView {
		return ErrProxyNotFound
	}
	canUse, err := access.Can(ctx, caller, resource.ID, PermUse, "")
	if err != nil {
		return err
	}
	if !canUse {
		return ErrForbiddenPerm
	}
	return nil
}

// ValidateToolEgress checks tool-level egress_mode/proxy_id at save time.
// MySQL/Mongo inherit + Agent HTTP is rejected later in BuildRegistry (no Agent context here).
func ValidateToolEgress(ctx context.Context, caller string, config *structpb.Struct, proxies ProxyRepo, resources ResourceRepo, access *AccessChecker) error {
	mode, proxyID := toolEgressFields(config)
	normalized := netx.NormalizeMode(mode)
	switch normalized {
	case netx.ModeInherit, netx.ModeOff:
		return nil
	case netx.ModeProxy:
	default:
		return ErrToolEgressInvalidMode
	}
	if strings.TrimSpace(proxyID) == "" {
		return ErrToolEgressProxyID
	}
	if err := requireProxyUse(ctx, caller, proxyID, proxies, resources, access); err != nil {
		return err
	}
	kind := toolDatasourceKind(config)
	switch kind {
	case "hive":
		return ErrToolEgressHiveMongo
	case "mysql", "mongodb":
		if proxies == nil {
			return ErrProxyNotFound
		}
		meta, err := proxies.GetByID(ctx, strings.TrimSpace(proxyID))
		if err != nil {
			return ErrProxyNotFound
		}
		if strings.EqualFold(strings.TrimSpace(meta.Type), netx.TypeHTTP) {
			return ErrToolEgressMySQLHTTP
		}
	}
	return nil
}

func toolEgressFields(config *structpb.Struct) (mode, proxyID string) {
	if config == nil {
		return "", ""
	}
	m := config.AsMap()
	mode, _ = m["egress_mode"].(string)
	proxyID, _ = m["proxy_id"].(string)
	return mode, proxyID
}

func toolDatasourceKind(config *structpb.Struct) string {
	if config == nil {
		return ""
	}
	m := config.AsMap()
	ds, _ := m["datasource"].(map[string]any)
	if ds == nil {
		return ""
	}
	typ, _ := ds["type"].(string)
	typ = strings.ToLower(strings.TrimSpace(typ))
	switch typ {
	case "mysql", "hive":
		return typ
	case "mongo", "mongodb":
		return "mongodb"
	case "":
	default:
		return typ
	}
	dsn, _ := ds["dsn"].(string)
	dsn = strings.ToLower(strings.TrimSpace(dsn))
	switch {
	case strings.HasPrefix(dsn, "mysql:"):
		return "mysql"
	case strings.HasPrefix(dsn, "mongodb:"):
		return "mongodb"
	case strings.HasPrefix(dsn, "hive:"):
		return "hive"
	default:
		return ""
	}
}
