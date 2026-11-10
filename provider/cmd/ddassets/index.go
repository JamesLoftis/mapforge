package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"

	_ "modernc.org/sqlite"
)

// SchemaVersion is what other readers of the index (Campaign Creator) check.
const SchemaVersion = 1

// searchTextVersion changes whenever searchText does; an index holding older
// search text has it rebuilt when opened.
const searchTextVersion = "2"

const schema = `
CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS assets (
	path         TEXT PRIMARY KEY,
	category     TEXT NOT NULL,
	pack         TEXT NOT NULL,
	kind         TEXT NOT NULL,
	width_tiles  INTEGER NOT NULL DEFAULT 0,
	height_tiles INTEGER NOT NULL DEFAULT 0,
	placement    TEXT NOT NULL DEFAULT '',
	description  TEXT NOT NULL DEFAULT '',
	tags_what    TEXT NOT NULL DEFAULT '[]',
	tags_where   TEXT NOT NULL DEFAULT '[]',
	tags_style   TEXT NOT NULL DEFAULT '[]',
	picture      TEXT NOT NULL DEFAULT '',
	thumb        TEXT NOT NULL DEFAULT '',
	error        TEXT NOT NULL DEFAULT '',
	hand_edited  TEXT NOT NULL DEFAULT '[]'
);
CREATE VIRTUAL TABLE IF NOT EXISTS assets_fts USING fts5(path UNINDEXED, text);
`

// An Asset is one entry in the index: a Dungeondraft asset you can see on a map.
type Asset struct {
	Path, Category, Pack, Kind     string
	WidthTiles, HeightTiles        int    // objects only; 0 otherwise
	Placement                      string // "wall", "center" or ""
	Description                    string
	TagsWhat, TagsWhere, TagsStyle []string
	Picture, Thumb                 string   // file names under pictures/ and thumbs/
	Error                          string   // the last load, export or describe failure
	HandEdited                     []string // fields the GM corrected; a re-run never overwrites them
}

// Query narrows a search; zero fields do not filter.
type Query struct {
	Text, Kind, Where string
	MaxTiles, Limit   int
}

// Index is the SQLite asset index.
type Index struct{ db *sql.DB }

var kinds = map[string]string{
	"Objects": "object", "Walls": "wall", "Simple Tiles": "floor tile", "Patterns": "pattern",
	"Patterns Colorable": "pattern", "Materials": "material", "Portals": "door", "Terrain": "terrain",
	"Paths": "path", "Roofs": "roof",
}

// KindFor says what kind of asset a Dungeondraft category holds, and whether
// it is indexed at all: abstract categories (light shapes, cave masks, smart
// tile rules) are not.
func KindFor(category string) (kind string, indexed bool) {
	kind, indexed = kinds[category]
	return kind, indexed
}

// IndexedCategories lists every Dungeondraft category the index covers, in a stable order.
func IndexedCategories() []string {
	categories := make([]string, 0, len(kinds))
	for category := range kinds {
		categories = append(categories, category)
	}
	slices.Sort(categories)
	return categories
}

// PackOf names the pack an asset comes from: the folder after res://packs/, or "built-in".
func PackOf(path string) string {
	rest, inPack := strings.CutPrefix(path, "res://packs/")
	if !inPack {
		return "built-in"
	}
	pack, _, _ := strings.Cut(rest, "/")
	return pack
}

func OpenIndex(path string) (*Index, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("opening the asset index %s: %w", path, err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("creating the asset index schema in %s: %w", path, err)
	}
	if _, err := db.Exec(`INSERT OR IGNORE INTO meta (key, value) VALUES ('schema_version', ?)`, fmt.Sprint(SchemaVersion)); err != nil {
		db.Close()
		return nil, err
	}
	ix := &Index{db: db}
	if err := ix.refreshSearchText(); err != nil {
		db.Close()
		return nil, fmt.Errorf("rebuilding the search text in %s: %w", path, err)
	}
	return ix, nil
}

func (ix *Index) refreshSearchText() error {
	var version string
	err := ix.db.QueryRow(`SELECT value FROM meta WHERE key = 'search_text_version'`).Scan(&version)
	if err == nil && version == searchTextVersion {
		return nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	rows, err := ix.db.Query(selectAssets)
	if err != nil {
		return err
	}
	assets, err := scanAssets(rows)
	if err != nil {
		return err
	}
	tx, err := ix.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, a := range assets {
		if err := writeSearchText(tx, a); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`INSERT INTO meta (key, value) VALUES ('search_text_version', ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, searchTextVersion); err != nil {
		return err
	}
	return tx.Commit()
}

func (ix *Index) Close() error { return ix.db.Close() }

// Upsert adds an asset or updates it. A non-empty new value replaces the old
// one unless the GM corrected that field by hand; empty values never erase.
func (ix *Index) Upsert(a Asset) error {
	old, found, err := ix.Get(a.Path)
	if err != nil {
		return err
	}
	if found {
		a = merged(old, a)
	}
	if a.Pack == "" {
		a.Pack = PackOf(a.Path)
	}
	tx, err := ix.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.Exec(`INSERT INTO assets (path, category, pack, kind, width_tiles, height_tiles, placement, description,
		tags_what, tags_where, tags_style, picture, thumb, error, hand_edited)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(path) DO UPDATE SET category=excluded.category, pack=excluded.pack, kind=excluded.kind,
		width_tiles=excluded.width_tiles, height_tiles=excluded.height_tiles, placement=excluded.placement,
		description=excluded.description, tags_what=excluded.tags_what, tags_where=excluded.tags_where,
		tags_style=excluded.tags_style, picture=excluded.picture, thumb=excluded.thumb, error=excluded.error,
		hand_edited=excluded.hand_edited`,
		a.Path, a.Category, a.Pack, a.Kind, a.WidthTiles, a.HeightTiles, a.Placement, a.Description,
		jsonList(a.TagsWhat), jsonList(a.TagsWhere), jsonList(a.TagsStyle), a.Picture, a.Thumb, a.Error, jsonList(a.HandEdited))
	if err != nil {
		return fmt.Errorf("saving %s: %w", a.Path, err)
	}
	if err := writeSearchText(tx, a); err != nil {
		return err
	}
	return tx.Commit()
}

func writeSearchText(tx *sql.Tx, a Asset) error {
	if _, err := tx.Exec(`DELETE FROM assets_fts WHERE path = ?`, a.Path); err != nil {
		return err
	}
	_, err := tx.Exec(`INSERT INTO assets_fts (path, text) VALUES (?, ?)`, a.Path, searchText(a))
	return err
}

func merged(old, next Asset) Asset {
	hand := func(field string) bool { return slices.Contains(old.HandEdited, field) }
	pickString := func(field, o, n string) string {
		if hand(field) || n == "" {
			return o
		}
		return n
	}
	pickList := func(field string, o, n []string) []string {
		if hand(field) || len(n) == 0 {
			return o
		}
		return n
	}
	pickInt := func(o, n int) int {
		if n == 0 {
			return o
		}
		return n
	}
	out := next
	out.Category = pickString("category", old.Category, next.Category)
	out.Pack = pickString("pack", old.Pack, next.Pack)
	out.Kind = pickString("kind", old.Kind, next.Kind)
	out.WidthTiles = pickInt(old.WidthTiles, next.WidthTiles)
	out.HeightTiles = pickInt(old.HeightTiles, next.HeightTiles)
	out.Placement = pickString("placement", old.Placement, next.Placement)
	out.Description = pickString("description", old.Description, next.Description)
	out.TagsWhat = pickList("tagsWhat", old.TagsWhat, next.TagsWhat)
	out.TagsWhere = pickList("tagsWhere", old.TagsWhere, next.TagsWhere)
	out.TagsStyle = pickList("tagsStyle", old.TagsStyle, next.TagsStyle)
	out.Picture = pickString("picture", old.Picture, next.Picture)
	out.Thumb = pickString("thumb", old.Thumb, next.Thumb)
	out.Error = next.Error // a new run's outcome always replaces the last failure
	out.HandEdited = union(old.HandEdited, next.HandEdited)
	return out
}

func (ix *Index) Get(path string) (Asset, bool, error) {
	rows, err := ix.db.Query(selectAssets+` WHERE path = ?`, path)
	if err != nil {
		return Asset{}, false, err
	}
	assets, err := scanAssets(rows)
	if err != nil || len(assets) == 0 {
		return Asset{}, false, err
	}
	return assets[0], true, nil
}

// Search finds assets matching every word of q.Text (in names, descriptions and tags), filtered by q's other fields.
func (ix *Index) Search(q Query) ([]Asset, error) {
	query := selectAssets + ` WHERE 1 = 1`
	var args []any
	if words := ftsWords(q.Text); words != "" {
		query += ` AND path IN (SELECT path FROM assets_fts WHERE assets_fts MATCH ?)`
		args = append(args, words)
	}
	if q.Kind != "" {
		query += ` AND kind = ?`
		args = append(args, q.Kind)
	}
	if q.Where != "" {
		query += ` AND tags_where LIKE ?`
		args = append(args, `%"`+q.Where+`"%`)
	}
	if q.MaxTiles > 0 {
		query += ` AND width_tiles <= ? AND height_tiles <= ?`
		args = append(args, q.MaxTiles, q.MaxTiles)
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 50
	}
	query += ` ORDER BY path LIMIT ?`
	args = append(args, limit)
	rows, err := ix.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("searching the asset index: %w", err)
	}
	return scanAssets(rows)
}

// Pending lists the assets a stage still has to do: "picture" (no picture yet)
// or "describe" (a picture but no description).
func (ix *Index) Pending(stage string) ([]Asset, error) {
	var where string
	switch stage {
	case "picture":
		where = `picture = ''`
	case "describe":
		where = `picture <> '' AND description = ''`
	default:
		return nil, errors.New("unknown stage " + stage)
	}
	rows, err := ix.db.Query(selectAssets + ` WHERE ` + where + ` ORDER BY path`)
	if err != nil {
		return nil, err
	}
	return scanAssets(rows)
}

const selectAssets = `SELECT path, category, pack, kind, width_tiles, height_tiles, placement, description,
	tags_what, tags_where, tags_style, picture, thumb, error, hand_edited FROM assets`

func scanAssets(rows *sql.Rows) ([]Asset, error) {
	defer rows.Close()
	var assets []Asset
	for rows.Next() {
		var a Asset
		var what, where, style, hand string
		if err := rows.Scan(&a.Path, &a.Category, &a.Pack, &a.Kind, &a.WidthTiles, &a.HeightTiles, &a.Placement,
			&a.Description, &what, &where, &style, &a.Picture, &a.Thumb, &a.Error, &hand); err != nil {
			return nil, err
		}
		a.TagsWhat, a.TagsWhere, a.TagsStyle, a.HandEdited = parseList(what), parseList(where), parseList(style), parseList(hand)
		assets = append(assets, a)
	}
	return assets, rows.Err()
}

// genericFolders say nothing about what an asset is, so they are left out of its search text.
var genericFolders = map[string]bool{"": true, "res:": true, "packs": true, "textures": true}

// searchText is what full-text search matches: the file and folder names, so
// an undescribed asset can still be found, then the description and tags.
// Campaign Creator's library.ts mirrors it; change both, and searchTextVersion.
func searchText(a Asset) string {
	folders, file := path.Split(a.Path)
	parts := []string{nameWords(strings.TrimSuffix(file, path.Ext(file)))}
	for _, folder := range strings.Split(folders, "/") {
		if !genericFolders[folder] {
			parts = append(parts, nameWords(folder))
		}
	}
	parts = append(parts, a.Kind, a.Description)
	parts = append(parts, a.TagsWhat...)
	parts = append(parts, a.TagsWhere...)
	parts = append(parts, a.TagsStyle...)
	return strings.Join(parts, " ")
}

func nameWords(name string) string {
	return strings.NewReplacer("_", " ", "-", " ").Replace(name)
}

// ftsWords turns free text into an FTS5 query where every word must match (as a prefix).
func ftsWords(text string) string {
	var terms []string
	for _, word := range strings.Fields(text) {
		word = strings.Trim(word, `"*()'`)
		if word != "" {
			terms = append(terms, `"`+word+`"*`)
		}
	}
	return strings.Join(terms, " ")
}

func jsonList(items []string) string {
	if items == nil {
		items = []string{}
	}
	raw, _ := json.Marshal(items)
	return string(raw)
}

func parseList(raw string) []string {
	var items []string
	_ = json.Unmarshal([]byte(raw), &items)
	return items
}

func union(a, b []string) []string {
	out := slices.Clone(a)
	for _, item := range b {
		if !slices.Contains(out, item) {
			out = append(out, item)
		}
	}
	return out
}
