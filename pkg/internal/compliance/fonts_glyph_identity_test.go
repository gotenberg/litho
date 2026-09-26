package compliance

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// glyphPagesObjs returns the objects of a PDF with one page per body, each
// naming the font of object 5 /F1 and drawing its body as its own content
// stream. The font takes objects 5 to 9, and the pages start at object 10,
// their streams at object 20.
func glyphPagesObjs(font []string, bodies ...string) []string {
	kids := make([]string, len(bodies))
	for i := range bodies {
		kids[i] = fmt.Sprintf("%d 0 R", 10+i)
	}
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), len(bodies)),
		"null",
		"null",
	}
	objs = append(objs, font...)
	for len(objs) < 9 {
		objs = append(objs, "null")
	}
	for i := range bodies {
		objs = append(objs, fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] "+
			"/Resources << /Font << /F1 5 0 R >> >> /Contents %d 0 R >>", 20+i))
	}
	for len(objs) < 19 {
		objs = append(objs, "null")
	}
	for _, b := range bodies {
		objs = append(objs, exponentStream("", b))
	}
	return objs
}

// glyphText wraps shows in a text object that selects /F1.
func glyphText(shows string) string {
	return "BT /F1 12 Tf 10 50 Td " + shows + " ET"
}

// TestGlyphRulesStatedOncePerGlyph pins that a glyph-level rule is stated once
// per glyph veraPDF tells apart, across the whole document; see
// [fontJudgements.glyphs].
//
// Every count was measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b on a
// missing glyph: once for a code two pages show, once per rendering mode other
// than 3 it is shown in (the .notdef rule counts mode 3 too), once per BDC
// sequence carrying its own property list, once for two BMC sequences, and
// once per code.
func TestGlyphRulesStatedOncePerGlyph(t *testing.T) {
	t.Parallel()

	// CID 256 lies past the 38 glyphs of the program, so it is absent from it.
	const missing = "<0100> Tj"
	cases := []struct {
		name   string
		bodies []string
		want   int
	}{
		{"one page", []string{glyphText(missing)}, 1},
		{"two pages", []string{glyphText(missing), glyphText(missing)}, 1},
		{"twice on one page", []string{glyphText(missing + " [<0100>] TJ")}, 1},
		{"two codes", []string{glyphText("<01000101> Tj")}, 2},
		{"rendering modes 0, 2 and 3", []string{glyphText("0 Tr " + missing + " 2 Tr " + missing + " 3 Tr " + missing)}, 2},
		{"only in rendering mode 3", []string{glyphText("3 Tr " + missing)}, 0},
		{"two BDC with inline lists", []string{
			"/P <</MCID 0>> BDC " + glyphText(missing) + " EMC /P <</MCID 1>> BDC " + glyphText(missing) + " EMC",
		}, 2},
		{"two BMC", []string{
			"/Span BMC " + glyphText(missing) + " EMC /Span BMC " + glyphText(missing) + " EMC",
		}, 1},
		{"nested BDC with inline lists", []string{
			"/P <</MCID 0>> BDC /Span <</Lang (en)>> BDC " + glyphText(missing) + " EMC " +
				"/Span <</Lang (fr)>> BDC " + glyphText(missing) + " EMC EMC",
		}, 2},
		{"two BDC around BMC", []string{
			"/P <</MCID 0>> BDC /Span BMC " + glyphText(missing) + " EMC EMC " +
				"/P <</MCID 1>> BDC /Span BMC " + glyphText(missing) + " EMC EMC",
		}, 1},
	}
	for _, p := range []struct {
		profile models.PDFAProfile
		rule    Rule
	}{
		{models.PDFA1b, Rule_19005_1_6_3_5_t01},
		{models.PDFA2b, Rule_19005_2_6_2_11_4_1_t02},
	} {
		for _, tc := range cases {
			t.Run(string(p.profile)+" "+tc.name, func(t *testing.T) {
				t.Parallel()
				doc, _ := objsDocPDF(t, glyphPagesObjs(subsetType0Font(t, ""), tc.bodies...)...)
				viols, err := ValidatePDFA(context.Background(), doc, p.profile)
				if err != nil {
					t.Fatalf("ValidatePDFA: %v", err)
				}
				if got := countRuleErrs(t, viols, p.rule); got != tc.want {
					t.Errorf("%s test %d fired %d times, want %d", p.rule.Clause, p.rule.TestNumber, got, tc.want)
				}
			})
		}
	}
}

// TestGlyphNotdefCountsInvisibleModes pins that the .notdef rule, whose test
// carries no rendering-mode term (ISO 19005-2, 6.2.11.8), states a CID 0 once
// per rendering mode it is shown in, mode 3 included, and once for two pages.
// Measured with veraPDF 1.30 at PDF/A-2b.
func TestGlyphNotdefCountsInvisibleModes(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		bodies []string
		want   int
	}{
		{"rendering modes 0, 2 and 3", []string{glyphText("0 Tr <0000> Tj 2 Tr <0000> Tj 3 Tr <0000> Tj")}, 3},
		{"two pages", []string{glyphText("<0000> Tj"), glyphText("<0000> Tj")}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := objsDocPDF(t, glyphPagesObjs(subsetType0Font(t, ""), tc.bodies...)...)
			viols, err := ValidatePDFA(context.Background(), doc, models.PDFA2b)
			if err != nil {
				t.Fatalf("ValidatePDFA: %v", err)
			}
			if got := countRuleErrs(t, viols, Rule_19005_2_6_2_11_8_t01); got != tc.want {
				t.Errorf("6.2.11.8 test 1 fired %d times, want %d", got, tc.want)
			}
		})
	}
}

// TestGlyphWidthStatedPerCode pins that the width rule is stated for every
// shown code whose declared width disagrees with the program, not for the
// first alone, and once for a code two pages show. veraPDF 1.30 reports each
// such glyph at PDF/A-1b and PDF/A-2b.
func TestGlyphWidthStatedPerCode(t *testing.T) {
	t.Parallel()

	font := subsetType1Font(t)
	// Both A and B advance well past a declared width of 100.
	font[0] = strings.Replace(font[0], "/Widths [600 600]", "/Widths [100 100]", 1)
	for _, p := range []struct {
		profile models.PDFAProfile
		rule    Rule
	}{
		{models.PDFA1b, Rule_19005_1_6_3_6_t01},
		{models.PDFA2b, Rule_19005_2_6_2_11_5_t01},
	} {
		for _, tc := range []struct {
			name   string
			bodies []string
			want   int
		}{
			{"one page", []string{glyphText("(AB) Tj")}, 2},
			{"two pages", []string{glyphText("(AB) Tj"), glyphText("(BA) Tj")}, 2},
			{"rendering modes 0 and 3", []string{glyphText("(A) Tj 3 Tr (A) Tj")}, 1},
		} {
			t.Run(string(p.profile)+" "+tc.name, func(t *testing.T) {
				t.Parallel()
				doc, _ := objsDocPDF(t, glyphPagesObjs(font, tc.bodies...)...)
				viols, err := ValidatePDFA(context.Background(), doc, p.profile)
				if err != nil {
					t.Fatalf("ValidatePDFA: %v", err)
				}
				if got := countRuleErrs(t, viols, p.rule); got != tc.want {
					t.Errorf("%s test %d fired %d times, want %d", p.rule.Clause, p.rule.TestNumber, got, tc.want)
				}
			})
		}
	}
}
