package handbook

import (
	"context"
	"encoding/json"
	"time"
)

// GeneratorVersion changes whenever the output format changes; repos built by an older
// generator are rebuilt on the next pass.
const GeneratorVersion = "p2a-2"

// BuildInput identifies the checkout to build from.
type BuildInput struct {
	RepoID  string
	RelPath string
	Root    string // absolute repository root with symlinks resolved
	Commit  string
	Now     time.Time
}

// Stats is stored in repositories.handbook_stats.
type Stats struct {
	GeneratorVersion string `json:"generator_version"`
	BuiltAt          string `json:"built_at"`
	Files            int    `json:"files"`
	GoFiles          int    `json:"go_files"`
	Packages         int    `json:"packages"`
	Areas            int    `json:"areas"`
	Symbols          int    `json:"symbols"`
	Registers        int    `json:"registers"`
	Truncated        bool   `json:"truncated"`
}

// Map converts stats to the JSON object stored in the database.
func (s Stats) Map() map[string]any {
	b, _ := json.Marshal(s)
	m := map[string]any{}
	_ = json.Unmarshal(b, &m)
	return m
}

// Manifest describes one published version.
type Manifest struct {
	RelPath          string    `json:"rel_path"`
	Commit           string    `json:"commit"`
	GeneratorVersion string    `json:"generator_version"`
	LeafMode         string    `json:"leaf_mode"`
	GeneratedAt      time.Time `json:"generated_at"`
	Stats            Stats     `json:"stats"`
}

// Output is the file set of one version, keyed by slash path relative to v<N>/.
type Output struct {
	Files map[string][]byte
	Stats Stats
}

// Build collects facts from in.Root and renders the handbook files.
func Build(ctx context.Context, in BuildInput) (*Output, error) {
	facts, err := CollectFacts(ctx, in.Root)
	if err != nil {
		return nil, err
	}
	if in.Now.IsZero() {
		in.Now = time.Now()
	}
	meta := RenderMeta{RelPath: in.RelPath, Commit: in.Commit, GeneratedAt: in.Now}
	out := &Output{Files: map[string][]byte{}}
	for p, c := range Render(meta, facts) {
		out.Files["skill/"+p] = []byte(c)
	}
	regNames := map[[2]string]bool{}
	for _, h := range facts.Registers {
		regNames[[2]string{h.Kind, h.Name}] = true
	}
	stats := Stats{
		GeneratorVersion: GeneratorVersion, BuiltAt: in.Now.UTC().Format(time.RFC3339),
		Files: len(facts.Files), Packages: len(facts.Packages), Areas: len(buildAreas(facts)),
		Registers: len(regNames), Truncated: facts.Coverage.Truncated,
	}
	for _, f := range facts.Files {
		if f.Lang == "go" {
			stats.GoFiles++
		}
	}
	for _, s := range facts.Symbols {
		stats.Symbols += len(s)
	}
	docs := map[string]any{
		"facts/files.json":     facts.Files,
		"facts/symbols.json":   facts.Symbols,
		"facts/registers.json": facts.Registers,
		"facts/packages.json":  facts.Packages,
		"coverage.json":        facts.Coverage,
		"manifest.json": Manifest{
			RelPath: in.RelPath, Commit: in.Commit, GeneratorVersion: GeneratorVersion,
			LeafMode: "file", GeneratedAt: in.Now, Stats: stats,
		},
	}
	for p, v := range docs {
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return nil, err
		}
		out.Files[p] = b
	}
	out.Stats = stats
	return out, nil
}
