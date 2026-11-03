package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

const scratchPath = `C:\x y\a.dungeondraft_map`

func newTestClient(t *testing.T, fb *fakeBridge) *Client {
	t.Helper()
	host, port := splitAddr(fb.addr())
	c := New(host, port)
	c.Timeout = 500 * time.Millisecond
	c.Attempts = 1
	return c
}

func statusReply(done bool) string {
	reply := map[string]any{"ok": true, "result": map[string]any{
		"map_open": true,
		"busy":     false,
		"map_file": scratchPath,
		"counts":   map[string]any{"paths": 1, "lights": 2},
		"last_map_op": map[string]any{
			"op": "save_map", "path": scratchPath, "done": done,
		},
	}}
	b, _ := json.Marshal(reply)
	return string(b)
}

func TestStatusParsesMapFields(t *testing.T) {
	c := newTestClient(t, startFakeBridge(t, statusReply(true)))
	got, err := c.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	want := MapStatus{
		MapOpen:   true,
		Busy:      false,
		MapFile:   scratchPath,
		LastMapOp: MapOp{Op: "save_map", Path: scratchPath, Done: true},
		Counts:    map[string]int{"paths": 1, "lights": 2},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Status = %+v, want %+v", got, want)
	}
}

func TestStatusParsesModified(t *testing.T) {
	reply := `{"ok":true,"result":{"map_open":true,"modified":true}}`
	got, err := newTestClient(t, startFakeBridge(t, reply)).Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !got.Modified {
		t.Errorf("Modified = false, want true")
	}
}

func TestWaitMapOpReturnsOpError(t *testing.T) {
	reply := `{"ok":true,"result":{"busy":false,"last_map_op":` +
		`{"op":"save_map","path":"a.dungeondraft_map","done":true,"error":"nothing was written"}}}`
	c := newTestClient(t, startFakeBridge(t, reply))
	err := c.WaitMapOp(context.Background(), "save_map", "a.dungeondraft_map", 5*time.Second)
	if err == nil || !strings.Contains(err.Error(), "nothing was written") {
		t.Fatalf("err = %v, want the op's own error", err)
	}
}

func TestWaitMapOpReturnsWhenDone(t *testing.T) {
	fb := startFakeBridgeSeq(t, []string{statusReply(false), statusReply(false), statusReply(true)})
	c := newTestClient(t, fb)
	if err := c.WaitMapOp(context.Background(), "save_map", scratchPath, 5*time.Second); err != nil {
		t.Fatalf("WaitMapOp: %v", err)
	}
}

func TestWaitMapOpTimesOut(t *testing.T) {
	c := newTestClient(t, startFakeBridge(t, statusReply(false)))
	err := c.WaitMapOp(context.Background(), "save_map", scratchPath, 600*time.Millisecond)
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	for _, part := range []string{"save_map", scratchPath, "timed out"} {
		if !strings.Contains(err.Error(), part) {
			t.Errorf("error %q does not mention %q", err, part)
		}
	}
}

func TestWaitMapOpToleratesOfflineDuringRestart(t *testing.T) {
	offline := `{"ok":false,"error":"dungeondraft offline"}`
	fb := startFakeBridgeSeq(t, []string{offline, offline, statusReply(true)})
	c := newTestClient(t, fb)
	if err := c.WaitMapOp(context.Background(), "save_map", scratchPath, 5*time.Second); err != nil {
		t.Fatalf("WaitMapOp: %v", err)
	}
}

func TestSaveMapRejectionSurfaces(t *testing.T) {
	c := newTestClient(t, startFakeBridge(t, `{"ok":false,"error":"busy"}`))
	err := c.SaveMap(context.Background(), scratchPath)
	var appErr *Error
	if !errors.As(err, &appErr) || !strings.Contains(err.Error(), "busy") {
		t.Fatalf("SaveMap error = %v, want *Error mentioning busy", err)
	}
}
