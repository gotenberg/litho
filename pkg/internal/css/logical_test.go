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
