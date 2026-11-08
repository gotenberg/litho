package layout

import (
	"context"
	"fmt"
	"math"
	"sort"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/text"
)

// Warning codes the engine reports.
const (
	codeMissingGlyph     = "missing-glyph"
	codeImage            = "image-failed"
	codeUnsupported      = "unsupported-css"
	codeClipped          = "monolithic-clipped"
	codeTooDeep          = "tree-too-deep"
	codeTooManyFloats    = "too-many-floats"
	codeTooManyLines     = "too-many-lines"
	codeTooManyGlyphs    = "too-many-glyphs"
	codeRelayoutBudget   = "relayout-budget"
	codeLink             = "link-unresolved"
	codeBackgroundTiles  = "background-tiles"
	codeGroupDepth       = "group-too-deep"
	codeFontSize         = "font-size-clamped"
	codeStructTooDeep    = "structure-too-deep"
	codeTooManyFragments = "too-many-fragments"
	codeTableTooLarge    = "table-too-large"
)

// eps absorbs the rounding of float sums when comparing positions.
const eps = 1e-6

// pendingWarning is a warning whose page is known once the layout ends:
// y is the flow position it lands at, negative for none.
type pendingWarning struct {
	code, msg string
	y         float64
}

// engine is the state of one [Layout] call. It is never shared between
// goroutines.
type engine struct {
	ctx   context.Context
	fonts *text.Fonts
	opts  Options
	// pageW and pageH are the page area's size.
	pageW, pageH float64
	maxPages     int
	err          error
	warnings     []pendingWarning
	warned       map[string]bool
	boxes        int
	relayouts    int
	// seenPage is the highest page index the layout reached, for the
	// per-page context check.
	seenPage int
	// noPaginate counts the monolithic layouts in progress: inside one,
	// nothing moves to a next page.
	noPaginate int
	images     map[string]*Picture
	metrics    map[*css.Style]text.Metrics
	spaces     map[*css.Style]float64
	missing    map[rune]bool
	root       *compose.StructElem
	rootBox    *box
	// pageFrags counts the fragments of every page as they are assembled.
	pageFrags []int
	// geom is where the pages cut the flow.
	geom geometry
	// rtlRoot reports a root laid out right to left, whose first page is
	// a left page.
	rtlRoot bool
	// flexPasses counts the layouts made to measure flex items, against
	// [maxFlexPasses].
	flexPasses int
	// marks are the page names and blank pages the forced breaks decided,
	// by page index (CSS Page 3, 4.1 and 4.2.3).
	marks []pageMark
	// clamp counts the lines of the line clamp container being laid out,
	// nil outside one (CSS Overflow 4, 6).
	clamp *clampState
	// leaders are the strings of the leaders laid out, by box.
	leaders map[*box]*leaderInfo
	// decimals caches the decimal separator of each language, for decimal
	// tab stops.
	decimals map[string]rune
	// foot is the footnotes' state, nil for a document without any.
	foot *footState
	// balancePasses counts the layouts balancing columns took, against
	// [maxBalanceLayouts].
	balancePasses int
	// anchorsFound are the anchors of the finished layout, which the
	// links resolve against.
	anchorsFound map[string]compose.Dest
	// cols is the fragmentation context of the column set being laid out,
	// nil outside columns: the page queries answer for its columns.
	cols *colCtx
}

// fail records the first error; the layout unwinds once it is set.
func (e *engine) fail(err error) {
	if e.err == nil {
		e.err = err
	}
}

// warn records a warning at flow position y, negative for none.
func (e *engine) warn(code, msg string, y float64) {
	e.warnings = append(e.warnings, pendingWarning{code: code, msg: msg, y: y})
}

// warnOnce records a warning the first time its code and message are met.
func (e *engine) warnOnce(code, msg string, y float64) {
	key := code + "\x00" + msg
	if e.warned[key] {
		return
	}
	e.warned[key] = true
	e.warn(code, msg, y)
}

// relayout reports whether one more layout pass fits the budget, counting
// it, and warns the first time it does not.
func (e *engine) relayout(y float64) bool {
	if e.relayouts >= maxRelayouts {
		e.warnOnce(codeRelayoutBudget, fmt.Sprintf("more than %d extra layout passes: later boxes keep their first layout", maxRelayouts), y)
		return false
	}
	e.relayouts++
	return true
}

// paginating reports whether placement honours page breaks.
func (e *engine) paginating() bool { return e.noPaginate == 0 }

// pageOf returns the 0-based page index of flow position y.
func (e *engine) pageOf(y float64) int {
	if e.cols != nil {
		return e.cols.of(y)
	}
	if y <= 0 {
		return 0
	}
	if g := &e.geom; g.varies() {
		for g.tops[len(g.tops)-1] <= y+eps && g.grow() {
		}
		if last := g.tops[len(g.tops)-1]; last <= y+eps {
			// Past the page cap: the pages beyond are as tall as the first.
			return len(g.boxes) + int(min((y+eps-last)/e.pageH, math.MaxInt32/2))
		}
		return sort.SearchFloat64s(g.tops, y+eps+1e-9) - 1
	}
	p := math.Floor((y + eps) / e.pageH)
	if p > float64(math.MaxInt32) {
		return math.MaxInt32
	}
	return int(p) // fits int: at most MaxInt32, checked above
}

// pageTop returns the flow position of the top of page p.
func (e *engine) pageTop(p int) float64 {
	if e.cols != nil {
		return e.cols.top(p)
	}
	if g := &e.geom; g.varies() {
		for p >= len(g.tops) && g.grow() {
		}
		if p < len(g.tops) {
			return g.tops[max(p, 0)]
		}
		last := len(g.tops) - 1
		return g.tops[last] + float64(p-last)*e.pageH
	}
	return float64(p) * e.pageH
}

// atPageTop reports whether y is the top of a page.
func (e *engine) atPageTop(y float64) bool {
	return math.Abs(y-e.pageTop(e.pageOf(y))) < 1e-4
}

// nextPage returns the top of the page after the one y lies on.
func (e *engine) nextPage(y float64) float64 { return e.pageTop(e.pageOf(y) + 1) }

// reach records that the layout reached flow position y, failing past the
// page cap and checking the context once per new page.
func (e *engine) reach(y float64) {
	if e.err != nil {
		return
	}
	if e.cols != nil {
		e.cols.reach(y)
		return
	}
	p := e.pageOf(y)
	if p >= e.maxPages {
		e.fail(fmt.Errorf("%w: more than %d pages", ErrTooManyPages, e.maxPages))
		return
	}
	if p > e.seenPage {
		e.seenPage = p
		if err := e.ctx.Err(); err != nil {
			e.fail(fmt.Errorf("laying out: %w", err))
		}
	}
}

// fit returns where a monolithic piece of height h whose top would be at y
// lands: y itself, or the top of the next page when the piece would
// straddle a page cut and starts below a page top (CSS Fragmentation 3,
// 4.1). A piece taller than a page lands at a page top and overflows onto
// the pages after it, sliced at each cut as Chromium paints monolithic
// overflow; the content after it follows its full height.
func (e *engine) fit(y, h float64) float64 {
	if !e.paginating() || h <= 0 {
		return y
	}
	if e.pageOf(y) != e.pageOf(y+h-metricSlack) && !e.atPageTop(y) {
		y = e.nextPage(y)
	}
	e.reach(y + h - metricSlack)
	return y
}

// clamp returns v bounded to [-maxLength, maxLength], 0 when it is not
// finite.
func clamp(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return min(max(v, -maxLength), maxLength)
}

// resolve returns the used value of a length or percentage d against ref,
// 0 for any other kind.
func resolve(d css.Dimension, ref float64) float64 {
	if d.Kind == css.Points {
		return clamp(d.Value)
	}
	if d.Kind == css.Percent {
		return clamp(d.Value * ref)
	}
	return 0
}

// colorOf converts a computed colour, reporting false for one that paints
// nothing.
func colorOf(c css.Color) (compose.Color, bool) {
	cl := func(v float64) float64 { return min(max(clamp(v), 0), 1) }
	out := compose.Color{R: cl(c.R), G: cl(c.G), B: cl(c.B), A: cl(c.A)}
	return out, out.A > 0
}

// fontSize returns the style's font size bounded to what the text stack
// lays out, 0 for text that draws nothing.
func (e *engine) fontSize(st *css.Style) float64 {
	s := clamp(st.FontSize)
	if s <= 0 {
		return 0
	}
	if s > 1000 {
		e.warnOnce(codeFontSize, "a font size above 1000 points was laid out at 1000 points", -1)
		return 1000
	}
	return s
}

// textStyle returns the text stack's style for st in language lang.
func (e *engine) textStyle(st *css.Style, lang string) text.Style {
	c, _ := colorOf(st.Color)
	spacing := func(d css.Dimension) float64 {
		if d.Kind != css.Points {
			return 0
		}
		return min(max(clamp(d.Value), -1000), 1000)
	}
	size := e.fontSize(st)
	if size <= 0 {
		size = 1
	}
	oblique := 0.0
	if st.FontStyle == css.FontOblique {
		oblique = min(max(st.FontObliqueAngle, -90), 90)
	}
	return text.Style{
		Families:          text.BrowserFamilies(st.FontFamilies),
		Weight:            st.FontWeight,
		Italic:            st.FontStyle == css.FontItalic || oblique != 0,
		Oblique:           oblique,
		Stretch:           min(max(st.FontStretch, 0), 1000),
		NoSyntheticBold:   st.NoSynthesizeWeight,
		NoSyntheticItalic: st.NoSynthesizeStyle,
		NoSpacingTrim:     st.NoSpacingTrim,
		Autospace:         autospace(st, size),
		AutospaceAlpha:    st.TextAutospace&css.AutospaceIdeographAlpha != 0,
		AutospaceNumeric:  st.TextAutospace&css.AutospaceIdeographNumeric != 0,
		Size:              size,
		Lang:              lang,
		Color:             c,
		LetterSpacing:     spacing(st.LetterSpacing),
		WordSpacing:       spacing(st.WordSpacing),
		Scale:             textScale(st.TextScale),
		NoKerning:         st.FontKerning == css.KerningNone,
	}
}

// textScale returns the horizontal scale of the text stack for an Office
// text scale v: 0 for the natural width, else v within the bounds of
// compose.Text.HScale.
func textScale(v float64) float64 {
	if v == 0 || v == 1 || math.IsNaN(v) {
		return 0
	}
	return min(max(v, compose.MinHScale), compose.MaxHScale)
}

// autospace returns the space text-autospace adds in st at size points:
// an eighth of an em, or Chromium's text-autospace-size (CSS Text 4, 8.2).
func autospace(st *css.Style, size float64) float64 {
	k := 0.125
	if st.TextAutospaceSize != 0 {
		k = min(max(st.TextAutospaceSize, -1), 1)
	}
	return k * size
}

// faceMetrics returns the vertical metrics of the first available font of
// st (CSS 2.1, 10.8.1), 0.8 and 0.2 em when the index has no face.
func (e *engine) faceMetrics(st *css.Style) text.Metrics {
	if m, ok := e.metrics[st]; ok {
		return m
	}
	size := e.fontSize(st)
	m := text.Metrics{Ascent: 0.8 * size, Descent: 0.2 * size, XHeight: size / 2}
	if fc := e.fonts.Match(e.textStyle(st, "")); fc != nil && size > 0 {
		m = fc.Metrics(size)
		if e.opts.PixelFontMetrics {
			m = roundMetrics(m)
		}
	}
	e.metrics[st] = m
	return m
}

// lineHeight returns the used line-height of st (CSS 2.1, 10.8.1): normal
// is the font's ascent, descent and line gap.
func (e *engine) lineHeight(st *css.Style) float64 {
	if st.LineHeight.Kind == css.Points {
		return max(clamp(st.LineHeight.Value), 0)
	}
	if st.LineHeight.Kind == css.Number {
		return max(clamp(st.LineHeight.Value)*e.fontSize(st), 0)
	}
	m := e.faceMetrics(st)
	return m.Ascent + m.Descent + max(m.LineGap, 0)
}

// spaceWidth returns the advance of U+0020 SPACE in st, for tab stops.
func (e *engine) spaceWidth(st *css.Style, lang string) float64 {
	if w, ok := e.spaces[st]; ok {
		return w
	}
	w := 0.25 * e.fontSize(st)
	if p, _, err := e.fonts.Shape(e.ctx, []text.ParagraphRun{{Text: " ", Style: e.textStyle(st, lang)}}, text.ParagraphOptions{}); err == nil && p.Len() == 1 {
		w = p.Advance(0)
	}
	e.spaces[st] = w
	return w
}

// picture loads the image a style names, once per URL, warning when it
// fails.
func (e *engine) picture(url string, y float64) *Picture {
	if url == "" {
		return nil
	}
	if p, ok := e.images[url]; ok {
		return p
	}
	var pic *Picture
	if e.opts.Image == nil {
		e.warn(codeImage, fmt.Sprintf("image %q left out: no image loader", url), y)
	} else {
		p, err := e.opts.Image(e.ctx, url)
		switch {
		case err != nil:
			e.warn(codeImage, fmt.Sprintf("image %q left out: %v", url, err), y)
		case p == nil || p.Source == nil && p.Vector == nil || !(p.Width > 0) || !(p.Height > 0) || math.IsInf(p.Width, 0) || math.IsInf(p.Height, 0):
			e.warn(codeImage, fmt.Sprintf("image %q left out: no picture or no intrinsic size", url), y)
		default:
			pic = p
		}
	}
	e.images[url] = pic
	return pic
}
