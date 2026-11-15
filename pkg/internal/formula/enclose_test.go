package formula

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// enclose lays out an menclose of notation over an mspace 10pt wide, 6pt
// high and 2pt deep, at 10pt, with rules, and returns the formula.
func enclose(t *testing.T, notation string, rules *Enclosure) *Box {
	t.Helper()
	s := dejavu(t)
	st := mathStyle("DejaVu Math TeX Gyre", 10)
	st.Enclose = rules
	return lay(t, s, el("math", attrs(el("menclose", space("10pt", "6pt", "2pt")), "notation", notation)), st)
}

// TestMenclose_RowWithoutRules lays menclose out as an mrow when the
// style asks for no enclosure, as MathML Core and Chromium do.
func TestMenclose_RowWithoutRules(t *testing.T) {
	b := enclose(t, "box circle", nil)
	if !near(b.Width, 10) || !near(b.Ascent, 6) || len(rects(b)) != 0 {
		t.Fatalf("menclose without rules: %g wide, %g high, %d paths", b.Width, b.Ascent, len(rects(b)))
	}
}

// TestMenclose_BoxKeepsItsGaps draws the four lines of a box the rule's
// thickness wide, the gaps from the content's advance and ink, and pads
// the box by gap and line (MathML 4, 3.3.9.2).
func TestMenclose_BoxKeepsItsGaps(t *testing.T) {
	b := enclose(t, "box", &Enclosure{InlineGap: 0.2, BlockGap: 0.1, Rule: 0.05})
	if !near(b.Width, 10+2*(2+0.5)) || !near(b.Ascent, 6+1+0.5) || !near(b.Descent, 2+1+0.5) {
		t.Fatalf("box %g wide, %g up, %g down; want 15, 7.5, 3.5", b.Width, b.Ascent, b.Descent)
	}
	rs := rects(b)
	if len(rs) != 4 {
		t.Fatalf("%d lines, want 4", len(rs))
	}
	top := rs[0].Ops
	if !near(top[0].P[0].Y, -7.5) || !near(top[2].P[0].Y-top[0].P[0].Y, 0.5) || !near(top[0].P[0].X, 0) || !near(top[1].P[0].X, 15) {
		t.Fatalf("top line %v", top)
	}
}

// TestMenclose_SidesAndStrikes pads only the sides that draw a line, and
// lets strikes cross the content without padding it.
func TestMenclose_SidesAndStrikes(t *testing.T) {
	rules := &Enclosure{InlineGap: 0.2, BlockGap: 0.1, Rule: 0.05}
	b := enclose(t, "top RIGHT", rules)
	if !near(b.Width, 12.5) || !near(b.Ascent, 7.5) || !near(b.Descent, 2) || len(rects(b)) != 2 {
		t.Fatalf("actuarial-like sides: %g wide, %g up, %g down, %d lines", b.Width, b.Ascent, b.Descent, len(rects(b)))
	}
	b = enclose(t, "horizontalstrike updiagonalstrike", rules)
	if !near(b.Width, 10) || !near(b.Ascent, 6) {
		t.Fatalf("strikes padded the content: %g wide, %g up", b.Width, b.Ascent)
	}
	var strokes, fills int
	for _, f := range b.Draw(0, 0) {
		if p, ok := f.(*typeset.Path); ok {
			if p.Stroke != nil {
				strokes++
			} else {
				fills++
			}
		}
	}
	if strokes != 1 || fills != 1 {
		t.Fatalf("%d strokes and %d fills, want a stroked diagonal and a filled horizontal", strokes, fills)
	}
}

// TestMenclose_StrikeRaiseAndNotations strikes at the height the rules
// give and draws only the notations they list.
func TestMenclose_StrikeRaiseAndNotations(t *testing.T) {
	rules := &Enclosure{Rule: 0.1, StrikeRaise: 0.3, Notations: []string{"horizontalstrike"}}
	b := enclose(t, "box horizontalstrike", rules)
	rs := rects(b)
	if len(rs) != 1 || !near(b.Width, 10) {
		t.Fatalf("%d paths over %g, want the strike alone over 10", len(rs), b.Width)
	}
	if y := rs[0].Ops[0].P[0].Y; !near(y, -3.5) {
		t.Fatalf("strike top at %g, want -3.5: its middle 3pt up, 1pt thick", y)
	}
}

// TestMenclose_CircleThroughTheCorners draws one ellipse through the
// corners of the gapped content, padding by what it reaches past them.
func TestMenclose_CircleThroughTheCorners(t *testing.T) {
	b := enclose(t, "circle", &Enclosure{Rule: 0.05})
	if b.Width <= 10*1.4 || b.Ascent+b.Descent <= 8*1.4 {
		t.Fatalf("circle box %g by %g, want past the content's corners", b.Width, b.Ascent+b.Descent)
	}
}
