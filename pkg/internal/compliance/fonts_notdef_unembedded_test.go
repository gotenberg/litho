package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestUnembeddedNotdefNames pins how the .notdef rule names the glyph a code
// selects through a simple font that embeds no program: through the tables
// veraPDF holds, MacRomanEncoding, MacExpertEncoding and WinAnsiEncoding, and
// through StandardEncoding for a Latin standard 14 font read from its metrics,
// a code a table leaves undefined referencing .notdef; see
// [checkUnembeddedNotdef].
//
// Every count was measured with veraPDF 1.30 at PDF/A-2b, PDF/A-4 and
// PDF/UA-1.
func TestUnembeddedNotdefNames(t *testing.T) {
	t.Parallel()

	const (
		helvetica = "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica"
		times     = "<< /Type /Font /Subtype /Type1 /BaseFont /Times-Roman"
		arial     = "<< /Type /Font /Subtype /Type1 /BaseFont /Arial"
		arialTT   = "<< /Type /Font /Subtype /TrueType /BaseFont /Arial"
		differs   = " /Encoding << /Differences [65 /A] >>"
		withFD    = " /FirstChar 0 /LastChar 1 /Widths [500 500] /FontDescriptor 6 0 R"
	)
	descriptor := func(flags string) string {
		return "<< /Type /FontDescriptor /FontName /Arial /Flags " + flags + " /Ascent 700 /Descent -200 " +
			"/CapHeight 700 /StemV 80 /FontBBox [0 0 500 700] /ItalicAngle 0 >>"
	}
	nonsymbolic, symbolic := descriptor("32"), descriptor("4")

	for _, tc := range []struct {
		name  string
		font  string
		fd    string
		shows string
		want  int
	}{
		{"Helvetica, code 1", helvetica + " >>", nonsymbolic, `(\001) Tj`, 1},
		{"Helvetica, code 0", helvetica + " >>", nonsymbolic, `(\000) Tj`, 1},
		{"Helvetica, DEL", helvetica + " >>", nonsymbolic, `(\177) Tj`, 1},
		{"Helvetica, code 0x80", helvetica + " >>", nonsymbolic, `(\200) Tj`, 1},
		{"Helvetica, exclamdown", helvetica + " >>", nonsymbolic, `(\241) Tj`, 0},
		{"Helvetica, a control code between letters", helvetica + " >>", nonsymbolic, `(A\001B) Tj`, 1},
		{"Helvetica, in rendering mode 3", helvetica + " >>", nonsymbolic, `3 Tr (\001) Tj`, 1},
		{"Helvetica-Bold, code 0x1F", "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold >>", nonsymbolic, `(\037) Tj`, 1},
		{"Courier, code 1", "<< /Type /Font /Subtype /Type1 /BaseFont /Courier >>", nonsymbolic, `(\001) Tj`, 1},
		{"Times-Roman with an empty font descriptor", times + " /FontDescriptor << >> >>", nonsymbolic, `(\001) Tj`, 1},
		{"Helvetica under /StandardEncoding", helvetica + " /Encoding /StandardEncoding >>", nonsymbolic, `(\001) Tj`, 1},
		{"Helvetica under an unknown encoding name", helvetica + " /Encoding /Foo >>", nonsymbolic, `(\001) Tj`, 1},
		{"Times-Roman under /Differences alone, 0xE2", times + differs + " >>", nonsymbolic, `(\342) Tj`, 1},
		{"Times-Roman under /Differences alone, 0x80", times + differs + " >>", nonsymbolic, `(\200) Tj`, 1},
		{"Times-Roman over /StandardEncoding", times + " /Encoding << /BaseEncoding /StandardEncoding /Differences [65 /A] >> >>", nonsymbolic, `(\200) Tj`, 1},
		{"Helvetica under /WinAnsiEncoding, code 1", helvetica + " /Encoding /WinAnsiEncoding >>", nonsymbolic, `(\001) Tj`, 1},
		{"Helvetica under /WinAnsiEncoding, 0x81", helvetica + " /Encoding /WinAnsiEncoding >>", nonsymbolic, `(\201) Tj`, 0},
		{"Helvetica under /MacRomanEncoding, 0x80", helvetica + " /Encoding /MacRomanEncoding >>", nonsymbolic, `(\200) Tj`, 0},
		{"Symbol, code 1", "<< /Type /Font /Subtype /Type1 /BaseFont /Symbol >>", nonsymbolic, `(\001) Tj`, 0},
		{"Symbol under /Differences alone", "<< /Type /Font /Subtype /Type1 /BaseFont /Symbol /Encoding << /Differences [65 /alpha] >> >>", nonsymbolic, `(\001) Tj`, 0},
		{"ZapfDingbats, code 1", "<< /Type /Font /Subtype /Type1 /BaseFont /ZapfDingbats >>", nonsymbolic, `(\001) Tj`, 0},
		{"Arial, code 1", arial + " >>", nonsymbolic, `(\001) Tj`, 0},
		{"Arial under /StandardEncoding", arial + " /Encoding /StandardEncoding >>", nonsymbolic, `(\001) Tj`, 0},
		{"Arial under /Differences alone, code 1", arial + differs + " >>", nonsymbolic, `(\001) Tj`, 0},
		{"Arial under /Differences alone, 0x80", arial + differs + " >>", nonsymbolic, `(\200) Tj`, 0},
		{"a symbolic Arial under /WinAnsiEncoding", arial + " /Encoding /WinAnsiEncoding" + withFD + " >>", symbolic, `(\001) Tj`, 1},
		{"Helvetica with a font descriptor", helvetica + withFD + " >>", nonsymbolic, `(\001) Tj`, 0},
		{"Helvetica with a symbolic font descriptor", helvetica + withFD + " >>", symbolic, `(\001) Tj`, 0},
		{"Helvetica with a font descriptor under /StandardEncoding", helvetica + " /Encoding /StandardEncoding" + withFD + " >>", nonsymbolic, `(\001) Tj`, 0},
		{"Helvetica with a font descriptor under /Differences alone", helvetica + differs + withFD + " >>", nonsymbolic, `(\001) Tj`, 0},
		{"a TrueType Arial, code 0", arialTT + withFD + " >>", nonsymbolic, `(\000) Tj`, 1},
		{"a TrueType Arial, code 1", arialTT + withFD + " >>", nonsymbolic, `(\001) Tj`, 0},
		{"a TrueType Arial under /WinAnsiEncoding", arialTT + " /Encoding /WinAnsiEncoding" + withFD + " >>", nonsymbolic, `(\001) Tj`, 1},
		{"a TrueType Arial under /Differences alone", arialTT + differs + withFD + " >>", nonsymbolic, `(\001) Tj`, 0},
		{"a symbolic TrueType Arial under /WinAnsiEncoding, code 1", arialTT + " /Encoding /WinAnsiEncoding" + withFD + " >>", symbolic, `(\001) Tj`, 0},
		{"a symbolic TrueType Arial under /WinAnsiEncoding, code 0", arialTT + " /Encoding /WinAnsiEncoding" + withFD + " >>", symbolic, `(\000) Tj`, 1},
	} {
		for _, p := range []struct {
			profile string
			rule    Rule
		}{
			{string(models.PDFA2b), Rule_19005_2_6_2_11_8_t01},
			{string(models.PDFA4), Rule_19005_4_6_2_10_9_t01},
			{string(models.PDFUA1), Rule_14289_1_7_21_8_t01},
		} {
			t.Run(p.profile+" "+tc.name, func(t *testing.T) {
				t.Parallel()
				doc, _ := objsDocPDF(t, glyphPagesObjs([]string{tc.font, tc.fd}, glyphText(tc.shows))...)
				var viols []error
				var err error
				if IsPDFUA(p.profile) {
					viols, err = ValidatePDFUA(context.Background(), doc, models.PDFUAProfile(p.profile))
				} else {
					viols, err = ValidatePDFA(context.Background(), doc, models.PDFAProfile(p.profile))
				}
				if err != nil {
					t.Fatalf("validate: %v", err)
				}
				if got := countRuleErrs(t, viols, p.rule); got != tc.want {
					t.Errorf("%s test %d fired %d times, want %d", p.rule.Clause, p.rule.TestNumber, got, tc.want)
				}
			})
		}
	}
}
