//go:build integration

package integration

import (
	"errors"
	"testing"

	bridge "mapforge/bridgeclient"
)

const (
	upperLevel  = "Upper"
	groundLevel = "Ground" // every new map's first level
)

func TestElementLandsOnNamedLevelAndActiveLevelRestored(t *testing.T) {
	scratchMap(t)
	addLevel(ctx(t), t, upperLevel)
	before := activeLevel(t)

	if _, err := dd.Request(ctx(t), "add_light", map[string]any{"x": 100, "y": 100, "level": upperLevel}); err != nil {
		t.Fatalf("add_light on %s: %v", upperLevel, err)
	}

	if after := activeLevel(t); after != before {
		t.Errorf("active level changed from %d to %d; the command must restore it", before, after)
	}
	if n := lightsOn(t, groundLevel); n != 0 {
		t.Errorf("%s holds %d lights; the light belongs on %s", groundLevel, n, upperLevel)
	}
	if n := lightsOn(t, upperLevel); n != 1 {
		t.Errorf("%s holds %d lights, want 1", upperLevel, n)
	}
}

func TestDuplicateLevelLabelRefused(t *testing.T) {
	scratchMap(t)
	addLevel(ctx(t), t, upperLevel)
	_, err := dd.Request(ctx(t), "add_level", map[string]any{"label": upperLevel})
	if code := errorCode(err); code != "label_exists" {
		t.Fatalf("second add_level %q: err %v (code %q), want code label_exists", upperLevel, err, code)
	}
}

func TestUnknownLevelLabelRefused(t *testing.T) {
	scratchMap(t)
	_, err := dd.Request(ctx(t), "add_light", map[string]any{"x": 100, "y": 100, "level": "Nowhere"})
	if code := errorCode(err); code != "level_not_found" {
		t.Fatalf("add_light on an unknown level: err %v (code %q), want code level_not_found", err, code)
	}
	if n := lightsOn(t, groundLevel); n != 0 {
		t.Errorf("a refused add_light still placed %d lights", n)
	}
}

func errorCode(err error) string {
	var appErr *bridge.Error
	if errors.As(err, &appErr) {
		return appErr.Code
	}
	return ""
}

// activeLevel is the index of the level the Dungeondraft UI has active.
func activeLevel(t *testing.T) int {
	t.Helper()
	res, err := dd.Request(ctx(t), "get_status", nil)
	if err != nil {
		t.Fatalf("get_status: %v", err)
	}
	n, _ := res["level_id"].(float64)
	return int(n)
}

// lightsOn counts the lights on the level labelled label, via get_status's
// per-level counts on that level.
func lightsOn(t *testing.T, label string) int {
	t.Helper()
	res, err := dd.Request(ctx(t), "get_status", map[string]any{"level": label})
	if err != nil {
		t.Fatalf("get_status on %s: %v", label, err)
	}
	counts, _ := res["counts"].(map[string]any)
	n, _ := counts["lights"].(float64)
	return int(n)
}
