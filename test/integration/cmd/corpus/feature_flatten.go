package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/gotenberg/litho/pkg/models"
	"github.com/gotenberg/litho/pkg/pdf"
)

// Metrics of the flatten feature.
const (
	// metricFlattenChecked counts the pages the rendering checks ran on.
	metricFlattenChecked = "pages_checked"
	// metricFlattenUnchanged counts the PDFs with no transparency to
	// flatten, skipped: an inventory.
	metricFlattenUnchanged = "flatten_unchanged"
	// metricFlattenRefused counts the PDFs [pdf.Writer.FlattenTransparency]
	// refused with [pdf.ErrTransparencyFlatteningRefused], its documented
	// answer for a page past its budgets, which are no failures.
	metricFlattenRefused = "flatten_refused"
	// metricFlattenFailed counts the PDFs whose flattening or write failed
	// with any other error.
	metricFlattenFailed = "flatten_failed"
	// metricFlattenLithoMismatch and metricFlattenPopplerMismatch count the
	// pages whose output litho, or poppler, draws apart from the source at
	// the render cell's bars.
	metricFlattenLithoMismatch   = "litho_mismatch"
	metricFlattenPopplerMismatch = "poppler_mismatch"
	// metricFlattenTextChanged counts the PDFs whose extracted text, white
	// space aside, differs from the source's on some page.
	metricFlattenTextChanged = "text_changed"
	// metricFlattenTransparencyLeft counts the PDFs whose output still
	// paints with transparency: a second flattening changes a page, or
	// PDF/A-1b validation finds ISO 19005-1, 6.4 transparency while no
	// annotation was left transparent.
	metricFlattenTransparencyLeft = "transparency_left"
	// metricFlattenAnnotations counts the PDFs whose annotations are left
	// painting with transparency, which flattening does not draw into the
	// page (see [pdf.FlattenReport.TransparentAnnotations]): an inventory.
	metricFlattenAnnotations = "annotations_left"
	// metricFlattenSkippedPages counts the pages whose content does not
	// read, left as they are: an inventory.
	metricFlattenSkippedPages = "pages_skipped"
)

// flattenFeature flattens each PDF's transparency with
// [pdf.Writer.FlattenTransparency] at its default 300 dpi and checks the
// output against what flattening promises:
//
//   - it reopens in litho (reopen_failed);
//   - each of its first [renderMaxPages] pages draws, in litho and in the
//     pinned poppler, as the source page draws in the same renderer, at
//     [renderDPI] and the render cell's bars (litho_mismatch,
//     poppler_mismatch);
//   - the text of every page extracts as the source's, white space aside
//     (text_changed): text is never rasterised;
//   - no transparency is left (transparency_left): flattening the output
//     again changes no page, and PDF/A-1b validation finds no transparency
//     (ISO 19005-1, 6.4) when no annotation was left transparent.
//
// A PDF with nothing to flatten is skipped (flatten_unchanged), a refusal
// counts in flatten_refused and skips it, and any other error counts in
// flatten_failed. The PDFs are those the render goldens cover, or every PDF
// of the corpus when its scope file says so (see [flattenScopeFile]): the
// cell runs poppler itself, so it needs no poppler golden, only the fonts of
// the render goldens, which serve litho's renderings so an unembedded font
// draws alike.
//
// # Corpus evidence
//
//   - litho (254 files, flattentext_2b, flattenannots_2b, blendspace_2b,
//     flattentiles_1b, smaskindata_2b, flatuntagged_ua1, iccgroupalt_2b,
//     greyintent_2b and sharedimage_2b among them): 36 flattened, all
//     match; flattenrefuse_1b and smaskbudget_2b are refused, a group on
//     their second page past the render pixel budget; 216 have no
//     transparency. Before annotations were drawn into the page, 20
//     flattened and 5 left annotations painting with transparency; 1 does
//     now, laterreads_2u.pdf, whose Screen annotation has no appearance
//     stream.
//   - govdocs, all 1091 files (scope file): 34 flatten, all match, and
//     1057 have no transparency.
//   - pdfjs, all 1202 files but the 1 the manifest skips: 267 of 276
//     flattened match, 915 have no transparency, 4 are refused
//     (issue14982.pdf past the render pixel budget; REDHAT-1531897-0.pdf,
//     bug1020226.pdf and issue15590.pdf, whose page tree does not resolve)
//     and 7 do not open without a password. issue18765.pdf, issue19281.pdf
//     and issue19319.pdf, once left out for cost, flatten and match.
//   - pdfjs: 8 of the 9 mismatches are what flattening approximates by
//     design. A watermark a transparent black box tinted is now drawn above
//     it untinted (issue18466.pdf: litho_mismatch, poppler_mismatch). A
//     watermark composited over the paper is drawn over a drawing
//     (issue12810.pdf: poppler_mismatch). Hairlines and pixel-snapped edges
//     the 300 dpi composite draws thinner or softer at 72 dpi
//     (issue17779.pdf: poppler_mismatch; issue18529.pdf, issue20914.pdf:
//     litho_mismatch, poppler_mismatch). Type 3 glyphs that paint with
//     transparency stay text (issue16127.pdf, issue19971.pdf:
//     transparency_left). issue18973.pdf changes its text on page 45, the
//     page whose content does not read, which the rewrite writing the
//     output gives its signed signature widget's appearance, as every
//     rewrite does (text_changed).
//   - pdfjs issue7821.pdf: its Stamp fills a pattern in a transparency group
//     that litho's renderer draws apart from poppler 26.09.0 and PDFium on
//     the source page too (1,046 pixels apart from PDFium at 72 dpi); the
//     composite carries litho's drawing (poppler_mismatch).
//   - pdfjs issue16263.pdf: its 2x2 black image's 151-megapixel /SMask,
//     past the image pixel budget, is read averaged down to the page's
//     resolution, so litho draws the masked arrows poppler and PDFium draw,
//     and the page matches; before, litho painted the image opaque.
//   - pdfjs: the text the covering images of issue16800.pdf and the black
//     box of issue18466.pdf hide stays hidden, and in the content.
//   - pdfjs: drawing transparent annotations into the page took the files
//     that leave annotations painting with transparency from 62 to 10 over
//     the whole corpus, 4 of them in scope (annotations_left).
type flattenFeature struct {
	workers int
	// flatten runs the flattening; tests replace it to plant a defect.
	flatten func(ctx context.Context, w *pdf.Writer) (*pdf.FlattenReport, error)
	// poppler renders the batch, [renderWithPoppler] outside tests, and pin
	// returns the version it must report, "" to accept any.
	poppler func(ctx context.Context, dir string, jobs []popplerJob) (string, error)
	pin     func() string

	mu      sync.RWMutex
	sampled map[string]bool
	// excluded holds the PDFs the corpus's scope file leaves out, with
	// why (see [flattenScopeFile]).
	excluded map[string]string
	fonts    pdf.RenderOption
	outputs  map[string][]byte
}

func newFlattenFeature() *flattenFeature {
	return &flattenFeature{
		workers: runtime.NumCPU(),
		flatten: func(ctx context.Context, w *pdf.Writer) (*pdf.FlattenReport, error) {
			return w.FlattenTransparency(ctx)
		},
		poppler: renderWithPoppler,
		pin:     popplerPin,
		outputs: make(map[string][]byte),
	}
}

func (f *flattenFeature) Name() string         { return "flatten" }
func (f *flattenFeature) GoldenSource() string { return "litho+poppler" }

// OncePerPDF marks the feature as profile-independent.
func (f *flattenFeature) OncePerPDF() {}

// LoadGoldens reads which PDFs the render goldens cover and their fonts (see
// [loadRenderSample]).
func (f *flattenFeature) LoadGoldens(dir string) error {
	sampled, fonts, err := loadRenderSample(dir)
	if err != nil {
		return err
	}
	f.mu.Lock()
	f.sampled, f.fonts, f.excluded = sampled, fonts, nil
	f.mu.Unlock()
	return nil
}

func (f *flattenFeature) HasGolden(fixtureID string) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	id := baseFixtureID(fixtureID)
	if _, ok := f.excluded[id]; ok {
		return false
	}
	return f.sampled == nil || f.sampled[id]
}

// flattenScopeFile is the layout of a committed flatten scope file (see
// [Manifest.GoldenOverrides]). It replaces the render sample, which the
// cell reads its scope from, for a corpus whose render goldens cover one PDF
// in sixteen: the cell needs no poppler golden, since it runs poppler on the
// source and the output itself, so its scope can be the whole corpus. A PDF
// with nothing to flatten costs one analysis and skips.
//
// A PDF on which one run of the cell, measured on its own, takes more CPU
// time than a quarter of [prepareFixtureTimeout] is excluded. The quarter
// keeps a fourfold margin under the timeout the cell runs each PDF under,
// which a heavier PDF overruns once the run's other workers load the
// machine, a verdict that then depends on the load rather than on litho.
//
// # Corpus evidence
//
//   - pdfjs: no PDF passes the bar. issue19281.pdf takes 38 s of CPU,
//     issue19319.pdf 38 s, issue16127.pdf 17 s and issue18765.pdf 4 s, where
//     they took 46 s, 71 s, 29 s and 62 s, measured back to back on a load
//     of 4, before the renderer rasterised a clipping path over the pixels
//     painted under it, and before the flattening drew the page whole only
//     at the glyph pixels it reads. issue18765.pdf, issue19281.pdf and
//     issue19319.pdf were excluded until then, the first and the last
//     timing out under a load of 50.
//   - govdocs: the heaviest, thread_200_200744.pdf, takes 16 s.
type flattenScopeFile struct {
	// WholeCorpus puts every PDF of the corpus in scope, not only those
	// the render goldens cover.
	WholeCorpus bool `json:"whole_corpus"`
	// Exclude maps a PDF, by file name without its extension, to why the
	// cell leaves it out.
	Exclude map[string]string `json:"exclude,omitempty"`
}

// errFlattenScopeInvalid flags a scope file that breaks its contract (see
// [flattenScopeFile]).
var errFlattenScopeInvalid = errors.New("invalid flatten scope")

// readFlattenScope parses and validates a flatten scope file.
func readFlattenScope(path string) (flattenScopeFile, error) {
	var scope flattenScopeFile
	data, err := os.ReadFile(path) //nolint:gosec // path comes from the committed corpus manifest
	if err != nil {
		return scope, fmt.Errorf("read %s: %w", path, err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&scope); err != nil {
		return scope, fmt.Errorf("parse %s: %w", path, err)
	}
	for id, reason := range scope.Exclude {
		if id == "" || strings.TrimSpace(reason) == "" {
			return scope, fmt.Errorf("%w: exclusion %q gives no reason", errFlattenScopeInvalid, id)
		}
	}
	return scope, nil
}

// LoadOverrides reads the corpus's flatten scope file (see
// [flattenScopeFile]).
func (f *flattenFeature) LoadOverrides(path string) error {
	scope, err := readFlattenScope(path)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if scope.WholeCorpus {
		f.sampled = nil
	}
	f.excluded = scope.Exclude
	return nil
}

// flattenOutput is the feature's per-PDF output.
type flattenOutput struct {
	// Error says why the PDF was not checked: it does not open, the
	// flattening refused it (Refused) or failed (Failed), or it had nothing
	// to flatten (Unchanged).
	Error     string             `json:"error,omitempty"`
	Refused   bool               `json:"refused,omitempty"`
	Failed    bool               `json:"failed,omitempty"`
	Unchanged bool               `json:"unchanged,omitempty"`
	Report    *pdf.FlattenReport `json:"report,omitempty"`
	// ReopenFailed reports an output litho cannot open.
	ReopenFailed bool `json:"reopen_failed,omitempty"`
	// TextChanged lists the pages whose text changed.
	TextChanged []int `json:"text_changed,omitempty"`
	// Reflattened lists the pages a second flattening changes, and
	// Transparency64 the ISO 19005-1, 6.4 violations PDF/A-1b validation
	// finds in the output.
	Reflattened    []int         `json:"reflattened,omitempty"`
	Transparency64 int           `json:"transparency_6_4,omitempty"`
	Pages          []flattenPage `json:"pages,omitempty"`
}

// flattenPage is one page's rendering checks.
type flattenPage struct {
	Page    int         `json:"page"`
	Litho   *pageResult `json:"litho,omitempty"`
	Poppler *pageResult `json:"poppler,omitempty"`
	// PopplerSkipped says why poppler's half did not run: poppler is
	// unavailable, or it is no oracle for the page (see [popplerPair]).
	PopplerSkipped string `json:"poppler_skipped,omitempty"`
}

// transparencyLeft reports whether the output still paints with
// transparency. Annotations left transparent excuse both checks: ISO
// 19005-1, 6.4 reads their appearances, and the rewrite that writes the
// output bakes a signed signature widget's appearance into the page content,
// as every rewrite does (see [pdf.Writer.DroppedSignatures]), after the
// flattening ran.
func (o flattenOutput) transparencyLeft() bool {
	if o.Report != nil && o.Report.TransparentAnnotations > 0 {
		return false
	}
	return len(o.Reflattened) > 0 || o.Transparency64 > 0
}

// flattenStaged is one PDF's progress through Prepare.
type flattenStaged struct {
	id   string
	out  flattenOutput
	jobs []popplerJob
	// in and outPDF are the staged source and output, relative to the
	// staging directory.
	in, outPDF string
	// sources holds litho's rendering of each checked source page, which
	// poppler's must match for poppler to be an oracle for the page.
	sources map[int]image.Image
}

// Prepare flattens and checks every PDF in scope, renders the pages poppler
// draws in one batch, and scores them. Run looks the result up.
func (f *flattenFeature) Prepare(ctx context.Context, fixtures []Fixture) error {
	start := time.Now()
	staging, err := os.MkdirTemp("", "litho-flatten-")
	if err != nil {
		return fmt.Errorf("staging dir: %w", err)
	}
	defer os.RemoveAll(staging)

	var todo []Fixture
	for _, fx := range fixtures {
		if fx.SkipReason == "" && f.HasGolden(fx.ID) {
			todo = append(todo, fx)
		}
	}
	results := make([]*flattenStaged, len(todo))
	f.pool(len(todo), func(i int) {
		fctx, cancel := context.WithTimeout(ctx, prepareFixtureTimeout)
		defer cancel()
		results[i] = f.prepareOne(fctx, staging, i, todo[i])
	})
	if err := ctx.Err(); err != nil {
		return err
	}

	var jobs []popplerJob
	for _, res := range results {
		jobs = append(jobs, res.jobs...)
	}
	unavailable := ""
	if len(jobs) > 0 {
		version, err := f.poppler(ctx, staging, jobs)
		switch {
		case err != nil:
			unavailable = err.Error()
		case f.pin() != "" && version != f.pin():
			unavailable = fmt.Sprintf("%v: the image runs poppler %s, versions.env pins %s", errPopplerUnavailable, version, f.pin())
		}
		if unavailable != "" {
			fmt.Fprintf(os.Stderr, "flatten: %s\n", unavailable)
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	f.pool(len(results), func(i int) {
		res := results[i]
		for k := range res.out.Pages {
			p := &res.out.Pages[k]
			if unavailable != "" {
				p.PopplerSkipped = unavailable
				continue
			}
			p.Poppler, p.PopplerSkipped = popplerPair(staging, res.in, res.outPDF, p.Page, res.sources[p.Page])
		}
	})

	f.mu.Lock()
	for _, res := range results {
		data, err := json.Marshal(res.out)
		if err != nil {
			data = []byte(`{"error":"marshal"}`)
		}
		f.outputs[res.id] = data
	}
	f.mu.Unlock()
	fmt.Fprintf(os.Stderr, "flatten: %d PDFs checked in %s (poppler %d pages)\n",
		len(todo), time.Since(start).Round(time.Millisecond), len(jobs))
	return nil
}

// pool runs fn for 0..n-1 on at most f.workers goroutines.
func (f *flattenFeature) pool(n int, fn func(i int)) {
	work := make(chan int)
	var wg sync.WaitGroup
	for range max(1, f.workers) {
		wg.Go(func() {
			for i := range work {
				fn(i)
			}
		})
	}
	for i := range n {
		work <- i
	}
	close(work)
	wg.Wait()
}

// prepareOne flattens one PDF, runs litho's checks, and stages the source
// and the output for poppler.
func (f *flattenFeature) prepareOne(ctx context.Context, staging string, n int, fx Fixture) (res *flattenStaged) {
	res = &flattenStaged{id: fx.ID}
	// A panic past the library's own recover is a failed flattening, not a
	// crashed run.
	defer func() {
		if r := recover(); r != nil {
			res.out = flattenOutput{Error: fmt.Sprintf("panic: %v", r), Failed: true}
			res.jobs = nil
		}
	}()
	input, err := os.ReadFile(fx.PDFPath)
	if err != nil {
		res.out.Error = fmt.Sprintf("read: %v", err)
		return res
	}
	src, err := pdf.Open(ctx, bytes.NewReader(input), int64(len(input)))
	if err != nil {
		res.out.Error = fmt.Sprintf("open: %v", err)
		return res
	}
	defer src.Close()

	output, report, err := f.flattenOnce(ctx, src)
	if err != nil {
		res.out.Error = err.Error()
		if errors.Is(err, pdf.ErrTransparencyFlatteningRefused) {
			res.out.Refused = true
		} else {
			res.out.Failed = true
		}
		return res
	}
	res.out.Report = report
	if output == nil {
		res.out.Unchanged = true
		return res
	}
	after, err := pdf.Open(ctx, bytes.NewReader(output), int64(len(output)))
	if err != nil {
		res.out.ReopenFailed = true
		res.out.Error = fmt.Sprintf("output does not open: %v", err)
		return res
	}
	defer after.Close()

	pages, err := after.PageCount(ctx)
	if err != nil {
		res.out.ReopenFailed = true
		res.out.Error = fmt.Sprintf("output page count: %v", err)
		return res
	}
	drawn := make(map[int]bool)
	for _, p := range report.Pages {
		drawn[p.Page] = p.Annotations > 0
	}
	res.out.TextChanged = textChanges(ctx, src, after, pages, drawn)
	res.out.Reflattened, res.out.Transparency64 = f.transparencyLeft(ctx, after)

	f.mu.RLock()
	fonts := f.fonts
	f.mu.RUnlock()
	opts := []pdf.RenderOption{pdf.WithRenderDPI(renderDPI)}
	if fonts != nil {
		opts = append(opts, fonts)
	}
	res.in, res.outPDF = fmt.Sprintf("in-%d.pdf", n), fmt.Sprintf("out-%d.pdf", n)
	staged := os.WriteFile(filepath.Join(staging, res.in), input, 0o600) == nil && //nolint:gosec // res.in is a name this function formats inside the staging directory
		os.WriteFile(filepath.Join(staging, res.outPDF), output, 0o600) == nil //nolint:gosec // res.outPDF is a name this function formats inside the staging directory
	for page := 1; page <= min(pages, renderMaxPages); page++ {
		result, source := lithoPair(ctx, src, after, page, opts)
		res.out.Pages = append(res.out.Pages, flattenPage{Page: page, Litho: result})
		if source != nil {
			if res.sources == nil {
				res.sources = make(map[int]image.Image)
			}
			res.sources[page] = source
		}
		if staged {
			res.jobs = append(res.jobs,
				popplerJob{PDF: res.in, Page: page, PNG: pngRoot(res.in, page)},
				popplerJob{PDF: res.outPDF, Page: page, PNG: pngRoot(res.outPDF, page)})
		}
	}
	return res
}

// flattenOnce flattens src on a Writer of its own and writes the result, nil
// when there was nothing to flatten.
func (f *flattenFeature) flattenOnce(ctx context.Context, src *pdf.Reader) ([]byte, *pdf.FlattenReport, error) {
	w := pdf.NewWriter(src)
	defer w.Close()
	report, err := f.flatten(ctx, w)
	if err != nil {
		return nil, nil, err
	}
	if len(report.Pages) == 0 {
		return nil, report, nil
	}
	var buf bytes.Buffer
	if err := w.WriteTo(ctx, &buf); err != nil {
		return nil, nil, fmt.Errorf("writing: %w", err)
	}
	return buf.Bytes(), report, nil
}

// transparencyLeft returns the pages a second flattening of after changes
// and the ISO 19005-1, 6.4 violations PDF/A-1b validation finds in it.
func (f *flattenFeature) transparencyLeft(ctx context.Context, after *pdf.Reader) ([]int, int) {
	var pages []int
	w := pdf.NewWriter(after)
	if report, err := w.FlattenTransparency(ctx); err == nil {
		for _, p := range report.Pages {
			pages = append(pages, p.Page)
		}
	}
	_ = w.Close()
	violations, err := after.ValidatePDFAReport(ctx, models.PDFA1b)
	if err != nil {
		return pages, 0
	}
	n := 0
	for _, v := range violations {
		if v.Clause == "6.4" || strings.HasPrefix(v.Clause, "6.4.") {
			n++
		}
	}
	return pages, n
}

// textChanges returns the pages whose text, white space collapsed and
// invisible text included, differs between src and after. Flattening keeps
// every text object, but a removed form XObject ends a text run where it
// stood, which can move where the extractor puts a space, and text under a
// constant alpha of 0 is drawn in rendering mode 3, which the default
// extraction leaves out as invisible. On a page in drawn, annotations were
// drawn into the content, and the text their appearances show joins the
// page's, which the extraction leaves out of an annotation: there the page
// changes when a character of the source's text is missing from the
// output's.
func textChanges(ctx context.Context, src, after *pdf.Reader, pages int, drawn map[int]bool) []int {
	var changed []int
	for page := 1; page <= pages; page++ {
		a, errA := src.ExtractText(ctx, page, pdf.WithInvisibleText())
		b, errB := after.ExtractText(ctx, page, pdf.WithInvisibleText())
		// A page with no text whose painting became an image reads as a
		// scan: no text either way.
		if errors.Is(errA, pdf.ErrRequiresOCR) {
			errA = nil
		}
		if errors.Is(errB, pdf.ErrRequiresOCR) {
			errB = nil
		}
		before, out := strings.Join(strings.Fields(a), ""), strings.Join(strings.Fields(b), "")
		if (errA == nil) != (errB == nil) || before != out && (!drawn[page] || !runesWithin(before, out)) {
			changed = append(changed, page)
		}
	}
	return changed
}

// runesWithin reports whether every character of a occurs in b at least as
// many times.
func runesWithin(a, b string) bool {
	count := make(map[rune]int)
	for _, r := range b {
		count[r]++
	}
	for _, r := range a {
		if count[r]--; count[r] < 0 {
			return false
		}
	}
	return true
}

// lithoPair compares litho's renderings of page in src and after, and
// returns the source's, nil when it does not render.
func lithoPair(ctx context.Context, src, after *pdf.Reader, page int, opts []pdf.RenderOption) (*pageResult, image.Image) {
	before, err := src.RenderPage(ctx, page, opts...)
	if err != nil {
		return &pageResult{Page: page, GoldenError: err.Error()}, nil
	}
	got, err := after.RenderPage(ctx, page, opts...)
	if err != nil {
		return &pageResult{Page: page, Error: err.Error()}, before
	}
	res := comparePage(got, before)
	res.Page = page
	return &res, before
}

// popplerPair compares poppler's renderings of page in the staged source and
// output. Poppler is no oracle for a page, said in the second result, when it
// does not render the source page, or draws it apart from litho's rendering,
// lithoSource: the composite is litho's, so poppler then judges the renderers'
// disagreement, which the render cell scores, rather than the flattening.
func popplerPair(staging, in, out string, page int, lithoSource image.Image) (*pageResult, string) {
	before, err := readPNG(filepath.Join(staging, pngRoot(in, page)+".png"))
	if err != nil {
		return nil, "poppler renders no source page"
	}
	if lithoSource == nil {
		return nil, "litho renders no source page"
	}
	if agree := comparePage(before, lithoSource); !agree.Match {
		return nil, fmt.Sprintf("litho and poppler draw the source page apart (%d pixels)", agree.DiffPixels)
	}
	var got image.Image
	got, err = readPNG(filepath.Join(staging, pngRoot(out, page)+".png"))
	if err != nil {
		return &pageResult{Page: page, Error: "poppler renders no output page"}, ""
	}
	// Flattening keeps every page's boxes, which litho's half checks: a size
	// poppler reads apart is a source page tree poppler reads apart from
	// litho, which leaves it no oracle for the page.
	if before.Bounds().Size() != got.Bounds().Size() {
		return nil, fmt.Sprintf("poppler reads the source page at %v and the output's at %v", before.Bounds().Size(), got.Bounds().Size())
	}
	res := comparePage(got, before)
	res.Page = page
	return &res, ""
}

// pngRoot is the PNG path, without its extension, poppler renders page of
// the staged PDF name into.
func pngRoot(name string, page int) string {
	return fmt.Sprintf("%s-p%d", strings.TrimSuffix(name, ".pdf"), page)
}

// Run returns the output Prepare scored.
func (f *flattenFeature) Run(_ context.Context, _, fixtureID string) ([]byte, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if out, ok := f.outputs[fixtureID]; ok {
		return out, nil
	}
	return json.Marshal(flattenOutput{Error: "not prepared"})
}

// Compare sums the checks of one PDF.
func (f *flattenFeature) Compare(fixtureID string, output []byte) Verdict {
	var out flattenOutput
	if err := json.Unmarshal(output, &out); err != nil {
		return Verdict{FixtureID: fixtureID, Reason: fmt.Sprintf("decode output: %v", err)}
	}
	m := map[string]int{
		metricFlattenChecked: 0, metricFlattenUnchanged: 0, metricFlattenRefused: 0, metricFlattenFailed: 0,
		metricReopenFailed: 0, metricFlattenLithoMismatch: 0, metricFlattenPopplerMismatch: 0,
		metricFlattenTextChanged: 0, metricFlattenTransparencyLeft: 0, metricFlattenAnnotations: 0,
		metricFlattenSkippedPages: 0, metricPopplerUnavailable: 0, metricPopplerNoOracle: 0, metricRenderFailed: 0,
	}
	if out.Report != nil {
		if out.Report.TransparentAnnotations > 0 {
			m[metricFlattenAnnotations]++
		}
		m[metricFlattenSkippedPages] += len(out.Report.SkippedPages)
	}
	switch {
	case out.Refused:
		m[metricFlattenRefused]++
		return Verdict{FixtureID: fixtureID, Skipped: true, Reason: out.Error, Metrics: m}
	case out.Unchanged:
		m[metricFlattenUnchanged]++
		return Verdict{FixtureID: fixtureID, Skipped: true, Reason: "no transparency to flatten", Metrics: m}
	case out.Failed:
		m[metricFlattenFailed]++
	case out.ReopenFailed:
		m[metricReopenFailed]++
	case out.Error != "":
		return Verdict{FixtureID: fixtureID, Skipped: true, Reason: out.Error, Metrics: m}
	}
	failed := out.Failed || out.ReopenFailed
	if len(out.TextChanged) > 0 {
		m[metricFlattenTextChanged]++
		failed = true
	}
	if out.transparencyLeft() {
		m[metricFlattenTransparencyLeft]++
		failed = true
	}
	for _, p := range out.Pages {
		if p.Litho != nil && p.Litho.GoldenError != "" {
			// The source page does not render within the budgets: there
			// is nothing to compare the output with.
			m[metricRenderFailed]++
			continue
		}
		m[metricFlattenChecked]++
		if p.Litho == nil || !p.Litho.Match {
			m[metricFlattenLithoMismatch]++
			failed = true
		}
		switch {
		case p.Poppler != nil && !p.Poppler.Match:
			m[metricFlattenPopplerMismatch]++
			failed = true
		case p.Poppler == nil && strings.Contains(p.PopplerSkipped, errPopplerUnavailable.Error()):
			// An unavailable poppler checks nothing, which is no pass.
			m[metricPopplerUnavailable]++
			failed = true
		case p.Poppler == nil && p.PopplerSkipped != "":
			m[metricPopplerNoOracle]++
		}
	}
	v := Verdict{FixtureID: fixtureID, Match: !failed, Score: 1, Metrics: m}
	if failed {
		v.Score = 0
		v.Reason = "the flattened output fails a check"
		if out.Error != "" {
			v.Reason = out.Error
		}
		if raw, err := json.Marshal(out); err == nil {
			v.Mismatches = []json.RawMessage{raw}
		}
	}
	return v
}
