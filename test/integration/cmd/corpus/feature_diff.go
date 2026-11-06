package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/text/unicode/norm"

	"github.com/gotenberg/litho/pkg/pdf"
)

// The page the html corpus is converted on, in points: A4 (210 by 297 mm)
// with the 0.39 inch margins Gotenberg's Chromium route applies by default.
// corpus/html/chromium.sh asks Chromium for the same, and
// corpus/html/build/main.go converts with the same; keep the three in step.
const (
	diffPaperWidth  = 595.276
	diffPaperHeight = 841.890
	diffMargin      = 0.39 * 72
)

// diffDPI is the resolution the diff goldens are drawn at, mirroring
// gen-render.sh: one pixel per point.
const diffDPI = renderDPI

// diffDownsample is the side, in pixels, of the blocks the layout score
// averages a page into: 8 pixels at 72 dpi are 8 points, about one line of
// small text, so text set in another face or wrapped a word apart averages
// out, while a block, an image or a page break out of place does not.
const diffDownsample = 8

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
//   - html: each Chromium reference converted back through the feature (litho
//     reads and draws the PDF the goldens come from) scores text F1 1 on 11
//     sources and 0.89 on i18n, whose Arabic, Hebrew and Thai litho's text
//     extraction and PDF.js tokenise apart; layout 0.85 (i18n, lists) to 0.99
//     (backgrounds, images), the anti-aliasing of two rasterizers weighed
//     against little ink; strict 1 on every page.
//   - html: the lists reference scored against the 11 other sources reaches
//     text F1 0.14 and layout 0.14 at most, and a blank page scores 0 on both.
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
)

// metricPagesEqual counts the fixtures whose page count is Chromium's.
const metricPagesEqual = "pages_equal"

// diffFontsTarget is the make target that fetches the fonts the diff feature
// converts with.
const diffFontsTarget = "make html-fixtures"

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
}

// diffFeature scores litho's HTML converter against Chromium, the engine it
// replaces in Gotenberg. Each source of the html corpus is converted by
// litho (print media, A4, Gotenberg's default margins) and compared with the
// PDF Gotenberg's Chromium route wrote from the same source, read at
// provisioning by PDF.js (text, links) and poppler (pages at 72 dpi). Five
// scores, each in [0, 1], land in the baseline per fixture:
//
//   - pages: 1 when the page counts agree, else the smaller over the larger.
//   - text_f1: the token F1 of each page's text, litho's extraction of its
//     PDF against PDF.js's of Chromium's, NFKC-normalised and split at
//     whitespace, averaged over the pages either has.
//   - link_recall: the share of Chromium's links, by page and target, litho
//     writes too; 1 when Chromium writes none.
//   - layout: how much of the ink of the two pages, averaged into 8 by 8
//     pixel blocks, lies in the same place (see [layoutScore]), averaged
//     over the pages.
//   - strict: the share of pages the render cell's pixel rule matches.
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
}

func newDiffFeature() *diffFeature {
	return &diffFeature{convert: convertHTML, goldens: make(map[string]*diffGolden)}
}

func (f *diffFeature) Name() string         { return "diff" }
func (f *diffFeature) GoldenSource() string { return "chromium" }

// ScoreTolerance is the drop each recorded score may take.
func (f *diffFeature) ScoreTolerance() float64 { return diffScoreTolerance }

// SetReportDir sets where pages that fail the strict rule are drawn.
func (f *diffFeature) SetReportDir(dir string) {
	f.mu.Lock()
	f.reportDir = dir
	f.mu.Unlock()
}

// LoadGoldens reads the text, render and link goldens under dir, keyed by
// source name; a source needs all three.
func (f *diffFeature) LoadGoldens(dir string) error {
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
	goldens := make(map[string]*diffGolden, len(texts))
	for id, t := range texts {
		r, l := renders[id], links[id]
		if r == nil || l == nil {
			continue
		}
		if r.DPI != diffDPI {
			return fmt.Errorf("diff render golden %s was drawn at %g dpi, the feature compares at %d: re-provision goldens/diff", id, r.DPI, diffDPI)
		}
		goldens[id] = &diffGolden{text: t, render: r, links: l}
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
	return f.goldens[id]
}

// diffOutput is Run's output.
type diffOutput struct {
	Outcome     string             `json:"outcome"`
	Reason      string             `json:"reason,omitempty"`
	Pages       int                `json:"pages"`
	GoldenPages int                `json:"golden_pages"`
	Scores      map[string]float64 `json:"scores"`
	PageResults []diffPageResult   `json:"page_results,omitempty"`
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
}

// Run converts the source, reads litho's PDF back and scores it against the
// goldens.
func (f *diffFeature) Run(ctx context.Context, srcPath, fixtureID string) ([]byte, error) {
	g := f.golden(fixtureID)
	if g == nil {
		return json.Marshal(diffOutput{Outcome: outcomeError, Reason: "no golden", Scores: zeroDiffScores()})
	}
	f.mu.RLock()
	fonts, renderDir, reportDir := f.fontDirs, f.renderDir, f.reportDir
	f.mu.RUnlock()
	src, err := os.ReadFile(srcPath) //nolint:gosec // committed corpus source
	if err != nil {
		return json.Marshal(diffOutput{Outcome: outcomeError, Reason: "read: " + err.Error(), Scores: zeroDiffScores()})
	}
	data, err := f.convert(ctx, src, htmlSettings{
		Media:     "print",
		Width:     diffPaperWidth,
		Height:    diffPaperHeight,
		Top:       diffMargin,
		Right:     diffMargin,
		Bottom:    diffMargin,
		Left:      diffMargin,
		Resources: os.DirFS(filepath.Dir(srcPath)),
		FontDirs:  fonts,
	})
	var pages []diffLithoPage
	if err == nil {
		pages, err = readDiffPages(ctx, data)
	}
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return json.Marshal(diffOutput{Outcome: outcomeError, Reason: err.Error(), GoldenPages: g.text.NumPages, Scores: zeroDiffScores()})
	}
	out := scoreDiff(g, pages, renderDir, func(page int, got, want image.Image, d pixelDiff) {
		if reportDir == "" {
			return
		}
		p := filepath.Join(reportDir, fmt.Sprintf("%s.p%d.png", reportName(fixtureID), page))
		if err := writeDiffReport(p, got, want, d, 1); err != nil {
			fmt.Fprintf(os.Stderr, "diff: report for %s page %d: %v\n", fixtureID, page, err)
		}
	})
	return json.Marshal(out)
}

// zeroDiffScores is the score set of a fixture litho could not convert.
func zeroDiffScores() map[string]float64 {
	return map[string]float64{scorePages: 0, scoreText: 0, scoreLinks: 0, scoreLayout: 0, scoreStrict: 0}
}

// readDiffPages reads each page of litho's PDF: its text, its links and its
// drawing at 72 dpi.
func readDiffPages(ctx context.Context, data []byte) ([]diffLithoPage, error) {
	r, err := pdf.Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("open the conversion: %w", err)
	}
	defer r.Close()
	n, err := r.PageCount(ctx)
	if err != nil {
		return nil, fmt.Errorf("count pages: %w", err)
	}
	n = min(n, maxConvertedPages)
	pages := make([]diffLithoPage, 0, n)
	for p := 1; p <= n; p++ {
		text, err := r.ExtractText(ctx, p)
		if err != nil {
			return nil, fmt.Errorf("extract text of page %d: %w", p, err)
		}
		annots, err := r.ExtractAnnotations(ctx, p)
		if err != nil {
			return nil, fmt.Errorf("read annotations of page %d: %w", p, err)
		}
		var links []diffLink
		for _, a := range annots {
			if a.Subtype == "Link" {
				links = append(links, diffLink{Page: p, Rect: a.Rect[:], URI: a.URI, DestPage: a.DestPage})
			}
		}
		img, err := r.RenderPage(ctx, p, pdf.WithRenderDPI(diffDPI))
		if err != nil {
			return nil, fmt.Errorf("render page %d: %w", p, err)
		}
		pages = append(pages, diffLithoPage{text: text, links: links, img: img})
	}
	return pages, nil
}

// scoreDiff scores litho's pages against the goldens. report is called for
// each page the strict rule fails, with litho's drawing, Chromium's and
// their comparison.
func scoreDiff(g *diffGolden, pages []diffLithoPage, renderDir string, report func(page int, got, want image.Image, d pixelDiff)) diffOutput {
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
	for p := 1; p <= min(n, maxConvertedPages); p++ {
		res := diffPageResult{Page: p}
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
	out.Scores = map[string]float64{
		scorePages:  pageCountScore(len(pages), g.text.NumPages),
		scoreText:   meanOver(textSum, n),
		scoreLinks:  linkRecall(g.links.Links, pages),
		scoreLayout: meanOver(layoutSum, n),
		scoreStrict: meanOver(strictSum, n),
	}
	s := out.Scores
	if s[scorePages] == 1 && s[scoreText] >= diffTextBar && s[scoreLinks] == 1 && s[scoreLayout] >= diffLayoutBar {
		out.Outcome = outcomePass
	} else {
		out.Outcome = outcomeFail
		out.Reason = fmt.Sprintf("pages %d against %d, text F1 %.3f, link recall %.3f, layout %.3f, strict %.3f",
			len(pages), g.text.NumPages, s[scoreText], s[scoreLinks], s[scoreLayout], s[scoreStrict])
	}
	return out
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

// layoutScore compares two pages averaged into diffDownsample-pixel blocks
// over their overlap: one minus the summed absolute channel difference of the
// blocks over the summed ink, a block channel's ink being its distance from
// white on whichever page draws it darker. Weighing by ink rather than by
// the page keeps a mostly white page from scoring near 1 whatever it draws:
// a blank page scores 0 against any page that draws, an identical one 1.
// Two blank pages score 1. Two pages of different sizes compare their common
// top-left region; the pages score reads a page size defect.
func layoutScore(got, want image.Image) float64 {
	gb, wb := got.Bounds(), want.Bounds()
	w, h := min(gb.Dx(), wb.Dx()), min(gb.Dy(), wb.Dy())
	if w <= 0 || h <= 0 {
		return 0
	}
	g, wt := blockMeans(flattenOnWhite(got, w, h), w, h), blockMeans(flattenOnWhite(want, w, h), w, h)
	var diff, ink float64
	for i := range g {
		diff += math.Abs(g[i] - wt[i])
		ink += 255 - min(g[i], wt[i])
	}
	if ink == 0 {
		return 1
	}
	return max(0, 1-diff/ink)
}

// blockMeans averages an RGB image into diffDownsample-pixel blocks, the
// last row and column of blocks covering what remains, and returns each
// block's mean red, green and blue.
func blockMeans(px []uint8, w, h int) []float64 {
	bw, bh := (w+diffDownsample-1)/diffDownsample, (h+diffDownsample-1)/diffDownsample
	sums := make([]float64, 3*bw*bh)
	counts := make([]float64, bw*bh)
	for y := range h {
		for x := range w {
			b := (y/diffDownsample)*bw + x/diffDownsample
			i := 3 * (y*w + x)
			sums[3*b] += float64(px[i])
			sums[3*b+1] += float64(px[i+1])
			sums[3*b+2] += float64(px[i+2])
			counts[b]++
		}
	}
	for b, c := range counts {
		sums[3*b] /= c
		sums[3*b+1] /= c
		sums[3*b+2] /= c
	}
	return sums
}

// Compare turns Run's scores into a verdict: matched when every bar is
// cleared, scored by the mean of pages, text, links and layout.
func (f *diffFeature) Compare(fixtureID string, output []byte) Verdict {
	var got diffOutput
	if err := json.Unmarshal(output, &got); err != nil {
		return Verdict{
			FixtureID: fixtureID, Outcome: outcomeError, Reason: fmt.Sprintf("decode output: %v", err),
			Scores: zeroDiffScores(), Metrics: map[string]int{metricConvertError: 1, metricPagesEqual: 0},
		}
	}
	if got.Scores == nil {
		got.Scores = zeroDiffScores()
	}
	v := Verdict{
		FixtureID: fixtureID,
		Outcome:   got.Outcome,
		Reason:    got.Reason,
		Scores:    got.Scores,
		Metrics:   map[string]int{metricConvertError: 0, metricPagesEqual: 0},
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
