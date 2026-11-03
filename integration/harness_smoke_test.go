//go:build integration

package integration

import (
	"errors"
	"strings"
	"testing"

	bridge "dungeondraft-mcp/bridgeclient"
)

func TestScratchMapRoundTrip(t *testing.T) {
	path := scratchMap(t)
	saveAndReopen(t, path)

	st, err := dd.Status(ctx(t))
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st.MapFile != path {
		t.Errorf("map_file = %q, want %q", st.MapFile, path)
	}
	if !strings.Contains(path, " ") {
		t.Errorf("scratch path %q has no space; it must exercise one", path)
	}
}

func TestPreconditionRejectsRealMap(t *testing.T) {
	err := checkPreconditions(bridgePing{DDConnected: true}, bridge.MapStatus{
		MapOpen: true,
		MapFile: `C:\maps\real.dungeondraft_map`,
	})
	if err == nil || !strings.Contains(err.Error(), "non-scratch map") {
		t.Fatalf("err = %v, want it to mention a non-scratch map", err)
	}
}

func TestPreconditionRejectsUnsavedMapWithContent(t *testing.T) {
	err := checkPreconditions(bridgePing{DDConnected: true}, bridge.MapStatus{
		MapOpen: true,
		Counts:  map[string]int{"walls": 3},
	})
	if err == nil || !strings.Contains(err.Error(), "unsaved map") {
		t.Fatalf("err = %v, want it to mention an unsaved map", err)
	}
}

func TestPreconditionRejectsModifiedUnsavedMap(t *testing.T) {
	err := checkPreconditions(bridgePing{DDConnected: true}, bridge.MapStatus{
		MapOpen:  true,
		Modified: true,
	})
	if err == nil || !strings.Contains(err.Error(), "unsaved changes") {
		t.Fatalf("err = %v, want it to mention unsaved changes", err)
	}
}

func TestPreconditionRejectsStaleMod(t *testing.T) {
	err := checkPreconditions(bridgePing{DDConnected: true, Protocol: 19, AgentProtocol: 18}, bridge.MapStatus{})
	if err == nil || !strings.Contains(err.Error(), "v18") || !strings.Contains(err.Error(), "restart Dungeondraft") {
		t.Fatalf("err = %v, want it to name v18 and say to restart Dungeondraft", err)
	}
}

func TestPreconditionAllowsEmptyUnsavedMap(t *testing.T) {
	err := checkPreconditions(bridgePing{DDConnected: true}, bridge.MapStatus{
		MapOpen: true,
		Counts:  map[string]int{"walls": 0, "objects": 0},
	})
	if err != nil {
		t.Fatalf("err = %v, want nil for an empty unsaved map", err)
	}
}

func TestBridgeDownMessage(t *testing.T) {
	err := checkPreconditions(bridgePing{Err: errors.New("connection refused")}, bridge.MapStatus{})
	if err == nil || !strings.Contains(err.Error(), "dd-launch") {
		t.Fatalf("err = %v, want it to mention dd-launch", err)
	}
}

func TestDungeondraftNotConnectedMessage(t *testing.T) {
	err := checkPreconditions(bridgePing{DDConnected: false}, bridge.MapStatus{})
	if err == nil || !strings.Contains(err.Error(), "TF Bridge Agent") {
		t.Fatalf("err = %v, want it to name the TF Bridge Agent mod", err)
	}
}

func TestMapOpFailureNamesMapAndKinds(t *testing.T) {
	msg := mapOpFailure("save_map", `C:\t\a b\m.dungeondraft_map`, errors.New("timed out"),
		bridge.MapStatus{Counts: map[string]int{"paths": 1, "lights": 2}})
	for _, want := range []string{"save_map", `C:\t\a b\m.dungeondraft_map`, "timed out", "paths=1", "lights=2"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q does not mention %q", msg, want)
		}
	}
}
