package pdf

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/render/colorconv"
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
// sRGB as [Reader.RenderPage] converts it; text both filled and stroked is
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
// [models.ContrastReport.Undetermined] and not judged.
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
	verdicts, err := s.judgeOwnInk(ctx, r, pg, [4]float64{}, stackGlyphs(stacks), ownInkOptions{under: func(k int, px []byte) { under[k].add(px) }})
	if err != nil {
		return models.ContrastReport{}, sceneError(pageNum, err)
	}
	cache := &colorconv.Cache{}
	for n, stack := range stacks {
		if verdicts[n] != inkSeen || len(under[n].lums) == 0 {
			continue
		}
		inks, ok := r.stackColours(ctx, s, runs, stack, pg.Dict, cache)
		if !ok {
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
func (r *Reader) stackColours(ctx context.Context, s *textScene, runs [][]int, stack runStack, pageDict ast.PdfDict, cache *colorconv.Cache) ([][3]float64, bool) {
	var inks [][3]float64
	for _, k := range stack.members {
		c, ok := r.textColours(ctx, s.paints[runs[k][0]], pageDict, cache)
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

// samePaint reports two glyphs painted alike: the same mode, colours and
// transparency.
func samePaint(a, b graphics.TextPaint) bool {
	return a.Mode == b.Mode && sameColour(a.Fill, b.Fill) && sameColour(a.Stroke, b.Stroke) &&
		a.FillAlpha == b.FillAlpha && a.StrokeAlpha == b.StrokeAlpha && a.BlendMode == b.BlendMode &&
		a.SoftMask == b.SoftMask && a.Type3 == b.Type3
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
func (r *Reader) textColours(ctx context.Context, p graphics.TextPaint, pageDict ast.PdfDict, cache *colorconv.Cache) ([][3]float64, bool) {
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
		ink, ok := r.textColour(ctx, p, sd.c, sd.alpha, pageDict, cache)
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
// does not convert.
func (r *Reader) textColour(ctx context.Context, p graphics.TextPaint, c graphics.Color, alpha float64, pageDict ast.PdfDict, cache *colorconv.Cache) ([3]float64, bool) {
	if p.Type3 || p.SoftMask || alpha < 1 || (p.BlendMode != "" && p.BlendMode != "Normal" && p.BlendMode != "Compatible") ||
		c.Space.Family == graphics.FamilyPattern || c.Pattern != "" {
		return [3]float64{}, false
	}
	conv, err := colorconv.For(ctx, r.doc, c.Space, p.Resources, colorconv.Options{Intent: p.Intent, Page: pageDict, Cache: cache})
	if err != nil {
		r.logger().Debug("text contrast: colour left undetermined", slog.String("space", c.Space.Family), slog.Any("err", err))
		return [3]float64{}, false
	}
	red, green, blue := conv.RGB(c.Components)
	return [3]float64{float64(red), float64(green), float64(blue)}, true
}

// contrastSamples collects the colours the page shows under the own ink of
// one run ([ownInkOptions.under]).
type contrastSamples struct {
	sum  [3]float64
	lums []float64
}

// add counts px, RGB in its three bytes.
func (c *contrastSamples) add(px []byte) {
	r, g, b := float64(px[0])/255, float64(px[1])/255, float64(px[2])/255
	c.sum[0] += r
	c.sum[1] += g
	c.sum[2] += b
	c.lums = append(c.lums, relativeLuminance(r, g, b))
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
