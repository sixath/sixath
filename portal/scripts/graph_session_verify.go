//go:build ignore

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"backend/internal/chat"

	"github.com/sixath/framework/memory"
	"go.yaml.in/yaml/v2"
)

type overlay struct {
	MemoryGraph *struct {
		Enabled               bool    `yaml:"enabled"`
		MinRelationConfidence float64 `yaml:"min_relation_confidence"`
		MaxEntities           int     `yaml:"max_entities"`
		Auxiliary             *struct {
			Provider string `yaml:"provider"`
			Model    string `yaml:"model"`
			APIKey   string `yaml:"api_key"`
			BaseURL  string `yaml:"base_url"`
		} `yaml:"auxiliary"`
		Neo4j *struct {
			URI      string `yaml:"uri"`
			Username string `yaml:"username"`
			Password string `yaml:"password"`
			Database string `yaml:"database"`
		} `yaml:"neo4j"`
	} `yaml:"memory_graph"`
	MemoryExtraction *struct {
		Auxiliary *struct {
			Provider string `yaml:"provider"`
			Model    string `yaml:"model"`
			APIKey   string `yaml:"api_key"`
			BaseURL  string `yaml:"base_url"`
		} `yaml:"auxiliary"`
	} `yaml:"memory_extraction"`
}

func main() {
	confPath := os.Getenv("PORTAL_CONF")
	if confPath == "" {
		confPath = `E:\configs\sixath\portal\agent_extra.yaml`
	}
	raw, err := os.ReadFile(confPath)
	if err != nil {
		log.Fatal(err)
	}
	var ov overlay
	if err := yaml.Unmarshal(raw, &ov); err != nil {
		log.Fatal(err)
	}
	if ov.MemoryGraph == nil || ov.MemoryGraph.Neo4j == nil {
		log.Fatal("memory_graph.neo4j missing")
	}
	aux := ov.MemoryGraph.Auxiliary
	if aux == nil && ov.MemoryExtraction != nil {
		aux = ov.MemoryExtraction.Auxiliary
	}
	if aux == nil || aux.APIKey == "" {
		log.Fatal("auxiliary model missing")
	}

	g, err := memory.NewNeo4jGraphStore(memory.Neo4jConfig{
		URI: ov.MemoryGraph.Neo4j.URI, Username: ov.MemoryGraph.Neo4j.Username,
		Password: ov.MemoryGraph.Neo4j.Password, Database: ov.MemoryGraph.Neo4j.Database,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer g.Close()

	m, err := chat.BuildModel(aux.Provider, aux.Model, aux.APIKey, aux.BaseURL)
	if err != nil {
		log.Fatal(err)
	}

	minConf := ov.MemoryGraph.MinRelationConfidence
	if minConf <= 0 {
		minConf = 0.45
	}
	maxEnt := ov.MemoryGraph.MaxEntities
	if maxEnt <= 0 {
		maxEnt = 32
	}
	sessionID := "b73cf880-f42f-497a-8672-3fd39414fb2a"
	user := "Extract cloud-phone access topology edges with English service names:\n" +
		"SDK -> api-gateway -> union-access -> union_resource -> vm-manager\n" +
		"SDK -> access-service -> migu-access -> cgsession -> cgschedule\n" +
		"SDK -> bytedance_access -> cgsession\nInclude HTTP/gRPC edges."
	asst := "Edges (all scope=session):\n" +
		"SDK -calls-> api-gateway\n" +
		"api-gateway -calls-> union-access\n" +
		"union-access -speaks_http-> union_resource\n" +
		"union-access -speaks_grpc-> union_resource\n" +
		"union_resource -speaks_http-> vm-manager\n" +
		"SDK -calls-> access-service\n" +
		"access-service -calls-> migu-access\n" +
		"migu-access -calls-> cgsession\n" +
		"cgsession -speaks_grpc-> cgschedule\n" +
		"SDK -calls-> bytedance_access\n" +
		"bytedance_access -calls-> cgsession"

	pipe := &memory.GraphPipeline{
		Graph: g, Enabled: true, MaxEntities: maxEnt, MinRelationConfidence: minConf,
		Extractor: &memory.LLMGraphExtractor{Model: m, MaxEntities: maxEnt},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	st, err := pipe.AddGraphFromTurnWithStats(ctx, memory.TurnInput{
		SessionID: sessionID, AgentID: "1989b2bd-2d4f-4c01-8bc6-da934159f295",
		UserMessage: user, AssistantMessage: asst,
	})
	fmt.Printf("memory graph done session_id=%s result=%s cand_ent=%d cand_rel=%d written_ent=%d written_rel=%d drops=%v dur_ms=%d err=%v\n",
		sessionID, st.Result, st.CandidateEntities, st.CandidateRels, st.WrittenEntities, st.WrittenRels, st.Drops, st.Duration.Milliseconds(), err)
}
