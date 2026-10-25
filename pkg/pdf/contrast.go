package pdf

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/render/colorconv"
	"github.com/gotenberg/litho/pkg/internal/render/page"
	"github.com/gotenberg/litho/pkg/internal/typography"
	"github.com/gotenberg/litho/pkg/models"
)

// The contrast ratios WCAG 2.x requires of text against its background:
// Success Criterion 1.4.3, Contrast (Minimum), level AA, 4.5:1 and 3:1 for
// large-scale text; Success Criterion 1.4.6, Contrast (Enhanced), level AAA,
// 7:1 and 4.5:1.
const (
	contrastMinimum       = 4.5
	contrastMinimumLarge  = 3
	contrastEnhanced      = 7
	contrastEnhancedLarge = 4.5
)

// Large-scale text in WCAG 2.x is at least 18 points, or 14 points and bold
// (Understanding Success Criterion 1.4.3, the definition of large scale).
const (
	largeTextPoints     = 18
	largeBoldTextPoints = 14
)

// backgroundSpread is the spread of relative luminance, between the 10th and
// the 90th percentile of the pixels under a run, past which
// [models.TextContrast.BackgroundVaries] reports a varied background. Not
// spec-mandated: a tenth of the luminance scale is what separates a flat
// tint, whose spread is anti-aliasing at most, from an image or a gradient.
const backgroundSpread = 0.1

// ContrastOption configures [Reader.CheckTextContrast].
// ContrastOption closures must be safe to call from any goroutine.
type ContrastOption func(*contrastOpts)

type contrastOpts struct {
	enhanced bool
}

// WithEnhancedContrast judges the ratios of WCAG 2.x Success Criterion 1.4.6,
// Contrast (Enhanced), level AAA: 7:1, and 4.5:1 for large-scale text, rather
// than those of 1.4.3, level AA.
func WithEnhancedContrast() ContrastOption {
	return func(o *contrastOpts) {
		o.enhanced = true
	}
}

// CheckTextContrast measures the contrast of the visible text of the given
// 1-based page against what the page shows under it, and reports the runs
// that fall short of WCAG 2.x Success Criterion 1.4.3, Contrast (Minimum):
// 4.5:1, and 3:1 for large-scale text, at least 18 points or 14 points and
// bold, the size read through the text matrix and the CTM. Pass
// [WithEnhancedContrast] for Success Criterion 1.4.6.
//
// The check is advisory. PDF/UA (ISO 14289-1 and -2) sets no contrast
// requirement and the Matterhorn Protocol defines no machine check for it, so
// no validation litho runs depends on it; the requirement reaches a PDF
// through EN 301 549, 10.1.4.3, and Section 508, E205.4, which apply WCAG to
// documents that are not web pages. WCAG exempts text that is part of a logo
// or purely decorative, which a measurement cannot tell, so a failure is a
// finding for a person to review. Remediation cannot repair one: a new colour
// changes how the page looks.
//
// The text's colour is the one the content stream paints it in, converted to
// sRGB and adjusted by the transfer function in force as [Reader.RenderPage]
// converts and adjusts it; text both filled and stroked is
// judged by whichever of its two colours contrasts more, so a white letter
// outlined in black is judged by its outline. A run is judged where its own
// ink marks the page, as [Reader.ExtractUnseenText] judges it seen: the page
// drawn at 72 dpi without the run differs there from the page drawn whole. The
// background is what the page shows there without the run, averaged: over the
// pixels the run's ink marks outside every other run's glyph boxes, or, when
// it marks none, over all it marks with the page drawn without it and no run
// it shares a pixel with, the page's other text under it included, so white
// text set over other, dark text is judged against it. A piece of text drawn
// several times over itself, a fake bold, a drop shadow or an outline pass,
// each copy the same characters at the same size under a quarter em from the
// others as default text extraction matches them, is judged once: against what
// the page shows under the whole stack drawn without every copy, by whichever
// colour of its copies contrasts most, as an outline counts, and reported by
// its first copy. A run whose own ink marks no pixel is not seen and not
// judged, nor is one past the bound of 16 such drawings a page. A run whose
// colour the content stream does not settle, or a stack one of whose copies'
// colour it does not, painted with a pattern, transparency, a blend mode or a
// soft mask, or by a Type 3 glyph that may set its own colour, is counted in
// [models.ContrastReport.Undetermined] and not judged, as is a run whose own
// ink the page shows in colours no mix of its colours with what lies under it
// makes: a blend mode, a soft mask or transparency painted over it recolours
// it, so the content stream's colour is not the one shown. Text painted over
// and drawn again later at its place in rendering mode 7, by a copy that
// shows through its clip, shows in the colours of what the clip lets
// through, and is counted there too.
//
// It returns [ErrInvalidPageNumber] for a page outside the document,
// [ErrFontMissing] when the page's text cannot be read, and
// [ErrRenderTooLarge] when the page cannot be drawn within
// [Limits.MaxRenderPixels] or its glyph boxes cover the page past 64 times.
// It is safe for concurrent use with every other read of the [Reader].
func (r *Reader) CheckTextContrast(ctx context.Context, pageNum int, opts ...ContrastOption) (_ models.ContrastReport, err error) {
	defer recoverToError(r.logger(), "Reader.CheckTextContrast", &err)
	var o contrastOpts
	for _, opt := range opts {
		opt(&o)
	}
	pg, err := r.resolvePage(ctx, pageNum)
	if err != nil {
		return models.ContrastReport{}, err
	}
	s, err := r.readScene(ctx, pg)
	if err != nil {
		if errors.Is(err, typography.ErrFontNotFound) {
			return models.ContrastReport{}, fmt.Errorf("page %d: %w", pageNum, ErrFontMissing)
		}
		return models.ContrastReport{}, fmt.Errorf("page %d: %w", pageNum, err)
	}
	var rep models.ContrastReport
	runs := contrastRuns(s)
	if len(runs) == 0 {
		return rep, nil
	}
	stacks, err := s.stackRuns(ctx, runs, nil)
	if err != nil {
		return models.ContrastReport{}, fmt.Errorf("page %d: %w", pageNum, err)
	}
	under := make([]contrastSamples, len(stacks))
	verdicts, err := s.judgeOwnInk(ctx, r, pg, [4]float64{}, stackGlyphs(stacks), ownInkOptions{under: func(k int, with, without []byte) { under[k].add(with, without) }})
	if err != nil {
		return models.ContrastReport{}, sceneError(pageNum, err)
	}
	throughClip, err := s.shownThroughClip(ctx, r, pg, stacks, verdicts)
	if err != nil {
		return models.ContrastReport{}, sceneError(pageNum, err)
	}
	conv := &inkConverter{cache: &colorconv.Cache{}}
	for n, stack := range stacks {
		if throughClip[n] {
			rep.Undetermined++
			continue
		}
		if verdicts[n] != inkSeen || len(under[n].lums) == 0 {
			continue
		}
		inks, ok := r.stackColours(ctx, s, runs, stack, pg.Dict, conv)
		if !ok || under[n].offInk(inks) {
			rep.Undetermined++
			continue
		}
		st := under[n].stats()
		ink := strongestInk(inks, st.background)
		rep.Judged++
		if f, short := contrastFinding(s, runs[stack.members[0]], stack.glyphs, st, ink, o.enhanced); short {
			rep.Failures = append(rep.Failures, f)
		}
	}
	return rep, nil
}

// shownThroughClip reports, parallel to stacks, the stacks whose own ink
// marks nothing (verdicts) but that are drawn again later at their place in
// rendering mode 7 by text that shows through its clip, judged as
// [Reader.ExtractUnseenText] judges it ([textScene.clipCopied]): a heading
// painted over and drawn again to clip an image to its letters. What shows
// there is the paint the clip lets through, in the image's colours, not the
// text's, and WCAG 2.x judges the colours shown (Success Criterion 1.4.3,
// Contrast (Minimum)), so such text is counted undetermined, as text painted
// with a pattern is. The clipping copies are drawn only on a page where one
// copies such a stack.
func (s *textScene) shownThroughClip(ctx context.Context, r *Reader, pg *document.Page, stacks []runStack, verdicts []inkVerdict) ([]bool, error) {
	out := make([]bool, len(stacks))
	var hidden [][]int
	var at []int
	for n, st := range stacks {
		if verdicts[n] == inkUnseen {
			hidden = append(hidden, st.glyphs)
			at = append(at, n)
		}
	}
	if len(hidden) == 0 {
		return out, nil
	}
	var clips [][]int
	for _, run := range unseenRuns(s) {
		if s.paints[run[0]].Mode == 7 && !s.elements[run[0]].IsClipped {
			clips = append(clips, run)
		}
	}
	if !slices.Contains(s.clipCopied(hidden, clips), true) {
		return out, nil
	}
	seen, err := s.judgeOwnInk(ctx, r, pg, [4]float64{}, clips, ownInkOptions{omitClip: true})
	if err != nil {
		return nil, err
	}
	var shown [][]int
	for k, v := range seen {
		if v == inkSeen {
			shown = append(shown, clips[k])
		}
	}
	for j, ok := range s.clipCopied(hidden, shown) {
		out[at[j]] = ok
	}
	return out, nil
}

// stackColours returns the colours every copy of stack shows
// ([Reader.textColours]), or false when the content stream does not settle
// one of them. A copy drawn under another shows round its edges, a border
// round the letters, and WCAG 2.x counts a border toward the contrast ("the
// border can add contrast and would be used in calculating the contrast
// between the letter and its background", Understanding Success Criterion
// 1.4.3, Contrast (Minimum)), so a stack is judged by whichever colour of
// its copies contrasts most with what lies under the whole stack
// ([strongestInk]), not by the copy drawn last alone: a light letter over its
// dark shadow reads by the shadow's edge.
func (r *Reader) stackColours(ctx context.Context, s *textScene, runs [][]int, stack runStack, pageDict ast.PdfDict, conv *inkConverter) ([][3]float64, bool) {
	var inks [][3]float64
	for _, k := range stack.members {
		c, ok := r.textColours(ctx, s.paints[runs[k][0]], pageDict, conv)
		if !ok {
			return nil, false
		}
		inks = append(inks, c...)
	}
	return inks, true
}

// contrastRuns groups a scene's glyphs into runs judged as one: consecutive
// glyphs on one line painted with the same colours and graphics state, in a
// mode that fills or strokes them, inside the crop box, the white space
// between words included and runs of white space alone left out.
func contrastRuns(s *textScene) [][]int {
	keep := func(i int) bool {
		return !invisibleMode(s.paints[i].Mode) && !s.elements[i].IsClipped
	}
	var runs [][]int
	for _, run := range sceneRuns(s.elements, keep) {
		start := 0
		for k := 1; k <= len(run); k++ {
			if k == len(run) || !samePaint(s.paints[run[start]], s.paints[run[k]]) || s.paints[run[start]].Bold != s.paints[run[k]].Bold {
				runs = appendTrimmed(runs, s, run[start:k])
				start = k
			}
		}
	}
	return runs
}

// samePaint reports two glyphs painted alike: the same mode, colours,
// transparency and transfer function.
func samePaint(a, b graphics.TextPaint) bool {
	return a.Mode == b.Mode && sameColour(a.Fill, b.Fill) && sameColour(a.Stroke, b.Stroke) &&
		a.FillAlpha == b.FillAlpha && a.StrokeAlpha == b.StrokeAlpha && a.BlendMode == b.BlendMode &&
		a.SoftMask == b.SoftMask && a.Type3 == b.Type3 && transferKey(a.Transfer) == transferKey(b.Transfer)
}

// transferKey names a transfer function as written, the empty string for
// none.
func transferKey(obj ast.PdfObject) string {
	if obj == nil {
		return ""
	}
	return fmt.Sprintf("%v", obj)
}

// sameColour reports two colours in one space with the same components.
func sameColour(a, b graphics.Color) bool {
	if a.Space.Family != b.Space.Family || a.Space.Resource != b.Space.Resource || a.Pattern != b.Pattern || len(a.Components) != len(b.Components) {
		return false
	}
	for i := range a.Components {
		if a.Components[i] != b.Components[i] {
			return false
		}
	}
	return true
}

// textColours returns the sRGB colours, each channel in [0, 1], a glyph
// painted with p shows, one for each paint its rendering mode applies, or
// false when the content stream does not settle one of them. Modes 1 and 5
// stroke the glyphs without filling them, so their colour is the stroke's;
// modes 2 and 6 fill and stroke them, and show both colours; every other
// mode that paints fills them (ISO 32000-1, 9.3.6, Table 106). In modes 2
// and 6 a paint whose constant alpha is 0 leaves no mark (11.6.4.4) and gives
// no colour, unless both do.
func (r *Reader) textColours(ctx context.Context, p graphics.TextPaint, pageDict ast.PdfDict, conv *inkConverter) ([][3]float64, bool) {
	type side struct {
		c     graphics.Color
		alpha float64
	}
	var sides []side
	switch p.Mode {
	case 1, 5:
		sides = []side{{p.Stroke, p.StrokeAlpha}}
	case 2, 6:
		if p.FillAlpha > 0 || p.StrokeAlpha <= 0 {
			sides = append(sides, side{p.Fill, p.FillAlpha})
		}
		if p.StrokeAlpha > 0 {
			sides = append(sides, side{p.Stroke, p.StrokeAlpha})
		}
	default:
		sides = []side{{p.Fill, p.FillAlpha}}
	}
	inks := make([][3]float64, 0, len(sides))
	for _, sd := range sides {
		ink, ok := r.textColour(ctx, p, sd.c, sd.alpha, pageDict, conv)
		if !ok {
			return nil, false
		}
		inks = append(inks, ink)
	}
	return inks, true
}

// strongestInk returns the colour of inks that contrasts most with
// background. A glyph filled in one colour and outlined in another shows
// both, and WCAG 2.x counts the outline toward the contrast: "When there is
// a border around the letter, the border can add contrast and would be used
// in calculating the contrast between the letter and its background"
// (Understanding Success Criterion 1.4.3, Contrast (Minimum)), so a white
// letter outlined in black is judged by its black outline.
//
// # Corpus evidence
//
//   - verapdf PDF_A-4__veraPDF_test_suite_6-2-4-3-t04-fail-l.pdf: orange
//     "HELLO WORLD" outlined in black on white, a failure at 1.7:1 by its
//     fill, now passes by its outline.
//   - pdfjs issue16759.pdf page 11: white text outlined in orange on a pale
//     band is judged by the orange, 1.9:1, rather than white, 1.3:1.
func strongestInk(inks [][3]float64, background [3]float64) [3]float64 {
	bg := relativeLuminance(background[0], background[1], background[2])
	best, bestRatio := inks[0], -1.0
	for _, ink := range inks {
		if ratio := contrastRatio(relativeLuminance(ink[0], ink[1], ink[2]), bg); ratio > bestRatio {
			best, bestRatio = ink, ratio
		}
	}
	return best
}

// textColour returns the sRGB colour, each channel in [0, 1], one paint of a
// glyph painted with p shows, the colour c at the constant alpha alpha, or
// false when the content stream does not settle it: a pattern, a constant
// alpha below 1, a blend mode other than Normal (ISO 32000-1, 11.3.5), a soft
// mask, a Type 3 glyph, which may set its own colour (9.6.5), or a space that
// does not convert. The transfer function in force adjusts the colour as it
// adjusts the page's drawing (10.5), so text a transfer inverts is judged in
// the colour it shows over the background the drawing shows.
//
// # Corpus evidence
//
//   - pdfjs issue20722.pdf: every page draws under an inverting /TR, so
//     white text reads black on the black the drawing shows; read through
//     the transfer, its 10,128 failures fall to 56, each in colours poppler
//     shows.
func (r *Reader) textColour(ctx context.Context, p graphics.TextPaint, c graphics.Color, alpha float64, pageDict ast.PdfDict, ic *inkConverter) ([3]float64, bool) {
	if p.Type3 || p.SoftMask || alpha < 1 || (p.BlendMode != "" && p.BlendMode != "Normal" && p.BlendMode != "Compatible") ||
		c.Space.Family == graphics.FamilyPattern || c.Pattern != "" {
		return [3]float64{}, false
	}
	conv, err := colorconv.For(ctx, r.doc, c.Space, p.Resources, colorconv.Options{Intent: p.Intent, Page: pageDict, Cache: ic.cache})
	if err != nil {
		r.logger().Debug("text contrast: colour left undetermined", slog.String("space", c.Space.Family), slog.Any("err", err))
		return [3]float64{}, false
	}
	red, green, blue := conv.RGB(c.Components)
	if t := r.transfer(ctx, ic, p.Transfer); t != nil {
		red, green, blue = t(red, green, blue)
	}
	return [3]float64{float64(red), float64(green), float64(blue)}, true
}

// maxContrastTransfers bounds the transfer functions [inkConverter] keeps
// compiled over one page. Anti-DoS guard, not spec-mandated: the same bound
// the renderer keeps (maxTransferCache in pkg/internal/render/page), since a
// real page sets a handful; past it a function is compiled again at each
// colour.
const maxContrastTransfers = 64

// inkConverter converts the colours of one page's text to the sRGB the page
// is drawn in, keeping the conversions and the transfer functions it built.
type inkConverter struct {
	cache     *colorconv.Cache
	transfers map[string]page.Transfer
}

// transfer returns the transfer function obj applies, nil for the identity.
// A function the renderer cannot apply it draws under the identity (see
// [page.CompileTransfer]), and so is it read here.
func (r *Reader) transfer(ctx context.Context, ic *inkConverter, obj ast.PdfObject) page.Transfer {
	if obj == nil {
		return nil
	}
	key := transferKey(obj)
	if t, ok := ic.transfers[key]; ok {
		return t
	}
	t, err := page.CompileTransfer(ctx, r.doc, obj)
	if err != nil {
		r.logger().Debug("text contrast: transfer function read as the identity", slog.Any("err", err))
		t = nil
	}
	if ic.transfers == nil {
		ic.transfers = make(map[string]page.Transfer)
	}
	if len(ic.transfers) < maxContrastTransfers {
		ic.transfers[key] = t
	}
	return t
}

// contrastSamples collects the colours the page shows under the own ink of
// one run, and those it shows there drawn whole ([ownInkOptions.under]).
type contrastSamples struct {
	sum  [3]float64
	lums []float64
	// pairs holds the pixels, six bytes each: the RGB of the page drawn
	// whole, then that of the page drawn without the run.
	pairs []byte
}

// add counts one pixel, with the page drawn whole and without the page
// drawn without the run, RGB in their three bytes.
func (c *contrastSamples) add(with, without []byte) {
	r, g, b := float64(without[0])/255, float64(without[1])/255, float64(without[2])/255
	c.sum[0] += r
	c.sum[1] += g
	c.sum[2] += b
	c.lums = append(c.lums, relativeLuminance(r, g, b))
	c.pairs = append(c.pairs, with[0], with[1], with[2], without[0], without[1], without[2])
}

// runStats is the background of one run: background is its mean colour,
// each channel in [0, 1], and lumLow and lumHigh are the
// [sceneLuminanceLow] and [sceneLuminanceHigh] percentiles of its relative
// luminance.
type runStats struct {
	background      [3]float64
	lumLow, lumHigh float64
}

// sceneLuminanceLow and sceneLuminanceHigh are the percentiles of a run's
// background luminance [runStats] reports: its spread without the tenth of
// its pixels at either end, where a shape's edge or an anti-aliased boundary
// lies. Not spec-mandated.
const (
	sceneLuminanceLow  = 0.1
	sceneLuminanceHigh = 0.9
)

// stats returns the background c sampled. It holds at least one sample.
func (c *contrastSamples) stats() runStats {
	n := float64(len(c.lums))
	st := runStats{background: [3]float64{c.sum[0] / n, c.sum[1] / n, c.sum[2] / n}}
	slices.Sort(c.lums)
	st.lumLow = c.lums[int(sceneLuminanceLow*(n-1))]
	st.lumHigh = c.lums[int(sceneLuminanceHigh*(n-1))]
	return st
}

// contrastFinding measures one run painted in ink against its background and
// reports it when it falls short of its required ratio. run is the first copy
// of the text, which names it and gives its size, and glyphs every glyph of
// its stack ([runStack]), whose boxes the report holds.
func contrastFinding(s *textScene, run, glyphs []int, st runStats, ink [3]float64, enhanced bool) (models.TextContrast, bool) {
	first := s.elements[run[0]]
	size := math.Abs(first.FontSize)
	bold := s.paints[run[0]].Bold
	large := size >= largeTextPoints || (bold && size >= largeBoldTextPoints)
	required := contrastMinimum
	switch {
	case enhanced && large:
		required = contrastEnhancedLarge
	case enhanced:
		required = contrastEnhanced
	case large:
		required = contrastMinimumLarge
	}
	ratio := contrastRatio(relativeLuminance(ink[0], ink[1], ink[2]),
		relativeLuminance(st.background[0], st.background[1], st.background[2]))
	if ratio >= required {
		return models.TextContrast{}, false
	}
	u := unseenOf(s, run, glyphs, "")
	return models.TextContrast{
		Text:             u.Text,
		BBox:             u.BBox,
		FontSize:         size,
		Bold:             bold,
		Large:            large,
		Ratio:            ratio,
		Required:         required,
		TextColor:        rgb8(ink),
		BackgroundColor:  rgb8(st.background),
		BackgroundVaries: st.lumHigh-st.lumLow > backgroundSpread,
	}, true
}

// contrastRatio is the WCAG 2.x contrast ratio of two relative luminances,
// (L1 + 0.05) / (L2 + 0.05) with L1 the lighter.
func contrastRatio(a, b float64) float64 {
	return (math.Max(a, b) + 0.05) / (math.Min(a, b) + 0.05)
}

// rgb8 rounds a colour, each channel in [0, 1], to 8 bits a channel.
func rgb8(c [3]float64) [3]uint8 {
	var out [3]uint8
	for i, v := range c {
		out[i] = uint8(math.Round(math.Max(0, math.Min(1, v)) * 255)) //nolint:gosec // G115: v is clamped to [0, 1], so the product fits in a byte.
	}
	return out
}

// inkSlack is how far, in the sRGB cube with each channel in [0, 1], a pixel
// of a run's own ink may lie from every mix of the run's colours with what
// the page shows under it and still read as such a mix. Not spec-mandated: it
// absorbs the 8-bit rounding of the drawing and a colour a conversion reads a
// few levels apart, while an inverting transfer or blend mode painted over
// the run moves its pixels across the whole cube.
const inkSlack = 0.06

// inkLuminanceSlack is how far, in relative luminance, a pixel of a run's own
// ink may lie outside the luminances of the run's colours and of what the
// page shows under it and still read as a mix of them. Not spec-mandated, as
// [inkSlack].
const inkLuminanceSlack = 0.03

// inkAlteredShare is the share of a run's own-ink pixels no mix explains
// beyond which [contrastSamples.offInk] finds the run's colours altered by
// what the page paints over them. Not spec-mandated: a stray pixel at a
// glyph's edge, where another mark's anti-aliasing meets it, stays below it.
const inkAlteredShare = 0.25

// maxHullInks bounds the distinct colours of one run whose mixes
// [contrastSamples.offInk] builds in the sRGB cube. Anti-DoS guard, not
// spec-mandated: a glyph shows its fill and its stroke and a stack a few
// copies, so three colours cover a fill and a stroke over a shadow; a run
// past it is tested on luminance alone.
const maxHullInks = 3

// offInk reports that the page shows the run's own ink in colours no mix of
// its colours with what lies under it makes: no pixel the run turns to one of
// its colours, within [inkSlack], shows it, and more than [inkAlteredShare]
// of its pixels lie both farther than [inkSlack] from every such mix in the
// sRGB cube and past [inkLuminanceSlack] outside the range of their relative
// luminances. A glyph's pixel is its colours over what lies under it, in
// shares its coverage sets (ISO 32000-1, 11.3.6, the Normal blend mode),
// unless something painted after it alters it: a later painting under a blend
// mode other than Normal, a soft mask or a constant alpha below 1 (11.3.5),
// or a transparency group holding such a painting, recolours the glyph and
// the page under it alike, so the colour the content stream sets is not the
// one the page shows. WCAG 2.x judges the colours shown (Success Criterion
// 1.4.3, Contrast (Minimum)), so such a run is not judged by the content
// stream's colour. Each test alone would find mixes where there are none: a
// page composited in sRGB mixes along the straight line between two colours,
// whose luminance dips below both when their hues differ, and a page
// composited in CMYK mixes along a curve off that line, whose luminance stays
// between theirs. A pixel showing the run's colour proves it shown: the edges
// of a later opaque mark, a ring drawn round a dot of a map symbol, mix the
// glyph with a colour neither it nor what lies under it holds, and leave its
// inside as the content stream paints it.
//
// # Corpus evidence
//
//   - All corpora: 73 runs of 2,122,995 judged turn undetermined, in 7
//     files.
//   - govdocs thread_400_400902.pdf: 31 runs of curved text set glyph by
//     glyph over a shadow copy in another colour.
//   - govdocs thread_000_000152.pdf page 38, thread_400_400625.pdf page 26:
//     map symbols built of several glyphs, 32 runs.
//   - govdocs thread_000_000162.pdf page 1: white text over a CMYK
//     composite mixes off the straight line, within the luminance test.
func (c *contrastSamples) offInk(inks [][3]float64) bool {
	n := len(c.pairs) / 6
	if n == 0 {
		return false
	}
	var distinct [][3]float64
	low, high := math.Inf(1), math.Inf(-1)
	for _, ink := range inks {
		l := relativeLuminance(ink[0], ink[1], ink[2])
		low, high = math.Min(low, l), math.Max(high, l)
		if !slices.ContainsFunc(distinct, func(d [3]float64) bool { return colourDistance(d, ink) <= 1.0/512 }) {
			distinct = append(distinct, ink)
		}
	}
	if len(distinct) > maxHullInks {
		distinct = nil
	}
	pts := make([][3]float64, 0, len(distinct)+1)
	off := 0
	for i := range n {
		px := c.pairs[6*i : 6*i+6]
		with := [3]float64{float64(px[0]) / 255, float64(px[1]) / 255, float64(px[2]) / 255}
		under := [3]float64{float64(px[3]) / 255, float64(px[4]) / 255, float64(px[5]) / 255}
		if slices.ContainsFunc(inks, func(ink [3]float64) bool {
			return colourDistance(with, ink) <= inkSlack && colourDistance(under, ink) > inkSlack
		}) {
			return false
		}
		if distinct != nil {
			pts = append(append(pts[:0], under), distinct...)
			if hullDistance(with, pts) <= inkSlack {
				continue
			}
		}
		lw, lu := relativeLuminance(with[0], with[1], with[2]), relativeLuminance(under[0], under[1], under[2])
		if lw < math.Min(low, lu)-inkLuminanceSlack || lw > math.Max(high, lu)+inkLuminanceSlack {
			off++
		}
	}
	return float64(off) > inkAlteredShare*float64(n)
}

// hullDistance returns how far p lies from the convex hull of pts, at most
// four points: from the nearest of the triangles, segments and points they
// make, which for three points or fewer is the hull itself, and for four
// leaves out the inside of the tetrahedron they span, which then counts as
// off the hull.
func hullDistance(p [3]float64, pts [][3]float64) float64 {
	best := math.Inf(1)
	for i := range pts {
		best = math.Min(best, colourDistance(p, pts[i]))
		for j := i + 1; j < len(pts); j++ {
			best = math.Min(best, segmentDistance(p, pts[i], pts[j]))
			for k := j + 1; k < len(pts); k++ {
				best = math.Min(best, triangleDistance(p, pts[i], pts[j], pts[k]))
			}
		}
	}
	return best
}

// colourDistance is the Euclidean distance of two colours.
func colourDistance(a, b [3]float64) float64 {
	d := sub3(a, b)
	return math.Sqrt(dot3(d, d))
}

// segmentDistance is the distance of p from the segment ab.
func segmentDistance(p, a, b [3]float64) float64 {
	ab := sub3(b, a)
	l := dot3(ab, ab)
	if l == 0 {
		return colourDistance(p, a)
	}
	t := math.Max(0, math.Min(1, dot3(sub3(p, a), ab)/l))
	return colourDistance(p, [3]float64{a[0] + t*ab[0], a[1] + t*ab[1], a[2] + t*ab[2]})
}

// triangleDistance is the distance of p from the triangle abc, inside and
// edges alike; a degenerate triangle is its edges.
func triangleDistance(p, a, b, c [3]float64) float64 {
	ab, ac, ap := sub3(b, a), sub3(c, a), sub3(p, a)
	d00, d01, d11 := dot3(ab, ab), dot3(ab, ac), dot3(ac, ac)
	den := d00*d11 - d01*d01
	edges := math.Min(segmentDistance(p, a, b), math.Min(segmentDistance(p, b, c), segmentDistance(p, a, c)))
	if den <= 1e-12 {
		return edges
	}
	d20, d21 := dot3(ap, ab), dot3(ap, ac)
	v := (d11*d20 - d01*d21) / den
	w := (d00*d21 - d01*d20) / den
	if v < 0 || w < 0 || v+w > 1 {
		return edges
	}
	return colourDistance(p, [3]float64{a[0] + v*ab[0] + w*ac[0], a[1] + v*ab[1] + w*ac[1], a[2] + v*ab[2] + w*ac[2]})
}

// sub3 is a - b.
func sub3(a, b [3]float64) [3]float64 {
	return [3]float64{a[0] - b[0], a[1] - b[1], a[2] - b[2]}
}

// dot3 is the dot product of a and b.
func dot3(a, b [3]float64) float64 {
	return a[0]*b[0] + a[1]*b[1] + a[2]*b[2]
}
