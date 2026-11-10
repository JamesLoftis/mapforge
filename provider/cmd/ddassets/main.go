// Command ddassets builds and keeps the asset index: every Dungeondraft asset
// you can see on a map, with its picture, size, description and tags.
//
//	ddassets collect  [-limit N] [-kind K]   add new assets and save their pictures
//	ddassets describe [-limit N] [-model M]  draft descriptions and tags (SpriteForge; Claude by default)
//	ddassets describe [-model M] <path> ...  describe just these assets, collecting any that are new
//
// The index is %LOCALAPPDATA%\DungeondraftBridge\assets\index.db, with
// pictures\ and thumbs\ beside it (-dir overrides the folder).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	bridge "mapforge/bridgeclient"
)

const (
	bridgeHost     = "127.0.0.1"
	bridgePort     = 8787
	requestTimeout = 30 * time.Second
	listAllLimit   = 100000
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "ddassets:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: ddassets collect|describe [flags]")
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	limit := flags.Int("limit", 0, "assets to process this run (0 = all)")
	kind := flags.String("kind", "", "only this kind (object, wall, floor tile, ...)")
	dir := flags.String("dir", defaultDir(), "folder holding index.db, pictures and thumbs")
	model := flags.String("model", "claude", "describe: SpriteForge's describe model (claude, or \"\" for its local default)")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if err := os.MkdirAll(*dir, 0o755); err != nil {
		return err
	}
	ix, err := OpenIndex(filepath.Join(*dir, "index.db"))
	if err != nil {
		return err
	}
	defer ix.Close()
	ctx := context.Background()
	collector := &Collector{Index: ix, Bridge: liveBridge{bridge.New(bridgeHost, bridgePort)}, Dir: *dir, Log: os.Stdout}
	switch args[0] {
	case "collect":
		_, err = collector.Collect(ctx, CollectOptions{Limit: *limit, Kind: *kind})
		return err
	case "describe":
		d, err := liveDescriber(collector, *model)
		if err != nil {
			return err
		}
		if named := flags.Args(); len(named) > 0 {
			return d.DescribePaths(ctx, named)
		}
		return d.Describe(ctx, *limit)
	default:
		return fmt.Errorf("unknown command %q; want collect or describe", args[0])
	}
}

func defaultDir() string {
	return filepath.Join(os.Getenv("LOCALAPPDATA"), "DungeondraftBridge", "assets")
}

// liveBridge talks to Dungeondraft through bridge_app.
type liveBridge struct{ client *bridge.Client }

func (b liveBridge) ListAssets(ctx context.Context, category string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	reply, err := b.client.Request(ctx, "list_assets", map[string]any{"category": category, "limit": listAllLimit})
	if err != nil {
		return nil, err
	}
	raw, _ := reply["assets"].([]any)
	paths := make([]string, 0, len(raw))
	for _, p := range raw {
		if s, ok := p.(string); ok {
			paths = append(paths, s)
		}
	}
	return paths, nil
}

func (b liveBridge) ExportAsset(ctx context.Context, category, path, out string) (int, int, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	reply, err := b.client.Request(ctx, "export_asset", map[string]any{"category": category, "path": path, "out": out})
	if err != nil {
		return 0, 0, err
	}
	width, _ := reply["width"].(float64)
	height, _ := reply["height"].(float64)
	return int(width), int(height), nil
}
