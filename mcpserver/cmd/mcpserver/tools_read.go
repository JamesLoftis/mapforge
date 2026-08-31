package main

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (a *app) Ping(ctx context.Context, _ *mcp.CallToolRequest, _ NoArgs) (*mcp.CallToolResult, any, error) {
	return a.forward(ctx, "ping", nil)
}

func (a *app) GetStatus(ctx context.Context, _ *mcp.CallToolRequest, _ NoArgs) (*mcp.CallToolResult, any, error) {
	return a.forward(ctx, "get_status", nil)
}

func (a *app) ListAssetCategories(ctx context.Context, _ *mcp.CallToolRequest, _ NoArgs) (*mcp.CallToolResult, any, error) {
	return a.forward(ctx, "list_asset_categories", nil)
}

type ListAssetsInput struct {
	Category string `json:"category,omitempty" jsonschema:"asset category, e.g. Objects, Walls, Paths, Terrain, Lights, Portals, Roofs (default Objects)"`
	Search   string `json:"search,omitempty" jsonschema:"case-insensitive substring to match against the asset path"`
	Limit    int    `json:"limit,omitempty" jsonschema:"max results (default 100, capped at 300 — use search to narrow instead of raising this)"`
}

func (a *app) ListAssets(ctx context.Context, _ *mcp.CallToolRequest, in ListAssetsInput) (*mcp.CallToolResult, any, error) {
	params := map[string]any{}
	if in.Category != "" {
		params["category"] = in.Category
	}
	if in.Search != "" {
		params["search"] = in.Search
	}
	if in.Limit != 0 {
		params["limit"] = clampLimit(in.Limit)
	}
	return a.forward(ctx, "list_assets", params)
}

type ListElementsInput struct {
	Kind  string `json:"kind,omitempty" jsonschema:"one of objects, walls, lights, paths, portals, roofs, texts (default objects)"`
	Limit int    `json:"limit,omitempty" jsonschema:"max results (default 200, capped at 300 — filter by kind instead of raising this)"`
}

func (a *app) ListElements(ctx context.Context, _ *mcp.CallToolRequest, in ListElementsInput) (*mcp.CallToolResult, any, error) {
	params := map[string]any{}
	if in.Kind != "" {
		params["kind"] = in.Kind
	}
	if in.Limit != 0 {
		params["limit"] = clampLimit(in.Limit)
	}
	return a.forward(ctx, "list_elements", params)
}

type GetElementInput struct {
	ID int `json:"id" jsonschema:"element id"`
}

func (a *app) GetElement(ctx context.Context, _ *mcp.CallToolRequest, in GetElementInput) (*mcp.CallToolResult, any, error) {
	return a.forward(ctx, "get_element", map[string]any{"id": in.ID})
}

func (a *app) DumpState(ctx context.Context, _ *mcp.CallToolRequest, _ NoArgs) (*mcp.CallToolResult, any, error) {
	result, err := a.bridge.Request(ctx, "dump_state", nil)
	if err != nil {
		return nil, nil, err
	}
	if elements, ok := result["elements"].([]any); ok && len(elements) > maxDumpStateElements {
		return nil, nil, fmt.Errorf(
			"dump_state would return %d elements, over the %d-element limit for a single MCP call — "+
				"use list_elements with a kind filter (see get_status's counts) or get_element for specific ids instead",
			len(elements), maxDumpStateElements,
		)
	}
	return nil, result, nil
}

func (a *app) ListLevels(ctx context.Context, _ *mcp.CallToolRequest, _ NoArgs) (*mcp.CallToolResult, any, error) {
	return a.forward(ctx, "list_levels", nil)
}
