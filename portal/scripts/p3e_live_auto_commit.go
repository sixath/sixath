//go:build ignore

// Live auto_commit E2E against configured MySQL using Portal chat wiring.
//
//	cd portal && go run ./scripts/p3e_live_auto_commit.go
package main

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"backend/internal/chat"
	"backend/internal/data"

	fwconfig "github.com/sixath/framework/config"
	"github.com/sixath/framework/events"
	"github.com/sixath/framework/memory"
	"github.com/sixath/framework/tool"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func main() {
	confDir := `E:\configs\sixath\portal`
	if v := os.Getenv("SATH_CONF"); v != "" {
		confDir = v
	}

	dsn := os.Getenv("SATH_MYSQL_DSN")
	if dsn == "" {
		raw, err := os.ReadFile(confDir + `\config.yaml`)
		if err != nil {
			fatal("read config: %v", err)
		}
		re := regexp.MustCompile(`source:\s*([^\s]+)`)
		m := re.FindSubmatch(raw)
		if m == nil {
			fatal("no database source in config.yaml")
		}
		dsn = strings.Trim(string(m[1]), `"'`)
	}

	extraPath, err := fwconfig.ResolvePortalAgentExtraPath(confDir)
	if err != nil {
		fatal("resolve agent_extra: %v", err)
	}
	extra, err := fwconfig.LoadPortalAgentExtra(extraPath)
	if err != nil {
		fatal("load agent_extra: %v", err)
	}
	if extra == nil || extra.MemoryProceduralRepair == nil || !extra.MemoryProceduralRepair.Enabled {
		fatal("memory_procedural_repair.enabled must be true in agent_extra")
	}
	pr := *extra.MemoryProceduralRepair
	pr.AutoCommit = true
	if pr.MinSupport <= 0 {
		pr.MinSupport = 2
	}
	if len(pr.Bindings) == 0 {
		pr.Bindings = []fwconfig.MemoryProceduralBindingYAML{{
			TriggerCode:  memory.FailureCodeToolFailed,
			TriggerQuery: "ssh",
			ActionKind:   memory.BindingActionToolSequence,
			ToolNames:    []string{"ask_user"},
			Mode:         memory.BindingModeSuggest,
		}}
	}
	chat.SetProceduralRepairConfig(&pr)

	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		fatal("mysql open: %v", err)
	}
	units := data.NewSessionUnitsBackend(db)
	store := memory.NewFacade(memory.FacadeConfig{Session: units})
	chat.SetPrefetchMemoryStore(store)

	sessionID := fmt.Sprintf("p3e-live-%d", time.Now().Unix())
	agentID := "e8107fb3-e40a-4207-9d9a-6768847aaf79"
	agentName := "zone-4100-agent"

	bus := events.NewBus()
	memory.AttachFailureSignalBridge(bus, chat.DefaultFailureSignalSink())
	ctx := context.Background()
	ctx = context.WithValue(ctx, tool.ContextKeyAgentID, agentID)
	ctx = context.WithValue(ctx, tool.ContextKeyAgentName, agentName)
	ctx = context.WithValue(ctx, tool.ContextKeySessionID, sessionID)

	n := pr.MinSupport
	for i := 0; i < n; i++ {
		bus.Publish(ctx, events.Event{
			Kind:    events.ToolFailed,
			Payload: map[string]any{"tool": "ssh_exec", "error": fmt.Sprintf("e2e fail %d", i+1)},
			At:      time.Now().UTC(),
		})
	}

	procs, err := store.Recall(ctx, memory.RecallQuery{
		Scope: memory.ScopeSession, ScopeID: sessionID, Source: memory.SourceUnits,
		Kind: memory.KindProcedural, Limit: 10,
	})
	if err != nil {
		fatal("recall procedural: %v", err)
	}
	facts, err := store.Recall(ctx, memory.RecallQuery{
		Scope: memory.ScopeSession, ScopeID: sessionID, Source: memory.SourceUnits, Limit: 10,
	})
	if err != nil {
		fatal("recall facts: %v", err)
	}

	fmt.Printf("session=%s procedural=%d fact=%d\n", sessionID, len(procs), len(facts))
	if len(procs) != 1 {
		fatal("want 1 procedural unit, got %d", len(procs))
	}
	if len(facts) != 0 {
		fatal("fact lane leaked procedural (%d)", len(facts))
	}
	fmt.Printf("unit_id=%s\n", procs[0].ID)
	fmt.Printf("content=%s\n", procs[0].Content)

	pf := &memory.StorePrefetchBackend{
		Store: store, MaxSnippets: 5, MaxProcedural: 3,
		LoadPersistedProcedural: true,
		ProceduralBindings:      mustBindings(),
	}
	parts, err := pf.Prefetch(ctx, memory.PrefetchQuery{
		UserMessage: "please retry ssh",
		AgentID:     agentID,
		SessionID:   sessionID,
	})
	if err != nil {
		fatal("prefetch: %v", err)
	}
	okHint := false
	for _, p := range parts {
		if p.Label == "procedural" && strings.Contains(p.Content, "ask_user") {
			okHint = true
		}
	}
	if !okHint {
		fatal("prefetch missing procedural hint; parts=%d", len(parts))
	}

	_ = store.Delete(ctx, memory.GetRef{Scope: memory.ScopeSession, ID: procs[0].ID, ScopeID: sessionID})
	fmt.Println("PASS live FailureSignal → auto_commit → MySQL → Prefetch")
}

func mustBindings() []memory.ProceduralBinding {
	b, _, _ := chat.ProceduralBindingsForPrefetch()
	return b
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "FAIL: "+format+"\n", args...)
	os.Exit(1)
}
