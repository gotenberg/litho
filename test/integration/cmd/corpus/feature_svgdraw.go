package main

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
)

// svgDrawDPI is the resolution the svg tests are drawn at: one CSS pixel
// per device pixel, the scale of the reference PNGs.
const svgDrawDPI = 96

// svgFontsTarget is the make target that fetches what the svgdraw feature
// reads.
const svgFontsTarget = "make svg-fetch"

// maxSVGReference bounds the side of a reference PNG, in pixels. Anti-DoS
// guard, not spec-mandated: resvg draws its tests 300 pixels wide, and a
// few 500 pixels.
const maxSVGReference = 4096

// svgTest is one test of corpus/svg/tests.json, written by
// corpus/svg/list-candidates.py.
type svgTest struct {
	ID     string `json:"id"`
	PNG    string `json:"png"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// svgDrawFeature scores litho's SVG renderer on resvg's tests. Each test's
// SVG is converted by the HTML converter as the one image of a page of the
// reference PNG's size, an img element of that size with no margin, so the
// drawing fills the page as resvg scales it to the PNG's width; the page is
// drawn at 96 dpi and compared with the reference under the render cell's
// pixel rule (channel tolerance 64, neighbourhood 1, 1% of the pixels).
// The sizes must agree to the pixel. A conversion or drawing failure is an
// error, not a failure, and every test's outcome lands in the baseline.
//
// Each document is converted with the file system rooted at the test's own
// directory, and with the fixture fonts plus the Noto fonts resvg draws its
// text tests with.
type svgDrawFeature struct {
	convert htmlConverter

	mu        sync.RWMutex
	tests     map[string]*svgTest
	fontDirs  []string
	reportDir string
}

func newSVGDrawFeature() *svgDrawFeature {
	return &svgDrawFeature{convert: convertHTML, tests: make(map[string]*svgTest)}
}

func (f *svgDrawFeature) Name() string         { return "svgdraw" }
func (f *svgDrawFeature) GoldenSource() string { return "svg" }

// RecordsOutcomes records each test's outcome in the baseline.
func (f *svgDrawFeature) RecordsOutcomes() {}

// SetReportDir sets where failing tests are drawn.
func (f *svgDrawFeature) SetReportDir(dir string) {
	f.mu.Lock()
	f.reportDir = dir
	f.mu.Unlock()
}

// LoadGoldens reads tests.json from dir, the corpus source directory, and
// resolves the fonts the tests are converted with.
func (f *svgDrawFeature) LoadGoldens(dir string) error {
	data, err := os.ReadFile(filepath.Join(dir, "tests.json")) //nolint:gosec // committed corpus list
	if err != nil {
		return ErrNoGoldens
	}
	var entries []*svgTest
	if err := json.Unmarshal(data, &entries); err != nil {
		return fmt.Errorf("decode tests.json: %w", err)
	}
	tests := make(map[string]*svgTest, len(entries))
	for _, e := range entries {
		if !validInputID(e.ID) || !validInputID(e.PNG) || !strings.HasSuffix(e.ID, ".svg") ||
			e.Width <= 0 || e.Height <= 0 || e.Width > maxSVGReference || e.Height > maxSVGReference {
			return fmt.Errorf("tests.json: entry %+v is invalid", *e)
		}
		tests[e.ID] = e
	}
	if len(tests) == 0 {
		return ErrNoGoldens
	}
	fonts, err := fixtureFontDirs(fixtureFontsRoot(), svgFontsTarget, "resvg")
	if err != nil {
		return err
	}
	f.mu.Lock()
	f.tests, f.fontDirs = tests, fonts
	f.mu.Unlock()
	return nil
}

func (f *svgDrawFeature) HasGolden(fixtureID string) bool { return f.test(fixtureID) != nil }

func (f *svgDrawFeature) test(id string) *svgTest {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.tests[id]
}

// svgDrawOutput is Run's output.
type svgDrawOutput struct {
	Outcome string `json:"outcome"`
	Reason  string `json:"reason,omitempty"`
}

// Run converts and draws the test and compares the drawing with its
// reference PNG. srcPath is the SVG under the fetched tree, whose root is
// the path with the test ID removed.
func (f *svgDrawFeature) Run(ctx context.Context, srcPath, fixtureID string) ([]byte, error) {
	t := f.test(fixtureID)
	if t == nil {
		return json.Marshal(svgDrawOutput{Outcome: outcomeError, Reason: "not in tests.json"})
	}
	root := strings.TrimSuffix(filepath.ToSlash(srcPath), t.ID)
	ref, err := readReferencePNG(filepath.Join(filepath.FromSlash(root), filepath.FromSlash(t.PNG)))
	if err != nil {
		return json.Marshal(svgDrawOutput{Outcome: outcomeError, Reason: "reference: " + err.Error()})
	}
	got, err := f.draw(ctx, root, t)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return json.Marshal(svgDrawOutput{Outcome: outcomeError, Reason: err.Error()})
	}
	d := comparePixels(got, ref, renderPixelRule, 0)
	if d.within(renderPixelRule) {
		return json.Marshal(svgDrawOutput{Outcome: outcomePass})
	}
	f.mu.RLock()
	dir := f.reportDir
	f.mu.RUnlock()
	if dir != "" {
		if err := writeDiffReport(filepath.Join(dir, reportName(fixtureID)+".png"), got, ref, d, 1); err != nil {
			fmt.Fprintf(os.Stderr, "svgdraw: report for %s: %v\n", fixtureID, err)
		}
	}
	return json.Marshal(svgDrawOutput{Outcome: outcomeFail, Reason: d.String()})
}

// svgPage is the HTML page a test is converted on: the SVG as an img
// element of the reference's size, filling a page as large.
const svgPage = `<!DOCTYPE html><html><head><style>html, body { margin: 0 } img { display: block; width: %dpx; height: %dpx }</style></head><body><img src="%s" alt=""></body></html>`

// draw converts the test's SVG on its page and draws the first page.
func (f *svgDrawFeature) draw(ctx context.Context, root string, t *svgTest) (image.Image, error) {
	f.mu.RLock()
	fonts := f.fontDirs
	f.mu.RUnlock()
	src := fmt.Sprintf(svgPage, t.Width, t.Height, path.Base(t.ID))
	data, err := f.convert(ctx, []byte(src), htmlSettings{
		Media:     "screen",
		Width:     float64(t.Width) * 0.75,
		Height:    float64(t.Height) * 0.75,
		Resources: os.DirFS(filepath.Join(filepath.FromSlash(root), filepath.FromSlash(path.Dir(t.ID)))),
		FontDirs:  fonts,
	})
	if err != nil {
		return nil, fmt.Errorf("convert: %w", err)
	}
	pages, err := renderPDF(ctx, data, svgDrawDPI, 1)
	if err != nil {
		return nil, err
	}
	if len(pages) == 0 {
		return nil, fmt.Errorf("the conversion has no page")
	}
	return pages[0], nil
}

// readReferencePNG decodes a reference PNG, refusing one larger than
// maxSVGReference along a side before decoding its pixels.
func readReferencePNG(name string) (image.Image, error) {
	fh, err := os.Open(name) //nolint:gosec // reference of the fetched tree, listed in tests.json
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	defer fh.Close() //nolint:errcheck // read-only file
	cfg, err := png.DecodeConfig(fh)
	if err != nil {
		return nil, fmt.Errorf("decode header: %w", err)
	}
	if cfg.Width > maxSVGReference || cfg.Height > maxSVGReference {
		return nil, fmt.Errorf("%dx%d pixels, over %d", cfg.Width, cfg.Height, maxSVGReference)
	}
	if _, err := fh.Seek(0, 0); err != nil {
		return nil, fmt.Errorf("rewind: %w", err)
	}
	img, err := png.Decode(fh)
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return img, nil
}

// Compare turns Run's outcome into a verdict.
func (f *svgDrawFeature) Compare(fixtureID string, output []byte) Verdict {
	return outcomeVerdict(fixtureID, output)
}
