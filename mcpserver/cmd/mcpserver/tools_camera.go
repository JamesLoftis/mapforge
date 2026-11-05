package main

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (a *app) GetCamera(ctx context.Context, _ *mcp.CallToolRequest, _ NoArgs) (*mcp.CallToolResult, any, error) {
	return a.forward(ctx, "get_camera", nil)
}

type SetCameraInput struct {
	X    float64 `json:"x,omitempty"`
	Y    float64 `json:"y,omitempty"`
	Zoom float64 `json:"zoom,omitempty" jsonschema:"larger = zoomed out (0.5 magnifies 2x, 2.0 shows 2x more)"`
}

func (a *app) SetCamera(ctx context.Context, _ *mcp.CallToolRequest, in SetCameraInput) (*mcp.CallToolResult, any, error) {
	params := map[string]any{}
	addNonZero(params, "x", in.X)
	addNonZero(params, "y", in.Y)
	addNonZero(params, "zoom", in.Zoom)
	return a.forward(ctx, "set_camera", params)
}

type FocusElementInput struct {
	ID   int     `json:"id"`
	Zoom float64 `json:"zoom,omitempty"`
}

func (a *app) FocusElement(ctx context.Context, _ *mcp.CallToolRequest, in FocusElementInput) (*mcp.CallToolResult, any, error) {
	params := map[string]any{"id": in.ID}
	addNonZero(params, "zoom", in.Zoom)
	return a.forward(ctx, "focus_element", params)
}

type FitElementsInput struct {
	IDs []int   `json:"ids"`
	Pad float64 `json:"pad,omitempty" jsonschema:"fractional padding around the bounds, default 0.15"`
}

func (a *app) FitElements(ctx context.Context, _ *mcp.CallToolRequest, in FitElementsInput) (*mcp.CallToolResult, any, error) {
	params := map[string]any{"ids": in.IDs}
	addNonZero(params, "pad", in.Pad)
	return a.forward(ctx, "fit_elements", params)
}

type SetLevelInput struct {
	Index int `json:"index"`
}

// SetLevel switches which level the editor shows; like the camera, it
// changes the view, not the map.
func (a *app) SetLevel(ctx context.Context, _ *mcp.CallToolRequest, in SetLevelInput) (*mcp.CallToolResult, any, error) {
	return a.forward(ctx, "set_level", map[string]any{"index": in.Index})
}
