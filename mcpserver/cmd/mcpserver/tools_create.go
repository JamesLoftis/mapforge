package main

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type PlaceObjectInput struct {
	Asset    string  `json:"asset" jsonschema:"asset path, e.g. objects/chair.png"`
	X        float64 `json:"x,omitempty" jsonschema:"woxel x (default: map center)"`
	Y        float64 `json:"y,omitempty" jsonschema:"woxel y (default: map center)"`
	Scale    float64 `json:"scale,omitempty" jsonschema:"default 1"`
	Rotation float64 `json:"rotation,omitempty" jsonschema:"degrees, default 0"`
	Sorting  int     `json:"sorting,omitempty"`
	Color    string  `json:"color,omitempty" jsonschema:"#rrggbb or rrggbb"`
	Z        int     `json:"z,omitempty" jsonschema:"absolute z-index override (default 200)"`
}

func (a *app) PlaceObject(ctx context.Context, _ *mcp.CallToolRequest, in PlaceObjectInput) (*mcp.CallToolResult, any, error) {
	params := map[string]any{"asset": in.Asset}
	addNonZero(params, "x", in.X)
	addNonZero(params, "y", in.Y)
	addNonZero(params, "scale", in.Scale)
	addNonZero(params, "rotation", in.Rotation)
	addNonZero(params, "sorting", in.Sorting)
	addNonEmpty(params, "color", in.Color)
	addNonZero(params, "z", in.Z)
	return a.forward(ctx, "place_object", params)
}

type DrawWallInput struct {
	Points [][]float64 `json:"points" jsonschema:"[[x,y],...] polyline, >= 2 points"`
	Asset  string      `json:"asset,omitempty"`
	Loop   bool        `json:"loop,omitempty" jsonschema:"close the polyline into a loop, default false"`
	Shadow *bool       `json:"shadow,omitempty" jsonschema:"cast shadow, default true"`
	Type   int         `json:"type,omitempty" jsonschema:"wall type: 0=auto,1=manual,2=cave (default 0)"`
	Joint  int         `json:"joint,omitempty" jsonschema:"wall joint: 0=sharp,1=bevel,2=round (default 1)"`
	Color  string      `json:"color,omitempty"`
}

func (a *app) DrawWall(ctx context.Context, _ *mcp.CallToolRequest, in DrawWallInput) (*mcp.CallToolResult, any, error) {
	params := map[string]any{"points": in.Points, "loop": in.Loop}
	addNonEmpty(params, "asset", in.Asset)
	if in.Shadow != nil {
		params["shadow"] = *in.Shadow
	}
	addNonZero(params, "type", in.Type)
	addNonZero(params, "joint", in.Joint)
	addNonEmpty(params, "color", in.Color)
	return a.forward(ctx, "draw_wall", params)
}

type DrawPathInput struct {
	Points      [][]float64 `json:"points" jsonschema:"[[x,y],...] polyline"`
	Asset       string      `json:"asset" jsonschema:"path/road/river texture asset"`
	Layer       int         `json:"layer,omitempty"`
	Sorting     int         `json:"sorting,omitempty"`
	Smoothness  float64     `json:"smoothness,omitempty"`
	Width       float64     `json:"width,omitempty"`
}

func (a *app) DrawPath(ctx context.Context, _ *mcp.CallToolRequest, in DrawPathInput) (*mcp.CallToolResult, any, error) {
	params := map[string]any{"points": in.Points, "asset": in.Asset}
	addNonZero(params, "layer", in.Layer)
	addNonZero(params, "sorting", in.Sorting)
	addNonZero(params, "smoothness", in.Smoothness)
	addNonZero(params, "width", in.Width)
	return a.forward(ctx, "draw_path", params)
}

type AddLightInput struct {
	X       float64 `json:"x,omitempty"`
	Y       float64 `json:"y,omitempty"`
	Color   string  `json:"color,omitempty"`
	Energy  float64 `json:"energy,omitempty" jsonschema:"default 1"`
	Range   float64 `json:"range,omitempty" jsonschema:"texture scale, default 1"`
	Shadows *bool   `json:"shadows,omitempty" jsonschema:"default true"`
	Asset   string  `json:"asset,omitempty"`
}

func (a *app) AddLight(ctx context.Context, _ *mcp.CallToolRequest, in AddLightInput) (*mcp.CallToolResult, any, error) {
	params := map[string]any{}
	addNonZero(params, "x", in.X)
	addNonZero(params, "y", in.Y)
	addNonEmpty(params, "color", in.Color)
	addNonZero(params, "energy", in.Energy)
	addNonZero(params, "range", in.Range)
	if in.Shadows != nil {
		params["shadows"] = *in.Shadows
	}
	addNonEmpty(params, "asset", in.Asset)
	return a.forward(ctx, "add_light", params)
}

type AddPortalInput struct {
	Asset        string  `json:"asset" jsonschema:"door/window texture asset"`
	X            float64 `json:"x,omitempty"`
	Y            float64 `json:"y,omitempty"`
	Closed       bool    `json:"closed,omitempty"`
	Radius       float64 `json:"radius,omitempty" jsonschema:"door half-width in woxels, default 64"`
	Mount        string  `json:"mount,omitempty" jsonschema:"wall (default, snaps to nearest wall) or free (freestanding)"`
	SnapMax      float64 `json:"snap_max,omitempty" jsonschema:"max woxels to a wall for mount=wall, default 256"`
	Flip         bool    `json:"flip,omitempty" jsonschema:"reverse facing"`
	FallbackFree *bool   `json:"fallback_free,omitempty" jsonschema:"fall back to freestanding if no wall is near, default true"`
	Rotation     float64 `json:"rotation,omitempty"`
	WallID       int     `json:"wall_id,omitempty" jsonschema:"mount on this specific wall id instead of searching"`
}

func (a *app) AddPortal(ctx context.Context, _ *mcp.CallToolRequest, in AddPortalInput) (*mcp.CallToolResult, any, error) {
	params := map[string]any{"asset": in.Asset, "closed": in.Closed, "flip": in.Flip}
	addNonZero(params, "x", in.X)
	addNonZero(params, "y", in.Y)
	addNonZero(params, "radius", in.Radius)
	addNonEmpty(params, "mount", in.Mount)
	addNonZero(params, "snap_max", in.SnapMax)
	if in.FallbackFree != nil {
		params["fallback_free"] = *in.FallbackFree
	}
	addNonZero(params, "rotation", in.Rotation)
	addNonZero(params, "wall_id", in.WallID)
	return a.forward(ctx, "add_portal", params)
}

type AddRoofInput struct {
	Points  [][]float64 `json:"points" jsonschema:"[[x,y],...] the roof's RIDGE line, not a footprint; >= 2 points"`
	Asset   string      `json:"asset"`
	Width   float64     `json:"width,omitempty" jsonschema:"slope run perpendicular to the ridge, default 256"`
	Type    int         `json:"type,omitempty" jsonschema:"roof type: 0=gable,1=hip,2=dormer (default 0)"`
	Sorting int         `json:"sorting,omitempty"`
}

func (a *app) AddRoof(ctx context.Context, _ *mcp.CallToolRequest, in AddRoofInput) (*mcp.CallToolResult, any, error) {
	params := map[string]any{"points": in.Points, "asset": in.Asset}
	addNonZero(params, "width", in.Width)
	addNonZero(params, "type", in.Type)
	addNonZero(params, "sorting", in.Sorting)
	return a.forward(ctx, "add_roof", params)
}

type AddTextInput struct {
	Text  string  `json:"text"`
	X     float64 `json:"x,omitempty"`
	Y     float64 `json:"y,omitempty"`
	Size  int     `json:"size,omitempty"`
	Color string  `json:"color,omitempty"`
	Font  string  `json:"font,omitempty"`
}

func (a *app) AddText(ctx context.Context, _ *mcp.CallToolRequest, in AddTextInput) (*mcp.CallToolResult, any, error) {
	params := map[string]any{"text": in.Text}
	addNonZero(params, "x", in.X)
	addNonZero(params, "y", in.Y)
	addNonZero(params, "size", in.Size)
	addNonEmpty(params, "color", in.Color)
	addNonEmpty(params, "font", in.Font)
	return a.forward(ctx, "add_text", params)
}

type PlacePatternInput struct {
	Asset    string      `json:"asset"`
	Rect     []float64   `json:"rect,omitempty" jsonschema:"[x,y,w,h] — provide this or points, not both"`
	Points   [][]float64 `json:"points,omitempty" jsonschema:"[[x,y],...] polygon — provide this or rect, not both"`
	Category string      `json:"category,omitempty" jsonschema:"default Patterns"`
	Color    string      `json:"color,omitempty"`
	Rotation float64     `json:"rotation,omitempty"`
	Z        int         `json:"z,omitempty" jsonschema:"absolute z-index override (default -100)"`
}

func (a *app) PlacePattern(ctx context.Context, _ *mcp.CallToolRequest, in PlacePatternInput) (*mcp.CallToolResult, any, error) {
	params := map[string]any{"asset": in.Asset}
	if len(in.Rect) > 0 {
		params["rect"] = in.Rect
	}
	if len(in.Points) > 0 {
		params["points"] = in.Points
	}
	addNonEmpty(params, "category", in.Category)
	addNonEmpty(params, "color", in.Color)
	addNonZero(params, "rotation", in.Rotation)
	addNonZero(params, "z", in.Z)
	return a.forward(ctx, "place_pattern", params)
}

type BuildRoomInput struct {
	Rect          []float64   `json:"rect,omitempty" jsonschema:"[x,y,w,h] — provide this or points, not both"`
	Points        [][]float64 `json:"points,omitempty" jsonschema:"[[x,y],...] polygon — provide this or rect, not both"`
	WallAsset     string      `json:"wall_asset,omitempty"`
	Floor         string      `json:"floor,omitempty" jsonschema:"pattern, terrain, or none (default pattern)"`
	FloorAsset    string      `json:"floor_asset,omitempty"`
	FloorCategory string      `json:"floor_category,omitempty" jsonschema:"default Simple Tiles"`
	FloorColor    string      `json:"floor_color,omitempty"`
	FloorSlot     int         `json:"floor_slot,omitempty" jsonschema:"terrain slot when floor=terrain, default 1"`
	WallType      int         `json:"wall_type,omitempty" jsonschema:"wall type: 0=auto,1=manual,2=cave (default 0)"`
	WallJoint     int         `json:"wall_joint,omitempty" jsonschema:"wall joint: 0=sharp,1=bevel,2=round (default 1)"`
}

func (a *app) BuildRoom(ctx context.Context, _ *mcp.CallToolRequest, in BuildRoomInput) (*mcp.CallToolResult, any, error) {
	params := map[string]any{}
	if len(in.Rect) > 0 {
		params["rect"] = in.Rect
	}
	if len(in.Points) > 0 {
		params["points"] = in.Points
	}
	addNonEmpty(params, "wall_asset", in.WallAsset)
	addNonEmpty(params, "floor", in.Floor)
	addNonEmpty(params, "floor_asset", in.FloorAsset)
	addNonEmpty(params, "floor_category", in.FloorCategory)
	addNonEmpty(params, "floor_color", in.FloorColor)
	addNonZero(params, "floor_slot", in.FloorSlot)
	addNonZero(params, "wall_type", in.WallType)
	addNonZero(params, "wall_joint", in.WallJoint)
	return a.forward(ctx, "build_room", params)
}
