package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/gotenberg/litho/pkg/pdf"
)

// renderDPI is the resolution the render goldens are drawn at, mirroring
// RENDER_DPI in corpus/lib/gen-render.sh: one pixel per default user space
// unit (ISO 32000-1, 8.3.2.3), so a page maps to device space by an integer
// scale and a US Letter page is 612x792 pixels.
const renderDPI = 72

// renderMaxPages is how many leading pages of a PDF the goldens hold,
// mirroring RENDER_MAX_PAGES in corpus/lib/gen-render.sh. Most fixtures are
// one page; three reach the page-level resources, inheritance and rotation a
// later page carries without growing the goldens with every page of a long
// document.
const renderMaxPages = 3

// renderChannelTolerance is the largest difference, in 8-bit levels, a pixel's
// red, green or blue may show against the golden and still count as equal.
//
// Two correct anti-aliasing rasterizers disagree on an edge pixel's coverage:
// poppler's Splash samples 4x4 points per pixel, so its coverage moves in
// steps of 1/16, and FreeType renders glyphs with its own coverage rule. On a
// black edge over white that is a few tens of levels. A shape drawn in the
// wrong place, in the wrong colour or not at all differs by far more on the
// pixels it covers: 255 for black on white.
const renderChannelTolerance = 64

// renderNeighbourhood is the radius, in pixels, of the window a pixel looks for
// its match in. Snapping a thin stroke to the pixel grid (Splash's stroke
// adjustment) or rounding a glyph origin moves an edge by up to one pixel;
// the window lets the edge match where the other rasterizer put it. A shift
// of the whole page by one pixel passes too, the price of tolerating the two.
const renderNeighbourhood = 1

// renderMaxDiffPerMillion is the largest share of a page's pixels, in parts
// per million, that may differ for the page to match: 1%. It absorbs the
// residue of anti-aliasing the tolerances above leave (a hairline whose
// coverage two rasterizers split across different pixels) while a missing
// glyph run, image or fill still fails: one line of 12-point text across a
// Letter page covers about 1.5% of it.
//
// # Corpus evidence
//
//   - litho: poppler's pdftocairo, a second anti-aliasing rasterizer over the
//     same parser, matches 302 of the 309 golden pages Splash drew under
//     these tolerances, run on a macOS host with its own fonts. Each of the 7 others
//     differs at a glance: complex_2b upsamples a small image with another
//     filter, gstate_2b paints rectangles Splash does not.
const renderMaxDiffPerMillion = 10_000

// renderSizeSlack is how many pixels litho's page may differ from the
// golden's along each axis. A crop box whose width is not a whole number of
// units rounds to a pixel count either way; the pixels compared are the
// overlap of the two images.
const renderSizeSlack = 1

// metricRenderFailed counts the golden pages litho failed to render.
const metricRenderFailed = "render_failed"

// renderFontsDir is the directory, under goldens/render, that holds the font
// files poppler rendered the goldens with, mirroring the copy entrypoint.sh
// makes of the image's poppler bundle.
const renderFontsDir = "fonts"

// pageRenderer renders one page (1-based) of an open document at dpi, with
// opts, into an image whose (0,0) is the top-left corner of the page's crop
// box. A pixel that is not opaque is composed over white, the background
// pdftoppm draws.
type pageRenderer func(ctx context.Context, r *pdf.Reader, page int, dpi float64, opts ...pdf.RenderOption) (image.Image, error)

// lithoRenderPage is the render feature's hook into litho: [pdf.Reader.RenderPage]
// over white, the background pdftoppm draws. Set to nil, every fixture
// scores as skipped.
//
//nolint:gochecknoglobals // the one switch the renderer integration flips
var lithoRenderPage pageRenderer = func(ctx context.Context, r *pdf.Reader, page int, dpi float64, opts ...pdf.RenderOption) (image.Image, error) {
	return r.RenderPage(ctx, page, append([]pdf.RenderOption{pdf.WithRenderDPI(dpi)}, opts...)...)
}

// renderFeature renders the first pages of each PDF with litho and compares
// them, pixel by pixel within the tolerances above, with the pages poppler's
// pdftoppm rendered at provisioning. It is the gate's reach into rendering:
// no other feature looks at what a page draws.
//
// litho bundles no font program, and poppler draws the fonts a document does
// not embed with the fonts of the image it runs in: URW base 35, the
// metric-compatible stand-ins of the standard 14 fonts, and DejaVu. For a like
// for like comparison, provisioning copies those fonts to
// goldens/render/fonts, and the feature hands them to litho through
// [pdf.WithRenderFontFiles]. Testdata provisioned before the copy existed
// has none: litho then draws such fonts as placeholder boxes, and the feature
// says so once.
type renderFeature struct {
	render pageRenderer

	mu      sync.RWMutex
	dir     string
	goldens map[string]*renderIndex
	// fonts draws the fonts a document does not embed with the fonts the
	// goldens were drawn with, nil when the testdata holds none. One option
	// value serves every render, so its index is built once.
	fonts pdf.RenderOption
}

func newRenderFeature() *renderFeature {
	return &renderFeature{render: lithoRenderPage, goldens: make(map[string]*renderIndex)}
}

// renderIndex is the per-PDF index gen-render.sh writes next to the pages it
// rendered.
type renderIndex struct {
	ID        string            `json:"id"`
	DPI       float64           `json:"dpi"`
	PageCount int               `json:"page_count"`
	Pages     []renderIndexPage `json:"pages"`
	Error     string            `json:"error,omitempty"`
}

// renderIndexPage names one page's golden PNG, or why poppler rendered none.
type renderIndexPage struct {
	Page  int    `json:"page"`
	File  string `json:"file,omitempty"`
	Error string `json:"error,omitempty"`
}

// renderOutput is the feature's Run output: the comparison of every page
// with a golden, made in Run so no page image crosses to Compare.
type renderOutput struct {
	NotImplemented bool         `json:"not_implemented,omitempty"`
	Pages          []pageResult `json:"pages,omitempty"`
}

// pageResult is the comparison of one page.
type pageResult struct {
	Page         int    `json:"page"`
	Width        int    `json:"width,omitempty"`
	Height       int    `json:"height,omitempty"`
	GoldenWidth  int    `json:"golden_width,omitempty"`
	GoldenHeight int    `json:"golden_height,omitempty"`
	Pixels       int    `json:"pixels,omitempty"`
	DiffPixels   int    `json:"diff_pixels,omitempty"`
	MaxDelta     int    `json:"max_delta,omitempty"`
	Match        bool   `json:"match"`
	Error        string `json:"error,omitempty"`
	GoldenError  string `json:"golden_error,omitempty"`
}

func (f *renderFeature) Name() string         { return "render" }
func (f *renderFeature) GoldenSource() string { return "poppler" }

// OncePerPDF marks the feature as profile-independent: a page draws the same
// whatever profile an expand manifest scores the PDF under.
func (f *renderFeature) OncePerPDF() {}

func (f *renderFeature) LoadGoldens(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ErrNoGoldens
	}
	loaded := make(map[string]*renderIndex)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return fmt.Errorf("read render golden %s: %w", e.Name(), err)
		}
		var idx renderIndex
		if err := json.Unmarshal(data, &idx); err != nil {
			return fmt.Errorf("decode render golden %s: %w", e.Name(), err)
		}
		// A golden drawn at another resolution has other pixel counts, so it
		// would mismatch every page: refuse it loudly instead.
		if idx.DPI != renderDPI {
			return fmt.Errorf("render golden %s was drawn at %g dpi, the feature compares at %d: re-provision goldens/render", e.Name(), idx.DPI, renderDPI)
		}
		for _, p := range idx.Pages {
			if p.Page < 1 || p.Page > renderMaxPages {
				return fmt.Errorf("render golden %s holds page %d, outside the first %d the feature compares: re-provision goldens/render", e.Name(), p.Page, renderMaxPages)
			}
		}
		loaded[strings.TrimSuffix(e.Name(), ".json")] = &idx
	}
	if len(loaded) == 0 {
		return ErrNoGoldens
	}
	var fonts pdf.RenderOption
	fontsDir := filepath.Join(dir, renderFontsDir)
	if info, err := os.Stat(fontsDir); err == nil && info.IsDir() {
		fonts = pdf.WithRenderFontFiles(os.DirFS(fontsDir))
	} else {
		fmt.Fprintf(os.Stderr, "render: %s holds no %s directory, so litho draws unembedded fonts as boxes: re-provision goldens/render\n", dir, renderFontsDir)
	}
	f.mu.Lock()
	f.dir = dir
	f.goldens = loaded
	f.fonts = fonts
	f.mu.Unlock()
	return nil
}

func (f *renderFeature) HasGolden(fixtureID string) bool {
	return f.golden(fixtureID) != nil
}

func (f *renderFeature) golden(fixtureID string) *renderIndex {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.goldens[baseFixtureID(fixtureID)]
}

// Run renders each page the golden holds a PNG for and compares it with that
// PNG. Until litho renders, it opens nothing and reports so.
func (f *renderFeature) Run(ctx context.Context, pdfPath, fixtureID string) ([]byte, error) {
	golden := f.golden(fixtureID)
	if f.render == nil {
		return json.Marshal(renderOutput{NotImplemented: true})
	}
	var out renderOutput
	if golden == nil || golden.Error != "" || renderedPages(golden) == 0 {
		return json.Marshal(out)
	}
	file, err := os.Open(pdfPath) //nolint:gosec // corpus fixture path
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat: %w", err)
	}
	r, err := pdf.Open(ctx, file, info.Size())
	if err != nil {
		return nil, fmt.Errorf("pdf.Open: %w", err)
	}
	defer r.Close()
	f.mu.RLock()
	dir, fonts := f.dir, f.fonts
	f.mu.RUnlock()
	var opts []pdf.RenderOption
	if fonts != nil {
		opts = append(opts, fonts)
	}
	for _, gp := range golden.Pages {
		if gp.File == "" {
			continue
		}
		res := pageResult{Page: gp.Page}
		want, err := readPNG(filepath.Join(dir, gp.File))
		if err != nil {
			res.GoldenError = err.Error()
			out.Pages = append(out.Pages, res)
			continue
		}
		got, err := f.render(ctx, r, gp.Page, renderDPI, opts...)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			res.Error = err.Error()
			out.Pages = append(out.Pages, res)
			continue
		}
		res = comparePage(got, want)
		res.Page = gp.Page
		out.Pages = append(out.Pages, res)
	}
	return json.Marshal(out)
}

// renderedPages counts the pages poppler rendered.
func renderedPages(g *renderIndex) int {
	n := 0
	for _, p := range g.Pages {
		if p.File != "" {
			n++
		}
	}
	return n
}

func readPNG(p string) (image.Image, error) {
	file, err := os.Open(p) //nolint:gosec // golden file under the corpus testdata root
	if err != nil {
		return nil, fmt.Errorf("open golden: %w", err)
	}
	defer file.Close()
	img, err := png.Decode(file)
	if err != nil {
		return nil, fmt.Errorf("decode golden %s: %w", filepath.Base(p), err)
	}
	return img, nil
}

// Compare scores the fraction of golden pages that match. A PDF poppler could
// not open, one it rendered no page of, and every PDF while litho does not
// render, are skipped.
func (f *renderFeature) Compare(fixtureID string, output []byte) Verdict {
	golden := f.golden(fixtureID)
	if golden == nil {
		return Verdict{FixtureID: fixtureID, Skipped: true, Reason: "no golden"}
	}
	var got renderOutput
	if err := json.Unmarshal(output, &got); err != nil {
		return Verdict{FixtureID: fixtureID, Reason: fmt.Sprintf("decode output: %v", err)}
	}
	if got.NotImplemented {
		return Verdict{FixtureID: fixtureID, Skipped: true, Reason: "litho does not render pages yet"}
	}
	if golden.Error != "" {
		return Verdict{FixtureID: fixtureID, Skipped: true, Reason: "poppler: " + golden.Error}
	}
	total := renderedPages(golden)
	if total == 0 {
		return Verdict{FixtureID: fixtureID, Skipped: true, Reason: "poppler rendered no page"}
	}
	byPage := make(map[int]pageResult, len(got.Pages))
	for _, p := range got.Pages {
		byPage[p.Page] = p
	}
	matched, failed := 0, 0
	var mismatches []json.RawMessage
	for _, gp := range golden.Pages {
		if gp.File == "" {
			continue
		}
		res, ok := byPage[gp.Page]
		switch {
		case !ok:
			res = pageResult{Page: gp.Page, Error: "not rendered"}
			failed++
		case res.Match:
			matched++
			continue
		case res.Error != "":
			failed++
		}
		data, err := json.Marshal(res)
		if err != nil {
			data = json.RawMessage(`{"error":"marshal"}`)
		}
		mismatches = append(mismatches, data)
	}
	v := Verdict{
		FixtureID:  fixtureID,
		Match:      matched == total,
		Score:      float64(matched) / float64(total),
		Metrics:    map[string]int{"pages_match": matched, metricRenderFailed: failed},
		Mismatches: mismatches,
	}
	if !v.Match {
		v.Reason = fmt.Sprintf("%d of %d pages match", matched, total)
	}
	return v
}

// errRenderSize reports a rendering whose size lies outside renderSizeSlack of
// the golden's.
var errRenderSize = errors.New("page size differs from the golden's")

// comparePage compares litho's rendering of a page with the golden's, both
// composed over white. A pixel differs when no pixel within
// renderNeighbourhood of its place in the other image lies within
// renderChannelTolerance of it, checked both ways, so an edge one rasterizer
// put a pixel away from the other still matches. MaxDelta is the largest
// channel difference at the same place, before the neighbourhood applies.
//
// Cost: O(w*h) when the images agree pixel for pixel, O(w*h*(2r+1)^2) at
// worst.
func comparePage(got, want image.Image) pageResult {
	gb, wb := got.Bounds(), want.Bounds()
	res := pageResult{Width: gb.Dx(), Height: gb.Dy(), GoldenWidth: wb.Dx(), GoldenHeight: wb.Dy()}
	if absInt(res.Width-res.GoldenWidth) > renderSizeSlack || absInt(res.Height-res.GoldenHeight) > renderSizeSlack {
		res.Error = fmt.Sprintf("%v: %dx%d, golden %dx%d", errRenderSize, res.Width, res.Height, res.GoldenWidth, res.GoldenHeight)
		return res
	}
	w, h := min(res.Width, res.GoldenWidth), min(res.Height, res.GoldenHeight)
	g, wt := flattenOnWhite(got, w, h), flattenOnWhite(want, w, h)
	res.Pixels = w * h
	for y := range h {
		for x := range w {
			i := 3 * (y*w + x)
			d := channelDelta(g[i:i+3], wt[i:i+3])
			res.MaxDelta = max(res.MaxDelta, d)
			if d <= renderChannelTolerance {
				continue
			}
			if !nearMatch(g[i:i+3], wt, w, h, x, y) || !nearMatch(wt[i:i+3], g, w, h, x, y) {
				res.DiffPixels++
			}
		}
	}
	res.Match = int64(res.DiffPixels)*1_000_000 <= int64(renderMaxDiffPerMillion)*int64(res.Pixels)
	return res
}

// nearMatch reports whether a pixel within renderNeighbourhood of (x, y) in
// img lies within renderChannelTolerance of px.
func nearMatch(px, img []uint8, w, h, x, y int) bool {
	for ny := max(0, y-renderNeighbourhood); ny <= min(h-1, y+renderNeighbourhood); ny++ {
		for nx := max(0, x-renderNeighbourhood); nx <= min(w-1, x+renderNeighbourhood); nx++ {
			j := 3 * (ny*w + nx)
			if channelDelta(px, img[j:j+3]) <= renderChannelTolerance {
				return true
			}
		}
	}
	return false
}

// channelDelta is the largest difference between two RGB pixels' channels.
func channelDelta(a, b []uint8) int {
	return max(absInt(int(a[0])-int(b[0])), absInt(int(a[1])-int(b[1])), absInt(int(a[2])-int(b[2])))
}

// flattenOnWhite returns the top-left w by h pixels of img as 8-bit RGB
// triples, each composed over white. image.Image reports premultiplied
// colour, so composing adds the uncovered share of white: c + (1 - a).
func flattenOnWhite(img image.Image, w, h int) []uint8 {
	b := img.Bounds()
	out := make([]uint8, 3*w*h)
	for y := range h {
		for x := range w {
			r, g, bl, a := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			white := 0xffff - a
			i := 3 * (y*w + x)
			out[i] = uint8((r + white) >> 8)    //nolint:gosec // r <= a, so r + (0xffff - a) <= 0xffff
			out[i+1] = uint8((g + white) >> 8)  //nolint:gosec // g <= a, so the sum stays within 16 bits
			out[i+2] = uint8((bl + white) >> 8) //nolint:gosec // b <= a, so the sum stays within 16 bits
		}
	}
	return out
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
