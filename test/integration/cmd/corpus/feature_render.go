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

// renderMaxDiffPerInkMillion is the largest share of a page's inked pixels,
// in parts per million, that may differ for the page to match: 50%. A pixel
// is inked when either rendering puts it more than renderChannelTolerance
// from the white background. The page bar above weighs a difference against
// the whole page, so a page whose only content is a few glyphs passes it
// with every glyph missing: one word on a Letter page covers well under 1%.
// This bar weighs the same difference against what the page draws.
//
// A differing pixel is always inked: its channels lie more than
// renderChannelTolerance apart, so one of them lies that far from white. The
// share therefore runs from 0 to 100%, and a rendering that draws nothing
// where the other draws something sits at 100%: each inked pixel of the one
// is a differing pixel. The bar sits in the widest gap of the shares the
// pages that pass the page bar show, above every page whose difference is
// an edge one rasterizer places otherwise, and below every page where the
// two draw different things.
//
// # Corpus evidence
//
//   - All corpora: of the 662 golden pages the page bar passes, 15 share more
//     than 50% of their ink, all of them litho fixtures, and on each one
//     renderer draws a glyph, a fill or an annotation the other does not (the
//     litho corpus README lists and adjudicates them). The shares jump from
//     53% to 40% (litho cmappredef_2b, limits_2b), the widest gap above 31%.
//   - pdfjs issue11768_reduced: the CJK page the render feature's evidence
//     adjudicates as matching shares 31% of its ink (71 of 227 pixels).
//   - litho complex_2a, complex_2u, complex_3b, complex_4, typedreads_1b: an
//     image interpolated half a sample apart and a box edge anti-aliased
//     otherwise share 17% to 28%.
//   - A planted blank golden fails the bar on every page that draws ink
//     (TestComparePageInkShare).
const renderMaxDiffPerInkMillion = 500_000

// renderSizeSlack is how many pixels litho's page may differ from the
// golden's along each axis. A crop box whose width is not a whole number of
// units rounds to a pixel count either way; the pixels compared are the
// overlap of the two images.
const renderSizeSlack = 1

// metricRenderFailed counts the golden pages litho failed to render: an
// error from RenderPage, or a page it reported none for.
const metricRenderFailed = "render_failed"

// metricRenderSizeMismatch counts, in the render cell, the golden pages litho
// rendered at another size than poppler's, past renderSizeSlack. Kept apart
// from metricRenderFailed so a crash and a size disagreement read apart.
//
// # Corpus evidence
//
//   - litho kidsarray_2b pages 2 and 3, kidsrepair_1b page 3: 1 by 1 goldens
//     poppler wrote for pages it did not render. pageboxes_2b pages 1 to 3:
//     the page boxes litho reads as viewers do, 612 by 792 against 500 by
//     700. These 6 were all of litho's render_failed before the split.
//   - pdfjs bug1978317 page 1: a 1 by 1 golden; Pages-tree-refs page 2 is a
//     page litho does not count and stays in render_failed.
const metricRenderSizeMismatch = "render_size_mismatch"

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
// metric-compatible stand-ins of the standard 14 fonts, DejaVu, and the Noto
// Sans CJK face of each CJK character collection's region, which litho asks
// for by name for a CIDFont of that collection. For a like
// for like comparison, provisioning copies those fonts to
// goldens/render/fonts, and the feature hands them to litho through
// [pdf.WithRenderFontFiles]. Testdata provisioned before the copy existed
// has none: litho then draws such fonts as placeholder boxes, and the feature
// says so once.
//
// Some mismatches are adjudicated and stay. Like poppler, litho draws an
// annotation with no appearance stream from its dictionary where ISO
// 32000-1, 12.5.6 defines how (package render/annot), and draws no Text,
// FreeText, Stamp or other icon or text annotation without one. Poppler
// strokes a link's /Border over its appearance stream, an xpdf legacy: per
// 12.5.5 the appearance stream is the annotation's appearance, and an empty
// one draws nothing. Poppler stops at the /Count of a page tree whose
// subtree is a cycle, and caps a page's /Annots; ISO 32000-1, 7.7.3.2 makes
// the page tree a tree, so litho counts the leaves it reaches. For a font
// the file does not embed, poppler shrinks the substitute horizontally when
// its m is wider than the /Widths m, takes an Identity-ordered CID of a
// CIDFontType2 without /ToUnicode as Unicode, and picks a sans-serif fallback through fontconfig where the
// name and the descriptor's Serif flag (9.8.2, Table 123) ask for Courier.
// litho selects a CIDFont's substitute glyph by the character its /ToUnicode
// or its Adobe collection's UCS2 CMap gives the code (9.10.2), or, for a
// non-embedded Identity CIDFontType2, the glyph order PDF.js knows for the
// face it names: its CIDs are that face's glyph indices (9.7.4.2).
//
// # Corpus evidence
//
//   - pdfjs annotation-square-circle-without-appearance, issue13447: Square,
//     Circle, Line, PolyLine, Ink, Highlight and StrikeOut annotations with
//     no /AP, which both renderers build from the dictionary and which match.
//   - govdocs thread_300_300511 page 2: Link annotations with no /AP and no
//     /F, boxed from /Border and /C by both, which match.
//   - pdfjs bug886717: 45 Link annotations whose /AP /N is an empty stream,
//     boxed by poppler from /Border; poppler's own rendering with
//     -hide-annotations matches litho's page (1291 pixels, 0.26%), and so
//     does PDFium's (1428 pixels, 0.28%, 5.9% of the ink).
//   - pdfjs Pages-tree-refs: /Count 2 over one leaf and a /Kids cycle 3, 4,
//     5, 3; poppler writes a 1 by 1 golden for a page 2, litho reports one
//     page, and PDFium counts two but fails to load the second.
//   - pdfjs bug1978317: poppler refuses page 1, its /Annots past its 32768
//     cap, and writes a 1 by 1 golden. The annotations are borderless links
//     (/BS /W 0) that draw nothing. With them removed poppler draws the page,
//     and litho matches it (0 pixels differ). Before litho rounded a
//     clipping rectangle to whole rows as Splash does, 6951 pixels (1.43%,
//     15.9% of the ink) differed: underlines a half-pixel clip cuts, which
//     litho painted at half coverage.
//   - pdfjs bitmap-halftone-skip-grid-template2, issue17871_top_right: JBIG2
//     halftone regions poppler draws as nothing; bitmap-refine-tpgron: a
//     JBIG2 refinement with TPGRON poppler garbles. litho draws the test
//     image, and the pdf.js image goldens of the three agree with it.
//     PDFium draws bitmap-refine-tpgron and issue17871_top_right as litho
//     does (0 pixels differ) and garbles the skipped halftone grid.
//   - pdfjs bug1671312_reduced: an unembedded Helvetica,Bold whose /Widths
//     m is 608 against the substitute's 889; poppler draws every glyph 0.68
//     wide at the same positions. The /Widths are a regular face's shifted
//     out of place (s and e 326), so litho, scaling each glyph to its
//     width, draws "ses" narrow; PDFium scales none, and each of the three
//     differs from the others by 31 to 54 pixels (12% to 19% of the ink).
//   - pdfjs issue15594_reduced: an unembedded Calibri, Identity-H,
//     Adobe-Identity, no /ToUnicode: litho draws CIDs 9, 80, 109, 263, 386
//     and 442 as ÄÖÜäöü through the Calibri glyph order, as PDF.js does;
//     poppler draws U+0009, P, m, ć, Ƃ and ƺ, and PDFium a third reading
//     (69% of the ink differs from poppler, 90% from PDFium).
//   - govdocs thread_100_100161, thread_200_200618 pages 1 to 3: an
//     unembedded CourierNewPSMT (Serif flag set) poppler draws in DejaVu
//     Sans Mono, litho in Nimbus Mono PS. PDFium draws a Courier as litho
//     does: 325 to 477 pixels differ, 1.0% to 2.6% of the ink, against 28%
//     to 32% from poppler.
//   - pdfjs issue7200: a scan reduced 10:9, which poppler averages over
//     whole samples and litho weighs by area (package render/imgpaint).
//     PDFium agrees with litho: 1270 pixels (0.25%) differ, against 6055
//     (1.21%) from poppler.
//   - pdfjs issue12705: small Hebrew text whose glyph edges fall a pixel
//     apart, 1.16% of a 250 by 50 page: within reason, but over the bar.
//     PDFium draws it as litho does, 0 pixels apart. The glyphs are Type 3
//     glyphs that open with d1, which poppler draws into a bitmap cache
//     and lays at the pixel holding each glyph's origin
//     (SplashOutputDev::beginType3Char, Splash::fillGlyph), up to a pixel
//     from where ISO 32000-1, 9.6.5 places them. Drawn that way, litho
//     comes within 1 pixel of poppler and 111 from PDFium. Over the 71
//     pages of all corpora that drawing moves, 53 come closer to poppler
//     and 43 of 58 move away from PDFium, which places the glyphs exactly,
//     as litho does.
//   - pdfjs issue11768_reduced: an embedded Adobe-Japan1 CIDFontType2
//     under a CMap stream that uses UniJIS-UTF8-H. poppler drew nothing
//     without poppler-data's CMaps; with them both renderers draw it and
//     match (71 pixels differ).
//   - litho cmapcidtext_2b, fontembed_2b: unembedded Adobe-Japan1 fonts
//     showing B, A and U+3042, which poppler drew as nothing and litho with
//     Nimbus Sans, U+3042 as a box; both now draw them with Noto Sans JP,
//     and 1 and 60 pixels differ, 4 and 130 before litho asked for the CJK
//     face. cmapcollect_1b, cmappredef_2b, cmapusevert_1b
//     and cmapverabase_2b: embedded fonts of CJK collections with no
//     /ToUnicode, whose text poppler drew only with poppler-data.
//   - pdfjs, govdocs: the 10 pdfjs and 2 govdocs files above are every
//     render mismatch of the two corpora under the ink bar, re-read with
//     PDFium (pypdfium2 4.30.0) as a third renderer at 72 dpi: none is a
//     litho defect.
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
	InkPixels    int    `json:"ink_pixels,omitempty"`
	MaxDelta     int    `json:"max_delta,omitempty"`
	Match        bool   `json:"match"`
	Error        string `json:"error,omitempty"`
	GoldenError  string `json:"golden_error,omitempty"`
	// SizeMismatch marks a page litho rendered at another size than the
	// golden's, past renderSizeSlack: Error then says so, and the page
	// counts in render_size_mismatch rather than render_failed.
	SizeMismatch bool `json:"size_mismatch,omitempty"`
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
	matched, failed, sized := 0, 0, 0
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
		case res.SizeMismatch:
			sized++
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
		Metrics:    map[string]int{"pages_match": matched, metricRenderFailed: failed, metricRenderSizeMismatch: sized},
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
// put a pixel away from the other still matches. The page matches when the
// differing pixels stay within renderMaxDiffPerMillion of the page and within
// renderMaxDiffPerInkMillion of the pixels either rendering inks. MaxDelta is
// the largest channel difference at the same place, before the neighbourhood
// applies.
//
// Cost: O(w*h) when the images agree pixel for pixel, O(w*h*(2r+1)^2) at
// worst.
func comparePage(got, want image.Image) pageResult {
	gb, wb := got.Bounds(), want.Bounds()
	res := pageResult{Width: gb.Dx(), Height: gb.Dy(), GoldenWidth: wb.Dx(), GoldenHeight: wb.Dy()}
	if absInt(res.Width-res.GoldenWidth) > renderSizeSlack || absInt(res.Height-res.GoldenHeight) > renderSizeSlack {
		res.Error = fmt.Sprintf("%v: %dx%d, golden %dx%d", errRenderSize, res.Width, res.Height, res.GoldenWidth, res.GoldenHeight)
		res.SizeMismatch = true
		return res
	}
	w, h := min(res.Width, res.GoldenWidth), min(res.Height, res.GoldenHeight)
	g, wt := flattenOnWhite(got, w, h), flattenOnWhite(want, w, h)
	res.Pixels = w * h
	white := []uint8{0xff, 0xff, 0xff}
	for y := range h {
		for x := range w {
			i := 3 * (y*w + x)
			if channelDelta(g[i:i+3], white) > renderChannelTolerance || channelDelta(wt[i:i+3], white) > renderChannelTolerance {
				res.InkPixels++
			}
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
	res.Match = res.withinPageShare() &&
		int64(res.DiffPixels)*1_000_000 <= int64(renderMaxDiffPerInkMillion)*int64(res.InkPixels)
	return res
}

// withinPageShare reports whether the differing pixels stay within
// renderMaxDiffPerMillion of the page, the bar alone, without the share of
// the inked pixels renderMaxDiffPerInkMillion adds.
func (r pageResult) withinPageShare() bool {
	return int64(r.DiffPixels)*1_000_000 <= int64(renderMaxDiffPerMillion)*int64(r.Pixels)
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
