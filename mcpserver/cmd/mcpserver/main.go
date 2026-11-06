// Command mcpserver speaks MCP over stdio to Claude Code and forwards every
// tool call to bridge_app's wire protocol (see PROTOCOL.md). Replaces the
// Python server/ package; env vars DD_BRIDGE_HOST/DD_BRIDGE_PORT carry over
// unchanged.
package main

import (
	"context"
	"log"
	"os"
	"strconv"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"mapforge/bridgeclient"
)

// version is stamped at build time (see scripts/install.ps1); "dev" for a
// plain `go build`.
var version = "dev"

// app carries the bridge client every tool handler forwards through.
type app struct {
	bridge *bridge.Client
}

func main() {
	host := envOr("DD_BRIDGE_HOST", "127.0.0.1")
	port := envIntOr("DD_BRIDGE_PORT", 8787)

	client := bridge.New(host, port)
	client.Name = "mcp" // names this server's commands in bridge_app's command log
	a := &app{bridge: client}

	server := mcp.NewServer(&mcp.Implementation{Name: "dungeondraft-mcp", Version: version}, nil)
	a.registerTools(server)

	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envIntOr(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}
