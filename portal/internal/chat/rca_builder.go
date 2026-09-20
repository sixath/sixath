package chat

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sixath/framework/executor"
	"github.com/sixath/framework/tool"
)

var vmRunCmdPending = tool.NewInMemoryVMRunCmdPendingStore()

// registerRCATool 按 cfg["func_path"] 构造并注册 RCA 工具。
// es_log_query 由 registerESLogFromAgentTools 一次性注册，此处不再处理。
// 缺配置/依赖缺失时跳过并记 warn,绝不 panic 或阻断整体构建。
func registerRCATool(reg *tool.Registry, cfg map[string]interface{}, workspace string, opts ...RegistryBuildOptions) {
	if reg == nil {
		return
	}
	var o RegistryBuildOptions
	if len(opts) > 0 {
		o = opts[0]
	}
	rcaMap, _ := cfg["rca"].(map[string]interface{})
	if rcaMap == nil {
		slog.Warn("rca: tool config missing 'rca' section, skip")
		return
	}
	funcPath, _ := rcaMap["func_path"].(string)
	switch funcPath {
	case "rca_code":
		roots := MergeRCARoots(workspace, stringSliceFromAny(rcaMap["roots"]))
		if len(roots) == 0 {
			slog.Warn("rca: rca_code has no roots, skip")
			return
		}
		_ = tool.RegisterRCACodeTools(reg, roots)
	case "rca_symbol":
		roots := MergeRCARoots(workspace, stringSliceFromAny(rcaMap["roots"]))
		if len(roots) == 0 {
			slog.Warn("rca: rca_symbol has no roots, skip")
			return
		}
		goplsPath, _ := rcaMap["gopls_path"].(string)
		opts := tool.RCASymbolOpts{GoplsPath: goplsPath}
		if readyTimeout, ok := rcaTimeoutSeconds(rcaMap["ready_timeout_sec"]); ok {
			opts.ReadyTimeout = readyTimeout
		}
		if requestTimeout, ok := rcaTimeoutSeconds(rcaMap["request_timeout_sec"]); ok {
			opts.RequestTimeout = requestTimeout
		}
		_ = tool.RegisterRCASymbolTool(reg, roots, opts)
	case "jaeger_trace":
		queryURL, _ := rcaMap["query_url"].(string)
		if queryURL == "" {
			slog.Warn("rca: jaeger_trace has no query_url, skip")
			return
		}
		client, err := resolveJaegerClient(queryURL, toolEgressBinding(cfg), o)
		if err != nil {
			slog.Error("rca: skip jaeger_trace, proxy not in catalog", "err", err)
			return
		}
		_ = tool.RegisterJaegerTool(reg, queryURL, client)
	case "es_log_query":
		return // registered once via registerESLogFromAgentTools
	case "vm_run_cmd":
		return // deferred to BuildRegistry after MySQL executor is ready
	default:
		slog.Warn("rca: unknown func_path, skip", "func_path", funcPath)
	}
}

func registerVMRunCmdFromPortal(reg *tool.Registry, cfg map[string]interface{}, o RegistryBuildOptions, mysqlExec executor.Executor, mysqlIDs []string) {
	if reg == nil {
		return
	}
	rcaMap, _ := cfg["rca"].(map[string]interface{})
	preferred, _ := rcaMap["datasource_id"].(string)

	var lookup tool.VMIPLookup
	if mysqlExec != nil {
		lookup = vmIPLookup(mysqlExec)
	}

	_ = tool.RegisterVMRunCmd(reg, tool.VMRunCmdConfig{
		PreferredDatasourceID: preferred,
		MySQLIDs:              mysqlIDs,
		Lookup:                lookup,
		PendingStore:          vmRunCmdPending,
		TokenGen:              tool.RandomTokenGenerator{},
		ClientForHost: func(host string, port int) (*http.Client, error) {
			dest := "http://" + host + ":" + strconv.Itoa(port)
			c, err := resolveJaegerClient(dest, toolEgressBinding(cfg), o)
			if err != nil {
				return nil, err
			}
			if c == nil {
				return &http.Client{Timeout: outboundClientTimeout}, nil
			}
			return c, nil
		},
	})
}

func vmIPLookup(exec executor.Executor) tool.VMIPLookup {
	return func(ctx context.Context, dsID string, vmid int64) (string, bool, error) {
		if exec == nil {
			return "", false, errors.New("mysql executor missing")
		}
		res, err := exec.Execute(ctx, dsID, tool.VMIPLookupSQL, executor.ExecuteOptions{
			Timeout:          10,
			MaxRows:          8,
			PositionalParams: []any{vmid},
		})
		if err != nil {
			return "", false, err
		}
		addrs := collectVMIPAddresses(res)
		switch len(addrs) {
		case 0:
			return "", false, errors.New("no IP found for vmid")
		case 1:
			return addrs[0], false, nil
		default:
			return addrs[0], true, nil
		}
	}
}

func collectVMIPAddresses(res *executor.Result) []string {
	if res == nil {
		return nil
	}
	col := 0
	for i, name := range res.Columns {
		if strings.EqualFold(strings.TrimSpace(name), "mgr_ipv4_address") {
			col = i
			break
		}
	}
	var addrs []string
	for _, row := range res.Rows {
		if col >= len(row) {
			continue
		}
		if s := vmIPCellString(row[col]); s != "" {
			addrs = append(addrs, s)
		}
	}
	return addrs
}

func vmIPCellString(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case []byte:
		return strings.TrimSpace(string(t))
	case fmt.Stringer:
		return strings.TrimSpace(t.String())
	default:
		s := strings.TrimSpace(fmt.Sprint(t))
		if s == "<nil>" {
			return ""
		}
		return s
	}
}

// rcaTimeoutSeconds converts config values decoded from structpb to durations.
func rcaTimeoutSeconds(v interface{}) (time.Duration, bool) {
	switch seconds := v.(type) {
	case float64:
		return time.Duration(seconds * float64(time.Second)), true
	case int:
		return time.Duration(seconds) * time.Second, true
	default:
		return 0, false
	}
}

// stringSliceFromAny 把 structpb 解出的 []any / []string 归一化为 []string。
func stringSliceFromAny(v interface{}) []string {
	switch xs := v.(type) {
	case []string:
		return xs
	case []interface{}:
		out := make([]string, 0, len(xs))
		for _, e := range xs {
			if s, ok := e.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}
