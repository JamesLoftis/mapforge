package main

import (
	"path/filepath"
	"testing"
)

// Final review I3: a command log that cannot be opened must not stop
// bridge_app; commands run without it.
func TestUnopenableCommandLogRunsWithoutOne(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no such folder", "commands.jsonl")
	w := commandLogOrNone(path)
	if w != nil {
		t.Fatalf("commandLogOrNone(%q) = %v; want nil (no command log)", path, w)
	}
}
