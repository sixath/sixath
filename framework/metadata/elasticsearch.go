package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"github.com/sixath/framework/datasource"
)

// 日期后缀：索引名形如 base-2026.01.02 或 base.2026-01-02，将 base 视为模式前缀，模式名为 base-*。
var esDateSuffixRegex = regexp.MustCompile(`^(.+)[.-](\d{4}[.-]\d{2}[.-]\d{2})$`)

// esIndexNameAllowed 为 true 时表示索引名不含 %, {, [ 等易导致 URL/API 问题的字符，可参与分组与 mapping 拉取。
func esIndexNameAllowed(name string) bool {
	return !strings.ContainsAny(name, "%{[]")
}

// esIndexGroup 表示一个索引模式下的分组：模式名、代表索引、该组全部索引、时间约定说明。
type esIndexGroup struct {
	Pattern        string   // 如 vm-manager-*
	Representative string   // 用于拉 mapping 的代表索引
	Indices        []string // 该组下所有索引（用于 Comment 示例与 IndexToPattern）
	TimeComment    string   // 时间序列约定说明
}

// groupIndicesByPattern 将索引名列表按「日期后缀」规则分组；无法归组的单独成组（模式名=索引名）。
func groupIndicesByPattern(indexNames []string) []*esIndexGroup {
	type key struct {
		base string
		date string // 用于同组内选代表索引（取最大 date）
	}
	groups := make(map[string]*esIndexGroup)
	for _, idx := range indexNames {
		m := esDateSuffixRegex.FindStringSubmatch(idx)
		if len(m) >= 3 {
			base := m[1]
			pattern := base + "-*"
			g, ok := groups[pattern]
			if !ok {
				g = &esIndexGroup{
					Pattern:        pattern,
					Representative: idx,
					Indices:        []string{idx},
					TimeComment:    fmt.Sprintf("时间序列：按日滚动，索引后缀为 YYYY.MM.DD。查询某日数据请使用索引 %s-YYYY.MM.DD 或 pattern %s，并在 query 中限定时间范围。", base, pattern),
				}
				groups[pattern] = g
			} else {
				g.Indices = append(g.Indices, idx)
			}
		} else {
			// 无法归组：单索引成组
			groups[idx] = &esIndexGroup{
				Pattern:        idx,
				Representative: idx,
				Indices:        []string{idx},
			}
		}
	}
	// 对日期组按代表索引的日期排序，取最新为 representative
	for _, g := range groups {
		if len(g.Indices) > 1 && g.TimeComment != "" {
			sort.Slice(g.Indices, func(i, j int) bool {
				di := ""
				if m := esDateSuffixRegex.FindStringSubmatch(g.Indices[i]); len(m) >= 3 {
					di = m[2]
				}
				dj := ""
				if m := esDateSuffixRegex.FindStringSubmatch(g.Indices[j]); len(m) >= 3 {
					dj = m[2]
				}
				return di > dj // 降序，第一个为最新
			})
			g.Representative = g.Indices[0]
		}
	}
	out := make([]*esIndexGroup, 0, len(groups))
	for _, g := range groups {
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Pattern < out[j].Pattern })
	return out
}

// FetchSchemaElasticsearch 从 ES 拉取索引列表并按「索引模式」分组，每个模式只对代表索引拉一次 mapping，
// 得到逻辑表（Table.Name = 模式名，Table.Comment = 示例索引 + 时间约定），减少请求数与重复 mapping。
func FetchSchemaElasticsearch(ctx context.Context, client *datasource.ESHTTP) (*Schema, error) {
	if client == nil {
		return nil, fmt.Errorf("metadata: elasticsearch client is nil")
	}

	status, body, err := client.Do(ctx, http.MethodGet, "/_cat/indices?format=json", nil)
	if err != nil {
		return nil, fmt.Errorf("metadata: cat indices: %w", err)
	}
	if status >= 400 {
		return nil, fmt.Errorf("metadata: cat indices: HTTP %d %s", status, strings.TrimSpace(string(body)))
	}

	var catRows []struct {
		Index string `json:"index"`
	}
	if err := json.Unmarshal(body, &catRows); err != nil {
		return nil, fmt.Errorf("metadata: decode cat indices: %w", err)
	}

	var indexNames []string
	for _, row := range catRows {
		if row.Index != "" && !strings.HasPrefix(row.Index, ".") && esIndexNameAllowed(row.Index) {
			indexNames = append(indexNames, row.Index)
		}
	}

	schema := &Schema{Name: "elasticsearch"}
	if len(indexNames) == 0 {
		return schema, nil
	}

	groups := groupIndicesByPattern(indexNames)
	indexToPattern := make(map[string]string)
	for _, g := range groups {
		for _, idx := range g.Indices {
			indexToPattern[idx] = g.Pattern
		}
	}
	schema.IndexToPattern = indexToPattern

	for _, g := range groups {
		tbl := Table{Name: g.Pattern}
		var parts []string
		n := len(g.Indices)
		if n > 3 {
			n = 3
		}
		for i := 0; i < n; i++ {
			parts = append(parts, g.Indices[i])
		}
		tbl.Comment = "示例索引: " + strings.Join(parts, ", ")
		if g.TimeComment != "" {
			tbl.Comment += "。 " + g.TimeComment
		}

		mapPath := "/" + g.Representative + "/_mapping"
		st, mapBody, err := client.Do(ctx, http.MethodGet, mapPath, nil)
		if err != nil {
			return nil, fmt.Errorf("metadata: get mapping for index %q: %w", g.Representative, err)
		}
		if st >= 400 {
			return nil, fmt.Errorf("metadata: get mapping for index %q: HTTP %d %s", g.Representative, st, strings.TrimSpace(string(mapBody)))
		}
		var raw map[string]any
		if err := json.Unmarshal(mapBody, &raw); err != nil {
			return nil, fmt.Errorf("metadata: decode mapping for index %q: %w", g.Representative, err)
		}
		entry, _ := raw[g.Representative].(map[string]any)
		if entry == nil && len(raw) == 1 {
			for _, v := range raw {
				entry, _ = v.(map[string]any)
				break
			}
		}
		props := mappingProperties(entry)
		for name := range props {
			tbl.Columns = append(tbl.Columns, Column{
				Name:       name,
				Type:       "keyword",
				IsNullable: true,
			})
		}
		schema.Tables = append(schema.Tables, tbl)
	}
	return schema, nil
}

func mappingProperties(indexEntry map[string]any) map[string]any {
	if indexEntry == nil {
		return nil
	}
	mappings, _ := indexEntry["mappings"].(map[string]any)
	if mappings == nil {
		return nil
	}
	if props, ok := mappings["properties"].(map[string]any); ok {
		return props
	}
	for _, inner := range mappings {
		m, ok := inner.(map[string]any)
		if !ok {
			continue
		}
		if props, ok := m["properties"].(map[string]any); ok {
			return props
		}
	}
	return nil
}
