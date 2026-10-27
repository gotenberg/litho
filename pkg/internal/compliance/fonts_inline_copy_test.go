package compliance

import (
	"context"
	"fmt"
	"maps"
	"os"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// sparseObjs returns the objects of a PDF whose object n is objs[n], every
// number up to the highest one given holding null otherwise.
func sparseObjs(objs map[int]string) []string {
	highest := 0
	for n := range objs {
		highest = max(highest, n)
	}
	out := make([]string, highest)
	for n := 1; n <= highest; n++ {
		out[n-1] = "null"
		if o, ok := objs[n]; ok {
			out[n-1] = o
		}
	}
	return out
}

// inlineFontSubObjects returns the objects the fonts of [inlineFontLayouts]
// point at: descriptors 40 and 41, the CFF program 42 of a Type1 subset, and a
// descendant CIDFont 43 over the TrueType program 45 through descriptor 44.
func inlineFontSubObjects(t *testing.T) map[int]string {
	t.Helper()
	cff, err := os.ReadFile("testdata/lato_bold_subset.cff")
	if err != nil {
		t.Fatalf("read CFF fixture: %v", err)
	}
	ttf, err := os.ReadFile("testdata/cidfont_notdef_min.ttf")
	if err != nil {
		t.Fatalf("read TTF fixture: %v", err)
	}
	const descriptor = "<< /Type /FontDescriptor /FontName /%s /Flags 32 /Ascent 700 /Descent -200 /CapHeight 700 " +
		"/StemV 80 /FontBBox [0 0 500 700] /ItalicAngle 0%s >>"
	return map[int]string{
		1:  "<< /Type /Catalog /Pages 2 0 R >>",
		40: fmt.Sprintf(descriptor, "Helvetica", ""),
		41: fmt.Sprintf(descriptor, "ABCDEF+Lato-Bold", " /FontFile3 42 0 R"),
		42: exponentStream("/Subtype /Type1C", string(cff)),
		43: "<< /Type /Font /Subtype /CIDFontType2 /BaseFont /ABCDEF+Arial /DW 1000 /CIDToGIDMap /Identity " +
			"/FontDescriptor 44 0 R /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> >>",
		44: fmt.Sprintf(descriptor, "ABCDEF+Arial", " /FontFile2 45 0 R"),
		45: exponentStream("", string(ttf)),
	}
}

// inlineFontLayouts returns, by name, the objects of documents that show text
// through font, a font dictionary written directly in the resources, in every
// way a document can write or share it. Each shows shows once per page or
// form.
func inlineFontLayouts(font, shows string) map[string]map[int]string {
	text := func(name string) string { return "BT /" + name + " 12 Tf 10 50 Td " + shows + " ET" }
	page := func(res, contents string) string {
		if res != "" {
			res = " /Resources " + res
		}
		return "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100]" + res + " /Contents " + contents + " >>"
	}
	form := func(res string) string {
		return exponentStream("/Type /XObject /Subtype /Form /BBox [0 0 200 100] /Resources "+res, text("F1"))
	}
	const (
		onePage  = "<< /Type /Pages /Kids [10 0 R] /Count 1 >>"
		twoPages = "<< /Type /Pages /Kids [10 0 R 11 0 R] /Count 2 >>"
	)
	res := "<< /Font << /F1 " + font + " >> >>"
	shown := exponentStream("", text("F1"))
	oneForm := "<< /XObject << /X1 32 0 R >> >>"
	twoForms := "<< /XObject << /X1 32 0 R /X2 33 0 R >> >>"
	return map[string]map[int]string{
		"one page":                    {2: onePage, 10: page(res, "20 0 R"), 20: shown},
		"two pages, a copy each":      {2: twoPages, 10: page(res, "20 0 R"), 11: page(res, "21 0 R"), 20: shown, 21: shown},
		"two pages sharing resources": {2: twoPages, 10: page("30 0 R", "20 0 R"), 11: page("30 0 R", "21 0 R"), 20: shown, 21: shown, 30: res},
		"two pages sharing a font dictionary": {
			2: twoPages, 10: page("<< /Font 31 0 R >>", "20 0 R"), 11: page("<< /Font 31 0 R >>", "21 0 R"),
			20: shown, 21: shown, 31: "<< /F1 " + font + " >>",
		},
		"two pages inheriting resources": {
			2:  "<< /Type /Pages /Kids [10 0 R 11 0 R] /Count 2 /Resources " + res + " >>",
			10: page("", "20 0 R"), 11: page("", "21 0 R"), 20: shown, 21: shown,
		},
		"an indirect font, then a copy of the same name": {
			2: twoPages, 10: page("<< /Font << /F1 50 0 R >> >>", "20 0 R"), 11: page(res, "21 0 R"), 20: shown, 21: shown, 50: font,
		},
		"a copy, then an indirect font of the same name": {
			2: twoPages, 10: page(res, "20 0 R"), 11: page("<< /Font << /F1 50 0 R >> >>", "21 0 R"), 20: shown, 21: shown, 50: font,
		},
		"two names on one page": {
			2: onePage, 10: page("<< /Font << /F1 "+font+" /F2 "+font+" >> >>", "20 0 R"),
			20: exponentStream("", text("F1")+" "+text("F2")),
		},
		"one form on two pages": {
			2: twoPages, 10: page(oneForm, "20 0 R"), 11: page(oneForm, "21 0 R"),
			20: exponentStream("", "/X1 Do"), 21: exponentStream("", "/X1 Do"), 32: form(res),
		},
		"two forms, a copy each": {2: onePage, 10: page(twoForms, "20 0 R"), 20: exponentStream("", "/X1 Do /X2 Do"), 32: form(res), 33: form(res)},
		"two forms sharing resources": {
			2: onePage, 10: page(twoForms, "20 0 R"), 20: exponentStream("", "/X1 Do /X2 Do"),
			30: res, 32: form("30 0 R"), 33: form("30 0 R"),
		},
	}
}

// inlineFontCopies is how many font objects veraPDF tells apart in each of
// [inlineFontLayouts]: one per dictionary that writes the font.
var inlineFontCopies = map[string]int{
	"one page":                                       1,
	"two pages, a copy each":                         2,
	"two pages sharing resources":                    1,
	"two pages sharing a font dictionary":            1,
	"two pages inheriting resources":                 1,
	"an indirect font, then a copy of the same name": 2,
	"a copy, then an indirect font of the same name": 2,
	"two names on one page":                          2,
	"one form on two pages":                          1,
	"two forms, a copy each":                         2,
	"two forms sharing resources":                    1,
}

// TestInlineFontJudgedPerCopy pins that a font dictionary written directly in
// the resources is judged once per dictionary that writes it, however many
// pages or forms reach that dictionary; see [fontIdentity].
//
// Every count was measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b: the
// embedding rule of an unembedded Helvetica, the width rule of a Type1 subset
// whose /Widths disagree with its program for both codes shown, the /CharSet
// rule of that subset, and the glyph-presence and .notdef rules of a Type0
// font showing CID 256 of a 38-glyph program, whose shared descendant fails
// the /CIDSet rule once.
func TestInlineFontJudgedPerCopy(t *testing.T) {
	t.Parallel()

	const (
		unembedded = "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /FirstChar 65 /LastChar 66 " +
			"/Widths [600 600] /Encoding /WinAnsiEncoding /FontDescriptor 40 0 R >>"
		wrongWidths = "<< /Type /Font /Subtype /Type1 /BaseFont /ABCDEF+Lato-Bold /FirstChar 65 /LastChar 66 " +
			"/Widths [100 100] /Encoding /WinAnsiEncoding /FontDescriptor 41 0 R >>"
		composite = "<< /Type /Font /Subtype /Type0 /BaseFont /ABCDEF+Arial /Encoding /Identity-H /DescendantFonts [43 0 R] >>"
	)
	type count struct {
		rule    Rule
		perCopy int // failures per font object
		once    bool
	}
	fonts := []struct {
		name, font, shows string
		profile           models.PDFAProfile
		counts            []count
	}{
		{"unembedded", unembedded, "(AB) Tj", models.PDFA1b, []count{{rule: Rule_19005_1_6_3_4_t01, perCopy: 1}}},
		{"unembedded", unembedded, "(AB) Tj", models.PDFA2b, []count{{rule: Rule_19005_2_6_2_11_4_1_t01, perCopy: 1}}},
		{"wrong widths", wrongWidths, "(AB) Tj", models.PDFA1b, []count{
			{rule: Rule_19005_1_6_3_6_t01, perCopy: 2}, {rule: Rule_19005_1_6_3_5_t02, perCopy: 1},
		}},
		{"wrong widths", wrongWidths, "(AB) Tj", models.PDFA2b, []count{{rule: Rule_19005_2_6_2_11_5_t01, perCopy: 2}}},
		{"composite", composite, "<0100> Tj", models.PDFA1b, []count{
			{rule: Rule_19005_1_6_3_5_t01, perCopy: 1}, {rule: Rule_19005_1_6_3_5_t03, once: true},
		}},
		{"composite", composite, "<0100> Tj", models.PDFA2b, []count{
			{rule: Rule_19005_2_6_2_11_4_1_t02, perCopy: 1}, {rule: Rule_19005_2_6_2_11_8_t01, perCopy: 1},
		}},
	}
	for _, f := range fonts {
		for layout, objs := range inlineFontLayouts(f.font, f.shows) {
			t.Run(string(f.profile)+" "+f.name+" "+layout, func(t *testing.T) {
				t.Parallel()
				all := inlineFontSubObjects(t)
				maps.Copy(all, objs)
				doc, _ := objsDocPDF(t, sparseObjs(all)...)
				viols, err := ValidatePDFA(context.Background(), doc, f.profile)
				if err != nil {
					t.Fatalf("ValidatePDFA: %v", err)
				}
				for _, c := range f.counts {
					want := c.perCopy * inlineFontCopies[layout]
					if c.once {
						want = 1
					}
					if got := countRuleErrs(t, viols, c.rule); got != want {
						t.Errorf("%s test %d fired %d times, want %d", c.rule.Clause, c.rule.TestNumber, got, want)
					}
				}
			})
		}
	}
}

// TestDescendantRulesOncePerDescendant pins that the rules veraPDF states on a
// composite font's descendant CIDFont, its embedding and its /CIDToGIDMap, are
// stated once per descendant object, while the /CIDSystemInfo rule, stated on
// the composite font, counts every composite font; see
// [fontJudgements.descendantRules]. Every count was measured with veraPDF 1.30
// at PDF/A-1b and PDF/A-2b on a Type0 font whose unembedded descendant has no
// /CIDToGIDMap and an Adobe-Korea1 /CIDSystemInfo under UniJIS-UCS2-H; only
// PDF/A-1b states the /CIDToGIDMap rule for a descendant that embeds nothing.
func TestDescendantRulesOncePerDescendant(t *testing.T) {
	t.Parallel()

	const descendant = "<< /Subtype /CIDFontType2 /BaseFont /FooBar /DW 1000 /FontDescriptor 47 0 R " +
		"/CIDSystemInfo << /Registry (Adobe) /Ordering (Korea1) /Supplement 0 >> >>"
	type0 := func(desc string) string {
		return "<< /Type /Font /Subtype /Type0 /BaseFont /FooBar /Encoding /UniJIS-UCS2-H /DescendantFonts [" + desc + "] >>"
	}
	const (
		text     = "BT /F1 12 Tf 10 50 Td <00410042> Tj ET"
		twoTexts = text + " BT /F2 12 Tf 10 50 Td <00410042> Tj ET"
		twoPages = "<< /Type /Pages /Kids [10 0 R 11 0 R] /Count 2 >>"
		onePage  = "<< /Type /Pages /Kids [10 0 R] /Count 1 >>"
	)
	page := func(fonts, contents string) string {
		return "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources << /Font << " + fonts +
			" >> >> /Contents " + contents + " >>"
	}
	layouts := []struct {
		name string
		objs func(font string) map[int]string
	}{
		{"two pages, a Type0 copy each", func(font string) map[int]string {
			return map[int]string{
				2: twoPages, 10: page("/F1 "+font, "20 0 R"), 11: page("/F1 "+font, "21 0 R"),
				20: exponentStream("", text), 21: exponentStream("", text),
			}
		}},
		{"two Type0 objects on one page", func(font string) map[int]string {
			return map[int]string{
				2: onePage, 10: page("/F1 50 0 R /F2 51 0 R", "20 0 R"), 20: exponentStream("", twoTexts), 50: font, 51: font,
			}
		}},
	}
	for _, p := range []struct {
		profile                         models.PDFAProfile
		embedding, cidToGIDMap, sysInfo Rule
		// mapFails is 0 at PDF/A-2b, whose /CIDToGIDMap test passes a
		// descendant that embeds no program.
		mapFails int
	}{
		{models.PDFA1b, Rule_19005_1_6_3_4_t01, Rule_19005_1_6_3_3_2_t01, Rule_19005_1_6_3_3_1_t01, 1},
		{models.PDFA2b, Rule_19005_2_6_2_11_4_1_t01, Rule_19005_2_6_2_11_3_2_t01, Rule_19005_2_6_2_11_3_1_t01, 0},
	} {
		for _, d := range []struct {
			name        string
			font        string
			descendants int
		}{
			{"one descendant object", type0("46 0 R"), 1},
			{"a descendant written in each", type0(descendant), 2},
		} {
			for _, l := range layouts {
				t.Run(strings.Join([]string{string(p.profile), d.name, l.name}, " "), func(t *testing.T) {
					t.Parallel()
					all := map[int]string{
						1:  "<< /Type /Catalog /Pages 2 0 R >>",
						46: descendant,
						47: "<< /Type /FontDescriptor /FontName /FooBar /Flags 32 /Ascent 700 /Descent -200 " +
							"/CapHeight 700 /StemV 80 /FontBBox [0 0 500 700] /ItalicAngle 0 >>",
					}
					maps.Copy(all, l.objs(d.font))
					doc, _ := objsDocPDF(t, sparseObjs(all)...)
					viols, err := ValidatePDFA(context.Background(), doc, p.profile)
					if err != nil {
						t.Fatalf("ValidatePDFA: %v", err)
					}
					for _, c := range []struct {
						rule Rule
						want int
					}{{p.embedding, d.descendants}, {p.cidToGIDMap, p.mapFails * d.descendants}, {p.sysInfo, 2}} {
						if got := countRuleErrs(t, viols, c.rule); got != c.want {
							t.Errorf("%s test %d fired %d times, want %d", c.rule.Clause, c.rule.TestNumber, got, c.want)
						}
					}
				})
			}
		}
	}
}
