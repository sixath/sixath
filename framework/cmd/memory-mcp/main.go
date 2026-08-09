package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	memorymcp "github.com/sixath/framework/memory/mcp"
)

func main() {
	transport := flag.String("transport", "stdio", "stdio | http")
	addr := flag.String("addr", ":8765", "HTTP listen address (transport=http)")
	agentWrite := flag.Bool("agent-write", false, "allow scope=agent writes")
	flag.Parse()

	store := memorymcp.NewDefaultStore()
	srv, err := memorymcp.NewServer(store, memorymcp.Options{
		AgentWriteEnabled: *agentWrite,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "memory-mcp: %v\n", err)
		os.Exit(1)
	}

	switch strings.ToLower(strings.TrimSpace(*transport)) {
	case "stdio", "":
		if err := memorymcp.ServeStdio(srv); err != nil {
			fmt.Fprintf(os.Stderr, "memory-mcp stdio: %v\n", err)
			os.Exit(1)
		}
	case "http":
		fmt.Fprintf(os.Stderr, "memory-mcp listening on %s (streamable HTTP /mcp)\n", *addr)
		if err := memorymcp.ListenAndServeHTTP(srv, *addr); err != nil {
			fmt.Fprintf(os.Stderr, "memory-mcp http: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "memory-mcp: unknown transport %q\n", *transport)
		os.Exit(2)
	}
}
