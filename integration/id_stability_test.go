//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"testing"
)

// positionTolerance is how far (in woxels) a reopened element may drift.
const positionTolerance = 1.0

// createdElement is one element made for the id-stability experiment.
type createdElement struct {
	label string
	id    any
}

// TestElementIDsSurviveSaveAndReopen answers the spec's first check: do the
// ids Terraform stores still name the same elements after Dungeondraft
// saves and reopens the map?
func TestElementIDsSurviveSaveAndReopen(t *testing.T) {
	path := scratchMap(t)
	c := ctx(t)
	elements := createOneOfEachKind(c, t)

	before := describeAll(c, t, elements)
	addLevel(c, t, upperLevel)
	if _, err := dd.Request(c, "add_light", map[string]any{"x": 100, "y": 100, "level": upperLevel}); err != nil {
		t.Fatalf("add_light on %s: %v", upperLevel, err)
	}
	saveAndReopen(t, path)

	// Level ids are renumbered by position on load; a level's identity is
	// its label (spec, 2026-11-03), so the label and what is on it must survive.
	t.Run("level", func(t *testing.T) {
		if n := labelCount(levelLabels(ctx(t), t), upperLevel); n != 1 {
			t.Fatalf("%d levels are labelled %q after reopen, want 1", n, upperLevel)
		}
		if n := lightsOn(t, upperLevel); n != 1 {
			t.Errorf("%s holds %d lights after reopen, want 1", upperLevel, n)
		}
	})

	for _, el := range elements {
		t.Run(el.label, func(t *testing.T) {
			after, err := dd.Request(ctx(t), "get_element", map[string]any{"id": el.id})
			if err != nil {
				t.Fatalf("id %v no longer resolves after reopen: %v\nbefore: %s", el.id, err, asJSON(before[el.label]))
			}
			if !isSameElement(before[el.label], after) {
				t.Errorf("id %v changed across save and reopen\nbefore: %s\nafter:  %s",
					el.id, asJSON(before[el.label]), asJSON(after))
			}
		})
	}
}

func createOneOfEachKind(c context.Context, t *testing.T) []createdElement {
	t.Helper()
	var made []createdElement
	create := func(label, cmd string, params map[string]any) map[string]any {
		res, err := dd.Request(c, cmd, params)
		if err != nil {
			t.Fatalf("%s: %v", cmd, err)
		}
		if id, ok := res["id"]; ok {
			made = append(made, createdElement{label: label, id: id})
		}
		return res
	}

	create("object", "place_object", map[string]any{"asset": firstAsset(c, t, "Objects"),
		"x": 1000, "y": 1000, "scale": 1, "rotation": 0, "sorting": 0})
	wall := create("wall", "draw_wall", map[string]any{"asset": firstAsset(c, t, "Walls"),
		"points": [][]float64{{600, 600}, {1600, 600}}, "loop": false, "shadow": true, "type": 0, "joint": 1})
	create("wall_portal", "add_portal", map[string]any{"asset": firstAsset(c, t, "Portals"),
		"x": 1100, "y": 600, "wall_id": wall["id"], "closed": false, "radius": 64, "mount": "wall",
		"flip": false, "rotation": 0})
	create("light", "add_light", map[string]any{"asset": firstAsset(c, t, "Lights"),
		"x": 1500, "y": 1500, "color": "#ffe6b3", "energy": 1, "range": 1, "shadows": true})
	create("path", "draw_path", map[string]any{"asset": firstAsset(c, t, "Paths"),
		"points": [][]float64{{500, 2000}, {1800, 2400}}, "layer": 0, "sorting": 0, "smoothness": 0})
	create("pattern", "place_pattern", map[string]any{"asset": firstAsset(c, t, "Patterns"),
		"rect": []float64{2000, 2000, 512, 512}, "category": "Patterns", "color": "#808080", "rotation": 0})
	create("roof", "add_roof", map[string]any{"asset": firstAsset(c, t, "Roofs"),
		"points": [][]float64{{2000, 3200}, {3000, 3200}}, "width": 256, "type": 0, "sorting": 0})
	create("text", "add_text", map[string]any{"text": "id test", "x": 3000, "y": 1000,
		"size": 40, "color": "#000000"})

	room := create("room", "build_room", map[string]any{"rect": []float64{3500, 3500, 1024, 1024},
		"wall_asset": firstAsset(c, t, "Walls"), "floor": "pattern",
		"floor_asset": firstAsset(c, t, "Simple Tiles"), "floor_category": "Simple Tiles",
		"wall_type": 0, "wall_joint": 1})
	t.Logf("build_room returned %s", asJSON(room))
	made = append(made, createdElement{label: "room_wall", id: room["wall_id"]})
	if floor, ok := room["floor_id"]; ok {
		made = append(made, createdElement{label: "room_floor", id: floor})
	}
	return made
}

func addLevel(c context.Context, t *testing.T, label string) {
	t.Helper()
	if _, err := dd.Request(c, "add_level", map[string]any{"label": label}); err != nil {
		t.Fatalf("add_level: %v", err)
	}
}

func labelCount(labels map[string]string, label string) int {
	n := 0
	for _, l := range labels {
		if l == label {
			n++
		}
	}
	return n
}

func levelLabels(c context.Context, t *testing.T) map[string]string {
	t.Helper()
	res, err := dd.Request(c, "list_levels", nil)
	if err != nil {
		t.Fatalf("list_levels: %v", err)
	}
	labels := map[string]string{}
	levels, _ := res["levels"].([]any)
	for _, raw := range levels {
		lv, _ := raw.(map[string]any)
		labels[fmt.Sprint(lv["id"])] = fmt.Sprint(lv["label"])
	}
	return labels
}

func firstAsset(c context.Context, t *testing.T, category string) string {
	t.Helper()
	res, err := dd.Request(c, "list_assets", map[string]any{"category": category, "limit": 1})
	if err != nil {
		t.Fatalf("list_assets %s: %v", category, err)
	}
	assets, _ := res["assets"].([]any)
	if len(assets) == 0 {
		t.Fatalf("no %s assets: %s", category, asJSON(res))
	}
	return assets[0].(string)
}

func describeAll(c context.Context, t *testing.T, elements []createdElement) map[string]map[string]any {
	t.Helper()
	described := make(map[string]map[string]any, len(elements))
	for _, el := range elements {
		d, err := dd.Request(c, "get_element", map[string]any{"id": el.id})
		if err != nil {
			t.Fatalf("get_element %s (id %v) before save: %v", el.label, el.id, err)
		}
		described[el.label] = d
	}
	return described
}

func isSameElement(before, after map[string]any) bool {
	return before["kind"] == after["kind"] && isNear(before["position"], after["position"])
}

func isNear(a, b any) bool {
	pa, okA := a.([]any)
	pb, okB := b.([]any)
	if !okA || !okB {
		return okA == okB
	}
	if len(pa) != 2 || len(pb) != 2 {
		return false
	}
	for i := range pa {
		if math.Abs(pa[i].(float64)-pb[i].(float64)) > positionTolerance {
			return false
		}
	}
	return true
}

func asJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
