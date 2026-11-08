package xlsx

import (
	"context"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
	"github.com/gotenberg/litho/pkg/internal/text"
)

// maxShapeRunes caps the characters of one shape's text. Anti-DoS guard,
// not spec-mandated: a text box holds a few paragraphs.
const maxShapeRunes = 8192

// The defaults of a shape's text body (ECMA-376 Part 1, 21.1.2.1.1
// bodyPr: insets of 0.1 inch left and right and 0.05 inch top and bottom;
// 21.1.2.3.9 rPr: a size of 18 points the spreadsheet application
// replaces by 11, Excel's and LibreOffice's text box default).
const (
	shapeInsetLR   = 7.2
	shapeInsetTB   = 3.6
	shapeTextSize  = 11.0
	maxShapeTextPt = 400.0
)

// shapeText is the text body of a shape (ECMA-376 Part 1, 20.5.2.31
// txBody): its paragraphs, insets, vertical anchoring and wrapping.
type shapeText struct {
	paras  []shapePara
	insets [4]float64
	// anchor is the bodyPr anchor: "t", "ctr" or "b".
	anchor string
	wrap   bool
}

// shapePara is one paragraph of a shape's text with its alignment, "l",
// "ctr", "r" or "just".
type shapePara struct {
	runs  []text.Run
	align string
	size  float64
}

// shapeTextOf reads a shape's text body, nil when it holds no text: the
// runs, fields and breaks of its paragraphs, each run in its rPr face,
// size, weight, slant and solid colour over the shape style's font
// reference (ECMA-376 Part 1, 20.1.4.2.10 fontRef), the theme's minor
// font when the run names none.
func (r *reader) shapeTextOf(tb, style *ooxml.Element) *shapeText {
	if tb == nil || tb.Find(ooxml.NSA, "t") == nil {
		return nil
	}
	st := &shapeText{insets: [4]float64{shapeInsetTB, shapeInsetLR, shapeInsetTB, shapeInsetLR}, anchor: "t", wrap: true}
	body := tb.Child(ooxml.NSA, "bodyPr")
	for side, name := range [4]string{"tIns", "rIns", "bIns", "lIns"} {
		if v, ok := ooxml.EMU(attr(body, name)); ok {
			st.insets[side] = max(0, v)
		}
	}
	if a := attr(body, "anchor"); a == "ctr" || a == "b" {
		st.anchor = a
	}
	st.wrap = attr(body, "wrap") != "none"
	base := text.Style{Families: r.themeFont("+mn-lt"), Size: shapeTextSize, Color: compose.Color{A: 1}}
	if fr := style.Child(ooxml.NSA, "fontRef"); fr != nil {
		if c, ok := ooxml.SchemeColor(fr, r.theme); ok {
			base.Color = composeColor(c)
		}
		if attr(fr, "idx") == "major" {
			base.Families = r.themeFont("+mj-lt")
		}
	}
	n := 0
paras:
	for _, p := range tb.Kids() {
		if !p.Is(ooxml.NSA, "p") {
			continue
		}
		para := shapePara{align: attr(p.Child(ooxml.NSA, "pPr"), "algn"), size: base.Size}
		endStyle := r.runStyle(p.Child(ooxml.NSA, "endParaRPr"), base)
		para.size = endStyle.Size
		for _, k := range p.Kids() {
			var s string
			rs := base
			switch {
			case k.Is(ooxml.NSA, "r"), k.Is(ooxml.NSA, "fld"):
				s = textOf(k.Child(ooxml.NSA, "t"))
				rs = r.runStyle(k.Child(ooxml.NSA, "rPr"), base)
			case k.Is(ooxml.NSA, "br"):
				s = "\n"
				rs = r.runStyle(k.Child(ooxml.NSA, "rPr"), base)
			default:
				continue
			}
			if n += utf8.RuneCountInString(s); n > maxShapeRunes {
				break paras
			}
			para.runs = append(para.runs, text.Run{Text: s, Style: rs})
		}
		st.paras = append(st.paras, para)
	}
	return st
}

// themeFont returns the face a DrawingML typeface names: the theme's
// minor or major Latin font for +mn-lt and +mj-lt, the name itself
// otherwise.
func (r *reader) themeFont(name string) []string {
	switch name {
	case "+mn-lt", "+mn-ea", "+mn-cs":
		if r.theme != nil && r.theme.Minor.Latin != "" {
			return []string{r.theme.Minor.Latin}
		}
		return []string{"Calibri"}
	case "+mj-lt", "+mj-ea", "+mj-cs":
		if r.theme != nil && r.theme.Major.Latin != "" {
			return []string{r.theme.Major.Latin}
		}
		return []string{"Calibri Light"}
	}
	return []string{name}
}

// runStyle applies a DrawingML run's properties (ECMA-376 Part 1,
// 21.1.2.3.9 rPr: sz in hundredths of a point, b, i, a solid fill, the
// latin typeface) over base.
func (r *reader) runStyle(pr *ooxml.Element, base text.Style) text.Style {
	s := base
	if pr == nil {
		return s
	}
	if v, err := strconv.Atoi(strings.TrimSpace(attr(pr, "sz"))); err == nil && v > 0 {
		s.Size = min(float64(v)/100, maxShapeTextPt)
	}
	if v, ok := pr.Attr("b"); ok {
		s.Weight = 400
		if ooxml.OnOff(v, true) {
			s.Weight = 700
		}
	}
	if v, ok := pr.Attr("i"); ok {
		s.Italic = ooxml.OnOff(v, true)
	}
	if f := pr.Child(ooxml.NSA, "solidFill"); f != nil {
		if c, ok := ooxml.SchemeColor(f, r.theme); ok {
			s.Color = composeColor(c)
		}
	}
	if l := attr(pr.Child(ooxml.NSA, "latin"), "typeface"); l != "" {
		s.Families = r.themeFont(l)
	}
	return s
}

// composeColor converts a CSS colour to a compose one.
func composeColor(c css.Color) compose.Color {
	return compose.Color{R: c.R, G: c.G, B: c.B, A: c.A}
}

// draw lays the text out in box, inside the insets, each paragraph's
// lines aligned and the whole anchored to the top, middle or bottom, and
// returns its glyph runs. Text that does not lay out draws nothing.
func (st *shapeText) draw(ctx context.Context, fonts *text.Fonts, box compose.Rect) []compose.Fragment {
	avail := box.Width - st.insets[css.Left] - st.insets[css.Right]
	w := avail
	if !st.wrap || w <= 0 {
		w = 1 << 16
	}
	type placed struct {
		line  text.Line
		align string
	}
	var lines []placed
	height := 0.0
	for _, p := range st.paras {
		runs := p.runs
		if len(runs) == 0 || allEmpty(runs) {
			runs = []text.Run{{Text: " ", Style: text.Style{Size: p.size, Families: []string{"Calibri"}}}}
		}
		ls, _, err := fonts.Layout(ctx, runs, w, text.Options{Whitespace: text.Preserve})
		if err != nil {
			continue
		}
		for _, l := range ls {
			lines = append(lines, placed{l, p.align})
			height += l.Ascent + l.Descent + l.LineGap
		}
	}
	top := box.Y + st.insets[css.Top]
	inner := box.Height - st.insets[css.Top] - st.insets[css.Bottom]
	switch st.anchor {
	case "ctr":
		top += (inner - height) / 2
	case "b":
		top += inner - height
	}
	var out []compose.Fragment
	y := top
	for _, pl := range lines {
		l := pl.line
		y += l.Ascent
		x := box.X + st.insets[css.Left]
		switch pl.align {
		case "ctr":
			x += (avail - l.Width) / 2
		case "r":
			x += avail - l.Width
		}
		for _, run := range l.Runs {
			if run.Color.A <= 0 {
				// A fully transparent run paints nothing, and compose
				// refuses one.
				continue
			}
			t := *run
			t.Origin = compose.Point{X: x + run.Origin.X, Y: y}
			out = append(out, &t)
		}
		y += l.Descent + l.LineGap
	}
	return out
}

// allEmpty reports whether every run's text is empty.
func allEmpty(runs []text.Run) bool {
	for _, r := range runs {
		if r.Text != "" {
			return false
		}
	}
	return true
}
