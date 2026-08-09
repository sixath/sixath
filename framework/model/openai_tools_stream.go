package model

import (
	"context"
	"errors"
	"io"
	"sort"
	"strings"

	openai "github.com/sashabaranov/go-openai"

	"github.com/sixath/framework/tool"
)

type accumulatedToolCall struct {
	ID        string
	Name      string
	Arguments string
}

// ChatWithToolsStream 流式执行带工具的对话。
func (c *OpenAIClient) ChatWithToolsStream(ctx context.Context, messages []Message, reg *tool.Registry, opts ...Option) (<-chan string, <-chan *Generation, error) {
	if len(messages) == 0 {
		return nil, nil, errors.New("messages is empty")
	}
	if reg == nil {
		return nil, nil, errors.New("tool registry is nil")
	}

	callCfg := ApplyOptions(opts...)
	modelName := c.model
	if callCfg.ModelName != "" {
		modelName = callCfg.ModelName
	}

	msgs := PrepareChatContextCtx(ctx, messages, callCfg)

	deferActive, _ := ctx.Value(tool.ContextKeyToolSearchActive).(bool)
	tools := reg.ListForAPIWithDefer(ctx, nil, deferActive)
	if len(tools) == 0 {
		return nil, nil, errors.New("no tools registered")
	}

	req := openai.ChatCompletionRequest{
		Model:       modelName,
		Messages:    make([]openai.ChatCompletionMessage, 0, len(msgs)),
		Tools:       make([]openai.Tool, 0, len(tools)),
		ToolChoice:  "auto",
		Temperature: float32(callCfg.Temperature),
		MaxTokens:   callCfg.MaxTokens,
	}

	for _, m := range msgs {
		msg, err := openAIChatMessage(m)
		if err != nil {
			return nil, nil, err
		}
		req.Messages = append(req.Messages, msg)
	}
	for _, tl := range tools {
		req.Tools = append(req.Tools, openai.Tool{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        tl.Name,
				Description: tl.Description,
				Parameters:  tl.Parameters,
			},
		})
	}

	stream, err := c.client.CreateChatCompletionStream(ctx, req)
	if err != nil {
		return nil, nil, err
	}

	ch := make(chan string)
	genCh := make(chan *Generation, 1)

	go func() {
		defer close(ch)
		defer close(genCh)
		defer stream.Close()

		sendFail := func(err error) {
			if err == nil {
				err = errors.New("stream failed")
			}
			select {
			case genCh <- &Generation{Err: err}:
			default:
			}
		}

		var contentAccum strings.Builder
		var reasoningAccum strings.Builder
		toolCallsAccum := make(map[int]*accumulatedToolCall)

		for {
			select {
			case <-ctx.Done():
				sendFail(ctx.Err())
				return
			default:
			}

			resp, err := stream.Recv()
			if err != nil {
				if err != io.EOF {
					// 旧逻辑直接 return 会空关闭 genCh，ReAct 误报 "missing streamed generation"。
					sendFail(err)
					return
				}
				// EOF：根据累积结果构造 finalGen
				if len(toolCallsAccum) > 0 {
					gen := c.buildToolCallGeneration(ctx, toolCallsAccum, reg, reasoningAccum.String())
					if gen != nil {
						genCh <- gen
					} else {
						sendFail(errors.New("incomplete tool call in stream"))
					}
				} else {
					genCh <- &Generation{
						Text: contentAccum.String(),
						Raw: ToolStep{
							Used:             false,
							ReasoningContent: reasoningAccum.String(),
						},
					}
				}
				return
			}

			if len(resp.Choices) == 0 {
				continue
			}

			choice := resp.Choices[0]

			if choice.Delta.ReasoningContent != "" {
				reasoningAccum.WriteString(choice.Delta.ReasoningContent)
			}

			if choice.Delta.Content != "" {
				contentAccum.WriteString(choice.Delta.Content)
				select {
				case ch <- choice.Delta.Content:
				case <-ctx.Done():
					sendFail(ctx.Err())
					return
				}
			}

			for _, tc := range choice.Delta.ToolCalls {
				idx := 0
				if tc.Index != nil {
					idx = *tc.Index
				}
				if toolCallsAccum[idx] == nil {
					toolCallsAccum[idx] = &accumulatedToolCall{}
				}
				acc := toolCallsAccum[idx]
				if tc.ID != "" {
					acc.ID = tc.ID
				}
				if tc.Function.Name != "" {
					acc.Name = tc.Function.Name
				}
				if tc.Function.Arguments != "" {
					acc.Arguments += tc.Function.Arguments
				}
			}

			switch choice.FinishReason {
			case openai.FinishReasonStop, openai.FinishReasonLength, openai.FinishReasonContentFilter:
				genCh <- &Generation{
					Text: contentAccum.String(),
					Raw: ToolStep{
						Used:             false,
						ReasoningContent: reasoningAccum.String(),
					},
				}
				return
			case openai.FinishReasonToolCalls:
				gen := c.buildToolCallGeneration(ctx, toolCallsAccum, reg, reasoningAccum.String())
				if gen != nil {
					genCh <- gen
				} else {
					sendFail(errors.New("incomplete tool call in stream"))
				}
				return
			}
			// FinishReason 为空（""）或 "null" 表示本 chunk 非终止帧，继续读取后续增量；
			// 真正的终止由上述具名 finish reason 或流 EOF（见上方 stream.Recv 分支）驱动。
		}
	}()

	return ch, genCh, nil
}

func (c *OpenAIClient) buildToolCallGeneration(ctx context.Context, toolCallsAccum map[int]*accumulatedToolCall, reg *tool.Registry, reasoningContent string) *Generation {
	_ = ctx
	var indexes []int
	for idx, acc := range toolCallsAccum {
		if acc != nil && acc.Name != "" {
			indexes = append(indexes, idx)
		}
	}
	sort.Ints(indexes)
	if len(indexes) == 0 {
		return &Generation{
			Text: "error: incomplete tool call in stream",
			Raw:  ToolStep{Used: true, Observation: map[string]string{"error": "incomplete tool call"}},
		}
	}

	calls := make([]ToolCall, 0, len(indexes))
	for _, idx := range indexes {
		acc := toolCallsAccum[idx]
		if _, ok := reg.Get(acc.Name); !ok {
			return &Generation{
				Text: "error: tool not found: " + acc.Name,
				Raw:  ToolStep{Used: true, ToolName: acc.Name, Observation: map[string]string{"error": "tool not found"}},
			}
		}
		params, repaired, parseErr := parseToolArguments(acc.Arguments)
		calls = append(calls, ToolCall{
			ID:                     acc.ID,
			Name:                   acc.Name,
			Arguments:              params,
			ArgumentsRepaired:      repaired,
			RawArgumentsPreview:    rawArgumentsPreview(acc.Arguments),
			RawArgumentsParseError: parseErr,
		})
	}
	first := calls[0]

	return &Generation{
		Raw: ToolStep{
			Used:             true,
			ToolCallID:       first.ID,
			ToolName:         first.Name,
			Arguments:        first.Arguments,
			ToolCalls:        calls,
			ReasoningContent: reasoningContent,
		},
	}
}
