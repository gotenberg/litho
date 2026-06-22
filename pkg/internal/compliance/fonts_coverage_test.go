package compliance

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestCheckCharSetRenderedGlyphs covers the part-1 §6.3.5 t2 rendered-glyph
// CharSet check: only a glyph that is rendered, defined by the program, and
// absent from /CharSet is reported. WinAnsiEncoding maps code 65→A, 66→B,
// 67→C, 120→x. The synthetic program defines A, B, x (not C).
func TestCheckCharSetRenderedGlyphs(t *testing.T) {
	t.Parallel()
	ctx := testCtx()
	doc := nopDoc()
	fontDict := ast.PdfDict{"Encoding": ast.PdfName("WinAnsiEncoding")}
	glyphNames := map[uint16]string{0: ".notdef", 1: "A", 2: "B", 3: "x"}
	rule := Rule_19005_1_6_3_5_t02

	usage := func(codes ...byte) *fontUsageInfo {
		u := &fontUsageInfo{charCodes: map[byte]bool{}}
		for _, c := range codes {
			u.charCodes[c] = true
		}
		return u
	}
	fires := func(charSet string, u *fontUsageInfo) bool {
		return len(checkCharSetRenderedGlyphs(ctx, doc, fontDict, glyphNames, charSet, "F", 1, rule, u)) > 0
	}

	tests := []struct {
		name    string
		charSet string
		usage   *fontUsageInfo
		want    bool
	}{
		{"rendered glyph listed", "/A/B", usage(65), false},
		{"rendered glyph missing from CharSet", "/B", usage(65), true},
		{"rendered extra glyph missing from CharSet", "/A/B", usage(65, 66, 120), true},
		{"unused program glyph not flagged", "/A/B", usage(65, 66), false},
		{"encoding name absent from program is skipped", "/A/B", usage(67), false},
		{"nil usage defers", "/B", nil, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := fires(tc.charSet, tc.usage); got != tc.want {
				t.Errorf("checkCharSetRenderedGlyphs(%q, %v) fired=%v, want %v",
					tc.charSet, tc.usage, got, tc.want)
			}
		})
	}
}
