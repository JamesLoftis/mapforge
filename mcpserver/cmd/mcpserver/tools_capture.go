package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var (
	screenshotPath = filepath.Join(os.TempDir(), "dd_mcp_screenshot.png")
	exportPath     = filepath.Join(os.TempDir(), "dd_mcp_export.png")
)

func (a *app) Screenshot(ctx context.Context, _ *mcp.CallToolRequest, _ NoArgs) (*mcp.CallToolResult, any, error) {
	if _, err := a.bridge.Request(ctx, "screenshot", map[string]any{"path": screenshotPath}); err != nil {
		return nil, nil, err
	}
	return imageResult(screenshotPath, "try export_map with a low ppi for a smaller full-map view instead")
}

type ExportMapInput struct {
	PPI int `json:"ppi,omitempty" jsonschema:"pixels per tile for the export, default 40, capped at 60"`
}

func (a *app) ExportMap(ctx context.Context, _ *mcp.CallToolRequest, in ExportMapInput) (*mcp.CallToolResult, any, error) {
	// Drop any stale export from a previous call so the wait-for-stable-size
	// poll below can't mistake a leftover file for a freshly finished render.
	if err := os.Remove(exportPath); err != nil && !os.IsNotExist(err) {
		return nil, nil, fmt.Errorf("removing stale export: %w", err)
	}

	if in.PPI > maxExportPPI {
		in.PPI = maxExportPPI
	}
	params := map[string]any{"path": exportPath}
	addNonZero(params, "ppi", in.PPI)
	if _, err := a.bridge.Request(ctx, "export_map", params); err != nil {
		return nil, nil, err
	}
	if err := waitForStableFile(ctx, exportPath, 60*time.Second); err != nil {
		return nil, nil, err
	}
	return imageResult(exportPath, "call it again with a lower ppi")
}

// imageResult reads path as a PNG tool result, refusing (via checkImageSize)
// rather than forwarding an oversized capture into the conversation. hint is
// what the caller should do differently — folded into the size-limit error.
func imageResult(path, hint string) (*mcp.CallToolResult, any, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, nil, fmt.Errorf("reading %s: %w", path, err)
	}
	if err := checkImageSize(path, info.Size(), hint); err != nil {
		return nil, nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.ImageContent{Data: data, MIMEType: "image/png"}},
	}, nil, nil
}

// waitForStableFile polls path until it exists with a non-zero size that is
// unchanged across two consecutive polls (export_map renders on a background
// thread in DD; this is how the caller knows the write finished).
func waitForStableFile(ctx context.Context, path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	last := int64(-1)
	for time.Now().Before(deadline) {
		if info, err := os.Stat(path); err == nil {
			if size := info.Size(); size > 0 && size == last {
				return nil
			} else {
				last = size
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
	return fmt.Errorf("render did not complete within %s (%s)", timeout, path)
}
