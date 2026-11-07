package layout

import (
	"context"
	"os"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/text"
)

// fixtureLiberation is the pinned Liberation directory, whose Serif face
// kerns A and V. Ahem, the one font of the repository the layout tests
// draw with, has no kerning.
const fixtureLiberation = "../../../test/integration/testdata/fonts/liberation-2.1.5"

// TestFontKerning lays a kerned pair out with font-kerning normal and none
// on a block and on an inline box inside it, which inherits the block's:
// none places the second glyph at the first's natural advance.
func TestFontKerning(t *testing.T) {
	if _, err := os.Stat(fixtureLiberation); err != nil {
		t.Skipf("fixture fonts absent: %v", err)
	}
	fonts, err := text.NewFonts(context.Background(), nil, []string{fixtureLiberation})
	if err != nil {
		t.Fatal(err)
	}
	serif := func(k css.FontKerning) func(*css.Style) {
		return func(s *css.Style) {
			s.FontFamilies = []string{"Liberation Serif"}
			s.FontSize = 100
			s.FontKerning = k
		}
	}
	vAt := func(k css.FontKerning, inline bool) float64 {
		block := sty(serif(k))
		kids := []*Node{txt(block, "AV")}
		if inline {
			span := inlineSty(serif(k))
			kids = []*Node{el(span, txt(span, "AV"))}
		}
		res, err := Layout(context.Background(), el(sty(), el(block, kids...)), fonts, Options{Page: page(1000, 500)})
		if err != nil {
			t.Fatal(err)
		}
		r := texts(res, 1)[0].run
		return r.Glyphs[1].X
	}
	tests := []struct {
		name   string
		inline bool
	}{
		{"block", false},
		{"inline box", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kerned, natural := vAt(css.KerningNormal, tt.inline), vAt(css.KerningNone, tt.inline)
			auto := vAt(css.KerningAuto, tt.inline)
			// Liberation Serif's A is 72.22 points wide at 100 points and
			// the A-V pair kerns by about 13.
			if !near(natural, 72.216796875) || !(natural-kerned > 10) || !near(auto, kerned) {
				t.Errorf("V at %v kerned, %v auto, %v unkerned", kerned, auto, natural)
			}
		})
	}
}
