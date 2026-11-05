//go:build integration

package integration

import (
	"strings"
	"testing"
)

// Spec finding 7: the mod's undo/redo stacks are removed, along with both
// commands; the command log replaces what history was wanted for.
func TestUndoAndRedoAreGone(t *testing.T) {
	scratchMap(t)
	for _, cmd := range []string{"undo", "redo"} {
		_, err := dd.Request(ctx(t), cmd, nil)
		if err == nil || !strings.Contains(err.Error(), "unknown cmd") {
			t.Errorf("%s: got error %v; want an unknown cmd error", cmd, err)
		}
	}
}
