// Command catalog dumps every asset path in every category from a live
// bridge into one JSON file — a reference for writing Terraform (or any
// bridge client) without guessing search terms and silently falling back to
// "first asset in the category" (which is how a ceremonial floor ended up as
// roof-tile-look basketweave brick, and a "column" search matched a broken
// ruin pillar before a proper one, in an earlier session).
//
// Bypasses mcpserver deliberately: this goes straight to a file, not into an
// LLM's context window, so none of mcpserver's context-size caps apply here
// — a full, uncapped catalog is exactly the point.
//
//	go run ./cmd/catalog                          # writes ./asset_catalog.json
//	go run ./cmd/catalog -out ../maps/asset_catalog.json
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"

	bridge "mapforge/bridgeclient"
)

func main() {
	host := flag.String("host", "127.0.0.1", "bridge host")
	port := flag.Int("port", 8787, "bridge port")
	out := flag.String("out", "asset_catalog.json", "output file path")
	flag.Parse()

	c := bridge.New(*host, *port)
	ctx := context.Background()

	catResult, err := c.Request(ctx, "list_asset_categories", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "list_asset_categories: %v\n", err)
		os.Exit(1)
	}
	categories := toStrings(catResult["categories"])
	sort.Strings(categories)

	catalog := map[string][]string{}
	total := 0
	for _, cat := range categories {
		res, err := c.Request(ctx, "list_assets", map[string]any{
			"category": cat,
			"search":   "",
			"limit":    100000, // this is a file, not an LLM context — no need to cap
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "list_assets %s: %v\n", cat, err)
			os.Exit(1)
		}
		assets := toStrings(res["assets"])
		sort.Strings(assets)
		catalog[cat] = assets
		total += len(assets)
		fmt.Fprintf(os.Stderr, "%-20s %d\n", cat, len(assets))
	}

	f, err := os.Create(*out)
	if err != nil {
		fmt.Fprintf(os.Stderr, "creating %s: %v\n", *out, err)
		os.Exit(1)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(catalog); err != nil {
		fmt.Fprintf(os.Stderr, "writing %s: %v\n", *out, err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "\n%d assets across %d categories -> %s\n", total, len(categories), *out)
}

func toStrings(v any) []string {
	raw, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, r := range raw {
		if s, ok := r.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
