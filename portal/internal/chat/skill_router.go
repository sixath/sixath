package chat

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"strings"
	"sync"

	"backend/internal/biz"

	"github.com/sixath/framework/skills"
)

// Skill 语义路由：启动/变更时批量 embed 全部 Skill 的 "name + description + tags"，
// 运行时按 query 余弦相似度取 top-1，命中则把 SKILL.md 正文注入 system prompt。
//
// embedding 模型复用 resolveVectorEmbedModel 的解析链
// （memory_vector.embedding → extraction.auxiliary → agent chat model），零新增配置。
// 任何一步不可用（无索引、无 embedding 模型、embed 失败）都返回 nil，
// 调用方按「未启用语义路由」处理，Agent 退化为模型自主 load_skill。
//
// Chat 路径每请求重建 Agent，因此 router 按「skills 内容 + embed 模型身份」做进程级缓存：
// 配置与技能不变时整个会话期间只 embed 一次；embed 失败不缓存（临时故障不致永久禁用）。

var (
	skillRouterMu   sync.Mutex
	skillRouterKey  string
	skillRouterInst *skills.EmbedRouter
)

// SkillEmbedRouterFor 返回按缓存键复用的语义路由器；不可用或未启用时返回 nil。
func SkillEmbedRouterFor(ctx context.Context, idx *skills.Index, agentMeta *biz.AgentMeta) *skills.EmbedRouter {
	if idx == nil || len(skills.VisibleSkills(idx.All())) == 0 {
		return nil
	}
	key := skillRouterCacheKey(idx, agentMeta)

	skillRouterMu.Lock()
	if skillRouterInst != nil && skillRouterKey == key {
		r := skillRouterInst
		skillRouterMu.Unlock()
		return r
	}
	skillRouterMu.Unlock()

	r := buildSkillEmbedRouter(ctx, idx, agentMeta)
	if r == nil {
		return nil
	}

	skillRouterMu.Lock()
	skillRouterInst = r
	skillRouterKey = key
	skillRouterMu.Unlock()
	return r
}

// buildSkillEmbedRouter 实际执行批量 embed 并构造路由器；失败返回 nil。
func buildSkillEmbedRouter(ctx context.Context, idx *skills.Index, agentMeta *biz.AgentMeta) *skills.EmbedRouter {
	m, err := resolveVectorEmbedModel(agentMeta)
	if err != nil || m == nil {
		if err != nil {
			log.Printf("skill route: resolve embed model failed: %v", err)
		}
		return nil
	}
	embed := func(ctx context.Context, texts []string) ([][]float32, error) {
		embs, err := m.Embed(ctx, texts)
		if err != nil {
			return nil, err
		}
		out := make([][]float32, len(embs))
		for i, e := range embs {
			out[i] = e.Vector
		}
		return out, nil
	}
	// threshold=0 → 使用 skills.DefaultEmbedRouteThreshold（0.5）
	router, err := skills.NewEmbedRouter(ctx, idx, embed, 0)
	if err != nil {
		log.Printf("skill route: build embed router failed: %v", err)
		return nil
	}
	return router
}

// skillRouterCacheKey 由 skills 内容哈希 + embed 模型身份组成；任一变化都会触发重建。
func skillRouterCacheKey(idx *skills.Index, agentMeta *biz.AgentMeta) string {
	var b strings.Builder
	for _, m := range skills.VisibleSkills(idx.All()) {
		b.WriteString(m.Name)
		b.WriteByte('|')
		b.WriteString(m.Description)
		b.WriteByte('|')
		b.WriteString(strings.Join(m.Tags, ","))
		b.WriteByte(';')
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:16]) + "@" + skillEmbedModelKey(agentMeta)
}

// skillEmbedModelKey 与 resolveVectorEmbedModel 的优先级保持一致：
// memory_vector.embedding → extraction.auxiliary → agent chat model。
func skillEmbedModelKey(agentMeta *biz.AgentMeta) string {
	if storedVectorYAML != nil && storedVectorYAML.Embedding != nil {
		if e := storedVectorYAML.Embedding; strings.TrimSpace(e.Model) != "" {
			return "mv:" + e.Provider + "/" + e.Model + "@" + e.BaseURL
		}
	}
	if storedExtractionYAML != nil && storedExtractionYAML.Auxiliary != nil {
		if aux := storedExtractionYAML.Auxiliary; strings.TrimSpace(aux.Model) != "" {
			return "aux:" + aux.Provider + "/" + aux.Model + "@" + aux.BaseURL
		}
	}
	if agentMeta != nil {
		mc := agentMeta.ModelConfig
		return "chat:" + mc.Provider + "/" + mc.Model + "@" + mc.BaseURL
	}
	return "none"
}
