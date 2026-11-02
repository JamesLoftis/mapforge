package main

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (a *app) Undo(ctx context.Context, _ *mcp.CallToolRequest, _ NoArgs) (*mcp.CallToolResult, any, error) {
	return a.forward(ctx, "undo", nil)
}

func (a *app) Redo(ctx context.Context, _ *mcp.CallToolRequest, _ NoArgs) (*mcp.CallToolResult, any, error) {
	return a.forward(ctx, "redo", nil)
}
