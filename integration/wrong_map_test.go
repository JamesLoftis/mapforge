//go:build integration

package integration

import (
	"reflect"
	"strings"
	"testing"
)

// TestRefreshOnAnotherMapKeepsState: state belongs to one map. With a
// different map open, a refresh must fail and leave state alone, not find
// every element "gone".
func TestRefreshOnAnotherMapKeepsState(t *testing.T) {
	tf := newTerraform(t)
	tf.mapFile = scratchMap(t)
	tf.run(t, "init", "-input=false")
	tf.run(t, "apply", "-auto-approve", "-input=false")
	before := tf.stateList(t)

	scratchMap(t)
	code, out := tf.output(t, "apply", "-refresh-only", "-auto-approve", "-input=false",
		"-target=dungeondraft_text.text")

	if code == 0 || !strings.Contains(out, "is for") {
		t.Errorf("refresh on another map exited %d; want a failure saying the command is for %s",
			code, tf.mapFile)
	}
	if after := tf.stateList(t); !reflect.DeepEqual(after, before) {
		t.Errorf("state changed while another map was open:\nbefore %v\nafter  %v", before, after)
	}
}
