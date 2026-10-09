package handbook

import (
	"context"
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/sixath/framework/model"
)

const (
	minStages            = 2
	maxStages            = 15
	skeletonInputBudget  = 120 << 10
	skeletonBaseTokens   = 1000
	skeletonTokensPerDir = 20
	skeletonMaxTokensCap = 8192 // many OpenAI-compatible providers reject larger max_tokens
	skeletonMaxDirs      = (skeletonMaxTokensCap - skeletonBaseTokens) / skeletonTokensPerDir
	maxDirPurposes       = 3
	rebuildChangedRatio  = 5  // full rebuild when changed files exceed 1/5 (20%) of the base
	rebuildUnassignedPct = 10 // full rebuild when more than 10% of files are unassigned
	otherStageID         = "other"
)

// FallbackRetryInterval is how long a skeleton that fell back on an unusable reply is kept
// before the next run retries the model.
const FallbackRetryInterval = 24 * time.Hour

// Fallback reasons recorded in Skeleton.FallbackReason.
const (
	FallbackTooLarge   = "too_large"
	FallbackBadReply   = "bad_reply"
	FallbackFewStages  = "few_stages"
	FallbackUnassigned = "unassigned"
	FallbackNoModel    = "no_model"
)

var stageIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

type dirSummary struct {
	Dir      string
	Files    []File
	Purposes []string
}

func eligibleFiles(f *Facts) []File {
	var out []File
	for _, file := range f.Files {
		if CardEligible(file) {
			out = append(out, file)
		}
	}
	return out
}

func eligibleDirs(f *Facts, cards map[string]*Card) []dirSummary {
	byDir := map[string]*dirSummary{}
	var dirs []string
	for _, file := range eligibleFiles(f) {
		d := path.Dir(file.Path)
		s := byDir[d]
		if s == nil {
			s = &dirSummary{Dir: d}
			byDir[d] = s
			dirs = append(dirs, d)
		}
		s.Files = append(s.Files, file)
		if c := cards[file.Path]; c != nil && len(s.Purposes) < maxDirPurposes {
			s.Purposes = append(s.Purposes, path.Base(file.Path)+"："+clipRunes(collapseSpaces(c.Purpose), 60))
		}
	}
	sort.Strings(dirs)
	out := make([]dirSummary, 0, len(dirs))
	for _, d := range dirs {
		out = append(out, *byDir[d])
	}
	return out
}

// collapseDirs merges every directory deeper than depth into its depth-level ancestor and
// keeps at most purposes responsibility lines per resulting directory. depth 0 keeps dirs.
func collapseDirs(dirs []dirSummary, depth, purposes int) []dirSummary {
	byDir := map[string]*dirSummary{}
	var names []string
	for _, d := range dirs {
		name := d.Dir
		if depth > 0 && name != "." {
			if parts := strings.Split(name, "/"); len(parts) > depth {
				name = strings.Join(parts[:depth], "/")
			}
		}
		s := byDir[name]
		if s == nil {
			s = &dirSummary{Dir: name}
			byDir[name] = s
			names = append(names, name)
		}
		s.Files = append(s.Files, d.Files...)
		for _, p := range d.Purposes {
			if len(s.Purposes) < purposes {
				s.Purposes = append(s.Purposes, p)
			}
		}
	}
	sort.Strings(names)
	out := make([]dirSummary, 0, len(names))
	for _, n := range names {
		out = append(out, *byDir[n])
	}
	return out
}

func topDir(p string) string {
	if i := strings.IndexByte(p, '/'); i >= 0 {
		return p[:i]
	}
	return "."
}

func topDirsOf(files []File) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range files {
		if t := topDir(f.Path); !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	sort.Strings(out)
	return out
}

// dirChain lists dir and its ancestors up to the top-level directory. Repository-root files
// form their own group ".", which is not an ancestor of nested directories.
func dirChain(dir string) []string {
	if dir == "." || dir == "" {
		return []string{"."}
	}
	var out []string
	for d := dir; d != "." && d != "/" && d != ""; d = path.Dir(d) {
		out = append(out, d)
	}
	return out
}

// stageIndex counts assigned files per stage under each directory of dirChain.
type stageIndex map[string]map[string]int

func newStageIndex(files map[string]FileAssign) stageIndex {
	ix := stageIndex{}
	for p, a := range files {
		ix.add(p, a.Stage)
	}
	return ix
}

func (ix stageIndex) add(p, stage string) {
	if stage == "" {
		return
	}
	for _, d := range dirChain(path.Dir(p)) {
		if ix[d] == nil {
			ix[d] = map[string]int{}
		}
		ix[d][stage]++
	}
}

// guess returns the majority stage of assigned files under the nearest directory of
// dirChain(dir) that has any; ties go to the smallest stage id.
func (ix stageIndex) guess(dir string) string {
	for _, d := range dirChain(dir) {
		best, n := "", 0
		for s, c := range ix[d] {
			if c > n || (c == n && s < best) {
				best, n = s, c
			}
		}
		if best != "" {
			return best
		}
	}
	return ""
}

func collapseSpaces(s string) string { return strings.Join(strings.Fields(s), " ") }

const skeletonSystemPrompt = "你是资深架构师，把代码仓库按运行时行为划分为执行阶段（stage），供故障排查使用。只输出一个 JSON 对象，不要输出其他文字。"

func skeletonPrompt(relPath string, f *Facts, dirs []dirSummary, depth int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "仓库：%s\n", relPath)
	var mains []string
	for _, p := range f.Packages {
		if p.Main {
			mains = append(mains, p.Dir)
		}
	}
	if len(mains) > 0 {
		fmt.Fprintf(&b, "入口（package main）：%s\n", strings.Join(mains, "、"))
	}
	b.WriteString("\n目录（括号内为源码文件数，后面是部分文件的职责）：\n")
	if depth > 0 {
		fmt.Fprintf(&b, "（目录已合并到前 %d 级，文件数包含子目录）\n", depth)
	}
	for _, d := range dirs {
		fmt.Fprintf(&b, "- %s（%d 个文件）", d.Dir, len(d.Files))
		if len(d.Purposes) > 0 {
			b.WriteString("：" + strings.Join(d.Purposes, "；"))
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "\n把仓库划分为 %d-%d 个执行阶段（按请求/任务的处理流程，而不是按技术分层），每个目录归入一个阶段。\n", minStages+1, maxStages)
	b.WriteString(`输出 JSON：{"stages":[{"id":"小写字母数字和连字符","title":"中文短标题","summary":"一句话"}],"assign":{"目录":"stage id"}}` + "\n")
	b.WriteString("assign 必须覆盖上面列出的每个目录，目录名原样复制。\n")
	return b.String()
}

// skeletonLevels are tried in order until the prompt fits: fewer responsibility lines first,
// then directories collapsed to shallower depths.
var skeletonLevels = []struct{ depth, purposes int }{
	{0, 3}, {0, 1}, {0, 0}, {4, 1}, {4, 0}, {3, 1}, {3, 0}, {2, 1}, {2, 0}, {1, 1}, {1, 0},
}

// fitSkeletonPrompt returns the first prompt within the input budget whose directories the
// reply can assign within skeletonMaxTokensCap, and the directories it lists.
func fitSkeletonPrompt(relPath string, f *Facts, dirs []dirSummary) (string, []dirSummary, bool) {
	for _, lv := range skeletonLevels {
		ds := collapseDirs(dirs, lv.depth, lv.purposes)
		if len(ds) > skeletonMaxDirs {
			continue
		}
		if p := skeletonPrompt(relPath, f, ds, lv.depth); len(p) <= skeletonInputBudget {
			return p, ds, true
		}
	}
	return "", nil, false
}

func skeletonMaxTokens(dirs int) int {
	return min(skeletonBaseTokens+skeletonTokensPerDir*dirs, skeletonMaxTokensCap)
}

type skeletonReplyJSON struct {
	Stages []Stage           `json:"stages"`
	Assign map[string]string `json:"assign"`
}

func newSkeleton(files []File, commit string, now time.Time) *Skeleton {
	return &Skeleton{
		PromptVersion: LLMPromptVersion, Commit: commit, BuiltAt: now, UpdatedAt: now,
		BaseFiles: len(files), TopDirs: topDirsOf(files), Files: map[string]FileAssign{},
	}
}

// inferSkeleton builds a fresh skeleton by asking the model to assign directories to stages.
// Directories the reply misses inherit their nearest assigned ancestor, then the majority
// stage of their neighbours. A prompt that cannot fit even with directories collapsed, an
// unusable reply, too few stages or too many unassigned files fall back to directory areas.
func inferSkeleton(ctx context.Context, m model.Model, relPath string, f *Facts, cards map[string]*Card, commit string, now time.Time, u *usage) (*Skeleton, error) {
	dirs := eligibleDirs(f, cards)
	if len(dirs) == 0 {
		return newSkeleton(nil, commit, now), nil
	}
	if m == nil {
		return fallbackSkeleton(f, commit, now, FallbackNoModel), nil
	}
	prompt, promptDirs, ok := fitSkeletonPrompt(relPath, f, dirs)
	if !ok {
		return fallbackSkeleton(f, commit, now, FallbackTooLarge), nil
	}
	var r skeletonReplyJSON
	if err := llmJSON(ctx, m, skeletonSystemPrompt, prompt, skeletonMaxTokens(len(promptDirs)), &r, u); err != nil {
		if errors.Is(err, errBadReply) {
			return fallbackSkeleton(f, commit, now, FallbackBadReply), nil
		}
		return nil, err
	}
	valid := map[string]bool{}
	var stages []Stage
	for _, s := range r.Stages {
		id := strings.TrimSpace(s.ID)
		if !stageIDRe.MatchString(id) || valid[id] {
			continue
		}
		valid[id] = true
		title := clipRunes(collapseSpaces(firstNonEmpty(s.Title, id)), stageTitleRunes)
		stages = append(stages, Stage{ID: id, Title: title, Summary: clipRunes(collapseSpaces(s.Summary), 200)})
	}
	files := eligibleFiles(f)
	sk := newSkeleton(files, commit, now)
	counts := map[string]int{}
	for _, d := range dirs {
		stage := ""
		for _, a := range dirChain(d.Dir) {
			if s := strings.TrimSpace(r.Assign[a]); valid[s] {
				stage = s
				break
			}
		}
		for _, file := range d.Files {
			sk.Files[file.Path] = FileAssign{Stage: stage, CardHash: file.Hash, Hash: file.Hash}
		}
		if stage != "" {
			counts[stage] += len(d.Files)
		}
	}
	kept := clampStages(stages, counts)
	for p, a := range sk.Files {
		if a.Stage != "" && !kept[a.Stage] {
			a.Stage = ""
			sk.Files[p] = a
		}
	}
	for _, s := range stages {
		if kept[s.ID] {
			sk.Stages = append(sk.Stages, s)
		}
	}
	if len(sk.Stages) < minStages {
		return fallbackSkeleton(f, commit, now, FallbackFewStages), nil
	}
	if assignUnassigned(sk, files)*100 > len(files)*rebuildUnassignedPct {
		return fallbackSkeleton(f, commit, now, FallbackUnassigned), nil
	}
	return sk, nil
}

// clampStages returns the used stages, keeping the maxStages with the most files.
func clampStages(stages []Stage, counts map[string]int) map[string]bool {
	var used []Stage
	for _, s := range stages {
		if counts[s.ID] > 0 {
			used = append(used, s)
		}
	}
	sort.SliceStable(used, func(i, j int) bool { return counts[used[i].ID] > counts[used[j].ID] })
	kept := map[string]bool{}
	for i, s := range used {
		if i == maxStages {
			break
		}
		kept[s.ID] = true
	}
	return kept
}

// assignUnassigned gives unassigned files the majority stage of their nearest directory and
// returns how many stay unassigned.
func assignUnassigned(sk *Skeleton, files []File) int {
	ix := newStageIndex(sk.Files)
	left := 0
	for _, file := range files {
		a := sk.Files[file.Path]
		if a.Stage != "" {
			continue
		}
		if a.Stage = ix.guess(path.Dir(file.Path)); a.Stage == "" {
			left++
			continue
		}
		sk.Files[file.Path] = a
		ix.add(file.Path, a.Stage)
	}
	return left
}

// fallbackSkeleton uses the directory areas of card-eligible files as stages. Beyond
// maxStages areas, the largest maxStages-1 are kept and the rest merge into "other".
func fallbackSkeleton(f *Facts, commit string, now time.Time, reason string) *Skeleton {
	files := eligibleFiles(f)
	sk := newSkeleton(files, commit, now)
	sk.FallbackAreas, sk.FallbackReason = true, reason
	areas := buildAreas(&Facts{Files: files})
	keep := map[string]bool{}
	if len(areas) > maxStages {
		bySize := make([]area, len(areas))
		copy(bySize, areas)
		sort.SliceStable(bySize, func(i, j int) bool { return len(bySize[i].Files) > len(bySize[j].Files) })
		for _, a := range bySize[:maxStages-1] {
			keep[a.ID] = true
		}
	}
	otherID := otherStageID
	for k := 2; ; k++ {
		taken := false
		for _, a := range areas {
			taken = taken || a.ID == otherID
		}
		if !taken {
			break
		}
		otherID = fmt.Sprintf("%s-%d", otherStageID, k)
	}
	hasOther := false
	for _, a := range areas {
		id := a.ID
		if len(keep) > 0 && !keep[id] {
			id, hasOther = otherID, true
		} else {
			sk.Stages = append(sk.Stages, Stage{ID: a.ID, Title: a.Name})
		}
		for _, file := range a.Files {
			sk.Files[file.Path] = FileAssign{Stage: id, CardHash: file.Hash, Hash: file.Hash}
		}
	}
	if hasOther {
		sk.Stages = append(sk.Stages, Stage{ID: otherID, Title: "其他"})
	}
	return sk
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// updateSkeleton applies file changes since the skeleton was last updated. It returns the
// remaining stages whose file set or file contents changed and the changed paths. Stages left
// without files are removed. CardHash of changed files is left for the caller, which knows
// whether a card of the new content exists.
func updateSkeleton(sk *Skeleton, f *Facts, commit string, now time.Time) (map[string]bool, map[string]bool) {
	if sk.Files == nil {
		sk.Files = map[string]FileAssign{}
	}
	affected, changed := map[string]bool{}, map[string]bool{}
	current := map[string]File{}
	for _, file := range eligibleFiles(f) {
		current[file.Path] = file
	}
	for p, a := range sk.Files {
		if _, ok := current[p]; !ok {
			delete(sk.Files, p)
			changed[p] = true
			if a.Stage != "" {
				affected[a.Stage] = true
			}
		}
	}
	var added []File
	for p, file := range current {
		a, ok := sk.Files[p]
		if !ok {
			added = append(added, file)
			continue
		}
		if a.organizedHash() != file.Hash {
			a.Hash = file.Hash
			sk.Files[p] = a
			changed[p] = true
			if a.Stage != "" {
				affected[a.Stage] = true
			}
		}
	}
	sort.Slice(added, func(i, j int) bool { return added[i].Path < added[j].Path })
	ix := newStageIndex(sk.Files)
	for _, file := range added {
		stage := ix.guess(path.Dir(file.Path))
		sk.Files[file.Path] = FileAssign{Stage: stage, CardHash: file.Hash, Hash: file.Hash}
		ix.add(file.Path, stage)
		changed[file.Path] = true
		if stage != "" {
			affected[stage] = true
		}
	}
	nonEmpty := map[string]bool{}
	for _, a := range sk.Files {
		nonEmpty[a.Stage] = true
	}
	stages := sk.Stages[:0]
	for _, s := range sk.Stages {
		if nonEmpty[s.ID] {
			stages = append(stages, s)
		} else {
			delete(affected, s.ID)
		}
	}
	sk.Stages = stages
	sk.ChangedPaths = mergePaths(sk.ChangedPaths, changed)
	sk.ChangedSinceRebuild = len(sk.ChangedPaths)
	sk.Commit, sk.UpdatedAt = commit, now
	return affected, changed
}

func mergePaths(old []string, add map[string]bool) []string {
	set := make(map[string]bool, len(old)+len(add))
	for _, p := range old {
		set[p] = true
	}
	for p := range add {
		set[p] = true
	}
	out := make([]string, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// rebuildReason reports why the skeleton needs a full rebuild, or "" when an incremental
// update suffices. Call it after updateSkeleton so new files are already organized.
func rebuildReason(sk *Skeleton, f *Facts, now time.Time, maxAgeDays int) string {
	switch {
	case sk == nil:
		return "none"
	case sk.PromptVersion != LLMPromptVersion:
		return "prompt"
	case maxAgeDays > 0 && now.Sub(sk.BuiltAt) > time.Duration(maxAgeDays)*24*time.Hour:
		return "age"
	case sk.FallbackAreas && sk.FallbackReason == FallbackBadReply && now.Sub(sk.BuiltAt) > FallbackRetryInterval:
		return "fallback_retry"
	case sk.BaseFiles > 0 && max(sk.ChangedSinceRebuild, len(sk.ChangedPaths))*rebuildChangedRatio > sk.BaseFiles:
		return "changes"
	}
	known := map[string]bool{}
	for _, d := range sk.TopDirs {
		known[d] = true
	}
	files := eligibleFiles(f)
	for _, d := range topDirsOf(files) {
		if d != "." && !known[d] {
			return "topdir"
		}
	}
	unassigned := 0
	for _, file := range files {
		if sk.Files[file.Path].Stage == "" {
			unassigned++
		}
	}
	if len(files) > 0 && unassigned*100 > len(files)*rebuildUnassignedPct {
		return "unassigned"
	}
	return ""
}
