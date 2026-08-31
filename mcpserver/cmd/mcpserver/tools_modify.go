package main

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type MoveElementInput struct {
	ID int     `json:"id"`
	X  float64 `json:"x"`
	Y  float64 `json:"y"`
}

func (a *app) MoveElement(ctx context.Context, _ *mcp.CallToolRequest, in MoveElementInput) (*mcp.CallToolResult, any, error) {
	return a.forward(ctx, "move_element", map[string]any{"id": in.ID, "x": in.X, "y": in.Y})
}

type ModifyObjectInput struct {
	ID       int     `json:"id"`
	Scale    float64 `json:"scale,omitempty" jsonschema:"omit to leave unchanged"`
	Rotation float64 `json:"rotation,omitempty" jsonschema:"degrees; omit to leave unchanged"`
	Color    string  `json:"color,omitempty" jsonschema:"omit to leave unchanged"`
	Shadow   *bool   `json:"shadow,omitempty" jsonschema:"omit to leave unchanged"`
}

func (a *app) ModifyObject(ctx context.Context, _ *mcp.CallToolRequest, in ModifyObjectInput) (*mcp.CallToolResult, any, error) {
	params := map[string]any{"id": in.ID}
	addNonZero(params, "scale", in.Scale)
	addNonZero(params, "rotation", in.Rotation)
	addNonEmpty(params, "color", in.Color)
	if in.Shadow != nil {
		params["shadow"] = *in.Shadow
	}
	return a.forward(ctx, "modify_object", params)
}

type DuplicateObjectInput struct {
	ID int     `json:"id"`
	DX float64 `json:"dx,omitempty" jsonschema:"x offset for the copy, default 64"`
	DY float64 `json:"dy,omitempty" jsonschema:"y offset for the copy, default 0"`
}

func (a *app) DuplicateObject(ctx context.Context, _ *mcp.CallToolRequest, in DuplicateObjectInput) (*mcp.CallToolResult, any, error) {
	params := map[string]any{"id": in.ID}
	addNonZero(params, "dx", in.DX)
	addNonZero(params, "dy", in.DY)
	return a.forward(ctx, "duplicate_object", params)
}

type DeleteElementInput struct {
	ID int `json:"id"`
}

func (a *app) DeleteElement(ctx context.Context, _ *mcp.CallToolRequest, in DeleteElementInput) (*mcp.CallToolResult, any, error) {
	return a.forward(ctx, "delete_element", map[string]any{"id": in.ID})
}

type AddLevelInput struct {
	Label string `json:"label,omitempty" jsonschema:"default Level"`
}

func (a *app) AddLevel(ctx context.Context, _ *mcp.CallToolRequest, in AddLevelInput) (*mcp.CallToolResult, any, error) {
	params := map[string]any{}
	addNonEmpty(params, "label", in.Label)
	return a.forward(ctx, "add_level", params)
}

type SetLevelInput struct {
	Index int `json:"index"`
}

func (a *app) SetLevel(ctx context.Context, _ *mcp.CallToolRequest, in SetLevelInput) (*mcp.CallToolResult, any, error) {
	return a.forward(ctx, "set_level", map[string]any{"index": in.Index})
}

type SelectElementsInput struct {
	IDs []int `json:"ids"`
}

func (a *app) SelectElements(ctx context.Context, _ *mcp.CallToolRequest, in SelectElementsInput) (*mcp.CallToolResult, any, error) {
	return a.forward(ctx, "select_elements", map[string]any{"ids": in.IDs})
}

func (a *app) ClearSelection(ctx context.Context, _ *mcp.CallToolRequest, _ NoArgs) (*mcp.CallToolResult, any, error) {
	return a.forward(ctx, "clear_selection", nil)
}
