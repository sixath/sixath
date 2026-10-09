package handbook

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"
)

// MaxPageBytes keeps each page small enough to read in one read_skill_file call.
const MaxPageBytes = 48 << 10

const (
	maxSymbolsPerFile  = 60
	maxPackagesPerArea = 50
	maxLocationsPerReg = 30
	maxRequires        = 40
)

// RenderMeta identifies the repository and commit a handbook was built from.
type RenderMeta struct {
	RelPath     string
	Commit      string
	GeneratedAt time.Time
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func slug(s string) string {
	s = strings.Trim(slugRe.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if len(s) > 60 {
		s = strings.TrimRight(s[:60], "-")
	}
	if s == "" {
		s = "root"
	}
	return s
}

// SkillName is the handbook skill name for a repository logical name (rel_path).
func SkillName(relPath string) string { return "handbook-" + slug(relPath) }

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
	used := map[string]int{}
	out := make([]area, 0, len(names))
	for _, n := range names {
		a := byName[n]
		id := slug(n)
		used[id]++
		if used[id] > 1 {
			id = fmt.Sprintf("%s-%d", id, used[id])
		}
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

// Render returns the skill pages keyed by path relative to the skill directory.
func Render(meta RenderMeta, f *Facts) map[string]string {
	areas := buildAreas(f)
	pages := map[string]string{}
	for p, c := range paginate("references/registers", "# "+meta.RelPath+" 寄存器", registerSections(f.Registers)) {
		pages[p] = c
	}
	for _, a := range areas {
		for p, c := range paginate("references/areas/"+a.ID, "# 分区 "+a.Name, areaSections(a, f)) {
			pages[p] = c
		}
	}
	pages["references/index.md"] = renderIndex(meta, areas, f)
	pages["references/overview.md"] = renderOverview(meta, f, areas)
	pages["SKILL.md"] = renderSkill(meta)
	return pages
}

// paginate splits sections into pages of at most MaxPageBytes without splitting a section.
// The first page is <base>.md and lists continuation pages <base>.p2.md, <base>.p3.md, ...
func paginate(base, title string, sections []string) map[string]string {
	var chunks [][]string
	var cur []string
	size := 0
	for _, s := range sections {
		if len(cur) > 0 && size+len(s) > MaxPageBytes {
			chunks = append(chunks, cur)
			cur, size = nil, 0
		}
		cur = append(cur, s)
		size += len(s)
	}
	if len(cur) > 0 || len(chunks) == 0 {
		chunks = append(chunks, cur)
	}
	name := func(i int) string {
		if i == 0 {
			return base + ".md"
		}
		return fmt.Sprintf("%s.p%d.md", base, i+1)
	}
	out := make(map[string]string, len(chunks))
	for i, c := range chunks {
		var b strings.Builder
		b.WriteString(title)
		if len(chunks) > 1 {
			fmt.Fprintf(&b, "（第 %d/%d 页）", i+1, len(chunks))
		}
		b.WriteString("\n\n")
		if i == 0 && len(chunks) > 1 {
			b.WriteString("续页：")
			for j := 1; j < len(chunks); j++ {
				if j > 1 {
					b.WriteString("、")
				}
				fmt.Fprintf(&b, "`%s`", name(j))
			}
			b.WriteString("\n\n")
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

var registerKinds = []struct{ kind, title string }{
	{RegTable, "数据表"}, {RegRoute, "HTTP 路由"}, {RegTopic, "MQ topic"}, {RegCacheKey, "缓存键前缀"},
}

var accessLabels = map[string]string{AccessRead: "读", AccessWrite: "写", AccessRef: "引用", AccessServe: "提供"}

// registerSections expects hits sorted by sortRegisters (writes first within a name).
func registerSections(hits []RegisterHit) []string {
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

func areaSections(a area, f *Facts) []string {
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
		syms := f.Symbols[file.Path]
		for i, s := range syms {
			if i == maxSymbolsPerFile {
				fmt.Fprintf(&b, "  - …另有 %d 个符号\n", len(syms)-i)
				break
			}
			fmt.Fprintf(&b, "  - %s `%s` L%d-%d\n", s.Kind, s.Name, s.Line, s.EndLine)
		}
		out = append(out, b.String())
	}
	return out
}

func renderIndex(meta RenderMeta, areas []area, f *Facts) string {
	pkgsByArea := map[string][]GoPackage{}
	for _, p := range f.Packages {
		n := areaOfDir(p.Dir)
		pkgsByArea[n] = append(pkgsByArea[n], p)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s 分区索引\n\n按目录分区。分区页列出文件和符号；表、路由、topic、缓存键见 `references/registers.md`。\n\n", meta.RelPath)
	b.WriteString("| 分区 | 文件数 | 页面 |\n|---|---|---|\n")
	for _, a := range areas {
		fmt.Fprintf(&b, "| %s | %d | `references/areas/%s.md` |\n", a.Name, len(a.Files), a.ID)
	}
	for _, a := range areas {
		pk := pkgsByArea[a.Name]
		if len(pk) == 0 {
			continue
		}
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
	}
	return b.String()
}

func renderOverview(meta RenderMeta, f *Facts, areas []area) string {
	langs := map[string]int{}
	tests := 0
	for _, file := range f.Files {
		langs[file.Lang]++
		if file.Test {
			tests++
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s 概览\n\n", meta.RelPath)
	fmt.Fprintf(&b, "- commit `%s`，生成于 %s（静态分析，无 LLM）\n", shortCommit(meta.Commit), meta.GeneratedAt.Format("2006-01-02 15:04"))
	fmt.Fprintf(&b, "- 文件 %d 个（测试 %d），Go 包 %d 个，分区 %d 个\n", len(f.Files), tests, len(f.Packages), len(areas))
	if f.Module != nil && f.Module.Path != "" {
		fmt.Fprintf(&b, "- Go module `%s`\n", f.Module.Path)
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
		for _, m := range mains {
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

func renderSkill(meta RenderMeta) string {
	var b strings.Builder
	fmt.Fprintf(&b, "---\nname: %s\ndescription: >-\n  %s 的代码地图（静态生成，commit %s）：目录分区、Go 包与符号、数据表/路由/topic/缓存键的读写位置。RCA 定位时按需下钻，结论以 rca_read 读到的源码为准。\nhidden_from_summary: true\n---\n\n",
		SkillName(meta.RelPath), meta.RelPath, shortCommit(meta.Commit))
	fmt.Fprintf(&b, "# %s Handbook\n\n仓库逻辑名 `%s`（rca_* 工具的 repo 参数）。commit `%s`，生成于 %s。\n\n",
		meta.RelPath, meta.RelPath, shortCommit(meta.Commit), meta.GeneratedAt.Format("2006-01-02 15:04"))
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
