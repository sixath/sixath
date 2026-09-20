package chat

import "strings"

// price 为每 1M token 的美元单价（input / output）。
type price struct {
	in  float64
	out float64
}

// defaultPrices 是默认计价表（USD / 1M tokens）。这些是**近似公开定价**，且价格会
// 随厂商调整而变化；运维应按实际接入的模型与计费口径覆盖此表（改这里即可，无需改逻辑）。
var defaultPrices = map[string]price{
	"gpt-4o":       {in: 2.50, out: 10.00},
	"gpt-4o-mini":  {in: 0.15, out: 0.60},
	"gpt-4.1":      {in: 2.00, out: 8.00},
	"gpt-4.1-mini": {in: 0.40, out: 1.60},
	"qwen-max":     {in: 2.40, out: 9.60},
	"qwen-plus":    {in: 0.80, out: 2.00},
}

// defaultPrice 未命中计价表时的兜底粗估（避免成本字段恒为 0）。
var defaultPrice = price{in: 1.00, out: 4.00}

// EstimateCost 按模型名 + token 用量估算成本（美元）。
// 未匹配模型时用兜底价；返回 0 表示无用量（token 全为 0）。
func EstimateCost(model string, inputTokens, outputTokens int) float64 {
	if inputTokens <= 0 && outputTokens <= 0 {
		return 0
	}
	p, ok := defaultPrices[strings.ToLower(strings.TrimSpace(model))]
	if !ok {
		p = defaultPrice
	}
	return (float64(inputTokens)*p.in + float64(outputTokens)*p.out) / 1_000_000
}
