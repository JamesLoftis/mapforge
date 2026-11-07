// Command ddmap gives other tools a blank map to build on and a picture of
// what they built, through bridge_app:
//
//	ddmap new -w <tiles> -h <tiles> -out <path>   create a blank map and save it
//	ddmap export -ppi <n> -out <path.png>         render the open map to a PNG
//
// Exit status 1 with one line on stderr on any failure.
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
	bridgeHost       = "127.0.0.1"
	bridgePort       = 8787
	mapOpTimeout     = 90 * time.Second
	exportTimeout    = 60 * time.Second
	exportPoll       = 250 * time.Millisecond
	defaultExportPPI = 40
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "ddmap:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: ddmap new|export [flags]")
	}
	client := bridge.New(bridgeHost, bridgePort)
	switch args[0] {
	case "new":
		return newMap(client, args[1:])
	case "export":
		return exportMap(client, args[1:])
	default:
		return fmt.Errorf("unknown command %q; want new or export", args[0])
	}
}

func newMap(client *bridge.Client, args []string) error {
	flags := flag.NewFlagSet("new", flag.ContinueOnError)
	width := flags.Int("w", 0, "map width in tiles")
	height := flags.Int("h", 0, "map height in tiles")
	out := flags.String("out", "", "absolute path to save the new map to")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *width < 1 || *height < 1 || *out == "" {
		return errors.New("new needs -w and -h (tiles, at least 1) and -out (the map file)")
	}
	path, err := filepath.Abs(*out)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*mapOpTimeout)
	defer cancel()
	if err := client.NewMap(ctx, *width, *height); err != nil {
		return fmt.Errorf("new_map: %w", err)
	}
	if err := client.WaitMapOp(ctx, "new_map", "", mapOpTimeout); err != nil {
		return fmt.Errorf("new_map: %w", err)
	}
	if err := client.SaveMap(ctx, path); err != nil {
		return fmt.Errorf("save_map %s: %w", path, err)
	}
	return client.WaitMapOp(ctx, "save_map", path, mapOpTimeout)
}

func exportMap(client *bridge.Client, args []string) error {
	flags := flag.NewFlagSet("export", flag.ContinueOnError)
	ppi := flags.Int("ppi", defaultExportPPI, "pixels per tile")
	out := flags.String("out", "", "absolute path of the PNG to write")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *out == "" {
		return errors.New("export needs -out (the PNG file)")
	}
	path, err := filepath.Abs(*out)
	if err != nil {
		return err
	}
	_ = os.Remove(path) // a stale render must not count as this one
	ctx, cancel := context.WithTimeout(context.Background(), exportTimeout)
	defer cancel()
	if _, err := client.Request(ctx, "export_map", map[string]any{"path": path, "ppi": *ppi}); err != nil {
		return fmt.Errorf("export_map: %w", err)
	}
	return waitForStableFile(ctx, path)
}

// waitForStableFile returns once path exists, is not empty, and has the same
// size on two polls in a row: Dungeondraft renders on a background thread.
func waitForStableFile(ctx context.Context, path string) error {
	lastSize := int64(-1)
	for {
		if info, err := os.Stat(path); err == nil && info.Size() > 0 {
			if info.Size() == lastSize {
				return nil
			}
			lastSize = info.Size()
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("export_map: %s was not written within %s", path, exportTimeout)
		case <-time.After(exportPoll):
		}
	}
}
