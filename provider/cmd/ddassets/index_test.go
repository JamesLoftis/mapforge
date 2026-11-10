package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func openTestIndex(t *testing.T) *Index {
	t.Helper()
	ix, err := OpenIndex(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatalf("OpenIndex: %v", err)
	}
	t.Cleanup(func() { ix.Close() })
	return ix
}

func TestKindForMapsEveryVisibleCategoryAndRefusesAbstractOnes(t *testing.T) {
	want := map[string]string{
		"Objects": "object", "Walls": "wall", "Simple Tiles": "floor tile", "Patterns": "pattern",
		"Patterns Colorable": "pattern", "Materials": "material", "Portals": "door", "Terrain": "terrain",
		"Paths": "path", "Roofs": "roof",
	}
	for category, kind := range want {
		if got, indexed := KindFor(category); !indexed || got != kind {
			t.Errorf("KindFor(%q) = %q, %v; want %q, true", category, got, indexed, kind)
		}
	}
	for _, abstract := range []string{"Lights", "Caves", "Smart Tiles", "Smart Tiles Double"} {
		if _, indexed := KindFor(abstract); indexed {
			t.Errorf("KindFor(%q) is indexed; it is abstract", abstract)
		}
	}
}

func TestPackOf(t *testing.T) {
	if got := PackOf("res://packs/DnDgCORE/textures/objects/x.webp"); got != "DnDgCORE" {
		t.Errorf("PackOf pack asset = %q", got)
	}
	if got := PackOf("res://textures/objects/x.png"); got != "built-in" {
		t.Errorf("PackOf built-in asset = %q", got)
	}
}

func TestUpsertKeepsWhatTheGMCorrected(t *testing.T) {
	ix := openTestIndex(t)
	path := "res://textures/objects/furniture/tables/round_table_01.png"
	if err := ix.Upsert(Asset{Path: path, Category: "Objects", Kind: "object", Description: "a table", HandEdited: []string{"description"}}); err != nil {
		t.Fatal(err)
	}
	if err := ix.Upsert(Asset{Path: path, Category: "Objects", Kind: "object", Description: "a round wooden table", TagsWhat: []string{"table"}}); err != nil {
		t.Fatal(err)
	}
	got, found, err := ix.Get(path)
	if err != nil || !found {
		t.Fatalf("Get: %v, found %v", err, found)
	}
	if got.Description != "a table" {
		t.Errorf("hand-corrected description was overwritten: %q", got.Description)
	}
	if len(got.TagsWhat) != 1 || got.TagsWhat[0] != "table" {
		t.Errorf("tags not taken from the re-run: %v", got.TagsWhat)
	}
	if len(got.HandEdited) != 1 || got.HandEdited[0] != "description" {
		t.Errorf("hand-edited fields lost: %v", got.HandEdited)
	}
}

func TestSearchFindsByTagAndFiltersByKindAndSize(t *testing.T) {
	ix := openTestIndex(t)
	for _, a := range []Asset{
		{Path: "res://a/card_table.png", Category: "Objects", Kind: "object", WidthTiles: 2, HeightTiles: 2, Description: "round table", TagsWhere: []string{"tavern", "gambling den"}},
		{Path: "res://a/banquet.png", Category: "Objects", Kind: "object", WidthTiles: 6, HeightTiles: 2, Description: "long table", TagsWhere: []string{"gambling den", "great hall"}},
		{Path: "res://a/den_floor.png", Category: "Simple Tiles", Kind: "floor tile", Description: "dark planks", TagsWhere: []string{"gambling den"}},
	} {
		if err := ix.Upsert(a); err != nil {
			t.Fatal(err)
		}
	}
	got, err := ix.Search(Query{Text: "gambling", Kind: "object", MaxTiles: 3, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Path != "res://a/card_table.png" {
		t.Errorf("Search = %v; want only the card table", paths(got))
	}
}

func TestPendingListsWhatEachStageStillNeeds(t *testing.T) {
	ix := openTestIndex(t)
	_ = ix.Upsert(Asset{Path: "res://a/no_picture.png", Category: "Objects", Kind: "object"})
	_ = ix.Upsert(Asset{Path: "res://a/needs_words.png", Category: "Objects", Kind: "object", Picture: "p.png", Thumb: "t.png"})
	_ = ix.Upsert(Asset{Path: "res://a/done.png", Category: "Objects", Kind: "object", Picture: "p2.png", Thumb: "t2.png", Description: "done"})
	pictures, _ := ix.Pending("picture")
	words, _ := ix.Pending("describe")
	if p := paths(pictures); len(p) != 1 || p[0] != "res://a/no_picture.png" {
		t.Errorf("Pending(picture) = %v", p)
	}
	if p := paths(words); len(p) != 1 || p[0] != "res://a/needs_words.png" {
		t.Errorf("Pending(describe) = %v", p)
	}
}

func paths(assets []Asset) []string {
	out := make([]string, len(assets))
	for i, a := range assets {
		out[i] = a.Path
	}
	return out
}

const tableInAFolder = "res://packs/P/textures/objects/furniture/long_tables/oak_01.png"

func TestSearchFindsAnUndescribedAssetByItsFolder(t *testing.T) {
	ix := openTestIndex(t)
	if err := ix.Upsert(Asset{Path: tableInAFolder, Category: "Objects", Kind: "object"}); err != nil {
		t.Fatal(err)
	}
	got, err := ix.Search(Query{Text: "tables", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("Search(tables) = %v; want the oak table found by its folder", paths(got))
	}
}

func TestOpeningAnIndexWithOldSearchTextRebuildsIt(t *testing.T) {
	file := filepath.Join(t.TempDir(), "index.db")
	ix, err := OpenIndex(file)
	if err != nil {
		t.Fatal(err)
	}
	_ = ix.Upsert(Asset{Path: tableInAFolder, Category: "Objects", Kind: "object"})
	_, _ = ix.db.Exec(`UPDATE assets_fts SET text = 'oak 01 object'`)
	_, _ = ix.db.Exec(`DELETE FROM meta WHERE key = 'search_text_version'`)
	ix.Close()

	ix = openIndexAt(t, file)
	got, _ := ix.Search(Query{Text: "tables", Limit: 10})
	if len(got) != 1 {
		t.Errorf("after reopening, Search(tables) = %v; want the old row's search text rebuilt", paths(got))
	}
}

func openIndexAt(t *testing.T, file string) *Index {
	t.Helper()
	ix, err := OpenIndex(file)
	if err != nil {
		t.Fatalf("OpenIndex: %v", err)
	}
	t.Cleanup(func() { ix.Close() })
	return ix
}

func TestAnAssetWhoseDescriptionTheGMSetIsNotPendingDescribing(t *testing.T) {
	ix := openTestIndex(t)
	asset := Asset{Path: "res://a/one.png", Category: "Objects", Kind: "object", Picture: "one.png", HandEdited: []string{"description"}}
	if err := ix.Upsert(asset); err != nil {
		t.Fatal(err)
	}
	pending, err := ix.Pending("describe")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Errorf("%d assets pending describing; want none", len(pending))
	}
}

func TestUpsertWaitsForAnotherWriterToFinish(t *testing.T) {
	indexPath := filepath.Join(t.TempDir(), "index.db")
	ix, err := OpenIndex(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	other, err := sql.Open("sqlite", indexPath)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	ctx := context.Background()
	writer, err := other.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if _, err := writer.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	const otherWriterHolds = 150 * time.Millisecond
	released := make(chan error, 1)
	go func() {
		time.Sleep(otherWriterHolds)
		_, err := writer.ExecContext(ctx, "COMMIT")
		released <- err
	}()
	if err := ix.Upsert(Asset{Path: "res://a/one.png", Category: "Objects", Kind: "object"}); err != nil {
		t.Errorf("Upsert gave up instead of waiting: %v", err)
	}
	if err := <-released; err != nil {
		t.Fatal(err)
	}
}
