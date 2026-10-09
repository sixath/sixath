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
	skeletonMaxTokens    = 6000
	maxDirPurposes       = 3
	rebuildChangedRatio  = 5  // full rebuild when changed files exceed 1/5 (20%) of the base
	rebuildUnassignedPct = 10 // full rebuild when more than 10% of files are unassigned
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
			s.Purposes = append(s.Purposes, path.Base(file.Path)+"："+clipRunes(c.Purpose, 60))
		}
	}
	sort.Strings(dirs)
	out := make([]dirSummary, 0, len(dirs))
	for _, d := range dirs {
		out = append(out, *byDir[d])
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

const skeletonSystemPrompt = "你是资深架构师，把代码仓库按运行时行为划分为执行阶段（stage），供故障排查使用。只输出一个 JSON 对象，不要输出其他文字。"

func skeletonPrompt(relPath string, f *Facts, dirs []dirSummary) string {
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
// An over-budget prompt or unusable reply falls back to directory areas as stages.
func inferSkeleton(ctx context.Context, m model.Model, relPath string, f *Facts, cards map[string]*Card, commit string, now time.Time, u *usage) (*Skeleton, error) {
	dirs := eligibleDirs(f, cards)
	prompt := skeletonPrompt(relPath, f, dirs)
	if len(prompt) > skeletonInputBudget {
		return fallbackSkeleton(f, commit, now), nil
	}
	var r skeletonReplyJSON
	if err := llmJSON(ctx, m, skeletonSystemPrompt, prompt, skeletonMaxTokens, &r, u); err != nil {
		if errors.Is(err, errBadReply) {
			return fallbackSkeleton(f, commit, now), nil
		}
		return nil, err
	}
	sk := newSkeleton(eligibleFiles(f), commit, now)
	valid := map[string]bool{}
	var stages []Stage
	for _, s := range r.Stages {
		id := strings.TrimSpace(s.ID)
		if !stageIDRe.MatchString(id) || valid[id] || len(stages) == maxStages {
			continue
		}
		valid[id] = true
		stages = append(stages, Stage{ID: id, Title: clipRunes(firstNonEmpty(s.Title, id), 40), Summary: clipRunes(s.Summary, 200)})
	}
	used := map[string]bool{}
	for _, d := range dirs {
		stage := strings.TrimSpace(r.Assign[d.Dir])
		if !valid[stage] {
			stage = ""
		}
		for _, file := range d.Files {
			sk.Files[file.Path] = FileAssign{Stage: stage, CardHash: file.Hash}
		}
		if stage != "" {
			used[stage] = true
		}
	}
	for _, s := range stages {
		if used[s.ID] {
			sk.Stages = append(sk.Stages, s)
		}
	}
	if len(sk.Stages) < minStages {
		return fallbackSkeleton(f, commit, now), nil
	}
	return sk, nil
}

// fallbackSkeleton uses the directory areas of card-eligible files as stages.
func fallbackSkeleton(f *Facts, commit string, now time.Time) *Skeleton {
	files := eligibleFiles(f)
	sk := newSkeleton(files, commit, now)
	sk.FallbackAreas = true
	for _, a := range buildAreas(&Facts{Files: files}) {
		sk.Stages = append(sk.Stages, Stage{ID: a.ID, Title: a.Name})
		for _, file := range a.Files {
			sk.Files[file.Path] = FileAssign{Stage: a.ID, CardHash: file.Hash}
		}
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
// stages whose file set or file contents changed and the changed paths.
func updateSkeleton(sk *Skeleton, f *Facts, commit string, now time.Time) (map[string]bool, map[string]bool) {
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
		if a.CardHash != file.Hash {
			a.CardHash = file.Hash
			sk.Files[p] = a
			changed[p] = true
			if a.Stage != "" {
				affected[a.Stage] = true
			}
		}
	}
	sort.Slice(added, func(i, j int) bool { return added[i].Path < added[j].Path })
	for _, file := range added {
		stage := guessStage(sk, path.Dir(file.Path))
		sk.Files[file.Path] = FileAssign{Stage: stage, CardHash: file.Hash}
		changed[file.Path] = true
		if stage != "" {
			affected[stage] = true
		}
	}
	sk.ChangedSinceRebuild += len(changed)
	sk.Commit, sk.UpdatedAt = commit, now
	return affected, changed
}

// guessStage returns the majority stage of organized files under dir, walking up to the
// top-level directory; ties go to the smallest stage id.
func guessStage(sk *Skeleton, dir string) string {
	for d := dir; d != "." && d != ""; d = path.Dir(d) {
		counts := map[string]int{}
		for p, a := range sk.Files {
			if a.Stage != "" && strings.HasPrefix(p, d+"/") {
				counts[a.Stage]++
			}
		}
		best, n := "", 0
		for s, c := range counts {
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
	case sk.BaseFiles > 0 && sk.ChangedSinceRebuild*rebuildChangedRatio > sk.BaseFiles:
		return "changes"
	}
	known := map[string]bool{}
	for _, d := range sk.TopDirs {
		known[d] = true
	}
	files := eligibleFiles(f)
	for _, d := range topDirsOf(files) {
		if !known[d] {
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
