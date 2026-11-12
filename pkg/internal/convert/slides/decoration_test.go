package slides

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// TestInline_DecorationStyles locks how a run's underline style, heavy
// underline and double strike reach the layout's decoration (ECMA-376
// Part 1, 20.1.10.82 ST_TextUnderlineType, 20.1.10.79 ST_TextStrikeType).
func TestInline_DecorationStyles(t *testing.T) {
	t.Parallel()
	b := &builder{}
	ps := css.Initial()
	for _, tc := range []struct {
		name  string
		rs    RunStyle
		lines css.TextDecoration
		style css.DecorationStyle
		thick css.Dimension
	}{
		{"single", RunStyle{Size: 10, Underline: true}, css.Underline, css.DecorationSolid, css.Initial().TextDecorationThickness},
		{"heavy dotted", RunStyle{Size: 10, Underline: true, UnderlineStyle: css.DecorationDotted, UnderlineHeavy: true}, css.Underline, css.DecorationDotted, css.Pts(1)},
		{"double", RunStyle{Size: 10, Underline: true, UnderlineStyle: css.DecorationDouble}, css.Underline, css.DecorationDouble, css.Pts(0.4)},
		{"double strike", RunStyle{Size: 10, Strike: true, DoubleStrike: true}, css.LineThrough, css.DecorationDouble, css.Initial().TextDecorationThickness},
		{"double strike under a wavy underline", RunStyle{Size: 10, Underline: true, UnderlineStyle: css.DecorationWavy, Strike: true, DoubleStrike: true}, css.Underline | css.LineThrough, css.DecorationWavy, css.Initial().TextDecorationThickness},
	} {
		st := b.inline(ps, &tc.rs, 1)
		if st.TextDecoration != tc.lines || st.TextDecorationStyle != tc.style || st.TextDecorationThickness != tc.thick {
			t.Errorf("%s: lines %v style %v thickness %+v, want %v %v %+v", tc.name, st.TextDecoration, st.TextDecorationStyle, st.TextDecorationThickness, tc.lines, tc.style, tc.thick)
		}
	}
}
