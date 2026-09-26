package compliance

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// subsetFontObjs returns the objects of a PDF of one page per contents entry,
// each naming the font of object 5 /F1 and taking that entry as its
// /Contents. Objects 3 and 4 are two streams that both show show with /F1,
// and the pages start at object 10.
func subsetFontObjs(font []string, show string, contents ...string) []string {
	kids := make([]string, len(contents))
	for i := range contents {
		kids[i] = fmt.Sprintf("%d 0 R", 10+i)
	}
	text := "BT /F1 12 Tf 10 50 Td " + show + " Tj ET"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), len(contents)),
		exponentStream("", text),
		exponentStream("", text),
	}
	objs = append(objs, font...)
	for len(objs) < 9 {
		objs = append(objs, "null")
	}
	for _, c := range contents {
		objs = append(objs, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] "+
			"/Resources << /Font << /F1 5 0 R >> >> /Contents "+c+" >>")
	}
	return objs
}

// subsetType0Font returns objects 5 to 9 of [subsetFontObjs]: a Type0 subset
// over an embedded TrueType program whose descriptor carries cidSet, and the
// /CIDSet stream object 9, which identifies CID 1 alone.
func subsetType0Font(t *testing.T, cidSet string) []string {
	t.Helper()
	ttf, err := os.ReadFile("testdata/cidfont_notdef_min.ttf")
	if err != nil {
		t.Fatalf("read TTF fixture: %v", err)
	}
	return []string{
		"<< /Type /Font /Subtype /Type0 /BaseFont /ABCDEF+Arial /Encoding /Identity-H /DescendantFonts [6 0 R] >>",
		"<< /Type /Font /Subtype /CIDFontType2 /BaseFont /ABCDEF+Arial /DW 1000 /CIDToGIDMap /Identity " +
			"/FontDescriptor 7 0 R /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> >>",
		"<< /Type /FontDescriptor /FontName /ABCDEF+Arial /Flags 32 /Ascent 700 /Descent -200 /CapHeight 700 " +
			"/StemV 80 /FontBBox [0 0 500 700] /ItalicAngle 0 /FontFile2 8 0 R" + cidSet + " >>",
		exponentStream("", string(ttf)),
		exponentStream("", "\x40\x00\x00\x00\x00\x00\x00\x00"),
	}
}

// subsetType1Font returns objects 5 to 7 of [subsetFontObjs]: a Type1 subset
// over an embedded CFF program, with no /CharSet.
func subsetType1Font(t *testing.T) []string {
	t.Helper()
	cff, err := os.ReadFile("testdata/lato_bold_subset.cff")
	if err != nil {
		t.Fatalf("read CFF fixture: %v", err)
	}
	return []string{
		"<< /Type /Font /Subtype /Type1 /BaseFont /ABCDEF+Lato-Bold /FirstChar 65 /LastChar 66 /Widths [600 600] " +
			"/Encoding /WinAnsiEncoding /FontDescriptor 6 0 R >>",
		"<< /Type /FontDescriptor /FontName /ABCDEF+Lato-Bold /Flags 32 /Ascent 700 /Descent -200 /CapHeight 700 " +
			"/StemV 80 /FontBBox [0 0 500 700] /ItalicAngle 0 /FontFile3 7 0 R >>",
		exponentStream("/Subtype /Type1C", string(cff)),
	}
}

// TestFontSubsetRuleStatedOncePerFont pins that the /CharSet and /CIDSet rules
// are stated once per font object, however many pages show text with it; see
// [fontJudgements.subsetCoverage].
//
// Every case was measured with veraPDF 1.30: a Type1 subset with no /CharSet
// fails ISO 19005-1, 6.3.5 t2 once, a Type0 subset with no /CIDSet fails ISO
// 19005-1, 6.3.5 t3 once, and one whose /CIDSet identifies CID 1 alone, while
// the program holds 38 glyphs, fails ISO 19005-2, 6.2.11.4.2 t2 once, whether
// one page shows the text, two pages share one content stream, two pages draw
// one stream each or two pages list one stream in direct arrays.
func TestFontSubsetRuleStatedOncePerFont(t *testing.T) {
	t.Parallel()

	layouts := []struct {
		name     string
		contents []string
	}{
		{"one page", []string{"3 0 R"}},
		{"two pages sharing a stream", []string{"3 0 R", "3 0 R"}},
		{"two pages with a stream each", []string{"3 0 R", "4 0 R"}},
		{"two direct arrays", []string{"[3 0 R]", "[3 0 R]"}},
	}
	fonts := []struct {
		name    string
		font    []string
		show    string
		profile models.PDFAProfile
		rule    Rule
	}{
		{"Type1 subset with no CharSet", subsetType1Font(t), "(AB)", models.PDFA1b, Rule_19005_1_6_3_5_t02},
		{"Type0 subset with no CIDSet", subsetType0Font(t, ""), "<00010002>", models.PDFA1b, Rule_19005_1_6_3_5_t03},
		{"Type0 subset with an incomplete CIDSet", subsetType0Font(t, " /CIDSet 9 0 R"), "<00010002>", models.PDFA2b, Rule_19005_2_6_2_11_4_2_t02},
	}
	for _, f := range fonts {
		for _, l := range layouts {
			t.Run(f.name+", "+l.name, func(t *testing.T) {
				t.Parallel()
				doc, _ := objsDocPDF(t, subsetFontObjs(f.font, f.show, l.contents...)...)
				viols, err := ValidatePDFA(context.Background(), doc, f.profile)
				if err != nil {
					t.Fatalf("ValidatePDFA: %v", err)
				}
				if got := countRuleErrs(t, viols, f.rule); got != 1 {
					t.Errorf("%s test %d fired %d times, want 1", f.rule.Clause, f.rule.TestNumber, got)
				}
			})
		}
	}
}

// TestCIDSetRuleStatedOncePerDescendant pins that the /CIDSet rule is stated
// once per descendant CIDFont object, however many Type0 fonts name it; see
// [fontJudgements.subsetCoverage]. Measured with veraPDF 1.30: two Type0
// fonts over one CIDFont object whose /CIDSet leaves out the shown CIDs fail
// ISO 19005-1, 6.3.5 t3 and ISO 19005-2, 6.2.11.4.2 t2 once, and two over a
// CIDFont object each, sharing one descriptor, twice.
func TestCIDSetRuleStatedOncePerDescendant(t *testing.T) {
	t.Parallel()

	// Objects 5 to 9 are [subsetType0Font]; object 10 is a second Type0 font,
	// object 11 a second CIDFont over the same descriptor.
	objs := func(t *testing.T, cidSet, secondDescendant string) []string {
		t.Helper()
		font := subsetType0Font(t, cidSet)
		const text = "BT /F1 12 Tf 10 50 Td <00010002> Tj /F2 12 Tf 10 30 Td <00010002> Tj ET"
		return append([]string{
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [12 0 R] /Count 1 >>",
			exponentStream("", text),
			"null",
		}, append(font,
			"<< /Type /Font /Subtype /Type0 /BaseFont /ABCDEF+Arial /Encoding /Identity-H /DescendantFonts ["+secondDescendant+"] >>",
			"<< /Type /Font /Subtype /CIDFontType2 /BaseFont /ABCDEF+Arial /DW 1000 /CIDToGIDMap /Identity "+
				"/FontDescriptor 7 0 R /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] "+
				"/Resources << /Font << /F1 5 0 R /F2 10 0 R >> >> /Contents 3 0 R >>",
		)...)
	}
	for _, tc := range []struct {
		name       string
		descendant string
		want       int
	}{
		{"one CIDFont object", "6 0 R", 1},
		{"a CIDFont object each", "11 0 R", 2},
	} {
		for _, p := range []struct {
			profile models.PDFAProfile
			cidSet  string
			rule    Rule
		}{
			{models.PDFA1b, "", Rule_19005_1_6_3_5_t03},
			{models.PDFA2b, " /CIDSet 9 0 R", Rule_19005_2_6_2_11_4_2_t02},
		} {
			t.Run(tc.name+" "+string(p.profile), func(t *testing.T) {
				t.Parallel()
				doc, _ := objsDocPDF(t, objs(t, p.cidSet, tc.descendant)...)
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

// TestSharedPageContentSelectsTheFirstPageFonts pins that a content stream
// two pages share selects its fonts through the first page's resources alone;
// see [markJudgedContents]. Each page maps /F1 to its own unembedded font, and
// veraPDF 1.30 fails the embedding rule for the first page's font alone when
// the pages share the stream, and for both when each draws its own.
func TestSharedPageContentSelectsTheFirstPageFonts(t *testing.T) {
	t.Parallel()

	objs := func(first, second string) []string {
		const text = "BT /F1 12 Tf 10 50 Td (A) Tj ET"
		return []string{
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [6 0 R 7 0 R] /Count 2 >>",
			exponentStream("", text),
			"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
			"<< /Type /Font /Subtype /Type1 /BaseFont /Courier >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources << /Font << /F1 4 0 R >> >> /Contents " + first + " >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources << /Font << /F1 5 0 R >> >> /Contents " + second + " >>",
			exponentStream("", text),
		}
	}
	for _, tc := range []struct {
		name string
		want int
		objs []string
	}{
		{"shared stream", 1, objs("3 0 R", "3 0 R")},
		{"a stream each", 2, objs("3 0 R", "8 0 R")},
	} {
		for _, p := range []struct {
			profile models.PDFAProfile
			rule    Rule
		}{
			{models.PDFA1b, Rule_19005_1_6_3_4_t01},
			{models.PDFA2b, Rule_19005_2_6_2_11_4_1_t01},
		} {
			t.Run(tc.name+" "+string(p.profile), func(t *testing.T) {
				t.Parallel()
				doc, _ := objsDocPDF(t, tc.objs...)
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
