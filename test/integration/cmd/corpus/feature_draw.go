package main

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/gotenberg/litho/pkg/pdf"
)

// drawDPI is the resolution the WeasyPrint draw tests are drawn at: their
// grids hold one pixel per CSS pixel, 96 dpi.
const drawDPI = 96

// drawPixelRule is how a drawn pixel is compared with its grid cell.
// WeasyPrint draws its tests with Ghostscript without anti-aliasing and
// compares every pixel exactly. litho draws anti-aliased, so a glyph or a
// rounded corner whose edge falls inside a pixel blends it: the render
// cell's channel tolerance of 64 accepts such a blend, while white, black
// and the primaries most grids use lie 127 levels and more apart. The
// blended colours of the opacity and CMYK tests lie closer (S and s, h and v
// 64 levels, P and Q 13, V and p 1), and the rule cannot tell those apart.
// The neighbourhood is 0 and no pixel may differ: the grids are a few pixels
// across, and a pixel out of place is the defect the tests are written to
// catch.
//
//nolint:gochecknoglobals // a fixed rule, read only
var drawPixelRule = pixelRule{Tolerance: renderChannelTolerance}

// drawFontsTarget is the make target that extracts the fixtures and fetches
// the fonts the draw feature reads.
const drawFontsTarget = "make weasyprint-fixtures"

// drawFixture is one fixture corpus select writes for the weasyprint corpus.
type drawFixture struct {
	Name     string   `json:"name"`
	Source   string   `json:"source"`
	XFail    bool     `json:"xfail"`
	HTML     string   `json:"html"`
	Expected []string `json:"expected"`
	// Pixels maps each grid character to its colour; a null colour is a
	// pixel the test does not check.
	Pixels map[string]*[3]int `json:"pixels"`
}

// drawFeature scores litho's HTML converter on WeasyPrint's draw tests: each
// fixture's HTML is converted for print, every page drawn at 96 dpi, the
// pages stacked top to bottom as WeasyPrint's test harness stacks them, and
// the result compared with the test's pixel grid under [drawPixelRule]. The
// sizes must agree. A conversion or drawing failure is an error, not a
// failure, and every fixture's outcome lands in the baseline.
//
// WeasyPrint runs its tests under a user agent stylesheet of its own
// (tests/resources/tests_ua.css): display values only, no margin anywhere,
// and the test font. The grids assume it, so the feature prepends
// corpus/weasyprint/ua.css to each document, which takes the margins and
// paddings a browser's stylesheet gives back out. The tests set their page
// size with @page themselves; the feature asks for no page margin. Each
// document is converted with the file system rooted at the corpus resources
// directory, where the tests' images are, and with the fixture fonts plus
// the WeasyPrint test font.
type drawFeature struct {
	convert htmlConverter

	mu        sync.RWMutex
	fixtures  map[string]*drawFixture
	ua        string
	resources string
	fontDirs  []string
	reportDir string
}

func newDrawFeature() *drawFeature {
	return &drawFeature{convert: convertHTML, fixtures: make(map[string]*drawFixture)}
}

func (f *drawFeature) Name() string         { return "draw" }
func (f *drawFeature) GoldenSource() string { return "weasyprint" }

// RecordsOutcomes records each fixture's outcome in the baseline.
func (f *drawFeature) RecordsOutcomes() {}

// SetReportDir sets where failing fixtures are drawn.
func (f *drawFeature) SetReportDir(dir string) {
	f.mu.Lock()
	f.reportDir = dir
	f.mu.Unlock()
}

// LoadGoldens reads every fixture under dir, the corpus fixtures directory,
// the user agent stylesheet beside it, and resolves the fonts.
func (f *drawFeature) LoadGoldens(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ErrNoGoldens
	}
	fixtures := make(map[string]*drawFixture, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name())) //nolint:gosec // committed corpus fixture
		if err != nil {
			return fmt.Errorf("read draw fixture %s: %w", e.Name(), err)
		}
		var fx drawFixture
		if err := json.Unmarshal(data, &fx); err != nil {
			return fmt.Errorf("decode draw fixture %s: %w", e.Name(), err)
		}
		if _, _, err := fx.grid(); err != nil {
			return fmt.Errorf("draw fixture %s: %w", e.Name(), err)
		}
		fixtures[strings.TrimSuffix(e.Name(), ".json")] = &fx
	}
	if len(fixtures) == 0 {
		return ErrNoGoldens
	}
	parent := filepath.Dir(dir)
	ua, err := os.ReadFile(filepath.Join(parent, "ua.css")) //nolint:gosec // committed corpus stylesheet
	if err != nil {
		return fmt.Errorf("read the draw tests' user agent stylesheet: %w", err)
	}
	fonts, err := fixtureFontDirs(fixtureFontsRoot(), drawFontsTarget, "weasyprint")
	if err != nil {
		return err
	}
	f.mu.Lock()
	f.fixtures, f.ua, f.resources, f.fontDirs = fixtures, string(ua), filepath.Join(parent, "resources"), fonts
	f.mu.Unlock()
	return nil
}

// maxGridPixels bounds the expected grid of one fixture. Anti-DoS guard, not
// spec-mandated: the grids are typed by hand, the largest a few hundred
// pixels, and a grid is held as one colour per pixel.
const maxGridPixels = 1 << 20

// grid returns the expected grid's size, failing when its rows differ in
// width, it is empty or too large, or a character has no colour.
func (fx *drawFixture) grid() (int, int, error) {
	if len(fx.Expected) == 0 {
		return 0, 0, fmt.Errorf("empty grid")
	}
	w := len(fx.Expected[0])
	for _, row := range fx.Expected {
		if len(row) != w {
			return 0, 0, fmt.Errorf("rows of %d and %d pixels", w, len(row))
		}
		for _, c := range row {
			if _, ok := fx.Pixels[string(c)]; !ok {
				return 0, 0, fmt.Errorf("character %q has no colour", c)
			}
		}
	}
	if w == 0 || w*len(fx.Expected) > maxGridPixels {
		return 0, 0, fmt.Errorf("grid of %dx%d pixels", w, len(fx.Expected))
	}
	return w, len(fx.Expected), nil
}

func (f *drawFeature) HasGolden(fixtureID string) bool { return f.fixture(fixtureID) != nil }

func (f *drawFeature) fixture(id string) *drawFixture {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.fixtures[id]
}

// drawOutput is Run's output.
type drawOutput struct {
	Outcome string `json:"outcome"`
	Reason  string `json:"reason,omitempty"`
}

// Run converts the fixture's HTML, draws and stacks its pages, and compares
// them with the grid.
func (f *drawFeature) Run(ctx context.Context, _, fixtureID string) ([]byte, error) {
	fx := f.fixture(fixtureID)
	if fx == nil {
		return json.Marshal(drawOutput{Outcome: outcomeError, Reason: "no fixture"})
	}
	f.mu.RLock()
	ua, resources, fonts, reportDir := f.ua, f.resources, f.fontDirs, f.reportDir
	f.mu.RUnlock()
	src := "<style>\n" + ua + "</style>\n" + fx.HTML
	data, err := f.convert(ctx, []byte(src), htmlSettings{
		Media:     "print",
		Top:       0,
		Right:     0,
		Bottom:    0,
		Left:      0,
		Resources: os.DirFS(resources),
		FontDirs:  fonts,
		// WeasyPrint's @page size is the page, WeasyPrint shrinks nothing
		// to fit it and keeps the fonts' fractional metrics.
		PreferCSSPageSize: true,
		Print:             []pdf.ConvertOption{pdf.WithShrinkToFit(false), pdf.WithFractionalFontMetrics(true)},
	})
	if err == nil {
		var pages []image.Image
		pages, err = renderPDF(ctx, data, drawDPI, 0)
		if err == nil {
			got := stackPages(pages)
			out := compareGrid(got, fx)
			if out.Outcome == outcomeFail && reportDir != "" {
				f.report(reportDir, fixtureID, got, fx)
			}
			return json.Marshal(out)
		}
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return json.Marshal(drawOutput{Outcome: outcomeError, Reason: err.Error()})
}

// stackPages stacks pages top to bottom, each centred horizontally on the
// widest, as WeasyPrint's test harness pastes the PNGs Ghostscript draws.
// The harness pastes onto a transparent canvas, which reads as black where
// a page narrower than the widest leaves it uncovered; so does this one.
func stackPages(pages []image.Image) image.Image {
	w, h := 0, 0
	for _, p := range pages {
		w = max(w, p.Bounds().Dx())
		h += p.Bounds().Dy()
	}
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(out, out.Bounds(), image.NewUniform(color.Black), image.Point{}, draw.Src)
	top := 0
	for _, p := range pages {
		b := p.Bounds()
		left := (w - b.Dx()) / 2
		dst := image.Rect(left, top, left+b.Dx(), top+b.Dy())
		draw.Draw(out, dst, image.NewUniform(color.White), image.Point{}, draw.Src)
		draw.Draw(out, dst, p, b.Min, draw.Over)
		top += b.Dy()
	}
	return out
}

// compareGrid compares a drawing with the fixture's grid: the sizes must
// agree, and every pixel the grid checks must lie within drawPixelRule of its
// colour.
func compareGrid(got image.Image, fx *drawFixture) drawOutput {
	w, h, err := fx.grid()
	if err != nil {
		return drawOutput{Outcome: outcomeError, Reason: err.Error()}
	}
	b := got.Bounds()
	if b.Dx() != w || b.Dy() != h {
		return drawOutput{Outcome: outcomeFail, Reason: fmt.Sprintf("size %dx%d, expected %dx%d", b.Dx(), b.Dy(), w, h)}
	}
	px := flattenOnWhite(got, w, h)
	diff, first := 0, ""
	for y, row := range fx.Expected {
		for x, c := range row {
			want := fx.Pixels[string(c)]
			if want == nil {
				continue
			}
			i := 3 * (y*w + x)
			wantPx := []uint8{clampByte(want[0]), clampByte(want[1]), clampByte(want[2])}
			if channelDelta(px[i:i+3], wantPx) > drawPixelRule.Tolerance {
				if diff == 0 {
					first = fmt.Sprintf("pixel (%d, %d) is rgb(%d, %d, %d), expected %q rgb(%d, %d, %d)",
						x, y, px[i], px[i+1], px[i+2], c, want[0], want[1], want[2])
				}
				diff++
			}
		}
	}
	if diff == 0 {
		return drawOutput{Outcome: outcomePass}
	}
	return drawOutput{Outcome: outcomeFail, Reason: fmt.Sprintf("%d of %d pixels differ; first: %s", diff, w*h, first)}
}

// clampByte narrows a colour channel to 0..255.
func clampByte(v int) uint8 {
	return uint8(min(max(v, 0), 0xff)) //nolint:gosec // clamped to 0..255 on this line
}

// gridImage paints the fixture's grid, an unchecked pixel in mid grey.
func gridImage(fx *drawFixture) image.Image {
	w, h, err := fx.grid()
	if err != nil {
		return image.NewRGBA(image.Rect(0, 0, 1, 1))
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y, row := range fx.Expected {
		for x, c := range row {
			col := color.RGBA{0x80, 0x80, 0x80, 0xff}
			if want := fx.Pixels[string(c)]; want != nil {
				col = color.RGBA{clampByte(want[0]), clampByte(want[1]), clampByte(want[2]), 0xff}
			}
			img.SetRGBA(x, y, col)
		}
	}
	return img
}

// drawReportSide is the size, in pixels, a failure report scales a grid up
// to along its longer side: the grids are a few pixels across.
const drawReportSide = 240

// report draws a failing fixture: the drawing, the grid and their
// differences, scaled up.
func (f *drawFeature) report(dir, id string, got image.Image, fx *drawFixture) {
	want := gridImage(fx)
	d := comparePixels(got, want, drawPixelRule, 0)
	b := want.Bounds()
	scale := drawReportSide / max(b.Dx(), b.Dy(), 1)
	if err := writeDiffReport(filepath.Join(dir, reportName(id)+".png"), got, want, d, min(max(scale, 1), 32)); err != nil {
		fmt.Fprintf(os.Stderr, "draw: report for %s: %v\n", id, err)
	}
}

// Compare turns Run's outcome into a verdict.
func (f *drawFeature) Compare(fixtureID string, output []byte) Verdict {
	return outcomeVerdict(fixtureID, output)
}
