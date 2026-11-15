package formula

import (
	"os"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	mt "github.com/gotenberg/litho/pkg/internal/typography/mathtable"
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

// TestUnderOver_ScriptsKeepTheirGaps keeps UnderbarVerticalGap between
// the ink of a base that is no operator and the ink top of its
// underscript, and OverbarVerticalGap between its ink top and the ink
// bottom of its overscript, as a limit under a function name sits in an
// Office equation.
func TestUnderOver_ScriptsKeepTheirGaps(t *testing.T) {
	s := dejavu(t)
	st := mathStyle("DejaVu Math TeX Gyre", 20)
	under := el("munder", tok("mi", "lim"), el("mrow", tok("mi", "n")))
	b := lay(t, s, el("math", under), st)
	_, _, base := childAt(b, 0, 0)
	_, y, script := childAt(b, 0, 1)
	f := s.faces[faceKey{face: s.fonts.Match(textStyle(&st))}]
	gap := f.constant(mt.UnderbarVerticalGap, st.Size)
	if got := y - script.inkAsc - base.inkDesc; !near(got, gap) || gap <= 0 {
		t.Fatalf("underscript ink %g below the base's ink, want UnderbarVerticalGap %g", got, gap)
	}
	over := el("mover", tok("mi", "x"), el("mrow", tok("mi", "y")))
	b = lay(t, s, el("math", over), st)
	_, _, base = childAt(b, 0, 0)
	_, y, script = childAt(b, 0, 1)
	gap = f.constant(mt.OverbarVerticalGap, st.Size)
	if got := -y - script.inkDesc - base.inkAsc; !near(got, gap) || gap <= 0 {
		t.Fatalf("overscript ink %g above the base's ink, want OverbarVerticalGap %g", got, gap)
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

// TestSpace_HeightCSSResolved takes the line-ascent of an mspace whose
// height attribute stands alone in a unit the layout cannot resolve from
// the height CSS resolved for it, the attribute being its presentational
// hint (MathML Core, 3.2.5).
func TestSpace_HeightCSSResolved(t *testing.T) {
	s := dejavu(t)
	st := mathStyle("DejaVu Math TeX Gyre", 10)
	sp := attrs(&Element{Name: "mspace"}, "height", "2rem")
	sst := st
	sst.Height, sst.HasHeight = 24, true
	sp.Style = &sst
	b := lay(t, s, el("math", sp), st)
	if !near(b.Ascent, 24) || !near(b.Descent, 0) {
		t.Fatalf("ascent %g, descent %g; want 24 and 0", b.Ascent, b.Descent)
	}
}

// TestRTL_ContentBoxStartsOnTheRight places the math content box of a
// right-to-left element CSS makes wider at the content box's right edge,
// its inline-start edge (MathML Core, 3.1.2).
func TestRTL_ContentBoxStartsOnTheRight(t *testing.T) {
	s := dejavu(t)
	st := mathStyle("DejaVu Math TeX Gyre", 10)
	st.RTL = true
	row := el("mrow", space("10pt", "1pt", "0"))
	rst := st
	rst.Width, rst.HasWidth = 30, true
	row.Style = &rst
	b := lay(t, s, el("math", row), st)
	if x, _, _ := childAt(b, 0, 0); !near(x, 20) {
		t.Fatalf("content at %g, want 20", x)
	}
}

// TestRTL_MirrorsTheRadicalOfARoot draws the surd of a right-to-left
// mroot on the right of its base, the index further right (MathML Core,
// 3.1.1 and 3.3.3.3).
func TestRTL_MirrorsTheRadicalOfARoot(t *testing.T) {
	s := dejavu(t)
	st := mathStyle("DejaVu Math TeX Gyre", 10)
	st.RTL = true
	b := lay(t, s, el("math", el("mroot", space("20pt", "10pt", "0"), space("5pt", "5pt", "0"))), st)
	_, _, radical := childAt(b, 0, 1)
	baseX, _, _ := childAt(b, 0, 1, 1)
	surdX, _, _ := childAt(b, 0, 1, 0)
	if !near(baseX, 0) || surdX < 20-0.01 || radical.w <= 20 {
		t.Fatalf("base at %g, surd at %g in a radical %g wide; want the base at 0, the surd past it", baseX, surdX, radical.w)
	}
}

// TestDerive_Direction reads the dir attribute as a presentational hint
// for direction and lets the children of a derived style inherit it
// (MathML Core, 2.1.3; CSS Writing Modes 3, 2.1: direction inherits).
func TestDerive_Direction(t *testing.T) {
	s := dejavu(t)
	st := mathStyle("DejaVu Math TeX Gyre", 10)
	row := func() *Element { return el("mrow", space("10pt", "1pt", "0"), space("20pt", "1pt", "0")) }
	for _, c := range []struct {
		root *Element
		path []int
	}{
		{el("math", attrs(row(), "dir", "rtl")), []int{0, 0}},
		{el("math", attrs(el("mstyle", row()), "dir", "rtl")), []int{0, 0, 0}},
	} {
		b := lay(t, s, c.root, st)
		if x, _, _ := childAt(b, c.path...); !near(x, 20) {
			t.Fatalf("first space at depth %d at %g, want 20", len(c.path), x)
		}
	}
}

// TestEdges_BorderOfOneColourIsARing fills a border of one colour all
// round as one ring between the border box and the padding box, as the
// HTML layout fills it, and a border of several colours a side at a time
// (MathML Core, 3.1.2; CSS Backgrounds 3, 4).
func TestEdges_BorderOfOneColourIsARing(t *testing.T) {
	s := dejavu(t)
	st := mathStyle("DejaVu Math TeX Gyre", 10)
	blue, red := typeset.Color{B: 1, A: 1}, typeset.Color{R: 1, A: 1}
	for _, c := range []struct {
		colors [4]typeset.Color
		paths  int
	}{{[4]typeset.Color{blue, blue, blue, blue}, 1}, {[4]typeset.Color{blue, red, blue, blue}, 4}} {
		sp := space("10pt", "5pt", "0")
		sst := st
		sst.Border, sst.BorderColor = [4]float64{1, 2, 1, 2}, c.colors
		sp.Style = &sst
		rs := rects(lay(t, s, el("math", sp), st))
		if len(rs) != c.paths {
			t.Fatalf("%d paths, want %d", len(rs), c.paths)
		}
		if c.paths == 1 && (len(rs[0].Ops) != 10 || rs[0].Fill.Rule != typeset.EvenOdd || !near(rs[0].Ops[5].P[0].X-rs[0].Ops[0].P[0].X, 2)) {
			t.Fatalf("ring %+v, want two rectangles filled even-odd, the inner one 2pt in", rs[0])
		}
	}
}

// TestDraw_BackgroundsUnderEveryGlyph paints the backgrounds of every
// element of a formula before any glyph, so a later sibling's background
// stays under an earlier sibling's text (CSS 2.1, Appendix E.2; WPT
// mrow-painting-order).
func TestDraw_BackgroundsUnderEveryGlyph(t *testing.T) {
	s := dejavu(t)
	st := mathStyle("DejaVu Math TeX Gyre", 10)
	green := typeset.Color{G: 1, A: 1}
	a, c := tok("mn", "1"), tok("mn", "2")
	for _, e := range []*Element{a, c} {
		est := st
		est.Background = green
		e.Style = &est
	}
	frags := lay(t, s, el("math", a, c), st).Draw(0, 0)
	var kinds []string
	for _, f := range frags {
		switch f.(type) {
		case *typeset.Path:
			kinds = append(kinds, "bg")
		case *typeset.Text:
			kinds = append(kinds, "text")
		}
	}
	if strings.Join(kinds, " ") != "bg bg text text" {
		t.Fatalf("paint order %v, want both backgrounds first", kinds)
	}
}

// TestEdges_MarginsCountAsInk places the denominator of a fraction whose
// numerator has a margin as if the margin were ink, as Chromium does (WPT
// padding-border-margin-007), and leaves the ink of a padding alone
// (MathML Core, 3.1.2).
func TestEdges_MarginsCountAsInk(t *testing.T) {
	s := dejavu(t)
	st := mathStyle("DejaVu Math TeX Gyre", 10)
	sp := space("10pt", "5pt", "5pt")
	mst := st
	mst.Margin = [4]float64{3, 0, 3, 0}
	sp.Style = &mst
	_, _, b := childAt(lay(t, s, el("math", sp), st), 0)
	if !near(b.inkAsc, 8) || !near(b.inkDesc, 8) {
		t.Fatalf("ink %g and %g, want the margin box's, 8 and 8", b.inkAsc, b.inkDesc)
	}
	pd := space("10pt", "5pt", "5pt")
	pst := st
	pst.Padding = [4]float64{3, 0, 3, 0}
	pd.Style = &pst
	_, _, b = childAt(lay(t, s, el("math", pd), st), 0)
	if !near(b.inkAsc, 5) || !near(b.asc, 8) {
		t.Fatalf("ink %g in a box of ascent %g, want 5 in 8", b.inkAsc, b.asc)
	}
}

// TestBrowserRules_RowOfStretchersLaidOutFree lets an mrow that is an
// embellished operator, alone in its formula, stretch its operator over
// its own mspace under browser rules, as Chromium draws it, where MathML
// Core, 3.3.1.1 gives it the stretch size constraint (0, 0).
func TestBrowserRules_RowOfStretchersLaidOutFree(t *testing.T) {
	for _, browser := range []bool{false, true} {
		s := wpt(t, "stretchy")
		s.BrowserRules = browser
		st := mathStyle("stretchy", 20)
		b := lay(t, s, el("math", el("mrow", space("0", "100pt", "100pt"), tok("mo", "⥜"))), st)
		_, _, op := childAt(b, 0, 1)
		if tall := op.height() >= 200; tall != browser {
			t.Fatalf("browser rules %v: operator %g tall", browser, op.height())
		}
	}
}

// TestBrowserRules_UnstretchedGlyphCentred centres the glyph of a
// stretchy operator its font cannot stretch on the target sizes under
// browser rules, the target of a symmetric one on the axis, as Chromium
// draws WPT op-dict-3, and lays it out as text without them (MathML Core,
// 3.2.4.3).
func TestBrowserRules_UnstretchedGlyphCentred(t *testing.T) {
	for _, browser := range []bool{false, true} {
		s := liberation(t)
		s.BrowserRules = browser
		st := mathStyle("Liberation Serif", 20)
		var ys [2]float64
		for i, sym := range []string{"false", "true"} {
			b := lay(t, s, el("math", attrs(tok("mo", "("), "symmetric", sym), space("0", "100pt", "100pt")), st)
			_, _, op := childAt(b, 0)
			ys[i] = op.asc
		}
		if moved := !near(ys[0], ys[1]); moved != browser {
			t.Fatalf("browser rules %v: glyph ascents %v", browser, ys)
		}
	}
}

// TestUnderOver_AccentsSitOnTheirBase sets an accent's ink bottom on the
// base when the base reaches AccentBaseHeight, AccentBaseHeight above
// the baseline when it does not, from the accent's line-descent, and
// hangs an accent underscript from the base's ink by its line-ascent
// (MathML Core, 3.4.2.3 and 3.4.2.4; OpenType MATH, AccentBaseHeight).
func TestUnderOver_AccentsSitOnTheirBase(t *testing.T) {
	s := dejavu(t)
	st := mathStyle("DejaVu Math TeX Gyre", 100)
	f := (&layouter{ctx: t.Context(), s: s}).faceOf(&st)
	abh := f.constant(mt.AccentBaseHeight, 100)
	for _, base := range []string{"x", "A"} {
		b := lay(t, s, el("math", attrs(el("mover", tok("mi", base), tok("mo", "ˆ")), "accent", "true")), st)
		_, _, bx := childAt(b, 0, 0)
		_, y, acc := childAt(b, 0, 1)
		bottom := -y - acc.inkDesc
		if want := max(bx.inkAsc, abh); !near(bottom, want) {
			t.Fatalf("accent over %s: ink bottom %g above the baseline, want %g", base, bottom, want)
		}
	}
	// A font of web-platform-tests with no AccentBaseHeight: the accent's
	// box sits on the base's ink.
	w := wpt(t, "limits-upperlimitgapmin7000")
	wst := mathStyle("limits-upperlimitgapmin7000", 10)
	b := lay(t, w, el("math", attrs(el("mover", space("10pt", "3pt", "1pt"), space("10pt", "2pt", "4pt")), "accent", "true"),
		attrs(el("munder", space("10pt", "3pt", "1pt"), space("10pt", "2pt", "4pt")), "accentunder", "true")), wst)
	if _, y, _ := childAt(b, 0, 1); !near(-y, 3+4) {
		t.Fatalf("accent baseline %g up, want 7: its descent on the base's ink", -y)
	}
	if _, y, _ := childAt(b, 1, 1); !near(y, 1+2) {
		t.Fatalf("accentunder baseline %g down, want 3: its ascent under the base's ink", y)
	}
}

// TestRTL_MirroredGlyphs draws the Bidi_Mirrored characters of a
// right-to-left formula mirrored, a stretched fence included, and draws
// no glyph for the mark that makes its text right to left (MathML 4,
// 3.1.5.2; Unicode, BidiMirroring).
func TestRTL_MirroredGlyphs(t *testing.T) {
	s := dejavu(t)
	gids := func(rtl bool) map[string]uint16 {
		st := mathStyle("DejaVu Math TeX Gyre", 10)
		st.RTL = rtl
		root := el("math", tok("mo", "("), el("mfrac", tok("mi", "a"), tok("mi", "b")), tok("mo", "≤"), tok("mn", "12"))
		out := map[string]uint16{}
		for _, tx := range texts(lay(t, s, root, st)) {
			for _, g := range tx.Glyphs {
				if g.Text == "‏" {
					t.Fatal("the right-to-left mark is drawn")
				}
				if g.Text != "" {
					out[g.Text] = g.GID
				}
			}
		}
		return out
	}
	ltr, rtl := gids(false), gids(true)
	for _, c := range []string{"(", "≤"} {
		if ltr[c] == rtl[c] || rtl[c] == 0 {
			t.Fatalf("%s drawn as glyph %d in both directions", c, rtl[c])
		}
	}
	if ltr["1"] != rtl["1"] {
		t.Fatal("a digit is mirrored")
	}
}
