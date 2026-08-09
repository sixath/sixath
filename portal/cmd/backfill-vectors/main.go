package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"backend/internal/biz"
	"backend/internal/chat"
	"backend/internal/conf"
	"backend/internal/data"

	"github.com/go-kratos/kratos/v2/config"
	"github.com/go-kratos/kratos/v2/config/file"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/sixath/framework/memory"

	fwconfig "github.com/sixath/framework/config"
)

func main() {
	confPath := flag.String("conf", "../configs", "config path, eg: -conf config.yaml")
	force := flag.Bool("force", false, "rebuild vectors even when Has reports present")
	dryRun := flag.Bool("dry-run", false, "count missing without Embed/Upsert")
	scope := flag.String("scope", "all", "session|user|all")
	batch := flag.Int("batch", 50, "List page size")
	sleep := flag.Duration("sleep", 200*time.Millisecond, "pause between List pages")
	flag.Parse()

	args := []string{
		"--scope", *scope,
		"--batch", fmt.Sprintf("%d", *batch),
		"--sleep", sleep.String(),
	}
	if *force {
		args = append(args, "--force")
	}
	if *dryRun {
		args = append(args, "--dry-run")
	}
	cliFlags, err := chat.ParseBackfillArgs(args)
	if err != nil {
		fatalf("%v", err)
	}

	logger := log.With(log.NewStdLogger(os.Stderr),
		"ts", log.DefaultTimestamp,
		"caller", log.DefaultCaller,
		"service.name", "backfill-vectors",
	)

	c := config.New(config.WithSource(file.NewSource(*confPath)))
	defer c.Close()
	if err := c.Load(); err != nil {
		fatalf("load config: %v", err)
	}
	var bc conf.Bootstrap
	if err := c.Scan(&bc); err != nil {
		fatalf("scan config: %v", err)
	}
	if bc.Data == nil {
		fatalf("data config required")
	}
	chat.SetMemoryVectorDataRoot(bc.Data.GetDataRoot())

	if p, err := fwconfig.ResolvePortalAgentExtraPath(*confPath); err == nil {
		if extra, err := fwconfig.LoadPortalAgentExtra(p); err != nil {
			fatalf("agent_extra: %v", err)
		} else if extra != nil {
			chat.SetPortalAgentExtra(extra)
		}
	}

	d, cleanup, err := data.NewData(bc.Data, bc.Auth, logger)
	if err != nil {
		fatalf("database: %v", err)
	}
	defer cleanup()

	agentRepo := data.NewAgentRepo(d, bc.Data, logger)
	chat.SetMemoryAgentGetter(repoAgentGetter{repo: agentRepo})

	units := data.NewSessionUnitsBackendFromData(d)
	opts := chat.DefaultMemoryStoreOptions()
	if opts.UnitVectors == nil {
		fatalf("unit vector index unavailable (check memory_vector provider / data_root)")
	}
	if opts.UnitEmbedder == nil {
		fatalf("unit embedder unavailable (set memory_extraction.auxiliary or ensure agents are readable)")
	}

	bf := memory.NewUnitBackfiller(cliFlags.ToBackfillConfig(units, opts.UnitVectors, opts.UnitEmbedder))
	st, err := bf.Run(context.Background())
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(st)
	if err != nil {
		fmt.Fprintf(os.Stderr, "backfill-vectors: run error: %v\n", err)
		os.Exit(1)
	}
	if st.Tripped {
		fmt.Fprintf(os.Stderr, "backfill-vectors: embed circuit tripped; partial stats above\n")
		os.Exit(0)
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "backfill-vectors: "+format+"\n", args...)
	os.Exit(1)
}

// repoAgentGetter resolves agent chat models without ACL (ops CLI).
type repoAgentGetter struct {
	repo biz.AgentRepo
}

func (g repoAgentGetter) Get(ctx context.Context, id string) (*biz.AgentMeta, error) {
	return g.repo.GetByID(ctx, id)
}
