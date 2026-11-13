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
	n = min(max(n, -maxMarkerValue), maxMarkerValue)
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
	n = min(max(n, -maxMarkerValue), maxMarkerValue)
	switch t {
	case css.ListNone, css.ListString:
		return ""
	case css.Disc:
		return "• "
	case css.Circle:
		return "◦ "
	case css.Square:
		return "▪ "
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
		return anchor - w/2
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

// symbolRect returns the rectangle, in flow coordinates, of the bullet an
// outside disc, circle or square marker of the list item b draws, for a
// font of the given ascent and a first line at baseline. CSS Lists 3 leaves
// the marker's look to the user agent; this is Blink's symbol marker,
// independent of the font's own bullet: with A the ascent rounded to whole
// pixels and o = A*2/3, a square of (o+1)/2 pixels, o + 7 pixels of gap
// and 1 pixel of side bearing from the content edge, 3*(A-o)/2 pixels
// below the top of the ascent, in integer arithmetic.
//
// # Corpus evidence
//
//   - probe, html: the Chromium of gotenberg/gotenberg:9.0.0 prints the
//     symbol marker of a list in Liberation Serif at 100px, A 89, as a 30
//     pixel bullet whose ink spans 66 to 37 pixels left of the content
//     edge, its top 45 pixels below the ascent's.
func symbolRect(b *box, ascent, baseline float64) typeset.Rect {
	a := int(math.Round(min(max(ascent/css.Pt, 0), maxSymbolAscent))) // fits int: bounded
	o := a * 2 / 3
	w := float64((o+1)/2) * css.Pt
	x := b.contentX() - float64(o+7+1-1)*css.Pt
	if b.st.Direction == css.RTL {
		x = b.contentX() + b.w + float64(o+7+1-1)*css.Pt - w
	}
	y := baseline - float64(a)*css.Pt + float64(3*(a-o)/2)*css.Pt
	return typeset.Rect{X: x, Y: y, Width: w, Height: w}
}

// maxSymbolAscent bounds the ascent, in pixels, a symbol marker is sized
// from. Anti-DoS guard, not spec-mandated: it keeps the integer arithmetic
// of [symbolRect] in range whatever the font size.
const maxSymbolAscent = 1 << 20
