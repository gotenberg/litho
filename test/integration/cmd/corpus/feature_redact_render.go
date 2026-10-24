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
	"slices"
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

// redactRenderFallbackTolerance is the largest difference, in 8-bit levels,
// a channel may show on litho's half of a fallback scenario's outside check
// where the after page's images paint over a flat part of the source page
// ([flatAround]), in place of [renderChannelTolerance]. The raster fallback
// promises its images hold what litho's renderer draws of the source
// ([pdf.WithRedactionRasterFallback]), and resampling them keeps a flat
// area's colour, so there litho's renderings before and after differ by the
// rounding [redactRenderSelfTolerance] bounds. The render tolerance holds
// two rasterizers' anti-aliasing apart, which litho against itself does not
// need: under it, an image drawn faded by an alpha of 0.8 over white moves
// no channel past 51 levels and passes. At an edge the render tolerance
// stays (see [redactPixelSets.countOutside]), and poppler's half keeps it
// everywhere, since poppler draws the source with its own rasterizer.
//
// # Corpus evidence
//
//   - litho transparency_2b with the opaque state of the fallback's images
//     removed (the fault b291dd6b fixed): the band and quarter scenarios
//     fail on litho's half with 3,120 and 528 faded pixels over flat areas,
//     where the render tolerance read 0.
//   - pdfjs issue12752, govdocs thread_200_200163, pdfcpu
//     FOSDEM14_HPC_devroom_14_GoCUDA page 2: 2 to 1,497 pixels per scenario
//     move past 16 levels at the edges of hairlines and glyphs the image
//     samples at a point, none over a flat area.
const redactRenderFallbackTolerance = redactRenderSelfTolerance

// redactRenderDarkLevel is the largest 8-bit channel value a pixel of litho's
// images-only rendering over black may take inside a region. A redaction
// blanks a picture's covered samples to opaque black (package mutations,
// blankedPicture: the darkest value of the image's own colour space) and a
// stencil's to unpainted, so over a black background a blanked image paints
// black or nothing. The level leaves room for a black that colour management
// does not map to sRGB 0: the CMYK black of a printer profile is a dark grey.
const redactRenderDarkLevel = 80

// redactRenderSpillSpread is how far, in device pixels at [renderDPI],
// poppler's half lets image spill reach past the pixels litho's images-only
// renderings show changed ([redactPixelSets.spreadSpill]). poppler
// interpolates an enlarged image half a sample off litho's centred reading,
// so its blend of a blank into the kept samples sits up to half a sample's
// width further out on one side: 8 pixels covers a sample 16 pixels wide,
// 16 points at 72 dpi, and stops short of a whole image drawn otherwise.
//
// # Corpus evidence
//
//   - litho blankkeys_ua1 p1-quarter and p1-band: poppler's blend of the
//     blank into ImA, a 16 by 16 image drawn 160 points wide, reaches 3 to 4
//     pixels past litho's (138 and 79 pixels); litho and PDFium 4.30.0
//     (pdfium 126.0.6462.0) draw the blend alike, to a level.
const redactRenderSpillSpread = 8

// redactFallbackSuffix names a fallback scenario after the scenario it
// re-runs: "p1-quarter+raster".
const redactFallbackSuffix = "+raster"

// redactAsDefault is what scenario returns for a fallback scenario that
// rasterised nothing after a default run that succeeded.
const redactAsDefault = "as-default"

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
	// metricInterpolationSpill counts the scenarios where pixels inside the
	// eroded region showed a blanked image's blank blended with the samples
	// it kept outside the region, by either renderer (see
	// [redactPixelSets.blankBlend]): an interpolated image (ISO 32000-1,
	// 8.9.5.3) smooths the samples on either side of the region's edge into
	// each other, so these are by design and do not fail the inside check.
	// The redaction's own pixel check, which the cell skips, refuses them.
	metricInterpolationSpill = "interpolation_spill"

	// The fallback scenarios' metrics (see [redactRenderFeature]):
	// fallback_scenarios_run counts those the checks ran on and
	// rasterised_units the units their reports rasterised, both coverage;
	// fallback_refused and fallback_incomplete count those the fallback
	// refused as well; fallback_outside_changed and fallback_inside_leaked
	// count those a check failed on, by either renderer; and
	// fallback_poppler_no_oracle counts those whose poppler half stands
	// aside, as poppler_no_oracle does. fallback_litho_reading counts those
	// where poppler drew a pixel outside the region changed that shows
	// litho's rendering of the source, which the rasterised image holds,
	// and fallback_resampled those where a renderer drew a pixel outside
	// changed to a blend of the source around it, which the image's
	// resampling makes.
	metricFallbackScenarios       = "fallback_scenarios_run"
	metricRasterisedUnits         = "rasterised_units"
	metricFallbackRefused         = "fallback_refused"
	metricFallbackIncomplete      = "fallback_incomplete"
	metricFallbackOutsideChanged  = "fallback_outside_changed"
	metricFallbackInsideLeaked    = "fallback_inside_leaked"
	metricFallbackPopplerNoOracle = "fallback_poppler_no_oracle"
	metricFallbackLithoReading    = "fallback_litho_reading"
	metricFallbackResampled       = "fallback_resampled"
	// metricFallbackStrictChanged counts the fallback scenarios whose litho
	// half found a pixel changed where the images paint over a flat area
	// that the render tolerance alone would have passed
	// ([redactRenderFallbackTolerance]):
	// an image that does not show litho's rendering of the source, as one
	// drawn faded does. Each also counts in fallback_outside_changed.
	metricFallbackStrictChanged = "fallback_strict_changed"
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
//     poppler sees the content litho's parser may miss. A pixel only an
//     image paints that shows the blank blended with the samples the image
//     kept outside the region ([redactPixelSets.blankBlend]) is an
//     interpolated image's smoothing, by design, and counts in
//     interpolation_spill, not as a leak: redaction keeps /Interpolate
//     (ISO 32000-1, 8.9.5.3), and the redaction's own pixel check, which
//     the cell skips, refuses such a page.
//
// A scenario passes when both checks pass for both renderers. A scenario
// whose poppler half could not run counts in poppler_unavailable and does
// not pass. One on a source page poppler is no oracle for ([popplerCheck]),
// or whose outside check poppler cannot answer because it draws the source
// page apart from litho, counts in poppler_no_oracle and stands on litho's
// half. A PDF matches when every scenario it ran passes.
//
// Each scenario runs again with [pdf.WithRedactionRasterFallback], its name
// suffixed with [redactFallbackSuffix], unless the default mode found it
// incomplete. The fallback draws as images at 300 dpi the form XObjects and
// shading paints the default mode refuses, and the forms whose images it
// would blank in place. A fallback scenario that rasterised nothing after a
// default that succeeded wrote what the default wrote, and is not checked
// again; one that refuses counts in fallback_refused or
// fallback_incomplete. The others run both checks, the
// outside check with two allowances where the images paint, since the
// option promises they show what litho's renderer draws, at their
// resolution: a pixel that shows litho's rendering of the source, and one
// whose value lies between those the source shows around it
// ([redactPixelSets.countOutside]). Where the images paint over a flat part
// of the source, litho's half reads the pixels under
// [redactRenderFallbackTolerance], not the render tolerance: litho compares
// its own rendering of the source with the images' copy of it, so an image
// that shows it otherwise, as one drawn faded, fails
// (fallback_strict_changed). image_spill does not apply: the
// images change the images-only rendering wherever they paint. Their
// outcomes count in the fallback_* metrics and rasterised_units, and a
// failure fails the PDF as any scenario's does.
//
// The inside check does not see a shape the alpha of a blanked image keeps:
// the black of the blank paints where the soft mask lets it, as a picture
// would. Package mutations' tests hold redaction to making the covered mask
// samples opaque.
//
// Cost: two renders of each affected page before, four after per scenario
// (litho's full, images-only over white and over black, poppler's), all at
// 72 dpi, on pages 1 and 2 only, and one container per corpus run; and for
// each fallback scenario a second redaction, which renders the units it
// rasterises at 300 dpi, and, when it rasterised any, the same renders
// again. The run prints its wall time per corpus on stderr.
//
// The PDFs are those the render goldens cover, the same one-in-sixteen
// sample on pdfjs and govdocs, and every PDF of a corpus whose golden
// directory holds no render index (pdfcpu). The goldens' fonts serve
// litho's renderings, so both renderers draw an unembedded font alike; a
// corpus without them draws such text as placeholder boxes, which changes
// nothing a before-and-after comparison sees, unless a removed glyph's box
// reaches past the glyph box the report gives.
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
//   - litho megafont_2b p1-quarter, multipage_2b p2-quarter: once blanking
//     kept /Interpolate, the 2 by 2 image drawn 200 points wide blends its
//     kept red and blue samples into the blank up to 15 pixels inside the
//     region by litho's rendering (876 pixels) and 21 by poppler's (1,399):
//     no green, the channel of the samples blanked, in either, and PDFium
//     4.30.0 draws litho's blend to a level. interpolation_spill.
//   - litho blankkeys_ua1 p1-band: poppler draws ImA's blend half a sample
//     lower than litho and PDFium, 88 pixels of a kept row's colour fading
//     into the blank inside the band: interpolation_spill.
//   - pdfjs bitmap-halftone-skip-grid-template2, bitmap-refine-tpgron,
//     issue17871_top_right: JBIG2 regions poppler garbles, which change in
//     poppler once redaction re-encodes them from litho's decode.
//   - govdocs thread_000_000372 p1-quarter: poppler's 1 changed pixel outside
//     the region is a decoder difference on a CMYK ICCBased JPEG blanked in a
//     copy, which stores litho's decode while poppler decodes the original.
//     litho's half sees 0. Accepted as a mismatch. The PDF has no render
//     golden, so it lies outside the cell's sample and the gate never runs
//     it: the pixel shows with every govdocs PDF in scope.
//   - litho, pdfcpu, pdfjs, govdocs: 70, 20, 20 and 6 fallback scenarios
//     checked, rasterising 112, 55, 20 and 17 units; the cell's wall time
//     went from about 51, 20, 46 and 65 seconds to 45, 24, 85 and 121
//     (4 workers, timed back to back on a machine at load 25 to 60).
//   - litho, pdfcpu, pdfjs, govdocs: 114 scenarios the default mode found
//     incomplete stayed incomplete with the fallback, every one of them.
//   - litho usedcs_2b: a shading painted over the whole page hid the
//     heading drawn before it, and the fallback drew its image again over
//     the hollow frames litho draws for the unembedded Helvetica only: the
//     glyphs poppler and the goldens' fonts draw showed through, 146
//     pixels outside the region by litho's rendering. The flattening now
//     reads such glyphs as their whole boxes.
//   - litho transparency_2b band: the images stood in for forms drawn at a
//     constant alpha of 0.8 and with the Screen blend mode, and were drawn
//     under those states, so their colours faded: 17 pixels by poppler,
//     663 by PDFium, none past the render tolerance by litho. They are now
//     drawn under an opaque state.
//   - pdfcpu FOSDEM14_HPC_devroom_14_GoCUDA page 2, VectorApple,
//     text_annotations, litho iccgroupalt_2b, formimgname_2b, pdfjs
//     issue2642, issue12752: up to 988 pixels per scenario that change
//     where the images paint show litho's rendering of the source, or a
//     blend of the source around them (formimgname_2b's checkerboard, whose
//     cell edges fall inside the image's samples, changes 566 pixels in
//     poppler and 1,408 in PDFium, 0 in litho).
//   - pdfjs issue12752 (three scenarios), issue17289 band, issue2642 page
//     2: 7, 7, 3, 2 and 1 pixels poppler draws changed that neither reading
//     accounts for, hairlines poppler draws fainter in the source than the
//     image holds them. PDFium 4.30.0 draws every one of those pixels the
//     same before and after, and changes no pixel outside the region of
//     issue12752: poppler-side, they stay mismatches.
//   - govdocs thread_200_200427 whole page: 2 pixels inside the region at
//     the hairline tip of the clip a blanked image is drawn through, which
//     poppler draws at 46 to 68 where litho draws 148 to 175 and PDFium 153
//     to 178: poppler-side, a mismatch.
//   - pdfcpu bookletTestA6L band: 60 pixels litho draws changed below a
//     watermark "1" in unembedded Helvetica at 112 points, whose
//     placeholder box reaches past the glyph box the report gives. The
//     glyph goes whole, so the change is the redaction's by design; the
//     exclusion box is the harness's shortfall, a mismatch until removed
//     glyphs are sized by what the renderer draws.
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
	redact func(ctx context.Context, w *pdf.Writer, region pdf.RedactRegion, opts ...pdf.RedactOption) (*pdf.RedactReport, error)

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
		// The redaction skips its own pixel check, which renders with litho
		// as this cell's litho half does: under it, a leak this cell exists
		// to find would come back as a refusal, which the cell counts but
		// does not gate. The cell is the independent judge of the engine,
		// with poppler as the second reading.
		redact: func(ctx context.Context, w *pdf.Writer, region pdf.RedactRegion, opts ...pdf.RedactOption) (*pdf.RedactReport, error) {
			return w.Redact(ctx, []pdf.RedactRegion{region}, append(opts, pdf.WithoutRedactionPixelCheck())...)
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
	// Fallback marks a scenario run with [pdf.WithRedactionRasterFallback]
	// after the default mode refused it; RasterisedUnits is what its report
	// counted ([pdf.RedactReport.RasterisedUnits]).
	Fallback        bool `json:"fallback,omitempty"`
	RasterisedUnits int  `json:"rasterised_units,omitempty"`
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
	// InterpolationSpill counts the pixels inside the eroded region that
	// show a blanked image's blank blended with its kept samples (see
	// [redactPixelSets.blankBlend]). They do not count in Inside.
	InterpolationSpill int `json:"interpolation_spill,omitempty"`
	// LithoReading counts, on a fallback scenario's poppler half, the pixels
	// outside the region poppler draws changed that show what litho drew of
	// the source page: the rasterised image holds litho's rendering, so
	// they are no change the redaction made (see [popplerCheck]).
	LithoReading int `json:"litho_reading,omitempty"`
	// Resampled counts, on a fallback scenario, the pixels outside the
	// region that changed to a value between those the source shows around
	// them: the rasterised image's samples, taken at its resolution and
	// resampled to the page's, blend the colours on either side of an edge
	// (see [redactPixelSets.countOutside]).
	Resampled int `json:"resampled,omitempty"`
	// StrictChanged counts, on a fallback scenario's litho half, the changed
	// pixels where the images paint that the render tolerance alone would
	// have passed (see [redactRenderFallbackTolerance]). They count in
	// Outside too.
	StrictChanged int `json:"strict_changed,omitempty"`
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

	run := &redactRun{src: src, staging: staging, in: in, idx: idx, opts: opts, before: make(map[int]*redactSource), staged: make(map[int]bool), res: res}
	// Room for every scenario and its fallback up front: the staged outcomes
	// point into the slice, which must not move.
	res.out.Scenarios = make([]redactRenderOutcome, 0, 2*len(redactScenarios))
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
		page := redactRunPage{pg: pg, text: text, si: si}
		def := &res.out.Scenarios[len(res.out.Scenarios)-1]
		// A scenario runs again with the raster fallback, which takes over
		// the form XObjects and shading paints a default refusal names, and
		// the forms whose images the default blanks in place. A fallback
		// that rasterised nothing after a default that succeeded wrote what
		// the default wrote, and is not checked twice. One the default
		// finds incomplete is not run again: the copy its scan found lies
		// outside what the fallback rasterises.
		if f.scenario(ctx, run, page, def, false) == "incomplete" {
			continue
		}
		res.out.Scenarios = append(res.out.Scenarios, redactRenderOutcome{Name: sc.name + redactFallbackSuffix, Page: sc.page, Rect: rect, Fallback: true})
		if f.scenario(ctx, run, page, &res.out.Scenarios[len(res.out.Scenarios)-1], def.Refused == "" && !def.Failed) == redactAsDefault {
			res.out.Scenarios = res.out.Scenarios[:len(res.out.Scenarios)-1]
		}
	}
	return res
}

// redactRun is what the scenarios of one PDF share: the source, where its
// copy and the outputs are staged, the options litho renders with, and the
// source pages already rendered and staged.
type redactRun struct {
	src     *pdf.Reader
	staging string
	in      string
	idx     int
	opts    []pdf.RenderOption
	before  map[int]*redactSource
	staged  map[int]bool
	res     *redactRenderPDF
}

// redactRunPage is the source page a scenario redacts: the page, its text,
// and the scenario's index in [redactScenarios].
type redactRunPage struct {
	pg   models.Page
	text []models.TextElement
	si   int
}

// scenario applies outcome's region, with the raster fallback when outcome
// is a fallback scenario, runs litho's half of the checks on the output and
// stages what poppler renders. It returns the refusal class, "" when the
// redaction did not refuse. With onlyRasterised, a redaction that rasterised
// nothing is not checked and returns [redactAsDefault].
func (f *redactRenderFeature) scenario(ctx context.Context, run *redactRun, page redactRunPage, outcome *redactRenderOutcome, onlyRasterised bool) string {
	var redactOpts []pdf.RedactOption
	tag := ""
	if outcome.Fallback {
		redactOpts = append(redactOpts, pdf.WithRedactionRasterFallback())
		tag = "r"
	}
	output, report, err := f.redactOnce(ctx, run.src, pdf.RedactRegion{Page: outcome.Page, Rect: outcome.Rect}, redactOpts...)
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
		return outcome.Refused
	}
	var removed [][4]float64
	if report != nil {
		removed = report.RemovedGlyphBoxes[outcome.Page]
		outcome.RasterisedUnits = report.RasterisedUnits
	}
	if onlyRasterised && outcome.RasterisedUnits == 0 {
		return redactAsDefault
	}
	after, err := pdf.Open(ctx, bytes.NewReader(output), int64(len(output)))
	if err != nil {
		outcome.Failed, outcome.Cause = true, fmt.Sprintf("output does not open: %v", err)
		return ""
	}
	geom := redactPageGeometry(page.pg)
	sets := newRedactPixelSets(geom, outcome.Rect, page.text, removed)
	check, images := lithoCheck(ctx, run.src, after, outcome.Page, sets, run.before, run.opts, outcome.Fallback)
	after.Close()
	outcome.Litho = check
	if check.Error != "" {
		return ""
	}

	root := fmt.Sprintf("%d.p%d", run.idx, outcome.Page)
	lithoBefore := filepath.Join(run.staging, root+".litho.png")
	if !run.staged[outcome.Page] {
		if err := writePNG(lithoBefore, run.before[outcome.Page].full, sets.w, sets.h); err != nil {
			outcome.PopplerSkipped = fmt.Sprintf("stage: %v", err)
			return ""
		}
		run.staged[outcome.Page] = true
		run.res.jobs = append(run.res.jobs, popplerJob{PDF: run.in, Page: outcome.Page, PNG: root})
	}
	afterRoot := fmt.Sprintf("%d.s%d%s", run.idx, page.si, tag)
	imagesPath := filepath.Join(run.staging, afterRoot+".images.png")
	if err := os.WriteFile(filepath.Join(run.staging, afterRoot+".pdf"), output, 0o600); err != nil { //nolint:gosec // staging is our temp dir, the name built from indices
		outcome.PopplerSkipped = fmt.Sprintf("stage: %v", err)
		return ""
	}
	if err := writePNG(imagesPath, images, sets.w, sets.h); err != nil {
		outcome.PopplerSkipped = fmt.Sprintf("stage: %v", err)
		return ""
	}
	run.res.jobs = append(run.res.jobs, popplerJob{PDF: afterRoot + ".pdf", Page: outcome.Page, PNG: afterRoot})
	run.res.staged = append(run.res.staged, redactRenderStaged{
		outcome: outcome, sets: sets, before: root, after: afterRoot, images: imagesPath, lithoBefore: lithoBefore,
	})
	return ""
}

// redactOnce applies one region to a Writer over src and writes the result,
// with the redaction's report.
func (f *redactRenderFeature) redactOnce(ctx context.Context, src *pdf.Reader, region pdf.RedactRegion, opts ...pdf.RedactOption) ([]byte, *pdf.RedactReport, error) {
	w := pdf.NewWriter(src)
	defer w.Close()
	report, err := f.redact(ctx, w, region, opts...)
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
func lithoCheck(ctx context.Context, src, after *pdf.Reader, page int, sets *redactPixelSets, before map[int]*redactSource, opts []pdf.RenderOption, fallback bool) (*redactRenderCheck, []uint8) {
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
	// The raster fallback's images change the images-only rendering
	// wherever they paint, so on a fallback scenario that rendering cannot
	// tell blanking apart from what the images took over. The images are
	// opaque: where an image paints, the after page's images drawn over
	// white and over black agree, and those pixels are the ones the
	// fallback may have resampled. Litho's half reads the pixels any image
	// paints, opaque or not: an image drawn translucent, which shows the
	// source otherwise than litho draws it, is one its stricter tolerance
	// exists to catch, and the opaque set leaves it out.
	if fallback {
		sets.imaged = sets.grown(func(i int) bool {
			return channelDelta(images[3*i:3*i+3], onBlack[3*i:3*i+3]) <= redactRenderSelfTolerance
		})
		white, black := []uint8{0xff, 0xff, 0xff}, []uint8{0, 0, 0}
		sets.painted = sets.grown(func(i int) bool {
			return channelDelta(images[3*i:3*i+3], white) > 0 || channelDelta(onBlack[3*i:3*i+3], black) > 0
		})
	} else {
		sets.spill = sets.grown(func(i int) bool {
			return channelDelta(orig.images[3*i:3*i+3], images[3*i:3*i+3]) > redactRenderSelfTolerance
		})
		sets.onBlack, sets.onBlackW, sets.onBlackH = onBlack, sets.w, sets.h
	}
	out := sets.countOutside(orig.full, full, sets.w, nil, sets.painted, redactRenderFallbackTolerance)
	check.OutsidePixels, check.Outside, check.OutsideBox, check.ImageSpill, check.Resampled = out.read, out.changed, out.box, out.spill, out.resampled
	check.StrictChanged = out.strict
	var leaks pixelBox
	for y := range sets.h {
		for x := range sets.w {
			if !sets.inside(x, y) {
				continue
			}
			check.InsidePixels++
			i := 3 * (y*sets.w + x)
			painted := channelDelta(full[i:i+3], images[i:i+3]) > redactRenderSelfTolerance
			if !painted && max(onBlack[i], onBlack[i+1], onBlack[i+2]) <= redactRenderDarkLevel {
				continue
			}
			// Only an image may show a blend: a glyph, fill or annotation
			// left in the region is a leak whatever it looks like.
			if !painted && sets.blankBlend(orig.images, images, sets.w, x, y, redactRenderSelfTolerance) {
				check.InterpolationSpill++
				continue
			}
			leaks.add(x, y)
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
		var reading []uint8
		if st.outcome.Fallback {
			reading = cropRGB(flattenOnWhite(lithoBefore, sets.w, sets.h), sets.w, w, h)
		}
		before, after := cropRGB(orig, bw, w, h), cropRGB(full, aw, w, h)
		if o.spill != nil {
			o.spill = o.spreadSpill(before, after)
		}
		out := o.countOutside(before, after, w, reading, o.imaged, renderChannelTolerance)
		check.OutsidePixels, check.Outside, check.OutsideBox, check.ImageSpill = out.read, out.changed, out.box, out.spill
		check.LithoReading, check.Resampled = out.reading, out.resampled
	}
	images := cropRGB(flattenOnWhite(imagesImg, sets.w, sets.h), sets.w, w, h)
	full = cropRGB(full, aw, w, h)
	orig = cropRGB(orig, bw, w, h)
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
			if withinRange(full[i:i+3], images, w, h, x, y, renderChannelTolerance) {
				continue
			}
			// poppler interpolates half a sample off litho's centred
			// reading (package imgpaint), so its blend of the blank and the
			// kept samples falls where litho's images-only rendering is
			// black, or the other way round.
			if o.blankBlend(orig, full, w, x, y, renderChannelTolerance) {
				check.InterpolationSpill++
				continue
			}
			leaks.add(x, y)
		}
	}
	check.Inside, check.InsideBox = leaks.n, leaks.rect()
	return check, ""
}

// withinRange reports whether each channel of px lies within tol of the
// range that channel spans over the pixels within [renderNeighbourhood] of
// (x, y) in img. Two rasterizers put an edge a pixel apart and anti-alias it
// with their own coverage, so an edge pixel of one takes any value between
// the two sides the other shows nearby; a mark the other has no trace of
// near that place lies outside that range. Between two rasterizers tol is
// [renderChannelTolerance].
func withinRange(px, img []uint8, w, h, x, y, tol int) bool {
	for c := range 3 {
		lo, hi := 255, 0
		for ny := max(0, y-renderNeighbourhood); ny <= min(h-1, y+renderNeighbourhood); ny++ {
			for nx := max(0, x-renderNeighbourhood); nx <= min(w-1, x+renderNeighbourhood); nx++ {
				v := int(img[3*(ny*w+nx)+c])
				lo, hi = min(lo, v), max(hi, v)
			}
		}
		if v := int(px[c]); v < lo-tol || v > hi+tol {
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

// Compare sums the scenarios' outcomes. A fallback scenario counts in the
// fallback_* metrics, and in render_failed, redaction_failed and
// poppler_unavailable with the rest: a failure is one whatever the mode.
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
		metricRenderFailed: 0, metricImageSpill: 0, metricInterpolationSpill: 0,
		metricFallbackScenarios: 0, metricRasterisedUnits: 0, metricFallbackRefused: 0, metricFallbackIncomplete: 0,
		metricFallbackOutsideChanged: 0, metricFallbackInsideLeaked: 0, metricFallbackPopplerNoOracle: 0, metricFallbackLithoReading: 0,
		metricFallbackResampled: 0, metricFallbackStrictChanged: 0,
	}
	checked, passed := 0, 0
	var mismatches []json.RawMessage
	for _, sc := range out.Scenarios {
		// key names a metric for sc's mode: the fallback's own key when
		// sc is a fallback scenario and the metric has one.
		key := func(name, fallback string) string {
			if sc.Fallback {
				return fallback
			}
			return name
		}
		switch {
		case sc.Refused == "refused":
			m[key(metricRedactRefused, metricFallbackRefused)]++
			continue
		case sc.Refused == "incomplete":
			m[key(metricRedactIncomplete, metricFallbackIncomplete)]++
			continue
		case sc.Failed:
			m[metricRedactFailed]++
		case sc.Litho == nil:
			continue
		case sc.Litho.Error != "":
			m[metricRenderFailed]++
		default:
			m[key(metricRedactScenarios, metricFallbackScenarios)]++
			m[metricRasterisedUnits] += sc.RasterisedUnits
			checked++
			outside := sc.Litho.Outside > 0
			inside := sc.Litho.Inside > 0
			spill := sc.Litho.ImageSpill > 0
			blend := sc.Litho.InterpolationSpill > 0
			resampled := sc.Litho.Resampled > 0
			if sc.Litho.StrictChanged > 0 {
				m[metricFallbackStrictChanged]++
			}
			// The per-renderer halves count the default mode alone; a
			// fallback failure names its renderer in the mismatch.
			if outside && !sc.Fallback {
				m[metricOutsideChangedLitho]++
			}
			if inside && !sc.Fallback {
				m[metricInsideLeakedLitho]++
			}
			switch {
			case sc.Poppler == nil && sc.NoOracle:
				m[key(metricPopplerNoOracle, metricFallbackPopplerNoOracle)]++
			case sc.Poppler == nil:
				m[metricPopplerUnavailable]++
			case sc.Poppler.Error != "":
				m[metricRenderFailed]++
			default:
				if sc.Poppler.OutsideNoOracle != "" {
					m[key(metricPopplerNoOracle, metricFallbackPopplerNoOracle)]++
				}
				if sc.Poppler.LithoReading > 0 {
					m[metricFallbackLithoReading]++
				}
				if sc.Poppler.Resampled > 0 {
					resampled = true
				}
				if sc.Poppler.ImageSpill > 0 {
					spill = true
				}
				if sc.Poppler.InterpolationSpill > 0 {
					blend = true
				}
				if sc.Poppler.Outside > 0 {
					if !sc.Fallback {
						m[metricOutsideChangedPoppler]++
					}
					outside = true
				}
				if sc.Poppler.Inside > 0 {
					if !sc.Fallback {
						m[metricInsideLeakedPoppler]++
					}
					inside = true
				}
			}
			if outside {
				m[key(metricOutsideChanged, metricFallbackOutsideChanged)]++
			}
			if inside {
				m[key(metricInsideLeaked, metricFallbackInsideLeaked)]++
			}
			if spill {
				m[metricImageSpill]++
			}
			if blend {
				m[metricInterpolationSpill]++
			}
			if resampled {
				m[metricFallbackResampled]++
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
	// imaged marks, on a fallback scenario, the pixels an image of the
	// after page paints, grown by [renderNeighbourhood]: those the outside
	// check lets show litho's reading of the source or its resampling.
	imaged []bool
	// painted marks, on a fallback scenario, the pixels any image of the
	// after page paints, opaque or translucent, grown by
	// [renderNeighbourhood]: those litho's half of the outside check reads
	// under [redactRenderFallbackTolerance], with the allowances imaged
	// grants poppler's.
	painted []bool
	// onBlack is, on a default scenario, litho's rendering of the after page
	// with its images alone over black, onBlackW by onBlackH pixels: what
	// the images paint, and nothing where they paint nothing. blankBlend
	// reads the colours the kept samples paint just outside the region from
	// it.
	onBlack            []uint8
	onBlackW, onBlackH int
}

// blankBlend reports whether the pixel at (x, y) inside the region, in one
// renderer's renderings before and after of the page, stride pixels a row,
// shows a blanked image's blank blended with the samples it kept outside the
// region. A redaction blanks the samples whose centres the region covers
// (package mutations, blankedPicture) and keeps /Interpolate (ISO 32000-1,
// 8.9.5.3, Table 89), and a renderer interpolating an enlarged image blends
// each sample into its neighbours up to a sample away: the kept samples
// beside the region's edge reach into it, fading into the blank. Such a pixel
// holds nothing the redaction removed. It is told by three things a sample
// left unblanked does not show together:
//
//   - The blank reached it: some channel is darker after than before by more
//     than [redactRenderSelfTolerance], since the blank is the darkest value
//     of the image's colour space, and an unblanked sample draws as before.
//   - Nothing lightened it: no channel is lighter after than before by more
//     than [redactRenderSelfTolerance]. A blend of the source's samples with
//     a darker blank is darker than the source wherever it differs.
//   - Its colour comes from outside the region: no channel exceeds, by more
//     than tol, what litho's images-only rendering over black ([onBlack])
//     shows at the nearest pixels clear of the dilated region in the four
//     directions of the pixel's row and column, where the kept samples paint.
//     The blend fades from them toward the blank as it goes in.
//
// tol is [redactRenderSelfTolerance] for litho's own renderings and
// [renderChannelTolerance] for poppler's, which draws the blend half a
// sample off litho's (package imgpaint).
//
// A blank lighter than the darkest value that still darkens the picture
// would pass for a blend; package mutations' tests hold blanking to the
// darkest value, as they hold it to making the covered mask samples opaque.
func (s *redactPixelSets) blankBlend(before, after []uint8, stride, x, y, tol int) bool {
	if s.onBlack == nil {
		return false
	}
	i := 3 * (y*stride + x)
	darker := false
	for c := range 3 {
		b, a := int(before[i+c]), int(after[i+c])
		if a > b+redactRenderSelfTolerance {
			return false
		}
		if b-a > redactRenderSelfTolerance {
			darker = true
		}
	}
	if !darker {
		return false
	}
	bound, ok := s.keptColours(x, y)
	if !ok {
		return false
	}
	for c := range 3 {
		if int(after[i+c]) > bound[c]+tol {
			return false
		}
	}
	return true
}

// keptColours returns, channel by channel, the most [onBlack] shows at the
// nearest pixels clear of the dilated region left of, right of, above and
// below (x, y), false when none of them lies on the page.
func (s *redactPixelSets) keptColours(x, y int) ([3]int, bool) {
	d := grow(s.region, redactRenderMargin)
	// A pixel is clear of d when x+1 <= d[0] or x >= d[2] (see [meets]).
	left, right := math.Floor(d[0]-1), math.Ceil(d[2])
	top, bottom := math.Floor(d[1]-1), math.Ceil(d[3])
	var bound [3]int
	found := false
	for _, p := range [4][2]float64{{left, float64(y)}, {right, float64(y)}, {float64(x), top}, {float64(x), bottom}} {
		if p[0] < 0 || p[1] < 0 || p[0] >= float64(s.onBlackW) || p[1] >= float64(s.onBlackH) {
			continue
		}
		j := 3 * (int(p[1])*s.onBlackW + int(p[0]))
		for c := range 3 {
			bound[c] = max(bound[c], int(s.onBlack[j+c]))
		}
		found = true
	}
	return bound, found
}

// spreadSpill returns the spill mask grown into the pixels, connected to it
// through each other, that another renderer draws darker after than before
// in some channel by more than [redactRenderSelfTolerance] and lighter in
// none: the blank of a blanked image, which that renderer blends into the
// kept samples elsewhere than litho does. poppler interpolates an enlarged
// image half a sample toward the origin of its sample grid (package
// imgpaint), so its blend reaches past the region by up to half a sample
// more than litho's on one side. The growth stops [redactRenderSpillSpread]
// pixels from the mask, so a page the other renderer draws darker all over
// after the redaction, as its own decode of an image the redaction
// re-encoded can, still fails the outside check.
func (s *redactPixelSets) spreadSpill(before, after []uint8) []bool {
	out := slices.Clone(s.spill)
	dist := make([]int, len(out))
	darker := func(i int) bool {
		dark := false
		for c := range 3 {
			b, a := int(before[3*i+c]), int(after[3*i+c])
			if a > b+redactRenderSelfTolerance {
				return false
			}
			if b-a > redactRenderSelfTolerance {
				dark = true
			}
		}
		return dark
	}
	var queue []int
	for i, v := range out {
		if v {
			queue = append(queue, i)
		}
	}
	for head := 0; head < len(queue); head++ {
		i := queue[head]
		if dist[i] == redactRenderSpillSpread {
			continue
		}
		x, y := i%s.w, i/s.w
		for ny := max(0, y-1); ny <= min(s.h-1, y+1); ny++ {
			for nx := max(0, x-1); nx <= min(s.w-1, x+1); nx++ {
				j := ny*s.w + nx
				if !out[j] && darker(j) {
					out[j], dist[j] = true, dist[i]+1
					queue = append(queue, j)
				}
			}
		}
	}
	return out
}

// grown returns, w pixels a row, the pixels marked holds, the index of a
// pixel in a raster w pixels wide, grown by [renderNeighbourhood].
func (s *redactPixelSets) grown(marked func(i int) bool) []bool {
	changed := make([]bool, s.w*s.h)
	for i := range changed {
		changed[i] = marked(i)
	}
	mask := make([]bool, s.w*s.h)
	for y := range s.h {
		for x := range s.w {
			if !changed[y*s.w+x] {
				continue
			}
			for ny := max(0, y-renderNeighbourhood); ny <= min(s.h-1, y+renderNeighbourhood); ny++ {
				for nx := max(0, x-renderNeighbourhood); nx <= min(s.w-1, x+renderNeighbourhood); nx++ {
					mask[ny*s.w+nx] = true
				}
			}
		}
	}
	return mask
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
	crop := func(mask []bool) []bool {
		if mask == nil || w == s.w {
			return mask
		}
		out := make([]bool, w*h)
		for y := range h {
			copy(out[y*w:(y+1)*w], mask[y*s.w:y*s.w+w])
		}
		return out
	}
	c.spill, c.imaged = crop(s.spill), crop(s.imaged)
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

// outsideCount is what the outside check found on one rendering.
type outsideCount struct {
	read, changed int
	box           [4]int
	// spill counts the differing pixels image blanking changed, reading
	// those whose after pixel shows the other reading of the source, and
	// resampled those a fallback's image blends. strict counts the changed
	// pixels where a fallback's images paint that the render tolerance
	// alone would have passed.
	spill, reading, resampled, strict int
}

// countOutside compares before and after, RGB rasters w pixels wide, on the
// outside pixels under the render cell's pixel rule: a pixel differs when a
// channel moves by more than [renderChannelTolerance] and no pixel within
// [renderNeighbourhood] of it in the other raster lies within the tolerance
// of it. A differing pixel image blanking changed counts as spill, not as
// changed.
//
// Where a fallback scenario's images paint, the pixels paints marks
// ([redactPixelSets.imaged] for poppler's half, [redactPixelSets.painted]
// for litho's), two more readings stand. The images hold litho's rendering
// of the source, so a differing pixel that reading, when not nil, litho's
// rendering of the source, accounts for under the same rule counts as
// reading, not as changed. And they hold it at their own resolution, which a renderer
// resamples to the page's: a sample straddling an edge holds a blend of both
// sides, an edge pixel no vector rendering has, of a value between them. A
// differing pixel whose value lies within the range the source spans around
// it ([withinRange]), in before or in reading, counts as resampled, not as
// changed. Elsewhere on the page nothing may change: a mark the redaction
// lost there, a hairline among them, is no image's.
//
// imagedTol is the channel tolerance where the images paint over a flat
// part of before ([flatAround]), both readings included:
// [renderChannelTolerance] when before is another rasterizer's rendering,
// [redactRenderFallbackTolerance] when before and after are both litho's.
// Resampling keeps a flat area's colour, so there an image that shows
// litho's rendering differs from it by rounding alone. At an edge the
// render tolerance stands: a renderer samples the image at a point, which
// can fall on a stroke thinner than the page's pixel that the vector
// rendering of the pixel spreads over its area, a value outside the range
// around it by up to the stroke's contrast. A changed pixel the render
// tolerance would have passed counts in strict as well.
func (s *redactPixelSets) countOutside(before, after []uint8, w int, reading []uint8, paints []bool, imagedTol int) outsideCount {
	var out outsideCount
	var diff pixelBox
	radius := renderNeighbourhood
	same := func(a, b []uint8, x, y, i, tol int) bool {
		return channelDelta(a[i:i+3], b[i:i+3]) <= tol ||
			nearWithin(a[i:i+3], b, w, s.h, x, y, tol, radius) && nearWithin(b[i:i+3], a, w, s.h, x, y, tol, radius)
	}
	// accounted reports whether an imaged pixel shows a reading of the
	// source, or its resampling, under tol, and which one.
	accounted := func(x, y, i, tol int) (isReading, ok bool) {
		switch {
		case same(before, after, x, y, i, tol):
			return false, true
		case reading != nil && same(reading, after, x, y, i, tol):
			return true, true
		}
		return false, withinRange(after[i:i+3], before, w, s.h, x, y, tol) ||
			reading != nil && withinRange(after[i:i+3], reading, w, s.h, x, y, tol)
	}
	for y := range s.h {
		for x := range s.w {
			if !s.outside(x, y) {
				continue
			}
			out.read++
			i := 3 * (y*w + x)
			imaged := paints != nil && paints[y*s.w+x]
			switch {
			case !imaged && same(before, after, x, y, i, renderChannelTolerance):
			case s.spill != nil && s.spill[y*s.w+x]:
				out.spill++
			case !imaged:
				diff.add(x, y)
			default:
				tol := renderChannelTolerance
				if flatAround(before, w, s.h, x, y) {
					tol = imagedTol
				}
				isReading, ok := accounted(x, y, i, tol)
				switch {
				case ok && isReading:
					out.reading++
				case ok:
					if !same(before, after, x, y, i, tol) {
						out.resampled++
					}
				default:
					diff.add(x, y)
					if _, loose := accounted(x, y, i, renderChannelTolerance); loose {
						out.strict++
					}
				}
			}
		}
	}
	out.changed, out.box = diff.n, diff.rect()
	return out
}

// flatAround reports whether every channel of img spans no more than
// [redactRenderSelfTolerance] over the pixels within [renderNeighbourhood]
// of (x, y): a part of the page no edge crosses.
func flatAround(img []uint8, w, h, x, y int) bool {
	for c := range 3 {
		lo, hi := 255, 0
		for ny := max(0, y-renderNeighbourhood); ny <= min(h-1, y+renderNeighbourhood); ny++ {
			for nx := max(0, x-renderNeighbourhood); nx <= min(w-1, x+renderNeighbourhood); nx++ {
				v := int(img[3*(ny*w+nx)+c])
				lo, hi = min(lo, v), max(hi, v)
			}
		}
		if hi-lo > redactRenderSelfTolerance {
			return false
		}
	}
	return true
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
