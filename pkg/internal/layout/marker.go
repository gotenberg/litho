package layout

import (
	"math"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// counterNames names the predefined counter style (CSS Counter Styles 3,
// 6) of each [css.ListStyleType] that spells numbers. Immutable lookup
// table.
var counterNames = map[css.ListStyleType]string{
	css.Decimal: "decimal", css.DecimalLeadingZero: "decimal-leading-zero",
	css.LowerRoman: "lower-roman", css.UpperRoman: "upper-roman",
	css.LowerAlpha: "lower-alpha", css.UpperAlpha: "upper-alpha", css.LowerGreek: "lower-greek",
	css.Armenian: "armenian", css.Georgian: "georgian",
}

// predefinedCounters spells the predefined counter styles: a nil
// [css.CounterStyles] holds those alone.
var predefinedCounters *css.CounterStyles

// MarkerText returns the text of the default marker of a list item styled
// st and numbered n (CSS Lists 3, 3.1): its list-style-type string as
// written, or what markerText draws. A converter that generates a
// ::marker itself calls it for content: normal.
func MarkerText(st *css.Style, n int) string {
	if st.ListStyleType == css.ListString {
		return st.ListStyleString
	}
	return markerText(st.ListStyleType, n)
}

// CounterText spells n in the counter style t without a suffix, for
// counter() and counters() (CSS Lists 3, 4.8): the bullet of disc, circle
// and square, nothing for none, decimal for a value the style does not
// define (CSS Counter Styles 3, 3.1.1).
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 writes counter(c,
//     square) as U+25A0 BLACK SQUARE.
func CounterText(t css.ListStyleType, n int) string {
	n = css.ClampInteger(int64(n))
	switch t {
	case css.ListNone:
		return ""
	case css.Disc:
		return "•"
	case css.Circle:
		return "◦"
	case css.Square:
		// CSS Counter Styles 3, 6.3 gives square U+25AA; Chromium writes
		// U+25A0, and litho follows Chromium.
		return "■"
	case css.DisclosureOpen:
		return "▾"
	case css.DisclosureClosed:
		return "▸"
	case css.ListString, css.ListNamed:
		return strconv.Itoa(n)
	case css.Decimal, css.DecimalLeadingZero, css.LowerRoman, css.UpperRoman, css.LowerAlpha, css.UpperAlpha,
		css.LowerGreek, css.Armenian, css.Georgian:
	}
	return counter(t, n)
}

// markerText returns the text of a list item's marker numbered n in style t,
// suffix included (CSS Lists 3, 3.1): the bullet and a space, or the
// counter, a full stop and a space. A value a style does not define falls
// back to decimal (CSS Counter Styles 3, 3.1.1). ListNone and ListString,
// whose string the style holds, have no marker here.
func markerText(t css.ListStyleType, n int) string {
	n = css.ClampInteger(int64(n))
	switch t {
	case css.ListNone, css.ListString:
		return ""
	case css.Disc:
		return "• "
	case css.Circle:
		return "◦ "
	case css.Square:
		return "▪ "
	case css.DisclosureOpen:
		return "▾ "
	case css.DisclosureClosed:
		return "▸ "
	case css.Decimal, css.DecimalLeadingZero, css.LowerRoman, css.UpperRoman, css.LowerAlpha, css.UpperAlpha,
		css.LowerGreek, css.Armenian, css.Georgian, css.ListNamed:
	}
	return counter(t, n) + ". "
}

// markerAlt returns the text a reader hears for an image marker: the marker
// the style would draw, without its trailing space, a bullet for none.
func markerAlt(st *css.Style, n int) string {
	if s := strings.TrimSuffix(MarkerText(st, n), " "); s != "" {
		return s
	}
	return "•"
}

// counter spells n in the counter style t (CSS Counter Styles 3, 1.3),
// decimal for a style that spells no number.
func counter(t css.ListStyleType, n int) string {
	if name, ok := counterNames[t]; ok {
		return predefinedCounters.Counter(name, n)
	}
	return strconv.Itoa(n)
}

// markerBoxWidth returns the width of the marker box the converter's
// marker node m asks for: its style's Width when a length, as an Office
// converter sets it to the distance from the number position to the text
// indent (ECMA-376 Part 1, 17.9.6 lvlText and 17.9.24 ind; ODF 1.3 Part
// 3, 19.880 text:list-level-position-and-space-mode label-alignment). 0
// lays the marker out as CSS does, its end edge on the item's content
// edge.
func markerBoxWidth(m *Node) float64 {
	if m == nil || m.Style == nil || m.Style.Width.Kind != css.Points {
		return 0
	}
	if w := clamp(m.Style.Width.Value); w > 0 {
		return w
	}
	return 0
}

// markerX returns the left edge of the label w wide of the Office marker m
// of list item b. Its box is m.boxW wide outside the item at its start
// edge, ending on the content edge, and the label aligns on the box's
// start edge, the number position, by the marker's text-align, as
// LibreOffice aligns a list label on it (and Word a lvlJc): start puts
// the label's start there, end its end, centre its centre. A label wider
// than its room overlaps the content.
func markerX(b *box, m *markerBox, w float64) float64 {
	rtl := b.st.Direction == css.RTL
	anchor := b.contentX() - m.boxW
	if rtl {
		anchor = b.contentX() + b.w + m.boxW
	}
	end := false
	switch m.st.TextAlign {
	case css.AlignCenter:
		return anchor - float64(w/2)
	case css.AlignEnd:
		end = true
	case css.AlignLeft:
		end = rtl
	case css.AlignRight:
		end = !rtl
	case css.AlignStart, css.AlignAuto, css.AlignJustify, css.AlignJustifyAll:
	}
	if end != rtl {
		return anchor - w
	}
	return anchor
}

// markerEdges returns the left and right edges an outside marker of the
// list item b sits beside: under [Options.BrowserRules] its border box's,
// as the marker box lies outside the principal block box (CSS 2.1,
// 12.5.1) and Chromium puts it there; else its content box's, which an
// Office list indents its label from.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 (Gotenberg 9.4.1) ends a 20px "X" marker
//     at the left border edge of a list item whatever its padding-left
//     and border-left.
func (e *engine) markerEdges(b *box) (float64, float64) {
	if e.opts.BrowserRules {
		return b.x, b.x + b.borderW()
	}
	return b.contentX(), b.contentX() + b.w
}

// symbolType reports whether the list-style-type t draws as a symbol
// marker under [Options.BrowserRules]: disc, circle, square and the two
// disclosure triangles.
func symbolType(t css.ListStyleType) bool {
	switch t { //nolint:exhaustive // the other styles draw text
	case css.Disc, css.Circle, css.Square, css.DisclosureOpen, css.DisclosureClosed:
		return true
	}
	return false
}

// disclosureSize returns the side of the square a disclosure triangle of
// a marker in style st fills: 0.66 times its font size, cut to Blink's
// layout unit, a 64th of a pixel. CSS Counter Styles 3, 6.2 leaves the
// triangle's look to the user agent; this is Blink's.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 draws the
//     disclosure triangle of a list item in Liberation Serif at 20 and
//     100px in a square of 13 and 66 pixels whose bottom is on the
//     baseline, the outside marker ending 8 pixels before the item's
//     border edge, the inside one followed by 0.4em.
func (e *engine) disclosureSize(st *css.Style) float64 {
	px := e.fontSize(st) / css.Pt
	return float64(math.Floor(float64(px*0.66)*64) / 64 * css.Pt)
}

// disclosureOps returns the path of the disclosure triangle of style st in
// the square r, as Blink draws it: disclosure-open points down between
// 7 and 93 per cent of the square's height, disclosure-closed toward the
// inline end over 86 per cent of its width.
func disclosureOps(st *css.Style, r typeset.Rect) []typeset.PathOp {
	pts := [3][2]float64{{0, 0.07}, {0.5, 0.93}, {1, 0.07}}
	if st.ListStyleType == css.DisclosureClosed {
		pts = [3][2]float64{{0, 0}, {0.86, 0.5}, {0, 1}}
		if st.Direction == css.RTL {
			pts = [3][2]float64{{1, 0}, {0.14, 0.5}, {1, 1}}
		}
	}
	ops := make([]typeset.PathOp, 0, 4)
	for i, p := range pts {
		kind := typeset.LineTo
		if i == 0 {
			kind = typeset.MoveTo
		}
		ops = append(ops, typeset.PathOp{Kind: kind, P: [3]typeset.Point{{X: r.X + float64(p[0]*r.Width), Y: r.Y + float64(p[1]*r.Height)}}})
	}
	return append(ops, typeset.PathOp{Kind: typeset.ClosePath})
}

// symbolRect returns the rectangle, in flow coordinates, of the bullet an
// outside symbol marker of style mst of the list item b draws, for a font
// of the given ascent and a first line at baseline, beside the item's
// edges left and right ([engine.markerEdges]). CSS Lists 3 leaves
// the marker's look to the user agent; this is Blink's symbol marker,
// independent of the font's own bullet: with A the ascent rounded to whole
// pixels and o = A*2/3, a square of (o+1)/2 pixels, o + 7 pixels of gap
// and 1 pixel of side bearing from the edge, 3*(A-o)/2 pixels
// below the top of the ascent, in integer arithmetic. A disclosure
// triangle's square ([engine.disclosureSize]) sits on the baseline, 8
// pixels from the edge.
//
// # Corpus evidence
//
//   - probe, html: the Chromium of gotenberg/gotenberg:9.0.0 prints the
//     symbol marker of a list in Liberation Serif at 100px, A 89, as a 30
//     pixel bullet whose ink spans 66 to 37 pixels left of the content
//     edge, its top 45 pixels below the ascent's.
func (e *engine) symbolRect(b *box, mst *css.Style, ascent, baseline, left, right float64) typeset.Rect {
	if t := mst.ListStyleType; t == css.DisclosureOpen || t == css.DisclosureClosed {
		w := e.disclosureSize(mst)
		x := left - float64(8*css.Pt) - w
		if b.st.Direction == css.RTL {
			x = right + float64(8*css.Pt)
		}
		return typeset.Rect{X: x, Y: baseline - w, Width: w, Height: w}
	}
	a := int(math.Round(min(max(ascent/css.Pt, 0), maxSymbolAscent))) // fits int: bounded
	o := a * 2 / 3
	w := float64(float64((o+1)/2) * css.Pt)
	x := left - float64(float64(o+7+1-1)*css.Pt)
	if b.st.Direction == css.RTL {
		x = right + float64(float64(o+7+1-1)*css.Pt) - w
	}
	y := baseline - float64(float64(a)*css.Pt) + float64(float64(3*(a-o)/2)*css.Pt)
	return typeset.Rect{X: x, Y: y, Width: w, Height: w}
}

// maxSymbolAscent bounds the ascent, in pixels, a symbol marker is sized
// from. Anti-DoS guard, not spec-mandated: it keeps the integer arithmetic
// of [symbolRect] in range whatever the font size.
const maxSymbolAscent = 1 << 20

// markerRoom returns the room above its baseline the line being built
// keeps for an outside marker set on it, under [Options.BrowserRules]:
// when it is the first line of its inline formatting context and the
// first formatted line of a list item with an outside marker, the height
// above the baseline of the marker's own line, its strut or its picture
// ([engine.markerLine]). CSS Lists 3, 3.1 leaves the marker's effect on
// the line to the user agent; Chromium moves the item's content down so
// the marker's line fits above the baseline, and lets it hang below.
//
// # Corpus evidence
//
//   - wpt: css/CSS2/selectors/universal-selector-005 and
//     css/css-pseudo/marker-content-010 draw their references in
//     Chromium 156.
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 draws items of
//     20px Ahem text with line-height 1 54px tall under a 50px picture
//     marker, 36px under a 40px ::marker, 50px under a ::marker of
//     line-height 80px, 36px when the first line is a nested block's,
//     and 56px under a 40px ::marker at line-height 50px: the baseline
//     moves down to the marker line's ascent, the room under it does not
//     grow.
func (s *inlineState) markerRoom() (float64, bool) {
	e := s.e
	if !e.opts.BrowserRules || len(s.lines) > 0 {
		return 0, false
	}
	for c, depth := s.root, 0; c != nil && depth < maxTreeDepth; c, depth = c.parent, depth+1 {
		if c.outside != nil {
			above, _ := e.markerLine(c)
			return above, true
		}
		if !opensItemLine(c) {
			break
		}
	}
	return 0, false
}

// opensItemLine reports whether the first line of the block container c
// is the first line of its parent's flow, the one [markerBaseline] sets
// an outside marker on: c takes part in its parent's block formatting
// context ([inParentFlow]) and no in-flow block before it holds a line.
func opensItemLine(c *box) bool {
	if !inParentFlow(c) {
		return false
	}
	for _, k := range c.parent.kids {
		if k == c {
			return true
		}
		if k.inline || k.float != css.FloatNone || k.abs || k.kind != kindBlock || !k.laidOut {
			continue
		}
		if _, ok := firstBaseline(k); ok {
			return false
		}
	}
	return false
}
