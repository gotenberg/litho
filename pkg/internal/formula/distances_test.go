package formula

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// distanceStyle is Liberation Serif, which has no MATH table, at 12 pt
// under d.
func distanceStyle(d *Distances) Style {
	st := mathStyle("Liberation Serif", 12)
	st.Distances = d
	return st
}

// TestDistances_ScriptsByFontBoxes raises a superscript and drops a
// subscript by the distances from their base's baseline, a base taller
// than its font's box moving them by what it passes it.
func TestDistances_ScriptsByFontBoxes(t *testing.T) {
	s := liberation(t)
	st := distanceStyle(&Distances{SuperscriptRaise: 0.5, SubscriptDrop: 0.25})
	b := lay(t, s, el("math", el("msubsup", tok("mi", "a"), tok("mn", "1"), tok("mn", "2"))), st)
	if _, y, _ := childAt(b, 0, 1); !near(y, 3) {
		t.Fatalf("subscript %g down, want 3", y)
	}
	if _, y, _ := childAt(b, 0, 2); !near(-y, 6) {
		t.Fatalf("superscript %g up, want 6", -y)
	}
	_, _, a := childAt(b, 0, 0)
	f := (&layouter{ctx: t.Context(), s: s}).faceOf(&st)
	asc, desc, _ := f.fontBox(12)
	if !near(a.inkAsc, asc) || !near(a.inkDesc, desc) {
		t.Fatalf("token ink %g and %g, want its font's box %g and %g", a.inkAsc, a.inkDesc, asc, desc)
	}
	tall := lay(t, s, el("math", el("msup", space("1pt", "20pt", "0"), tok("mn", "2"))), st)
	if _, y, _ := childAt(tall, 0, 1); !near(-y, 6+20-asc) {
		t.Fatalf("superscript of a tall base %g up, want %g", -y, 6+20-asc)
	}
}

// TestDistances_LargeOperatorsAndLimits draws a display large operator
// larger and keeps LimitGap between its ink and its limits' font boxes.
func TestDistances_LargeOperatorsAndLimits(t *testing.T) {
	s := liberation(t)
	st := distanceStyle(&Distances{LargeOperatorScale: 1.5, LimitGap: 0.1})
	st.Display = true
	b := lay(t, s, el("math", el("munder", tok("mo", "∑"), tok("mi", "i"))), st)
	var sum *typeset.Text
	for _, tx := range texts(b) {
		if tx.Glyphs[0].Text == "∑" {
			sum = tx
		}
	}
	if sum == nil || !near(sum.Size, 18) {
		t.Fatalf("sum drawn %+v, want at 18 pt", sum)
	}
	_, _, op := childAt(b, 0, 0)
	_, y, lim := childAt(b, 0, 1)
	if gap := y - lim.inkAsc - op.inkDesc; !near(gap, 1.2) {
		t.Fatalf("limit %g below the operator's ink, want 1.2", gap)
	}
}

// TestDistances_BracketsScaledAndRowSpacing scales a bracket the font
// cannot stretch over its content and the excess, keeping its width, and
// spaces neighbours and the argument of a large operator.
func TestDistances_BracketsScaledAndRowSpacing(t *testing.T) {
	s := liberation(t)
	st := distanceStyle(&Distances{BracketExcess: 0.1, ElementSpacing: 0.1, OperatorSpacing: 0.25})
	open := attrs(tok("mo", "("), "stretchy", "true", "lspace", "0", "rspace", "0")
	b := lay(t, s, el("math", open, space("1pt", "30pt", "10pt")), st)
	_, _, p := childAt(b, 0)
	if !near(p.inkAsc+p.inkDesc, 44) {
		t.Fatalf("bracket %g tall, want 44: 40 and a tenth more", p.inkAsc+p.inkDesc)
	}
	if run := texts(b)[0]; run.HScale >= 1 || run.Size <= 12 {
		t.Fatalf("bracket drawn at %g, hscale %g; want taller and narrowed back", run.Size, run.HScale)
	}
	row := lay(t, s, el("math", space("10pt", "1pt", "0"), space("10pt", "1pt", "0"), el("munder", tok("mi", "lim"), tok("mi", "x")), space("1pt", "1pt", "0")), st)
	if x, _, _ := childAt(row, 1); !near(x, 11.2) {
		t.Fatalf("second space at %g, want 11.2", x)
	}
	_, _, lim := childAt(row, 2)
	limX, _, _ := childAt(row, 2)
	if x, _, _ := childAt(row, 3); !near(x, limX+lim.w+3) {
		t.Fatalf("argument at %g, want %g", x, limX+lim.w+3)
	}
}
