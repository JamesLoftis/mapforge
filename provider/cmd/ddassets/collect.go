package main

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"image"
	"image/png"
	"io"
	"math"
	"os"
	"path/filepath"

	"golang.org/x/image/draw"
)

const (
	// ThumbSize is a thumbnail's longer side, in pixels.
	ThumbSize = 128
	// PixelsPerTile is Dungeondraft's scale: one five-foot tile.
	PixelsPerTile = 256
	logEvery      = 100
)

// Bridge is what collecting needs from Dungeondraft.
type Bridge interface {
	ListAssets(ctx context.Context, category string) ([]string, error)
	ExportAsset(ctx context.Context, category, path, out string) (width, height int, err error)
}

// Collector adds every visible asset to the index and saves its picture.
type Collector struct {
	Index  *Index
	Bridge Bridge
	// Dir holds pictures/ and thumbs/.
	Dir string
	Log io.Writer
}

type CollectOptions struct {
	Limit int    // pictures to export this run; 0 = all
	Kind  string // only this kind; "" = every kind
}

type CollectReport struct{ Added, Pictures, Failed int }

func (c *Collector) Collect(ctx context.Context, opts CollectOptions) (CollectReport, error) {
	var report CollectReport
	for _, category := range IndexedCategories() {
		kind, _ := KindFor(category)
		if opts.Kind != "" && opts.Kind != kind {
			continue
		}
		added, err := c.addNew(ctx, category, kind)
		if err != nil {
			return report, err
		}
		report.Added += added
	}
	pending, err := c.Index.Pending("picture")
	if err != nil {
		return report, err
	}
	for _, dir := range []string{"pictures", "thumbs"} {
		if err := os.MkdirAll(filepath.Join(c.Dir, dir), 0o755); err != nil {
			return report, err
		}
	}
	for _, asset := range pending {
		if opts.Limit > 0 && report.Pictures+report.Failed >= opts.Limit {
			break
		}
		if opts.Kind != "" && asset.Kind != opts.Kind {
			continue
		}
		if err := c.savePicture(ctx, asset); err != nil {
			report.Failed++
			asset.Error = err.Error()
			if err := c.Index.Upsert(asset); err != nil {
				return report, err
			}
		} else {
			report.Pictures++
		}
		if done := report.Pictures + report.Failed; done%logEvery == 0 {
			fmt.Fprintf(c.Log, "%d pictures saved, %d failed\n", report.Pictures, report.Failed)
		}
	}
	fmt.Fprintf(c.Log, "collect: %d new assets, %d pictures saved, %d failed\n", report.Added, report.Pictures, report.Failed)
	return report, nil
}

func (c *Collector) addNew(ctx context.Context, category, kind string) (int, error) {
	paths, err := c.Bridge.ListAssets(ctx, category)
	if err != nil {
		return 0, fmt.Errorf("listing %s: %w", category, err)
	}
	added := 0
	for _, path := range paths {
		if _, found, err := c.Index.Get(path); err != nil {
			return added, err
		} else if found {
			continue
		}
		if err := c.Index.Upsert(Asset{Path: path, Category: category, Kind: kind, Pack: PackOf(path)}); err != nil {
			return added, err
		}
		added++
	}
	return added, nil
}

func (c *Collector) savePicture(ctx context.Context, asset Asset) error {
	name := fileName(asset.Path)
	picture := filepath.Join(c.Dir, "pictures", name)
	width, height, err := c.Bridge.ExportAsset(ctx, asset.Category, asset.Path, picture)
	if err != nil {
		return err
	}
	if err := writeThumbnail(picture, filepath.Join(c.Dir, "thumbs", name)); err != nil {
		return fmt.Errorf("thumbnail for %s: %w", asset.Path, err)
	}
	asset.Picture, asset.Thumb, asset.Error = name, name, ""
	if asset.Kind == "object" {
		asset.WidthTiles, asset.HeightTiles = tiles(width), tiles(height)
	}
	return c.Index.Upsert(asset)
}

// fileName is a stable, filesystem-safe name for an asset's picture.
func fileName(path string) string {
	sum := sha1.Sum([]byte(path))
	return hex.EncodeToString(sum[:]) + ".png"
}

func tiles(pixels int) int {
	return max(1, int(math.Round(float64(pixels)/PixelsPerTile)))
}

func writeThumbnail(picturePath, thumbPath string) error {
	in, err := os.Open(picturePath)
	if err != nil {
		return err
	}
	defer in.Close()
	src, _, err := image.Decode(in)
	if err != nil {
		return err
	}
	b := src.Bounds()
	scale := float64(ThumbSize) / float64(max(b.Dx(), b.Dy()))
	w, h := max(1, int(math.Round(float64(b.Dx())*scale))), max(1, int(math.Round(float64(b.Dy())*scale)))
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Over, nil)
	out, err := os.Create(thumbPath)
	if err != nil {
		return err
	}
	defer out.Close()
	return png.Encode(out, dst)
}
