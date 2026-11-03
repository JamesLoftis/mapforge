package bridge

import (
	"context"
	"fmt"
	"time"
)

// mapOpPollInterval is how often WaitMapOp re-reads get_status.
const mapOpPollInterval = 250 * time.Millisecond

// MapStatus is the map-lifecycle part of get_status.
type MapStatus struct {
	MapOpen   bool
	Busy      bool
	MapFile   string
	LastMapOp MapOp
}

// MapOp is the most recent new_map/open_map/save_map the mod started.
type MapOp struct {
	Op   string
	Path string
	Done bool
}

func (op MapOp) isDone(name, path string) bool {
	return op.Done && op.Op == name && op.Path == path
}

// Status reads the map-lifecycle fields of get_status.
func (c *Client) Status(ctx context.Context) (MapStatus, error) {
	res, err := c.Request(ctx, "get_status", nil)
	if err != nil {
		return MapStatus{}, err
	}
	op, _ := res["last_map_op"].(map[string]any)
	return MapStatus{
		MapOpen: res["map_open"] == true,
		Busy:    res["busy"] == true,
		MapFile: stringField(res, "map_file"),
		LastMapOp: MapOp{
			Op:   stringField(op, "op"),
			Path: stringField(op, "path"),
			Done: op["done"] == true,
		},
	}, nil
}

func stringField(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

// NewMap asks Dungeondraft to replace the open map with a blank one of the
// given size in tiles. It returns once the command is accepted; follow it
// with WaitMapOp("new_map", "").
func (c *Client) NewMap(ctx context.Context, widthTiles, heightTiles int) error {
	_, err := c.Request(ctx, "new_map", map[string]any{"width": widthTiles, "height": heightTiles})
	return err
}

// OpenMap asks Dungeondraft to open the map file at path. It returns once
// the command is accepted; follow it with WaitMapOp("open_map", path).
func (c *Client) OpenMap(ctx context.Context, path string) error {
	_, err := c.Request(ctx, "open_map", map[string]any{"path": path})
	return err
}

// SaveMap asks Dungeondraft to save the open map to path. It returns once
// the command is accepted; follow it with WaitMapOp("save_map", path).
func (c *Client) SaveMap(ctx context.Context, path string) error {
	_, err := c.Request(ctx, "save_map", map[string]any{"path": path})
	return err
}

// WaitMapOp polls Status until the last map op is op on path, done, and
// Dungeondraft is not busy. open_map and new_map restart the mod, so a
// Status error during the wait counts as "not yet"; the last one is
// reported if the wait times out.
func (c *Client) WaitMapOp(ctx context.Context, op, path string, timeout time.Duration) error {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var lastErr error
	for {
		st, err := c.Status(ctx)
		if err == nil && st.LastMapOp.isDone(op, path) && !st.Busy {
			return nil
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return fmt.Errorf("%s \"%s\" timed out after %s (last status error: %v)",
				op, path, time.Since(start).Round(time.Millisecond), lastErr)
		case <-time.After(mapOpPollInterval):
		}
	}
}
