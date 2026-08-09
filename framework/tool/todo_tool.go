package tool

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

// TodoStatus is the lifecycle state of a session todo item.
type TodoStatus string

const (
	TodoStatusPending    TodoStatus = "pending"
	TodoStatusInProgress TodoStatus = "in_progress"
	TodoStatusCompleted  TodoStatus = "completed"
	TodoStatusCancelled  TodoStatus = "cancelled"
)

// TodoItem is a single session-scoped task entry.
type TodoItem struct {
	ID      string     `json:"id"`
	Content string     `json:"content"`
	Status  TodoStatus `json:"status"`
}

// TodoStore holds per-session todo lists (not persisted across sessions).
type TodoStore interface {
	List(sessionID string) []TodoItem
	Replace(sessionID string, items []TodoItem) []TodoItem
	Merge(sessionID string, items []TodoItem) []TodoItem
}

// InMemoryTodoStore is a process-local todo store keyed by session_id.
type InMemoryTodoStore struct {
	mu       sync.RWMutex
	sessions map[string]*sessionTodoState
}

type sessionTodoState struct {
	order []string
	items map[string]TodoItem
}

// DefaultTodoStore is shared by runtime todo tool registrations.
var DefaultTodoStore = NewInMemoryTodoStore()

// NewInMemoryTodoStore creates an empty in-memory todo store.
func NewInMemoryTodoStore() *InMemoryTodoStore {
	return &InMemoryTodoStore{sessions: make(map[string]*sessionTodoState)}
}

func (s *InMemoryTodoStore) state(sessionID string) *sessionTodoState {
	st, ok := s.sessions[sessionID]
	if !ok {
		st = &sessionTodoState{items: make(map[string]TodoItem)}
		s.sessions[sessionID] = st
	}
	return st
}

func (s *InMemoryTodoStore) List(sessionID string) []TodoItem {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st := s.sessions[sessionID]
	if st == nil {
		return nil
	}
	return orderedTodoItems(st)
}

func (s *InMemoryTodoStore) Replace(sessionID string, items []TodoItem) []TodoItem {
	normalized := normalizeTodoBatch(items)
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.state(sessionID)
	st.order = make([]string, 0, len(normalized))
	st.items = make(map[string]TodoItem, len(normalized))
	for _, item := range normalized {
		st.order = append(st.order, item.ID)
		st.items[item.ID] = item
	}
	enforceSingleInProgress(st)
	return orderedTodoItems(st)
}

func (s *InMemoryTodoStore) Merge(sessionID string, items []TodoItem) []TodoItem {
	normalized := normalizeTodoBatch(items)
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.state(sessionID)
	for _, item := range normalized {
		if _, exists := st.items[item.ID]; !exists {
			st.order = append(st.order, item.ID)
		}
		st.items[item.ID] = item
	}
	enforceSingleInProgress(st)
	return orderedTodoItems(st)
}

// FormatTodosForInjection returns pending/in_progress items for context injection (H-P0-C2).
func FormatTodosForInjection(items []TodoItem) string {
	var lines []string
	for _, item := range items {
		if item.Status != TodoStatusPending && item.Status != TodoStatusInProgress {
			continue
		}
		lines = append(lines, fmt.Sprintf("- [%s] %s: %s", item.Status, item.ID, item.Content))
	}
	if len(lines) == 0 {
		return ""
	}
	return "Active todos:\n" + strings.Join(lines, "\n")
}

// RegisterTodoTool registers the Hermes-aligned session todo tool.
func RegisterTodoTool(reg *Registry, store TodoStore) error {
	if reg == nil {
		return errors.New("todo: registry is nil")
	}
	if store == nil {
		store = DefaultTodoStore
	}
	return reg.Register(Tool{
		Name: "todo",
		Description: "Manage your task list for the current session. Use for complex tasks with 3+ steps. " +
			"Call with no todos to read the current list. merge=false replaces the list; merge=true updates by id.",
		Toolset: ToolsetTodo,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"todos": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"id":      map[string]any{"type": "string"},
							"content": map[string]any{"type": "string"},
							"status": map[string]any{
								"type": "string",
								"enum": []string{
									string(TodoStatusPending),
									string(TodoStatusInProgress),
									string(TodoStatusCompleted),
									string(TodoStatusCancelled),
								},
							},
						},
						"required": []string{"id", "content", "status"},
					},
				},
				"merge": map[string]any{
					"type":        "boolean",
					"description": "When true, merge todos by id; when false (default), replace the entire list.",
				},
			},
		},
		Execute: func(ctx context.Context, params map[string]any) (any, error) {
			sessionID, _ := ctx.Value(ContextKeySessionID).(string)
			if sessionID == "" {
				return map[string]any{"error": "session_id is required"}, nil
			}

			rawTodos, hasTodos := params["todos"]
			if !hasTodos || rawTodos == nil {
				return todoListResponse(store.List(sessionID)), nil
			}

			items, err := parseTodoParams(rawTodos)
			if err != nil {
				return map[string]any{"error": err.Error()}, nil
			}

			merge, _ := params["merge"].(bool)
			var out []TodoItem
			if merge {
				out = store.Merge(sessionID, items)
			} else {
				out = store.Replace(sessionID, items)
			}
			return todoListResponse(out), nil
		},
	})
}

func todoListResponse(items []TodoItem) map[string]any {
	if items == nil {
		items = []TodoItem{}
	}
	payload := make([]map[string]any, 0, len(items))
	for _, item := range items {
		payload = append(payload, map[string]any{
			"id":      item.ID,
			"content": item.Content,
			"status":  string(item.Status),
		})
	}
	return map[string]any{"todos": payload}
}

func parseTodoParams(raw any) ([]TodoItem, error) {
	arr, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("todos must be an array")
	}
	out := make([]TodoItem, 0, len(arr))
	for i, entry := range arr {
		m, ok := entry.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("todos[%d] must be an object", i)
		}
		id, _ := m["id"].(string)
		content, _ := m["content"].(string)
		statusRaw, _ := m["status"].(string)
		if strings.TrimSpace(id) == "" {
			return nil, fmt.Errorf("todos[%d].id is required", i)
		}
		if strings.TrimSpace(content) == "" {
			return nil, fmt.Errorf("todos[%d].content is required", i)
		}
		status := TodoStatus(strings.TrimSpace(statusRaw))
		if !isValidTodoStatus(status) {
			return nil, fmt.Errorf("todos[%d].status is invalid", i)
		}
		out = append(out, TodoItem{ID: id, Content: content, Status: status})
	}
	return out, nil
}

func isValidTodoStatus(status TodoStatus) bool {
	switch status {
	case TodoStatusPending, TodoStatusInProgress, TodoStatusCompleted, TodoStatusCancelled:
		return true
	default:
		return false
	}
}

func normalizeTodoBatch(items []TodoItem) []TodoItem {
	out := make([]TodoItem, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, TodoItem{
			ID:      id,
			Content: strings.TrimSpace(item.Content),
			Status:  item.Status,
		})
	}
	return out
}

func enforceSingleInProgress(st *sessionTodoState) {
	if st == nil {
		return
	}
	var winner string
	for i := len(st.order) - 1; i >= 0; i-- {
		id := st.order[i]
		item, ok := st.items[id]
		if !ok {
			continue
		}
		if item.Status == TodoStatusInProgress {
			winner = id
			break
		}
	}
	if winner == "" {
		return
	}
	for id, item := range st.items {
		if id != winner && item.Status == TodoStatusInProgress {
			item.Status = TodoStatusPending
			st.items[id] = item
		}
	}
}

func orderedTodoItems(st *sessionTodoState) []TodoItem {
	if st == nil || len(st.order) == 0 {
		return nil
	}
	out := make([]TodoItem, 0, len(st.order))
	for _, id := range st.order {
		if item, ok := st.items[id]; ok {
			out = append(out, item)
		}
	}
	return out
}
