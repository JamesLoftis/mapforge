package main

import (
	"context"
	"slices"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Spec goal 2: the MCP server only observes the map. Maps are built with
// Terraform; screenshots, export and the camera change the view, not the map.
var lookTools = []string{
	"dump_state", "export_map", "fit_elements", "focus_element", "get_camera",
	"get_element", "get_status", "list_asset_categories", "list_assets",
	"list_elements", "list_levels", "ping", "screenshot", "set_camera", "set_level",
}

func TestServerExposesOnlyLookTools(t *testing.T) {
	ctx := context.Background()
	server := mcp.NewServer(&mcp.Implementation{Name: "test"}, nil)
	(&app{}).registerTools(server)
	serverSide, clientSide := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, serverSide, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test-client"}, nil).Connect(ctx, clientSide, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer session.Close()

	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	var names []string
	for _, tool := range listed.Tools {
		names = append(names, tool.Name)
	}
	slices.Sort(names)
	if !slices.Equal(names, lookTools) {
		t.Fatalf("tools = %v\nwant exactly %v", names, lookTools)
	}
}
