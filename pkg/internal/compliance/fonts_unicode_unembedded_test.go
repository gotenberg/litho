package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestUnembeddedSimpleFontUnicodeByName pins that the Unicode rules judge each
// glyph a simple font that embeds no program shows by the name veraPDF gives
// its code: no name fails the presence rule, ISO 19005-2, 6.2.11.7.2 t1, the
// name .notdef has the value U+0000 and fails the value rule, t2, and a name
// with no Unicode value fails t1. The .notdef rule, 6.2.11.8 t1, reads the
// same names but for a symbolic TrueType font, whose encoding it ignores. See
// [unembeddedUnicodeFaults] and [checkUnembeddedNotdef].
//
// Every count was measured with veraPDF 1.30 at PDF/A-2u on these fonts
// showing the eleven codes 0, 1, 0x1F, 0x3C, 0x41, 0x7F, 0x80, 0x81, 0xA1,
// 0xAD and 0xE2 in one string.
func TestUnembeddedSimpleFontUnicodeByName(t *testing.T) {
	t.Parallel()

	const (
		shown  = "BT /F1 12 Tf 10 50 Td <00011f3c417f8081a1ade2> Tj ET"
		widths = "/FirstChar 0 /LastChar 255 /Widths 6 0 R"
		dif    = "<< /Type /Encoding /Differences [65 /.notdef 128 /A /a71 226 /uni0041] >>"
	)
	descriptor := func(flags string) string {
		return "<< /Type /FontDescriptor /FontName /Arial /Flags " + flags +
			" /FontBBox [0 0 1000 1000] /ItalicAngle 0 /Ascent 800 /Descent -200 /CapHeight 700 /StemV 80 >>"
	}
	const toUnicode = "/CIDInit /ProcSet findresource begin 12 dict begin begincmap /CMapName /X def\n" +
		"1 begincodespacerange <00> <FF> endcodespacerange\n" +
		"2 beginbfchar <01> <0041> <41> <0000> endbfchar endcmap CMapName currentdict /CMap defineresource pop end end"

	for _, tc := range []struct {
		name            string
		font            string
		t1, t2, notdef  int
		descriptorFlags string
	}{
		{"Helvetica with no /Encoding", "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>", 0, 7, 7, ""},
		{"Helvetica under /Foo", "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /Foo >>", 0, 7, 7, ""},
		{"Helvetica under /Differences", "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding " + dif + " >>", 1, 5, 5, ""},
		{
			"Helvetica with a /ToUnicode mapping code 1 to A and 0x41 to U+0000",
			"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /ToUnicode 7 0 R >>", 0, 7, 7, "",
		},
		{"Arial Type1 under /StandardEncoding", "<< /Type /Font /Subtype /Type1 /BaseFont /Arial /Encoding /StandardEncoding " + widths + " /FontDescriptor 5 0 R >>", 11, 0, 0, "32"},
		{"Arial Type1 under /MacExpertEncoding", "<< /Type /Font /Subtype /Type1 /BaseFont /Arial /Encoding /MacExpertEncoding " + widths + " /FontDescriptor 5 0 R >>", 0, 8, 8, "32"},
		{"Arial Type1 under /Differences", "<< /Type /Font /Subtype /Type1 /BaseFont /Arial /Encoding " + dif + " " + widths + " /FontDescriptor 5 0 R >>", 8, 1, 1, "32"},
		{"Arial TrueType with no /Encoding", "<< /Type /Font /Subtype /TrueType /BaseFont /Arial " + widths + " /FontDescriptor 5 0 R >>", 11, 0, 1, "32"},
		{"symbolic Arial TrueType under /WinAnsiEncoding", "<< /Type /Font /Subtype /TrueType /BaseFont /Arial /Encoding /WinAnsiEncoding " + widths + " /FontDescriptor 5 0 R >>", 0, 3, 1, "4"},
		{"symbolic Arial TrueType under /Differences", "<< /Type /Font /Subtype /TrueType /BaseFont /Arial /Encoding " + dif + " " + widths + " /FontDescriptor 5 0 R >>", 8, 1, 1, "4"},
		{
			"Symbol under /Differences",
			"<< /Type /Font /Subtype /Type1 /BaseFont /Symbol /Encoding << /Type /Encoding /Differences [65 /a71 /alpha /.notdef] >> >>", 10, 0, 0, "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fd := "null"
			if tc.descriptorFlags != "" {
				fd = descriptor(tc.descriptorFlags)
			}
			ws := "["
			for range 256 {
				ws += " 500"
			}
			objs := drawnImageObjs([]string{reachPage("<< /Font << /F1 4 0 R >> >>", "20 0 R", "")}, map[int]string{
				4: tc.font, 5: fd, 6: ws + " ]", 7: exponentStream("", toUnicode), 20: exponentStream("", shown),
			})
			doc, _ := objsDocPDF(t, objs...)
			viols, err := ValidatePDFA(context.Background(), doc, models.PDFA2u)
			if err != nil {
				t.Fatalf("ValidatePDFA: %v", err)
			}
			for _, r := range []struct {
				rule Rule
				want int
			}{
				{Rule_19005_2_6_2_11_7_2_t01, tc.t1},
				{Rule_19005_2_6_2_11_7_2_t02, tc.t2},
				{Rule_19005_2_6_2_11_8_t01, tc.notdef},
			} {
				if got := countRuleErrs(t, viols, r.rule); got != r.want {
					t.Errorf("%s test %d fired %d times, want %d", r.rule.Clause, r.rule.TestNumber, got, r.want)
				}
			}
		})
	}
}
