package handbook

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// MaxPageBytes keeps each page small enough to read in one read_skill_file call.
const MaxPageBytes = 48 << 10

const (
	maxSymbolsPerFile  = 60
	maxPackagesPerArea = 50
	maxLocationsPerReg = 30
	maxRequires        = 40
	maxMains           = 50
	indexTableRows     = 100
)

// RenderMeta identifies the repository and commit a handbook was built from.
type RenderMeta struct {
	RelPath     string
	Commit      string
	GeneratedAt time.Time
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

const maxSlug = 60

func slug(s string) string {
	s = strings.Trim(slugRe.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if len(s) > maxSlug {
		s = strings.TrimRight(s[:maxSlug], "-")
	}
	if s == "" {
		s = "root"
	}
	return s
}

// SkillName is the handbook skill name for a repository logical name (rel_path). When the
// slug does not reproduce rel_path exactly, a sha256 prefix of rel_path keeps names unique.
func SkillName(relPath string) string {
	s := strings.Trim(slugRe.ReplaceAllString(strings.ToLower(relPath), "-"), "-")
	if s != "" && s == relPath && len(s) <= maxSlug {
		return "handbook-" + s
	}
	sum := sha256.Sum256([]byte(relPath))
	suffix := hex.EncodeToString(sum[:4])
	if room := maxSlug - len(suffix) - 1; len(s) > room {
		s = strings.TrimRight(s[:room], "-")
	}
	if s == "" {
		s = "root"
	}
	return "handbook-" + s + "-" + suffix
}

func shortCommit(c string) string {
	if len(c) > 12 {
		return c[:12]
	}
	return c
}

var areaPrefixes = map[string]bool{
	"internal": true, "pkg": true, "cmd": true, "app": true, "apps": true,
	"src": true, "service": true, "services": true, "api": true,
}

// areaOfDir groups directories by their first segment, or first two under common prefixes.
func areaOfDir(dir string) string {
	if dir == "." || dir == "" {
		return "(root)"
	}
	parts := strings.Split(dir, "/")
	if areaPrefixes[parts[0]] && len(parts) > 1 {
		return parts[0] + "/" + parts[1]
	}
	return parts[0]
}

type area struct {
	Name  string
	ID    string
	Files []File
}

func buildAreas(f *Facts) []area {
	byName := map[string]*area{}
	var names []string
	for _, file := range f.Files {
		n := areaOfDir(path.Dir(file.Path))
		a := byName[n]
		if a == nil {
			a = &area{Name: n}
			byName[n] = a
			names = append(names, n)
		}
		a.Files = append(a.Files, file)
	}
	sort.Strings(names)
	taken := map[string]bool{}
	out := make([]area, 0, len(names))
	for _, n := range names {
		a := byName[n]
		base := slug(n)
		id := base
		for k := 2; taken[id]; k++ {
			id = fmt.Sprintf("%s-%d", base, k)
		}
		taken[id] = true
		a.ID = id
		sort.SliceStable(a.Files, func(i, j int) bool {
			di, dj := path.Dir(a.Files[i].Path), path.Dir(a.Files[j].Path)
			if di != dj {
				return di < dj
			}
			return a.Files[i].Path < a.Files[j].Path
		})
		out = append(out, *a)
	}
	return out
}

// Render returns the skill pages keyed by path relative to the skill directory. A nil or
// empty LLM layer renders the static handbook.
func Render(meta RenderMeta, f *Facts, l *LLMLayer) map[string]string {
	v := newLLMView(f, l)
	areas := buildAreas(f)
	pages := map[string]string{}
	for p, c := range paginate("references/registers", "# "+meta.RelPath+" 寄存器", registerSections(f.Registers, v.registerNotes())) {
		pages[p] = c
	}
	for _, a := range areas {
		for p, c := range paginate("references/areas/"+a.ID, "# 分区 "+a.Name, areaSections(a, f, v)) {
			pages[p] = c
		}
	}
	for _, st := range v.stageList() {
		for p, c := range paginate("references/stages/"+st.ID, "# 阶段 "+st.Title, stageSections(st, v.stageFiles[st.ID], f, v)) {
			pages[p] = c
		}
	}
	indexTitle := "# " + meta.RelPath + " 分区索引"
	if v.hasStages() {
		indexTitle = "# " + meta.RelPath + " 索引"
	}
	for p, c := range paginate("references/index", indexTitle, indexSections(areas, f, v)) {
		pages[p] = c
	}
	pages["references/overview.md"] = renderOverview(meta, f, areas, v)
	pages["SKILL.md"] = renderSkill(meta, v.hasStages())
	return pages
}

const (
	truncatedMark = "\n…（已截断）\n"
	minPageBudget = 4 << 10
)

// paginate splits sections into pages of at most MaxPageBytes without splitting a section;
// a section larger than a page is truncated at a rune boundary.
// The first page is <base>.md and lists continuation pages <base>.p2.md, <base>.p3.md, ...
func paginate(base, title string, sections []string) map[string]string {
	name := func(i int) string {
		if i == 0 {
			return base + ".md"
		}
		return fmt.Sprintf("%s.p%d.md", base, i+1)
	}
	contents := func(n int) string {
		if n <= 1 {
			return ""
		}
		var b strings.Builder
		b.WriteString("续页：")
		for j := 1; j < n; j++ {
			if j > 1 {
				b.WriteString("、")
			}
			fmt.Fprintf(&b, "`%s`", name(j))
		}
		b.WriteString("\n\n")
		return b.String()
	}
	var chunks [][]string
	for n := 1; ; {
		header := len(title) + len(fmt.Sprintf("（第 %d/%d 页）", n, n)) + len("\n\n")
		rest := max(MaxPageBytes-header, minPageBudget)
		first := max(rest-len(contents(n)), minPageBudget)
		chunks = splitSections(sections, first, rest)
		if len(chunks) <= n {
			break
		}
		n = len(chunks)
	}
	out := make(map[string]string, len(chunks))
	for i, c := range chunks {
		var b strings.Builder
		b.WriteString(title)
		if len(chunks) > 1 {
			fmt.Fprintf(&b, "（第 %d/%d 页）", i+1, len(chunks))
		}
		b.WriteString("\n\n")
		if i == 0 {
			b.WriteString(contents(len(chunks)))
		}
		if len(c) == 0 {
			b.WriteString("（无）\n")
		}
		for _, s := range c {
			b.WriteString(s)
		}
		out[name(i)] = b.String()
	}
	return out
}

// splitSections packs sections into chunks whose bodies fit first (page 1) or rest bytes.
func splitSections(sections []string, first, rest int) [][]string {
	var chunks [][]string
	var cur []string
	size := 0
	limit := func() int {
		if len(chunks) == 0 {
			return first
		}
		return rest
	}
	for _, s := range sections {
		if len(cur) > 0 && size+len(s) > limit() {
			chunks = append(chunks, cur)
			cur, size = nil, 0
		}
		if len(s) > limit() {
			s = truncateSection(s, limit())
		}
		cur = append(cur, s)
		size += len(s)
	}
	if len(cur) > 0 || len(chunks) == 0 {
		chunks = append(chunks, cur)
	}
	return chunks
}

func truncateSection(s string, limit int) string {
	cut := max(limit-len(truncatedMark), 0)
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + truncatedMark
}

var registerKinds = []struct{ kind, title string }{
	{RegTable, "数据表"}, {RegRoute, "HTTP 路由"}, {RegTopic, "MQ topic"}, {RegCacheKey, "缓存键前缀"},
}

var accessLabels = map[string]string{AccessRead: "读", AccessWrite: "写", AccessRef: "引用", AccessServe: "提供"}

// registerSections expects hits sorted by sortRegisters (writes first within a name).
// notes maps "kind:name" to the register's LLM usage note.
func registerSections(hits []RegisterHit, notes map[string]string) []string {
	var out []string
	for _, k := range registerKinds {
		byName := map[string][]RegisterHit{}
		var names []string
		for _, h := range hits {
			if h.Kind != k.kind {
				continue
			}
			if _, ok := byName[h.Name]; !ok {
				names = append(names, h.Name)
			}
			byName[h.Name] = append(byName[h.Name], h)
		}
		if len(names) == 0 {
			continue
		}
		sort.Strings(names)
		out = append(out, fmt.Sprintf("## %s（%d 个）\n\n", k.title, len(names)))
		for _, n := range names {
			hs := byName[n]
			var b strings.Builder
			fmt.Fprintf(&b, "### `%s`\n\n", n)
			if note := inlineClip(notes[k.kind+":"+n], registerNoteRunes); note != "" {
				fmt.Fprintf(&b, "用途：%s\n\n", note)
			}
			for i, h := range hs {
				if i == maxLocationsPerReg {
					fmt.Fprintf(&b, "- …另有 %d 处，用 rca_grep 搜索 `%s`\n", len(hs)-i, n)
					break
				}
				fmt.Fprintf(&b, "- %s `%s:%d`\n", accessLabels[h.Access], h.Path, h.Line)
			}
			b.WriteString("\n")
			out = append(out, b.String())
		}
	}
	return out
}

func areaSections(a area, f *Facts, v *llmView) []string {
	pkgs := map[string]GoPackage{}
	for _, p := range f.Packages {
		pkgs[p.Dir] = p
	}
	var out []string
	lastDir := ""
	for _, file := range a.Files {
		var b strings.Builder
		if dir := path.Dir(file.Path); dir != lastDir {
			lastDir = dir
			fmt.Fprintf(&b, "## `%s`", dir)
			if p, ok := pkgs[dir]; ok {
				fmt.Fprintf(&b, "（package %s）", p.Name)
				if p.Doc != "" {
					b.WriteString("\n\n" + p.Doc)
				}
			}
			b.WriteString("\n\n")
		}
		mark := ""
		if file.Test {
			mark = "，测试"
		}
		fmt.Fprintf(&b, "- `%s`（%s，%d 行%s）\n", file.Path, file.Lang, file.Lines, mark)
		card, stale := v.card(file.Path)
		var funcs map[string]string
		if purpose := cardPurpose(card); purpose != "" {
			if stale {
				fmt.Fprintf(&b, "  - 职责（已过期，以源码为准）：%s\n", purpose)
			} else {
				fmt.Fprintf(&b, "  - 职责：%s%s\n", purpose, roleSuffix(card.Role))
			}
		}
		if card != nil && !stale {
			funcs = map[string]string{}
			for _, fn := range card.Functions[:min(len(card.Functions), maxCardFuncs)] {
				funcs[fn.Name] = inlineClip(fn.Summary, cardFuncSummaryRunes)
			}
		}
		syms := f.Symbols[file.Path]
		for i, s := range syms {
			if i == maxSymbolsPerFile {
				fmt.Fprintf(&b, "  - …另有 %d 个符号\n", len(syms)-i)
				break
			}
			fmt.Fprintf(&b, "  - %s `%s` L%d-%d", s.Kind, s.Name, s.Line, s.EndLine)
			if sum := funcs[s.Name]; sum != "" {
				b.WriteString(" —— " + sum)
			}
			b.WriteString("\n")
		}
		out = append(out, b.String())
	}
	return out
}

// indexSections is the area table, split into standalone tables of indexTableRows rows,
// followed by one package list section per area. With LLM stages, the stage table and the
// unassigned and stale file lists come first.
func indexSections(areas []area, f *Facts, v *llmView) []string {
	pkgsByArea := map[string][]GoPackage{}
	for _, p := range f.Packages {
		n := areaOfDir(p.Dir)
		pkgsByArea[n] = append(pkgsByArea[n], p)
	}
	var out []string
	intro := "按目录分区。分区页列出文件和符号；表、路由、topic、缓存键见 `references/registers.md`。\n"
	if v.hasStages() {
		out = append(out, stageIndexSections(v)...)
		intro = "\n## 目录分区\n\n" + intro
	}
	out = append(out, intro)
	for i := 0; i < len(areas); i += indexTableRows {
		var b strings.Builder
		b.WriteString("\n| 分区 | 文件数 | 页面 |\n|---|---|---|\n")
		for _, a := range areas[i:min(i+indexTableRows, len(areas))] {
			fmt.Fprintf(&b, "| %s | %d | `references/areas/%s.md` |\n", a.Name, len(a.Files), a.ID)
		}
		out = append(out, b.String())
	}
	for _, a := range areas {
		pk := pkgsByArea[a.Name]
		if len(pk) == 0 {
			continue
		}
		var b strings.Builder
		fmt.Fprintf(&b, "\n## %s\n\n", a.Name)
		for i, p := range pk {
			if i == maxPackagesPerArea {
				fmt.Fprintf(&b, "- …另有 %d 个包，见分区页\n", len(pk)-i)
				break
			}
			fmt.Fprintf(&b, "- `%s`（package %s，%d 个文件）", p.Dir, p.Name, p.Files)
			if p.Doc != "" {
				b.WriteString("：" + p.Doc)
			}
			b.WriteString("\n")
		}
		out = append(out, b.String())
	}
	return out
}

func renderOverview(meta RenderMeta, f *Facts, areas []area, v *llmView) string {
	langs := map[string]int{}
	tests := 0
	for _, file := range f.Files {
		langs[file.Lang]++
		if file.Test {
			tests++
		}
	}
	source := "静态分析，无 LLM"
	if v != nil {
		source = fmt.Sprintf("静态分析 + LLM，文件卡片 %d 个", len(v.l.Cards))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s 概览\n\n", meta.RelPath)
	fmt.Fprintf(&b, "- commit `%s`，生成于 %s（%s）\n", shortCommit(meta.Commit), meta.GeneratedAt.UTC().Format("2006-01-02 15:04 UTC"), source)
	fmt.Fprintf(&b, "- 文件 %d 个（测试 %d），Go 包 %d 个，分区 %d 个\n", len(f.Files), tests, len(f.Packages), len(areas))
	if f.Module != nil && f.Module.Path != "" {
		fmt.Fprintf(&b, "- Go module `%s`\n", f.Module.Path)
	}
	if ov := v.overview(); ov != "" {
		fmt.Fprintf(&b, "\n## 系统总览（LLM 生成）\n\n%s\n", ov)
	}
	if v.hasStages() {
		b.WriteString("\n## 执行阶段\n\n")
		for _, st := range v.stages {
			fmt.Fprintf(&b, "- %s（`references/stages/%s.md`）", escapeLineStart(st.Title), st.ID)
			if lead := summaryLead(st.Summary, 80); lead != "" {
				b.WriteString("：" + lead)
			}
			b.WriteString("\n")
		}
	}
	b.WriteString("\n## 语言\n\n| 语言 | 文件数 |\n|---|---|\n")
	for _, l := range keysByCount(langs) {
		fmt.Fprintf(&b, "| %s | %d |\n", l, langs[l])
	}
	var mains []string
	for _, p := range f.Packages {
		if p.Main {
			mains = append(mains, p.Dir)
		}
	}
	if len(mains) > 0 {
		b.WriteString("\n## 入口（package main）\n\n")
		for i, m := range mains {
			if i == maxMains {
				fmt.Fprintf(&b, "- …另有 %d 个\n", len(mains)-i)
				break
			}
			fmt.Fprintf(&b, "- `%s`\n", m)
		}
	}
	if f.Module != nil && len(f.Module.Requires) > 0 {
		b.WriteString("\n## 直接依赖（go.mod）\n\n")
		for i, r := range f.Module.Requires {
			if i == maxRequires {
				fmt.Fprintf(&b, "- …另有 %d 个\n", len(f.Module.Requires)-i)
				break
			}
			fmt.Fprintf(&b, "- `%s`\n", r)
		}
	}
	names := map[string]map[string]bool{}
	for _, h := range f.Registers {
		if names[h.Kind] == nil {
			names[h.Kind] = map[string]bool{}
		}
		names[h.Kind][h.Name] = true
	}
	b.WriteString("\n## 寄存器\n\n")
	for _, k := range registerKinds {
		fmt.Fprintf(&b, "- %s %d 个\n", k.title, len(names[k.kind]))
	}
	b.WriteString("\n详见 `references/registers.md`。\n\n## 覆盖说明\n\n")
	c := f.Coverage
	fmt.Fprintf(&b, "- 跳过：大文件 %d，二进制 %d，生成代码 %d；不读 vendor、node_modules、隐藏目录等\n", c.SkippedLarge, c.SkippedBinary, c.SkippedGenerated)
	if c.Truncated {
		fmt.Fprintf(&b, "- 文件数超过 %d，清单已截断\n", MaxFiles)
	}
	if len(c.GoParseErrors) > 0 {
		fmt.Fprintf(&b, "- %d 个 Go 文件解析失败，未列符号\n", len(c.GoParseErrors))
	}
	b.WriteString("- 不含调用图；非 Go 文件只列清单和寄存器\n")
	if v != nil {
		b.WriteString("- LLM 内容（卡片、阶段、总览）可能落后于代码，以源码为准\n")
	}
	return b.String()
}

func keysByCount(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if m[keys[i]] != m[keys[j]] {
			return m[keys[i]] > m[keys[j]]
		}
		return keys[i] < keys[j]
	})
	return keys
}

func renderSkill(meta RenderMeta, hasStages bool) string {
	if hasStages {
		return renderStageSkill(meta)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "---\nname: %s\ndescription: >-\n  %s 的代码地图（静态生成，commit %s）：目录分区、Go 包与符号、数据表/路由/topic/缓存键的读写位置。RCA 定位时按需下钻，结论以 rca_read 读到的源码为准。\nhidden_from_summary: true\n---\n\n",
		SkillName(meta.RelPath), meta.RelPath, shortCommit(meta.Commit))
	fmt.Fprintf(&b, "# %s Handbook\n\n仓库逻辑名 `%s`（rca_* 工具的 repo 参数）。commit `%s`，生成于 %s。\n\n",
		meta.RelPath, meta.RelPath, shortCommit(meta.Commit), meta.GeneratedAt.UTC().Format("2006-01-02 15:04 UTC"))
	b.WriteString("## 文件\n\n" +
		"- `references/overview.md` —— 语言、规模、入口、直接依赖、覆盖说明。先读。\n" +
		"- `references/index.md` —— 目录分区及其中的 Go 包和包说明。\n" +
		"- `references/registers.md` —— 数据表、HTTP 路由、MQ topic、缓存键前缀的读写位置（path:line）。\n" +
		"- `references/areas/<分区>.md` —— 分区内每个文件的函数、方法、类型及行号。\n\n" +
		"页面过长时拆成续页（`*.p2.md` …），首页列出续页。用 `read_skill_file` 读取上述路径。\n\n")
	fmt.Fprintf(&b, "## 用法（RCA）\n\n"+
		"1. 读 overview 和 index，确定与现象相关的分区和寄存器，不要过早收窄到一个分区。\n"+
		"2. 涉及共享状态（表、缓存、topic、接口）时读 registers.md，记下所有写入点——异常状态往往在远处被写坏。\n"+
		"3. 打开相关分区页找候选文件和函数。\n"+
		"4. 用 `rca_read`（repo=`%s`）读真实源码确认。handbook 可能落后于代码，以源码为准；找不到时改用 `rca_grep`。\n", meta.RelPath)
	return b.String()
}

func renderStageSkill(meta RenderMeta) string {
	var b strings.Builder
	fmt.Fprintf(&b, "---\nname: %s\ndescription: >-\n  %s 的行为地图（commit %s）：按执行阶段组织文件卡片，并列出每个共享状态（表/路由/topic/缓存键）的全部读写位置。RCA 定位时按需下钻，结论以 rca_read 读到的源码为准。\nhidden_from_summary: true\n---\n\n",
		SkillName(meta.RelPath), meta.RelPath, shortCommit(meta.Commit))
	fmt.Fprintf(&b, "# %s Handbook\n\n仓库逻辑名 `%s`（rca_* 工具的 repo 参数）。commit `%s`，生成于 %s。\n\n",
		meta.RelPath, meta.RelPath, shortCommit(meta.Commit), meta.GeneratedAt.UTC().Format("2006-01-02 15:04 UTC"))
	b.WriteString("## 文件\n\n" +
		"- `references/overview.md` —— 系统总览：主流程、阶段划分、语言与规模、入口、外部依赖、覆盖说明。先读。\n" +
		"- `references/index.md` —— 每个阶段（做什么、文件数、页面），目录分区与 Go 包；列出未归类和已过期的文件。\n" +
		"- `references/registers.md` —— 数据表、HTTP 路由、MQ topic、缓存键前缀的用途与**全部**读写位置（path:line）。\n" +
		"- `references/stages/<id>.md` —— 阶段说明 + 每个文件的卡片（职责、执行时机、关键函数及行号）。\n" +
		"- `references/areas/<分区>.md` —— 按目录分区的完整文件与符号清单（含测试）。\n\n" +
		"页面过长时拆成续页（`*.p2.md` …），首页列出续页。用 `read_skill_file` 读取上述路径。\n\n")
	fmt.Fprintf(&b, "## 用法（RCA）\n\n"+
		"1. 读 overview 和 index，确定与现象相关的阶段与寄存器，不要过早收窄到单一阶段。\n"+
		"2. 涉及共享状态（表、缓存、topic、接口）时读 registers.md，记下所有写入点——异常状态往往在远处被写坏。\n"+
		"3. 打开相关 stages 页找候选文件和函数。\n"+
		"4. 对每个候选位置用 `rca_read`（repo=`%s`）读真实源码确认；已过期条目以源码为准，改用 `rca_grep`。\n"+
		"5. 结论只能基于读到的源码，不能基于 handbook 的描述。\n", meta.RelPath)
	return b.String()
}

const (
	maxIndexFileList = 100
	stageLeadRunes   = 80
	maxRoleRunes     = 20
	maxFuncNameRunes = 200
)

// llmView is the validated LLM content of one render; nil when the layer adds nothing.
type llmView struct {
	l          *LLMLayer
	stages     []Stage             // valid, distinct ids; single-line clipped titles, clipped summaries
	stageFiles map[string][]string // stage id -> card-eligible files of f, sorted
	unassigned []string            // card-eligible files of f without a valid stage, sorted
	files      map[string]File     // path -> card-eligible file of f
}

func newLLMView(f *Facts, l *LLMLayer) *llmView {
	if l.Empty() {
		return nil
	}
	v := &llmView{l: l, stageFiles: map[string][]string{}, files: map[string]File{}}
	sk := l.Skeleton
	if sk == nil {
		return v
	}
	v.stages = validStages(sk)
	if len(v.stages) == 0 {
		return v
	}
	valid := make(map[string]bool, len(v.stages))
	for _, s := range v.stages {
		valid[s.ID] = true
	}
	for _, file := range f.Files {
		if !CardEligible(file) {
			continue
		}
		v.files[file.Path] = file
		if id := sk.Files[file.Path].Stage; valid[id] {
			v.stageFiles[id] = append(v.stageFiles[id], file.Path)
		} else {
			v.unassigned = append(v.unassigned, file.Path)
		}
	}
	for _, files := range v.stageFiles {
		sort.Strings(files)
	}
	sort.Strings(v.unassigned)
	return v
}

// validStages returns the skeleton stages usable as page names: ids matching stageIDRe, first
// occurrence of each id kept.
func validStages(sk *Skeleton) []Stage {
	if sk == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []Stage
	for _, s := range sk.Stages {
		if !stageIDRe.MatchString(s.ID) || seen[s.ID] {
			continue
		}
		seen[s.ID] = true
		s.Title = inlineClip(firstNonEmpty(s.Title, s.ID), stageTitleRunes)
		s.Summary = clipRunes(s.Summary, stageSummaryRunes)
		out = append(out, s)
	}
	return out
}

func (v *llmView) hasStages() bool { return v != nil && len(v.stages) > 0 }

func (v *llmView) stageList() []Stage {
	if v == nil {
		return nil
	}
	return v.stages
}

// card returns the card shown for path: the current card, else the stale card (stale=true).
func (v *llmView) card(p string) (*Card, bool) {
	if v == nil {
		return nil, false
	}
	if c := v.l.Cards[p]; c != nil {
		return c, false
	}
	if c := v.l.Stale[p]; c != nil {
		return c, true
	}
	return nil, false
}

func (v *llmView) registerNotes() map[string]string {
	if v == nil || v.l.Skeleton == nil {
		return nil
	}
	return v.l.Skeleton.RegisterNotes
}

func (v *llmView) overview() string {
	if v == nil || v.l.Skeleton == nil {
		return ""
	}
	return markdownBlock(clipRunes(v.l.Skeleton.Overview, overviewRunes))
}

func cardPurpose(c *Card) string {
	if c == nil {
		return ""
	}
	return inlineClip(c.Purpose, cardPurposeRunes)
}

func roleSuffix(role string) string {
	if role = inlineClip(role, maxRoleRunes); role != "" {
		return "（" + role + "）"
	}
	return ""
}

// stageIndexSections is the stage table followed by the unassigned and stale file lists.
func stageIndexSections(v *llmView) []string {
	var b strings.Builder
	b.WriteString("## 执行阶段\n\n| 阶段 | 文件数 | 说明 | 页面 |\n|---|---|---|---|\n")
	for _, st := range v.stages {
		fmt.Fprintf(&b, "| %s | %d | %s | `references/stages/%s.md` |\n",
			tableCell(st.Title), len(v.stageFiles[st.ID]), tableCell(summaryLead(st.Summary, stageLeadRunes)), st.ID)
	}
	out := []string{b.String()}
	if s := fileListSection("未归类文件", v.unassigned); s != "" {
		out = append(out, s)
	}
	var stale []string
	for p := range v.l.Stale {
		stale = append(stale, p)
	}
	sort.Strings(stale)
	if s := fileListSection("已过期文件", stale); s != "" {
		out = append(out, s)
	}
	return out
}

func fileListSection(title string, files []string) string {
	if len(files) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\n## %s（%d 个）\n\n", title, len(files))
	for i, p := range files {
		if i == maxIndexFileList {
			fmt.Fprintf(&b, "- …共 %d 个，只列前 %d 个\n", len(files), maxIndexFileList)
			break
		}
		fmt.Fprintf(&b, "- `%s`\n", p)
	}
	return b.String()
}

// stageSections is the stage summary followed by one section per file.
func stageSections(st Stage, files []string, f *Facts, v *llmView) []string {
	summary := markdownBlock(st.Summary)
	if summary == "" {
		summary = "（暂无说明）"
	}
	out := []string{fmt.Sprintf("%s\n\n## 文件（%d 个）\n\n", summary, len(files))}
	for _, p := range files {
		file := v.files[p]
		var b strings.Builder
		fmt.Fprintf(&b, "### `%s`（%s，%d 行）", file.Path, file.Lang, file.Lines)
		card, stale := v.card(p)
		if stale {
			b.WriteString("（已过期，以源码为准，改用 rca_grep）")
		}
		b.WriteString("\n\n")
		if card == nil {
			b.WriteString("（暂无卡片）\n\n")
			out = append(out, b.String())
			continue
		}
		if purpose := cardPurpose(card); purpose != "" {
			fmt.Fprintf(&b, "职责：%s%s\n", purpose, roleSuffix(card.Role))
		}
		if d := inlineClip(card.Description, cardDescriptionRunes); d != "" {
			b.WriteString(escapeLineStart(d) + "\n")
		}
		if lc := inlineClip(card.Lifecycle, cardLifecycleRunes); lc != "" {
			fmt.Fprintf(&b, "执行时机：%s\n", lc)
		}
		if len(card.Functions) > 0 {
			lines := map[string]Symbol{}
			for _, s := range f.Symbols[p] {
				if _, ok := lines[s.Name]; !ok {
					lines[s.Name] = s
				}
			}
			b.WriteString("关键函数：\n")
			for _, fn := range card.Functions[:min(len(card.Functions), maxCardFuncs)] {
				name := strings.ReplaceAll(inlineClip(fn.Name, maxFuncNameRunes), "`", "")
				if name == "" {
					continue
				}
				fmt.Fprintf(&b, "- `%s`", name)
				if s, ok := lines[name]; ok {
					fmt.Fprintf(&b, " L%d-%d", s.Line, s.EndLine)
				}
				if sum := inlineClip(fn.Summary, cardFuncSummaryRunes); sum != "" {
					b.WriteString(" —— " + sum)
				}
				b.WriteString("\n")
			}
		}
		b.WriteString("\n")
		out = append(out, b.String())
	}
	return out
}

// summaryLead is the first sentence of s: cut after the first "。" or ". ", or at the first
// line break, then clipped to n runes.
func summaryLead(s string, n int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\r", "\n"))
	cut := len(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		cut = i
	}
	if i := strings.Index(s, "。"); i >= 0 && i+len("。") < cut {
		cut = i + len("。")
	}
	if i := strings.Index(s, ". "); i >= 0 && i+1 < cut {
		cut = i + 1
	}
	return clipRunes(collapseSpaces(s[:cut]), n)
}

// inlineText is LLM text reduced to one line.
func inlineText(s string) string { return collapseSpaces(s) }

// inlineClip is LLM text reduced to one line of at most n runes.
func inlineClip(s string, n int) string { return clipRunes(collapseSpaces(s), n) }

// tableCell is LLM text safe inside one Markdown table cell.
func tableCell(s string) string {
	return strings.ReplaceAll(inlineText(s), "|", `\|`)
}

var orderedListStartRe = regexp.MustCompile(`^(\d{1,9})([.)])`)

// escapeLineStart keeps a line of LLM text from starting a heading, quote, list, fence or
// HTML block.
func escapeLineStart(s string) string {
	if s != "" && strings.ContainsRune("#>-+*=|`~<", rune(s[0])) {
		return `\` + s
	}
	return orderedListStartRe.ReplaceAllString(s, `$1\$2`)
}

var (
	mdHeadingRe    = regexp.MustCompile(`^ {0,3}(#{1,6})(?:[ \t]|$)`)
	mdSetextRe     = regexp.MustCompile(`^ {0,3}(?:=+|-+)[ \t]*$`)
	mdFenceOpenRe  = regexp.MustCompile("^ {0,3}(?:(`{3,})[^`]*|(~{3,}).*)$")
	mdFenceCloseRe = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})[ \t]*$")
	mdHTMLRe       = regexp.MustCompile(`^ {0,3}<`)
)

// markdownBlock keeps multi-line LLM Markdown from breaking the page around it: headings are
// demoted below the page's own levels (to ####), setext underlines and HTML block starts are
// escaped, and an unclosed code fence is closed.
func markdownBlock(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n"))
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	fence := ""
	for i, ln := range lines {
		if fence != "" {
			if m := mdFenceCloseRe.FindStringSubmatch(ln); m != nil && m[1][0] == fence[0] && len(m[1]) >= len(fence) {
				fence = ""
			}
			continue
		}
		if m := mdFenceOpenRe.FindStringSubmatch(ln); m != nil {
			fence = m[1] + m[2]
			continue
		}
		if m := mdHeadingRe.FindStringSubmatchIndex(ln); m != nil {
			if level := m[3] - m[2]; level < 4 {
				lines[i] = ln[:m[2]] + strings.Repeat("#", 4-level) + ln[m[2]:]
			}
			continue
		}
		if mdSetextRe.MatchString(ln) || mdHTMLRe.MatchString(ln) {
			lines[i] = `\` + strings.TrimLeft(ln, " ")
		}
	}
	out := strings.Join(lines, "\n")
	if fence != "" {
		out += "\n" + fence
	}
	return out
}
