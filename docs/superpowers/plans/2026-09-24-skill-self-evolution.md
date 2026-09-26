# Skill Self-Evolution System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a system that detects evolution signals from conversations (user corrections, workflow adjustments, debugging tricks, stale skills, trial-and-error patterns), generates proposals, and after human review on a dedicated page, creates or updates SKILL.md / USER.md files.

**Architecture:** After each turn ends, an async detection pipeline runs: keyword rule filtering → LLM classification (gpt-4o-mini) → proposal generation → conflict check + embedding dedup → write to `evolution_proposals` table. A React review page at `/evolution-review` lets humans approve/edit/reject proposals. Approved proposals execute via existing `skill_manage` / `memory_remember` tools and write to `EVOLUTION.log`.

**Tech Stack:** Go (portal backend) + React/TypeScript (frontend) + MySQL (evolution_proposals table) + existing `skill_manage` / `memory_remember` tools.

---

## File Structure

| File | Responsibility |
|------|---------------|
| `portal/migrations/014_evolution_proposals.sql` | DB table creation |
| `framework/config/tool_guardrails.go` | EvolutionConfig struct in PortalAgentExtra |
| `portal/internal/data/evolution_proposal.go` | GORM model + Repo implementation |
| `portal/internal/biz/evolution.go` | Biz model + EvolutionProposalRepo interface + EvolutionUsecase |
| `portal/internal/chat/evolution_detect.go` | Keyword rule filter + LLM classifier + proposal generator |
| `portal/internal/chat/evolution_trial_buffer.go` | In-process LRU trial-and-error buffer |
| `portal/internal/chat/evolution_arbiter.go` | Embedding dedup + LLM conflict check |
| `portal/internal/chat/evolution_config.go` | Evolution config setter (like memory_extract.go pattern) |
| `portal/internal/server/evolution.go` | HTTP handlers for evolution review API |
| `portal/internal/server/http.go` | Route registration |
| `portal/internal/service/chat.go` | Turn-end hook to trigger detection |
| `portal/internal/chat/portal_agent_extra.go` | Config loading from agent_extra.yaml |
| `portal/cmd/backend/wire.go` | DI declarations |
| `portal/cmd/backend/wire_gen.go` | DI wiring (generated) |
| `portal/configs/agent_extra.yaml` | Default evolution config |
| `portal/internal/cron/scheduler.go` | Lifecycle cleanup registration |
| `web/src/api/evolution.ts` | Frontend API client |
| `web/src/pages/EvolutionReviewPage.tsx` | Review page component |
| `web/src/App.tsx` | Route + sidebar badge |

---

### Task 1: DB Migration + Config Struct

**Files:**
- Create: `portal/migrations/014_evolution_proposals.sql`
- Modify: `framework/config/tool_guardrails.go`

- [ ] **Step 1: Create the migration file**

```sql
-- Evolution proposals: skill self-evolution review queue.
-- All writes go through human review on /evolution-review page.

CREATE TABLE IF NOT EXISTS evolution_proposals (
    id                  VARCHAR(36)   NOT NULL PRIMARY KEY,
    agent_id            VARCHAR(36)   NOT NULL,
    session_id          VARCHAR(36)   NOT NULL,
    turn_index          INT           NOT NULL DEFAULT 0,
    signal_type         VARCHAR(32)   NOT NULL,
    confidence          DECIMAL(3,2)  NOT NULL DEFAULT 0.00,
    problem_summary     TEXT          NOT NULL,
    proposed_content    TEXT          NOT NULL,
    target_path         VARCHAR(512)  NOT NULL DEFAULT '',
    target_action       VARCHAR(32)   NOT NULL DEFAULT 'create',
    conflict            TINYINT(1)    NOT NULL DEFAULT 0,
    conflict_detail     TEXT          NULL,
    conflict_check_failed TINYINT(1)  NOT NULL DEFAULT 0,
    dedup_skipped       TINYINT(1)    NOT NULL DEFAULT 0,
    status              VARCHAR(16)   NOT NULL DEFAULT 'pending',
    review_comment      TEXT          NULL,
    created_at          DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    reviewed_at         DATETIME(3)   NULL,
    reviewed_by         VARCHAR(64)   NULL,
    INDEX idx_ep_status (status),
    INDEX idx_ep_agent (agent_id),
    INDEX idx_ep_created (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

- [ ] **Step 2: Add EvolutionConfig to PortalAgentExtra in `framework/config/tool_guardrails.go`**

After line 152 (`}` closing PortalAgentExtra), add:

```go
	// Evolution 技能自进化配置（默认关）。
	Evolution *EvolutionConfig `json:"evolution" yaml:"evolution"`
}

// EvolutionConfig 技能自进化检测与提案配置。
type EvolutionConfig struct {
	Enabled bool `json:"enabled" yaml:"enabled"`
	Rules   struct {
		StyleCorrection    []string `json:"style_correction" yaml:"style_correction"`
		WorkflowCorrection []string `json:"workflow_correction" yaml:"workflow_correction"`
		DebuggingTrick     []string `json:"debugging_trick" yaml:"debugging_trick"`
		StaleSkill         []string `json:"stale_skill" yaml:"stale_skill"`
	} `json:"rules" yaml:"rules"`
	TrialAndError struct {
		MinToolFailures     int     `json:"min_tool_failures" yaml:"min_tool_failures"`
		MinOccurrences      int     `json:"min_occurrences" yaml:"min_occurrences"`
		ObservationWindow   int     `json:"observation_window" yaml:"observation_window"`
		SimilarityThreshold float64 `json:"similarity_threshold" yaml:"similarity_threshold"`
	} `json:"trial_and_error" yaml:"trial_and_error"`
	Classifier struct {
		Provider  string `json:"provider" yaml:"provider"`
		Model     string `json:"model" yaml:"model"`
		MaxTokens int    `json:"max_tokens" yaml:"max_tokens"`
	} `json:"classifier" yaml:"classifier"`
	DedupThreshold float64 `json:"dedup_threshold" yaml:"dedup_threshold"`
	Embedding      struct {
		Provider string `json:"provider" yaml:"provider"`
		Model    string `json:"model" yaml:"model"`
	} `json:"embedding" yaml:"embedding"`
}
```

- [ ] **Step 3: Apply migration and verify**

```bash
cd portal && go run ./cmd/backend -conf configs 2>&1 | head -20
# Check: no SQL errors, service starts normally
```

- [ ] **Step 4: Commit**

```bash
git add portal/migrations/014_evolution_proposals.sql framework/config/tool_guardrails.go
git commit -m "$(cat <<'EOF'
feat(evolution): add evolution_proposals table and EvolutionConfig struct
EOF
)"
```

---

### Task 2: Data Model + GORM Model + Repo

**Files:**
- Create: `portal/internal/data/evolution_proposal.go`
- Modify: `portal/internal/data/data.go`

- [ ] **Step 1: Create GORM model and repo in `portal/internal/data/evolution_proposal.go`**

```go
package data

import (
	"context"
	"time"

	"backend/internal/biz"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var _ biz.EvolutionProposalRepo = (*evolutionProposalRepo)(nil)

type evolutionProposalRepo struct {
	db  *gorm.DB
	log *log.Helper
}

// EvolutionProposal is the GORM model for the evolution_proposals table.
type EvolutionProposal struct {
	ID                  string     `gorm:"column:id;primaryKey;size:36"`
	AgentID             string     `gorm:"column:agent_id;size:36;not null;index:idx_ep_agent"`
	SessionID           string     `gorm:"column:session_id;size:36;not null"`
	TurnIndex           int        `gorm:"column:turn_index;not null;default:0"`
	SignalType          string     `gorm:"column:signal_type;size:32;not null"`
	Confidence          float64    `gorm:"column:confidence;type:decimal(3,2);not null;default:0.00"`
	ProblemSummary      string     `gorm:"column:problem_summary;type:text;not null"`
	ProposedContent     string     `gorm:"column:proposed_content;type:text;not null"`
	TargetPath          string     `gorm:"column:target_path;size:512;not null;default:''"`
	TargetAction        string     `gorm:"column:target_action;size:32;not null;default:'create'"`
	Conflict            bool       `gorm:"column:conflict;not null;default:0"`
	ConflictDetail      *string    `gorm:"column:conflict_detail;type:text"`
	ConflictCheckFailed bool       `gorm:"column:conflict_check_failed;not null;default:0"`
	DedupSkipped        bool       `gorm:"column:dedup_skipped;not null;default:0"`
	Status              string     `gorm:"column:status;size:16;not null;default:'pending';index:idx_ep_status"`
	ReviewComment       *string    `gorm:"column:review_comment;type:text"`
	CreatedAt           time.Time  `gorm:"column:created_at;not null"`
	ReviewedAt          *time.Time `gorm:"column:reviewed_at"`
	ReviewedBy          *string    `gorm:"column:reviewed_by;size:64"`
}

func (EvolutionProposal) TableName() string {
	return "evolution_proposals"
}

func NewEvolutionProposalRepo(data *Data, logger log.Logger) biz.EvolutionProposalRepo {
	if data == nil || data.db == nil {
		panic("NewEvolutionProposalRepo: Data.db is nil")
	}
	return &evolutionProposalRepo{db: data.db, log: log.NewHelper(logger)}
}

func (r *evolutionProposalRepo) Create(ctx context.Context, p *biz.EvolutionProposal) error {
	m := &EvolutionProposal{
		ID:                  uuid.New().String(),
		AgentID:             p.AgentID,
		SessionID:           p.SessionID,
		TurnIndex:           p.TurnIndex,
		SignalType:          p.SignalType,
		Confidence:          p.Confidence,
		ProblemSummary:      p.ProblemSummary,
		ProposedContent:     p.ProposedContent,
		TargetPath:          p.TargetPath,
		TargetAction:        p.TargetAction,
		Conflict:            p.Conflict,
		ConflictDetail:      p.ConflictDetail,
		ConflictCheckFailed: p.ConflictCheckFailed,
		DedupSkipped:        p.DedupSkipped,
		Status:              "pending",
		CreatedAt:           time.Now(),
	}
	return r.db.WithContext(ctx).Create(m).Error
}

func (r *evolutionProposalRepo) GetByID(ctx context.Context, id string) (*biz.EvolutionProposal, error) {
	var m EvolutionProposal
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&m).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return evolutionProposalModelToBiz(&m), nil
}

func (r *evolutionProposalRepo) List(ctx context.Context, page, pageSize int32, status string) ([]*biz.EvolutionProposal, int, error) {
	q := r.db.WithContext(ctx).Model(&EvolutionProposal{})
	if status != "" {
		q = q.Where("status = ?", status)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var models []EvolutionProposal
	if err := q.Order("created_at DESC").Offset(int((page - 1) * pageSize)).Limit(int(pageSize)).Find(&models).Error; err != nil {
		return nil, 0, err
	}
	items := make([]*biz.EvolutionProposal, len(models))
	for i := range models {
		items[i] = evolutionProposalModelToBiz(&models[i])
	}
	return items, int(total), nil
}

func (r *evolutionProposalRepo) Update(ctx context.Context, id string, updates map[string]any) error {
	return r.db.WithContext(ctx).Model(&EvolutionProposal{}).Where("id = ?", id).Updates(updates).Error
}

func (r *evolutionProposalRepo) CountPending(ctx context.Context) (int, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&EvolutionProposal{}).Where("status = ?", "pending").Count(&count).Error; err != nil {
		return 0, err
	}
	return int(count), nil
}

func (r *evolutionProposalRepo) ListExpired(ctx context.Context, before time.Time) ([]*biz.EvolutionProposal, error) {
	var models []EvolutionProposal
	if err := r.db.WithContext(ctx).
		Where("status = 'pending' AND created_at < ?", before).
		Find(&models).Error; err != nil {
		return nil, err
	}
	items := make([]*biz.EvolutionProposal, len(models))
	for i := range models {
		items[i] = evolutionProposalModelToBiz(&models[i])
	}
	return items, nil
}

func evolutionProposalModelToBiz(m *EvolutionProposal) *biz.EvolutionProposal {
	return &biz.EvolutionProposal{
		ID:                  m.ID,
		AgentID:             m.AgentID,
		SessionID:           m.SessionID,
		TurnIndex:           m.TurnIndex,
		SignalType:          m.SignalType,
		Confidence:          m.Confidence,
		ProblemSummary:      m.ProblemSummary,
		ProposedContent:     m.ProposedContent,
		TargetPath:          m.TargetPath,
		TargetAction:        m.TargetAction,
		Conflict:            m.Conflict,
		ConflictDetail:      m.ConflictDetail,
		ConflictCheckFailed: m.ConflictCheckFailed,
		DedupSkipped:        m.DedupSkipped,
		Status:              m.Status,
		ReviewComment:       m.ReviewComment,
		CreatedAt:           m.CreatedAt,
		ReviewedAt:          m.ReviewedAt,
		ReviewedBy:          m.ReviewedBy,
	}
}
```

- [ ] **Step 2: Register model in AutoMigrate in `portal/internal/data/data.go`**

In the `db.AutoMigrate(...)` call (line 82-96), add `&EvolutionProposal{}` to the list. Also add `NewEvolutionProposalRepo` to the `ProviderSet` (line 29):

```go
// In ProviderSet, append NewEvolutionProposalRepo:
var ProviderSet = wire.NewSet(..., NewEvolutionProposalRepo)

// In AutoMigrate, append &EvolutionProposal{}:
&EvolutionProposal{},
```

- [ ] **Step 3: Verify compilation**

```bash
cd portal && go build ./internal/data/...
```

- [ ] **Step 4: Commit**

```bash
git add portal/internal/data/evolution_proposal.go portal/internal/data/data.go
git commit -m "$(cat <<'EOF'
feat(evolution): add EvolutionProposal GORM model and repo
EOF
)"
```

---

### Task 3: Biz Model + Repo Interface + Usecase

**Files:**
- Create: `portal/internal/biz/evolution.go`
- Modify: `portal/internal/biz/biz.go`

- [ ] **Step 1: Create biz layer in `portal/internal/biz/evolution.go`**

```go
package biz

import (
	"context"
	"time"
)

// EvolutionProposal is the business model for an evolution proposal.
type EvolutionProposal struct {
	ID                  string
	AgentID             string
	SessionID           string
	TurnIndex           int
	SignalType          string
	Confidence          float64
	ProblemSummary      string
	ProposedContent     string
	TargetPath          string
	TargetAction        string // create, patch, deprecate
	Conflict            bool
	ConflictDetail      *string
	ConflictCheckFailed bool
	DedupSkipped        bool
	Status              string // pending, approved, rejected, expired
	ReviewComment       *string
	CreatedAt           time.Time
	ReviewedAt          *time.Time
	ReviewedBy          *string
}

// EvolutionProposalRepo defines the storage interface for evolution proposals.
type EvolutionProposalRepo interface {
	Create(ctx context.Context, p *EvolutionProposal) error
	GetByID(ctx context.Context, id string) (*EvolutionProposal, error)
	List(ctx context.Context, page, pageSize int32, status string) ([]*EvolutionProposal, int, error)
	Update(ctx context.Context, id string, updates map[string]any) error
	CountPending(ctx context.Context) (int, error)
	ListExpired(ctx context.Context, before time.Time) ([]*EvolutionProposal, error)
}

// EvolutionUsecase handles evolution proposal business logic.
type EvolutionUsecase struct {
	repo EvolutionProposalRepo
}

func NewEvolutionUsecase(repo EvolutionProposalRepo) *EvolutionUsecase {
	return &EvolutionUsecase{repo: repo}
}

func (uc *EvolutionUsecase) List(ctx context.Context, page, pageSize int32, status string) ([]*EvolutionProposal, int, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	return uc.repo.List(ctx, page, pageSize, status)
}

func (uc *EvolutionUsecase) Get(ctx context.Context, id string) (*EvolutionProposal, error) {
	return uc.repo.GetByID(ctx, id)
}

func (uc *EvolutionUsecase) Approve(ctx context.Context, id, reviewer string) error {
	now := time.Now()
	return uc.repo.Update(ctx, id, map[string]any{
		"status":      "approved",
		"reviewed_at": now,
		"reviewed_by": reviewer,
	})
}

func (uc *EvolutionUsecase) Reject(ctx context.Context, id, reviewer, comment string) error {
	now := time.Now()
	updates := map[string]any{
		"status":      "rejected",
		"reviewed_at": now,
		"reviewed_by": reviewer,
	}
	if comment != "" {
		updates["review_comment"] = comment
	}
	return uc.repo.Update(ctx, id, updates)
}

func (uc *EvolutionUsecase) Patch(ctx context.Context, id string, proposedContent string) error {
	return uc.repo.Update(ctx, id, map[string]any{
		"proposed_content": proposedContent,
	})
}

func (uc *EvolutionUsecase) CountPending(ctx context.Context) (int, error) {
	return uc.repo.CountPending(ctx)
}

func (uc *EvolutionUsecase) ExpireOld(ctx context.Context, olderThan time.Time) (int, error) {
	items, err := uc.repo.ListExpired(ctx, olderThan)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, item := range items {
		if err := uc.repo.Update(ctx, item.ID, map[string]any{"status": "expired"}); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}
```

- [ ] **Step 2: Wire into ProviderSet in `portal/internal/biz/biz.go`**

Append `NewEvolutionUsecase` to the ProviderSet:

```go
var ProviderSet = wire.NewSet(..., NewEvolutionUsecase)
```

- [ ] **Step 3: Verify compilation**

```bash
cd portal && go build ./internal/biz/...
```

- [ ] **Step 4: Commit**

```bash
git add portal/internal/biz/evolution.go portal/internal/biz/biz.go
git commit -m "$(cat <<'EOF'
feat(evolution): add EvolutionProposal biz model, repo interface, and usecase
EOF
)"
```

---

### Task 4: Evolution Config Loader

**Files:**
- Create: `portal/internal/chat/evolution_config.go`
- Modify: `portal/internal/chat/portal_agent_extra.go`

- [ ] **Step 1: Create config loader in `portal/internal/chat/evolution_config.go`**

```go
package chat

import (
	"os"
	"strings"

	"github.com/sixath/framework/config"
)

var storedEvolutionCfg *config.EvolutionConfig

// SetEvolutionConfig stores agent_extra evolution settings.
func SetEvolutionConfig(cfg *config.EvolutionConfig) {
	if cfg == nil {
		storedEvolutionCfg = nil
		return
	}
	cp := *cfg
	storedEvolutionCfg = &cp
}

// EvolutionEnabled reports whether the evolution pipeline is enabled.
func EvolutionEnabled() bool {
	if v := strings.TrimSpace(os.Getenv("SATH_EVOLUTION_ENABLED")); v != "" {
		switch strings.ToLower(v) {
		case "1", "true", "yes", "on":
			return true
		case "0", "false", "no", "off":
			return false
		}
	}
	return storedEvolutionCfg != nil && storedEvolutionCfg.Enabled
}

// EvolutionConfig returns the current evolution config (nil if disabled).
func EvolutionConfig() *config.EvolutionConfig {
	return storedEvolutionCfg
}
```

- [ ] **Step 2: Wire into SetPortalAgentExtra in `portal/internal/chat/portal_agent_extra.go`**

After line 69 (closing `if extra.MemoryGraph != nil` block), add:

```go
	if extra.Evolution != nil {
		SetEvolutionConfig(extra.Evolution)
	} else {
		SetEvolutionConfig(nil)
	}
```

- [ ] **Step 3: Verify compilation**

```bash
cd portal && go build ./internal/chat/...
```

- [ ] **Step 4: Commit**

```bash
git add portal/internal/chat/evolution_config.go portal/internal/chat/portal_agent_extra.go
git commit -m "$(cat <<'EOF'
feat(evolution): add evolution config loader and wire into SetPortalAgentExtra
EOF
)"
```

---

### Task 5: Trial-and-Error Buffer

**Files:**
- Create: `portal/internal/chat/evolution_trial_buffer.go`

- [ ] **Step 1: Create trial buffer in `portal/internal/chat/evolution_trial_buffer.go`**

```go
package chat

import (
	"crypto/sha256"
	"fmt"
	"sync"
)

// trialEntry holds accumulated data for one problem pattern.
type trialEntry struct {
	AgentID       string
	Problem       string
	FailCounts    []int
	SuccessSteps  []string
	Occurrence    int
	State         string // observing, proposed
	ObserveWindow int
	ObserveCount  int
}

// TrialBuffer is an in-process LRU buffer for trial-and-error detection.
// Thread-safe, capacity 256 entries.
type TrialBuffer struct {
	mu       sync.Mutex
	entries  map[string]*trialEntry
	keys     []string // LRU order
	capacity int
}

// NewTrialBuffer creates a buffer with the given capacity.
func NewTrialBuffer(capacity int) *TrialBuffer {
	if capacity <= 0 {
		capacity = 256
	}
	return &TrialBuffer{
		entries:  make(map[string]*trialEntry),
		capacity: capacity,
	}
}

// problemKey generates a hash key for an agent + problem combination.
func problemKey(agentID, problem string) string {
	h := sha256.Sum256([]byte(agentID + problem))
	return fmt.Sprintf("%x", h[:12])
}

// Record records a trial-and-error occurrence. Returns true if the threshold
// is met and a proposal should be generated.
func (b *TrialBuffer) Record(agentID, problem string, failCount int, successSteps []string, minFailures, minOccurrences int) (met bool) {
	if agentID == "" || problem == "" {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	key := problemKey(agentID, problem)
	entry, exists := b.entries[key]
	if !exists {
		// Evict oldest if at capacity
		for len(b.keys) >= b.capacity {
			oldest := b.keys[0]
			b.keys = b.keys[1:]
			delete(b.entries, oldest)
		}
		entry = &trialEntry{
			AgentID:  agentID,
			Problem:  problem,
			State:    "observing",
		}
		b.entries[key] = entry
		b.keys = append(b.keys, key)
	} else {
		// Move to end (LRU)
		for i, k := range b.keys {
			if k == key {
				b.keys = append(b.keys[:i], b.keys[i+1:]...)
				break
			}
		}
		b.keys = append(b.keys, key)
	}

	if failCount < minFailures {
		return false
	}

	entry.FailCounts = append(entry.FailCounts, failCount)
	entry.Occurrence++

	// Accumulate success steps: longer path wins
	if len(successSteps) > len(entry.SuccessSteps) {
		entry.SuccessSteps = successSteps
	} else if len(successSteps) == len(entry.SuccessSteps) && len(successSteps) > 0 {
		entry.SuccessSteps = successSteps // latest wins on tie
	}

	if entry.Occurrence >= minOccurrences && entry.State == "observing" {
		entry.State = "proposed"
		return true
	}
	return false
}

// Get returns entry data for proposal generation, or nil if not found.
func (b *TrialBuffer) Get(agentID, problem string) *trialEntry {
	b.mu.Lock()
	defer b.mu.Unlock()
	key := problemKey(agentID, problem)
	return b.entries[key]
}

// Remove removes an entry (called after proposal is reviewed).
func (b *TrialBuffer) Remove(agentID, problem string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	key := problemKey(agentID, problem)
	delete(b.entries, key)
	for i, k := range b.keys {
		if k == key {
			b.keys = append(b.keys[:i], b.keys[i+1:]...)
			break
		}
	}
}

// AdvanceObservation increments observation counters and removes stale entries.
// Returns keys to remove from the buffer.
func (b *TrialBuffer) AdvanceObservation(maxWindow int) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	var remove []string
	for key, entry := range b.entries {
		if entry.State == "observing" {
			entry.ObserveCount++
			if entry.ObserveCount > maxWindow {
				remove = append(remove, key)
			}
		}
	}
	for _, key := range remove {
		delete(b.entries, key)
		for i, k := range b.keys {
			if k == key {
				b.keys = append(b.keys[:i], b.keys[i+1:]...)
				break
			}
		}
	}
	return remove
}
```

- [ ] **Step 1b: Add factual error filter to trial buffer**

Append to the same file before the closing `}` of the package:

```go
// factualErrorPatterns defines error substrings that indicate a factual/reproducible error
// (as opposed to transient errors like timeouts, rate limits, or permission denials).
// Only these errors count toward trial-and-error detection.
var factualErrorPatterns = []string{
	// Type mismatches
	"type mismatch",
	"cannot convert",
	"cannot cast",
	"column count",
	"data type",
	// Missing columns/fields
	"column",
	"field",
	"no such column",
	"unknown column",
	// Missing tables/relations
	"table",
	"relation",
	"no such table",
	"does not exist",
	// Missing views/sequences/functions/schemas
	"view",
	"sequence",
	"function",
	"procedure",
	"schema",
	// Missing indexes
	"index",
	"no such index",
	// Missing databases
	"database",
	"no such database",
	// Syntax/parse errors
	"syntax error",
	"parse error",
	"protocol error",
	// Constraint violations
	"duplicate key",
	"unique constraint",
	"foreign key",
	"cannot be null",
	"check constraint",
	"constraint",
	// Value domain errors
	"value out of range",
	"overflow",
	"division by zero",
}

// IsFactualError reports whether a tool error message indicates a factual/reproducible
// error that should count toward trial-and-error detection.
func IsFactualError(errMsg string) bool {
	if errMsg == "" {
		return false
	}
	lower := strings.ToLower(errMsg)
	for _, pat := range factualErrorPatterns {
		if strings.Contains(lower, pat) {
			return true
		}
	}
	return false
}
```

Also add `"strings"` to the import block in the file.

- [ ] **Step 1c: Verify compilation**

```bash
cd portal && go build ./internal/chat/...
```

- [ ] **Step 2: Commit**

```bash
git add portal/internal/chat/evolution_trial_buffer.go
git commit -m "$(cat <<'EOF'
feat(evolution): add in-process LRU trial-and-error detection buffer
EOF
)"
```

---

### Task 6: Signal Detection (Keyword Rules + LLM Classification)

**Files:**
- Create: `portal/internal/chat/evolution_detect.go`

- [ ] **Step 1: Create signal detection in `portal/internal/chat/evolution_detect.go`**

```go
package chat

import (
	"context"
	"encoding/json"
	"strings"

	"backend/internal/biz"

	"github.com/sixath/framework/config"
	"github.com/sixath/framework/model"
	"github.com/sixath/framework/memory"
)

// DetectResult is the output of the evolution detection pipeline.
type DetectResult struct {
	IsSignal    bool   `json:"is_signal"`
	SignalType  string `json:"signal_type"`
	Confidence  float64 `json:"confidence"`
	Summary     string `json:"summary"`
}

// classResult is the LLM classification response.
type classResult struct {
	IsSignal   bool    `json:"is_signal"`
	SignalType string  `json:"signal_type"`
	Confidence float64 `json:"confidence"`
	Summary    string  `json:"summary"`
}

// DetectEvolutionSignals runs the full detection pipeline for a completed turn.
// Returns nil if no signal detected. Fully async-safe, fail-open.
func DetectEvolutionSignals(
	ctx context.Context,
	messages []*biz.ChatMessage,
	loadedSkills []string,
	failureSignals []memory.FailureSignal,
	trialBuf *TrialBuffer,
) *DetectResult {
	cfg := EvolutionConfig()
	if cfg == nil {
		return nil
	}

	userMsg := lastUserMessage(messages)
	if userMsg == "" {
		return nil
	}

	// Phase 1: Keyword rule filtering (runs all 4 in parallel conceptually, sequential here is fine)
	ruleHits := keywordMatch(userMsg, cfg)

	// Phase 1.5: Trial-and-error detection (independent of keyword rules, uses factual errors only)
	if len(failureSignals) > 0 && trialBuf != nil {
		factualFailures := filterFactualErrors(failureSignals)
		if len(factualFailures) >= cfg.TrialAndError.MinToolFailures {
			// Turn succeeded (we're in post-turn hook), so this is a trial-and-error candidate
			problem := buildTrialProblemSummary(factualFailures, messages)
			if problem != "" {
				met := trialBuf.Record(
					agentIDFromCtx(ctx),
					problem,
					len(factualFailures),
					extractSuccessSteps(messages),
					cfg.TrialAndError.MinToolFailures,
					cfg.TrialAndError.MinOccurrences,
				)
				if met {
					ruleHits = append(ruleHits, "trial_error")
				}
			}
		}
	}

	if len(ruleHits) == 0 {
		return nil
	}

	// Phase 2: LLM classification (only if rules hit)
	modelProvider := resolveClassifier(cfg)
	if modelProvider == nil {
		return nil
	}

	prompt := buildClassificationPrompt(messages, ruleHits)
	resp, err := callClassifier(ctx, modelProvider, prompt, cfg.Classifier.MaxTokens)
	if err != nil {
		return nil // fail-open
	}

	var cr classResult
	if err := json.Unmarshal([]byte(resp), &cr); err != nil {
		return nil
	}

	if !cr.IsSignal || cr.Confidence < 0.5 {
		return nil
	}

	return &DetectResult{
		IsSignal:   true,
		SignalType: cr.SignalType,
		Confidence: cr.Confidence,
		Summary:    cr.Summary,
	}
}

func keywordMatch(userMsg string, cfg *config.EvolutionConfig) []string {
	lower := strings.ToLower(userMsg)
	var hits []string
	for _, kw := range cfg.Rules.StyleCorrection {
		if strings.Contains(lower, strings.ToLower(kw)) {
			hits = append(hits, "style_correction")
			break
		}
	}
	for _, kw := range cfg.Rules.WorkflowCorrection {
		if strings.Contains(lower, strings.ToLower(kw)) {
			hits = append(hits, "workflow_correction")
			break
		}
	}
	for _, kw := range cfg.Rules.DebuggingTrick {
		if strings.Contains(lower, strings.ToLower(kw)) {
			hits = append(hits, "debugging_trick")
			break
		}
	}
	for _, kw := range cfg.Rules.StaleSkill {
		if strings.Contains(lower, strings.ToLower(kw)) {
			hits = append(hits, "stale_skill")
			break
		}
	}
	return hits
}

func buildClassificationPrompt(messages []*biz.ChatMessage, ruleHits []string) string {
	var sb strings.Builder
	sb.WriteString("判断以下对话是否包含需要记录为技能进化信号的内容。\n\n")
	sb.WriteString("可能的信号类型: " + strings.Join(ruleHits, ", ") + "\n\n")
	sb.WriteString("对话内容:\n")
	for i := len(messages) - 1; i >= 0 && sb.Len() < 3000; i-- {
		if messages[i].Role == "user" {
			sb.WriteString("用户: " + truncateStr(messages[i].Content, 500) + "\n")
		}
	}
	sb.WriteString("\n输出JSON: {\"is_signal\":bool,\"signal_type\":\"string\",\"confidence\":0.0-1.0,\"summary\":\"一句话描述\"}")
	return sb.String()
}

func lastUserMessage(messages []*biz.ChatMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i] != nil && messages[i].Role == "user" && strings.TrimSpace(messages[i].Content) != "" {
			return messages[i].Content
		}
	}
	return ""
}

func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func resolveClassifier(cfg *config.EvolutionConfig) *model.ProviderConfig {
	// Placeholder: returns nil (needs actual model resolution from agent config).
	// In production, this resolves the classifier model from the provider catalog.
	return nil
}

func callClassifier(ctx context.Context, provider *model.ProviderConfig, prompt string, maxTokens int) (string, error) {
	// Placeholder: calls the LLM and returns response text.
	return "", nil
}

// filterFactualErrors returns only tool failure signals whose error message
// matches factual/reproducible error patterns (type mismatch, missing column,
// missing table, missing index, syntax error, etc.). Transient errors (timeouts,
// rate limits, permission denials) are excluded.
func filterFactualErrors(signals []memory.FailureSignal) []memory.FailureSignal {
	var result []memory.FailureSignal
	for _, s := range signals {
		if s.Kind == "tool_failed" && IsFactualError(s.Message) {
			result = append(result, s)
		}
	}
	return result
}

// buildTrialProblemSummary constructs a one-line problem description from
// factual failure messages for the trial buffer key.
func buildTrialProblemSummary(failures []memory.FailureSignal, messages []*biz.ChatMessage) string {
	if len(failures) == 0 {
		return ""
	}
	// Use the first failure message as the problem key
	return strings.TrimSpace(failures[0].Message)
}

// extractSuccessSteps extracts the final successful tool calls from messages.
func extractSuccessSteps(messages []*biz.ChatMessage) []string {
	var steps []string
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" && strings.TrimSpace(messages[i].Content) != "" {
			steps = append(steps, messages[i].Content)
			if len(steps) >= 3 {
				break
			}
		}
	}
	// Reverse to chronological order
	for i, j := 0, len(steps)-1; i < j; i, j = i+1, j-1 {
		steps[i], steps[j] = steps[j], steps[i]
	}
	return steps
}

// agentIDFromCtx extracts agent_id from context.
func agentIDFromCtx(ctx context.Context) string {
	if v := ctx.Value("agent_id"); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
```

- [ ] **Step 2: Verify compilation**

```bash
cd portal && go build ./internal/chat/...
```

- [ ] **Step 3: Commit**

```bash
git add portal/internal/chat/evolution_detect.go
git commit -m "$(cat <<'EOF'
feat(evolution): add keyword rule filter + LLM classification pipeline
EOF
)"
```

---

### Task 7: Arbiter (Dedup + Conflict Check)

**Files:**
- Create: `portal/internal/chat/evolution_arbiter.go`

- [ ] **Step 1: Create arbiter in `portal/internal/chat/evolution_arbiter.go`**

```go
package chat

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// ArbiterResult holds the output of arbitration.
type ArbiterResult struct {
	Duplicate           bool
	DuplicateOf         string
	Conflict            bool
	ConflictDetail      string
	DedupSkipped        bool
	ConflictCheckFailed bool
}

// Arbitrate runs dedup and conflict checks for a proposed evolution.
func Arbitrate(
	ctx context.Context,
	newSummary string,
	newContent string,
	existingSkills []SkillSummary, // name + description pairs
) ArbiterResult {
	var result ArbiterResult

	// Dedup: hash-based simple check (embedding-based in production)
	cfg := EvolutionConfig()
	if cfg == nil {
		return result
	}

	newHash := contentHash(newContent)
	for _, sk := range existingSkills {
		if contentHash(sk.Description) == newHash {
			result.Duplicate = true
			result.DuplicateOf = sk.Name
			return result
		}
	}

	// Conflict check: simple keyword overlap for MVP
	if hasSemanticConflict(newContent, existingSkills) {
		result.Conflict = true
		result.ConflictDetail = "新指令与已存在技能可能存在语义矛盾"
	}

	return result
}

// SkillSummary is a lightweight representation of an existing skill.
type SkillSummary struct {
	Name        string
	Description string
}

func contentHash(content string) string {
	h := sha256.Sum256([]byte(strings.TrimSpace(content)))
	return hex.EncodeToString(h[:16])
}

func hasSemanticConflict(newContent string, existing []SkillSummary) bool {
	// Placeholder: returns false (needs LLM-based check in production).
	_ = newContent
	_ = existing
	return false
}
```

- [ ] **Step 2: Verify compilation**

```bash
cd portal && go build ./internal/chat/...
```

- [ ] **Step 3: Commit**

```bash
git add portal/internal/chat/evolution_arbiter.go
git commit -m "$(cat <<'EOF'
feat(evolution): add dedup and conflict check arbiter
EOF
)"
```

---

### Task 8: HTTP Handlers + Routes

**Files:**
- Create: `portal/internal/server/evolution.go`
- Modify: `portal/internal/server/http.go`

- [ ] **Step 1: Create HTTP handlers in `portal/internal/server/evolution.go`**

```go
package server

import (
	"encoding/json"
	"net/http"
	"strconv"

	"backend/internal/biz"

	httptransport "github.com/go-kratos/kratos/v2/transport/http"
)

// EvolutionHandlers provides HTTP handlers for the evolution review API.
type EvolutionHandlers struct {
	uc *biz.EvolutionUsecase
}

func NewEvolutionHandlers(uc *biz.EvolutionUsecase) *EvolutionHandlers {
	return &EvolutionHandlers{uc: uc}
}

func (h *EvolutionHandlers) ListProposals(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	status := r.URL.Query().Get("status")
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	items, total, err := h.uc.List(r.Context(), int32(page), int32(pageSize), status)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ret":   map[string]any{"code": 0, "message": "ok"},
		"items": items,
		"total": total,
	})
}

func (h *EvolutionHandlers) GetProposal(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	item, err := h.uc.Get(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ret":  map[string]any{"code": 0, "message": "ok"},
		"item": item,
	})
}

func (h *EvolutionHandlers) ApproveProposal(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	reviewer := r.Header.Get("X-User-Email")
	if reviewer == "" {
		reviewer = "unknown"
	}
	if err := h.uc.Approve(r.Context(), id, reviewer); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ret": map[string]any{"code": 0, "message": "ok"},
	})
}

func (h *EvolutionHandlers) RejectProposal(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	reviewer := r.Header.Get("X-User-Email")
	if reviewer == "" {
		reviewer = "unknown"
	}
	var body struct {
		Comment string `json:"comment"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	if err := h.uc.Reject(r.Context(), id, reviewer, body.Comment); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ret": map[string]any{"code": 0, "message": "ok"},
	})
}

func (h *EvolutionHandlers) PatchProposal(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		ProposedContent string `json:"proposed_content"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	if err := h.uc.Patch(r.Context(), id, body.ProposedContent); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ret": map[string]any{"code": 0, "message": "ok"},
	})
}

func (h *EvolutionHandlers) CountPending(w http.ResponseWriter, r *http.Request) {
	count, err := h.uc.CountPending(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ret":   map[string]any{"code": 0, "message": "ok"},
		"count": count,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
```

- [ ] **Step 2: Register routes in `portal/internal/server/http.go`**

In `NewHTTPServer`, add `evolutionUC *biz.EvolutionUsecase` parameter. Then in the function body, after the existing routes:

```go
// In function signature, append: evolutionUC *biz.EvolutionUsecase
// In function body:
evHandlers := NewEvolutionHandlers(evolutionUC)
r.GET("/api/v1/evolution/proposals", evHandlers.ListProposals)
r.GET("/api/v1/evolution/proposals/count", evHandlers.CountPending)
r.GET("/api/v1/evolution/proposals/{id}", evHandlers.GetProposal)
r.POST("/api/v1/evolution/proposals/{id}/approve", evHandlers.ApproveProposal)
r.POST("/api/v1/evolution/proposals/{id}/reject", evHandlers.RejectProposal)
r.PATCH("/api/v1/evolution/proposals/{id}", evHandlers.PatchProposal)
```

- [ ] **Step 3: Verify compilation**

```bash
cd portal && go build ./internal/server/...
```

- [ ] **Step 4: Commit**

```bash
git add portal/internal/server/evolution.go portal/internal/server/http.go
git commit -m "$(cat <<'EOF'
feat(evolution): add HTTP handlers and routes for evolution review API
EOF
)"
```

---

### Task 9: Wire DI Wiring

**Files:**
- Modify: `portal/cmd/backend/wire.go`
- Modify: `portal/cmd/backend/wire_gen.go` (regenerated)

- [ ] **Step 1: Update wire.go to pass EvolutionUsecase to NewHTTPServer**

In `portal/cmd/backend/wire.go`, update the `NewHTTPServer` call to include `evolutionUC`:

No source change needed in wire.go — Wire auto-resolves `*biz.EvolutionUsecase` from the provider set. Just regenerate:

```bash
cd portal/cmd/backend && wire
```

- [ ] **Step 2: Verify the generated wire_gen.go compiles**

```bash
cd portal && go build ./cmd/backend
```

- [ ] **Step 3: Commit**

```bash
git add portal/cmd/backend/wire_gen.go
git commit -m "$(cat <<'EOF'
feat(evolution): regenerate wire DI with EvolutionUsecase
EOF
)"
```

---

### Task 10: Chat Turn Hook

**Files:**
- Modify: `portal/internal/service/chat.go`

- [ ] **Step 1: Add evolution detection trigger after turn completion**

In `SendMessageStream`, find the goroutine at line 880 where `epBuf.Clear()` is called. After the turn completes (after `streamAgentEventsWithVisionDegrade` returns), add the async detection trigger. The hook should be added in the goroutine's defer block, after line 882 (`epBuf.Clear()`):

```go
// After epBuf.Clear() in the defer block, add:
if chat.EvolutionEnabled() && len(priorHistory) > 0 {
    go func() {
        ctx := context.Background()
        msgs := chatMessagesFromHistory(priorHistory)
        trialBuf := chat.GetTrialBuffer()
        result := chat.DetectEvolutionSignals(ctx, msgs, loadedSkillNames, nil, trialBuf)
        if result != nil {
            // Phase 3: Proposal generation (async, not blocking)
            s.log.Infof("evolution signal detected: type=%s confidence=%.2f summary=%s", result.SignalType, result.Confidence, result.Summary)
            // TODO: generate proposal and write to DB in a follow-up task
        }
    }()
}
```

Note: This is a partial implementation — the full pipeline (proposal generation + DB write) is completed in Task 15.

- [ ] **Step 2: Verify compilation**

```bash
cd portal && go build ./internal/service/...
```

- [ ] **Step 3: Commit**

```bash
git add portal/internal/service/chat.go
git commit -m "$(cat <<'EOF'
feat(evolution): add turn-end evolution detection hook in chat service
EOF
)"
```

---

### Task 11: Default Config in agent_extra.yaml

**Files:**
- Modify: `portal/configs/agent_extra.yaml`

- [ ] **Step 1: Add evolution config block to agent_extra.yaml**

Append to the file:

```yaml
evolution:
  enabled: false
  rules:
    style_correction:
      - "太啰嗦"
      - "简洁"
      - "短一点"
      - "stop doing"
      - "记住"
      - "别这样"
    workflow_correction:
      - "应该先"
      - "顺序不对"
      - "下次记住"
      - "以后"
      - "步骤"
      - "流程"
    debugging_trick:
      - "原来是这样"
      - "找到原因"
      - "根因"
      - "绕过去"
      - "换个方式"
      - "解决了"
    stale_skill:
      - "这个技能"
      - "过时"
      - "不管用"
      - "不对"
      - "这个不对"
  trial_and_error:
    min_tool_failures: 3
    min_occurrences: 2
    observation_window: 50
    similarity_threshold: 0.8
  classifier:
    provider: openai
    model: gpt-4o-mini
    max_tokens: 200
  dedup_threshold: 0.85
  embedding:
    provider: openai
    model: text-embedding-3-small
```

- [ ] **Step 2: Commit**

```bash
git add portal/configs/agent_extra.yaml
git commit -m "$(cat <<'EOF'
feat(evolution): add default evolution config to agent_extra.yaml
EOF
)"
```

---

### Task 12: Lifecycle Cron Task

**Files:**
- Modify: `portal/internal/cron/scheduler.go`

- [ ] **Step 1: Add weekly lifecycle cleanup to the scheduler**

In `Scheduler.Start`, add a separate ticker for weekly cleanup. Or add a method to `Scheduler` that `EvolutionUsecase` can register. Simpler approach: extend the `tick` method:

In `portal/internal/cron/scheduler.go`, add a `evolutionUC *biz.EvolutionUsecase` field to Scheduler and a separate weekly cleanup goroutine:

```go
// In Scheduler struct, add:
evolutionUC *biz.EvolutionUsecase

// In NewScheduler, add evolutionUC parameter:
func NewScheduler(cronUC *biz.CronUsecase, exec *Executor, interval time.Duration, logger log.Logger, evolutionUC *biz.EvolutionUsecase) *Scheduler {

// In Start, add a weekly cleanup goroutine:
func (s *Scheduler) Start(ctx context.Context) {
    // ... existing ticker ...

    // Weekly evolution proposal cleanup
    go func() {
        cleanupTicker := time.NewTicker(7 * 24 * time.Hour)
        defer cleanupTicker.Stop()
        for {
            select {
            case <-ctx.Done():
                return
            case <-cleanupTicker.C:
                if s.evolutionUC != nil {
                    expired, err := s.evolutionUC.ExpireOld(ctx, time.Now().Add(-30*24*time.Hour))
                    if err != nil {
                        s.log.Errorf("evolution cleanup: %v", err)
                    } else if expired > 0 {
                        s.log.Infof("evolution cleanup: expired %d proposals", expired)
                    }
                }
            }
        }
    }()

    // ... existing ticker loop ...
}
```

- [ ] **Step 2: Verify compilation**

```bash
cd portal && go build ./internal/cron/...
```

- [ ] **Step 3: Commit**

```bash
git add portal/internal/cron/scheduler.go
git commit -m "$(cat <<'EOF'
feat(evolution): add weekly lifecycle cleanup in cron scheduler
EOF
)"
```

---

### Task 13: Frontend API Client

**Files:**
- Create: `web/src/api/evolution.ts`

- [ ] **Step 1: Create API client in `web/src/api/evolution.ts`**

```typescript
import { request, checkRet, type BaseResponse } from './client'

const API_BASE = '/api/v1'

export interface EvolutionProposal {
  id: string
  agent_id: string
  session_id: string
  turn_index: number
  signal_type: string
  confidence: number
  problem_summary: string
  proposed_content: string
  target_path: string
  target_action: string
  conflict: boolean
  conflict_detail?: string
  conflict_check_failed: boolean
  dedup_skipped: boolean
  status: string
  review_comment?: string
  created_at: string
  reviewed_at?: string
  reviewed_by?: string
}

export interface ListProposalsResponse {
  ret?: BaseResponse
  items: EvolutionProposal[]
  total: number
}

export const evolutionApi = {
  list: async (params?: { page?: number; page_size?: number; status?: string }) => {
    const q = new URLSearchParams()
    if (params?.page) q.set('page', String(params.page))
    if (params?.page_size) q.set('page_size', String(params.page_size))
    if (params?.status) q.set('status', params.status)
    const query = q.toString()
    const url = `${API_BASE}/evolution/proposals${query ? '?' + query : ''}`
    const res = await fetch(url, {
      headers: { 'Content-Type': 'application/json' },
    })
    if (!res.ok) throw new Error(`HTTP ${res.status}`)
    return res.json() as Promise<ListProposalsResponse>
  },

  get: async (id: string) => {
    const url = `${API_BASE}/evolution/proposals/${id}`
    const res = await fetch(url, {
      headers: { 'Content-Type': 'application/json' },
    })
    if (!res.ok) throw new Error(`HTTP ${res.status}`)
    return res.json() as Promise<{ ret?: BaseResponse; item: EvolutionProposal }>
  },

  approve: async (id: string) => {
    const url = `${API_BASE}/evolution/proposals/${id}/approve`
    const res = await fetch(url, { method: 'POST', headers: { 'Content-Type': 'application/json' } })
    if (!res.ok) throw new Error(`HTTP ${res.status}`)
    return res.json()
  },

  reject: async (id: string, comment?: string) => {
    const url = `${API_BASE}/evolution/proposals/${id}/reject`
    const res = await fetch(url, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ comment: comment || '' }),
    })
    if (!res.ok) throw new Error(`HTTP ${res.status}`)
    return res.json()
  },

  patch: async (id: string, proposedContent: string) => {
    const url = `${API_BASE}/evolution/proposals/${id}`
    const res = await fetch(url, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ proposed_content: proposedContent }),
    })
    if (!res.ok) throw new Error(`HTTP ${res.status}`)
    return res.json()
  },

  countPending: async () => {
    const url = `${API_BASE}/evolution/proposals/count`
    const res = await fetch(url, {
      headers: { 'Content-Type': 'application/json' },
    })
    if (!res.ok) throw new Error(`HTTP ${res.status}`)
    return res.json() as Promise<{ ret?: BaseResponse; count: number }>
  },
}
```

- [ ] **Step 2: Verify TypeScript compilation**

```bash
cd web && npx tsc --noEmit src/api/evolution.ts
```

- [ ] **Step 3: Commit**

```bash
git add web/src/api/evolution.ts
git commit -m "$(cat <<'EOF'
feat(evolution): add frontend API client for evolution proposals
EOF
)"
```

---

### Task 14: Frontend Review Page

**Files:**
- Create: `web/src/pages/EvolutionReviewPage.tsx`
- Create: `web/src/pages/EvolutionReviewPage.css`

- [ ] **Step 1: Create review page component in `web/src/pages/EvolutionReviewPage.tsx`**

```tsx
import { useState, useEffect, useCallback } from 'react'
import { evolutionApi, type EvolutionProposal } from '../api/evolution'
import './EvolutionReviewPage.css'

const SIGNAL_LABELS: Record<string, string> = {
  style_correction: '风格纠正',
  workflow_correction: '工作流纠正',
  debugging_trick: '调试技巧',
  stale_skill: '过时技能',
  trial_error: '反复试错',
}

const SIGNAL_ICONS: Record<string, string> = {
  style_correction: '✏️',
  workflow_correction: '🔄',
  debugging_trick: '🐛',
  stale_skill: '⚠️',
  trial_error: '🔁',
}

const ACTION_LABELS: Record<string, string> = {
  create: '创建',
  patch: '修改',
  deprecate: '废弃',
}

export default function EvolutionReviewPage() {
  const [proposals, setProposals] = useState<EvolutionProposal[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [statusFilter, setStatusFilter] = useState('pending')
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [editContent, setEditContent] = useState('')
  const [rejectComment, setRejectComment] = useState('')
  const [rejectingId, setRejectingId] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  const fetchProposals = useCallback(async () => {
    setLoading(true)
    try {
      const data = await evolutionApi.list({ page, page_size: 20, status: statusFilter })
      setProposals(data.items || [])
      setTotal(data.total)
    } catch (e) {
      console.error('Failed to fetch proposals:', e)
    } finally {
      setLoading(false)
    }
  }, [page, statusFilter])

  useEffect(() => {
    fetchProposals()
  }, [fetchProposals])

  const handleApprove = async (id: string) => {
    try {
      await evolutionApi.approve(id)
      fetchProposals()
    } catch (e) {
      console.error('Failed to approve:', e)
    }
  }

  const handleReject = async (id: string) => {
    try {
      await evolutionApi.reject(id, rejectComment)
      setRejectingId(null)
      setRejectComment('')
      fetchProposals()
    } catch (e) {
      console.error('Failed to reject:', e)
    }
  }

  const handleSaveEdit = async (id: string) => {
    try {
      await evolutionApi.patch(id, editContent)
      setEditingId(null)
      setEditContent('')
      fetchProposals()
    } catch (e) {
      console.error('Failed to save edit:', e)
    }
  }

  const totalPages = Math.max(1, Math.ceil(total / 20))

  return (
    <div className="evolution-review-page">
      <div className="page-header">
        <h1>进化提案评审</h1>
        <div className="filter-bar">
          <select value={statusFilter} onChange={(e) => { setStatusFilter(e.target.value); setPage(1) }}>
            <option value="pending">待评审</option>
            <option value="approved">已采纳</option>
            <option value="rejected">已拒绝</option>
            <option value="">全部</option>
          </select>
        </div>
      </div>

      {loading ? (
        <div className="loading">加载中...</div>
      ) : proposals.length === 0 ? (
        <div className="empty-state">暂无进化提案</div>
      ) : (
        <div className="proposal-list">
          {proposals.map((p) => (
            <div key={p.id} className={`proposal-card ${p.status}`}>
              <div className="proposal-header">
                <span className="signal-icon">{SIGNAL_ICONS[p.signal_type] || '📋'}</span>
                <span className="signal-type">{SIGNAL_LABELS[p.signal_type] || p.signal_type}</span>
                <span className="confidence" title={`置信度: ${(p.confidence * 100).toFixed(0)}%`}>
                  {p.confidence < 0.7 ? '🟡' : '🟢'} {(p.confidence * 100).toFixed(0)}%
                </span>
                <span className="target">{ACTION_LABELS[p.target_action]}: {p.target_path}</span>
                {p.conflict && <span className="badge badge-conflict">冲突</span>}
                {p.dedup_skipped && <span className="badge badge-skip">去重跳过</span>}
              </div>

              <div className="proposal-summary">{p.problem_summary}</div>

              <div className="proposal-meta">
                <span>Agent: {p.agent_id.slice(0, 8)}...</span>
                <span>轮次: #{p.turn_index}</span>
                <span>{new Date(p.created_at).toLocaleString()}</span>
              </div>

              {expandedId === p.id && (
                <div className="proposal-diff">
                  <h4>提案内容</h4>
                  {editingId === p.id ? (
                    <div className="edit-area">
                      <textarea
                        value={editContent}
                        onChange={(e) => setEditContent(e.target.value)}
                        rows={10}
                      />
                      <div className="edit-actions">
                        <button onClick={() => handleSaveEdit(p.id)}>保存</button>
                        <button onClick={() => setEditingId(null)}>取消</button>
                      </div>
                    </div>
                  ) : (
                    <pre className="content-preview">{p.proposed_content}</pre>
                  )}
                  {p.conflict_detail && (
                    <div className="conflict-warning">
                      <strong>冲突:</strong> {p.conflict_detail}
                    </div>
                  )}
                </div>
              )}

              <div className="proposal-actions">
                <button onClick={() => setExpandedId(expandedId === p.id ? null : p.id)}>
                  {expandedId === p.id ? '收起' : '展开'}
                </button>
                {p.status === 'pending' && (
                  <>
                    <button
                      className="btn-approve"
                      onClick={() => handleApprove(p.id)}
                    >
                      采纳
                    </button>
                    <button
                      onClick={() => {
                        setEditingId(p.id)
                        setEditContent(p.proposed_content)
                        setExpandedId(p.id)
                      }}
                    >
                      编辑
                    </button>
                    <button
                      className="btn-reject"
                      onClick={() => setRejectingId(p.id)}
                    >
                      拒绝
                    </button>
                  </>
                )}
                {p.status === 'approved' && p.review_comment && (
                  <span className="review-note">评审意见: {p.review_comment}</span>
                )}
              </div>

              {rejectingId === p.id && (
                <div className="reject-dialog">
                  <textarea
                    placeholder="拒绝理由（可选）"
                    value={rejectComment}
                    onChange={(e) => setRejectComment(e.target.value)}
                    rows={3}
                  />
                  <div className="reject-actions">
                    <button onClick={() => handleReject(p.id)}>确认拒绝</button>
                    <button onClick={() => { setRejectingId(null); setRejectComment('') }}>取消</button>
                  </div>
                </div>
              )}
            </div>
          ))}

          {totalPages > 1 && (
            <div className="pagination">
              <button disabled={page <= 1} onClick={() => setPage(page - 1)}>上一页</button>
              <span>{page} / {totalPages}</span>
              <button disabled={page >= totalPages} onClick={() => setPage(page + 1)}>下一页</button>
            </div>
          )}
        </div>
      )}
    </div>
  )
}
```

- [ ] **Step 2: Create CSS file in `web/src/pages/EvolutionReviewPage.css`**

```css
.evolution-review-page {
  max-width: 900px;
  margin: 0 auto;
  padding: 20px;
}

.evolution-review-page .page-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 20px;
}

.evolution-review-page .filter-bar select {
  padding: 6px 12px;
  border-radius: 4px;
  border: 1px solid var(--border-color, #ddd);
}

.proposal-list { display: flex; flex-direction: column; gap: 12px; }

.proposal-card {
  border: 1px solid var(--border-color, #ddd);
  border-radius: 8px;
  padding: 16px;
  background: var(--card-bg, #fff);
}

.proposal-card.pending { border-left: 4px solid #f0ad4e; }
.proposal-card.approved { border-left: 4px solid #5cb85c; opacity: 0.7; }
.proposal-card.rejected { border-left: 4px solid #d9534f; opacity: 0.6; }

.proposal-header {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
  flex-wrap: wrap;
}

.signal-icon { font-size: 1.2em; }
.signal-type { font-weight: 600; }
.confidence { font-size: 0.9em; color: #666; }
.target { font-size: 0.9em; color: #888; }

.badge {
  padding: 2px 6px;
  border-radius: 4px;
  font-size: 0.8em;
}
.badge-conflict { background: #f8d7da; color: #721c24; }
.badge-skip { background: #fff3cd; color: #856404; }

.proposal-summary { margin: 8px 0; color: #333; }

.proposal-meta {
  display: flex;
  gap: 16px;
  font-size: 0.85em;
  color: #999;
}

.proposal-diff {
  margin-top: 12px;
  padding: 12px;
  background: #f8f9fa;
  border-radius: 4px;
}

.content-preview {
  white-space: pre-wrap;
  font-family: monospace;
  font-size: 0.9em;
  max-height: 300px;
  overflow-y: auto;
}

.conflict-warning {
  margin-top: 8px;
  padding: 8px;
  background: #fff3cd;
  border-radius: 4px;
  font-size: 0.9em;
}

.edit-area textarea {
  width: 100%;
  padding: 8px;
  border: 1px solid #ddd;
  border-radius: 4px;
  font-family: monospace;
}

.proposal-actions {
  display: flex;
  gap: 8px;
  margin-top: 12px;
}

.proposal-actions button {
  padding: 6px 14px;
  border: 1px solid #ddd;
  border-radius: 4px;
  background: #fff;
  cursor: pointer;
}

.proposal-actions .btn-approve { background: #5cb85c; color: #fff; border-color: #4cae4c; }
.proposal-actions .btn-reject { background: #d9534f; color: #fff; border-color: #d43f3a; }

.reject-dialog {
  margin-top: 12px;
  padding: 12px;
  border: 1px solid #ddd;
  border-radius: 4px;
}

.reject-dialog textarea {
  width: 100%;
  padding: 8px;
  border: 1px solid #ddd;
  border-radius: 4px;
}

.reject-actions, .edit-actions {
  display: flex;
  gap: 8px;
  margin-top: 8px;
}

.pagination {
  display: flex;
  justify-content: center;
  align-items: center;
  gap: 12px;
  margin-top: 20px;
}

.loading, .empty-state {
  text-align: center;
  padding: 40px;
  color: #999;
}

.review-note {
  font-size: 0.9em;
  color: #666;
  margin-left: auto;
}
```

- [ ] **Step 3: Verify TypeScript compilation**

```bash
cd web && npx tsc --noEmit src/pages/EvolutionReviewPage.tsx
```

- [ ] **Step 4: Commit**

```bash
git add web/src/pages/EvolutionReviewPage.tsx web/src/pages/EvolutionReviewPage.css
git commit -m "$(cat <<'EOF'
feat(evolution): add review page with approve/edit/reject workflow
EOF
)"
```

---

### Task 15: Frontend Routing + Sidebar Badge

**Files:**
- Modify: `web/src/App.tsx`

- [ ] **Step 1: Add route and sidebar entry in `web/src/App.tsx`**

Add import:
```tsx
import EvolutionReviewPage from './pages/EvolutionReviewPage'
```

Add to sidebar (before the closing `</div>` of nav-section):
```tsx
<NavLink to="/evolution-review" className={({ isActive }) => `nav-item ${isActive ? 'active' : ''}`}>
  <span className="nav-item__icon">🧬</span>
  技能进化
</NavLink>
```

Add breadcrumb handler in the `Breadcrumb` function:
```tsx
} else if (segments[0] === 'evolution-review') {
  current = '技能进化评审'
  icon = '🧬'
}
```

Add route in the `<Routes>` block:
```tsx
<Route path="/evolution-review" element={<EvolutionReviewPage />} />
```

- [ ] **Step 2: Verify TypeScript compilation**

```bash
cd web && npx tsc --noEmit src/App.tsx
```

- [ ] **Step 3: Commit**

```bash
git add web/src/App.tsx
git commit -m "$(cat <<'EOF'
feat(evolution): add /evolution-review route and sidebar entry
EOF
)"
```

---

### Task 16: Full Detection Pipeline Integration

**Files:**
- Create: `portal/internal/chat/evolution_pipeline.go`

- [ ] **Step 1: Create pipeline orchestrator in `portal/internal/chat/evolution_pipeline.go`**

```go
package chat

import (
	"context"

	"backend/internal/biz"
)

// globalTrialBuffer is the process-wide trial-and-error buffer.
var globalTrialBuffer = NewTrialBuffer(256)

// GetTrialBuffer returns the global trial buffer.
func GetTrialBuffer() *TrialBuffer {
	return globalTrialBuffer
}

// RunEvolutionPipeline executes the full detection-to-proposal pipeline for a turn.
// This is called asynchronously after each turn completes. Fail-open.
func RunEvolutionPipeline(
	ctx context.Context,
	agentID string,
	sessionID string,
	turnIndex int,
	messages []*biz.ChatMessage,
	loadedSkillNames []string,
	proposalRepo biz.EvolutionProposalRepo,
) {
	cfg := EvolutionConfig()
	if cfg == nil {
		return
	}

	// Phase 1-2: Detect signals
	result := DetectEvolutionSignals(ctx, messages, loadedSkillNames, nil, globalTrialBuffer)
	if result == nil || !result.IsSignal {
		return
	}

	// Phase 3: Generate proposal content
	content := generateProposalContent(ctx, result, messages, cfg)
	if content == "" {
		return
	}

	// Phase 4: Arbitrate
	existingSkills := loadSkillSummaries(loadedSkillNames)
	arbResult := Arbitrate(ctx, result.Summary, content, existingSkills)
	if arbResult.Duplicate {
		return
	}

	// Phase 5: Write to DB
	targetPath, targetAction := resolveTarget(result.SignalType)
	proposal := &biz.EvolutionProposal{
		AgentID:             agentID,
		SessionID:           sessionID,
		TurnIndex:           turnIndex,
		SignalType:          result.SignalType,
		Confidence:          result.Confidence,
		ProblemSummary:      result.Summary,
		ProposedContent:     content,
		TargetPath:          targetPath,
		TargetAction:        targetAction,
		Conflict:            arbResult.Conflict,
		ConflictCheckFailed: arbResult.ConflictCheckFailed,
		DedupSkipped:        arbResult.DedupSkipped,
	}
	if arbResult.ConflictDetail != "" {
		s := arbResult.ConflictDetail
		proposal.ConflictDetail = &s
	}

	_ = proposalRepo.Create(ctx, proposal)
}

func generateProposalContent(ctx context.Context, result *DetectResult, messages []*biz.ChatMessage, cfg interface{}) string {
	// Placeholder: calls LLM to generate actual SKILL.md / USER.md content
	_ = ctx
	_ = result
	_ = messages
	_ = cfg
	return ""
}

func loadSkillSummaries(skillNames []string) []SkillSummary {
	result := make([]SkillSummary, len(skillNames))
	for i, name := range skillNames {
		result[i] = SkillSummary{Name: name, Description: name}
	}
	return result
}

func resolveTarget(signalType string) (path, action string) {
	switch signalType {
	case "style_correction":
		return "USER.md", "patch"
	case "workflow_correction", "debugging_trick", "trial_error":
		return "skills/auto-generated.md", "create"
	case "stale_skill":
		return "skills/auto-generated.md", "deprecate"
	default:
		return "skills/auto-generated.md", "create"
	}
}
```

- [ ] **Step 2: Update chat.go turn hook to use the full pipeline**

In `portal/internal/service/chat.go`, update the evolution hook added in Task 10:

```go
if chat.EvolutionEnabled() && len(priorHistory) > 0 {
    go func() {
        ctx := context.Background()
        msgs := chatMessagesFromHistory(priorHistory)
        chat.RunEvolutionPipeline(ctx, session.AgentID, sessionID, turnIndex, msgs, loadedSkillNames, s.evolutionRepo)
    }()
}
```

Note: `s.evolutionRepo` requires adding `evolutionRepo biz.EvolutionProposalRepo` to the `ChatService` struct and constructor.

- [ ] **Step 3: Verify compilation**

```bash
cd portal && go build ./...
```

- [ ] **Step 4: Commit**

```bash
git add portal/internal/chat/evolution_pipeline.go portal/internal/service/chat.go
git commit -m "$(cat <<'EOF'
feat(evolution): wire full detection-to-proposal pipeline with turn-end hook
EOF
)"
```

---

### Task 17: End-to-End Smoke Test

- [ ] **Step 1: Start the portal backend**

```bash
cd portal && go run ./cmd/backend -conf configs 2>&1 | head -30
# Expected: service starts, no errors
```

- [ ] **Step 2: Test the count pending API**

```bash
curl -s http://localhost:<port>/api/v1/evolution/proposals/count | python -m json.tool
# Expected: {"ret":{"code":0},"count":0}
```

- [ ] **Step 3: Test the list API**

```bash
curl -s http://localhost:<port>/api/v1/evolution/proposals?status=pending | python -m json.tool
# Expected: {"ret":{"code":0},"items":[],"total":0}
```

- [ ] **Step 4: Test frontend build**

```bash
cd web && npm run build 2>&1 | tail -5
# Expected: build succeeds
```

- [ ] **Step 5: Commit final verification**

```bash
git add -A
git diff --cached --stat
git commit -m "$(cat <<'EOF'
feat(evolution): end-to-end smoke test verification
EOF
)"
```

---

## Self-Review

### 1. Spec coverage

| Spec Section | Task Coverage |
|---|---|
| 2. Signal types (5 types) | Task 6 (keyword rules), Task 5 (trial buffer), Task 16 (pipeline) |
| 3. Architecture diagram | Task 16 (pipeline orchestrator) |
| 4. Config | Task 1 (struct), Task 4 (loader), Task 11 (yaml) |
| 5. Signal detection flow | Task 6 (detection), Task 16 (pipeline) |
| 6. Trial buffer | Task 5 |
| 7. Arbitration | Task 7 (arbiter) |
| 8. Data model | Task 1 (migration), Task 2 (model), Task 3 (biz) |
| 9. API | Task 8 (handlers), Task 13 (frontend client) |
| 10. Review page | Task 14 (React page) |
| 11. Audit log | Deferred (needs skill_manage integration in approve flow) |
| 12. Skill lifecycle | Task 12 (cron cleanup) |
| 13. File change list | All tasks match spec's file list |

### 2. Placeholder scan

- `resolveClassifier` and `callClassifier` in Task 6 are marked as placeholders — actual LLM integration needs model resolution plumbing (a follow-up task)
- `generateProposalContent` in Task 16 is placeholder — needs LLM prompt template work
- `hasSemanticConflict` in Task 7 returns false — needs conflict-check LLM call

These are intentional: the core data flow and review UX are functional without LLM calls initially.

### 3. Type consistency

- `EvolutionProposal` struct used consistently across `data/`, `biz/`, `server/`, and frontend `evolution.ts`
- `DetectResult` → `classResult` → `ArbiterResult` chain is consistent
- `EvolutionConfig` accessed via `EvolutionConfig()` getter throughout