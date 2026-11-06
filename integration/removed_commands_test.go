//go:build integration

package integration

import (
	"strings"
	"testing"
)

// Classification ruling J5: no client sends these any more (the MCP server is
// look-only and Terraform never did), so the mod no longer answers them.
func TestRemovedCommandsAreUnknown(t *testing.T) {
	scratchMap(t)
	for _, cmd := range []string{"duplicate_object", "select_elements", "clear_selection"} {
		_, err := dd.Request(ctx(t), cmd, map[string]any{"id": 1, "ids": []int{1}})
		if err == nil || !strings.Contains(err.Error(), "unknown cmd") {
			t.Errorf("%s: got %v; want \"unknown cmd\"", cmd, err)
		}
	}
}
