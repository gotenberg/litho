package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"math"
	"os"
	"os/exec"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/gotenberg/litho/pkg/models"
	"github.com/gotenberg/litho/pkg/pdf"
)

// recolorFamily is the family every colour of the recolor cell's output is
// written in: [pdf.CMYKColorTarget] converts to DeviceCMYK.
const recolorFamily = "DeviceCMYK"

// recolorInkLevel is how far, in 8-bit levels, a channel of a pixel must lie
// from white for the pixel to count as ink, the render cell's
// [renderChannelTolerance]: a mark a conversion keeps stays ink whatever its
// new colour, and the anti-aliased fringe of a glyph on white sits below it in
// both renderings alike.
const recolorInkLevel = renderChannelTolerance

// recolorDeltaE is the largest CIE 1976 colour difference (ΔE*ab, from the
// sRGB pixels litho renders) an ink pixel of the source may show against the
// output, within [renderNeighbourhood], and still count as the same colour.
//
// A conversion to a printing condition moves every colour outside the
// condition's gamut to its boundary, by design, and the sRGB primaries lie far
// outside a press's: the bar sits just above the largest move a correct
// relative colorimetric conversion through the built-in CMYK profile gives
// any sRGB colour, 71.7 for sRGB blue (TestRecolorDeltaE_Derivation), so a
// gamut mapping always passes, and the failures that are no gamut mapping, a
// channel read in the wrong order, an inverted tint or a colour left black,
// move most colours of a page by more and fail it. ΔE*ab is used over ΔE 2000
// because the bar is coarse and its arithmetic is plain.
//
// # Corpus evidence
//
//   - litho complex_2b.pdf, megafont_2b.pdf, multipage_2b.pdf,
//     transparency_2b.pdf: saturated sRGB blue covers 1% of a page at 52 to
//     71.7, which a bar of 50 failed.
//   - govdocs thread_000_000753.pdf: the same at 53.5.
const recolorDeltaE = 75

// recolorMaxPerMillion is the largest share of a page's pixels, in parts per
// million, the ink check or the colour check may fail for the page to pass:
// the render cell's 1% ([renderMaxDiffPerMillion]), for the anti-aliasing a
// colour change moves along an edge.
const recolorMaxPerMillion = renderMaxDiffPerMillion

// Metrics of the recolor feature.
const (
	// metricRecolorChecked counts the pages the checks ran on.
	metricRecolorChecked = "pages_checked"
	// metricRecolorRefused counts the PDFs [pdf.Writer.ConvertColors] refused
	// with [pdf.ErrColorConversionRefused], its documented answer for a
	// colour it cannot convert, or with [pdf.ErrOutputIntentConflict], its
	// answer for a kept output intent over another CMYK profile, which are
	// no failures.
	metricRecolorRefused = "recolor_refused"
	// metricRecolorFailed counts the PDFs whose conversion or write failed
	// with any other error.
	metricRecolorFailed = "recolor_failed"
	// metricOutOfTarget counts the pages of an output that paint in a colour
	// space other than the target or a kept spot colour over it.
	metricOutOfTarget = "out_of_target"
	// metricGroupOutOfTarget counts the pages whose transparency groups blend
	// in another space: an inventory, since a group's blending space is no
	// colour the page paints.
	metricGroupOutOfTarget = "group_out_of_target"
	// metricInkMoved counts the pages whose ink, what paints away from
	// white, appears or disappears between the source and the output.
	metricInkMoved = "ink_moved"
	// metricColourDrift counts the pages whose ink changed colour past
	// [recolorDeltaE].
	metricColourDrift = "colour_drift"
)

// recolorFeature converts each PDF to CMYK with [pdf.Writer.ConvertColors]
// and [pdf.CMYKColorTarget], the built-in CC0-derived printer profile of the
// CGATS TR 001 condition, keeping spot colours by default, and checks the
// output against what the conversion promises:
//
//   - it reopens in litho (reopen_failed);
//   - qpdf --check finds no warning or error class the input does not have
//     (qpdf_failed), when qpdf is on PATH;
//   - each of its first [renderMaxPages] pages paints only in DeviceCMYK or
//     in a spot colour, Separation or DeviceN, whose alternate space is
//     DeviceCMYK, by [pdf.Reader.ExtractColorSpaces] (out_of_target);
//   - each such page, rendered at [renderDPI] by litho before and after,
//     keeps its ink where it was (ink_moved) and its colours within
//     [recolorDeltaE] (colour_drift).
//
// A refusal counts in recolor_refused and skips the PDF; any other error
// counts in recolor_failed. A transparency group's blending space (ISO
// 32000-1, 11.6.6) in another family counts in group_out_of_target, an
// inventory, since the page paints no colour in it.
//
// The PDFs are those the render goldens cover, as for redact-render, whose
// fonts serve both renderings so an unembedded font draws alike.
//
// # Corpus evidence
//
//   - litho (215 files), pdfjs and govdocs (79 sampled files each): 199, 78
//     and 79 match; 14 litho files and no other are refused, 10 for a
//     construct the file holds on purpose (a colour space naming a resource,
//     33 colorants, a 3-bit or 2-channel JPEG 2000 image, an /Lzw or doubled
//     /Interpolate inline image, a shading function of the wrong output
//     count, an undecodable content stream) and 4 for a GTS_PDFX intent;
//     pdfjs print_protection.pdf does not open.
//   - litho defaultbox_2b.pdf, viewerboxes_2b.pdf: ink_moved on the pages
//     without a usable media box, which every full rewrite gives a size of
//     its own (see their known divergences), the conversion aside.
//   - litho iccbadcmyk_2b.pdf, pdfxnoprofile_2b.pdf, pdfxonly_2b.pdf,
//     twoprofile_1b.pdf: refused with pdf.ErrOutputIntentConflict. Each keeps
//     a GTS_PDFX intent over another CMYK profile, through which the renderer
//     reads the converted DeviceCMYK, 80.6 and 83.5 ΔE*ab from the source on
//     the last two; with pdf.WithReplacedOutputIntents all four match.
type recolorFeature struct {
	// convert runs the conversion; tests replace it to plant a defect.
	convert func(ctx context.Context, w *pdf.Writer) (*pdf.ColorReport, error)
	qpdf    string

	mu      sync.RWMutex
	sampled map[string]bool
	fonts   pdf.RenderOption
}

func newRecolorFeature() *recolorFeature {
	f := &recolorFeature{convert: func(ctx context.Context, w *pdf.Writer) (*pdf.ColorReport, error) {
		return w.ConvertColors(ctx, pdf.CMYKColorTarget())
	}}
	if p, err := exec.LookPath("qpdf"); err == nil {
		f.qpdf = p
	}
	return f
}

func (f *recolorFeature) Name() string         { return "recolor" }
func (f *recolorFeature) GoldenSource() string { return "litho-self" }

// OncePerPDF marks the feature as profile-independent.
func (f *recolorFeature) OncePerPDF() {}

// LoadGoldens reads which PDFs the render goldens cover and their fonts (see
// [loadRenderSample]).
func (f *recolorFeature) LoadGoldens(dir string) error {
	sampled, fonts, err := loadRenderSample(dir)
	if err != nil {
		return err
	}
	f.mu.Lock()
	f.sampled, f.fonts = sampled, fonts
	f.mu.Unlock()
	return nil
}

func (f *recolorFeature) HasGolden(fixtureID string) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.sampled == nil || f.sampled[baseFixtureID(fixtureID)]
}

// recolorOutput is the feature's per-PDF output.
type recolorOutput struct {
	// Error says why the PDF was not checked: it does not open, or the
	// conversion refused it (Refused) or failed (Failed).
	Error   string `json:"error,omitempty"`
	Refused bool   `json:"refused,omitempty"`
	Failed  bool   `json:"failed,omitempty"`
	// ReopenFailed and QPDF report the output-level checks; QPDF lists the
	// classes qpdf finds in the output and not the input.
	ReopenFailed bool              `json:"reopen_failed,omitempty"`
	QPDF         []string          `json:"qpdf,omitempty"`
	Report       *pdf.ColorReport  `json:"report,omitempty"`
	Pages        []recolorPageEval `json:"pages,omitempty"`
}

// recolorPageEval is one page's checks.
type recolorPageEval struct {
	Page int `json:"page"`
	// OutOfTarget and Groups describe the uses outside the target.
	OutOfTarget []string `json:"out_of_target,omitempty"`
	Groups      []string `json:"groups,omitempty"`
	Pixels      int      `json:"pixels,omitempty"`
	InkMoved    int      `json:"ink_moved,omitempty"`
	ColourDrift int      `json:"colour_drift,omitempty"`
	MaxDeltaE   float64  `json:"max_delta_e,omitempty"`
	// SizeChanged reports a rendering of another size than the source's.
	SizeChanged bool   `json:"size_changed,omitempty"`
	RenderError string `json:"render_error,omitempty"`
}

// failed reports whether the page fails a check.
func (p recolorPageEval) failed() bool {
	return len(p.OutOfTarget) > 0 || p.inkFailed() || p.driftFailed() || p.RenderError != ""
}

func (p recolorPageEval) inkFailed() bool {
	return p.SizeChanged || overShare(p.InkMoved, p.Pixels)
}

func (p recolorPageEval) driftFailed() bool {
	return overShare(p.ColourDrift, p.Pixels)
}

// overShare reports whether n of pixels passes [recolorMaxPerMillion].
func overShare(n, pixels int) bool {
	return pixels > 0 && int64(n)*1_000_000 > int64(recolorMaxPerMillion)*int64(pixels)
}

// Run converts the PDF and runs the checks.
func (f *recolorFeature) Run(ctx context.Context, pdfPath, _ string) (data []byte, err error) {
	// A panic past the library's own recover is a failed conversion, not a
	// crashed run.
	defer func() {
		if r := recover(); r != nil {
			data, err = json.Marshal(recolorOutput{Error: fmt.Sprintf("panic: %v", r), Failed: true})
		}
	}()
	return json.Marshal(f.run(ctx, pdfPath))
}

// run is Run without the panic guard.
func (f *recolorFeature) run(ctx context.Context, pdfPath string) recolorOutput {
	var out recolorOutput
	input, err := os.ReadFile(pdfPath) //nolint:gosec // pdfPath comes from the corpus testdata walk
	if err != nil {
		out.Error = fmt.Sprintf("read: %v", err)
		return out
	}
	src, err := pdf.Open(ctx, bytes.NewReader(input), int64(len(input)))
	if err != nil {
		out.Error = fmt.Sprintf("open: %v", err)
		return out
	}
	defer src.Close()

	output, report, err := f.convertOnce(ctx, src)
	if err != nil {
		out.Error = err.Error()
		if errors.Is(err, pdf.ErrColorConversionRefused) || errors.Is(err, pdf.ErrOutputIntentConflict) {
			out.Refused = true
		} else {
			out.Failed = true
		}
		return out
	}
	out.Report = report
	after, err := pdf.Open(ctx, bytes.NewReader(output), int64(len(output)))
	if err != nil {
		out.ReopenFailed = true
		out.Error = fmt.Sprintf("output does not open: %v", err)
		return out
	}
	defer after.Close()
	out.QPDF = f.newQPDFKinds(ctx, input, output)

	pages, err := after.PageCount(ctx)
	if err != nil {
		out.ReopenFailed = true
		out.Error = fmt.Sprintf("output page count: %v", err)
		return out
	}
	f.mu.RLock()
	fonts := f.fonts
	f.mu.RUnlock()
	opts := []pdf.RenderOption{pdf.WithRenderDPI(renderDPI)}
	if fonts != nil {
		opts = append(opts, fonts)
	}
	for page := 1; page <= min(pages, renderMaxPages); page++ {
		out.Pages = append(out.Pages, recolorCheckPage(ctx, src, after, page, opts))
	}
	return out
}

// convertOnce converts src on a Writer of its own and writes the result.
func (f *recolorFeature) convertOnce(ctx context.Context, src *pdf.Reader) ([]byte, *pdf.ColorReport, error) {
	w := pdf.NewWriter(src)
	defer w.Close()
	report, err := f.convert(ctx, w)
	if err != nil {
		return nil, nil, err
	}
	var buf bytes.Buffer
	if err := w.WriteTo(ctx, &buf); err != nil {
		return nil, nil, fmt.Errorf("writing: %w", err)
	}
	return buf.Bytes(), report, nil
}

// newQPDFKinds returns, sorted, the qpdf --check classes the output has and
// the input does not, nil when qpdf is absent or could not run.
func (f *recolorFeature) newQPDFKinds(ctx context.Context, input, output []byte) []string {
	if f.qpdf == "" {
		return nil
	}
	inKinds, err := qpdfCheck(ctx, f.qpdf, input, "")
	if err != nil {
		return nil
	}
	outKinds, err := qpdfCheck(ctx, f.qpdf, output, "")
	if err != nil {
		return nil
	}
	var fresh []string
	for k := range outKinds {
		if !inKinds[k] {
			fresh = append(fresh, k)
		}
	}
	sort.Strings(fresh)
	return fresh
}

// recolorCheckPage runs the colour space check and the two rendering checks
// on one page.
func recolorCheckPage(ctx context.Context, src, after *pdf.Reader, page int, opts []pdf.RenderOption) recolorPageEval {
	eval := recolorPageEval{Page: page}
	uses, err := after.ExtractColorSpaces(ctx, page)
	if err != nil {
		eval.RenderError = fmt.Sprintf("colour spaces: %v", err)
		return eval
	}
	// A name the source's resources already leave undefined selects no
	// colour space in either, so no conversion can put it in the target.
	undefined := map[string]bool{}
	if srcUses, err := src.ExtractColorSpaces(ctx, page); err == nil {
		for _, u := range srcUses {
			if u.Family == "" {
				undefined[undefinedKey(u)] = true
			}
		}
	}
	for _, u := range uses {
		if inRecolorTarget(u) || u.Family == "" && undefined[undefinedKey(u)] {
			continue
		}
		desc := fmt.Sprintf("%s %s %s %s #%d", u.Source, u.Operator, u.Name, spaceString(u), u.Object)
		if u.Source == "group" {
			eval.Groups = append(eval.Groups, desc)
			continue
		}
		eval.OutOfTarget = append(eval.OutOfTarget, desc)
	}

	before, err := src.RenderPage(ctx, page, opts...)
	if err != nil {
		// The source page does not render, so there is nothing to compare:
		// that is the render cell's to report.
		return eval
	}
	converted, err := after.RenderPage(ctx, page, opts...)
	if err != nil {
		eval.RenderError = fmt.Sprintf("render: %v", err)
		return eval
	}
	eval.Pixels, eval.InkMoved, eval.ColourDrift, eval.MaxDeltaE, eval.SizeChanged = compareRecolored(before, converted)
	return eval
}

// undefinedKey names a use of a colour space name its resources leave
// undefined: how it is selected, and the name. The object number is left
// out, since the full rewrite of the output renumbers objects.
func undefinedKey(u models.ColorSpaceUse) string {
	return u.Source + "|" + u.Operator + "|" + u.Name
}

// inRecolorTarget reports whether u paints in the target: DeviceCMYK, a spot
// colour over it, an Indexed space over it, or a Pattern space, whose
// shadings and tiles are reported as uses of their own (and whose
// underlying space, when it has one, must be in the target). A spot colour
// whose colorants are all None or All paints in no space of its own: None
// marks nothing and All marks every separation of the device (ISO 32000-1,
// 8.6.6.4), which a conversion keeps as they are.
func inRecolorTarget(u models.ColorSpaceUse) bool {
	switch u.Family {
	case recolorFamily:
		return true
	case "Separation", "DeviceN":
		if len(u.Colorants) > 0 && !slices.ContainsFunc(u.Colorants, func(c string) bool { return c != "None" && c != "All" }) {
			return true
		}
		return u.Alternate != nil && inRecolorTarget(*u.Alternate)
	case "Indexed":
		return u.Alternate != nil && inRecolorTarget(*u.Alternate)
	case "Pattern":
		return u.Alternate == nil || inRecolorTarget(*u.Alternate)
	}
	return false
}

// spaceString writes a space as family(alternate)[colorants].
func spaceString(u models.ColorSpaceUse) string {
	s := u.Family
	if s == "" {
		s = "(none)"
	}
	if u.Alternate != nil {
		s += "(" + spaceString(*u.Alternate) + ")"
	}
	if len(u.Colorants) > 0 {
		s += "[" + strings.Join(u.Colorants, ",") + "]"
	}
	return s
}

// compareRecolored compares the source's rendering a with the output's b:
// pixels is a's pixel count; moved counts the pixels that are ink in one and
// have no ink within [renderNeighbourhood] in the other, nor any pixel as
// close in colour as the ink level is to white; drift counts the ink pixels
// of a with no pixel of b within the neighbourhood closer than
// [recolorDeltaE], and maxDE is the largest of those smallest differences.
//
// The colour condition on moved keeps a pale area, a light gradient behind a
// slide's text, whose conversion moves it by a few levels across
// [recolorInkLevel] from counting as ink that appeared or vanished: such a
// pixel lies as close to its counterpart as a pixel just past the ink level
// lies to white, where a mark added or removed differs from what was there by
// far more.
//
// # Corpus evidence
//
//   - govdocs thread_000_000859.pdf: page 3's light blue background, 17 ΔE*ab
//     at most from the source, crosses the ink level on 70872 pixels, 18% of
//     the page.
func compareRecolored(a, b image.Image) (pixels, moved, drift int, maxDE float64, sizeChanged bool) {
	ab, bb := a.Bounds(), b.Bounds()
	if ab.Dx() != bb.Dx() || ab.Dy() != bb.Dy() {
		return ab.Dx() * ab.Dy(), 0, 0, 0, true
	}
	w, h := ab.Dx(), ab.Dy()
	la, ia := labPixels(a)
	lb, ib := labPixels(b)
	level := 1 - float64(recolorInkLevel)/255
	inkDE := deltaE76(srgbToLab([3]float64{1, 1, 1}), srgbToLab([3]float64{level, level, level}))
	r := renderNeighbourhood
	near := func(x, y int, fn func(i int) bool) bool {
		for dy := -r; dy <= r; dy++ {
			for dx := -r; dx <= r; dx++ {
				nx, ny := x+dx, y+dy
				if nx >= 0 && ny >= 0 && nx < w && ny < h && fn(ny*w+nx) {
					return true
				}
			}
		}
		return false
	}
	for y := range h {
		for x := range w {
			i := y*w + x
			if ia[i] && !near(x, y, func(j int) bool { return ib[j] || deltaE76(la[i], lb[j]) <= inkDE }) {
				moved++
			}
			if ib[i] && !near(x, y, func(j int) bool { return ia[j] || deltaE76(lb[i], la[j]) <= inkDE }) {
				moved++
			}
			if !ia[i] {
				continue
			}
			best := math.Inf(1)
			near(x, y, func(j int) bool {
				best = min(best, deltaE76(la[i], lb[j]))
				return best <= recolorDeltaE
			})
			if best > recolorDeltaE {
				drift++
				maxDE = max(maxDE, best)
			}
		}
	}
	return w * h, moved, drift, math.Round(maxDE*10) / 10, false
}

// labPixels returns the CIELAB colour of every pixel of img, row by row, and
// whether it is ink (see [recolorInkLevel]). A pixel not opaque is composed
// over white, the background litho renders over.
func labPixels(img image.Image) ([][3]float64, []bool) {
	b := img.Bounds()
	lab := make([][3]float64, 0, b.Dx()*b.Dy())
	ink := make([]bool, 0, b.Dx()*b.Dy())
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r16, g16, b16, a16 := img.At(x, y).RGBA()
			// Premultiplied over white: c + (1 - a).
			over := func(c uint32) float64 { return float64(c+(0xffff-a16)) / 0xffff }
			rgb := [3]float64{over(r16), over(g16), over(b16)}
			lab = append(lab, srgbToLab(rgb))
			isInk := false
			for _, c := range rgb {
				if 255-c*255 > recolorInkLevel {
					isInk = true
				}
			}
			ink = append(ink, isInk)
		}
	}
	return lab, ink
}

// srgbToLab converts an sRGB colour, components in [0, 1], to CIELAB under
// the D65 white of IEC 61966-2-1 (CIE 15:2004, 8.2.1).
func srgbToLab(rgb [3]float64) [3]float64 {
	var lin [3]float64
	for i, c := range rgb {
		if c <= 0.04045 {
			lin[i] = c / 12.92
		} else {
			lin[i] = math.Pow((c+0.055)/1.055, 2.4)
		}
	}
	// IEC 61966-2-1, the sRGB to XYZ matrix, normalised to the D65 white.
	x := (0.4124*lin[0] + 0.3576*lin[1] + 0.1805*lin[2]) / 0.95047
	y := 0.2126*lin[0] + 0.7152*lin[1] + 0.0722*lin[2]
	z := (0.0193*lin[0] + 0.1192*lin[1] + 0.9505*lin[2]) / 1.08883
	f := func(t float64) float64 {
		if t > 216.0/24389 {
			return math.Cbrt(t)
		}
		return (24389.0/27*t + 16) / 116
	}
	fx, fy, fz := f(x), f(y), f(z)
	return [3]float64{116*fy - 16, 500 * (fx - fy), 200 * (fy - fz)}
}

// deltaE76 is the CIE 1976 colour difference ΔE*ab.
func deltaE76(a, b [3]float64) float64 {
	return math.Sqrt((a[0]-b[0])*(a[0]-b[0]) + (a[1]-b[1])*(a[1]-b[1]) + (a[2]-b[2])*(a[2]-b[2]))
}

// Compare sums the checks of one PDF.
func (f *recolorFeature) Compare(fixtureID string, output []byte) Verdict {
	var out recolorOutput
	if err := json.Unmarshal(output, &out); err != nil {
		return Verdict{FixtureID: fixtureID, Reason: fmt.Sprintf("decode output: %v", err)}
	}
	m := map[string]int{
		metricRecolorChecked: 0, metricRecolorRefused: 0, metricRecolorFailed: 0, metricReopenFailed: 0,
		metricQPDFFailed: 0, metricOutOfTarget: 0, metricGroupOutOfTarget: 0, metricInkMoved: 0,
		metricColourDrift: 0, metricRenderFailed: 0,
	}
	switch {
	case out.Refused:
		m[metricRecolorRefused]++
		return Verdict{FixtureID: fixtureID, Skipped: true, Reason: out.Error, Metrics: m}
	case out.Failed:
		m[metricRecolorFailed]++
	case out.ReopenFailed:
		m[metricReopenFailed]++
	case out.Error != "":
		return Verdict{FixtureID: fixtureID, Skipped: true, Reason: out.Error, Metrics: m}
	}
	if len(out.QPDF) > 0 {
		m[metricQPDFFailed]++
	}
	failed := out.Failed || out.ReopenFailed || len(out.QPDF) > 0
	for _, p := range out.Pages {
		m[metricRecolorChecked]++
		if len(p.OutOfTarget) > 0 {
			m[metricOutOfTarget]++
		}
		if len(p.Groups) > 0 {
			m[metricGroupOutOfTarget]++
		}
		if p.inkFailed() {
			m[metricInkMoved]++
		}
		if p.driftFailed() {
			m[metricColourDrift]++
		}
		if p.RenderError != "" {
			m[metricRenderFailed]++
		}
		if p.failed() {
			failed = true
		}
	}
	v := Verdict{FixtureID: fixtureID, Match: !failed, Score: 1, Metrics: m}
	if failed {
		v.Score = 0
		v.Reason = "the converted output fails a check"
		if out.Error != "" {
			v.Reason = out.Error
		}
		if raw, err := json.Marshal(out); err == nil {
			v.Mismatches = []json.RawMessage{raw}
		}
	}
	return v
}
