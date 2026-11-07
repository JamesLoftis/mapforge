package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"golang.org/x/image/draw"
)

const (
	spriteForgeURL = "http://10.0.0.54:8760"
	describeTimeout = 3 * time.Minute
)

// parchment is the light background a picture is shown on: a transparent
// background arrives at the vision model as black, which misleads it.
var parchment = color.NRGBA{R: 235, G: 228, B: 214, A: 255}

// Describer drafts each pictured asset's description and tags with SpriteForge.
type Describer struct {
	Index       *Index
	Dir         string
	SpriteForge *SpriteForgeClient
	Log         io.Writer
}

// errServiceDown means SpriteForge itself is not answering: stop, try again later.
var errServiceDown = errors.New("SpriteForge isn't answering")

func describe(ctx context.Context, ix *Index, dir string, limit int) error {
	token, err := os.ReadFile(filepath.Join(os.Getenv("USERPROFILE"), ".spriteforge-token"))
	if err != nil {
		return fmt.Errorf("reading the SpriteForge token: %w", err)
	}
	d := &Describer{Index: ix, Dir: dir, SpriteForge: &SpriteForgeClient{Base: spriteForgeURL, Token: strings.TrimSpace(string(token))}, Log: os.Stdout}
	return d.Describe(ctx, limit)
}

func (d *Describer) Describe(ctx context.Context, limit int) error {
	pending, err := d.Index.Pending("describe")
	if err != nil {
		return err
	}
	done := 0
	for _, asset := range pending {
		if limit > 0 && done >= limit {
			break
		}
		started := time.Now()
		reply, err := d.describeOne(ctx, asset)
		if errors.Is(err, errServiceDown) {
			return fmt.Errorf("%w (%d described this run; the rest stay pending): %v", errServiceDown, done, err)
		}
		if err != nil {
			asset.Error = err.Error()
		} else {
			asset = withReply(asset, reply)
		}
		if err := d.Index.Upsert(asset); err != nil {
			return err
		}
		done++
		fmt.Fprintf(d.Log, "%.1fs  %s: %s\n", time.Since(started).Seconds(), path.Base(asset.Path), firstLine(asset.Description, asset.Error))
	}
	fmt.Fprintf(d.Log, "describe: %d assets described\n", done)
	return nil
}

func (d *Describer) describeOne(ctx context.Context, asset Asset) (describeReply, error) {
	picture, err := onParchment(filepath.Join(d.Dir, "pictures", asset.Picture))
	if err != nil {
		return describeReply{}, err
	}
	return d.SpriteForge.Describe(ctx, picture, focusFor(asset))
}

// focusFor tells the vision model what the picture is and what to say about
// it; the asset's own file and folder names are usually the best hint.
func focusFor(asset Asset) string {
	file := strings.TrimSuffix(path.Base(asset.Path), path.Ext(asset.Path))
	folder := path.Base(path.Dir(asset.Path))
	return fmt.Sprintf("A top-down Dungeondraft map asset (%s). Its file is '%s' in the folder '%s'. "+
		"Say plainly what it is. For setting, name the kinds of places in a fantasy world where it would be found "+
		"(for example kitchen, tavern, crypt, forest), not the background.", asset.Kind, file, folder)
}

type describeReply struct {
	Description string `json:"description"`
	Subject     string `json:"subject"`
	Style       string `json:"style"`
	Setting     string `json:"setting"`
}

func withReply(a Asset, r describeReply) Asset {
	a.Description = strings.TrimSpace(r.Description)
	a.TagsWhat = tagList(r.Subject)
	a.TagsWhere = tagList(r.Setting)
	a.TagsStyle = tagList(r.Style)
	a.Error = ""
	return a
}

var tagSplit = regexp.MustCompile(`\s*(?:,|;|/|\bor\b|\band\b)\s*`)

// tagList turns "fantasy dungeon or crypt" into ["fantasy dungeon", "crypt"].
func tagList(text string) []string {
	var tags []string
	for _, part := range tagSplit.Split(strings.ToLower(text), -1) {
		part = strings.Trim(strings.TrimSpace(part), ".")
		if part != "" && !containsTag(tags, part) {
			tags = append(tags, part)
		}
	}
	return tags
}

func containsTag(tags []string, tag string) bool {
	for _, t := range tags {
		if t == tag {
			return true
		}
	}
	return false
}

func firstLine(description, failure string) string {
	if failure != "" {
		return "FAILED: " + failure
	}
	return description
}

// onParchment returns the picture as PNG bytes on the light background.
func onParchment(picturePath string) ([]byte, error) {
	file, err := os.Open(picturePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	src, _, err := image.Decode(file)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", picturePath, err)
	}
	dst := image.NewNRGBA(src.Bounds())
	draw.Draw(dst, dst.Bounds(), image.NewUniform(parchment), image.Point{}, draw.Src)
	draw.Draw(dst, dst.Bounds(), src, src.Bounds().Min, draw.Over)
	var buf bytes.Buffer
	err = png.Encode(&buf, dst)
	return buf.Bytes(), err
}

// SpriteForgeClient calls SpriteForge's upload and describe routes. The token
// is only ever sent as a header.
type SpriteForgeClient struct {
	Base, Token string
}

func (c *SpriteForgeClient) Describe(ctx context.Context, picture []byte, focus string) (describeReply, error) {
	var upload struct {
		ID string `json:"id"`
	}
	if err := c.post(ctx, "/studio/uploads", "image/png", picture, &upload); err != nil {
		return describeReply{}, err
	}
	body, _ := json.Marshal(map[string]string{"upload": upload.ID, "focus": focus})
	var reply describeReply
	err := c.post(ctx, "/describe", "application/json", body, &reply)
	return reply, err
}

func (c *SpriteForgeClient) post(ctx context.Context, route, contentType string, body []byte, out any) error {
	ctx, cancel := context.WithTimeout(ctx, describeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Base+route, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-SpriteForge-Token", c.Token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %s", errServiceDown, route)
	}
	defer res.Body.Close()
	if res.StatusCode >= http.StatusInternalServerError {
		return fmt.Errorf("%w: %s answered %s", errServiceDown, route, res.Status)
	}
	if res.StatusCode != http.StatusOK {
		detail, _ := io.ReadAll(io.LimitReader(res.Body, 300))
		return fmt.Errorf("%s answered %s: %s", route, res.Status, strings.TrimSpace(string(detail)))
	}
	return json.NewDecoder(res.Body).Decode(out)
}
