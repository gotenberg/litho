package css

import (
	"context"
	"math"
	"testing"
)

// TestMathMLUserAgent_ParsesClean parses the MathML user agent sheet
// without a warning.
func TestMathMLUserAgent_ParsesClean(t *testing.T) {
	ua := MathMLUserAgent()
	if ua != MathMLUserAgent() {
		t.Fatal("MathMLUserAgent parses again")
	}
	if w := ua.Warnings(); len(w) > 0 {
		t.Fatalf("warnings: %v", w)
	}
}

// computeMath computes doc against both user agent sheets and author,
// fonts holding a MATH table that scales scripts by script and scripts
// of scripts by scriptScript.
func computeMath(t *testing.T, doc, author string, script, scriptScript float64) computed {
	t.Helper()
	root := parseHTML(t, doc)
	r := NewResolver(a4, []Sheet{
		{Sheet: HTMLUserAgent(), Origin: UserAgent}, {Sheet: MathMLUserAgent(), Origin: UserAgent},
		{Sheet: mustParse(t, author), Origin: Author},
	})
	units := func(*Style, string) FontUnits {
		return FontUnits{Math: true, ScriptPercentScaleDown: script, ScriptScriptPercentScaleDown: scriptScript}
	}
	c, err := r.Compute(context.Background(), root, ComputeOptions{Hints: HTMLHints, FontUnits: units})
	if err != nil {
		t.Fatal(err)
	}
	return computed{t: t, doc: root, c: c}
}

// TestFontSizeMath_ScalesByMathDepth scales scripts by the MATH table's
// scale-downs and by 0.71 a level past them (MathML Core, 4.5), and
// mathsize and scriptlevel act as presentational hints (2.1.5, 2.1.6).
func TestFontSizeMath_ScalesByMathDepth(t *testing.T) {
	doc := `<p style="font-size:20px"><math>
<msup><mi id=b>x</mi><msup><mi id=s1>y</mi><msup><mi>z</mi><mi id=s3>w</mi></msup></msup></msup>
<mfrac><mi id=n>a</mi><mi id=d>b</mi></mfrac>
<mi id=lvl scriptlevel="+2">c</mi><mi id=sz mathsize="200%">d</mi>
</math></p>`
	c := computeMath(t, doc, "", 0.8, 0.6)
	base := c.of("b")
	if base.FontSize != 15 || base.MathDepth != 0 || base.MathStyle != MathCompact {
		t.Fatalf("base: size %g, depth %d, style %d", base.FontSize, base.MathDepth, base.MathStyle)
	}
	s1 := c.c.Style(byID(c.doc, "s1").Parent)
	if !near(s1.FontSize, 12) || s1.MathDepth != 1 {
		t.Fatalf("script: size %g, depth %d; want 12, 1", s1.FontSize, s1.MathDepth)
	}
	s3 := c.of("s3")
	if !near(s3.FontSize, float64(15*0.6*0.71)) || s3.MathDepth != 3 {
		t.Fatalf("third level: size %g, depth %d; want %g, 3", s3.FontSize, s3.MathDepth, 15*0.6*0.71)
	}
	// A fraction in compact style scales its numerator and denominator
	// (auto-add), and cramps the denominator.
	if n, d := c.of("n"), c.of("d"); !near(n.FontSize, 12) || d.MathShift != MathShiftCompact || n.MathShift != MathShiftNormal {
		t.Fatalf("fraction: numerator %g, shifts %d and %d", n.FontSize, n.MathShift, d.MathShift)
	}
	if l := c.of("lvl"); l.MathDepth != 2 || !near(l.FontSize, 9) {
		t.Fatalf("scriptlevel=+2: depth %d, size %g; want 2, 9", l.MathDepth, l.FontSize)
	}
	if s := c.of("sz"); !near(s.FontSize, 30) {
		t.Fatalf("mathsize=200%%: size %g, want 30", s.FontSize)
	}
}

// TestMathDepth_Values parses auto-add, add() and integers and computes
// each against the inherited math-style (MathML Core, 4.5).
func TestMathDepth_Values(t *testing.T) {
	doc := `<div id=p style="math-depth:3;math-style:normal"><span id=a style="math-depth:auto-add"></span>
<span id=b style="math-depth:add(-5)"></span><span id=c style="math-depth:7"></span>
<i id=d style="math-style:compact"><b id=e style="math-depth:auto-add"></b></i></div>`
	c := computeMath(t, doc, "", 0.8, 0.6)
	for id, want := range map[string]int32{"p": 3, "a": 3, "b": -2, "c": 7, "e": 4} {
		if got := c.of(id).MathDepth; got != want {
			t.Errorf("%s: math-depth %d, want %d", id, got, want)
		}
	}
}

// TestDisplayMath_ComputesToFlow computes block math and inline math to
// block and inline outside MathML (MathML Core, 4.1).
func TestDisplayMath_ComputesToFlow(t *testing.T) {
	c := computeMath(t, `<div id=a style="display:block math"></div><span id=b style="display:inline math"></span><div id=c style="display:math"></div>`, "", 0.8, 0.6)
	if c.of("a").Display != Block || c.of("b").Display != Inline || c.of("c").Display != Inline {
		t.Fatalf("displays %d, %d, %d", c.of("a").Display, c.of("b").Display, c.of("c").Display)
	}
}

// TestMathHints_DisplaystyleAndMathvariant maps displaystyle onto
// math-style and mathvariant="normal" onto text-transform: none (MathML
// Core, 2.1.6 and 3.2.2).
func TestMathHints_DisplaystyleAndMathvariant(t *testing.T) {
	c := computeMath(t, `<math><mrow id=r displaystyle="TRUE"><mi id=i>x</mi><mi id=n mathvariant="normal">y</mi></mrow></math>`, "", 0.8, 0.6)
	if c.of("r").MathStyle != MathNormal {
		t.Fatal("displaystyle=TRUE is not math-style: normal")
	}
	if c.of("i").TextTransform != MathAuto || c.of("n").TextTransform != TransformNone {
		t.Fatalf("text-transform %d and %d", c.of("i").TextTransform, c.of("n").TextTransform)
	}
}

// TestMathFontScale_WithoutMathTable scales by 0.71 a level when the
// inherited font has no MATH table, and back up when the depth falls.
func TestMathFontScale_WithoutMathTable(t *testing.T) {
	if s := mathFontScale(0, 2, FontUnits{}); math.Abs(s-0.71*0.71) > 1e-12 {
		t.Fatalf("two levels: %g", s)
	}
	if s := mathFontScale(2, 0, FontUnits{Math: true, ScriptPercentScaleDown: 0.8, ScriptScriptPercentScaleDown: 0.6}); math.Abs(s-1/0.6) > 1e-12 {
		t.Fatalf("two levels up: %g", s)
	}
}

// TestMathStyleAttribute applies the style attribute of a MathML element
// as an HTML one's (MathML Core, 2.1.4).
func TestMathStyleAttribute(t *testing.T) {
	c := computeMath(t, `<math><mspace id=s style="background:green;color:red"/></math>`, "", 0.8, 0.6)
	if st := c.of("s"); st.BackgroundColor.G == 0 || st.Color.R != 1 {
		t.Fatalf("background %v, colour %v", st.BackgroundColor, st.Color)
	}
}
