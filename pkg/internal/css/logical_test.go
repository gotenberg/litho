package css

import "testing"

func TestLogical_InlineSidesFollowDirection(t *testing.T) {
	c := compute(t, `<div id=ltr></div><div id=rtl dir=rtl></div><div dir=rtl><p id=inh></p></div><div id=late></div><div id=phys dir=rtl></div>`, `
div, p { margin-inline-start: 5px; padding-inline: 1px 2px; border-inline-start: 3px solid red; inset-inline-end: 4px }
#late { direction: rtl }
#phys { margin-left: 9px }`)
	tests := []struct {
		id                   string
		ml, mr, pl, pr, bl, br float64
		left, right          bool
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
