package css

import (
	"context"
	"testing"
)

func TestLogical_InlineSidesFollowDirection(t *testing.T) {
	c := compute(t, `<div id=ltr></div><div id=rtl dir=rtl></div><div dir=rtl><p id=inh></p></div><div id=late></div><div id=phys dir=rtl></div>`, `
div, p { margin-inline-start: 5px; padding-inline: 1px 2px; border-inline-start: 3px solid red; inset-inline-end: 4px }
#late { direction: rtl }
#phys { margin-left: 9px }`)
	tests := []struct {
		id                     string
		ml, mr, pl, pr, bl, br float64
		left, right            bool
	}{
		{"ltr", 5, 0, 1, 2, 3, 0, false, true},
		{"rtl", 0, 5, 2, 1, 0, 3, true, false},
		{"inh", 0, 5, 2, 1, 0, 3, true, false},
		{"late", 0, 5, 2, 1, 0, 3, true, false},
		{"phys", 9, 5, 2, 1, 0, 3, true, false},
	}
	for _, tt := range tests {
		s := c.of(tt.id)
		bw := func(sd Side) float64 {
			if s.Border[sd].Style == BorderNone {
				return 0
			}
			return s.Border[sd].Width
		}
		got := []float64{s.Margin[Left].Value, s.Margin[Right].Value, s.Padding[Left].Value, s.Padding[Right].Value, bw(Left), bw(Right)}
		want := []float64{tt.ml, tt.mr, tt.pl, tt.pr, tt.bl, tt.br}
		for i := range got {
			if !near(got[i], want[i]*Pt) {
				t.Errorf("%s: sides %v, want %v px", tt.id, got, want)
				break
			}
		}
		if (s.Left.Kind == Points) != tt.left || (s.Right.Kind == Points) != tt.right {
			t.Errorf("%s: inset left %+v right %+v", tt.id, s.Left, s.Right)
		}
	}
}

func TestCascade_InlineLogicalFollowsDirection(t *testing.T) {
	t.Parallel()
	s, err := Parse(context.Background(), []byte(`
		.a { padding-inline-start: 10px; margin-inline-end: 20px }
		.b { padding-inline-start: 10px; padding-left: 30px }
		.c { padding-left: 30px; padding-inline-start: 10px }
		.d { padding-inline: 1px 2px; inset-inline-start: 3px }
		.e { margin-inline-start: var(--m) }
	`), ParseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	doc := parseHTML(t, `<div id=l class=a></div><div style="direction: rtl"><p id=r class=a></p><p id=b class=b></p><p id=c class=c></p><p id=d class=d style="--m: 4px"></p><p id=e class=e style="--m: 4px"></p></div><div id=s class=a style="direction: rtl"></div>`)
	c, err := NewResolver(a4, []Sheet{{Sheet: s, Origin: Author}}).Compute(context.Background(), doc, ComputeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	px := func(v float64) Dimension { return pts(v * 0.75) }
	tests := []struct {
		id          string
		left, right Dimension
		pad         bool
	}{
		{"l", px(10), pts(0), true},
		{"r", pts(0), px(10), true},
		{"s", pts(0), px(10), true},
		{"b", px(30), px(10), true},
		{"c", px(30), px(10), true},
		{"d", px(2), px(1), true},
		{"e", pts(0), px(4), false},
	}
	for _, tt := range tests {
		st := c.Style(byID(doc, tt.id))
		l, r := st.Margin[Left], st.Margin[Right]
		if tt.pad {
			l, r = st.Padding[Left], st.Padding[Right]
		}
		if !dimEq(l, tt.left) || !dimEq(r, tt.right) {
			t.Errorf("%s: left %+v right %+v, want %+v and %+v", tt.id, l, r, tt.left, tt.right)
		}
	}
	if st := c.Style(byID(doc, "r")); !dimEq(st.Margin[Left], px(20)) {
		t.Errorf("r: margin-inline-end set margin-left %+v", st.Margin[Left])
	}
	if st := c.Style(byID(doc, "d")); !dimEq(st.Right, px(3)) {
		t.Errorf("d: inset-inline-start set right %+v", st.Right)
	}
}

// TestLogical_WritingModes holds the flow-relative properties to CSS
// Logical Properties 1, 4 in each writing mode and direction (CSS Writing
// Modes 4, 6.1 and 6.2): the sides, the sizes and the corners they map
// onto. A box inside a vertical one maps by its own writing mode.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 (gotenberg/gotenberg:9.4.1) computes the
//     same margins, sizes and radii for every box, the inner one mapped as
//     vertical-rl.
func TestLogical_WritingModes(t *testing.T) {
	t.Parallel()
	const decls = `margin-block-start: 1px; margin-inline-start: 2px; inline-size: 30px; min-block-size: 40px;
		border-start-end-radius: 5px; border-end-start-radius: 6px`
	doc := `<div id=htb></div><div id=hrtl dir=rtl></div><div id=vrl></div><div id=vrlrtl dir=rtl></div>
		<div id=vlr></div><div id=slr></div><div id=srl></div><div id=outer><div id=inner></div></div>`
	c := compute(t, doc, `div { `+decls+` }
		#vrl, #vrlrtl, #outer { writing-mode: vertical-rl } #vlr { writing-mode: vertical-lr }
		#slr { writing-mode: sideways-lr } #srl { writing-mode: sideways-rl }`)
	tests := []struct {
		id                 string
		blockStart, inline Side
		widthIsInline      bool
		startEnd, endStart Corner
	}{
		{"htb", Top, Left, true, TopRight, BottomLeft},
		{"hrtl", Top, Right, true, TopLeft, BottomRight},
		{"vrl", Right, Top, false, BottomRight, TopLeft},
		{"vrlrtl", Right, Bottom, false, TopRight, BottomLeft},
		{"vlr", Left, Top, false, BottomLeft, TopRight},
		{"slr", Left, Bottom, false, TopLeft, BottomRight},
		{"srl", Right, Top, false, BottomRight, TopLeft},
		{"inner", Right, Top, false, BottomRight, TopLeft},
	}
	for _, tt := range tests {
		s := c.of(tt.id)
		for sd := Top; sd <= Left; sd++ {
			want := 0.0
			switch sd {
			case tt.blockStart:
				want = 1
			case tt.inline:
				want = 2
			}
			if !near(s.Margin[sd].Value, want*Pt) {
				t.Errorf("%s: margin %v %v, want %v px", tt.id, sd, s.Margin[sd].Value/Pt, want)
			}
		}
		w, mh := s.Width, s.MinHeight
		if !tt.widthIsInline {
			w, mh = s.Height, s.MinWidth
		}
		if !near(w.Value, 30*Pt) || !near(mh.Value, 40*Pt) {
			t.Errorf("%s: inline size %v, min block size %v", tt.id, w.Value/Pt, mh.Value/Pt)
		}
		for corner := TopLeft; corner <= BottomLeft; corner++ {
			want := 0.0
			switch corner {
			case tt.startEnd:
				want = 5
			case tt.endStart:
				want = 6
			}
			if !near(s.BorderRadius[corner].H.Value, want*Pt) {
				t.Errorf("%s: corner %v radius %v, want %v px", tt.id, corner, s.BorderRadius[corner].H.Value/Pt, want)
			}
		}
	}
}

// TestLogical_LaterDeclarationWins holds that a flow-relative and a
// physical declaration of the same side compete in cascade order (CSS
// Logical Properties 1, 4): the later one wins, whichever kind it is.
func TestLogical_LaterDeclarationWins(t *testing.T) {
	t.Parallel()
	c := compute(t, `<div id=a></div><div id=b></div><div id=c></div>`, `
		#a { border-start-start-radius: 1px; border-top-left-radius: 2px }
		#b { border-top-left-radius: 2px; border-start-start-radius: 1px }
		#c { writing-mode: vertical-rl; margin-right: 2px; margin-block-start: 1px; inline-size: 5px; height: 7px }`)
	if got := c.of("a").BorderRadius[TopLeft].H.Value; !near(got, 2*Pt) {
		t.Errorf("a: %v px, want 2", got/Pt)
	}
	if got := c.of("b").BorderRadius[TopLeft].H.Value; !near(got, 1*Pt) {
		t.Errorf("b: %v px, want 1", got/Pt)
	}
	if s := c.of("c"); !near(s.Margin[Right].Value, 1*Pt) || !near(s.Height.Value, 7*Pt) {
		t.Errorf("c: margin-right %v px, height %v px, want 1 and 7", s.Margin[Right].Value/Pt, s.Height.Value/Pt)
	}
}

// TestLogical_FloatAndClear holds float and clear inline-start and
// inline-end to CSS Logical Properties 1, 2.1: they take the side of the
// containing block's direction.
func TestLogical_FloatAndClear(t *testing.T) {
	t.Parallel()
	c := compute(t, `<div><p id=a></p><p id=b></p></div><div dir=rtl><p id=c></p><p id=d dir=ltr></p></div>`, `
		#a, #c, #d { float: inline-start; clear: inline-end } #b { float: inline-end; clear: inline-start }`)
	tests := []struct {
		id string
		f  Float
		c  Clear
	}{
		{"a", FloatLeft, ClearRight},
		{"b", FloatRight, ClearLeft},
		{"c", FloatRight, ClearLeft},
		{"d", FloatRight, ClearLeft},
	}
	for _, tt := range tests {
		if s := c.of(tt.id); s.Float != tt.f || s.Clear != tt.c {
			t.Errorf("%s: float %v clear %v, want %v %v", tt.id, s.Float, s.Clear, tt.f, tt.c)
		}
	}
}
