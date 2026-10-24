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
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/gotenberg/litho/pkg/models"
	"github.com/gotenberg/litho/pkg/pdf"
)

// redactRenderMaxPages is how many leading pages the scenarios redact: page 1
// three ways and page 2 once. It bounds the cell's cost to a few renders a
// PDF whatever its length, while page 2 still reaches the inherited
// resources and the page-tree walk a first page does not.
const redactRenderMaxPages = 2

// redactRenderMargin is the width, in device pixels at [renderDPI], of the
// band along a region's edge that neither check reads: the region is eroded
// by it before the inside check and dilated by it before the outside check.
//
// The band holds what both renderers legitimately change at the edge. A
// region's edge falls inside a pixel, and the clip that keeps a cut path off
// the region (and a renderer's anti-aliasing of it) gives that pixel a partial
// coverage on both sides; stroke adjustment and glyph origins move an edge by
// up to one more pixel (see [renderNeighbourhood]). A stroke the redaction
// cuts loses its geometry inside the region shrunk by the stroke's reach,
// and its caps stay inside the region (package mutations, rewriteSubpath),
// so the reach of a cut stroke never passes the edge.
const redactRenderMargin = 2

// redactRenderSelfTolerance is the largest difference, in 8-bit levels, a
// channel may show between two of litho's own renderings of the same pixels
// and still count as equal. Both run the same code on the same samples, so the
// difference is the rounding of composing a page with and without the content
// left out, a level or two; a glyph or a fill litho left in the region differs
// from the page under it by far more.
const redactRenderSelfTolerance = 16

// redactRenderDarkLevel is the largest 8-bit channel value a pixel of litho's
// images-only rendering over black may take inside a region. A redaction
// blanks a picture's covered samples to opaque black (package mutations,
// blankedPicture: the darkest value of the image's own colour space) and a
// stencil's to unpainted, so over a black background a blanked image paints
// black or nothing. The level leaves room for a black that colour management
// does not map to sRGB 0: the CMYK black of a printer profile is a dark grey.
const redactRenderDarkLevel = 80

// redactRenderBandShare is the height of the band scenario as a share of the
// crop box's height: about a line and a half of 10-point text on a Letter
// page, so the band crosses one line and cuts the glyphs of the lines on
// either side.
const redactRenderBandShare = 0.05

// Metrics of the redact-render feature.
const (
	// metricRedactScenarios counts the redactions the checks ran on.
	metricRedactScenarios = "scenarios_run"
	// metricRedactRefused and metricRedactIncomplete count the redactions
	// litho refused, by sentinel ([pdf.ErrRedactionRefused],
	// [pdf.ErrRedactionIncomplete]): refusing is the documented outcome for
	// content that cannot be removed exactly, so neither is a failure.
	metricRedactRefused    = "redaction_refused"
	metricRedactIncomplete = "redaction_incomplete"
	// metricRedactFailed counts the redactions that failed with any other
	// error, or whose output did not open.
	metricRedactFailed = "redaction_failed"
	// metricOutsideChanged and metricInsideLeaked count the scenarios a
	// check failed on, by either renderer; the per-renderer counts follow.
	metricOutsideChanged        = "outside_changed"
	metricInsideLeaked          = "inside_leaked"
	metricOutsideChangedLitho   = "outside_changed_litho"
	metricOutsideChangedPoppler = "outside_changed_poppler"
	metricInsideLeakedLitho     = "inside_leaked_litho"
	metricInsideLeakedPoppler   = "inside_leaked_poppler"
	// metricPopplerUnavailable counts the scenarios whose poppler half did
	// not run: no Docker, no image, a poppler other than the pinned one, or
	// a page pdftoppm could not render.
	metricPopplerUnavailable = "poppler_unavailable"
	// metricPopplerNoOracle counts the scenarios on a source page poppler
	// could not render, or drew at another size than litho: they stand on
	// litho's half.
	metricPopplerNoOracle = "poppler_no_oracle"
	// metricImageSpill counts the scenarios where image blanking changed
	// pixels outside the dilated region, by either renderer (see
	// [redactPixelSets.spill]): blanking is exact to the sample, not to the
	// region, so these are by design and do not fail the outside check.
	metricImageSpill = "image_spill"
)

// redactScenario is one redaction the feature applies: one region on one
// page, in one [pdf.Writer.Redact] call, in the default mode.
type redactScenario struct {
	name string
	page int
	// rect returns the region in page space from the page's crop box and its
	// text, false when the page offers no region of the kind.
	rect func(box [4]float64, text []models.TextElement) ([4]float64, bool)
}

// redactScenarios lists the scenarios, the regions chosen from each page's
// effective crop box (ISO 32000-1, 14.11.2) so they fall where the page is
// displayed:
//
//   - p1-whole: everything page 1 draws, the region of mutate-redact.
//   - p1-quarter: the centred quarter of page 1, which cuts runs, paths and
//     images across all four of its edges.
//   - p1-band: a full-width band [redactRenderBandShare] of the page high,
//     centred on the text run nearest the page's vertical centre (the centre
//     itself on a page with no text), which splits runs at their glyphs.
//   - p2-quarter: the centred quarter of page 2, when there is one.
//
//nolint:gochecknoglobals // static scenario table
var redactScenarios = []redactScenario{
	{name: "p1-whole", page: 1, rect: func(_ [4]float64, _ []models.TextElement) ([4]float64, bool) {
		return [4]float64{-mutationRedactReach, -mutationRedactReach, mutationRedactReach, mutationRedactReach}, true
	}},
	{name: "p1-quarter", page: 1, rect: centredQuarter},
	{name: "p1-band", page: 1, rect: textBand},
	{name: "p2-quarter", page: 2, rect: centredQuarter},
}

// centredQuarter returns the centred half-width, half-height rectangle of box.
func centredQuarter(box [4]float64, _ []models.TextElement) ([4]float64, bool) {
	w, h := box[2]-box[0], box[3]-box[1]
	return [4]float64{box[0] + w/4, box[1] + h/4, box[2] - w/4, box[3] - h/4}, w > 0 && h > 0
}

// textBand returns the p1-band region.
func textBand(box [4]float64, text []models.TextElement) ([4]float64, bool) {
	h := box[3] - box[1]
	if !(h > 0) {
		return [4]float64{}, false
	}
	centre := (box[1] + box[3]) / 2
	y, best := centre, math.Inf(1)
	for _, el := range text {
		if el.Hidden || el.Invisible || el.InkBox == [4]float64{} {
			continue
		}
		mid := (el.InkBox[1] + el.InkBox[3]) / 2
		if d := math.Abs(mid - centre); d < best {
			y, best = mid, d
		}
	}
	half := redactRenderBandShare * h / 2
	return [4]float64{-mutationRedactReach, y - half, mutationRedactReach, y + half}, true
}

// redactRenderFeature redacts the first pages of each PDF and checks, on
// renderings made before and after with litho's renderer and with the pinned
// poppler's pdftoppm, that the redaction did what [pdf.Writer.Redact]
// promises a viewer will see. Where the other features check the bytes a
// redaction writes (mutate-redact reads the text back), this one checks the
// pixels.
//
// Each [redactScenarios] entry runs one Redact call. A refusal, by
// [pdf.ErrRedactionRefused] or [pdf.ErrRedactionIncomplete], is counted and
// checks nothing. For a redaction that succeeds, the affected page is rendered
// before and after at [renderDPI] by both renderers, poppler for every PDF of
// the run in one container ([renderWithPoppler]), and two checks run per
// renderer:
//
//   - outside: every pixel clear of the region dilated by
//     [redactRenderMargin] is unchanged, before against after, under the
//     render cell's pixel rule ([renderChannelTolerance] within
//     [renderNeighbourhood]). A text run whose ink box meets the region is
//     left out of the check: the redaction removes every glyph whose box
//     meets the region whole, including the part of it that reaches outside.
//     A pixel litho's images-only renderings ([pdf.WithRenderContent]) show
//     changed is image blanking, which is exact to the sample: a sample whose
//     centre lies in the region is blanked whole, and an interpolated image
//     blends it into its neighbours. It counts in image_spill, not as a
//     change. Nothing else may change: a path's geometry outside the region
//     is kept under a clip that excludes the region, and the text after a
//     removed glyph keeps its place.
//   - inside: inside the region eroded by [redactRenderMargin], nothing the
//     redaction removed still paints. A successful redaction leaves only
//     images there, their covered samples blanked: text, paths and
//     annotations are gone, and a shading the region meets refuses. So, for
//     litho, the after page must equal litho's rendering of the after page
//     with images alone, within [redactRenderSelfTolerance], which no glyph,
//     fill, stroke, shading or annotation left in the region can pass; and
//     that images-only rendering, drawn over black, must stay at or under
//     [redactRenderDarkLevel], which an image whose samples were not blanked
//     fails wherever its picture is lighter than black. For poppler, each
//     channel of the after page must lie within the range litho's
//     images-only rendering spans around the pixel ([withinRange]), which
//     two rasterizers' edges meet and a mark litho has no trace of does not:
//     poppler sees the content litho's parser may miss.
//
// A scenario passes when both checks pass for both renderers. A scenario
// whose poppler half could not run counts in poppler_unavailable and does
// not pass. One on a source page poppler is no oracle for ([popplerCheck]),
// or whose outside check poppler cannot answer because it draws the source
// page apart from litho, counts in poppler_no_oracle and stands on litho's
// half. A PDF matches when every scenario it ran passes.
//
// The inside check does not see a shape the alpha of a blanked image keeps:
// the black of the blank paints where the soft mask lets it, as a picture
// would. Package mutations' tests hold redaction to making the covered mask
// samples opaque.
//
// Cost: two renders of each affected page before, four after per scenario
// (litho's full, images-only over white and over black, poppler's), all at
// 72 dpi, on pages 1 and 2 only, and one container per corpus run. The run
// prints its wall time per corpus on stderr.
//
// The PDFs are those the render goldens cover, the same one-in-sixteen
// sample on pdfjs and govdocs, and every PDF of a corpus whose golden
// directory holds no render index (pdfcpu). The goldens' fonts serve
// litho's renderings, so both renderers draw an unembedded font alike; a
// corpus without them draws such text as placeholder boxes, which changes
// nothing a before-and-after comparison sees.
//
// # Corpus evidence
//
//   - litho, pdfcpu, pdfjs, govdocs: 600, 101, 178 and 170 scenarios
//     checked in about 25, 11, 15 and 40 seconds (4 workers).
//   - litho directstream_2b: a band over text a content stream written
//     inline in /Contents draws reported success and removed nothing.
//   - litho cmaps_2b, type0nodesc_2b, usecmap_2u: codes extraction reads no
//     glyph for stayed, drawn by litho's renderer inside a whole-page region.
//   - litho qshyphen_2b: a Td inside q...Q in a text object put a word
//     poppler draws inside a band litho read clear.
//   - pdfjs text_clip_cff_cid: removing text in rendering mode 7 widened the
//     clip, and the fill it masked painted as a rectangle outside the band.
//   - pdfcpu WaldenFull: a cover image's soft mask kept its lettering
//     readable after a whole-page redaction.
//   - govdocs thread_400_400756 page 2: a table rule along a region's edge
//     drew grey across the table once split into one path.
//   - litho iccv4space_1b, megafont_2b, multipage_2b: image_spill, a
//     2 by 2 image's samples, one of them interpolated, reaching past the
//     region.
//   - pdfjs bitmap-halftone-skip-grid-template2, bitmap-refine-tpgron,
//     issue17871_top_right: JBIG2 regions poppler garbles, which change in
//     poppler once redaction re-encodes them from litho's decode.
//   - govdocs thread_000_000372 p1-quarter: poppler's 1 changed pixel outside
//     the region is a decoder difference on a CMYK ICCBased JPEG blanked in a
//     copy, which stores litho's decode while poppler decodes the original.
//     litho's half sees 0. Accepted as a mismatch.
//   - pdfjs issue269_2 p1-quarter and p1-band: litho's half sees 716 and 357
//     changed pixels outside the region, one-pixel anti-aliasing seams at the
//     edges of the paths the redaction removed or clipped (312 and 536 on the
//     quarter). poppler and PDFium see 0. Restoring the original forms and
//     images in the output leaves the same 716 pixels, so the form copies do
//     not cause them. Accepted as a mismatch until litho's seam rendering is
//     fixed.
type redactRenderFeature struct {
	// workers bounds the PDFs prepared at once.
	workers int
	// poppler renders the batch, [renderWithPoppler] outside tests.
	poppler func(ctx context.Context, dir string, jobs []popplerJob) (string, error)
	// pin returns the poppler version the batch must report, "" to accept
	// any.
	pin func() string
	// redact applies a scenario's region and returns its report, nil when it
	// has none; tests replace it to plant a leak.
	redact func(ctx context.Context, w *pdf.Writer, region pdf.RedactRegion) (*pdf.RedactReport, error)

	mu sync.RWMutex
	// sampled names the PDFs in scope, nil for every PDF.
	sampled map[string]bool
	fonts   pdf.RenderOption
	outputs map[string][]byte
}

func newRedactRenderFeature() *redactRenderFeature {
	return &redactRenderFeature{
		workers: runtime.NumCPU(),
		poppler: renderWithPoppler,
		pin:     popplerPin,
		redact: func(ctx context.Context, w *pdf.Writer, region pdf.RedactRegion) (*pdf.RedactReport, error) {
			return w.Redact(ctx, []pdf.RedactRegion{region})
		},
		outputs: make(map[string][]byte),
	}
}

func (f *redactRenderFeature) Name() string         { return "redact-render" }
func (f *redactRenderFeature) GoldenSource() string { return "litho+poppler" }

// OncePerPDF marks the feature as profile-independent.
func (f *redactRenderFeature) OncePerPDF() {}

// LoadGoldens reads which PDFs the render goldens cover and their fonts. A
// directory with no render index puts every PDF in scope.
func (f *redactRenderFeature) LoadGoldens(dir string) error {
	sampled, fonts, err := loadRenderSample(dir)
	if err != nil {
		return err
	}
	f.mu.Lock()
	f.sampled, f.fonts = sampled, fonts
	f.mu.Unlock()
	return nil
}

// loadRenderSample reads which PDFs the render goldens under dir cover, nil
// when dir holds no render index (every PDF in scope), and the option that
// draws with the fonts the goldens were drawn with, nil when it holds none.
// It returns [ErrNoGoldens] when dir does not exist.
func loadRenderSample(dir string) (map[string]bool, pdf.RenderOption, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, ErrNoGoldens
	}
	var sampled map[string]bool
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name())) //nolint:gosec // golden under the corpus testdata root
		if err != nil {
			return nil, nil, fmt.Errorf("read %s: %w", e.Name(), err)
		}
		var idx renderIndex
		if json.Unmarshal(data, &idx) != nil || idx.DPI != renderDPI {
			continue
		}
		if sampled == nil {
			sampled = make(map[string]bool)
		}
		sampled[strings.TrimSuffix(e.Name(), ".json")] = true
	}
	var fonts pdf.RenderOption
	if info, err := os.Stat(filepath.Join(dir, renderFontsDir)); err == nil && info.IsDir() {
		fonts = pdf.WithRenderFontFiles(os.DirFS(filepath.Join(dir, renderFontsDir)))
	}
	return sampled, fonts, nil
}

func (f *redactRenderFeature) HasGolden(fixtureID string) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.sampled == nil || f.sampled[baseFixtureID(fixtureID)]
}

// redactRenderOutput is the feature's per-PDF output.
type redactRenderOutput struct {
	Error     string                `json:"error,omitempty"`
	Scenarios []redactRenderOutcome `json:"scenarios,omitempty"`
}

// redactRenderOutcome is one scenario's outcome.
type redactRenderOutcome struct {
	Name string     `json:"name"`
	Page int        `json:"page"`
	Rect [4]float64 `json:"rect"`
	// Refused is the sentinel class of a refusal, "refused" or "incomplete";
	// Failed holds any other error. Cause is the error's text.
	Refused string             `json:"refused,omitempty"`
	Failed  bool               `json:"failed,omitempty"`
	Cause   string             `json:"cause,omitempty"`
	Litho   *redactRenderCheck `json:"litho,omitempty"`
	Poppler *redactRenderCheck `json:"poppler,omitempty"`
	// PopplerSkipped says why the poppler half did not run. NoOracle marks
	// a source page poppler is no oracle for (see [popplerCheck]): the
	// scenario stands on litho's half.
	PopplerSkipped string `json:"poppler_skipped,omitempty"`
	NoOracle       bool   `json:"no_oracle,omitempty"`
}

// redactRenderCheck is one renderer's two checks on one scenario: how many
// pixels each read, how many failed, and the box [x0 y0 x1 y1) holding the
// failures, in device pixels.
type redactRenderCheck struct {
	OutsidePixels int    `json:"outside_pixels"`
	Outside       int    `json:"outside_changed"`
	OutsideBox    [4]int `json:"outside_box,omitzero"`
	InsidePixels  int    `json:"inside_pixels"`
	Inside        int    `json:"inside_leaked"`
	InsideBox     [4]int `json:"inside_box,omitzero"`
	// ImageSpill counts the pixels outside the dilated region that changed
	// because image blanking changed them (see [redactPixelSets.spill]).
	ImageSpill int `json:"image_spill,omitempty"`
	// OutsideNoOracle says why the outside check did not run.
	OutsideNoOracle string `json:"outside_no_oracle,omitempty"`
	// Error says why the renderer's half did not run.
	Error string `json:"error,omitempty"`
}

func (c *redactRenderCheck) passed() bool {
	return c != nil && c.Error == "" && c.Outside == 0 && c.Inside == 0
}

// redactRenderStaged is what the poppler half of one scenario needs once the
// batch has rendered: the page's pixel sets and litho's images-only
// rendering of the after page.
type redactRenderStaged struct {
	outcome *redactRenderOutcome
	sets    *redactPixelSets
	before  string // poppler PNG root of the page before
	after   string // poppler PNG root of the page after
	images  string // litho's images-only rendering, a PNG path
	// lithoBefore is litho's rendering of the source page, a PNG path.
	lithoBefore string
}

// redactSource is litho's rendering of a source page: whole, and with its
// images alone, both over white.
type redactSource struct {
	full, images []uint8
}

// redactRenderPDF is one PDF's progress through Prepare.
type redactRenderPDF struct {
	id     string
	out    redactRenderOutput
	staged []redactRenderStaged
	jobs   []popplerJob
}

// Prepare runs every scenario of every PDF in scope, renders the pages
// poppler draws in one batch, and scores both halves. Run looks the result
// up.
func (f *redactRenderFeature) Prepare(ctx context.Context, fixtures []Fixture) error {
	start := time.Now()
	staging, err := os.MkdirTemp("", "litho-redact-render-")
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
	results := make([]*redactRenderPDF, len(todo))
	f.pool(len(todo), func(i int) {
		fctx, cancel := context.WithTimeout(ctx, prepareFixtureTimeout)
		defer cancel()
		results[i] = f.prepareOne(fctx, staging, i, todo[i])
	})
	if err := ctx.Err(); err != nil {
		return err
	}
	lithoDone := time.Since(start)

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
			fmt.Fprintf(os.Stderr, "redact-render: %s\n", unavailable)
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	f.pool(len(results), func(i int) {
		for _, st := range results[i].staged {
			if unavailable != "" {
				st.outcome.PopplerSkipped = unavailable
				continue
			}
			check, skip := popplerCheck(staging, st)
			st.outcome.Poppler, st.outcome.PopplerSkipped = check, skip
			st.outcome.NoOracle = check == nil
		}
	})

	f.mu.Lock()
	scenarios := 0
	for _, res := range results {
		for _, sc := range res.out.Scenarios {
			if sc.Litho != nil {
				scenarios++
			}
		}
		data, err := json.Marshal(res.out)
		if err != nil {
			data = []byte(`{"error":"marshal"}`)
		}
		f.outputs[res.id] = data
	}
	f.mu.Unlock()
	fmt.Fprintf(os.Stderr, "redact-render: %d PDFs, %d scenarios checked in %s (litho %s, poppler %d pages)\n",
		len(todo), scenarios, time.Since(start).Round(time.Millisecond), lithoDone.Round(time.Millisecond), len(jobs))
	return nil
}

// pool runs fn for 0..n-1 on at most f.workers goroutines. fn recovers
// nothing: prepareOne and popplerCheck do not panic on a PDF, and a panic in
// either is the harness's own defect.
func (f *redactRenderFeature) pool(n int, fn func(i int)) {
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

// Run returns the output Prepare scored.
func (f *redactRenderFeature) Run(_ context.Context, _, fixtureID string) ([]byte, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if out, ok := f.outputs[fixtureID]; ok {
		return out, nil
	}
	return json.Marshal(redactRenderOutput{Error: "not prepared"})
}

// prepareOne runs the scenarios of one PDF and litho's half of their checks,
// and stages what poppler renders.
func (f *redactRenderFeature) prepareOne(ctx context.Context, staging string, idx int, fx Fixture) (res *redactRenderPDF) {
	res = &redactRenderPDF{id: fx.ID}
	defer func() {
		if r := recover(); r != nil {
			res.out = redactRenderOutput{Error: fmt.Sprintf("panic: %v", r)}
			res.staged, res.jobs = nil, nil
		}
	}()
	data, err := os.ReadFile(fx.PDFPath) //nolint:gosec // fx.PDFPath comes from the corpus testdata walk
	if err != nil {
		res.out.Error = fmt.Sprintf("read: %v", err)
		return res
	}
	src, err := pdf.Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		res.out.Error = fmt.Sprintf("open: %v", err)
		return res
	}
	defer src.Close()
	pages, err := src.PageCount(ctx)
	if err != nil {
		res.out.Error = fmt.Sprintf("page count: %v", err)
		return res
	}
	in := fmt.Sprintf("%d.pdf", idx)
	if err := os.WriteFile(filepath.Join(staging, in), data, 0o600); err != nil { //nolint:gosec // staging is our temp dir, in a name built from an index
		res.out.Error = fmt.Sprintf("stage: %v", err)
		return res
	}
	f.mu.RLock()
	fonts := f.fonts
	f.mu.RUnlock()
	var opts []pdf.RenderOption
	if fonts != nil {
		opts = append(opts, fonts)
	}

	before := make(map[int]*redactSource)
	staged := make(map[int]bool)
	// Room for every scenario up front: the staged outcomes point into the
	// slice, which must not move.
	res.out.Scenarios = make([]redactRenderOutcome, 0, len(redactScenarios))
	for si, sc := range redactScenarios {
		if sc.page > min(pages, redactRenderMaxPages) {
			continue
		}
		pg, err := src.Page(ctx, sc.page)
		if err != nil {
			res.out.Error = fmt.Sprintf("page %d: %v", sc.page, err)
			return res
		}
		text, err := src.ExtractTextElements(ctx, sc.page)
		if err != nil {
			text = nil
		}
		box := viewedBox(pg)
		rect, ok := sc.rect(box, text)
		if !ok {
			continue
		}
		res.out.Scenarios = append(res.out.Scenarios, redactRenderOutcome{Name: sc.name, Page: sc.page, Rect: rect})
		outcome := &res.out.Scenarios[len(res.out.Scenarios)-1]

		output, report, err := f.redactOnce(ctx, src, pdf.RedactRegion{Page: sc.page, Rect: rect})
		if err != nil {
			outcome.Cause = err.Error()
			switch {
			case errors.Is(err, pdf.ErrRedactionRefused):
				outcome.Refused = "refused"
			case errors.Is(err, pdf.ErrRedactionIncomplete):
				outcome.Refused = "incomplete"
			default:
				outcome.Failed = true
			}
			continue
		}
		after, err := pdf.Open(ctx, bytes.NewReader(output), int64(len(output)))
		if err != nil {
			outcome.Failed, outcome.Cause = true, fmt.Sprintf("output does not open: %v", err)
			continue
		}
		geom := redactPageGeometry(pg)
		var removed [][4]float64
		if report != nil {
			removed = report.RemovedGlyphBoxes[sc.page]
		}
		sets := newRedactPixelSets(geom, rect, text, removed)
		check, images := lithoCheck(ctx, src, after, sc.page, sets, before, opts)
		after.Close()
		outcome.Litho = check
		if check.Error != "" {
			continue
		}

		root := fmt.Sprintf("%d.p%d", idx, sc.page)
		lithoBefore := filepath.Join(staging, root+".litho.png")
		if !staged[sc.page] {
			if err := writePNG(lithoBefore, before[sc.page].full, sets.w, sets.h); err != nil {
				outcome.PopplerSkipped = fmt.Sprintf("stage: %v", err)
				continue
			}
			staged[sc.page] = true
			res.jobs = append(res.jobs, popplerJob{PDF: in, Page: sc.page, PNG: root})
		}
		out := fmt.Sprintf("%d.s%d.pdf", idx, si)
		imagesPath := filepath.Join(staging, fmt.Sprintf("%d.s%d.images.png", idx, si))
		if err := os.WriteFile(filepath.Join(staging, out), output, 0o600); err != nil { //nolint:gosec // staging is our temp dir, out a name built from indices
			outcome.PopplerSkipped = fmt.Sprintf("stage: %v", err)
			continue
		}
		if err := writePNG(imagesPath, images, sets.w, sets.h); err != nil {
			outcome.PopplerSkipped = fmt.Sprintf("stage: %v", err)
			continue
		}
		afterRoot := fmt.Sprintf("%d.s%d", idx, si)
		res.jobs = append(res.jobs, popplerJob{PDF: out, Page: sc.page, PNG: afterRoot})
		res.staged = append(res.staged, redactRenderStaged{
			outcome: outcome, sets: sets, before: root, after: afterRoot, images: imagesPath, lithoBefore: lithoBefore,
		})
	}
	return res
}

// redactOnce applies one region to a Writer over src and writes the result,
// with the redaction's report.
func (f *redactRenderFeature) redactOnce(ctx context.Context, src *pdf.Reader, region pdf.RedactRegion) ([]byte, *pdf.RedactReport, error) {
	w := pdf.NewWriter(src)
	defer w.Close()
	report, err := f.redact(ctx, w, region)
	if err != nil {
		return nil, nil, err
	}
	var buf bytes.Buffer
	if err := w.WriteTo(ctx, &buf); err != nil {
		return nil, nil, err
	}
	return buf.Bytes(), report, nil
}

// lithoCheck renders page before and after with litho and runs both checks,
// and returns the images-only rendering of the after page over white that
// the poppler half compares with. before caches the source's renderings by
// page. It records in sets the pixels the redaction's image blanking changed
// ([redactPixelSets.spill]).
func lithoCheck(ctx context.Context, src, after *pdf.Reader, page int, sets *redactPixelSets, before map[int]*redactSource, opts []pdf.RenderOption) (*redactRenderCheck, []uint8) {
	check := &redactRenderCheck{}
	render := func(r *pdf.Reader, extra ...pdf.RenderOption) ([]uint8, error) {
		img, err := r.RenderPage(ctx, page, append(append([]pdf.RenderOption{pdf.WithRenderDPI(renderDPI)}, opts...), extra...)...)
		if err != nil {
			return nil, err
		}
		if b := img.Bounds(); b.Dx() != sets.w || b.Dy() != sets.h {
			return nil, fmt.Errorf("rendered %dx%d, page geometry %dx%d", b.Dx(), b.Dy(), sets.w, sets.h)
		}
		return flattenOnWhite(img, sets.w, sets.h), nil
	}
	orig, ok := before[page]
	if !ok {
		full, err := render(src)
		if err != nil {
			check.Error = fmt.Sprintf("render before: %v", err)
			return check, nil
		}
		images, err := render(src, pdf.WithRenderContent(pdf.RenderImages))
		if err != nil {
			check.Error = fmt.Sprintf("render before, images only: %v", err)
			return check, nil
		}
		orig = &redactSource{full: full, images: images}
		before[page] = orig
	}
	full, err := render(after)
	if err != nil {
		check.Error = fmt.Sprintf("render after: %v", err)
		return check, nil
	}
	images, err := render(after, pdf.WithRenderContent(pdf.RenderImages))
	if err != nil {
		check.Error = fmt.Sprintf("render after, images only: %v", err)
		return check, nil
	}
	onBlack, err := render(after, pdf.WithRenderContent(pdf.RenderImages), pdf.WithRenderBackground(color.Black))
	if err != nil {
		check.Error = fmt.Sprintf("render after, images only over black: %v", err)
		return check, nil
	}
	sets.markSpill(orig.images, images)
	check.OutsidePixels, check.Outside, check.OutsideBox, check.ImageSpill = sets.countOutside(orig.full, full, sets.w, renderChannelTolerance, renderNeighbourhood)
	var leaks pixelBox
	for y := range sets.h {
		for x := range sets.w {
			if !sets.inside(x, y) {
				continue
			}
			check.InsidePixels++
			i := 3 * (y*sets.w + x)
			if channelDelta(full[i:i+3], images[i:i+3]) > redactRenderSelfTolerance ||
				max(onBlack[i], onBlack[i+1], onBlack[i+2]) > redactRenderDarkLevel {
				leaks.add(x, y)
			}
		}
	}
	check.Inside, check.InsideBox = leaks.n, leaks.rect()
	return check, images
}

// popplerCheck runs both checks on poppler's renderings of one scenario. It
// returns no check and why when poppler is no oracle for the source page: it
// could not render the page, or drew it at another size than litho, which
// the render cell adjudicates poppler-side (a page tree poppler reads
// otherwise, a box it reads otherwise). A source page poppler drew and an
// output page it then could not draw, or drew at another size, is a check
// that failed: the redaction broke the page for a viewer.
func popplerCheck(staging string, st redactRenderStaged) (*redactRenderCheck, string) {
	load := func(root string) ([]uint8, int, int, string) {
		img, err := readPNG(filepath.Join(staging, root+".png"))
		if err != nil {
			msg, _ := os.ReadFile(filepath.Join(staging, root+".err")) //nolint:gosec // our staging directory
			return nil, 0, 0, fmt.Sprintf("pdftoppm: %s", strings.TrimSpace(firstLine(msg)))
		}
		b := img.Bounds()
		return flattenOnWhite(img, b.Dx(), b.Dy()), b.Dx(), b.Dy(), ""
	}
	sets := st.sets
	orig, bw, bh, skip := load(st.before)
	if skip != "" {
		return nil, "source page: " + skip
	}
	if absInt(bw-sets.w) > renderSizeSlack || absInt(bh-sets.h) > renderSizeSlack {
		return nil, fmt.Sprintf("source page: poppler draws %dx%d, litho %dx%d", bw, bh, sets.w, sets.h)
	}
	check := &redactRenderCheck{}
	full, aw, ah, skip := load(st.after)
	if skip != "" {
		check.Error = "redacted page: " + skip
		return check, ""
	}
	if bw != aw || bh != ah {
		check.Error = fmt.Sprintf("poppler draws the source page %dx%d, the redacted page %dx%d", bw, bh, aw, ah)
		return check, ""
	}
	imagesImg, err := readPNG(st.images)
	if err != nil {
		check.Error = fmt.Sprintf("litho images-only rendering: %v", err)
		return check, ""
	}
	lithoBefore, err := readPNG(st.lithoBefore)
	if err != nil {
		check.Error = fmt.Sprintf("litho rendering of the source page: %v", err)
		return check, ""
	}
	w, h := min(bw, sets.w), min(bh, sets.h)
	o := sets.cropped(w, h)
	// The outside check asks poppler whether the page changed, which it can
	// answer only where it draws the source page as litho does: where the
	// render cell finds the two apart, poppler's own decode of an image the
	// redaction re-encoded from litho's decode changes the page however
	// exact the redaction was (pdfjs JBIG2 halftone and refinement regions
	// poppler garbles). It reads the page bar alone: the share of the ink
	// the render cell also weighs withholds the oracle from a page where
	// the renderers disagree on one glyph or annotation, while poppler's
	// before and after still answer for the rest of the page.
	if cmp := comparePage(lithoBefore, decodedRGB(orig, bw, bh)); !cmp.withinPageShare() {
		check.OutsideNoOracle = fmt.Sprintf("poppler draws the source page apart from litho: %d of %d pixels differ", cmp.DiffPixels, cmp.Pixels)
	} else {
		check.OutsidePixels, check.Outside, check.OutsideBox, check.ImageSpill = o.countOutside(cropRGB(orig, bw, w, h), cropRGB(full, aw, w, h), w, renderChannelTolerance, renderNeighbourhood)
	}
	images := cropRGB(flattenOnWhite(imagesImg, sets.w, sets.h), sets.w, w, h)
	full = cropRGB(full, aw, w, h)
	var leaks pixelBox
	for y := range h {
		for x := range w {
			if !o.inside(x, y) {
				continue
			}
			check.InsidePixels++
			i := 3 * (y*w + x)
			// One way: the question is whether poppler draws something
			// litho's images-only rendering, checked clean above, has no
			// trace of. Where litho draws more than poppler, litho's own
			// half has already judged it.
			if !withinRange(full[i:i+3], images, w, h, x, y) {
				leaks.add(x, y)
			}
		}
	}
	check.Inside, check.InsideBox = leaks.n, leaks.rect()
	return check, ""
}

// withinRange reports whether each channel of px lies within
// [renderChannelTolerance] of the range that channel spans over the pixels
// within [renderNeighbourhood] of (x, y) in img. Two rasterizers put an edge
// a pixel apart and anti-alias it with their own coverage, so an edge pixel
// of one takes any value between the two sides the other shows nearby; a
// mark the other has no trace of near that place lies outside that range.
func withinRange(px, img []uint8, w, h, x, y int) bool {
	for c := range 3 {
		lo, hi := 255, 0
		for ny := max(0, y-renderNeighbourhood); ny <= min(h-1, y+renderNeighbourhood); ny++ {
			for nx := max(0, x-renderNeighbourhood); nx <= min(w-1, x+renderNeighbourhood); nx++ {
				v := int(img[3*(ny*w+nx)+c])
				lo, hi = min(lo, v), max(hi, v)
			}
		}
		if v := int(px[c]); v < lo-renderChannelTolerance || v > hi+renderChannelTolerance {
			return false
		}
	}
	return true
}

// decodedRGB wraps an RGB raster as an image.
func decodedRGB(rgb []uint8, w, h int) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := range w * h {
		copy(img.Pix[4*i:4*i+3], rgb[3*i:3*i+3])
		img.Pix[4*i+3] = 0xff
	}
	return img
}

// cropRGB returns the top-left w by h pixels of an RGB raster stride pixels
// wide.
func cropRGB(px []uint8, stride, w, h int) []uint8 {
	if stride == w && len(px) == 3*w*h {
		return px
	}
	out := make([]uint8, 3*w*h)
	for y := range h {
		copy(out[3*y*w:3*(y+1)*w], px[3*y*stride:3*y*stride+3*w])
	}
	return out
}

// writePNG writes an RGB raster w by h pixels as an opaque PNG.
func writePNG(path string, rgb []uint8, w, h int) error {
	var buf bytes.Buffer
	if err := png.Encode(&buf, decodedRGB(rgb, w, h)); err != nil {
		return fmt.Errorf("encode %s: %w", filepath.Base(path), err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	return nil
}

// Compare sums the scenarios' outcomes.
func (f *redactRenderFeature) Compare(fixtureID string, output []byte) Verdict {
	var out redactRenderOutput
	if err := json.Unmarshal(output, &out); err != nil {
		return Verdict{FixtureID: fixtureID, Reason: fmt.Sprintf("decode output: %v", err)}
	}
	if out.Error != "" {
		return Verdict{FixtureID: fixtureID, Skipped: true, Reason: out.Error}
	}
	m := map[string]int{
		metricRedactScenarios: 0, metricRedactRefused: 0, metricRedactIncomplete: 0, metricRedactFailed: 0,
		metricOutsideChanged: 0, metricInsideLeaked: 0, metricOutsideChangedLitho: 0, metricOutsideChangedPoppler: 0,
		metricInsideLeakedLitho: 0, metricInsideLeakedPoppler: 0, metricPopplerUnavailable: 0, metricPopplerNoOracle: 0,
		metricRenderFailed: 0, metricImageSpill: 0,
	}
	checked, passed := 0, 0
	var mismatches []json.RawMessage
	for _, sc := range out.Scenarios {
		switch {
		case sc.Refused == "refused":
			m[metricRedactRefused]++
			continue
		case sc.Refused == "incomplete":
			m[metricRedactIncomplete]++
			continue
		case sc.Failed:
			m[metricRedactFailed]++
		case sc.Litho == nil:
			continue
		case sc.Litho.Error != "":
			m[metricRenderFailed]++
		default:
			m[metricRedactScenarios]++
			checked++
			outside := sc.Litho.Outside > 0
			inside := sc.Litho.Inside > 0
			spill := sc.Litho.ImageSpill > 0
			if outside {
				m[metricOutsideChangedLitho]++
			}
			if inside {
				m[metricInsideLeakedLitho]++
			}
			switch {
			case sc.Poppler == nil && sc.NoOracle:
				m[metricPopplerNoOracle]++
			case sc.Poppler == nil:
				m[metricPopplerUnavailable]++
			case sc.Poppler.Error != "":
				m[metricRenderFailed]++
			default:
				if sc.Poppler.OutsideNoOracle != "" {
					m[metricPopplerNoOracle]++
				}
				if sc.Poppler.ImageSpill > 0 {
					spill = true
				}
				if sc.Poppler.Outside > 0 {
					m[metricOutsideChangedPoppler]++
					outside = true
				}
				if sc.Poppler.Inside > 0 {
					m[metricInsideLeakedPoppler]++
					inside = true
				}
			}
			if outside {
				m[metricOutsideChanged]++
			}
			if inside {
				m[metricInsideLeaked]++
			}
			if spill {
				m[metricImageSpill]++
			}
			if sc.Litho.passed() && (sc.Poppler.passed() || sc.Poppler == nil && sc.NoOracle) {
				passed++
				continue
			}
		}
		data, err := json.Marshal(sc)
		if err != nil {
			data = json.RawMessage(`{"error":"marshal"}`)
		}
		mismatches = append(mismatches, data)
	}
	v := Verdict{FixtureID: fixtureID, Match: len(mismatches) == 0, Score: 1, Metrics: m, Mismatches: mismatches}
	if checked > 0 {
		v.Score = float64(passed) / float64(checked)
	}
	if !v.Match {
		v.Reason = fmt.Sprintf("%d of %d scenarios fail a check", len(mismatches), len(out.Scenarios))
	}
	return v
}

// popplerPin returns the poppler release versions.env pins, the one the
// corpus image builds, or "" when the pins do not load, which accepts the
// image's.
func popplerPin() string {
	pins, err := loadPins()
	if err != nil {
		return ""
	}
	return pins.Values["POPPLER_VERSION"]
}

// viewedBox returns the region of pg a viewer shows, the box
// [pdf.Reader.RenderPage] draws: the effective crop box, else the media box,
// else US Letter, the first whose size is the page's Width and Height, which
// [pdf.Reader.Page] reports for the box a viewer uses.
func viewedBox(pg models.Page) [4]float64 {
	unit := pg.UserUnit
	if !(unit > 0) || math.IsInf(unit, 0) {
		unit = 1
	}
	for _, b := range [][4]float64{pg.CropBox, pg.MediaBox} {
		x0, x1 := min(b[0], b[2]), max(b[0], b[2])
		y0, y1 := min(b[1], b[3]), max(b[1], b[3])
		w, h := (x1-x0)*unit, (y1-y0)*unit
		if r := ((pg.Rotate % 360) + 360) % 360; r == 90 || r == 270 {
			w, h = h, w
		}
		if x1 > x0 && y1 > y0 && math.Abs(w-pg.Width) < 1e-6 && math.Abs(h-pg.Height) < 1e-6 {
			return [4]float64{x0, y0, x1, y1}
		}
	}
	return [4]float64{0, 0, 612, 792}
}

// redactGeometry maps a page's default user space to the device pixels of a
// rendering at [renderDPI], as [pdf.Reader.RenderPage] places the page: the
// viewed box scaled by /UserUnit, origin at its top-left corner, then turned
// clockwise by /Rotate (ISO 32000-1, Table 30).
type redactGeometry struct {
	m    [6]float64
	w, h int
}

func redactPageGeometry(pg models.Page) redactGeometry {
	box := viewedBox(pg)
	unit := pg.UserUnit
	if !(unit > 0) || math.IsInf(unit, 0) {
		unit = 1
	}
	s := renderDPI / 72.0 * unit
	w, h := (box[2]-box[0])*s, (box[3]-box[1])*s
	base := [6]float64{s, 0, 0, -s, -box[0] * s, box[3] * s}
	g := redactGeometry{w: devicePixels(w), h: devicePixels(h)}
	switch ((pg.Rotate % 360) + 360) % 360 {
	case 90:
		g.m = mulMatrix(base, [6]float64{0, 1, -1, 0, h, 0})
		g.w, g.h = g.h, g.w
	case 180:
		g.m = mulMatrix(base, [6]float64{-1, 0, 0, -1, w, h})
	case 270:
		g.m = mulMatrix(base, [6]float64{0, -1, 1, 0, 0, w})
		g.w, g.h = g.h, g.w
	default:
		g.m = base
	}
	return g
}

// devicePixels rounds a side up to whole pixels, at least one, as the
// renderer does.
func devicePixels(v float64) int {
	return max(1, int(math.Ceil(v-1e-6)))
}

// mulMatrix returns the matrix applying a, then b (ISO 32000-1, 8.3.4).
func mulMatrix(a, b [6]float64) [6]float64 {
	return [6]float64{
		a[0]*b[0] + a[1]*b[2], a[0]*b[1] + a[1]*b[3],
		a[2]*b[0] + a[3]*b[2], a[2]*b[1] + a[3]*b[3],
		a[4]*b[0] + a[5]*b[2] + b[4], a[4]*b[1] + a[5]*b[3] + b[5],
	}
}

// deviceRect maps a page-space rectangle to the device rectangle holding it.
func (g redactGeometry) deviceRect(r [4]float64) [4]float64 {
	out := [4]float64{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	for _, p := range [4][2]float64{{r[0], r[1]}, {r[2], r[1]}, {r[0], r[3]}, {r[2], r[3]}} {
		x := g.m[0]*p[0] + g.m[2]*p[1] + g.m[4]
		y := g.m[1]*p[0] + g.m[3]*p[1] + g.m[5]
		out[0], out[1] = math.Min(out[0], x), math.Min(out[1], y)
		out[2], out[3] = math.Max(out[2], x), math.Max(out[3], y)
	}
	return out
}

// redactPixelSets tells, for one scenario's page, which pixels each check
// reads.
type redactPixelSets struct {
	w, h int
	// region is the region in device space; excluded holds the device boxes
	// of the text runs the region meets and of the glyphs the redaction
	// removed, each dilated by the margin.
	region   [4]float64
	excluded [][4]float64
	// spill marks, w pixels a row, the pixels where litho's images-only
	// renderings of the page before and after differ, grown by
	// [renderNeighbourhood]: what the redaction changed by blanking image
	// samples.
	spill []bool
}

// markSpill records the pixels image blanking changed, from litho's
// images-only renderings of the page before and after.
func (s *redactPixelSets) markSpill(before, after []uint8) {
	changed := make([]bool, s.w*s.h)
	for i := range changed {
		changed[i] = channelDelta(before[3*i:3*i+3], after[3*i:3*i+3]) > redactRenderSelfTolerance
	}
	s.spill = make([]bool, s.w*s.h)
	for y := range s.h {
		for x := range s.w {
			if !changed[y*s.w+x] {
				continue
			}
			for ny := max(0, y-renderNeighbourhood); ny <= min(s.h-1, y+renderNeighbourhood); ny++ {
				for nx := max(0, x-renderNeighbourhood); nx <= min(s.w-1, x+renderNeighbourhood); nx++ {
					s.spill[ny*s.w+nx] = true
				}
			}
		}
	}
}

// newRedactPixelSets returns the pixel sets of a scenario whose region is rect
// on a page of geometry g, text the runs extraction reads on the page.
//
// removed holds the boxes of the glyphs the redaction removed
// ([pdf.RedactReport.RemovedGlyphBoxes]). A glyph goes whole when its box
// meets the region, so its ink past the region changes by design. The text
// runs alone miss some of them: extraction drops a run whose characters are
// all white space, though its glyphs may paint, as a font may draw a glyph
// for U+2002. Only the outside check reads the exclusions, so the inside
// check still sees any glyph the redaction kept inside the region.
//
// # Corpus evidence
//
//   - litho cmapverabase_2b page 1, band: a glyph shown for U+2002 inks 3.8
//     points past the band's top edge. Removed, it changed 12 pixels poppler
//     draws outside the band, which the text runs did not exclude.
func newRedactPixelSets(g redactGeometry, rect [4]float64, text []models.TextElement, removed [][4]float64) *redactPixelSets {
	r := [4]float64{min(rect[0], rect[2]), min(rect[1], rect[3]), max(rect[0], rect[2]), max(rect[1], rect[3])}
	sets := &redactPixelSets{w: g.w, h: g.h, region: g.deviceRect(r)}
	for _, el := range text {
		ink := el.InkBox
		if ink == [4]float64{} || ink[0] > r[2] || ink[2] < r[0] || ink[1] > r[3] || ink[3] < r[1] {
			continue
		}
		sets.excluded = append(sets.excluded, grow(g.deviceRect(ink), redactRenderMargin))
	}
	for _, box := range removed {
		sets.excluded = append(sets.excluded, grow(g.deviceRect(box), redactRenderMargin))
	}
	return sets
}

// cropped returns the sets for the top-left w by h pixels.
func (s *redactPixelSets) cropped(w, h int) *redactPixelSets {
	c := *s
	c.w, c.h = w, h
	if s.spill != nil && w != s.w {
		c.spill = make([]bool, w*h)
		for y := range h {
			copy(c.spill[y*w:(y+1)*w], s.spill[y*s.w:y*s.w+w])
		}
	}
	return &c
}

func grow(r [4]float64, by float64) [4]float64 {
	return [4]float64{r[0] - by, r[1] - by, r[2] + by, r[3] + by}
}

// meets reports whether the pixel at (x, y) and r share area.
func meets(r [4]float64, x, y int) bool {
	return float64(x+1) > r[0] && float64(x) < r[2] && float64(y+1) > r[1] && float64(y) < r[3]
}

// inside reports whether the pixel lies wholly inside the eroded region.
func (s *redactPixelSets) inside(x, y int) bool {
	e := grow(s.region, -redactRenderMargin)
	return float64(x) >= e[0] && float64(x+1) <= e[2] && float64(y) >= e[1] && float64(y+1) <= e[3]
}

// outside reports whether the outside check reads the pixel: clear of the
// dilated region and of every text run it meets.
func (s *redactPixelSets) outside(x, y int) bool {
	if meets(grow(s.region, redactRenderMargin), x, y) {
		return false
	}
	for _, e := range s.excluded {
		if meets(e, x, y) {
			return false
		}
	}
	return true
}

// countOutside compares before and after, RGB rasters w pixels wide, on the
// outside pixels: a pixel differs when a channel moves by more than tol and
// no pixel within radius of it in the other raster lies within tol of it. A
// differing pixel image blanking changed counts as spill, not as changed.
func (s *redactPixelSets) countOutside(before, after []uint8, w, tol, radius int) (read, changed int, box [4]int, spill int) {
	var diff pixelBox
	for y := range s.h {
		for x := range s.w {
			if !s.outside(x, y) {
				continue
			}
			read++
			i := 3 * (y*w + x)
			if channelDelta(before[i:i+3], after[i:i+3]) <= tol {
				continue
			}
			if nearWithin(before[i:i+3], after, w, s.h, x, y, tol, radius) && nearWithin(after[i:i+3], before, w, s.h, x, y, tol, radius) {
				continue
			}
			if s.spill != nil && s.spill[y*s.w+x] {
				spill++
				continue
			}
			diff.add(x, y)
		}
	}
	return read, diff.n, diff.rect(), spill
}

// nearWithin reports whether a pixel within radius of (x, y) in img lies
// within tol of px.
func nearWithin(px, img []uint8, w, h, x, y, tol, radius int) bool {
	for ny := max(0, y-radius); ny <= min(h-1, y+radius); ny++ {
		for nx := max(0, x-radius); nx <= min(w-1, x+radius); nx++ {
			j := 3 * (ny*w + nx)
			if channelDelta(px, img[j:j+3]) <= tol {
				return true
			}
		}
	}
	return false
}

// pixelBox counts pixels and bounds them.
type pixelBox struct {
	n              int
	x0, y0, x1, y1 int
}

func (b *pixelBox) add(x, y int) {
	if b.n == 0 {
		b.x0, b.y0, b.x1, b.y1 = x, y, x+1, y+1
	}
	b.n++
	b.x0, b.y0 = min(b.x0, x), min(b.y0, y)
	b.x1, b.y1 = max(b.x1, x+1), max(b.y1, y+1)
}

func (b *pixelBox) rect() [4]int {
	if b.n == 0 {
		return [4]int{}
	}
	return [4]int{b.x0, b.y0, b.x1, b.y1}
}
