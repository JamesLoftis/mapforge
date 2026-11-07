//go:build integration

package integration

import (
	"errors"
	"strings"
	"testing"

	bridge "mapforge/bridgeclient"
)

// Required means required:
// the mod refuses a missing or malformed value with code bad_param and runs
// nothing, instead of substituting a default.

// assertRefused sends cmd and checks it is refused as bad_param, with the
// error naming wantInError, and that the map gained no element.
func assertRefused(t *testing.T, cmd string, params map[string]any, wantInError string) {
	t.Helper()
	before := elementCount(t)
	_, err := dd.Request(ctx(t), cmd, params)
	var appErr *bridge.Error
	if !errors.As(err, &appErr) || appErr.Code != "bad_param" {
		t.Errorf("%s %v: got %v; want a bad_param refusal", cmd, params, err)
	} else if !strings.Contains(appErr.Msg, wantInError) {
		t.Errorf("%s %v: refusal %q does not mention %q", cmd, params, appErr.Msg, wantInError)
	}
	if after := elementCount(t); after != before {
		t.Errorf("%s %v: the map went from %d to %d elements; a refusal must create nothing",
			cmd, params, before, after)
	}
}

func assertAccepted(t *testing.T, c *bridge.Client, cmd string, params map[string]any) {
	t.Helper()
	if _, err := c.Request(ctx(t), cmd, params); err != nil {
		t.Errorf("%s %v: %v; want it accepted", cmd, params, err)
	}
}

// elementCount counts every element on the level, pattern shapes included:
// get_status's counts leave patterns out.
func elementCount(t *testing.T) int {
	t.Helper()
	dump, err := dd.Request(ctx(t), "dump_state", nil)
	if err != nil {
		t.Fatalf("dump_state: %v", err)
	}
	return int(dump["count"].(float64))
}

type paramCase struct {
	name        string
	cmd         string
	params      map[string]any
	wantInError string
}

func TestModRefusesBadParams(t *testing.T) {
	scratchMap(t)
	c := ctx(t)
	object, wall, light, pattern := firstAsset(c, t, "Objects"), firstAsset(c, t, "Walls"),
		firstAsset(c, t, "Lights"), firstAsset(c, t, "Patterns")
	portal := firstAsset(c, t, "Portals")
	cases := []paramCase{
		{"object without x", "place_object", map[string]any{"asset": object, "y": 100}, "x"},
		{"light with only x", "add_light", map[string]any{"asset": light, "x": 100}, "y"},
		{"portal without y", "add_portal", map[string]any{"asset": portal, "x": 100, "mount": "free"}, "y"},
		{"text empty", "add_text", map[string]any{"text": "", "x": 100, "y": 100}, "text"},
		{"x not a number", "place_object", map[string]any{"asset": object, "x": "abc", "y": 100}, "x"},
		{"loop not a bool", "draw_wall", map[string]any{"asset": wall, "points": [][]float64{{0, 0}, {500, 0}},
			"loop": "false"}, "loop"},
		{"a bad point", "draw_wall", map[string]any{"asset": wall, "points": []any{[]float64{0, 0}, "bad",
			[]float64{500, 500}}}, "points"},
		{"bad colour", "add_light", map[string]any{"asset": light, "x": 100, "y": 100, "color": "#zzz"}, "color"},
		{"sorting not whole", "place_object", map[string]any{"asset": object, "x": 100, "y": 100,
			"sorting": 2.5}, "sorting"},
		{"pattern without category", "place_pattern", map[string]any{"asset": pattern,
			"rect": []float64{0, 0, 256, 256}}, "category"},
		{"move without y", "move_element", map[string]any{"id": 1, "x": 100}, "y"},
		{"id not a number", "delete_element", map[string]any{"id": "x"}, "id"},
		{"set_level without index", "set_level", map[string]any{}, "index"},
		{"rect of three", "place_pattern", map[string]any{"asset": pattern, "category": "Patterns",
			"rect": []float64{0, 0, 10}}, "rect"},
		// Values that used to be clamped silently (classification section 2).
		{"negative energy", "add_light", map[string]any{"asset": light, "x": 100, "y": 100, "energy": -1}, "energy"},
		{"zero range", "add_light", map[string]any{"asset": light, "x": 100, "y": 100, "range": 0}, "range"},
		{"negative snap_max", "add_portal", map[string]any{"asset": portal, "x": 100, "y": 100,
			"snap_max": -1}, "snap_max"},
		{"negative smoothness", "draw_path", map[string]any{"asset": firstAsset(c, t, "Paths"),
			"points": [][]float64{{0, 0}, {500, 0}}, "smoothness": -1}, "smoothness"},
		{"text size zero", "add_text", map[string]any{"text": "a", "x": 100, "y": 100, "size": 0}, "size"},
		// Godot 3 reads 8 hex digits as #aarrggbb, so this is alpha 0.
		{"invisible pattern", "place_pattern", map[string]any{"asset": pattern, "category": "Patterns",
			"rect": []float64{0, 0, 256, 256}, "color": "#00808080"}, "color"},
		{"negative limit", "list_assets", map[string]any{"limit": -1}, "limit"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { assertRefused(t, tc.cmd, tc.params, tc.wantInError) })
	}
}

func TestModAcceptsGoodParams(t *testing.T) {
	path := scratchMap(t)
	c := ctx(t)
	object, light := firstAsset(c, t, "Objects"), firstAsset(c, t, "Lights")
	// Godot's JSON parser reads every number as a float.
	assertAccepted(t, dd, "place_object", map[string]any{"asset": object, "x": 100.0, "y": 100.0,
		"z": 200.0, "sorting": 0.0, "scale": 1.0, "rotation": 0.0})
	// Envelope keys (map, level, req_id, client) are not parameters.
	assertAccepted(t, boundClient(path), "add_light", map[string]any{"asset": light, "x": 100, "y": 100,
		"level": "Ground"})
	// An unknown extra parameter is ignored, so an older caller keeps working.
	assertAccepted(t, dd, "place_object", map[string]any{"asset": object, "x": 300, "y": 100, "future": 1})
	assertAccepted(t, dd, "list_assets", map[string]any{})
	assertAccepted(t, dd, "list_elements", map[string]any{})
	assertAccepted(t, dd, "add_light", map[string]any{"asset": light, "x": 500, "y": 100,
		"color": []float64{1, 0.9, 0.7}})
}

// Classification ruling J2: a door meant for a wall no longer floats free
// when no wall is near; a freestanding door is asked for with mount "free".
func TestPortalWithNoWallNearbyIsRefused(t *testing.T) {
	scratchMap(t)
	portal := firstAsset(ctx(t), t, "Portals")
	before := elementCount(t)
	_, err := dd.Request(ctx(t), "add_portal", map[string]any{"asset": portal, "x": 5000, "y": 5000})
	if err == nil || !strings.Contains(err.Error(), "snap_max") {
		t.Errorf("add_portal with no wall nearby: got %v; want a refusal naming snap_max", err)
	}
	if after := elementCount(t); after != before {
		t.Errorf("the refused portal left %d new elements", after-before)
	}
	res, err := dd.Request(ctx(t), "add_portal", map[string]any{"asset": portal, "x": 5000, "y": 5000,
		"mount": "free"})
	if err != nil || res["kind"] != "portal" {
		t.Errorf("add_portal with mount free: got %v, %v; want a freestanding portal", res, err)
	}
}

// Classification ruling J3: build_room makes its wall and its floor, or
// nothing.
func TestBuildRoomWithBadFloorCreatesNothing(t *testing.T) {
	scratchMap(t)
	c := ctx(t)
	room := map[string]any{"rect": []float64{1000, 1000, 1024, 1024}, "wall_asset": firstAsset(c, t, "Walls"),
		"floor_category": "Simple Tiles", "floor_asset": "no/such/tile.png"}
	before := elementCount(t)
	if _, err := dd.Request(c, "build_room", room); err == nil || !strings.Contains(err.Error(), "no/such/tile.png") {
		t.Errorf("build_room with a missing floor asset: got %v; want an error naming it", err)
	}
	if after := elementCount(t); after != before {
		t.Errorf("the failed build_room left %d elements behind (its wall)", after-before)
	}
	delete(room, "floor_asset")
	assertRefused(t, "build_room", room, "floor_asset")
}

// Final review: a misspelt mount would slip past J2, and an optional asset
// that is given but cannot be loaded silently became the default.
func TestModRefusesBadMountAndUnloadableAssets(t *testing.T) {
	scratchMap(t)
	c := ctx(t)
	assertRefused(t, "add_portal", map[string]any{"asset": firstAsset(c, t, "Portals"), "x": 5000, "y": 5000,
		"mount": "Wall"}, "mount")
	for _, tc := range []paramCase{
		{"wall", "draw_wall", map[string]any{"asset": "no/such/wall.png", "points": [][]float64{{0, 0}, {500, 0}}},
			"no/such/wall.png"},
		{"light", "add_light", map[string]any{"asset": "no/such/light.png", "x": 100, "y": 100}, "no/such/light.png"},
		{"room wall", "build_room", map[string]any{"rect": []float64{0, 0, 512, 512}, "floor": "none",
			"wall_asset": "no/such/wall.png"}, "no/such/wall.png"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := elementCount(t)
			if _, err := dd.Request(ctx(t), tc.cmd, tc.params); err == nil || !strings.Contains(err.Error(), tc.wantInError) {
				t.Errorf("%s with an unloadable asset: got %v; want an error naming it", tc.cmd, err)
			}
			if after := elementCount(t); after != before {
				t.Errorf("%s with an unloadable asset created %d elements", tc.cmd, after-before)
			}
		})
	}
}
