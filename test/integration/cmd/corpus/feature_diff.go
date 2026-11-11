package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/text/unicode/norm"

	"github.com/gotenberg/litho/pkg/models"
	"github.com/gotenberg/litho/pkg/pdf"
	"github.com/gotenberg/litho/test/integration/fixture"
)

// diffDPI is the resolution the diff goldens are drawn at, that of the render
// goldens: one pixel per point.
const diffDPI = renderDPI

// The layout score's tolerance of position (see [layoutScore]), in pixels
// at 72 dpi. A page is halved to one pixel per two points, blurred with a box
// of layoutBlur half-pixels each way, cut into bands of layoutBand
// half-pixel rows, and each band compared with the other page shifted by up
// to layoutShiftY rows and layoutShiftX columns. The blur, 12 points each
// way, turns words into blobs, so glyphs placed a pixel or two apart, or
// drawn by another rasterizer, read alike, while a line of other words, a
// missing block or an image out of place does not. The band search, 12
// points up or down and 4 across, absorbs a line pitch a fraction of a
// pixel apart, which drifts by a few pixels over a page, without letting a
// band slide onto the next line of text.
const (
	layoutBlur   = 6
	layoutBand   = 16
	layoutShiftY = 6
	layoutShiftX = 2
)

// diffScoreTolerance is how far a fixture's score may drop below the
// baseline's before corpus check calls it a regression: 0.02, two points of
// text F1 or of layout, a word in a paragraph of a hundred. Litho is
// deterministic, so a drop is a change of the converter, not noise; the
// tolerance lets a change that trades a little layout for a correctness fix
// through without a baseline recapture.
const diffScoreTolerance = 0.02

// The bars a fixture must clear on text and layout to count as matched in
// the cell's count. The baseline gates each score itself; the count reads how
// many fixtures look like Chromium's. Both sit between what Chromium's own
// PDF scores against its goldens, where litho reads and draws a PDF poppler
// and PDF.js read too, and what another document scores: the gap is the
// room a faithful conversion by another engine has.
//
// # Corpus evidence
//
//   - litho/html, gotenberg/html: each Chromium reference converted back through the feature (litho
//     reads and draws the PDF the goldens come from) scores text F1 1 on 11
//     sources and 0.89 on i18n, whose Arabic, Hebrew and Thai litho's text
//     extraction and PDF.js tokenise apart; layout 0.93 (i18n) to 0.99
//     (backgrounds, images, floats); strict 1 on every page.
//   - probe, html: litho's drawing of each reference's first page shifted by 1 or
//     2 pixels both ways, or stretched by 1% down the page (a line pitch a
//     fraction of a pixel apart), scores layout 0.92 to 0.99 against its
//     golden.
//   - litho/html, gotenberg/html: the lists reference scored against the 11 other sources reaches
//     text F1 0.14 and layout 0.05 at most (markdown), and a blank page
//     scores 0 on both.
const (
	diffTextBar   = 0.85
	diffLayoutBar = 0.75
)

// The scores the diff feature records per fixture, each in [0, 1].
const (
	scorePages  = "pages"
	scoreText   = "text_f1"
	scoreLinks  = "link_recall"
	scoreLayout = "layout"
	scoreStrict = "strict"
	// scoreOutline is the share of the reference's outline items, by title
	// and target page, litho's outline holds too; 1 when the reference has
	// no outline. A CSV fixture's items are matched by page alone:
	// LibreOffice names the sheet, and its outline entry, after the
	// upload's file name, a UUID of Gotenberg's.
	scoreOutline = "outline_recall"
)

// diffPageSizeSlack is how far, in points, a page's width or height may lie
// from the reference's before the page counts as another size: 1 point,
// above the rounding of Chromium's media boxes (595.92 for A4's 595.28, a
// 1/300 inch grid) and below any size a print option sets, a landscape page
// or a single page cut to its content differing by tens of points.
const diffPageSizeSlack = 1.0

// metricPagesEqual counts the fixtures whose page count is Chromium's.
const metricPagesEqual = "pages_equal"

// diffFontsTarget is the make target that fetches the fonts the diff feature
// converts with.
const diffFontsTarget = "make corpus-fetch CORPUS=fonts"

// diffLinkGolden is the golden gen-links.mjs writes for one Chromium PDF.
type diffLinkGolden struct {
	ID       string     `json:"id"`
	NumPages int        `json:"numPages"`
	Links    []diffLink `json:"links"`
	Error    string     `json:"error,omitempty"`
}

// diffLink is one Link annotation: its page, rectangle and target.
type diffLink struct {
	Page     int       `json:"page"`
	Rect     []float64 `json:"rect"`
	URI      string    `json:"uri,omitempty"`
	DestPage int       `json:"destPage,omitempty"`
}

// key is what two links must share to count as the same link: the page and
// the target. Rectangles move with any layout difference, so they are not
// compared. A URI is compared normalised (see [normalizeLinkURI]).
func (l diffLink) key() string {
	if l.URI != "" {
		return fmt.Sprintf("%d uri %s", l.Page, normalizeLinkURI(l.URI))
	}
	return fmt.Sprintf("%d page %d", l.Page, l.DestPage)
}

// normalizeLinkURI writes a URI the way a browser serialises it, so two
// spellings of one target compare equal: the scheme and host lower-cased, and
// an empty path of a URI with a host written "/" (WHATWG URL, the path of a
// special URL is never empty). Chromium writes "https://gotenberg.dev/" for
// the source's "https://gotenberg.dev". A URI that does not parse is
// compared as written.
func normalizeLinkURI(s string) string {
	u, err := url.Parse(s)
	if err != nil {
		return s
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	if u.Host != "" && u.Path == "" && u.RawPath == "" {
		u.Path = "/"
	}
	return u.String()
}

// diffGolden is everything the goldens hold for one source.
type diffGolden struct {
	text   *pdfjsReference
	render *renderIndex
	links  *diffLinkGolden
	// sizes are the reference's page sizes in points, read with litho from
	// the provisioned Chromium PDF; nil skips the page size check.
	sizes []diffPageSize
	// outline is the reference's outline, flattened in outline order.
	outline []diffOutlineItem
	// missingFonts are the faces a Microsoft reference draws that no
	// fixture font stands in for (see [missingReferenceFaces]): the
	// fixture is then left unscored.
	missingFonts []string
}

// diffPageSize is a page's width and height in points, after /Rotate.
type diffPageSize struct {
	W, H float64
}

// matches reports whether two page sizes agree within diffPageSizeSlack.
func (s diffPageSize) matches(o diffPageSize) bool {
	return math.Abs(s.W-o.W) <= diffPageSizeSlack && math.Abs(s.H-o.H) <= diffPageSizeSlack
}

// diffOutlineItem is one outline item: its title and the page it leads to.
type diffOutlineItem struct {
	Title string `json:"title"`
	Page  int    `json:"page"`
}

// key is what two outline items must share to count as the same item: the
// title, NFKC-normalised with its white space collapsed, and the page.
func (o diffOutlineItem) key() string {
	return fmt.Sprintf("%d %s", o.Page, strings.Join(textTokens(o.Title), " "))
}

// flattenOutline lists a bookmark tree depth first, in outline order.
func flattenOutline(marks []models.Bookmark) []diffOutlineItem {
	var out []diffOutlineItem
	var walk func([]models.Bookmark)
	walk = func(bs []models.Bookmark) {
		for _, b := range bs {
			out = append(out, diffOutlineItem{Title: b.Title, Page: b.PageNumber})
			walk(b.Children)
		}
	}
	walk(marks)
	return out
}

// diffFeature scores litho's HTML converter against Chromium, the engine it
// replaces in Gotenberg. Each source of an html corpus is converted by
// litho at the options of its options.json (test/integration/fixture: by default
// print media, A4, Gotenberg's default margins) and compared with the PDF
// Gotenberg's Chromium route wrote from the same source at the same options,
// read at provisioning by PDF.js (text, links) and poppler (pages at 72 dpi),
// and at load by litho (page sizes, outline). It scores the Office
// converters against LibreOffice alike: each docx, odt, xlsx, ods, csv,
// pptx or odp corpus source is converted at the LibreOffice route's fields its
// options.json names and compared with the PDF that route wrote, a
// reference the host builds and [diffFeature.Prepare] checks, both sides
// drawn without annotations. Six scores, each in [0, 1], land in the
// baseline per fixture:
//
//   - pages: the smaller page count over the larger, times the share of the
//     pages both have whose width and height lie within diffPageSizeSlack of
//     the reference's: 1 when the counts and every size agree.
//   - text_f1: the token F1 of each page's text, litho's extraction of its
//     PDF against PDF.js's of Chromium's, NFKC-normalised and split at
//     whitespace, averaged over the pages either has.
//   - link_recall: the share of Chromium's links, by page and target, litho
//     writes too; 1 when Chromium writes none.
//   - layout: how much of the ink of the two pages lies in the same place,
//     blurred and matched band by band within a few pixels (see
//     [layoutScore]), averaged over the pages.
//   - strict: the share of pages the render cell's pixel rule matches.
//   - outline_recall: the share of the reference's outline items, by title
//     and target page, litho's outline holds; 1 when the reference has none.
//
// A missing page scores 0 on every per-page score. Two engines lay text out
// with their own line breaking and font metrics, so strict stays low even
// for a faithful conversion; layout is the number to read, strict the one
// that says a page is right. A conversion error scores 0 everywhere.
type diffFeature struct {
	convert htmlConverter

	mu        sync.RWMutex
	goldens   map[string]*diffGolden
	renderDir string
	fontDirs  []string
	reportDir string

	// m is the corpus: its references (see [Manifest.References]) and its
	// name, which the commands a stale reference asks for name; testdataRoot
	// is its testdata root. office reads every source as an Office document.
	m            *Manifest
	testdataRoot string
	office       bool
	// pins are the resolved pins the references are checked against; nil
	// reads versions.env.
	pins map[string]string
	// borrowed maps a fixture whose options.json names reference_of to the
	// fixture whose goldens it is scored against, and lithoOnly a fixture
	// that sets litho's own options without one to those options: it has no
	// golden (see [referencePlan]).
	borrowed  map[string]string
	lithoOnly map[string][]string
}

func newDiffFeature() *diffFeature {
	return &diffFeature{convert: convertHTML, goldens: make(map[string]*diffGolden)}
}

func (f *diffFeature) Name() string { return "diff" }

// GoldenSource names the engine the references come from, the one the
// corpus's manifest names: Chromium for the html corpora, LibreOffice or
// Microsoft for an Office corpus.
func (f *diffFeature) GoldenSource() string {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if f.m != nil && f.m.References != nil {
		return f.m.References.Engine
	}
	return engineChromium
}

// SetManifest records the corpus whose references the cell checks.
func (f *diffFeature) SetManifest(m *Manifest) {
	f.mu.Lock()
	f.m, f.testdataRoot, f.office = m, m.TestdataRoot(), m.References != nil && m.References.Engine != engineChromium
	f.mu.Unlock()
}

// ScoreTolerance is the drop each recorded score may take.
func (f *diffFeature) ScoreTolerance() float64 { return diffScoreTolerance }

// SetReportDir sets where pages that fail the strict rule are drawn.
func (f *diffFeature) SetReportDir(dir string) {
	f.mu.Lock()
	f.reportDir = dir
	f.mu.Unlock()
}

// LoadGoldens reads the text, render and link goldens under dir, keyed by
// reference name, the fixture ID with "/" written "__" ([reportName]); a
// source needs all three.
func (f *diffFeature) LoadGoldens(dir string) error {
	f.mu.RLock()
	office := f.office
	checkFonts := f.m != nil && f.m.References != nil && f.m.References.Engine == engineMicrosoft
	f.mu.RUnlock()
	texts := make(map[string]*pdfjsReference)
	var textMu sync.RWMutex
	if err := loadPDFJSReferences(filepath.Join(dir, "text"), texts, &textMu); err != nil {
		return err
	}
	renders, err := loadDiffJSON[renderIndex](filepath.Join(dir, "render"))
	if err != nil {
		return err
	}
	links, err := loadDiffJSON[diffLinkGolden](filepath.Join(dir, "links"))
	if err != nil {
		return err
	}
	// The provisioned Chromium PDFs sit at <testdata>/html/pdfs, two levels
	// above goldens/diff (the manifest's pdf_root).
	pdfsDir := filepath.Join(filepath.Dir(filepath.Dir(dir)), "pdfs")
	goldens := make(map[string]*diffGolden, len(texts))
	for id, t := range texts {
		r, l := renders[id], links[id]
		if r == nil || l == nil {
			continue
		}
		if r.DPI != diffDPI {
			return fmt.Errorf("diff render golden %s was drawn at %g dpi, the feature compares at %d: re-provision goldens/diff", id, r.DPI, diffDPI)
		}
		if r.HideAnnotations != office {
			return fmt.Errorf("diff render golden %s was drawn with hide_annotations %v, the corpus compares with %v: re-provision goldens/diff", id, r.HideAnnotations, office)
		}
		sizes, outline, err := readReferenceShape(context.Background(), filepath.Join(pdfsDir, id+".pdf"))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return fmt.Errorf("diff reference %s: %w", id, err)
		}
		g := &diffGolden{text: t, render: r, links: l, sizes: sizes, outline: outline}
		if checkFonts {
			faces, err := referenceFonts(context.Background(), filepath.Join(pdfsDir, id+".pdf"))
			if err != nil {
				fmt.Fprintf(os.Stderr, "diff: reading the fonts of reference %s: %v; scored as it is\n", id, err)
			} else {
				g.missingFonts = missingReferenceFaces(faces)
			}
		}
		goldens[id] = g
	}
	if len(goldens) == 0 {
		return ErrNoGoldens
	}
	fonts, err := fixtureFontDirs(fixtureFontsRoot(), diffFontsTarget)
	if err != nil {
		return err
	}
	f.mu.Lock()
	f.goldens, f.renderDir, f.fontDirs = goldens, filepath.Join(dir, "render"), fonts
	f.mu.Unlock()
	return nil
}

// readReferenceShape reads the page sizes and the outline of a reference
// PDF with litho.
func readReferenceShape(ctx context.Context, path string) ([]diffPageSize, []diffOutlineItem, error) {
	data, err := os.ReadFile(path) //nolint:gosec // provisioned reference under the corpus testdata root
	if err != nil {
		return nil, nil, err
	}
	r, err := pdf.Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, nil, fmt.Errorf("open: %w", err)
	}
	defer r.Close()
	sizes, err := pageSizes(ctx, r)
	if err != nil {
		return nil, nil, err
	}
	marks, err := r.Bookmarks(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("read the outline: %w", err)
	}
	return sizes, flattenOutline(marks), nil
}

// maxDiffPages bounds the pages the diff feature reads and scores of one
// conversion. Anti-DoS guard, not spec-mandated: a layout bug that loops
// pages would otherwise draw thousands. It is above maxConvertedPages
// because a spreadsheet's print ranges run longer than any web page: the
// longest reference, ods's sc/tdf134332.ods, prints 208 pages, and a
// reference over the cap could never score its page count.
const maxDiffPages = 256

// pageSizes lists the size of each page of r, up to maxDiffPages.
func pageSizes(ctx context.Context, r *pdf.Reader) ([]diffPageSize, error) {
	n, err := r.PageCount(ctx)
	if err != nil {
		return nil, fmt.Errorf("count pages: %w", err)
	}
	n = min(n, maxDiffPages)
	sizes := make([]diffPageSize, 0, n)
	for p := 1; p <= n; p++ {
		pg, err := r.Page(ctx, p)
		if err != nil {
			return nil, fmt.Errorf("read page %d: %w", p, err)
		}
		sizes = append(sizes, diffPageSize{W: pg.Width, H: pg.Height})
	}
	return sizes, nil
}

// loadDiffJSON decodes every JSON file of dir into a map keyed by file name
// without its extension. A missing directory is [ErrNoGoldens].
func loadDiffJSON[T any](dir string) (map[string]*T, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, ErrNoGoldens
	}
	out := make(map[string]*T, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name())) //nolint:gosec // golden under the corpus testdata root
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", e.Name(), err)
		}
		v := new(T)
		if err := json.Unmarshal(data, v); err != nil {
			return nil, fmt.Errorf("decode %s: %w", e.Name(), err)
		}
		out[strings.TrimSuffix(e.Name(), ".json")] = v
	}
	return out, nil
}

func (f *diffFeature) HasGolden(fixtureID string) bool { return f.golden(fixtureID) != nil }

func (f *diffFeature) golden(id string) *diffGolden {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if other, ok := f.borrowed[id]; ok {
		id = other
	}
	if _, ok := f.lithoOnly[id]; ok {
		// A golden left from a reference built before the source set
		// litho's own options is another document's.
		return nil
	}
	return f.goldens[reportName(id)]
}

// CountsSkippedMetrics counts the metrics of the fixtures the cell leaves
// unscored, reference_fonts_missing.
func (f *diffFeature) CountsSkippedMetrics() {}

// diffOutput is Run's output.
type diffOutput struct {
	Outcome     string             `json:"outcome"`
	Reason      string             `json:"reason,omitempty"`
	Pages       int                `json:"pages"`
	GoldenPages int                `json:"golden_pages"`
	Scores      map[string]float64 `json:"scores"`
	PageResults []diffPageResult   `json:"page_results,omitempty"`
	// ReferenceOf names the fixture whose reference the conversion was
	// scored against, when its options.json borrows one.
	ReferenceOf string `json:"reference_of,omitempty"`
	// MissingFonts are the faces the reference draws that no fixture font
	// stands in for: the fixture is left unscored.
	MissingFonts []string `json:"reference_fonts_missing,omitempty"`
}

// diffPageResult is the comparison of one page.
type diffPageResult struct {
	Page   int     `json:"page"`
	TextF1 float64 `json:"text_f1"`
	Layout float64 `json:"layout"`
	Strict bool    `json:"strict"`
	Detail string  `json:"detail,omitempty"`
}

// diffLithoPage is what litho's PDF holds on one page.
type diffLithoPage struct {
	text  string
	links []diffLink
	img   image.Image
	size  diffPageSize
}

// diffLithoDoc is what litho's PDF holds: its pages and its outline.
type diffLithoDoc struct {
	pages   []diffLithoPage
	outline []diffOutlineItem
}

// Run converts the source, reads litho's PDF back and scores it against the
// goldens. srcPath is the source directory, or a file in it.
func (f *diffFeature) Run(ctx context.Context, srcPath, fixtureID string) ([]byte, error) {
	g := f.golden(fixtureID)
	if g == nil {
		return json.Marshal(diffOutput{Outcome: outcomeError, Reason: "no golden", Scores: zeroDiffScores()})
	}
	f.mu.RLock()
	fonts, renderDir, reportDir, office := f.fontDirs, f.renderDir, f.reportDir, f.office
	f.mu.RUnlock()
	src, settings, err := sourceSettings(srcPath, office, fonts)
	var data []byte
	if err == nil {
		data, err = f.convert(ctx, src, settings)
	}
	var doc diffLithoDoc
	if err == nil {
		doc, err = readDiffPages(ctx, data, office)
	}
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return json.Marshal(diffOutput{Outcome: outcomeError, Reason: err.Error(), GoldenPages: g.text.NumPages, Scores: zeroDiffScores()})
	}
	f.mu.RLock()
	borrowed := f.borrowed[fixtureID]
	f.mu.RUnlock()
	out := scoreDiff(g, doc, renderDir, settings.Format == fixture.FormatCSV, func(page int, got, want image.Image, d pixelDiff) {
		if reportDir == "" {
			return
		}
		p := filepath.Join(reportDir, fmt.Sprintf("%s.p%d.png", reportName(fixtureID), page))
		if err := writeDiffReport(p, got, want, d, 1); err != nil {
			fmt.Fprintf(os.Stderr, "diff: report for %s page %d: %v\n", fixtureID, page, err)
		}
	})
	out.ReferenceOf, out.MissingFonts = borrowed, g.missingFonts
	return json.Marshal(out)
}

// zeroDiffScores is the score set of a fixture litho could not convert.
func zeroDiffScores() map[string]float64 {
	return map[string]float64{scorePages: 0, scoreText: 0, scoreLinks: 0, scoreLayout: 0, scoreStrict: 0, scoreOutline: 0}
}

// readDiffPages reads litho's PDF: each page's text, links, size and
// drawing at 72 dpi, without its annotations when hideAnnotations is set as
// the Office corpora's goldens are drawn, and the outline. A page with no
// text at all reads as empty, as PDF.js reads it.
func readDiffPages(ctx context.Context, data []byte, hideAnnotations bool) (diffLithoDoc, error) {
	r, err := pdf.Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return diffLithoDoc{}, fmt.Errorf("open the conversion: %w", err)
	}
	defer r.Close()
	sizes, err := pageSizes(ctx, r)
	if err != nil {
		return diffLithoDoc{}, err
	}
	marks, err := r.Bookmarks(ctx)
	if err != nil {
		return diffLithoDoc{}, fmt.Errorf("read the outline: %w", err)
	}
	pages := make([]diffLithoPage, 0, len(sizes))
	for i, size := range sizes {
		p := i + 1
		// The goldens hold every word of the page, headers and footers
		// included, which litho writes as pagination artifacts.
		text, err := r.ExtractText(ctx, p, pdf.WithArtifacts())
		if errors.Is(err, pdf.ErrRequiresOCR) {
			text, err = "", nil
		}
		if err != nil {
			return diffLithoDoc{}, fmt.Errorf("extract text of page %d: %w", p, err)
		}
		annots, err := r.ExtractAnnotations(ctx, p)
		if err != nil {
			return diffLithoDoc{}, fmt.Errorf("read annotations of page %d: %w", p, err)
		}
		var links []diffLink
		for _, a := range annots {
			if a.Subtype == "Link" {
				links = append(links, diffLink{Page: p, Rect: a.Rect[:], URI: a.URI, DestPage: a.DestPage})
			}
		}
		renderOpts := []pdf.RenderOption{pdf.WithRenderDPI(diffDPI)}
		if hideAnnotations {
			renderOpts = append(renderOpts, pdf.WithRenderContent(pdf.RenderAllContent&^pdf.RenderAnnotations))
		}
		img, err := r.RenderPage(ctx, p, renderOpts...)
		if err != nil {
			return diffLithoDoc{}, fmt.Errorf("render page %d: %w", p, err)
		}
		pages = append(pages, diffLithoPage{text: text, links: links, img: img, size: size})
	}
	return diffLithoDoc{pages: pages, outline: flattenOutline(marks)}, nil
}

// scoreDiff scores litho's pages against the goldens. outlineByPage
// matches the outline items by page alone, for a reference whose titles
// no converter can know. report is called for each page the strict rule
// fails, with litho's drawing, Chromium's and their comparison.
func scoreDiff(g *diffGolden, doc diffLithoDoc, renderDir string, outlineByPage bool, report func(page int, got, want image.Image, d pixelDiff)) diffOutput {
	pages := doc.pages
	out := diffOutput{Pages: len(pages), GoldenPages: g.text.NumPages}
	n := max(len(pages), g.text.NumPages)
	goldenText := make(map[int]string, len(g.text.Pages))
	for _, p := range g.text.Pages {
		goldenText[p.PageNumber] = p.Text
	}
	goldenPNG := make(map[int]string, len(g.render.Pages))
	for _, p := range g.render.Pages {
		goldenPNG[p.Page] = p.File
	}
	var textSum, layoutSum, strictSum float64
	sizesCompared, sizesMatched := 0, 0
	var sizeDetail string
	for p := 1; p <= min(n, maxDiffPages); p++ {
		res := diffPageResult{Page: p}
		if p <= len(pages) && p <= len(g.sizes) {
			sizesCompared++
			if got, want := pages[p-1].size, g.sizes[p-1]; got.matches(want) {
				sizesMatched++
			} else if sizeDetail == "" {
				sizeDetail = fmt.Sprintf(", page %d is %.2fx%.2f pt against %.2fx%.2f", p, got.W, got.H, want.W, want.H)
			}
		}
		if p <= len(pages) && p <= g.text.NumPages {
			lp := pages[p-1]
			res.TextF1 = tokenF1(goldenText[p], lp.text)
			if file := goldenPNG[p]; file != "" {
				want, err := readPNG(filepath.Join(renderDir, file))
				if err != nil {
					res.Detail = err.Error()
				} else {
					res.Layout = layoutScore(lp.img, want)
					d := comparePixels(lp.img, want, renderPixelRule, renderSizeSlack)
					res.Strict = d.within(renderPixelRule)
					if !res.Strict {
						res.Detail = d.String()
						report(p, lp.img, want, d)
					}
				}
			} else {
				res.Detail = "poppler drew no golden of this page"
			}
		} else {
			res.Detail = "page missing on one side"
		}
		textSum += res.TextF1
		layoutSum += res.Layout
		if res.Strict {
			strictSum++
		}
		out.PageResults = append(out.PageResults, res)
	}
	pagesScore := pageCountScore(len(pages), g.text.NumPages)
	if sizesCompared > 0 {
		pagesScore *= float64(sizesMatched) / float64(sizesCompared)
	}
	out.Scores = map[string]float64{
		scorePages:   pagesScore,
		scoreText:    meanOver(textSum, n),
		scoreLinks:   linkRecall(g.links.Links, pages),
		scoreLayout:  meanOver(layoutSum, n),
		scoreStrict:  meanOver(strictSum, n),
		scoreOutline: outlineRecall(g.outline, doc.outline, outlineByPage),
	}
	s := out.Scores
	if s[scorePages] == 1 && s[scoreText] >= diffTextBar && s[scoreLinks] == 1 && s[scoreLayout] >= diffLayoutBar && s[scoreOutline] == 1 {
		out.Outcome = outcomePass
	} else {
		out.Outcome = outcomeFail
		out.Reason = fmt.Sprintf("pages %d against %d%s, text F1 %.3f, link recall %.3f, layout %.3f, strict %.3f, outline recall %.3f",
			len(pages), g.text.NumPages, sizeDetail, s[scoreText], s[scoreLinks], s[scoreLayout], s[scoreStrict], s[scoreOutline])
	}
	return out
}

// outlineRecall is the share of the reference's outline items, matched by
// title and page as multisets, or by page alone when byPage is set, that
// litho's outline holds; 1 when the reference has none. An outline litho
// writes and Chromium does not is not penalised: the route writes none
// unless asked (nativeBookmarks), and the fixtures that ask score the
// items Chromium writes.
func outlineRecall(golden, got []diffOutlineItem, byPage bool) float64 {
	if len(golden) == 0 {
		return 1
	}
	key := diffOutlineItem.key
	if byPage {
		key = func(o diffOutlineItem) string { return strconv.Itoa(o.Page) }
	}
	have := make(map[string]int, len(got))
	for _, o := range got {
		have[key(o)]++
	}
	found := 0
	for _, o := range golden {
		if have[key(o)] > 0 {
			have[key(o)]--
			found++
		}
	}
	return float64(found) / float64(len(golden))
}

// meanOver divides sum by n, 0 for no page.
func meanOver(sum float64, n int) float64 {
	if n <= 0 {
		return 0
	}
	return sum / float64(n)
}

// pageCountScore is 1 for equal page counts, else the smaller over the
// larger.
func pageCountScore(got, want int) float64 {
	if got == want {
		return 1
	}
	if got <= 0 || want <= 0 {
		return 0
	}
	return float64(min(got, want)) / float64(max(got, want))
}

// textTokens splits a page's text into the tokens text F1 counts:
// NFKC-normalised, split at whitespace.
func textTokens(s string) []string {
	return strings.Fields(norm.NFKC.String(s))
}

// tokenF1 is the F1 score of the token multisets of two texts: the harmonic
// mean of the share of litho's tokens Chromium's text holds and the share of
// Chromium's litho's holds. Two empty texts score 1, one empty 0.
func tokenF1(golden, got string) float64 {
	want, have := textTokens(golden), textTokens(got)
	if len(want) == 0 && len(have) == 0 {
		return 1
	}
	if len(want) == 0 || len(have) == 0 {
		return 0
	}
	counts := make(map[string]int, len(want))
	for _, t := range want {
		counts[t]++
	}
	common := 0
	for _, t := range have {
		if counts[t] > 0 {
			counts[t]--
			common++
		}
	}
	if common == 0 {
		return 0
	}
	precision := float64(common) / float64(len(have))
	recall := float64(common) / float64(len(want))
	return 2 * precision * recall / (precision + recall)
}

// linkRecall is the share of the golden's links, matched by page and target
// as multisets, that litho's pages carry; 1 when the golden has none.
func linkRecall(golden []diffLink, pages []diffLithoPage) float64 {
	if len(golden) == 0 {
		return 1
	}
	have := make(map[string]int)
	for _, p := range pages {
		for _, l := range p.links {
			have[l.key()]++
		}
	}
	found := 0
	for _, l := range golden {
		if have[l.key()] > 0 {
			have[l.key()]--
			found++
		}
	}
	return float64(found) / float64(len(golden))
}

// layoutScore compares where two pages put their ink, tolerating a small
// displacement (see layoutBlur and its siblings). Each page's ink, the
// distance of each channel from white, is halved and blurred over the two
// pages' overlap; every band of each page is matched against the other page
// at the shift that fits it best; and the score is one minus the summed
// absolute difference, both ways, over the summed ink of both pages. A blank
// page scores 0 against any page that draws, two pages that put nothing
// where the other inks score 0, an identical page 1, and two blank pages 1.
// Two pages of different sizes compare their common top-left region; the
// pages score reads a page size defect.
//
// Cost: O(w*h*(2*layoutShiftY+1)*(2*layoutShiftX+1)) per page at worst, a
// few tens of milliseconds for an A4 page at 72 dpi.
func layoutScore(got, want image.Image) float64 {
	gb, wb := got.Bounds(), want.Bounds()
	w, h := min(gb.Dx(), wb.Dx()), min(gb.Dy(), wb.Dy())
	if w <= 0 || h <= 0 {
		return 0
	}
	g, hw, hh := layoutInk(flattenOnWhite(got, w, h), w, h)
	wt, _, _ := layoutInk(flattenOnWhite(want, w, h), w, h)
	var ink float64
	for i := range g {
		ink += g[i] + wt[i]
	}
	if ink == 0 {
		return 1
	}
	d := bandShiftDiff(g, wt, hw, hh) + bandShiftDiff(wt, g, hw, hh)
	return max(0, 1-d/ink)
}

// layoutInk turns an RGB image into three planes of ink, 255 minus each
// channel, halved by averaging 2 by 2 pixels and box-blurred by layoutBlur.
// The planes are laid out one after the other, each hw by hh.
func layoutInk(px []uint8, w, h int) ([]float64, int, int) {
	hw, hh := (w+1)/2, (h+1)/2
	n := hw * hh
	out := make([]float64, 3*n)
	counts := make([]float64, n)
	for y := range h {
		for x := range w {
			k := (y/2)*hw + x/2
			i := 3 * (y*w + x)
			for c := range 3 {
				out[c*n+k] += 255 - float64(px[i+c])
			}
			counts[k]++
		}
	}
	for c := range 3 {
		plane := out[c*n : (c+1)*n]
		for k := range plane {
			plane[k] /= counts[k]
		}
		boxBlur(plane, hw, hh, layoutBlur)
	}
	return out, hw, hh
}

// boxBlur blurs a w by h plane in place with a box of radius r, separably,
// averaging over the part of the box inside the plane.
func boxBlur(p []float64, w, h, r int) {
	tmp := make([]float64, len(p))
	for y := range h {
		for x := range w {
			var s float64
			lo, hi := max(0, x-r), min(w-1, x+r)
			for k := lo; k <= hi; k++ {
				s += p[y*w+k]
			}
			tmp[y*w+x] = s / float64(hi-lo+1)
		}
	}
	for y := range h {
		lo, hi := max(0, y-r), min(h-1, y+r)
		for x := range w {
			var s float64
			for k := lo; k <= hi; k++ {
				s += tmp[k*w+x]
			}
			p[y*w+x] = s / float64(hi-lo+1)
		}
	}
}

// bandShiftDiff sums, over the layoutBand-row bands of a's three planes, the
// absolute difference with b at the shift within layoutShiftY rows and
// layoutShiftX columns that makes it smallest. Outside its plane, b reads as
// no ink.
func bandShiftDiff(a, b []float64, w, h int) float64 {
	n := w * h
	var total float64
	for y0 := 0; y0 < h; y0 += layoutBand {
		y1 := min(h, y0+layoutBand)
		best := math.Inf(1)
		for dy := -layoutShiftY; dy <= layoutShiftY; dy++ {
			for dx := -layoutShiftX; dx <= layoutShiftX; dx++ {
				var d float64
				for y := y0; y < y1 && d < best; y++ {
					by := y + dy
					for x := range w {
						bx := x + dx
						inside := by >= 0 && by < h && bx >= 0 && bx < w
						for c := range 3 {
							v := 0.0
							if inside {
								v = b[c*n+by*w+bx]
							}
							d += math.Abs(a[c*n+y*w+x] - v)
						}
					}
				}
				best = min(best, d)
			}
		}
		total += best
	}
	return total
}

// Compare turns Run's scores into a verdict: matched when every bar is
// cleared, scored by the mean of pages, text, links and layout.
func (f *diffFeature) Compare(fixtureID string, output []byte) Verdict {
	var got diffOutput
	if err := json.Unmarshal(output, &got); err != nil {
		return Verdict{
			FixtureID: fixtureID, Outcome: outcomeError, Reason: fmt.Sprintf("decode output: %v", err),
			Scores: zeroDiffScores(), Metrics: map[string]int{metricConvertError: 1, metricPagesEqual: 0, metricReferenceFontsMissing: 0},
		}
	}
	if got.Scores == nil {
		got.Scores = zeroDiffScores()
	}
	if got.ReferenceOf != "" && got.Reason != "" {
		got.Reason = fmt.Sprintf("against the reference of %s: %s", got.ReferenceOf, got.Reason)
	}
	if len(got.MissingFonts) > 0 {
		// Unscored, outside matched and mismatched, the scores kept for a
		// reader: no converter can match the metrics of a face it has no
		// stand-in for.
		reason := "unscored: the reference draws " + strings.Join(got.MissingFonts, ", ") + ", which no fixture font stands in for"
		if got.Reason != "" {
			reason += "; " + got.Reason
		}
		return Verdict{
			FixtureID: fixtureID, Skipped: true, Outcome: got.Outcome, Reason: reason, Scores: got.Scores,
			Metrics: map[string]int{metricReferenceFontsMissing: 1},
		}
	}
	v := Verdict{
		FixtureID: fixtureID,
		Outcome:   got.Outcome,
		Reason:    got.Reason,
		Scores:    got.Scores,
		Metrics:   map[string]int{metricConvertError: 0, metricPagesEqual: 0, metricReferenceFontsMissing: 0},
	}
	s := got.Scores
	v.Score = (s[scorePages] + s[scoreText] + s[scoreLinks] + s[scoreLayout]) / 4
	switch got.Outcome {
	case outcomePass:
		v.Match = true
	case outcomeFail:
	default:
		v.Outcome = outcomeError
		v.Metrics[metricConvertError] = 1
	}
	if got.Outcome != outcomeError && s[scorePages] == 1 {
		v.Metrics[metricPagesEqual] = 1
	}
	if data, err := json.Marshal(got.PageResults); err == nil && !v.Match {
		v.Mismatches = []json.RawMessage{data}
	}
	return v
}
