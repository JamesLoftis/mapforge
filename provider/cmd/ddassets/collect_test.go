package main

import (
	"context"
	"errors"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"testing"
)

type fakeBridge struct {
	assets  map[string][]string // category -> paths
	sizes   map[string][2]int   // path -> picture size; default 256x256
	failing map[string]bool
	exports []string
	closed  bool // Dungeondraft is not running
}

func (f *fakeBridge) ListAssets(_ context.Context, category string) ([]string, error) {
	if f.closed {
		return nil, errors.New("could not reach the Dungeondraft bridge")
	}
	return f.assets[category], nil
}

func (f *fakeBridge) ExportAsset(_ context.Context, category, path, out string) (int, int, error) {
	f.exports = append(f.exports, path)
	if f.failing[path] {
		return 0, 0, errors.New("could not load asset '" + path + "' as a picture")
	}
	size, ok := f.sizes[path]
	if !ok {
		size = [2]int{256, 256}
	}
	file, err := os.Create(out)
	if err != nil {
		return 0, 0, err
	}
	defer file.Close()
	return size[0], size[1], png.Encode(file, image.NewNRGBA(image.Rect(0, 0, size[0], size[1])))
}

func newCollector(t *testing.T, bridge *fakeBridge) (*Collector, *Index) {
	t.Helper()
	ix := openTestIndex(t)
	return &Collector{Index: ix, Bridge: bridge, Dir: t.TempDir(), Log: io.Discard}, ix
}

func TestCollectAddsNewAssetsOnceAndExportsOnlyWhatIsMissing(t *testing.T) {
	bridge := &fakeBridge{assets: map[string][]string{
		"Objects": {"res://a/table.png", "res://a/chair.png"},
		"Walls":   {"res://w/stone.png"},
	}}
	c, ix := newCollector(t, bridge)
	if _, err := c.Collect(context.Background(), CollectOptions{}); err != nil {
		t.Fatal(err)
	}
	if len(bridge.exports) != 3 {
		t.Fatalf("first run exported %v", bridge.exports)
	}
	if _, err := c.Collect(context.Background(), CollectOptions{}); err != nil {
		t.Fatal(err)
	}
	if len(bridge.exports) != 3 {
		t.Errorf("second run exported again: %v", bridge.exports)
	}
	wall, _, _ := ix.Get("res://w/stone.png")
	if wall.Kind != "wall" || wall.Category != "Walls" || wall.Picture == "" || wall.Thumb == "" {
		t.Errorf("wall entry = %+v", wall)
	}
}

func TestCollectSizesObjectsByTheGridAndShrinksThumbnails(t *testing.T) {
	bridge := &fakeBridge{
		assets: map[string][]string{"Objects": {"res://a/banquet.png"}},
		sizes:  map[string][2]int{"res://a/banquet.png": {1024, 512}},
	}
	c, ix := newCollector(t, bridge)
	if _, err := c.Collect(context.Background(), CollectOptions{}); err != nil {
		t.Fatal(err)
	}
	a, _, _ := ix.Get("res://a/banquet.png")
	if a.WidthTiles != 4 || a.HeightTiles != 2 {
		t.Errorf("size = %dx%d tiles; want 4x2", a.WidthTiles, a.HeightTiles)
	}
	thumb, err := os.Open(filepath.Join(c.Dir, "thumbs", a.Thumb))
	if err != nil {
		t.Fatal(err)
	}
	defer thumb.Close()
	cfg, err := png.DecodeConfig(thumb)
	if err != nil || cfg.Width != ThumbSize || cfg.Height != ThumbSize/2 {
		t.Errorf("thumbnail %dx%d (err %v); want %dx%d", cfg.Width, cfg.Height, err, ThumbSize, ThumbSize/2)
	}
}

func TestCollectRecordsAFailureAndCarriesOn(t *testing.T) {
	bridge := &fakeBridge{
		assets:  map[string][]string{"Objects": {"res://a/broken.png", "res://a/fine.png"}},
		failing: map[string]bool{"res://a/broken.png": true},
	}
	c, ix := newCollector(t, bridge)
	report, err := c.Collect(context.Background(), CollectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	broken, _, _ := ix.Get("res://a/broken.png")
	fine, _, _ := ix.Get("res://a/fine.png")
	if broken.Error == "" || broken.Picture != "" {
		t.Errorf("broken = %+v; want its error recorded and no picture", broken)
	}
	if fine.Picture == "" {
		t.Errorf("the asset after the failure was not exported")
	}
	if report.Failed != 1 || report.Pictures != 1 {
		t.Errorf("report = %+v", report)
	}
}

func TestCollectStopsAtTheLimit(t *testing.T) {
	bridge := &fakeBridge{assets: map[string][]string{"Objects": {"res://a/1.png", "res://a/2.png", "res://a/3.png"}}}
	c, _ := newCollector(t, bridge)
	if _, err := c.Collect(context.Background(), CollectOptions{Limit: 2}); err != nil {
		t.Fatal(err)
	}
	if len(bridge.exports) != 2 {
		t.Errorf("exported %d with limit 2", len(bridge.exports))
	}
}
