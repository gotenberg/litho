package layout

import (
	"context"
	"fmt"
	"math"
	"sort"

	gofont "github.com/go-text/typesetting/font"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// eps absorbs the rounding of float sums when comparing positions.
const eps = 1e-6

// pendingWarning is a warning whose page is known once the layout ends:
// y is the flow position it lands at, negative for none.
type pendingWarning struct {
	code   warn.Code
	effect warn.Effect
	msg    string
	y      float64
}

// engine is the state of one [Layout] call. It is never shared between
// goroutines.
type engine struct {
	// ctx is the context of the [Layout] call, which the engine does not
	// outlive.
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
	// ticks counts the boxes generated and laid out, for the context
	// check of [engine.tick].
	ticks int
	// seenPage is the highest page index the layout reached, for the
	// per-page context check.
	seenPage int
	// noPaginate counts the monolithic layouts in progress: inside one,
	// nothing moves to a next page.
	noPaginate int
	images     map[string]*Picture
	metrics    map[*css.Style]text.Metrics
	// fallbacks holds the font fallback rule of each style, under
	// LibreOffice's rule, nil before the first.
	fallbacks map[*css.Style]text.Fallback
	spaces    map[spaceKey]float64
	// hyphens caches the mark a hyphenated line ends with, by the style
	// and language of its soft hyphen.
	hyphens map[spaceKey]*hyphenMark
	// emMarks caches the emphasis mark of the text of a style and language.
	emMarks map[spaceKey]*emphasisMark
	// balance is the block whose lines text-wrap: balance narrows, while
	// it is laid out.
	balance balanceState
	missing map[rune]bool
	// firstLines holds the first formatted line of each inline formatting
	// context a ::first-line styles, by its block container, and cloneOf
	// the box each box of those lines stands for (firstline.go); nil
	// until one is built.
	firstLines map[*box]*firstLine
	cloneOf    map[*box]*box
	// outlines caches the parsed faces whose glyph outlines a text clip,
	// an underline that skips ink or an initial letter reads; letters the
	// styles of the initial letters, sized for their paragraphs
	// (initialletter.go).
	outlines map[*typeset.Face]*gofont.Face
	letters  map[*box]*css.Style

	root    *typeset.StructElem
	rootBox *box
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
	anchorsFound map[string]typeset.Dest
	// cols is the fragmentation context of the column set being laid out,
	// nil outside columns: the page queries answer for its columns.
	cols *colCtx
	// wraps are the rectangles the wrapped page-anchored boxes took in the
	// previous pass, which the root's formatting context starts with
	// (framewrap.go).
	wraps []wrapSeed
	// frozen holds, by node, where the previous pass placed the floats of
	// css.Office.WordAnchor it seeded: they land there again
	// (wordanchor.go).
	frozen map[*Node]wrapSeed
	// chains maps the links of the chains of linked boxes to what each
	// paints (chain.go).
	chains map[*Node]*chainLink
	// mem is the conversion's memory budget, nil for none; held is what
	// this engine charged it, released when the layout drops the engine.
	mem  *limits.Budget
	held int64
	// shared holds the styles of the boxes the engine makes of its own
	// and what it charged for the styles its boxes hold (share.go).
	shared sharedStyles
	// reshaped counts the inline contents shaped again after their
	// release (release.go).
	reshaped int
}

// charge charges the conversion's budget n bytes for what, failing the
// layout when the budget refuses them.
func (e *engine) charge(n int64, what string) {
	if err := e.mem.Charge(n, what); err != nil {
		e.fail(fmt.Errorf("laying out: %w", err))
		return
	}
	e.held += n
}

// release gives back what the engine charged: its boxes are garbage once
// the layout drops it.
func (e *engine) release() {
	if e == nil {
		return
	}
	e.mem.Release(e.held)
	e.held = 0
}

// fail records the first error; the layout unwinds once it is set.
func (e *engine) fail(err error) {
	if e.err == nil {
		e.err = err
	}
}

// shapeFailed settles an error of shaping the text a construct draws of
// its own, what (a list marker, a clamp's ellipsis, a tab leader): one
// that ends the conversion (see limits.Stops) fails the layout, any other
// leaves the text out with a warning at flow position y.
func (e *engine) shapeFailed(err error, what string, y float64) {
	if limits.Stops(err) {
		e.fail(fmt.Errorf("shaping %s: %w", what, err))
		return
	}
	e.warnOnce(warn.TextNotLaidOut, warn.Omitted, fmt.Sprintf("a %s is not drawn: %v", what, err), y)
}

// warn records a warning of the given effect at flow position y, negative
// for none.
func (e *engine) warn(code warn.Code, effect warn.Effect, msg string, y float64) {
	e.warnings = append(e.warnings, pendingWarning{code: code, effect: effect, msg: msg, y: y})
}

// warnOnce records a warning the first time its code, effect and message
// are met.
func (e *engine) warnOnce(code warn.Code, effect warn.Effect, msg string, y float64) {
	key := code.String() + "\x00" + effect.String() + "\x00" + msg
	if e.warned[key] {
		return
	}
	e.warned[key] = true
	e.warn(code, effect, msg, y)
}

// relayout reports whether one more layout pass fits the budget, counting
// it, and warns the first time it does not.
func (e *engine) relayout(y float64) bool {
	if e.relayouts >= maxRelayouts {
		e.warnOnce(warn.LimitReached, warn.Approximated, fmt.Sprintf("more than %d extra layout passes: later boxes keep their first layout", maxRelayouts), y)
		return false
	}
	e.relayouts++
	return true
}

// tick counts one box generated or laid out and checks the context every
// [ctxCheckEvery] of them.
func (e *engine) tick() {
	e.ticks++
	if e.ticks%ctxCheckEvery != 0 || e.err != nil {
		return
	}
	if err := e.ctx.Err(); err != nil {
		e.fail(fmt.Errorf("laying out: %w", err))
	}
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
		for g.tops[len(g.tops)-1] <= y+eps {
			if !g.grow() {
				break
			}
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
		for p >= len(g.tops) {
			if !g.grow() {
				break
			}
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
		e.fail(fmt.Errorf("%w: more than %d", limits.ErrTooManyPages, e.maxPages))
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

// resolve returns the used value of a length, a percentage or a
// calculation of both d against ref, 0 for any other kind.
func resolve(d css.Dimension, ref float64) float64 {
	switch d.Kind { //nolint:exhaustive // the other kinds have no length
	case css.Points:
		return clamp(d.Value)
	case css.Percent:
		return clamp(d.Value * ref)
	case css.Calc:
		return clamp(d.Resolve(clamp(ref)))
	}
	return 0
}

// colorOf converts a computed colour, reporting false for one that paints
// nothing.
func colorOf(c css.Color) (typeset.Color, bool) {
	cl := func(v float64) float64 { return min(max(clamp(v), 0), 1) }
	out := typeset.Color{R: cl(c.R), G: cl(c.G), B: cl(c.B), A: cl(c.A)}
	return out, out.A > 0
}

// fontSize returns the style's font size bounded to what the text stack
// lays out, 0 for text that draws nothing.
func (e *engine) fontSize(st *css.Style) float64 {
	s := clamp(st.FontSize)
	if s <= 0 {
		return 0
	}
	if s > maxFontSize {
		e.warnOnce(warn.LimitReached, warn.Approximated, fmt.Sprintf("a font size above %d points was laid out at %d points", maxFontSize, maxFontSize), -1)
		return maxFontSize
	}
	return s
}

// fallback returns the font fallback rule of a run of style st: the one of
// [Options.Fallback], which for LibreOffice's carries the generic family
// of st's own families, read once per style.
func (e *engine) fallback(st *css.Style) text.Fallback {
	if e.opts.Fallback != text.FallbackLibreOffice {
		return e.opts.Fallback
	}
	if fb, ok := e.fallbacks[st]; ok {
		return fb
	}
	if e.fallbacks == nil {
		e.fallbacks = make(map[*css.Style]text.Fallback)
	}
	fb := text.LibreOfficeFallback(st.FontFamilies)
	e.fallbacks[st] = fb
	return fb
}

// textStyle returns the text stack's style for st in language lang.
func (e *engine) textStyle(st *css.Style, lang string) text.Style {
	c, _ := colorOf(st.Color)
	spacing := func(d css.Dimension) float64 {
		if d.Kind != css.Points {
			return 0
		}
		return min(max(clamp(d.Value), -maxSpacing), maxSpacing)
	}
	size := e.fontSize(st)
	if size <= 0 {
		size = 1
	}
	oblique := 0.0
	if st.FontStyle == css.FontOblique {
		oblique = min(max(st.FontObliqueAngle, -90), 90)
	}
	ts := text.Style{
		Families:          text.BrowserFamilies(st.FontFamilies),
		Weight:            st.FontWeight,
		Italic:            st.FontStyle == css.FontItalic || oblique != 0,
		Oblique:           oblique,
		Stretch:           min(max(st.FontStretch, 0), maxFontStretch),
		NoSyntheticBold:   st.NoSynthesizeWeight,
		NoSyntheticItalic: st.NoSynthesizeStyle,
		NoSpacingTrim:     st.NoSpacingTrim,
		Autospace:         autospace(st, size),
		AutospaceAlpha:    st.TextAutospace&css.AutospaceIdeographAlpha != 0,
		AutospaceNumeric:  st.TextAutospace&css.AutospaceIdeographNumeric != 0,
		Size:              size,
		Lang:              lang,
		Fallback:          e.fallback(st),
		Color:             c,
		LetterSpacing:     spacing(st.LetterSpacing),
		WordSpacing:       spacing(st.WordSpacing),
		Scale:             textScale(st.TextScale),
		NoKerning:         st.FontKerning == css.KerningNone,
		WordMetrics:       st.WordRules > 0,
	}
	ts.Settings = e.fontSettings(st, &ts)
	return ts
}

// fontSettings returns the font settings of st for its text style ts,
// nil when every one is at its default, as for most text: the features
// of the font properties, font-size-adjust and font-language-override.
func (e *engine) fontSettings(st *css.Style, ts *text.Style) *text.FontSettings {
	features := e.fontFeatures(st, ts)
	a := st.FontSizeAdjust
	if features == nil && !a.Set && st.FontLanguageOverride == "" {
		return nil
	}
	fs := &text.FontSettings{Features: features, LanguageSystem: st.FontLanguageOverride}
	if a.Set {
		fs.SizeAdjust = text.SizeAdjust{Metric: text.AdjustMetric(a.Metric), Value: a.Value}
		if a.FromFont {
			fs.SizeAdjust.Value = e.fonts.Aspect(*ts, fs.SizeAdjust.Metric)
		}
	}
	return fs
}

// textScale returns the horizontal scale of the text stack for an Office
// text scale v: 0 for the natural width, else v within the bounds of
// typeset.Text.HScale.
func textScale(v float64) float64 {
	if v == 0 || v == 1 || math.IsNaN(v) {
		return 0
	}
	return min(max(v, typeset.MinHScale), typeset.MaxHScale)
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
	ts := e.textStyle(st, "")
	if fc := e.fonts.Match(ts); fc != nil && size > 0 {
		m = fc.MetricsFor(ts)
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

// spaceKey is what the advance of a space depends on: the style and the
// language, which can pick another face.
type spaceKey struct {
	st   *css.Style
	lang string
}

// spaceWidth returns the advance of U+0020 SPACE in st, for tab stops.
func (e *engine) spaceWidth(st *css.Style, lang string) float64 {
	k := spaceKey{st, lang}
	if w, ok := e.spaces[k]; ok {
		return w
	}
	w := 0.25 * e.fontSize(st)
	if p, _, err := e.fonts.Shape(e.ctx, []text.ParagraphRun{{Text: " ", Style: e.textStyle(st, lang)}}, text.ParagraphOptions{}); err == nil && p.Len() == 1 {
		w = p.Advance(0)
	}
	e.spaces[k] = w
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
		e.warn(warn.PictureUnreadable, warn.Omitted, fmt.Sprintf("image %q left out: no image loader", url), y)
	} else {
		p, err := e.opts.Image(e.ctx, url)
		switch {
		case err != nil:
			e.warn(warn.PictureUnreadable, warn.Omitted, fmt.Sprintf("image %q left out: %v", url, err), y)
		case !validPicture(p):
			e.warn(warn.PictureUnreadable, warn.Omitted, fmt.Sprintf("image %q left out: no picture or no intrinsic size", url), y)
		default:
			pic = p
		}
	}
	e.images[url] = pic
	return pic
}
