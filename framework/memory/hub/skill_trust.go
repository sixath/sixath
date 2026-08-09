package hub

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

var (
	// ErrSkillNotMaterialized means external skill content is not trusted/cached yet.
	ErrSkillNotMaterialized = errors.New("hub: skill not materialized")
	// ErrSkillTrustRejected means signature/checksum verification failed.
	ErrSkillTrustRejected = errors.New("hub: skill trust rejected")
)

// SkillContent is the payload fetched from an external Hub for materialization.
// This is intentionally separate from CommitProceduralRepair (design §3.5.3).
type SkillContent struct {
	Hub         string
	SkillID     string
	Name        string
	Version     string
	Body        []byte // SKILL.md (or primary body)
	Attachments map[string][]byte
	// ContentHash if non-empty is the Adapter-provided checksum (hex sha256 of Body).
	ContentHash string
	// Signed true means Adapter asserts a verified signature/checksum.
	Signed bool
}

// MaterializeResult is the local cache outcome after SkillTrustGate.
type MaterializeResult struct {
	Ref                AssetRef
	CacheDir           string
	ContentHash        string
	Version            string
	NewMaterialization bool
}

// SkillSource fetches remote skill bodies. Implemented by external Adapters.
type SkillSource interface {
	FetchSkill(ctx context.Context, hubName, skillID string) (SkillContent, error)
}

// SkillTrustConfig configures unsigned approval policy.
type SkillTrustConfig struct {
	RequireManualApproveUnsigned bool
	RematerializeOnVersionChange bool
}

// DefaultSkillTrustConfig returns design defaults (§3.5.3).
func DefaultSkillTrustConfig() SkillTrustConfig {
	return SkillTrustConfig{
		RequireManualApproveUnsigned: true,
		RematerializeOnVersionChange: true,
	}
}

// SkillCache looks up previously materialized skills.
type SkillCache interface {
	Lookup(hubName, skillID string) (cachedVersion, cachedHash, cacheDir string, ok bool)
	MarkSuperseded(hubName, skillID, oldVersion string) error
}

// MaterializeWriter persists skill body to local disk.
type MaterializeWriter interface {
	Write(content SkillContent, status AssetStatus) (cacheDir string, contentHash string, err error)
}

// SkillTrustGate materializes external skills into a local trusted cache.
// Not to be confused with CommitProceduralRepair.
type SkillTrustGate struct {
	Cfg    SkillTrustConfig
	Cache  SkillCache
	Writer MaterializeWriter
}

// NewSkillTrustGate builds a gate; nil cfg uses DefaultSkillTrustConfig.
func NewSkillTrustGate(writer MaterializeWriter, cache SkillCache, cfg *SkillTrustConfig) *SkillTrustGate {
	c := DefaultSkillTrustConfig()
	if cfg != nil {
		c = *cfg
	}
	return &SkillTrustGate{Cfg: c, Cache: cache, Writer: writer}
}

// Materialize verifies trust, writes cache, returns AssetRef with draft/active status.
func (g *SkillTrustGate) Materialize(ctx context.Context, content SkillContent) (MaterializeResult, error) {
	_ = ctx
	if g == nil || g.Writer == nil {
		return MaterializeResult{}, fmt.Errorf("%w: no materialize writer", ErrSkillNotMaterialized)
	}
	if content.Hub == "" || content.SkillID == "" {
		return MaterializeResult{}, fmt.Errorf("%w: hub and skill_id required", ErrSkillNotMaterialized)
	}
	if len(content.Body) == 0 {
		return MaterializeResult{}, fmt.Errorf("%w: empty body", ErrSkillNotMaterialized)
	}
	hash := HashSkillBody(content.Body)
	if content.ContentHash != "" && !equalFoldHex(content.ContentHash, hash) {
		return MaterializeResult{}, fmt.Errorf("%w: content_hash mismatch", ErrSkillTrustRejected)
	}
	status := AssetActive
	if !content.Signed && g.Cfg.RequireManualApproveUnsigned {
		status = AssetDraft
	}

	newMat := true
	if g.Cache != nil {
		ver, cachedHash, dir, ok := g.Cache.Lookup(content.Hub, content.SkillID)
		if ok {
			sameVer := ver == content.Version
			sameHash := equalFoldHex(cachedHash, hash)
			if sameVer && sameHash {
				newMat = false
				ref := AssetRef{
					Kind:    AssetKindSkill,
					ID:      content.SkillID,
					Hub:     content.Hub,
					Name:    content.Name,
					Version: content.Version,
					Status:  status,
					Meta: map[string]any{
						"content_hash": hash,
						"cache_dir":    dir,
					},
				}
				return MaterializeResult{
					Ref:                ref,
					CacheDir:           dir,
					ContentHash:        hash,
					Version:            content.Version,
					NewMaterialization: false,
				}, nil
			}
			if g.Cfg.RematerializeOnVersionChange {
				_ = g.Cache.MarkSuperseded(content.Hub, content.SkillID, ver)
			}
		}
	}
	dir, writtenHash, err := g.Writer.Write(content, status)
	if err != nil {
		return MaterializeResult{}, err
	}
	if writtenHash == "" {
		writtenHash = hash
	}
	ref := AssetRef{
		Kind:    AssetKindSkill,
		ID:      content.SkillID,
		Hub:     content.Hub,
		Name:    content.Name,
		Version: content.Version,
		Status:  status,
		Meta: map[string]any{
			"content_hash": writtenHash,
			"cache_dir":    dir,
		},
	}
	return MaterializeResult{
		Ref:                ref,
		CacheDir:           dir,
		ContentHash:        writtenHash,
		Version:            content.Version,
		NewMaterialization: newMat,
	}, nil
}

// HashSkillBody returns hex sha256 of body.
func HashSkillBody(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func equalFoldHex(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'F' {
			ca += 'a' - 'A'
		}
		if cb >= 'A' && cb <= 'F' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}

// --- FS materialize + memory cache ---

type skillManifest struct {
	Hub         string `json:"hub"`
	SkillID     string `json:"skill_id"`
	Version     string `json:"version"`
	ContentHash string `json:"content_hash"`
	Status      string `json:"status"`
	Superseded  bool   `json:"superseded,omitempty"`
}

// FSMaterializer writes hub-skills/<hub>/<id>@<version>/ and tracks an in-memory index.
type FSMaterializer struct {
	Root string
	mu   sync.Mutex
	idx  map[string]skillManifest // hub\0id → latest
}

// NewFSMaterializer creates a writer+cache rooted at root.
func NewFSMaterializer(root string) *FSMaterializer {
	return &FSMaterializer{Root: root, idx: map[string]skillManifest{}}
}

func cacheKey(hub, id string) string { return hub + "\x00" + id }

func (f *FSMaterializer) Lookup(hubName, skillID string) (string, string, string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.idx[cacheKey(hubName, skillID)]
	if !ok || m.Superseded {
		return "", "", "", false
	}
	dir := f.dirFor(hubName, skillID, m.Version)
	return m.Version, m.ContentHash, dir, true
}

func (f *FSMaterializer) MarkSuperseded(hubName, skillID, oldVersion string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	k := cacheKey(hubName, skillID)
	if m, ok := f.idx[k]; ok && m.Version == oldVersion {
		m.Superseded = true
		f.idx[k] = m
	}
	return nil
}

func (f *FSMaterializer) dirFor(hubName, skillID, version string) string {
	v := version
	if v == "" {
		v = "default"
	}
	return filepath.Join(f.Root, "hub-skills", hubName, skillID+"@"+v)
}

func (f *FSMaterializer) Write(content SkillContent, status AssetStatus) (string, string, error) {
	hash := HashSkillBody(content.Body)
	dir := f.dirFor(content.Hub, content.SkillID, content.Version)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", err
	}
	skillPath := filepath.Join(dir, "SKILL.md")
	_ = os.Remove(skillPath) // clear prior read-only materialization
	if err := os.WriteFile(skillPath, content.Body, 0o644); err != nil {
		return "", "", err
	}
	for name, data := range content.Attachments {
		p := filepath.Join(dir, filepath.Base(name))
		_ = os.Remove(p)
		if err := os.WriteFile(p, data, 0o644); err != nil {
			return "", "", err
		}
	}
	man := skillManifest{
		Hub:         content.Hub,
		SkillID:     content.SkillID,
		Version:     content.Version,
		ContentHash: hash,
		Status:      string(status),
	}
	b, _ := json.MarshalIndent(man, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), b, 0o644); err != nil {
		return "", "", err
	}
	f.mu.Lock()
	f.idx[cacheKey(content.Hub, content.SkillID)] = man
	f.mu.Unlock()
	return dir, hash, nil
}

var (
	_ MaterializeWriter = (*FSMaterializer)(nil)
	_ SkillCache        = (*FSMaterializer)(nil)
)
