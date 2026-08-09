package service

import (
	"context"
	"time"

	"backend/api/common"
	toolv1 "backend/api/tool/v1"
	"backend/internal/biz"

	"google.golang.org/protobuf/types/known/structpb"
)

// ToolService implements tool.v1.ToolHTTPServer and tool.v1.ToolServer
type ToolService struct {
	toolv1.UnimplementedToolServer
	uc *biz.ToolUsecase
}

// NewToolService creates a ToolService
func NewToolService(uc *biz.ToolUsecase) *ToolService {
	return &ToolService{uc: uc}
}

func toolMetaToReply(m *biz.ToolMeta) *toolv1.ToolReply {
	config := m.Config
	if config == nil {
		config = &structpb.Struct{Fields: make(map[string]*structpb.Value)}
	}
	return &toolv1.ToolReply{
		Ret:         &common.BaseResponse{Code: 0, Message: "ok"},
		Id:          m.ID,
		Name:        m.Name,
		Description: m.Description,
		Type:        string(m.Type),
		Config:      structToToolConfig(config),
		CreatedAt:   m.CreatedAt.Format(time.RFC3339),
		UpdatedAt:   m.UpdatedAt.Format(time.RFC3339),
	}
}

func structToToolConfig(s *structpb.Struct) *toolv1.ToolConfig {
	if s == nil || s.Fields == nil {
		return &toolv1.ToolConfig{}
	}
	c := &toolv1.ToolConfig{}
	if v, ok := s.Fields["func_path"]; ok && v.GetStringValue() != "" {
		c.FuncPath = v.GetStringValue()
	}
	if v, ok := s.Fields["parameters"]; ok && v.GetStructValue() != nil {
		c.Parameters = v.GetStructValue()
	}
	if v, ok := s.Fields["async"]; ok {
		c.Async = v.GetBoolValue()
	}
	if v, ok := s.Fields["mcp_server_id"]; ok {
		c.McpServerId = v.GetStringValue()
	}
	if v, ok := s.Fields["mcp_endpoint"]; ok {
		c.McpEndpoint = v.GetStringValue()
	}
	if v, ok := s.Fields["mcp_backend"]; ok {
		c.McpBackend = v.GetStringValue()
	}
	if v, ok := s.Fields["timeout_sec"]; ok {
		c.TimeoutSec = int32(v.GetNumberValue())
	}
	// McpConfig 嵌套（endpoint/id/backend），与 framework/tool.McpConfig 对齐
	if v, ok := s.Fields["mcp"]; ok && v.GetStructValue() != nil {
		ms := v.GetStructValue().GetFields()
		c.Mcp = &toolv1.McpConfig{}
		if x, ok := ms["endpoint"]; ok {
			c.Mcp.Endpoint = x.GetStringValue()
		}
		if x, ok := ms["id"]; ok {
			c.Mcp.Id = x.GetStringValue()
		}
		if x, ok := ms["backend"]; ok {
			c.Mcp.Backend = x.GetStringValue()
		}
	}
	// 若嵌套为空则用扁平字段填充 mcp
	needFlat := c.McpEndpoint != "" || c.McpServerId != "" || c.McpBackend != ""
	if needFlat {
		if c.Mcp == nil {
			c.Mcp = &toolv1.McpConfig{}
		}
		if c.Mcp.Endpoint == "" {
			c.Mcp.Endpoint = c.McpEndpoint
		}
		if c.Mcp.Id == "" {
			c.Mcp.Id = c.McpServerId
		}
		if c.Mcp.Backend == "" {
			c.Mcp.Backend = c.McpBackend
		}
	}
	// DatasourceConfig 嵌套
	if v, ok := s.Fields["datasource"]; ok && v.GetStructValue() != nil {
		ds := v.GetStructValue().GetFields()
		c.Datasource = &toolv1.DatasourceConfig{}
		if x, ok := ds["id"]; ok {
			c.Datasource.Id = x.GetStringValue()
		}
		if x, ok := ds["type"]; ok {
			c.Datasource.Type = x.GetStringValue()
		}
		if x, ok := ds["dsn"]; ok {
			c.Datasource.Dsn = x.GetStringValue()
		}
		if x, ok := ds["host"]; ok {
			c.Datasource.Host = x.GetStringValue()
		}
		if x, ok := ds["port"]; ok {
			c.Datasource.Port = int32(x.GetNumberValue())
		}
		if x, ok := ds["user"]; ok {
			c.Datasource.User = x.GetStringValue()
		}
		if x, ok := ds["password"]; ok {
			c.Datasource.Password = x.GetStringValue()
		}
		if x, ok := ds["dbname"]; ok {
			c.Datasource.Dbname = x.GetStringValue()
		}
		if x, ok := ds["read_only"]; ok {
			c.Datasource.ReadOnly = x.GetBoolValue()
		}
	}
	// RCAConfig 嵌套
	if v, ok := s.Fields["rca"]; ok && v.GetStructValue() != nil {
		rc := v.GetStructValue().GetFields()
		c.Rca = &toolv1.RCAConfig{}
		if x, ok := rc["func_path"]; ok {
			c.Rca.FuncPath = x.GetStringValue()
		}
		if x, ok := rc["roots"]; ok && x.GetListValue() != nil {
			for _, e := range x.GetListValue().GetValues() {
				if s := e.GetStringValue(); s != "" {
					c.Rca.Roots = append(c.Rca.Roots, s)
				}
			}
		}
		if x, ok := rc["query_url"]; ok {
			c.Rca.QueryUrl = x.GetStringValue()
		}
		if x, ok := rc["datasource_id"]; ok {
			c.Rca.DatasourceId = x.GetStringValue()
		}
		if x, ok := rc["default_index"]; ok {
			c.Rca.DefaultIndex = x.GetStringValue()
		}
		if x, ok := rc["trace_id_field"]; ok {
			c.Rca.TraceIdField = x.GetStringValue()
		}
	}
	return c
}

func protoToolConfigToStruct(c *toolv1.ToolConfig) *structpb.Struct {
	if c == nil {
		return &structpb.Struct{Fields: make(map[string]*structpb.Value)}
	}
	fields := make(map[string]*structpb.Value)
	if c.FuncPath != "" {
		fields["func_path"], _ = structpb.NewValue(c.FuncPath)
	}
	if c.Parameters != nil {
		fields["parameters"] = structpb.NewStructValue(c.Parameters)
	}
	fields["async"], _ = structpb.NewValue(c.Async)
	// MCP：优先用嵌套 mcp，否则用扁平字段
	if c.Mcp != nil && (c.Mcp.Endpoint != "" || c.Mcp.Id != "" || c.Mcp.Backend != "") {
		mcpFields := map[string]interface{}{
			"endpoint": c.Mcp.Endpoint,
			"id":       c.Mcp.Id,
			"backend":  c.Mcp.Backend,
		}
		if ms, err := structpb.NewStruct(mcpFields); err == nil {
			fields["mcp"], _ = structpb.NewValue(structpb.NewStructValue(ms))
		}
	}
	if c.McpServerId != "" {
		fields["mcp_server_id"], _ = structpb.NewValue(c.McpServerId)
	}
	if c.McpEndpoint != "" {
		fields["mcp_endpoint"], _ = structpb.NewValue(c.McpEndpoint)
	}
	if c.McpBackend != "" {
		fields["mcp_backend"], _ = structpb.NewValue(c.McpBackend)
	}
	if c.TimeoutSec != 0 {
		fields["timeout_sec"], _ = structpb.NewValue(float64(c.TimeoutSec))
	}
	// DatasourceConfig：逐字段构建，避免 NewStruct(map) 对部分 key 的兼容问题导致结果为 nil
	if c.Datasource != nil && (c.Datasource.Id != "" || c.Datasource.Type != "" || c.Datasource.Dsn != "") {
		dsFields := make(map[string]*structpb.Value)
		dsFields["id"], _ = structpb.NewValue(c.Datasource.Id)
		dsFields["type"], _ = structpb.NewValue(c.Datasource.Type)
		dsFields["dsn"], _ = structpb.NewValue(c.Datasource.Dsn)
		dsFields["host"], _ = structpb.NewValue(c.Datasource.Host)
		dsFields["port"], _ = structpb.NewValue(float64(c.Datasource.Port))
		dsFields["user"], _ = structpb.NewValue(c.Datasource.User)
		dsFields["password"], _ = structpb.NewValue(c.Datasource.Password)
		dsFields["dbname"], _ = structpb.NewValue(c.Datasource.Dbname)
		dsFields["read_only"], _ = structpb.NewValue(c.Datasource.ReadOnly)
		fields["datasource"] = structpb.NewStructValue(&structpb.Struct{Fields: dsFields})
	}
	if c.Rca != nil {
		rcaFields := map[string]interface{}{
			"func_path":      c.Rca.FuncPath,
			"query_url":      c.Rca.QueryUrl,
			"datasource_id":  c.Rca.DatasourceId,
			"default_index":  c.Rca.DefaultIndex,
			"trace_id_field": c.Rca.TraceIdField,
		}
		roots := make([]interface{}, 0, len(c.Rca.Roots))
		for _, r := range c.Rca.Roots {
			roots = append(roots, r)
		}
		rcaFields["roots"] = roots
		if rv, err := structpb.NewValue(rcaFields); err == nil {
			fields["rca"] = rv
		}
	}
	return &structpb.Struct{Fields: fields}
}

// CreateTool implements tool.v1.ToolHTTPServer
func (s *ToolService) CreateTool(ctx context.Context, req *toolv1.CreateToolRequest) (*toolv1.ToolReply, error) {
	config := protoToolConfigToStruct(req.GetConfig())
	tool, err := s.uc.Create(ctx, req.GetName(), req.GetDescription(), req.GetType(), config)
	if err != nil {
		return nil, err
	}
	return toolMetaToReply(tool), nil
}

// GetTool implements tool.v1.ToolHTTPServer
func (s *ToolService) GetTool(ctx context.Context, req *toolv1.GetToolRequest) (*toolv1.ToolReply, error) {
	tool, err := s.uc.Get(ctx, req.GetId())
	if err != nil {
		return nil, err
	}
	return toolMetaToReply(tool), nil
}

// ListTools implements tool.v1.ToolHTTPServer
func (s *ToolService) ListTools(ctx context.Context, req *toolv1.ListToolsRequest) (*toolv1.ListToolsReply, error) {
	items, total, err := s.uc.List(ctx, req.GetPage(), req.GetPageSize(), req.GetName(), req.GetType())
	if err != nil {
		return nil, err
	}
	replies := make([]*toolv1.ToolReply, len(items))
	for i, m := range items {
		replies[i] = toolMetaToReply(m)
	}
	return &toolv1.ListToolsReply{Ret: &common.BaseResponse{Code: 0, Message: "ok"}, Items: replies, Total: int32(total)}, nil
}

// UpdateTool implements tool.v1.ToolHTTPServer
func (s *ToolService) UpdateTool(ctx context.Context, req *toolv1.UpdateToolRequest) (*toolv1.ToolReply, error) {
	var name, desc, toolType *string
	var config *structpb.Struct
	if req.Name != nil {
		name = req.Name
	}
	if req.Description != nil {
		desc = req.Description
	}
	if req.Type != nil {
		toolType = req.Type
	}
	if req.Config != nil {
		config = protoToolConfigToStruct(req.Config)
	}
	tool, err := s.uc.Update(ctx, req.GetId(), toolType, name, desc, config)
	if err != nil {
		return nil, err
	}
	return toolMetaToReply(tool), nil
}

// DeleteTool implements tool.v1.ToolHTTPServer
func (s *ToolService) DeleteTool(ctx context.Context, req *toolv1.DeleteToolRequest) (*toolv1.DeleteToolReply, error) {
	if err := s.uc.Delete(ctx, req.GetId()); err != nil {
		return nil, err
	}
	return &toolv1.DeleteToolReply{Ret: &common.BaseResponse{Code: 0, Message: "ok"}}, nil
}
