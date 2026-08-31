// cmd/crudge is a throwaway live-CRUD harness: it runs the exact create/read/
// delete sequence the dungeondraft_wall + dungeondraft_portal resources issue
// against a live bridge, proving wall_id mounting and the delete path, then
// removes everything it made. No residue stays on the map.
//
//	go run ./cmd/crudge 127.0.0.1 8787
package main

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"dungeondraft-mcp/provider/internal/bridge"
)

func main() {
	host, port := "127.0.0.1", 8787
	if len(os.Args) > 1 {
		host = os.Args[1]
	}
	if len(os.Args) > 2 {
		port, _ = strconv.Atoi(os.Args[2])
	}
	c := bridge.New(host, port)
	ctx := context.Background()

	st, err := c.Request(ctx, "get_status", nil)
	die(err, "get_status")
	cx := num(st, "map_center").([]any)[0].(float64)
	cy := num(st, "map_center").([]any)[1].(float64)

	// A short east-facing wall, well away from anything (bridge is live on a
	// map the user owns; offset so it lands near the center).
	x, y := cx, cy
	w, err := c.Request(ctx, "draw_wall", map[string]any{
		"points": []any{[]any{x - 1000, y}, []any{x + 1000, y}},
		"joint":  1,
	})
	die(err, "draw_wall")
	wallID := id(w)
	fmt.Printf("wall id=%d\n", wallID)

	p, err := c.Request(ctx, "add_portal", map[string]any{
		"asset":   "res://textures/portals/door_00.png",
		"wall_id": wallID,
		"radius":  128,
	})
	if err != nil {
		// Asset name may differ icon-set-to-icon-set; try listing to learn.
		fmt.Printf("add_portal failed (%v); deleting test wall\n", err)
		_, _ = c.Request(ctx, "delete_element", map[string]any{"id": wallID})
		os.Exit(1)
	}
	portalID := id(p)
	fmt.Printf("portal id=%d\n", portalID)

	for _, eid := range []float64{float64(wallID), float64(portalID)} {
		el, err := c.Request(ctx, "get_element", map[string]any{"id": eid})
		die(err, "get_element")
		fmt.Printf("get_element %d -> kind=%v kind2=%v\n", int64(eid), el["kind"], el["kind2"])
	}

	for _, eid := range []float64{float64(portalID), float64(wallID)} {
		d, err := c.Request(ctx, "delete_element", map[string]any{"id": eid})
		die(err, "delete_element")
		fmt.Printf("delete %d ok=%v\n", int64(eid), d["deleted"])
	}

	st2, err := c.Request(ctx, "get_status", nil)
	die(err, "get_status")
	fmt.Printf("walls after cleanup: %v (want 0 net change)\n", num(st2, "counts").(map[string]any)["walls"])
}

func id(r map[string]any) int {
	switch v := r["id"].(type) {
	case float64:
		return int(v)
	case string:
		n, _ := strconv.Atoi(v)
		return n
	}
	return -1
}

func num(res map[string]any, key string) any { return res[key] }

func die(err error, label string) {
	if err == nil {
		return
	}
	fmt.Fprintf(os.Stderr, "%s: %v\n", label, err)
	os.Exit(1)
}
