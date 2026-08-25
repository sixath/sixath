package data

import (
	"context"
	"path/filepath"

	"backend/internal/biz"
	"backend/internal/conf"
	"backend/internal/data/model"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var _ biz.AgentRepo = (*agentRepo)(nil)

type agentRepo struct {
	db       *gorm.DB
	log      *log.Helper
	dataRoot string
}

// NewAgentRepo creates a new AgentRepo (MySQL/GORM impl)
func NewAgentRepo(data *Data, config *conf.Data, logger log.Logger) biz.AgentRepo {
	if data == nil || data.db == nil {
		panic("NewAgentRepo: Data.db is nil, database config required")
	}
	dataRoot := ""
	if config != nil {
		dataRoot = config.GetDataRoot()
	}
	return &agentRepo{db: data.db, log: log.NewHelper(logger), dataRoot: dataRoot}
}

func modelConfigToBiz(m model.ModelConfig) biz.ModelConfig {
	return biz.ModelConfig{
		Provider:        getStr(m, "provider"),
		Model:           getStr(m, "model"),
		APIKey:          getStr(m, "api_key"),
		BaseURL:         getStr(m, "base_url"),
		MaxOutputTokens: getInt(m, "max_output_tokens"),
		CodeProvider:    getStr(m, "code_provider"),
		CodeModel:       getStr(m, "code_model"),
		CodeAPIKey:      getStr(m, "code_api_key"),
		CodeBaseURL:     getStr(m, "code_base_url"),
	}
}

func bizModelConfigToMap(c biz.ModelConfig) model.ModelConfig {
	m := make(model.ModelConfig)
	if c.Provider != "" {
		m["provider"] = c.Provider
	}
	if c.Model != "" {
		m["model"] = c.Model
	}
	if c.APIKey != "" {
		m["api_key"] = c.APIKey
	}
	if c.BaseURL != "" {
		m["base_url"] = c.BaseURL
	}
	if c.MaxOutputTokens > 0 {
		m["max_output_tokens"] = c.MaxOutputTokens
	}
	if c.CodeProvider != "" {
		m["code_provider"] = c.CodeProvider
	}
	if c.CodeModel != "" {
		m["code_model"] = c.CodeModel
	}
	if c.CodeAPIKey != "" {
		m["code_api_key"] = c.CodeAPIKey
	}
	if c.CodeBaseURL != "" {
		m["code_base_url"] = c.CodeBaseURL
	}
	return m
}

func getInt(m map[string]interface{}, k string) int {
	if v, ok := m[k]; ok {
		switch x := v.(type) {
		case float64:
			return int(x)
		case int:
			return x
		case int64:
			return int(x)
		}
	}
	return 0
}

func getStr(m map[string]interface{}, k string) string {
	if v, ok := m[k]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func bizRuntimeToolsToModel(c biz.RuntimeToolsConfig) model.RuntimeToolsConfig {
	out := model.RuntimeToolsConfig{
		MemoryWriteEnabled:        c.MemoryWriteEnabled,
		SkillRuntimeManageEnabled: c.SkillRuntimeManageEnabled,
		TodoEnabled:               c.TodoEnabled,
		WorkspaceFilesEnabled:     c.WorkspaceFilesEnabled,
		WebToolsEnabled:           c.WebToolsEnabled,
		TerminalLocalEnabled:      c.TerminalLocalEnabled,
		CronjobToolEnabled:        c.CronjobToolEnabled,
		BrowserEnabled:            c.BrowserEnabled,
		MEAEnabled:                c.MEAEnabled,
	}
	if c.HybridRecall != nil {
		v := *c.HybridRecall
		out.HybridRecall = &v
	}
	if c.HubGovernance != nil {
		v := *c.HubGovernance
		out.HubGovernance = &v
	}
	if c.HubKnowledge != nil {
		v := *c.HubKnowledge
		out.HubKnowledge = &v
	}
	if c.HubFallbackToDefaultOnReadError != nil {
		v := *c.HubFallbackToDefaultOnReadError
		out.HubFallbackToDefaultOnReadError = &v
	}
	return out
}

func modelRuntimeToolsToBiz(c model.RuntimeToolsConfig) biz.RuntimeToolsConfig {
	out := biz.RuntimeToolsConfig{
		MemoryWriteEnabled:        c.MemoryWriteEnabled,
		SkillRuntimeManageEnabled: c.SkillRuntimeManageEnabled,
		TodoEnabled:               c.TodoEnabled,
		WorkspaceFilesEnabled:     c.WorkspaceFilesEnabled,
		WebToolsEnabled:           c.WebToolsEnabled,
		TerminalLocalEnabled:      c.TerminalLocalEnabled,
		CronjobToolEnabled:        c.CronjobToolEnabled,
		BrowserEnabled:            c.BrowserEnabled,
		MEAEnabled:                c.MEAEnabled,
	}
	if c.HybridRecall != nil {
		v := *c.HybridRecall
		out.HybridRecall = &v
	}
	if c.HubGovernance != nil {
		v := *c.HubGovernance
		out.HubGovernance = &v
	}
	if c.HubKnowledge != nil {
		v := *c.HubKnowledge
		out.HubKnowledge = &v
	}
	if c.HubFallbackToDefaultOnReadError != nil {
		v := *c.HubFallbackToDefaultOnReadError
		out.HubFallbackToDefaultOnReadError = &v
	}
	return out
}

func agentRowToMeta(m *model.Agent, toolIDs, mcpServerIDs []string) *biz.AgentMeta {
	if toolIDs == nil {
		toolIDs = []string{}
	}
	if mcpServerIDs == nil {
		mcpServerIDs = []string{}
	}
	return &biz.AgentMeta{
		ID:             m.ID,
		Name:           m.Name,
		Description:    m.Description,
		SystemPrompt:   m.SystemPrompt,
		ModelConfig:    modelConfigToBiz(m.ModelConfig),
		Workspace:      m.Workspace,
		DebugRun:       m.DebugRun,
		WecomChannelID: m.WecomChannelID,
		RuntimeTools:   modelRuntimeToolsToBiz(m.RuntimeTools),
		ToolIDs:        append([]string{}, toolIDs...),
		McpServerIDs:   append([]string{}, mcpServerIDs...),
		CreatedAt:      m.CreatedAt,
		UpdatedAt:      m.UpdatedAt,
	}
}

func (r *agentRepo) Create(ctx context.Context, id, name, description, systemPrompt, workspace string, modelConfig biz.ModelConfig, debugRun bool, wecomChannelID string, runtimeTools biz.RuntimeToolsConfig, toolIDs []string) (*biz.AgentMeta, error) {
	if id == "" {
		id = uuid.New().String()
	}
	if workspace == "" {
		workspace = filepath.Join(r.dataRoot, "agents", id)
	}
	agent := &model.Agent{
		ID:             id,
		Name:           name,
		Description:    description,
		SystemPrompt:   systemPrompt,
		ModelConfig:    bizModelConfigToMap(modelConfig),
		Workspace:      workspace,
		DebugRun:       debugRun,
		WecomChannelID: wecomChannelID,
		RuntimeTools:   bizRuntimeToolsToModel(runtimeTools),
	}
	if err := r.db.WithContext(ctx).Create(agent).Error; err != nil {
		if isDuplicateKey(err) {
			return nil, ErrDuplicateName
		}
		return nil, err
	}

	// bind tools
	if len(toolIDs) > 0 {
		for i, tid := range toolIDs {
			at := &model.AgentTool{AgentID: id, ToolID: tid, SortOrder: i}
			if err := r.db.WithContext(ctx).Create(at).Error; err != nil {
				r.log.WithContext(ctx).Warnf("bind tool %s: %v", tid, err)
			}
		}
	}

	return agentRowToMeta(agent, toolIDs, nil), nil
}

func (r *agentRepo) GetByID(ctx context.Context, id string) (*biz.AgentMeta, error) {
	var m model.Agent
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&m).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrNotFound
		}
		return nil, err
	}

	// load tool_ids from agent_tools
	var toolIDs []string
	r.db.WithContext(ctx).Model(&model.AgentTool{}).Where("agent_id = ?", id).Order("sort_order ASC, created_at ASC").Pluck("tool_id", &toolIDs)
	if toolIDs == nil {
		toolIDs = []string{}
	}
	var mcpServerIDs []string
	r.db.WithContext(ctx).Model(&model.AgentMcpServer{}).Where("agent_id = ?", id).Order("sort_order ASC, created_at ASC").Pluck("server_id", &mcpServerIDs)
	if mcpServerIDs == nil {
		mcpServerIDs = []string{}
	}

	return agentRowToMeta(&m, toolIDs, mcpServerIDs), nil
}

func (r *agentRepo) GetByName(ctx context.Context, name string) (*biz.AgentMeta, error) {
	var m model.Agent
	if err := r.db.WithContext(ctx).Where("name = ?", name).First(&m).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return r.GetByID(ctx, m.ID)
}

func (r *agentRepo) List(ctx context.Context, page, pageSize int32) ([]*biz.AgentMeta, int, error) {
	var total int64
	if err := r.db.WithContext(ctx).Model(&model.Agent{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 10
	}
	offset := int((page - 1) * pageSize)

	var rows []model.Agent
	if err := r.db.WithContext(ctx).Order("created_at DESC").Offset(offset).Limit(int(pageSize)).Find(&rows).Error; err != nil {
		return nil, 0, err
	}

	return r.attachToolIDs(ctx, rows, int(total))
}

func (r *agentRepo) ListByIDs(ctx context.Context, ids []string, page, pageSize int32) ([]*biz.AgentMeta, int, error) {
	if len(ids) == 0 {
		return []*biz.AgentMeta{}, 0, nil
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 10
	}
	offset := int((page - 1) * pageSize)

	var total int64
	if err := r.db.WithContext(ctx).Model(&model.Agent{}).Where("id IN ?", ids).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []model.Agent
	if err := r.db.WithContext(ctx).
		Where("id IN ?", ids).
		Order("created_at DESC").
		Offset(offset).
		Limit(int(pageSize)).
		Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return r.attachToolIDs(ctx, rows, int(total))
}

func (r *agentRepo) attachToolIDs(ctx context.Context, rows []model.Agent, total int) ([]*biz.AgentMeta, int, error) {
	items := make([]*biz.AgentMeta, len(rows))
	if len(rows) == 0 {
		return items, total, nil
	}
	agentIDs := make([]string, len(rows))
	for i := range rows {
		agentIDs[i] = rows[i].ID
	}
	type toolRow struct {
		AgentID string `gorm:"column:agent_id"`
		ToolID  string `gorm:"column:tool_id"`
	}
	var toolRows []toolRow
	if err := r.db.WithContext(ctx).
		Model(&model.AgentTool{}).
		Select("agent_id, tool_id").
		Where("agent_id IN ?", agentIDs).
		Order("sort_order ASC, created_at ASC").
		Find(&toolRows).Error; err != nil {
		return nil, 0, err
	}
	byAgent := make(map[string][]string, len(agentIDs))
	for _, tr := range toolRows {
		byAgent[tr.AgentID] = append(byAgent[tr.AgentID], tr.ToolID)
	}
	type mcpRow struct {
		AgentID  string `gorm:"column:agent_id"`
		ServerID string `gorm:"column:server_id"`
	}
	var mcpRows []mcpRow
	if err := r.db.WithContext(ctx).
		Model(&model.AgentMcpServer{}).
		Select("agent_id, server_id").
		Where("agent_id IN ?", agentIDs).
		Order("sort_order ASC, created_at ASC").
		Find(&mcpRows).Error; err != nil {
		return nil, 0, err
	}
	mcpByAgent := make(map[string][]string, len(agentIDs))
	for _, mr := range mcpRows {
		mcpByAgent[mr.AgentID] = append(mcpByAgent[mr.AgentID], mr.ServerID)
	}
	for i := range rows {
		toolIDs := byAgent[rows[i].ID]
		if toolIDs == nil {
			toolIDs = []string{}
		}
		mcpIDs := mcpByAgent[rows[i].ID]
		if mcpIDs == nil {
			mcpIDs = []string{}
		}
		items[i] = agentRowToMeta(&rows[i], toolIDs, mcpIDs)
	}
	return items, total, nil
}

func (r *agentRepo) Update(ctx context.Context, id string, updates map[string]any) (*biz.AgentMeta, error) {
	var m model.Agent
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&m).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrNotFound
		}
		return nil, err
	}

	upd := make(map[string]interface{})
	for k, v := range updates {
		switch k {
		case "name":
			upd["name"] = v.(string)
		case "description":
			upd["description"] = v.(string)
		case "system_prompt":
			upd["system_prompt"] = v.(string)
		case "model_config":
			upd["model_config"] = bizModelConfigToMap(v.(biz.ModelConfig))
		case "workspace":
			upd["workspace"] = v.(string)
		case "debug_run":
			upd["debug_run"] = v.(bool)
		case "wecom_channel_id":
			upd["wecom_channel_id"] = v.(string)
		case "runtime_tools":
			upd["runtime_tools"] = bizRuntimeToolsToModel(v.(biz.RuntimeToolsConfig))
		}
	}
	if len(upd) > 0 {
		if err := r.db.WithContext(ctx).Model(&m).Updates(upd).Error; err != nil {
			if isDuplicateKey(err) {
				return nil, ErrDuplicateName
			}
			return nil, err
		}
	}
	return r.GetByID(ctx, id)
}

func (r *agentRepo) Delete(ctx context.Context, id string) error {
	// agent_tools 有 ON DELETE CASCADE，或手动删除
	r.db.WithContext(ctx).Where("agent_id = ?", id).Delete(&model.AgentTool{})
	res := r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.Agent{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *agentRepo) BindTools(ctx context.Context, agentID string, toolIDs []string) error {
	var m model.Agent
	if err := r.db.WithContext(ctx).Where("id = ?", agentID).First(&m).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return ErrNotFound
		}
		return err
	}

	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("agent_id = ?", agentID).Delete(&model.AgentTool{}).Error; err != nil {
			return err
		}
		for i, tid := range toolIDs {
			at := &model.AgentTool{AgentID: agentID, ToolID: tid, SortOrder: i}
			if err := tx.Create(at).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *agentRepo) UnbindTools(ctx context.Context, agentID string, toolIDs []string) error {
	if len(toolIDs) == 0 {
		return nil
	}
	res := r.db.WithContext(ctx).Where("agent_id = ? AND tool_id IN ?", agentID, toolIDs).Delete(&model.AgentTool{})
	return res.Error
}

func (r *agentRepo) ListDistinctWorkspaces(ctx context.Context, limit int) ([]biz.CuratorWorkspace, error) {
	if limit <= 0 {
		limit = 200
	}
	type row struct {
		Workspace string
		AgentID   string
	}
	var rows []row
	err := r.db.WithContext(ctx).Model(&model.Agent{}).
		Select("workspace, MIN(id) AS agent_id").
		Where("workspace <> ''").
		Group("workspace").
		Limit(limit).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]biz.CuratorWorkspace, len(rows))
	for i, rw := range rows {
		out[i] = biz.CuratorWorkspace{WorkspaceKey: rw.Workspace, AgentID: rw.AgentID}
	}
	return out, nil
}

func (r *agentRepo) CountByWecomChannelID(ctx context.Context, channelID string) (int, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.Agent{}).Where("wecom_channel_id = ?", channelID).Count(&count).Error
	return int(count), err
}

func (r *agentRepo) ListAgentIDsByWorkspace(ctx context.Context, workspace string) ([]string, error) {
	if workspace == "" {
		return nil, nil
	}
	var ids []string
	err := r.db.WithContext(ctx).Model(&model.Agent{}).
		Where("workspace = ?", workspace).
		Pluck("id", &ids).Error
	return ids, err
}
