# [MW-B3] ContentSafety 改用 Aho-Corasick + 按 Role / Parts 区分

| 字段 | 值 |
|------|-----|
| **优先级** | P1 |
| **模块** | framework/middleware |
| **状态** | 已完成 |
| **关联报告** | [03-middleware.md B3](../03-middleware.md) |
| **预估工作量** | 1 天 |
| **依赖** | 无 |

## 问题位置

- `framework/middleware/content_safety.go: SimpleBlocklistFilter`

## 现状

```go
func (f *SimpleBlocklistFilter) CheckInput(text string) error {
    for _, w := range f.Blocked {
        if w == "" { continue }
        if strings.Contains(text, w) { return errs.ErrContentBlocked }
    }
    return nil
}
```

## 问题分析

1. **false positive 概率高**: 代码片段、错误日志原文、URL 路径误命中
2. **n*m 复杂度**: 每个 word 单独 Contains 一次
3. **不区分 Role**: `CheckOutput` 直接复用 `CheckInput`(详见 MW-C3)
4. **多模态 Parts.Text 没检查**: ContentSafety 当前只看 `m.Content`,Parts 内嵌的 caption / OCR 文本绕过
5. **Unicode / 中文边界感知差**: `strings.Contains` 不考虑词边界

## 改进方案

### Step 1 — Aho-Corasick 多模式匹配

```go
import "github.com/cloudflare/ahocorasick"

type AhoCorasickFilter struct {
    matcher *ahocorasick.Matcher
    blocked []string
}

func NewAhoCorasickFilter(words []string) *AhoCorasickFilter {
    return &AhoCorasickFilter{
        matcher: ahocorasick.NewStringMatcher(words),
        blocked: words,
    }
}

func (f *AhoCorasickFilter) CheckInput(text string) error {
    if hits := f.matcher.Match([]byte(text)); len(hits) > 0 {
        return errs.ErrContentBlocked
    }
    return nil
}
```

### Step 2 — 检查 Parts 内文本

```go
// content_safety.go: ContentSafetyMiddleware 改写
for _, m := range req.Messages {
    if m.Role != "user" { continue }
    if err := filter.CheckInput(m.Content); err != nil { return nil, errs.ErrContentBlocked }
    for _, p := range m.Parts {
        if p.Text != "" {
            if err := filter.CheckInput(p.Text); err != nil { return nil, errs.ErrContentBlocked }
        }
    }
}
```

### Step 3 — Filter 接口返回 `*Result` 而非 `error`

支持"命中 + 替换"语义(如把敏感词替换为 `***` 后放行):

```go
type FilterDecision int
const (
    FilterAllow FilterDecision = iota
    FilterRedact   // 替换敏感词
    FilterBlock
)

type FilterResult struct {
    Decision FilterDecision
    HitWords []string  // 不外发,仅本地日志/审计
    Redacted string    // Redact 时填充
}

type ContentFilter interface {
    Check(role string, text string) FilterResult
}
```

旧 `CheckInput / CheckOutput` 保留兼容,内部调用 `Check`。

## 验收标准

- [ ] `AhoCorasickFilter` 提供,基准测试性能优于 `SimpleBlocklistFilter` 至少 5 倍
- [ ] `ContentSafetyMiddleware` 同时检查 `Content` 与 `Parts.Text`
- [ ] 新 `Check` 接口与 `FilterDecision` 引入,旧接口保留兼容
- [ ] AC 自动机加载时间 < 100ms(10 万词级)

## 测试要求

- `TestAhoCorasickFilter_BasicMatch`: 单词 / 多词 / 多次命中
- `TestAhoCorasickFilter_NoMatch`
- `TestContentSafetyMiddleware_PartsCheck`: Parts 内含 blocked word 也被拦截
- `BenchmarkAhoCorasick_vs_Contains`: 1 万词,10KB 文本,对比 ns/op

## 风险

- 引入 `cloudflare/ahocorasick` 是新依赖
- 替换语义(Redact)对下游模型行为是新概念,文档需说明
- AC 自动机不支持词边界,要按需配合预处理(如分词后匹配)

## 关联 issue

- [MW-C3](MW-C3-filter-input-output-asymmetry.md): 输入/输出非对称实现
