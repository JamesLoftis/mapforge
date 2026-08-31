// Command ddcheck is the walking-skeleton proof: it talks to a live bridge
// using the same Go client the provider uses and prints a status summary.
//
//	go run ./cmd/ddcheck [host] [port]
package main

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"dungeondraft-mcp/provider/internal/bridge"
)

func main() {
	host := "127.0.0.1"
	port := 8787
	if len(os.Args) > 2 {
		if p, err := strconv.Atoi(os.Args[2]); err == nil {
			port = p
		}
	}
	if len(os.Args) > 1 {
		host = os.Args[1]
	}

	c := bridge.New(host, port)
	ctx := context.Background()

	pong, err := c.Request(ctx, "ping", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ping: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("ping ok: %v\n", pong)

	// ping reports dd_connected (app up, but is the Dungeondraft agent linked?).
	// Surface that explicitly; exit non-zero so a script can gate on it.
	connected, _ := pong["dd_connected"].(bool)
	if app, _ := pong["app"].(string); app != "" {
		fmt.Printf("bridge app: %s\n", app)
	}
	fmt.Printf("dd_connected: %v\n", connected)
	if !connected {
		fmt.Fprintf(os.Stderr, "bridge app is up but the Dungeondraft mod is not connected — check that DD is running with the MCP Bridge mod enabled and a map open.\n")
		os.Exit(2)
	}

	status, err := c.Request(ctx, "get_status", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "get_status: %v\n", err)
		os.Exit(1)
	}
	size, _ := status["map_size_woxels"].([]any)
	center, _ := status["map_center"].([]any)
	counts, _ := status["counts"].(map[string]any)
	fmt.Printf("map_open: %v\n", status["map_open"])
	fmt.Printf("map_size_woxels: %v\n", size)
	fmt.Printf("map_center:      %v\n", center)
	fmt.Printf("active_tool:     %v\n", status["active_tool"])
	fmt.Printf("counts: %v\n", counts)
}
