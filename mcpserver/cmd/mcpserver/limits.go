package main

import "fmt"

// Ceilings on how much a single tool call can hand back to the model. These
// exist only at the MCP boundary (bridge_app and provider are untouched —
// provider's Terraform state reads legitimately need the full, untruncated
// data). Nothing else here rate-limits the model; a single dump_state or
// export_map call on a large map is otherwise big enough to blow a whole
// context window in one shot.
const (
	// maxListLimit caps list_assets/list_elements' limit param. Both already
	// report total/matched vs returned, so clamping loses no information —
	// the caller can tell more exists and page with search/kind filters.
	maxListLimit = 300

	// maxDumpStateElements is the largest level dump_state will hand back
	// through MCP. Above this it refuses rather than silently truncating —
	// a partial "whole level" dump is worse than an error, since nothing
	// marks which elements were dropped.
	maxDumpStateElements = 500

	// maxExportPPI caps export_map's requested resolution.
	maxExportPPI = 60

	// maxImageBytes is a backstop on the two capture tools regardless of
	// what produced the file (a huge map even at maxExportPPI, a very
	// high-DPI DD window for screenshot).
	maxImageBytes = 12 * 1024 * 1024
)

func clampLimit(limit int) int {
	if limit > maxListLimit {
		return maxListLimit
	}
	return limit
}

// checkImageSize refuses an oversized capture instead of forwarding
// megabytes of base64 into the conversation.
func checkImageSize(path string, size int64, hint string) error {
	if size > maxImageBytes {
		return fmt.Errorf("%s is %.1fMB, over the %dMB limit for a single capture — %s", path, float64(size)/(1<<20), maxImageBytes/(1<<20), hint)
	}
	return nil
}
