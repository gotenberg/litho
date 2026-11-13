package chart

import (
	"context"
	"math"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/detmath"
	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// maxLabelRunes caps the characters of one text of the chart laid out.
// Anti-DoS guard, not spec-mandated: titles and labels are a line or two,
// and a longer one is cut rather than shaped.
const maxLabelRunes = 2048

// minFontSize and maxFontSize bound the font sizes drawn, in points: the
// text stack refuses sizes outside (0, 1000], and Office allows 1 to 400.
const (
	minFontSize = 1
	maxFontSize = 400
)

// defaultFontSize is the size of a chart text whose style gives none
// drawable, in points.
const defaultFontSize = 10

// fontSize returns the size v of a chart text, defaultFontSize when v is
// under minFontSize or NaN.
func fontSize(v float64) float64 {
	if !atLeast(v, minFontSize) {
		return defaultFontSize
	}
	return v
}

// block is a laid-out text: its lines, unrotated size and style.
type block struct {
	lines []text.Line
	// w and h are the unrotated size of the lines.
	w, h  float64
	style TextStyle
	// lineH is the height of each line.
	lineH []float64
}

// rotated returns the block's bounding box size once rotated.
func (b *block) rotated() (float64, float64) {
	if b == nil {
		return 0, 0
	}
	a := b.style.Rotation * math.Pi / 180
	c, s := math.Abs(detmath.Cos(a)), math.Abs(detmath.Sin(a))
	return float64(b.w*c) + float64(b.h*s), float64(b.w*s) + float64(b.h*c)
}

// textStyle maps a chart text style to the text stack's.
func textStyle(st TextStyle) text.Style {
	size := fontSize(st.Size)
	size = min(size, maxFontSize)
	w := 400
	if st.Bold {
		w = 700
	}
	col := st.Color
	if col.A <= 0 {
		col = typeset.Black
	}
	return text.Style{Families: st.Families, Size: size, Weight: w, Italic: st.Italic, Color: col}
}

// measure lays s out in st, breaking lines no wider than maxW points (0
// for no limit). It returns nil for an empty text or one the text stack
// cannot lay out.
func (rd *renderer) measure(s string, st TextStyle, maxW float64) *block {
	s = strings.TrimRight(s, "\n")
	if strings.TrimSpace(s) == "" || rd.fonts == nil {
		return nil
	}
	if r := []rune(s); len(r) > maxLabelRunes {
		s = string(r[:maxLabelRunes])
	}
	if !numeric.Positive(maxW) {
		maxW = 1e4
	}
	ts := textStyle(st)
	lines, _, err := rd.fonts.Layout(rd.ctx, []text.Run{{Text: s, Style: ts}}, maxW, text.Options{Whitespace: text.Preserve})
	if err != nil || len(lines) == 0 {
		return nil
	}
	b := &block{lines: lines, style: st}
	b.style.Size = ts.Size
	for _, l := range lines {
		b.w = max(b.w, l.Width)
		h := lineHeight(ts.Size)
		if rd.officeLayout() {
			h = faceLineHeight(l, ts.Size)
		}
		b.lineH = append(b.lineH, h)
		b.h += h
	}
	return b
}

// chartLineHeight is the height of a line of chart text, in ems.
// LibreOffice's chart text shapes take 1.12 to 1.125 times the font size a
// line, whether the face is Carlito or Liberation Sans, though their
// ascent and descent sum to 1.22 and 1.117 ems.
const chartLineHeight = 1.12

// lineHeight returns the height a chart text line of size points takes.
func lineHeight(size float64) float64 {
	return size * chartLineHeight
}

// faceLineHeight returns the height Office gives a line of chart text: the
// ascent, descent and line gap of the faces on l, or lineHeight(size) for
// a line without metrics.
//
// # Corpus evidence
//
//   - probe, xlsx: Excel prints a 10 pt bold Calibri title's baseline
//     9.52 pt below the line's top, Calibri's ascent, and moves the plot
//     area 2.44 pt down from 10 pt tick labels to 14 pt ones, half of four
//     times Calibri's line of 1.2207 ems.
//   - litho/xlsx: Excel prints a 14 pt title's baseline 13.33 pt below the
//     same top, and puts the plot area 4.89 pt lower than under the
//     probe's 10 pt title.
func faceLineHeight(l text.Line, size float64) float64 {
	if h := l.Ascent + l.Descent + l.LineGap; numeric.Positive(h) {
		return h
	}
	return lineHeight(size)
}

// align is a horizontal alignment of a block's lines.
type align uint8

const (
	alignCenter align = iota
	alignLeft
	alignRight
)

// draw emits b with its unrotated box centred on (cx, cy), its lines
// aligned within the box by al, rotated about the centre by its style's
// rotation.
func (b *block) draw(cx, cy float64, al align) []typeset.Fragment {
	if b == nil {
		return nil
	}
	x0, y := cx-float64(b.w/2), cy-float64(b.h/2)
	var out []typeset.Fragment
	for i, l := range b.lines {
		dx := 0.0
		switch al {
		case alignCenter:
			dx = (b.w - l.Width) / 2
		case alignRight:
			dx = b.w - l.Width
		case alignLeft:
		}
		base := y + float64((b.lineH[i]-l.Ascent-l.Descent)/2) + l.Ascent
		for _, run := range l.Runs {
			t := *run
			t.Origin = typeset.Point{X: x0 + dx + run.Origin.X, Y: base}
			out = append(out, &t)
		}
		y += b.lineH[i]
	}
	if b.style.Rotation == 0 || len(out) == 0 {
		return out
	}
	return []typeset.Fragment{&typeset.Group{Transform: rotateAbout(b.style.Rotation, cx, cy), Content: out}}
}

// rotateAbout returns the transform that turns the page counter-clockwise
// by deg degrees about (cx, cy), in y-down coordinates.
func rotateAbout(deg, cx, cy float64) typeset.Matrix {
	a := deg * math.Pi / 180
	c, s := detmath.Cos(a), detmath.Sin(a)
	return typeset.Matrix{A: c, B: -s, C: s, D: c, E: cx - float64(cx*c) - float64(cy*s), F: cy + float64(cx*s) - float64(cy*c)}
}

// renderer holds one [Render] call's state.
type renderer struct {
	ctx   context.Context
	fonts *text.Fonts
	c     *Chart
	// page is the chart's rectangle.
	page typeset.Rect
	out  []typeset.Fragment
	// fixed is the plot area's rectangle without its axis labels when the
	// file fixes it, nil when the layout finds it.
	fixed *typeset.Rect
	// points indexes each series' point overrides by point index.
	points map[*Series]map[int]*Point
}
