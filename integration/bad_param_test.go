//go:build integration

package integration

import (
	"errors"
	"strings"
	"testing"

	bridge "dungeondraft-mcp/bridgeclient"
)

// Plan 4 (spec goal 3, docs/superpowers/specs/2026-11-05-mod-fallback-classification.md):
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

func elementCount(t *testing.T) int {
	t.Helper()
	status, err := dd.Status(ctx(t))
	if err != nil {
		t.Fatalf("get_status: %v", err)
	}
	return totalElements(status)
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
