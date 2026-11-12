package odt

import (
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
)

// TestScriptTextProperties checks that each script of a paragraph prints
// in its own size: the Western, Asian and complex sizes a style sets,
// Writer's 10.5 and 12 points for the Asian and complex text a style
// leaves, whatever fo:font-size says. The Hebrew word reads in the order
// it is drawn.
//
// # Corpus evidence
//
//   - probe, odt: through gotenberg/gotenberg:9.4.1, "abc 漢字 עברית def
//     12" under sizes of 10, 20 and 30 points prints "abc " at 10, "漢字 "
//     at 20, "עברית " at 30 and "def 12" at 10; under a style setting
//     fo:font-size 20pt alone, "abc 漢字 עברית" prints at 20, 10.5 and 12.
func TestScriptTextProperties(t *testing.T) {
	for _, tc := range []struct {
		name, props string
		want        map[string]float64
	}{
		{"three sizes", `fo:font-size="10pt" style:font-size-asian="20pt" style:font-size-complex="30pt"`,
			map[string]float64{"abc": 10, "漢字": 20, "תירבע": 30, "12": 10}},
		{"western size alone", `fo:font-size="20pt"`,
			map[string]float64{"abc": 20, "漢字": 10.5, "תירבע": 12, "12": 20}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := doc{
				styles: `<style:style style:name="P" style:family="paragraph"><style:text-properties ` + tc.props + `/></style:style>`,
				body:   `<text:p text:style-name="P">abc 漢字 עברית def 12</text:p>`,
			}
			out := convertDoc(t, d.pack(t), office.Options{})
			got := map[string]float64{}
			for _, r := range runs(out.Pages[0]) {
				for word := range tc.want {
					if strings.Contains(r.text, word) {
						got[word] = r.size
					}
				}
			}
			for word, size := range tc.want {
				if !near(got[word], size, 0.01) {
					t.Errorf("%q prints at %v points, want %v", word, got[word], size)
				}
			}
		})
	}
}
