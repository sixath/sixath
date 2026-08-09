package memorysearch

import (
	"context"
	"path/filepath"

	"github.com/sixath/framework/config"
)

// MemorySearchManager 工作区记忆检索管理器接口。
type MemorySearchManager interface {
	Search(ctx context.Context, query string, opts *SearchOpts) ([]MemorySearchResult, error)
	ReadFile(ctx context.Context, params *ReadFileParams) (*ReadFileResult, error)
	Status(ctx context.Context) (*MemoryProviderStatus, error)
	Sync(ctx context.Context, params *SyncParams) error
}

// SearchOpts 检索选项。
type SearchOpts struct {
	MaxResults int
	MinScore   float64
}

// MemorySearchResult 单条检索结果。
type MemorySearchResult struct {
	Path      string
	StartLine int
	EndLine   int
	Score     float64
	Snippet   string
	Source    string
	Citation  string
}

// ReadFileParams 读文件参数。
type ReadFileParams struct {
	RelPath string
	From    int
	Lines   int
}

// ReadFileResult 读文件结果。
type ReadFileResult struct {
	Text string
	Path string
}

// MemoryProviderStatus 记忆服务状态。
type MemoryProviderStatus struct {
	Backend  string
	Provider string
	Model    string
	Files    int
	Chunks   int
	Vector   bool
	FTS      bool
	Cache    int
}

// SyncParams 同步参数。
type SyncParams struct {
	Reason   string
	Force    bool
	Progress func(phase string, current, total int)
}

// ResolvedMemorySearchConfig 解析后的记忆检索配置。
type ResolvedMemorySearchConfig struct {
	Enabled             bool
	Sources             []string
	ExtraPaths          []string
	Provider            string
	Model               string
	StorePath           string
	ChunkTokens         int
	ChunkOverlap        int
	OnSearch            bool
	Watch               bool
	WatchDebounceMs     int
	IntervalMinutes     int
	MaxResults          int
	MinScore            float64
	TimeoutSec          int
	HybridEnabled       bool
	VectorWeight        float64
	TextWeight          float64
	CandidateMultiplier int
	CacheEnabled        bool
	CacheMaxEntries     int
	// Phase 2: sessions 源
	SessionsDeltaBytes    int
	SessionsDeltaMessages int
}

// SessionTranscriptProvider 会话转录提供者（Phase 2）。由 Portal 或上层实现并注入。
// 虚拟路径约定：sessions/{sessionID}.md
type SessionTranscriptProvider interface {
	// ListSessionsForAgent 返回该 Agent 下需索引的会话 ID 列表。
	ListSessionsForAgent(ctx context.Context, agentID string) ([]string, error)
	// GetTranscript 返回会话的 Markdown 转录内容。
	GetTranscript(ctx context.Context, sessionID string) (string, error)
}

// SessionDirtyNotifier 会话脏标记通知（Phase 2）。可选接口，*MemoryIndexManager 与 *FallbackMemoryManager 实现。
// 上层在会话转录更新时调用 NotifySessionDirty，触发 session-delta 同步。
type SessionDirtyNotifier interface {
	NotifySessionDirty(sessionID string, bytesDelta, messagesDelta int)
}

// ResolveMemorySearch 从 config 解析 MemorySearchConfig。
func ResolveMemorySearch(cfg config.MemorySearchConfig, workspaceRoot string) *ResolvedMemorySearchConfig {
	if !cfg.Enabled {
		return nil
	}
	sources := cfg.Sources
	if len(sources) == 0 {
		sources = []string{"memory"}
	}
	tokens := cfg.Chunking.Tokens
	if tokens <= 0 {
		tokens = 512
	}
	overlap := cfg.Chunking.Overlap
	if overlap < 0 {
		overlap = 64
	}
	maxResults := cfg.Query.MaxResults
	if maxResults <= 0 {
		maxResults = 10
	}
	minScore := cfg.Query.MinScore
	if minScore <= 0 {
		minScore = 0.3
	}
	timeoutSec := cfg.Query.TimeoutSec
	if timeoutSec <= 0 {
		timeoutSec = 30
	}
	cacheMax := cfg.Cache.MaxEntries
	if cacheMax <= 0 {
		cacheMax = 10000
	}
	storePath := cfg.Store.Path
	if storePath == "" && workspaceRoot != "" {
		storePath = filepath.Join(workspaceRoot, ".memory_index.db")
	}
	res := &ResolvedMemorySearchConfig{
		Enabled:             true,
		Sources:             sources,
		ExtraPaths:          cfg.ExtraPaths,
		Provider:            cfg.Provider,
		Model:               cfg.Model,
		StorePath:           storePath,
		ChunkTokens:         tokens,
		ChunkOverlap:        overlap,
		OnSearch:            cfg.Sync.OnSearch,
		Watch:               cfg.Sync.Watch,
		WatchDebounceMs:     cfg.Sync.WatchDebounceMs,
		IntervalMinutes:     cfg.Sync.IntervalMinutes,
		MaxResults:          maxResults,
		MinScore:            minScore,
		TimeoutSec:          timeoutSec,
		HybridEnabled:       cfg.Query.Hybrid.Enabled,
		VectorWeight:        cfg.Query.Hybrid.VectorWeight,
		TextWeight:          cfg.Query.Hybrid.TextWeight,
		CandidateMultiplier: cfg.Query.Hybrid.CandidateMultiplier,
		CacheEnabled:        cfg.Cache.Enabled,
		CacheMaxEntries:     cacheMax,
	}
	if cfg.Sync.Sessions != nil {
		res.SessionsDeltaBytes = cfg.Sync.Sessions.DeltaBytes
		res.SessionsDeltaMessages = cfg.Sync.Sessions.DeltaMessages
	}
	if res.HybridEnabled {
		if res.VectorWeight <= 0 {
			res.VectorWeight = 0.5
		}
		if res.TextWeight <= 0 {
			res.TextWeight = 0.5
		}
		if res.CandidateMultiplier <= 0 {
			res.CandidateMultiplier = 3
		}
	}
	return res
}
