package text

import (
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
)

// testCJK builds a "Test CJK" face whose ideograph and fullwidth
// punctuation are one em wide.
func testCJK() []byte {
	return fontbuild.TrueType{
		PostScriptName: "TestCJK",
		Family:         "Test CJK",
		Subfamily:      "Regular",
		NotdefAdvance:  1000,
		Glyphs: []fontbuild.Glyph{
			{Name: "han", Unicode: '漢', Advance: 1000},
			{Name: "open", Unicode: '「', Advance: 1000},
			{Name: "close", Unicode: '」', Advance: 1000},
			{Name: "comma", Unicode: '、', Advance: 1000},
			{Name: "colon", Unicode: '：', Advance: 1000},
		},
	}.Build()
}

func TestShape_TrimsAdjacentPunctuation(t *testing.T) {
	f := newTestFonts(t, fontFile{name: "cjk.ttf", data: testCJK()})
	s := Style{Families: []string{"Test CJK"}, Size: 10}
	tests := []struct {
		text string
		want []float64
		trim bool
	}{
		{"漢」「漢", []float64{10, 10, 5, 10}, true},
		{"漢、「漢", []float64{10, 10, 5, 10}, true},
		{"漢：「漢", []float64{10, 10, 5, 10}, true},
		{"漢」」漢", []float64{10, 5, 10, 10}, true},
		{"「漢」漢", []float64{10, 10, 10, 10}, true},
		{"漢」「漢", []float64{10, 10, 10, 10}, false},
	}
	for _, tt := range tests {
		st := s
		st.NoSpacingTrim = !tt.trim
		p := shape(t, f, []ParagraphRun{{Text: tt.text, Style: st}}, ParagraphOptions{})
		for i, w := range tt.want {
			if math.Abs(p.Advance(i)-w) > 1e-6 {
				t.Errorf("%q: advance %d = %v, want %v", tt.text, i, p.Advance(i), w)
			}
		}
	}
}
