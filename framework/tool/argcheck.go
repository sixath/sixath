package tool

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// ArgCheck 是执行前的声明式参数规则。约定：
//   - (nil, nil)：通过，或规则不适用（未配置候选源、没有 mapper、候选集为空）；
//   - (nil, err)：规则本应判定但失败（拉取出错、超时），中间件跳过该规则并记 check_skipped，
//     若结果为 0 条则标 suspect（fail-open）。
type ArgCheck interface {
	Name() string
	Check(ctx context.Context, params map[string]any) ([]SchemaError, error)
}

const (
	suggestN        = 3
	listCandidatesN = 10
)

func trimmedStringParam(params map[string]any, key string) string {
	v, _ := params[key].(string)
	return strings.TrimSpace(v)
}

// OneOf 要求参数值在动态候选集中。参数缺失/为空时不判定（必填由 JSON Schema 负责）。
type OneOf struct {
	Param     string
	Source    func(ctx context.Context, params map[string]any) ([]string, error)
	Normalize func(params map[string]any, v string) string
	When      func(params map[string]any) bool
	Hint      string
}

func (c OneOf) Name() string { return "one_of:" + c.Param }

func (c OneOf) Check(ctx context.Context, params map[string]any) ([]SchemaError, error) {
	if c.When != nil && !c.When(params) {
		return nil, nil
	}
	v := trimmedStringParam(params, c.Param)
	if v == "" || c.Source == nil {
		return nil, nil
	}
	if c.Normalize != nil {
		v = c.Normalize(params, v)
	}
	cands, err := c.Source(ctx, params)
	if err != nil {
		return nil, err
	}
	if len(cands) == 0 {
		return nil, nil
	}
	for _, x := range cands {
		if x == v {
			return nil, nil
		}
	}
	sugg := Suggest(v, cands, suggestN)
	if len(sugg) == 0 {
		sorted := append([]string(nil), cands...)
		sort.Strings(sorted)
		if len(sorted) > listCandidatesN {
			sorted = sorted[:listCandidatesN]
		}
		sugg = sorted
	}
	return []SchemaError{{
		Path:       c.Param,
		Keyword:    KeywordOneOf,
		Message:    fmt.Sprintf("argument %q: unknown value %q", c.Param, v),
		Candidates: sugg,
		Hint:       c.Hint,
	}}, nil
}

// Pattern 校验参数格式。
type Pattern struct {
	Param string
	Regex *regexp.Regexp
	Hint  string
}

func (c Pattern) Name() string { return "pattern:" + c.Param }

func (c Pattern) Check(_ context.Context, params map[string]any) ([]SchemaError, error) {
	v := trimmedStringParam(params, c.Param)
	if v == "" || c.Regex == nil || c.Regex.MatchString(v) {
		return nil, nil
	}
	return []SchemaError{{
		Path:    c.Param,
		Keyword: KeywordPattern,
		Message: fmt.Sprintf("argument %q: invalid format %s", c.Param, describeValue(v)),
		Hint:    c.Hint,
	}}, nil
}

// Reject 在参数命中某类特征时拒绝调用，Redirect 告诉模型该用什么。
// Detect 返回 (参数路径, 原因, 是否命中)。
type Reject struct {
	Label    string
	Detect   func(params map[string]any) (path, reason string, hit bool)
	Redirect string
}

func (c Reject) Name() string { return "reject:" + c.Label }

func (c Reject) Check(_ context.Context, params map[string]any) ([]SchemaError, error) {
	if c.Detect == nil {
		return nil, nil
	}
	path, reason, hit := c.Detect(params)
	if !hit {
		return nil, nil
	}
	return []SchemaError{{
		Path:    path,
		Keyword: KeywordReject,
		Message: fmt.Sprintf("%s: %s", nameOf(path), reason),
		Hint:    c.Redirect,
	}}, nil
}

// FieldRef 是参数中引用的一个字段。
type FieldRef struct {
	Param string
	Field string
}

// FieldRefs 把参数中引用的字段与 mapping/表结构比对；发现未知字段时强制刷新一次再判定。
// Fields 返回空集合视为不适用；返回 error 视为规则失败。
type FieldRefs struct {
	Label    string
	Extract  func(params map[string]any) []FieldRef
	Fields   func(ctx context.Context, params map[string]any, refresh bool) ([]string, error)
	Known    func(field string, catalog []string) bool
	Similar  func(field string, catalog []string) []string
	BodyHint func(params map[string]any) string
}

func (c FieldRefs) Name() string { return "field_refs:" + c.Label }

func (c FieldRefs) Check(ctx context.Context, params map[string]any) ([]SchemaError, error) {
	if c.Extract == nil || c.Fields == nil {
		return nil, nil
	}
	refs := c.Extract(params)
	if len(refs) == 0 {
		return nil, nil
	}
	catalog, err := c.Fields(ctx, params, false)
	if err != nil {
		return nil, err
	}
	if len(catalog) == 0 {
		return nil, nil
	}
	unknown := c.unknown(refs, catalog)
	if len(unknown) == 0 {
		return nil, nil
	}
	if fresh, err := c.Fields(ctx, params, true); err == nil && len(fresh) > 0 {
		catalog = fresh
		unknown = c.unknown(refs, catalog)
		if len(unknown) == 0 {
			return nil, nil
		}
	}
	hint := ""
	if c.BodyHint != nil {
		hint = c.BodyHint(params)
	}
	errs := make([]SchemaError, 0, len(unknown))
	for _, r := range unknown {
		var cands []string
		if c.Similar != nil {
			cands = c.Similar(r.Field, catalog)
		} else {
			cands = Suggest(r.Field, catalog, suggestN)
		}
		if len(cands) > suggestN {
			cands = cands[:suggestN]
		}
		errs = append(errs, SchemaError{
			Path:       r.Param,
			Keyword:    KeywordUnknownField,
			Message:    fmt.Sprintf("argument %q references unknown field %q", r.Param, r.Field),
			Candidates: cands,
			Hint:       hint,
		})
	}
	return errs, nil
}

func (c FieldRefs) unknown(refs []FieldRef, catalog []string) []FieldRef {
	known := c.Known
	if known == nil {
		set := make(map[string]struct{}, len(catalog))
		for _, f := range catalog {
			set[f] = struct{}{}
		}
		known = func(f string, _ []string) bool { _, ok := set[f]; return ok }
	}
	var out []FieldRef
	seen := map[string]struct{}{}
	for _, r := range refs {
		if r.Field == "" || known(r.Field, catalog) {
			continue
		}
		key := r.Param + "\x00" + r.Field
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, r)
	}
	return out
}
