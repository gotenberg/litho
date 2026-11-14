package formula

import (
	"os"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// TestFraction_BarOnTheAxis centres the bar of a fraction on the math
// axis, FractionRuleThickness thick (MathML Core, 3.3.2.1), as
// web-platform-tests' frac-parameters-1 measures with a font of axis 7000
// and rule 1000 units an em of 1000.
func TestFraction_BarOnTheAxis(t *testing.T) {
	s := wpt(t, "fraction-axisheight7000-rulethickness1000")
	root := el("math", el("mfrac", space("3em", "1em", "0"), space("3em", "1em", "0")))
	st := mathStyle("fraction-axisheight7000-rulethickness1000", 10)
	st.Display = true // a display fraction keeps its children's size
	rs := rects(lay(t, s, root, st))
	if len(rs) != 1 {
		t.Fatalf("%d rectangles, want the bar alone", len(rs))
	}
	top, bottom := rs[0].Ops[0].P[0].Y, rs[0].Ops[2].P[0].Y
	if !near(top, -75) || !near(bottom, -65) {
		t.Fatalf("bar from %g to %g, want -75 to -65 (axis 70, thickness 10)", top, bottom)
	}
	// The bar spans the content box: both 30-point spaces, the 0.75-point
	// paddings of mfrac aside.
	if x0, x1 := rs[0].Ops[0].P[0].X, rs[0].Ops[1].P[0].X; !near(x0, 0.75) || !near(x1, 30.75) {
		t.Fatalf("bar from x %g to %g, want 0.75 to 30.75", x0, x1)
	}
}

// TestFraction_LineThickness reads linethickness as a length or a
// percentage of FractionRuleThickness, a value of 0 drawing no bar
// (MathML Core, 3.3.2).
func TestFraction_LineThickness(t *testing.T) {
	s := wpt(t, "fraction-rulethickness10000")
	st := mathStyle("fraction-rulethickness10000", 1)
	for _, c := range []struct {
		value string
		want  float64
	}{{"", 10}, {"50%", 5}, {"2pt", 2}, {"-3pt", 0}, {"0", 0}, {"bogus", 10}} {
		frac := el("mfrac", space("1em", "1em", "0"), space("1em", "1em", "0"))
		if c.value != "" {
			attrs(frac, "linethickness", c.value)
		}
		rs := rects(lay(t, s, el("math", frac), st))
		got := 0.0
		if len(rs) == 1 {
			got = rs[0].Ops[2].P[0].Y - rs[0].Ops[0].P[0].Y
		}
		if !near(got, c.want) {
			t.Errorf("linethickness %q: bar %g thick, want %g", c.value, got, c.want)
		}
	}
}

// TestScripts_Shifts shifts a superscript up by SuperscriptShiftUp and a
// subscript down by SubscriptShiftDown, each after its base (MathML Core,
// 3.4.1.2 and 3.4.1.3).
func TestScripts_Shifts(t *testing.T) {
	s := wpt(t, "scripts-superscriptshiftup7000")
	root := el("math", el("msup", space("1em", "1em", "0"), space("1em", "1em", "0")))
	b := lay(t, s, root, mathStyle("scripts-superscriptshiftup7000", 10))
	if x, y, _ := childAt(b, 0, 1); !near(y, -70) || !near(x, 10) {
		t.Fatalf("superscript at %g, %g; want 10, -70", x, y)
	}
	s = wpt(t, "scripts-subscriptshiftdown6000")
	root = el("math", el("msub", space("1em", "1em", "0"), space("1em", "1em", "0")))
	b = lay(t, s, root, mathStyle("scripts-subscriptshiftdown6000", 10))
	if _, y, _ := childAt(b, 0, 1); !near(y, 60) {
		t.Fatalf("subscript at y %g, want 60", y)
	}
}

// TestLargeOp_DisplayOperatorMinHeight draws a large operator in display
// style with its first variant at least DisplayOperatorMinHeight tall,
// and with its base glyph in compact style (MathML Core, 3.2.4.3).
func TestLargeOp_DisplayOperatorMinHeight(t *testing.T) {
	s := wpt(t, "largeop-displayoperatorminheight5000")
	st := mathStyle("largeop-displayoperatorminheight5000", 10)
	op := attrs(tok("mo", "⫿"), "largeop", "true")
	b := lay(t, s, el("math", op), st)
	if !near(b.Ascent+b.Descent, 10) {
		t.Fatalf("compact large operator %g tall, want 10", b.Ascent+b.Descent)
	}
	st.Display = true
	b = lay(t, s, el("math", op), st)
	if !near(b.Ascent+b.Descent, 50) {
		t.Fatalf("display large operator %g tall, want 50", b.Ascent+b.Descent)
	}
}

// TestStretchy_Assembly stretches a vertical arrow through its variants
// and then its glyph assembly to the height of the row (MathML Core,
// 3.3.1.1 and 5.3), the copied text reading the arrow once.
func TestStretchy_Assembly(t *testing.T) {
	s := wpt(t, "stretchy")
	st := mathStyle("stretchy", 10)
	for _, h := range []float64{2.5, 10, 17} {
		root := el("math", attrs(tok("mo", "⥜"), "stretchy", "true", "symmetric", "false"),
			space("1em", fmtLen(h), "0"))
		b := lay(t, s, root, st)
		_, _, op := childAt(b, 0)
		if op.asc+op.desc < float64(h*10)-0.01 {
			t.Fatalf("arrow over %gem: %g tall", h, op.asc+op.desc)
		}
		text, glyphs := "", 0
		for _, f := range b.Draw(0, 0) {
			if r, ok := f.(*typeset.Text); ok {
				for _, g := range r.Glyphs {
					text += g.Text
					glyphs++
				}
			}
		}
		if text != "⥜" {
			t.Fatalf("arrow over %gem reads %q", h, text)
		}
		if h == 17 && glyphs < 2 {
			t.Fatalf("arrow over 17em drawn with %d glyphs, want an assembly", glyphs)
		}
	}
}

// fmtLen returns n em as a CSS length.
func fmtLen(n float64) string {
	return formatFloat(n) + "em"
}

// TestRow_OperatorSpacing spaces an infix + by the medium space of its
// category and a prefix one by none (MathML Core, 3.3.1.2 and B.1).
func TestRow_OperatorSpacing(t *testing.T) {
	s := dejavu(t)
	st := mathStyle("DejaVu Math TeX Gyre", 18)
	b := lay(t, s, el("math", tok("mn", "1"), tok("mo", "+"), tok("mn", "2")), st)
	_, _, one := childAt(b, 0)
	x, _, _ := childAt(b, 1)
	if want := one.w + 18*mediumSpace; !near(x, want) {
		t.Fatalf("infix + at %g, want %g", x, want)
	}
	b = lay(t, s, el("math", tok("mo", "+"), tok("mn", "2")), st)
	if x, _, _ := childAt(b, 0); !near(x, 0) {
		t.Fatalf("prefix + at %g, want 0", x)
	}
}

// TestDerive_ScriptSizes scales a script by scriptPercentScaleDown and a
// script of a script by scriptScriptPercentScaleDown (MathML Core, 4.5).
func TestDerive_ScriptSizes(t *testing.T) {
	s := wpt(t, "scriptpercentscaledown80-scriptscriptpercentscaledown40")
	st := mathStyle("scriptpercentscaledown80-scriptscriptpercentscaledown40", 10)
	l := &layouter{ctx: t.Context(), s: s}
	msup := el("msup", tok("mi", "x"), el("msup", tok("mi", "y"), tok("mi", "z")))
	one := l.derive(msup.Children[1], msup, 1, &st)
	two := l.derive(msup.Children[1].Children[1], msup.Children[1], 1, one)
	if !near(one.Size, 8) || !near(two.Size, 4) {
		t.Fatalf("script sizes %g and %g, want 8 and 4", one.Size, two.Size)
	}
	// scriptlevel="0" brings a script back to the root's size.
	back := l.derive(attrs(tok("mi", "a"), "scriptlevel", "0"), msup.Children[1], 1, one)
	if !near(back.Size, 10) {
		t.Fatalf("scriptlevel=0 size %g, want 10", back.Size)
	}
}

// TestUnderOver_UpperLimit places the limit over a large operator in
// display style UpperLimitGapMin above its ink, and as a superscript in
// compact style, where ∑ has movablelimits (MathML Core, 3.4.2.1 and
// 3.4.2.4).
func TestUnderOver_UpperLimit(t *testing.T) {
	s := wpt(t, "limits-upperlimitgapmin7000")
	st := mathStyle("limits-upperlimitgapmin7000", 10)
	st.Display = true
	root := el("math", el("mover", tok("mo", "∑"), space("1em", "0", "1em")))
	b := lay(t, s, root, st)
	_, _, base := childAt(b, 0, 0)
	_, y, over := childAt(b, 0, 1)
	// The overscript, 1em deep, has its bottom UpperLimitGapMin over the
	// operator's ink top.
	if want := -(base.inkAsc + 70 + over.desc); !near(y, want) {
		t.Fatalf("limit baseline at %g, want %g", y, want)
	}
	st.Display = false
	b = lay(t, s, root, st)
	x0, _, _ := childAt(b, 0)
	if x, _, _ := childAt(b, 0, 1); !near(x-x0, base.w) {
		t.Fatalf("compact limit at x %g, want a superscript at %g", x-x0, base.w)
	}
}

// TestRadical_GapAndBar puts the overbar RadicalVerticalGap over the
// base's ink, RadicalRuleThickness thick, the surd stretched to cover
// both (MathML Core, 3.3.3).
func TestRadical_GapAndBar(t *testing.T) {
	s := wpt(t, "radical-verticalgap6000-rulethickness1000")
	st := mathStyle("radical-verticalgap6000-rulethickness1000", 10)
	b := lay(t, s, el("math", el("msqrt", space("1em", "1em", "0"))), st)
	rs := rects(b)
	if len(rs) != 1 {
		t.Fatalf("%d rectangles, want the overbar", len(rs))
	}
	if top, bottom := rs[0].Ops[0].P[0].Y, rs[0].Ops[2].P[0].Y; !near(bottom, -(10+60)) || !near(top, -(10+60+10)) {
		t.Fatalf("overbar from %g to %g, want -80 to -70", top, bottom)
	}
	_, _, surd := childAt(b, 0, 0)
	if surd.asc+surd.desc < 80-0.01 {
		t.Fatalf("surd %g tall, want at least 80", surd.asc+surd.desc)
	}
}

// TestMpadded_Attributes sizes an mpadded by its width, height and depth
// and moves its content by lspace and voffset (MathML Core, 3.3.6).
func TestMpadded_Attributes(t *testing.T) {
	s := dejavu(t)
	st := mathStyle("DejaVu Math TeX Gyre", 10)
	p := attrs(el("mpadded", space("1em", "1em", "1em")), "width", "5em", "height", "2em", "depth", "30%", "lspace", "1em", "voffset", "-1em")
	b := lay(t, s, el("math", p), st)
	if !near(b.Width, 50) || !near(b.Ascent, 20) || !near(b.Descent, 10) {
		t.Fatalf("mpadded %g wide, %g up and %g down; want 50, 20, 10 (a percentage depth keeps the inner one)", b.Width, b.Ascent, b.Descent)
	}
	if x, y, _ := childAt(b, 0, 0); !near(x, 10) || !near(y, 10) {
		t.Fatalf("content at %g, %g; want 10, 10", x, y)
	}
}

// TestTable_CentredOnTheAxis lays a two by two matrix out with its middle
// on the math axis and each cell centred in its column (MathML Core,
// 3.5).
func TestTable_CentredOnTheAxis(t *testing.T) {
	s := wpt(t, "fraction-axisheight7000-rulethickness1000")
	st := mathStyle("fraction-axisheight7000-rulethickness1000", 10)
	cell := func(w string) *Element { return el("mtd", space(w, "1em", "0")) }
	root := el("math", el("mtable", el("mtr", cell("1em"), cell("3em")), el("mtr", cell("2em"), cell("1em"))))
	b := lay(t, s, root, st)
	if mid := (b.Ascent - b.Descent) / 2; !near(mid, 70) {
		t.Fatalf("table middle at %g, want the axis at 70", mid)
	}
	_, _, tab := childAt(b, 0)
	var xs []float64
	for _, it := range tab.items {
		if it.child != nil {
			xs = append(xs, it.x)
		}
	}
	// Columns 2em and 3em wide plus 0.8em of padding: the first column's
	// cells centred in 28 points, the second's in 38 after it.
	want := []float64{9, 28 + 4, 4, 28 + 14}
	for i := range want {
		if i >= len(xs) || !near(xs[i], want[i]) {
			t.Fatalf("cell contents at %v, want %v", xs, want)
		}
	}
}

// TestBounds_DepthAndElements leaves out what lies past MaxDepth and past
// MaxElements, with a warning each.
func TestBounds_DepthAndElements(t *testing.T) {
	s := dejavu(t)
	st := mathStyle("DejaVu Math TeX Gyre", 10)
	deep := tok("mi", "x")
	for range MaxDepth + 5 {
		deep = el("mrow", deep)
	}
	if b := lay(t, s, el("math", deep), st); len(b.Warnings) != 1 || b.Width != 0 {
		t.Fatalf("a formula nested %d deep: width %g, warnings %v", MaxDepth+5, b.Width, b.Warnings)
	}
	wide := el("math")
	for range MaxElements + 10 {
		wide.Children = append(wide.Children, &Element{Name: "mspace"})
	}
	if b := lay(t, s, wide, st); len(b.Warnings) != 1 {
		t.Fatalf("%d elements: warnings %v", MaxElements+11, b.Warnings)
	}
}

// TestBrowserRules_ScriptFallbacks shifts scripts on a font without a
// MATH table, Liberation Serif, by Chromium's fallbacks under
// BrowserRules, by those of MathML Core, 5.1 without.
func TestBrowserRules_ScriptFallbacks(t *testing.T) {
	dir := "../../../test/integration/testdata/fonts/gotenberg/truetype/liberation"
	if _, err := os.Stat(dir); err != nil {
		t.Skipf("fixture fonts absent: %v", err)
	}
	fonts, err := text.NewFonts(t.Context(), nil, []string{dir})
	if err != nil {
		t.Fatal(err)
	}
	st := mathStyle("Liberation Serif", 100)
	root := el("math", el("msup", space("10px", "0", "0"), space("10px", "0", "0")))
	s := NewSetter(fonts, nil)
	f := (&layouter{ctx: t.Context(), s: s}).faceOf(&st)
	if f == nil || f.math != nil {
		t.Fatal("Liberation Serif does not load, or has a MATH table")
	}
	xh := f.units(f.xHeight, 100)
	b := lay(t, s, root, st)
	if _, y, _ := childAt(b, 0, 1); !near(-y, f.units(f.superOffset, 100)) {
		t.Fatalf("superscript %g up without BrowserRules, want ySuperscriptYOffset", -y)
	}
	s = NewSetter(fonts, nil)
	s.BrowserRules = true
	b = lay(t, s, root, st)
	if _, y, _ := childAt(b, 0, 1); !near(-y, xh) {
		t.Fatalf("superscript %g up, want the x-height %g", -y, xh)
	}
}

// TestEdges_NoItalicCorrectionFromMargins grows a box by its padding,
// border and margin without giving it an italic correction it has none
// of, so a row places the next child right after its margin box
// (MathML Core, 3.1.2).
func TestEdges_NoItalicCorrectionFromMargins(t *testing.T) {
	s := dejavu(t)
	st := mathStyle("DejaVu Math TeX Gyre", 10)
	a := space("10pt", "1pt", "0")
	as := st
	as.Margin = [4]float64{2, 3, 2, 3}
	as.Padding = [4]float64{0, 4, 0, 4}
	a.Style = &as
	b := lay(t, s, el("math", a, space("10pt", "1pt", "0")), st)
	if x, _, _ := childAt(b, 1); !near(x, 24) {
		t.Fatalf("second child at %g, want 24 (3 + 4 + 10 + 4 + 3)", x)
	}
}

// TestContentSize_WidthAndHeight sizes a content box by CSS width and
// height: a fraction's bar spans it with its children centred, an mrow
// keeps its content at the inline-start edge (MathML Core, 3.1.2 and
// 3.3.2.1).
func TestContentSize_WidthAndHeight(t *testing.T) {
	s := wpt(t, "fraction-rulethickness10000")
	st := mathStyle("fraction-rulethickness10000", 1)
	st.Display = true
	frac := el("mfrac", space("10pt", "1pt", "0"), space("10pt", "1pt", "0"))
	fs := st
	fs.Width, fs.HasWidth = 50, true
	frac.Style = &fs
	rs := rects(lay(t, s, el("math", frac), st))
	if len(rs) != 1 || !near(rs[0].Ops[1].P[0].X-rs[0].Ops[0].P[0].X, 50) {
		t.Fatalf("bar %v, want 50 wide", rs)
	}
	row := el("mrow", space("10pt", "1pt", "0"))
	rst := st
	rst.Width, rst.HasWidth, rst.Height, rst.HasHeight = 30, true, 20, true
	row.Style = &rst
	b := lay(t, s, el("math", row), st)
	if x, _, _ := childAt(b, 0, 0); !near(b.Width, 30) || !near(b.Ascent+b.Descent, 20) || !near(x, 0) {
		t.Fatalf("mrow %g by %g, content at %g; want 30 by 20 at 0", b.Width, b.Ascent+b.Descent, x)
	}
}

// TestRTL_MirrorsTheRow places the children of a right-to-left row from
// the right, the operator's lspace on its right (MathML Core, 3.1.1).
func TestRTL_MirrorsTheRow(t *testing.T) {
	s := dejavu(t)
	st := mathStyle("DejaVu Math TeX Gyre", 10)
	st.RTL = true
	b := lay(t, s, el("math", space("10pt", "1pt", "0"), attrs(tok("mo", "+"), "lspace", "5pt", "rspace", "0"), space("20pt", "1pt", "0")), st)
	_, _, plus := childAt(b, 1)
	if x, _, _ := childAt(b, 0); !near(x, b.Width-10) {
		t.Fatalf("first child at %g, want at the right end %g", x, b.Width-10)
	}
	if x, _, _ := childAt(b, 2); !near(x, 0) {
		t.Fatalf("last child at %g, want 0", x)
	}
	if x, _, _ := childAt(b, 1); !near(x, 20) || !near(b.Width, 35+plus.w) {
		t.Fatalf("operator at %g in %g, want 20 in %g", x, b.Width, 35+plus.w)
	}
}

// TestTable_ColumnSpan lets a cell of columnspan="2" span both columns
// and centres it across them (MathML Core, 3.5.3; HTML Standard, the
// algorithm for processing rows).
func TestTable_ColumnSpan(t *testing.T) {
	s := wpt(t, "fraction-axisheight7000-rulethickness1000")
	st := mathStyle("fraction-axisheight7000-rulethickness1000", 10)
	cell := func(w string) *Element { return el("mtd", space(w, "1em", "0")) }
	wide := attrs(cell("1em"), "columnspan", "2")
	root := el("math", el("mtable", el("mtr", cell("2em"), cell("3em")), el("mtr", wide)))
	b := lay(t, s, el("math", root), st)
	_, _, tab := childAt(b, 0, 0)
	var xs []float64
	for _, it := range tab.items {
		if it.child != nil {
			xs = append(xs, it.x)
		}
	}
	// Columns 28 and 38 points wide: the spanning cell's 10-point content
	// is centred in 66.
	if len(xs) != 3 || !near(xs[2], 28) || !near(b.Width, 66) {
		t.Fatalf("cell contents at %v in %g, want the third at 28 in 66", xs, b.Width)
	}
}
