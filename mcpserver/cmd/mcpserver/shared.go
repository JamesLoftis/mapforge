package main

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// NoArgs is the input type for tools that take no parameters.
type NoArgs struct{}

// forward runs a bridge command and hands the result straight back as the
// tool's structured output; a bridge rejection or transport failure becomes
// the handler's error, which the SDK turns into an IsError tool result.
func (a *app) forward(ctx context.Context, cmd string, params map[string]any) (*mcp.CallToolResult, any, error) {
	result, err := a.bridge.Request(ctx, cmd, params)
	if err != nil {
		return nil, nil, err
	}
	return nil, result, nil
}

func addTool[In any](s *mcp.Server, name, description string, h mcp.ToolHandlerFor[In, any]) {
	mcp.AddTool(s, &mcp.Tool{Name: name, Description: description}, h)
}

// addNonZero/addNonEmpty set params[key] only when the caller actually gave a
// value — a plain (non-pointer) field can't tell "0"/"" apart from "omitted",
// but every field these are used for either defaults to 0/"" on the bridge
// side too (so the two are indistinguishable in effect) or the bridge itself
// rejects a literal 0 anyway (radius/width must be > 0), so falling back to
// the bridge's own default is the more useful behavior either way. Fields
// where an explicit false/0 must be distinguished from "unset" (documented
// bridge default of true, or true/false both being meaningful) use *bool or
// are always sent — see the tool definitions.
func addNonEmpty(params map[string]any, key, v string) {
	if v != "" {
		params[key] = v
	}
}

func addNonZero[T int | float64](params map[string]any, key string, v T) {
	if v != 0 {
		params[key] = v
	}
}
