package layout

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// lineWidths returns the content width of each line of the first block of
// root laid out at w px wide, in px.
func lineWidths(t *testing.T, root *Node, w float64) []float64 {
	t.Helper()
	rb := boxes(t, root, page(w, 100))
	var out []float64
	for _, l := range kid(t, rb, 0, 0).lines {
		out = append(out, contentWidth(l)/css.Pt)
	}
	return out
}

// CSS Text 3, 5.4: a soft hyphen is a hyphenation opportunity under
// hyphens manual and auto, and the line it ends shows the
// hyphenate-character; hyphens: none breaks no word there. A break whose
// hyphen overflows still ends a line nothing else ends.
func TestHyphens_SoftHyphen(t *testing.T) {
	tests := []struct {
		name  string
		set   func(*css.Style)
		width float64
		want  []float64
	}{
		// Ahem 10px: "aaa" and its hyphen, then "bbb".
		{"manual", nil, 45, []float64{40, 30}},
		{"auto", func(s *css.Style) { s.Hyphens = css.HyphensAuto }, 45, []float64{40, 30}},
		{"none", func(s *css.Style) { s.Hyphens = css.HyphensNone }, 45, []float64{60}},
		{"string", func(s *css.Style) { s.HyphenateCharacter = css.HyphenateCharacter{Given: true, Text: "--"} }, 55, []float64{50, 30}},
		{"empty string", func(s *css.Style) { s.HyphenateCharacter = css.HyphenateCharacter{Given: true} }, 45, []float64{30, 30}},
		{"overflowing hyphen", nil, 35, []float64{40, 30}},
		{"no break", nil, 65, []float64{60}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var mods []func(*css.Style)
			if tt.set != nil {
				mods = append(mods, tt.set)
			}
			st := sty(mods...)
			got := lineWidths(t, doc(el(st, txt(st, "aaa­bbb"))), tt.width)
			if len(got) != len(tt.want) {
				t.Fatalf("lines %v, want %v", got, tt.want)
			}
			for i := range got {
				if !near(got[i], tt.want[i]) {
					t.Errorf("lines %v, want %v", got, tt.want)
				}
			}
		})
	}
}
