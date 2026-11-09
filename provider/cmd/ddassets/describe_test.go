package main

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testToken = "secret-token-123"

// fakeSpriteForge answers /studio/uploads and /describe like SpriteForge does.
func fakeSpriteForge(t *testing.T, describeStatus int, reply map[string]any) (*httptest.Server, *[]string) {
	t.Helper()
	var tokens []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokens = append(tokens, r.Header.Get("X-SpriteForge-Token"))
		switch r.URL.Path {
		case "/studio/uploads":
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "up1"})
		case "/describe":
			if describeStatus != http.StatusOK {
				w.WriteHeader(describeStatus)
				return
			}
			_ = json.NewEncoder(w).Encode(reply)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &tokens
}

// indexWithPictures adds assets that already have a picture on disk.
func indexWithPictures(t *testing.T, assets ...Asset) (*Index, string) {
	t.Helper()
	ix := openTestIndex(t)
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "pictures"), 0o755)
	for _, a := range assets {
		a.Picture, a.Thumb = fileName(a.Path), fileName(a.Path)
		var buf bytes.Buffer
		_ = png.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, 4, 4)))
		os.WriteFile(filepath.Join(dir, "pictures", a.Picture), buf.Bytes(), 0o644)
		if err := ix.Upsert(a); err != nil {
			t.Fatal(err)
		}
	}
	return ix, dir
}

var squidReply = map[string]any{
	"description": "A pale pink squid-like creature with black eyes.",
	"subject":     "squid",
	"style":       "2D pixel art, flat colors, game asset",
	"setting":     "fantasy dungeon or crypt",
}

func TestDescribeMapsTheReplyIntoTheIndex(t *testing.T) {
	srv, tokens := fakeSpriteForge(t, http.StatusOK, squidReply)
	ix, dir := indexWithPictures(t, Asset{Path: "res://packs/P/textures/objects/Sea/squid_02.webp", Category: "Objects", Kind: "object"})
	var out bytes.Buffer
	d := &Describer{Index: ix, Dir: dir, SpriteForge: &SpriteForgeClient{Base: srv.URL, Token: testToken}, Log: &out}
	if err := d.Describe(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	a, _, _ := ix.Get("res://packs/P/textures/objects/Sea/squid_02.webp")
	if a.Description != squidReply["description"] {
		t.Errorf("description = %q", a.Description)
	}
	if strings.Join(a.TagsWhat, "|") != "squid" || strings.Join(a.TagsWhere, "|") != "fantasy dungeon|crypt" {
		t.Errorf("tags what=%v where=%v", a.TagsWhat, a.TagsWhere)
	}
	if strings.Join(a.TagsStyle, "|") != "2d pixel art|flat colors|game asset" {
		t.Errorf("style tags = %v", a.TagsStyle)
	}
	for _, token := range *tokens {
		if token != testToken {
			t.Errorf("a request went without the token")
		}
	}
	if strings.Contains(out.String(), testToken) {
		t.Errorf("the token appeared in the output")
	}
}

func TestDescribeKeepsWhatTheGMCorrected(t *testing.T) {
	srv, _ := fakeSpriteForge(t, http.StatusOK, squidReply)
	path := "res://a/squid.png"
	ix, dir := indexWithPictures(t, Asset{Path: path, Category: "Objects", Kind: "object", TagsWhere: []string{"harbour"}, HandEdited: []string{"tagsWhere"}})
	d := &Describer{Index: ix, Dir: dir, SpriteForge: &SpriteForgeClient{Base: srv.URL, Token: testToken}, Log: &bytes.Buffer{}}
	if err := d.Describe(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	a, _, _ := ix.Get(path)
	if strings.Join(a.TagsWhere, "|") != "harbour" {
		t.Errorf("hand-corrected tags overwritten: %v", a.TagsWhere)
	}
}

func TestDescribeStopsWhenSpriteForgeIsDownAndLeavesTheRestPending(t *testing.T) {
	srv, _ := fakeSpriteForge(t, http.StatusServiceUnavailable, nil)
	ix, dir := indexWithPictures(t,
		Asset{Path: "res://a/one.png", Category: "Objects", Kind: "object"},
		Asset{Path: "res://a/two.png", Category: "Objects", Kind: "object"})
	d := &Describer{Index: ix, Dir: dir, SpriteForge: &SpriteForgeClient{Base: srv.URL, Token: testToken}, Log: &bytes.Buffer{}}
	err := d.Describe(context.Background(), 0)
	if err == nil || !strings.Contains(err.Error(), "SpriteForge") {
		t.Fatalf("got %v; want an error naming SpriteForge", err)
	}
	pending, _ := ix.Pending("describe")
	if len(pending) != 2 {
		t.Errorf("%d assets still pending; want 2", len(pending))
	}
}

func TestFocusGivesTheModelTheAssetsNameAndFolder(t *testing.T) {
	focus := focusFor(Asset{Path: "res://packs/P/textures/objects/[DnD] Food/squid_raw_02.webp", Kind: "object"})
	for _, want := range []string{"squid_raw_02", "[DnD] Food", "where"} {
		if !strings.Contains(focus, want) {
			t.Errorf("focus %q lacks %q", focus, want)
		}
	}
}

func TestDescribeAsksForTheChosenModelAndReadsWhereAndPlacement(t *testing.T) {
	var asked map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/studio/uploads":
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "up1"})
		case "/describe":
			_ = json.NewDecoder(r.Body).Decode(&asked)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"description": "A heavy oak wardrobe.", "subject": "wardrobe", "style": "painted",
				"setting": "bedroom", "where": []string{"Bedroom", "Noble house"}, "placement": "wall",
			})
		}
	}))
	t.Cleanup(srv.Close)
	ix, dir := indexWithPictures(t, Asset{Path: "res://a/wardrobe_01.png", Category: "Objects", Kind: "object"})
	d := &Describer{Index: ix, Dir: dir, SpriteForge: &SpriteForgeClient{Base: srv.URL, Token: testToken, Model: "claude"}, Log: &bytes.Buffer{}}
	if err := d.Describe(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	if asked["model"] != "claude" {
		t.Errorf("asked for model %q; want claude", asked["model"])
	}
	a, _, _ := ix.Get("res://a/wardrobe_01.png")
	if strings.Join(a.TagsWhere, "|") != "bedroom|noble house" || a.Placement != "wall" {
		t.Errorf("where=%v placement=%q; want the reply's where list and wall", a.TagsWhere, a.Placement)
	}
}

func TestFocusSuggestsNoExamplePlaces(t *testing.T) {
	focus := focusFor(Asset{Path: "res://a/b/thing.png", Kind: "object"})
	for _, parroted := range []string{"kitchen", "tavern", "crypt", "forest"} {
		if strings.Contains(focus, parroted) {
			t.Errorf("focus suggests %q, which the model then repeats: %q", parroted, focus)
		}
	}
}

func TestDescribeRidesOutAPassingOutage(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/studio/uploads":
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "up1"})
		case "/describe":
			calls++
			if calls == 1 {
				w.WriteHeader(http.StatusBadGateway)
				return
			}
			_ = json.NewEncoder(w).Encode(squidReply)
		}
	}))
	t.Cleanup(srv.Close)
	ix, dir := indexWithPictures(t, Asset{Path: "res://a/squid.png", Category: "Objects", Kind: "object"})
	d := &Describer{Index: ix, Dir: dir, SpriteForge: &SpriteForgeClient{Base: srv.URL, Token: testToken}, Log: &bytes.Buffer{},
		RetryWaits: []time.Duration{time.Millisecond}}
	if err := d.Describe(context.Background(), 0); err != nil {
		t.Fatalf("a single 502 stopped the run: %v", err)
	}
	if a, _, _ := ix.Get("res://a/squid.png"); a.Description == "" {
		t.Errorf("the asset was not described after the retry")
	}
}
