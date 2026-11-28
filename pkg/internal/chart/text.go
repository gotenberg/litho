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
	// shown, when not nil, is the part of the page that shows: a line
	// whose box does not reach it is not drawn.
	shown *typeset.Rect
	// hang, set with shown, is the advance of the spaces each line ends
	// with, which its box takes for the test against shown.
	hang []float64
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
	b := &block{lines: lines, style: st, shown: rd.shown}
	b.style.Size = ts.Size
	if rd.shown != nil {
		b.hang = make([]float64, len(lines))
		for i, l := range lines {
			b.hang[i] = rd.hangingAdvance(l, ts)
		}
	}
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

// hangingAdvance returns the advance of the spaces that end the line l,
// laid out in ts: the width they add between two letters. Excel counts
// them in the box of a line it tests against a page's edge
// ([RenderPart]).
//
// # Corpus evidence
//
//   - libreoffice/xlsx: chart2/external_str_ref.xlsx's 18 pt bold title
//     wraps after "9A ", and Excel writes that line, its space included,
//     on page 2, where the line ends 3.7 pt inside the clip with the
//     space and would end 0.3 pt short of it without.
func (rd *renderer) hangingAdvance(l text.Line, ts text.Style) float64 {
	var spaces []rune
	for i := len(l.Runs) - 1; i >= 0; i-- {
		gs := l.Runs[i].Glyphs
		j := len(gs) - 1
		for ; j >= 0; j-- {
			t := gs[j].Text
			if t == "" {
				continue
			}
			if strings.TrimSpace(t) != "" {
				break
			}
			spaces = append(spaces, []rune(t)...)
		}
		if j >= 0 {
			break
		}
	}
	if len(spaces) == 0 {
		return 0
	}
	width := func(s string) float64 {
		ls, _, err := rd.fonts.Layout(rd.ctx, []text.Run{{Text: s, Style: ts}}, 1e4, text.Options{Whitespace: text.Preserve})
		if err != nil || len(ls) != 1 {
			return 0
		}
		return ls[0].Width
	}
	return math.Max(0, width("x"+string(spaces)+"x")-width("xx"))
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
	var turn typeset.Matrix
	if b.style.Rotation != 0 {
		turn = rotateAbout(b.style.Rotation, cx, cy)
	}
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
		box := typeset.Rect{X: x0 + dx, Y: y, Width: l.Width, Height: b.lineH[i]}
		if i < len(b.hang) {
			box.Width += b.hang[i]
		}
		if b.shown != nil && !reaches(box, turn, *b.shown) {
			y += b.lineH[i]
			continue
		}
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
	return []typeset.Fragment{&typeset.Group{Transform: turn, Content: out}}
}

// reaches reports whether the box of a line, turned by m when m is not
// the zero matrix, overlaps shown: the bounding box of its turned corners
// and shown share some area.
func reaches(box typeset.Rect, m typeset.Matrix, shown typeset.Rect) bool {
	x0, y0, x1, y1 := box.X, box.Y, box.X+box.Width, box.Y+box.Height
	if m != (typeset.Matrix{}) {
		x0, y0, x1, y1 = math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
		for _, p := range [4][2]float64{{box.X, box.Y}, {box.X + box.Width, box.Y}, {box.X, box.Y + box.Height}, {box.X + box.Width, box.Y + box.Height}} {
			x := float64(m.A*p[0]) + float64(m.C*p[1]) + m.E
			y := float64(m.B*p[0]) + float64(m.D*p[1]) + m.F
			x0, y0, x1, y1 = min(x0, x), min(y0, y), max(x1, x), max(y1, y)
		}
	}
	return x1 > shown.X && x0 < shown.X+shown.Width && y1 > shown.Y && y0 < shown.Y+shown.Height
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
	// shown, when not nil, is the part of the page that shows
	// ([RenderPart]).
	shown *typeset.Rect
}
