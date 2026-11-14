package compliance

import (
	"fmt"
	"os"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestNotdef_SymbolicDifferencesLeaveOtherCodesToTheProgram pins that a
// symbolic TrueType whose encoding is a /Differences-only dictionary is not
// condemned for a code the differences leave alone.
//
// Such a dictionary names no base table, so it names only the codes it lists,
// and ISO 32000-1, 9.6.6.4 draws the others through the program's cmap. A base
// table that leaves the shown code undefined still fires, symbolic or not.
//
// # Corpus evidence
//
//   - litho symbolenc_2b.pdf: font S2 shows code 0x41 under a /Differences
//     naming 0x42 only once remediation removes its /BaseEncoding, and veraPDF
//     1.30.3 reports no .notdef at PDF/A-2b.
func TestNotdef_SymbolicDifferencesLeaveOtherCodesToTheProgram(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("testdata/cidfont_notdef_min.ttf")
	if err != nil {
		t.Fatalf("read TTF fixture: %v", err)
	}
	program := withPostNames(t, raw)

	fires := func(t *testing.T, encoding string) bool {
		t.Helper()
		data := cyclePDF(
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] "+
				"/Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
			"<< /Length 40 >>\nstream\nBT /F1 12 Tf 10 50 Td (A) Tj ET\nendstream",
			"<< /Type /Font /Subtype /TrueType /BaseFont /Test /FirstChar 65 /LastChar 65"+
				" /Widths [722] "+encoding+"/FontDescriptor 6 0 R >>",
			"<< /Type /FontDescriptor /FontName /Test /Flags 4 /Ascent 700 /Descent -200"+
				" /CapHeight 700 /StemV 80 /FontBBox [0 0 500 700] /ItalicAngle 0 /FontFile2 7 0 R >>",
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(program), string(program)),
		)
		return firesRuleProfile(t, data, models.PDFA2b, "6.2.11.8", 1)
	}

	t.Run("differences only", func(t *testing.T) {
		t.Parallel()
		if fires(t, "/Encoding << /Type /Encoding /Differences [66 /B] >> ") {
			t.Error("a code a /Differences-only dictionary leaves alone was reported as .notdef")
		}
	})

	t.Run("a base table leaving the code undefined", func(t *testing.T) {
		t.Parallel()
		if !fires(t, "/Encoding << /Type /Encoding /BaseEncoding /MacExpertEncoding /Differences [66 /B] >> ") {
			t.Error("MacExpertEncoding defines no glyph at 'A', so the code maps to .notdef and must fire")
		}
	})
}
