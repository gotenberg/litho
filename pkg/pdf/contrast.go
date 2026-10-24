package pdf

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"

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
// sRGB as [Reader.RenderPage] converts it. The background is the page drawn
// without its text at 72 dpi, averaged over the pixels the run's glyph boxes
// cover and no other glyph's does; a run whose text marks none of them is
// not seen and not judged (see [Reader.ExtractUnseenText]). A run whose colour
// the content stream does not settle, painted with a pattern, transparency, a
// blend mode or a soft mask, or by a Type 3 glyph that may set its own colour,
// is counted in [models.ContrastReport.Undetermined] and not judged.
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
	if err := s.draw(ctx, r, pg, [4]float64{}, false); err != nil {
		return models.ContrastReport{}, sceneError(pageNum, err)
	}
	cache := &colorconv.Cache{}
	for _, run := range runs {
		st := s.stats(run)
		if st.area == 0 || st.changed == 0 {
			continue
		}
		ink, ok := r.textColour(ctx, s.paints[run[0]], pg.Dict, cache)
		if !ok {
			rep.Undetermined++
			continue
		}
		rep.Judged++
		if f, short := contrastFinding(s, run, st, ink, o.enhanced); short {
			rep.Failures = append(rep.Failures, f)
		}
	}
	return rep, nil
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

// textColour returns the sRGB colour, each channel in [0, 1], a glyph painted
// with p shows, or false when the content stream does not settle it: a
// pattern, a constant alpha below 1, a blend mode other than Normal (ISO
// 32000-1, 11.3.5), a soft mask, a Type 3 glyph, which may set its own colour
// (9.6.5), or a space that does not convert. Modes 1 and 5 stroke the glyphs
// without filling them, so their colour is the stroke's; every other mode
// that paints fills them (9.3.6, Table 106).
func (r *Reader) textColour(ctx context.Context, p graphics.TextPaint, pageDict ast.PdfDict, cache *colorconv.Cache) ([3]float64, bool) {
	c, alpha := p.Fill, p.FillAlpha
	if p.Mode == 1 || p.Mode == 5 {
		c, alpha = p.Stroke, p.StrokeAlpha
	}
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

// contrastFinding measures one run painted in ink against its background and
// reports it when it falls short of its required ratio.
func contrastFinding(s *textScene, run []int, st runStats, ink [3]float64, enhanced bool) (models.TextContrast, bool) {
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
	u := unseenOf(s, run, "")
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
