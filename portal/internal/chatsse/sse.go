// Package chatsse holds shared chat SSE helpers used by Portal chat routes and Runtime turns.
package chatsse

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	portalchat "backend/internal/chat"
	"backend/internal/service"
)

// SetHeaders writes standard text/event-stream response headers and status 200.
func SetHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flush(w)
}

// WriteEvent emits one SSE event with JSON data payload.
func WriteEvent(w http.ResponseWriter, event string, data map[string]any) {
	_, _ = w.Write([]byte("event: " + event + "\n"))
	body, _ := json.Marshal(data)
	_, _ = w.Write([]byte("data: " + string(body) + "\n\n"))
}

// PersistAssistant saves the aggregated assistant message after a successful stream.
type PersistAssistant func(ctx context.Context, sessionID, content string, metadata map[string]any) error

// StreamResult summarizes a drained chat stream.
type StreamResult struct {
	Content   string
	Failed    bool
	HITL      bool // confirm_required or input_required observed
	Error     string
	HasContent bool
}

// WriteStream drains ch onto w with chat SSE event semantics (chunk/input/confirm/tool/model/error/done).
// persistCtx is used for SaveAssistantMessage (may differ from cancel-bound run context).
func WriteStream(persistCtx context.Context, w http.ResponseWriter, ch <-chan service.ChatStreamEvent, sessionID string, persist PersistAssistant) StreamResult {
	var fenceScrub *portalchat.MemoryFenceStreamScrubber
	if portalchat.StreamMemoryFenceScrubEnabled() {
		fenceScrub = portalchat.NewMemoryFenceStreamScrubber(portalchat.StreamMemoryFenceScrubTag())
	}

	var full strings.Builder
	var timeline service.TimelineAccumulator
	res := StreamResult{}

	for event := range ch {
		switch event.Type {
		case service.ChatStreamEventChunk:
			out := event.Content
			if fenceScrub != nil && out != "" {
				out = fenceScrub.Feed(out)
			}
			full.WriteString(out)
			if out != "" {
				res.HasContent = true
			}
			WriteEvent(w, "chunk", map[string]any{"content": out})
		case service.ChatStreamEventInputRequired:
			res.HITL = true
			if event.Input != nil {
				WriteEvent(w, "input_required", map[string]any{"input": event.Input})
			}
		case service.ChatStreamEventConfirmRequired:
			res.HITL = true
			if event.Confirmation != nil {
				WriteEvent(w, "confirm_required", map[string]any{"confirmation": event.Confirmation})
			}
		case service.ChatStreamEventConfirmResult:
			if event.ConfirmResult != nil {
				WriteEvent(w, "confirm_result", map[string]any{"confirm_result": event.ConfirmResult})
			}
		case service.ChatStreamEventError:
			if suppressTerminalStreamError(errors.New(event.Error), res.HasContent) {
				continue
			}
			res.Failed = true
			res.Error = event.Error
			WriteEvent(w, "error", map[string]any{"error": event.Error})
		case service.ChatStreamEventDebug:
			WriteEvent(w, "debug", map[string]any{"content": event.Content})
		case service.ChatStreamEventToolCall:
			if event.ToolCall != nil {
				timeline.ApplyToolCall(event.ToolCall)
				WriteEvent(w, "tool_call", map[string]any{"tool_call": event.ToolCall})
			}
		case service.ChatStreamEventModelCall:
			if event.ModelCall != nil {
				timeline.ApplyModelCall(event.ModelCall)
				WriteEvent(w, "model_call", map[string]any{"model_call": event.ModelCall})
			}
		case service.ChatStreamEventMEA:
			if event.MEA != nil {
				WriteEvent(w, "mea", map[string]any{"mea": event.MEA})
			}
		default:
			if event.Content != "" {
				out := event.Content
				if fenceScrub != nil {
					out = fenceScrub.Feed(out)
				}
				full.WriteString(out)
				res.HasContent = true
				WriteEvent(w, "chunk", map[string]any{"content": out})
			}
		}
		flush(w)
	}

	if fenceScrub != nil {
		tail, truncated := fenceScrub.Flush()
		if tail != "" {
			full.WriteString(tail)
			res.HasContent = true
			WriteEvent(w, "chunk", map[string]any{"content": tail})
			flush(w)
		}
		_ = truncated
	}

	res.Content = full.String()
	if res.Failed {
		return res
	}

	meta := service.MetadataWithTimeline(timeline.Finalize())
	if persist != nil {
		if err := persist(persistCtx, sessionID, res.Content, meta); err != nil {
			if suppressTerminalStreamError(err, full.Len() > 0) {
				WriteEvent(w, "done", map[string]any{"content": "", "done": true})
				flush(w)
				return res
			}
			res.Failed = true
			res.Error = err.Error()
			WriteEvent(w, "error", map[string]any{"error": err.Error()})
			flush(w)
			return res
		}
	}

	WriteEvent(w, "done", map[string]any{"content": "", "done": true})
	flush(w)
	return res
}

// AggregateFinal drains a chat stream without writing SSE; HITL events mark failure for non-interactive surfaces.
// Applies the same memory-fence scrub as WriteStream before returning content for persist/response.
// Unlike WriteStream, deadline/cancel errors are never suppressed: final mode has no prior client delivery.
// Only chunk text is aggregated into Content — debug / tool_call / model_call must not leak into
// channel replies (WeCom cards, webhook final, etc.), matching WriteStream persist semantics.
func AggregateFinal(ch <-chan service.ChatStreamEvent) StreamResult {
	var fenceScrub *portalchat.MemoryFenceStreamScrubber
	if portalchat.StreamMemoryFenceScrubEnabled() {
		fenceScrub = portalchat.NewMemoryFenceStreamScrubber(portalchat.StreamMemoryFenceScrubTag())
	}

	var full strings.Builder
	res := StreamResult{}
	for event := range ch {
		switch event.Type {
		case service.ChatStreamEventChunk:
			out := event.Content
			if fenceScrub != nil && out != "" {
				out = fenceScrub.Feed(out)
			}
			full.WriteString(out)
			if out != "" {
				res.HasContent = true
			}
		case service.ChatStreamEventInputRequired, service.ChatStreamEventConfirmRequired:
			res.HITL = true
			res.Failed = true
			if res.Error == "" {
				res.Error = "hitl required but reply_mode=final has no interactive surface"
			}
		case service.ChatStreamEventError:
			// Do not suppress deadline/cancel here — final JSON must surface failure (runFinalTurn also checks ctx).
			res.Failed = true
			res.Error = event.Error
		case service.ChatStreamEventDebug, service.ChatStreamEventToolCall, service.ChatStreamEventModelCall,
			service.ChatStreamEventConfirmResult, service.ChatStreamEventMEA:
			// Observability / HITL side-channels — never part of the assistant answer body.
		default:
			// Unknown types: ignore non-chunk content to avoid leaking protocol payloads into IM replies.
		}
	}
	if fenceScrub != nil {
		tail, _ := fenceScrub.Flush()
		if tail != "" {
			full.WriteString(tail)
			res.HasContent = true
		}
	}
	res.Content = full.String()
	return res
}

// SuppressTerminalStreamError reports whether a terminal stream error should be hidden
// after some assistant content was already delivered (deadline/cancel only).
func SuppressTerminalStreamError(err error, hasContent bool) bool {
	if err == nil || !hasContent {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "context deadline exceeded") || strings.Contains(msg, "context canceled")
}

func suppressTerminalStreamError(err error, hasContent bool) bool {
	return SuppressTerminalStreamError(err, hasContent)
}

func flush(w http.ResponseWriter) {
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}
