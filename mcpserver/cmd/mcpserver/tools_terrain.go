package main

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type SetTerrainSlotInput struct {
	Asset string `json:"asset"`
	Slot  int    `json:"slot,omitempty" jsonschema:"terrain slot 0-7, default 0"`
}

func (a *app) SetTerrainSlot(ctx context.Context, _ *mcp.CallToolRequest, in SetTerrainSlotInput) (*mcp.CallToolResult, any, error) {
	params := map[string]any{"asset": in.Asset}
	addNonZero(params, "slot", in.Slot)
	return a.forward(ctx, "set_terrain_slot", params)
}

type FillTerrainInput struct {
	Slot  int    `json:"slot,omitempty" jsonschema:"default 0"`
	Asset string `json:"asset,omitempty"`
}

func (a *app) FillTerrain(ctx context.Context, _ *mcp.CallToolRequest, in FillTerrainInput) (*mcp.CallToolResult, any, error) {
	params := map[string]any{}
	addNonZero(params, "slot", in.Slot)
	addNonEmpty(params, "asset", in.Asset)
	return a.forward(ctx, "fill_terrain", params)
}

type FillRegionInput struct {
	Rect   []float64   `json:"rect,omitempty" jsonschema:"[x,y,w,h] — provide this or points, not both"`
	Points [][]float64 `json:"points,omitempty" jsonschema:"[[x,y],...] polygon — provide this or rect, not both"`
	Slot   int         `json:"slot,omitempty" jsonschema:"default 0"`
	Asset  string      `json:"asset,omitempty"`
	Rate   float64     `json:"rate,omitempty" jsonschema:"paint strength 0-1, default 1"`
}

func (a *app) FillRegion(ctx context.Context, _ *mcp.CallToolRequest, in FillRegionInput) (*mcp.CallToolResult, any, error) {
	params := map[string]any{}
	if len(in.Rect) > 0 {
		params["rect"] = in.Rect
	}
	if len(in.Points) > 0 {
		params["points"] = in.Points
	}
	addNonZero(params, "slot", in.Slot)
	addNonEmpty(params, "asset", in.Asset)
	addNonZero(params, "rate", in.Rate)
	return a.forward(ctx, "fill_region", params)
}

type PaintTerrainInput struct {
	Slot   int     `json:"slot,omitempty" jsonschema:"default 0"`
	X      float64 `json:"x,omitempty"`
	Y      float64 `json:"y,omitempty"`
	Radius float64 `json:"radius,omitempty" jsonschema:"brush radius, default 64"`
	Rate   float64 `json:"rate,omitempty" jsonschema:"paint strength 0-1, default 1"`
	Asset  string  `json:"asset,omitempty"`
}

func (a *app) PaintTerrain(ctx context.Context, _ *mcp.CallToolRequest, in PaintTerrainInput) (*mcp.CallToolResult, any, error) {
	params := map[string]any{}
	addNonZero(params, "slot", in.Slot)
	addNonZero(params, "x", in.X)
	addNonZero(params, "y", in.Y)
	addNonZero(params, "radius", in.Radius)
	addNonZero(params, "rate", in.Rate)
	addNonEmpty(params, "asset", in.Asset)
	return a.forward(ctx, "paint_terrain", params)
}

type PaintPathInput struct {
	Points [][]float64 `json:"points" jsonschema:"[[x,y],...] continuous stroke"`
	Slot   int         `json:"slot,omitempty" jsonschema:"default 0"`
	Radius float64     `json:"radius,omitempty" jsonschema:"ribbon half-width, default 96"`
	Rate   float64     `json:"rate,omitempty" jsonschema:"paint strength 0-1, default 1"`
	Asset  string      `json:"asset,omitempty"`
}

func (a *app) PaintPath(ctx context.Context, _ *mcp.CallToolRequest, in PaintPathInput) (*mcp.CallToolResult, any, error) {
	params := map[string]any{"points": in.Points}
	addNonZero(params, "slot", in.Slot)
	addNonZero(params, "radius", in.Radius)
	addNonZero(params, "rate", in.Rate)
	addNonEmpty(params, "asset", in.Asset)
	return a.forward(ctx, "paint_path", params)
}

type DigCaveInput struct {
	Points      [][]float64 `json:"points,omitempty" jsonschema:"[[x,y],...] path to dig — provide this or x/y"`
	X           float64     `json:"x,omitempty"`
	Y           float64     `json:"y,omitempty"`
	Radius      float64     `json:"radius,omitempty" jsonschema:"default 256"`
	Dig         *bool       `json:"dig,omitempty" jsonschema:"true digs (default), false fills back to rock"`
	GroundColor string      `json:"ground_color,omitempty" jsonschema:"tints the whole cave system"`
	WallColor   string      `json:"wall_color,omitempty" jsonschema:"tints the whole cave system"`
	Texture     string      `json:"texture,omitempty" jsonschema:"cave floor asset from the Caves bank"`
}

func (a *app) DigCave(ctx context.Context, _ *mcp.CallToolRequest, in DigCaveInput) (*mcp.CallToolResult, any, error) {
	params := map[string]any{}
	if len(in.Points) > 0 {
		params["points"] = in.Points
	}
	addNonZero(params, "x", in.X)
	addNonZero(params, "y", in.Y)
	addNonZero(params, "radius", in.Radius)
	if in.Dig != nil {
		params["dig"] = *in.Dig
	}
	addNonEmpty(params, "ground_color", in.GroundColor)
	addNonEmpty(params, "wall_color", in.WallColor)
	addNonEmpty(params, "texture", in.Texture)
	return a.forward(ctx, "dig_cave", params)
}

func (a *app) ClearCaves(ctx context.Context, _ *mcp.CallToolRequest, _ NoArgs) (*mcp.CallToolResult, any, error) {
	return a.forward(ctx, "clear_caves", nil)
}
