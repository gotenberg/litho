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

// TestPercentFontSizes checks the font sizes in percent of each script
// as Writer reads them: a common paragraph style's multiplies its parent
// style's size, a common text style's the size of its own chain from the
// default paragraph style's, whatever the paragraph sets; an automatic
// style's leaves the size below it (see [stylePercent], which holds the
// measurements).
func TestPercentFontSizes(t *testing.T) {
	size := func(name, family, parent, w, a, c string) string {
		if parent != "" {
			parent = ` style:parent-style-name="` + parent + `"`
		}
		return `<style:style style:name="` + name + `" style:family="` + family + `"` + parent + `><style:text-properties fo:font-size="` + w +
			`" style:font-size-asian="` + a + `" style:font-size-complex="` + c + `"/></style:style>`
	}
	common := size("P20", "paragraph", "Standard", "20pt", "20pt", "20pt") +
		size("C50", "paragraph", "P20", "50%", "50%", "50%") +
		size("C200", "paragraph", "C50", "200%", "200%", "200%") +
		size("N50", "text", "", "50%", "50%", "50%")
	auto := size("A50", "paragraph", "P20", "50%", "50%", "50%") +
		size("T50", "text", "", "50%", "50%", "50%")
	const words = "abc 漢字 עברית"
	for _, tc := range []struct {
		name, body string
		want       [3]float64
	}{
		{"common style under a 20-point parent", `<text:p text:style-name="C50">` + words + `</text:p>`, [3]float64{10, 10, 10}},
		{"common style under a percentage parent", `<text:p text:style-name="C200">` + words + `</text:p>`, [3]float64{20, 20, 20}},
		{"automatic paragraph style", `<text:p text:style-name="A50">` + words + `</text:p>`, [3]float64{20, 20, 20}},
		{"automatic text style", `<text:p text:style-name="P20"><text:span text:style-name="T50">` + words + `</text:span></text:p>`, [3]float64{20, 20, 20}},
		{"common text style", `<text:p text:style-name="P20"><text:span text:style-name="N50">` + words + `</text:span></text:p>`, [3]float64{6, 5.25, 6}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := doc{styles: common, contentAuto: auto, body: tc.body}
			out := convertDoc(t, d.pack(t), office.Options{})
			got := map[string]float64{}
			for _, r := range runs(out.Pages[0]) {
				for _, word := range []string{"abc", "漢字", "תירבע"} {
					if strings.Contains(r.text, word) {
						got[word] = r.size
					}
				}
			}
			for i, word := range []string{"abc", "漢字", "תירבע"} {
				if !near(got[word], tc.want[i], 0.01) {
					t.Errorf("%q prints at %v points, want %v", word, got[word], tc.want[i])
				}
			}
		})
	}
}

// TestRuby checks that a ruby annotation prints at 6 points centred above
// its base, its descent on the base's ascent, and below it under
// style:ruby-position below; no warning.
//
// # Corpus evidence
//
//   - litho/odt: ja-ruby prints "にんげん" at 6 points over "人間" at 12,
//     both 24 points wide, starting where the base starts, its baseline
//     15.65 points above the base's in Noto Sans CJK JP.
func TestRuby(t *testing.T) {
	auto := `<style:style style:name="B" style:family="ruby"><style:ruby-properties style:ruby-position="below"/></style:style>`
	body := `<text:p>ab<text:ruby><text:ruby-base>xx</text:ruby-base><text:ruby-text>yyyy</text:ruby-text></text:ruby>cd</text:p>` +
		`<text:p>ab<text:ruby text:style-name="B"><text:ruby-base>xx</text:ruby-base><text:ruby-text>yyyy</text:ruby-text></text:ruby>cd</text:p>`
	out := convertDoc(t, doc{contentAuto: auto, body: body}.pack(t), office.Options{})
	if len(out.Warnings) != 0 {
		t.Errorf("warnings = %+v, want none", out.Warnings)
	}
	var bases, annots []run
	for _, r := range runs(out.Pages[0]) {
		switch r.text {
		case "xx":
			bases = append(bases, r)
		case "yyyy":
			annots = append(annots, r)
		}
	}
	if len(bases) != 2 || len(annots) != 2 {
		t.Fatalf("bases %+v, annotations %+v", bases, annots)
	}
	for i, above := range []bool{true, false} {
		b, a := bases[i], annots[i]
		if a.size != rubySize {
			t.Errorf("annotation %d at %v points, want %v", i, a.size, rubySize)
		}
		// "xx" at 12 points and "yyyy" at 6 are both 12 points wide in
		// Liberation Serif: the annotation starts where its base does.
		if !near(a.x, b.x, 0.05) {
			t.Errorf("annotation %d at x %.2f, base at %.2f", i, a.x, b.x)
		}
		if (a.y < b.y) != above {
			t.Errorf("annotation %d at y %.2f, base at %.2f, want above %v", i, a.y, b.y, above)
		}
	}
}

// TestVerticalPageWarns checks that a page layout of vertical writing
// mode, laid out horizontally, is reported.
func TestVerticalPageWarns(t *testing.T) {
	pl := `<style:page-layout style:name="pv"><style:page-layout-properties fo:page-width="21cm" fo:page-height="29.7cm" style:writing-mode="tb-rl"/></style:page-layout>`
	d := doc{autoStyles: pl, masters: `<style:master-page style:name="Standard" style:page-layout-name="pv"/>`, body: `<text:p>縦書き</text:p>`}
	out := convertDoc(t, d.pack(t), office.Options{})
	for _, w := range out.Warnings {
		if strings.Contains(w.Message, "vertical writing mode") {
			return
		}
	}
	t.Errorf("warnings = %+v, want the vertical page reported", out.Warnings)
}
