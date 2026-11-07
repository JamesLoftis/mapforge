//go:build integration

package integration

import (
	"bytes"
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	bridge "mapforge/bridgeclient"
)

// The asset index needs a picture of every asset, including the ones sealed
// inside Dungeondraft's own files and add-on packs.
func TestExportAssetSavesAPicture(t *testing.T) {
	for _, asset := range []string{
		"res://textures/objects/furniture/tables/round_table_01.png",
		"res://packs/DnDgCORE/textures/objects/[DnD] Lighting/candle_small_dish_clay.webp",
	} {
		t.Run(filepath.Base(asset), func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "asset.png")
			reply, err := dd.Request(ctx(t), "export_asset", map[string]any{"category": "Objects", "path": asset, "out": out})
			if err != nil {
				t.Fatalf("export_asset %s: %v", asset, err)
			}
			raw, err := os.ReadFile(out)
			if err != nil || !bytes.HasPrefix(raw, []byte("\x89PNG")) {
				t.Fatalf("no PNG at %s (err %v)", out, err)
			}
			cfg, err := png.DecodeConfig(bytes.NewReader(raw))
			if err != nil {
				t.Fatalf("decoding %s: %v", out, err)
			}
			if float64(cfg.Width) != reply["width"].(float64) || float64(cfg.Height) != reply["height"].(float64) {
				t.Errorf("reply says %vx%v, file is %dx%d", reply["width"], reply["height"], cfg.Width, cfg.Height)
			}
		})
	}
}

func TestExportAssetRefusesAMissingOut(t *testing.T) {
	_, err := dd.Request(ctx(t), "export_asset", map[string]any{"category": "Objects", "path": "res://textures/objects/furniture/tables/round_table_01.png"})
	var appErr *bridge.Error
	if !errors.As(err, &appErr) || appErr.Code != "bad_param" || !strings.Contains(appErr.Msg, "out") {
		t.Errorf("export_asset without out: got %v; want a bad_param refusal naming out", err)
	}
}

func TestExportAssetNamesAnAssetItCannotLoad(t *testing.T) {
	_, err := dd.Request(ctx(t), "export_asset", map[string]any{
		"category": "Objects", "path": "res://textures/objects/no_such_thing.png", "out": filepath.Join(t.TempDir(), "x.png"),
	})
	if err == nil || !strings.Contains(err.Error(), "no_such_thing.png") {
		t.Errorf("export_asset of a missing asset: got %v; want an error naming it", err)
	}
}
