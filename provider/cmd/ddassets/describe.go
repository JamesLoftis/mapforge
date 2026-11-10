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
	spriteForgeURL  = "http://10.0.0.54:8760"
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
	// Collector brings in named assets the index has no picture of yet; only
	// DescribePaths needs it.
	Collector *Collector
	// RetryWaits are the pauses before trying again while SpriteForge is not
	// answering, in a run over everything pending; once they are used up the
	// run stops. None means stop at once. Named assets are never waited for.
	RetryWaits []time.Duration
}

// overnightRetryWaits ride out a passing outage during a long run.
var overnightRetryWaits = []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute}

// errServiceDown means SpriteForge itself is not answering: stop, try again later.
var errServiceDown = errors.New("SpriteForge isn't answering")

// errServiceRefused means SpriteForge answered but will serve no asset (wrong
// token, no such route): stop, and do not wait, since waiting will not help.
var errServiceRefused = errors.New("SpriteForge refused the request")

var errNoDescription = errors.New("SpriteForge gave no description")

// noWaits is for an asset wanted now: one attempt, no riding out an outage.
var noWaits []time.Duration

// liveDescriber describes with the real SpriteForge, collecting through collector.
func liveDescriber(collector *Collector, model string) (*Describer, error) {
	token, err := os.ReadFile(filepath.Join(os.Getenv("USERPROFILE"), ".spriteforge-token"))
	if err != nil {
		return nil, fmt.Errorf("reading the SpriteForge token: %w", err)
	}
	client := &SpriteForgeClient{Base: spriteForgeURL, Token: strings.TrimSpace(string(token)), Model: model}
	return &Describer{Index: collector.Index, Dir: collector.Dir, SpriteForge: client, Log: os.Stdout,
		Collector: collector, RetryWaits: overnightRetryWaits}, nil
}

// Describe describes every asset still pending a description, up to limit (0 = all).
func (d *Describer) Describe(ctx context.Context, limit int) error {
	pending, err := d.Index.Pending("describe")
	if err != nil {
		return err
	}
	described, failed := 0, 0
	for _, asset := range pending {
		if limit > 0 && described+failed >= limit {
			break
		}
		saved, err := d.describeAndSave(ctx, asset, d.RetryWaits)
		if err != nil {
			return fmt.Errorf("%w (%d described this run; the rest stay pending)", err, described)
		}
		if saved.Error == "" {
			described++
		} else {
			failed++
		}
	}
	fmt.Fprintf(d.Log, "describe: %d assets described, %d failed\n", described, failed)
	if failed > 0 {
		return fmt.Errorf("could not describe %d of %d assets; each one's error is recorded in the index", failed, described+failed)
	}
	return nil
}

// DescribePaths describes just the named assets, collecting any the index has
// no picture of yet; ones already described are left alone. Every path is
// tried, and the ones that could not be described are named in the error.
func (d *Describer) DescribePaths(ctx context.Context, paths []string) error {
	// Collecting can fail (Dungeondraft closed) without stopping the assets
	// the index already has a picture of.
	collectFailure := d.collectMissing(ctx, paths)
	whyUnknown := "Dungeondraft has no such asset"
	if collectFailure != nil {
		whyUnknown = "not in the index yet"
	}
	var failures []string
	for _, p := range paths {
		failure, err := d.describeNamed(ctx, p, whyUnknown)
		if err != nil {
			return stoppedAfter(failures, err)
		}
		if failure != "" {
			failures = append(failures, p+": "+failure)
		}
	}
	if len(failures) == 0 {
		return nil
	}
	summary := fmt.Sprintf("could not describe %d of %d assets: %s", len(failures), len(paths), strings.Join(failures, "; "))
	if collectFailure != nil {
		return fmt.Errorf("%s (collecting from Dungeondraft failed: %w)", summary, collectFailure)
	}
	return errors.New(summary)
}

// stoppedAfter is the error for a run that had to stop, still naming the
// assets that had already failed.
func stoppedAfter(failures []string, cause error) error {
	if len(failures) == 0 {
		return cause
	}
	return fmt.Errorf("%w (before that, could not describe: %s)", cause, strings.Join(failures, "; "))
}

func (d *Describer) collectMissing(ctx context.Context, paths []string) error {
	var missing []string
	for _, p := range paths {
		asset, found, err := d.Index.Get(p)
		if err != nil {
			return err
		}
		if !found || asset.Picture == "" {
			missing = append(missing, p)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	if d.Collector == nil {
		return fmt.Errorf("%d named assets need collecting from Dungeondraft first, and no bridge was given", len(missing))
	}
	return d.Collector.CollectPaths(ctx, missing)
}

// describeNamed describes one named asset if it still needs it, and says why
// it could not (whyUnknown, for a path the index does not hold); only
// SpriteForge being down, or the index failing, is an error.
func (d *Describer) describeNamed(ctx context.Context, p, whyUnknown string) (failure string, err error) {
	asset, found, err := d.Index.Get(p)
	switch {
	case err != nil:
		return "", err
	case !found:
		return whyUnknown, nil
	case asset.HasSettledDescription():
		return "", nil
	case asset.Picture == "" && asset.Error != "":
		return "no picture: " + asset.Error, nil
	case asset.Picture == "":
		return "no picture yet", nil
	}
	saved, err := d.describeAndSave(ctx, asset, noWaits)
	return saved.Error, err
}

// describeAndSave describes one asset and saves the outcome, riding out an
// outage for as long as retryWaits allow. A failure for this asset alone is
// recorded on it; only SpriteForge being down or refusing, or the index
// failing, is returned.
func (d *Describer) describeAndSave(ctx context.Context, asset Asset, retryWaits []time.Duration) (Asset, error) {
	started := time.Now()
	reply, err := d.describeWithRetries(ctx, asset, retryWaits)
	if errors.Is(err, errServiceDown) || errors.Is(err, errServiceRefused) {
		return asset, err
	}
	if err != nil {
		asset.Error = err.Error()
	} else {
		asset = withReply(asset, reply)
	}
	if err := d.Index.Upsert(asset); err != nil {
		return asset, err
	}
	fmt.Fprintf(d.Log, "%.1fs  %s: %s\n", time.Since(started).Seconds(), path.Base(asset.Path), firstLine(asset.Description, asset.Error))
	return asset, nil
}

func (d *Describer) describeWithRetries(ctx context.Context, asset Asset, retryWaits []time.Duration) (describeReply, error) {
	reply, err := d.describeOne(ctx, asset)
	for _, wait := range retryWaits {
		if !errors.Is(err, errServiceDown) {
			break
		}
		fmt.Fprintf(d.Log, "SpriteForge isn't answering (%v); trying again in %s\n", err, wait)
		select {
		case <-ctx.Done():
			return reply, ctx.Err()
		case <-time.After(wait):
		}
		reply, err = d.describeOne(ctx, asset)
	}
	return reply, err
}

func (d *Describer) describeOne(ctx context.Context, asset Asset) (describeReply, error) {
	picture, err := onParchment(filepath.Join(d.Dir, "pictures", asset.Picture))
	if err != nil {
		return describeReply{}, err
	}
	reply, err := d.SpriteForge.Describe(ctx, picture, focusFor(asset))
	if err == nil && strings.TrimSpace(reply.Description) == "" {
		return reply, errNoDescription
	}
	return reply, err
}

// focusFor tells the vision model what the picture is and what to say about
// it; the asset's own file and folder names are usually the best hint.
func focusFor(asset Asset) string {
	file := strings.TrimSuffix(path.Base(asset.Path), path.Ext(asset.Path))
	folder := path.Base(path.Dir(asset.Path))
	return fmt.Sprintf("A top-down Dungeondraft map asset (%s). Its file is '%s' in the folder '%s'. "+
		"Say plainly what it is. For setting and where, name the kinds of places in a fantasy world where it "+
		"would really be found, not the background; say 'anywhere' if it could be found anywhere.", asset.Kind, file, folder)
}

type describeReply struct {
	Description string `json:"description"`
	Subject     string `json:"subject"`
	Style       string `json:"style"`
	Setting     string `json:"setting"`
	// Where and Placement come only from models that can give them (Claude).
	Where     []string `json:"where"`
	Placement string   `json:"placement"`
}

func withReply(a Asset, r describeReply) Asset {
	a.Description = strings.TrimSpace(r.Description)
	a.TagsWhat = tagList(r.Subject)
	a.TagsWhere = tagList(r.Setting)
	if len(r.Where) > 0 {
		a.TagsWhere = tagList(strings.Join(r.Where, ","))
	}
	if r.Placement == "wall" || r.Placement == "center" {
		a.Placement = r.Placement
	}
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
	// Model is the describe model SpriteForge should use; "" means its default (local).
	Model string
}

func (c *SpriteForgeClient) Describe(ctx context.Context, picture []byte, focus string) (describeReply, error) {
	var upload struct {
		ID string `json:"id"`
	}
	if err := c.post(ctx, "/studio/uploads", "image/png", picture, &upload); err != nil {
		return describeReply{}, err
	}
	request := map[string]string{"upload": upload.ID, "focus": focus}
	if c.Model != "" {
		request["model"] = c.Model
	}
	body, _ := json.Marshal(request)
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
	switch {
	case res.StatusCode == http.StatusOK:
		return json.NewDecoder(res.Body).Decode(out)
	case res.StatusCode >= http.StatusInternalServerError || res.StatusCode == http.StatusTooManyRequests:
		return fmt.Errorf("%w: %s answered %s", errServiceDown, route, res.Status)
	case refusesEveryAsset[res.StatusCode]:
		return fmt.Errorf("%w: %s answered %s", errServiceRefused, route, res.Status)
	default:
		detail, _ := io.ReadAll(io.LimitReader(res.Body, 300))
		return fmt.Errorf("%s answered %s: %s", route, res.Status, strings.TrimSpace(string(detail)))
	}
}

// refusesEveryAsset are the answers that are about the caller or the route,
// not about the one picture sent.
var refusesEveryAsset = map[int]bool{
	http.StatusUnauthorized: true, http.StatusForbidden: true, http.StatusNotFound: true,
}
