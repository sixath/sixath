package handbook

import (
	"regexp"
	"sort"
	"strings"
)

const (
	RegTable    = "table"
	RegRoute    = "route"
	RegTopic    = "topic"
	RegCacheKey = "cache_key"

	AccessWrite = "write"
	AccessServe = "serve"
	AccessRead  = "read"
	AccessRef   = "ref"
)

// RegisterHit is one literal reference to shared state (table, route, topic, cache key).
type RegisterHit struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Access string `json:"access"`
	Path   string `json:"path"`
	Line   int    `json:"line"`
}

type sqlPatterns struct{ insert, update, del, read *regexp.Regexp }

const sqlIdent = "`?([A-Za-z_][A-Za-z0-9_.]*)`?"

func compileSQL(flags string) sqlPatterns {
	return sqlPatterns{
		insert: regexp.MustCompile(flags + `\bINSERT\s+(?:IGNORE\s+)?INTO\s+` + sqlIdent),
		update: regexp.MustCompile(flags + `\bUPDATE\s+` + sqlIdent + `\s+SET\b`),
		del:    regexp.MustCompile(flags + `\bDELETE\s+FROM\s+` + sqlIdent),
		read:   regexp.MustCompile(flags + `\b(?:FROM|JOIN)\s+` + sqlIdent),
	}
}

var (
	// Source code only matches upper-case SQL keywords; English "from" in strings is common.
	sqlUpper   = compileSQL("")
	sqlAnyCase = compileSQL("(?i)")

	gormTableRe  = regexp.MustCompile(`\.Table\(\s*"([A-Za-z_][A-Za-z0-9_]*)"`)
	routeRe      = regexp.MustCompile(`\.(?:GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS|Any|Handle|HandleFunc)\(\s*"(/[^"]*)"`)
	protoRouteRe = regexp.MustCompile(`\b(?:get|post|put|delete|patch)\s*:\s*"(/[^"]+)"`)
	topicRe      = regexp.MustCompile(`(?i)\b\w*topic\w*"?\s*(?::=|=|:)\s*"([A-Za-z0-9][\w.\-]*)"`)
	yamlTopicRe  = regexp.MustCompile(`(?i)^\s*\w*topic\w*\s*:\s*["']?([A-Za-z0-9][\w.\-]*)["']?\s*$`)
	cacheKeyRe   = regexp.MustCompile(`"([A-Za-z][\w\-]*:[\w\-:%{}]*)"`)
	cacheCallRe  = regexp.MustCompile(`\.(\w+)\(`)
	cacheCtxRe   = regexp.MustCompile(`(?i)redis|cache|rdb|\bkey`)
)

var codeLangs = setOf("go", "java", "python", "javascript", "typescript", "php", "ruby", "kotlin", "csharp", "rust", "lua", "c", "cpp", "scala")

var cacheWrites = setOf("Set", "SetNX", "SetEX", "SetEx", "MSet", "Del", "Unlink", "HSet", "HMSet", "HDel", "HIncrBy",
	"Incr", "IncrBy", "Decr", "DecrBy", "Expire", "ExpireAt", "LPush", "RPush", "LPop", "RPop", "SAdd", "SRem",
	"ZAdd", "ZRem", "ZIncrBy", "Append", "GetSet", "GetDel")

var cacheReads = setOf("Get", "MGet", "HGet", "HGetAll", "HMGet", "HExists", "Exists", "TTL", "PTTL", "LRange", "LLen",
	"SMembers", "SIsMember", "SCard", "ZRange", "ZRangeByScore", "ZRevRange", "ZScore", "ZCard", "Scan", "Keys")

func setOf(items ...string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, s := range items {
		m[s] = true
	}
	return m
}

func scanRegisters(rel, lang string, src []byte) []RegisterHit {
	var out []RegisterHit
	for i, line := range strings.Split(string(src), "\n") {
		n := i + 1
		add := func(kind, name, access string) {
			out = append(out, RegisterHit{Kind: kind, Name: name, Access: access, Path: rel, Line: n})
		}
		switch {
		case lang == "sql":
			scanSQL(sqlAnyCase, line, add)
		case lang == "proto":
			for _, m := range protoRouteRe.FindAllStringSubmatch(line, -1) {
				add(RegRoute, m[1], AccessServe)
			}
		case lang == "yaml":
			if m := yamlTopicRe.FindStringSubmatch(line); m != nil {
				add(RegTopic, m[1], AccessRef)
			}
		case codeLangs[lang]:
			scanSQL(sqlUpper, line, add)
			for _, m := range gormTableRe.FindAllStringSubmatch(line, -1) {
				add(RegTable, m[1], AccessRef)
			}
			for _, m := range routeRe.FindAllStringSubmatch(line, -1) {
				add(RegRoute, m[1], AccessServe)
			}
			for _, m := range topicRe.FindAllStringSubmatch(line, -1) {
				add(RegTopic, m[1], AccessRef)
			}
			scanCacheKeys(line, add)
		}
	}
	return out
}

// scanSQL records writes first and blanks them out so "DELETE FROM t" is not also a read.
func scanSQL(p sqlPatterns, line string, add func(kind, name, access string)) {
	for _, re := range []*regexp.Regexp{p.insert, p.update, p.del} {
		for _, m := range re.FindAllStringSubmatch(line, -1) {
			add(RegTable, m[1], AccessWrite)
		}
		line = re.ReplaceAllString(line, " ")
	}
	for _, m := range p.read.FindAllStringSubmatch(line, -1) {
		add(RegTable, m[1], AccessRead)
	}
}

func scanCacheKeys(line string, add func(kind, name, access string)) {
	keys := cacheKeyRe.FindAllStringSubmatch(line, -1)
	if len(keys) == 0 || !cacheCtxRe.MatchString(line) {
		return
	}
	access := AccessRef
	for _, m := range cacheCallRe.FindAllStringSubmatch(line, -1) {
		if cacheWrites[m[1]] {
			access = AccessWrite
			break
		}
		if cacheReads[m[1]] {
			access = AccessRead
		}
	}
	for _, m := range keys {
		name := m[1]
		if i := strings.IndexAny(name, "%{"); i >= 0 {
			name = name[:i]
		}
		if strings.Contains(name, ":") {
			add(RegCacheKey, name, access)
		}
	}
}

var accessRank = map[string]int{AccessWrite: 0, AccessServe: 1, AccessRead: 2, AccessRef: 3}

// sortRegisters orders by kind and name, then writes before reads so renderers keep writers.
func sortRegisters(hs []RegisterHit) {
	sort.Slice(hs, func(i, j int) bool {
		a, b := hs[i], hs[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		if accessRank[a.Access] != accessRank[b.Access] {
			return accessRank[a.Access] < accessRank[b.Access]
		}
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		return a.Line < b.Line
	})
}

func dedupeRegisters(hs []RegisterHit) []RegisterHit {
	seen := make(map[RegisterHit]bool, len(hs))
	out := hs[:0]
	for _, h := range hs {
		if !seen[h] {
			seen[h] = true
			out = append(out, h)
		}
	}
	return out
}
