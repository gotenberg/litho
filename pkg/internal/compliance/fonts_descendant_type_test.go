package compliance

import (
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestDescendantCIDFontRequiresType pins that a Type0's descendant CIDFont is
// judged for /Type of its own.
//
// ISO 32000-1, 9.7.4, Table 117 requires /Type /Font on a CIDFont dictionary,
// and veraPDF models the descendant as a PDFont, reporting §6.2.11.2 t1 against
// it. litho ran the required-key checks on the Type0 parent and returned before
// reaching the descendant, so a descendant with no /Type went unreported: pdfjs
// issue7544.pdf writes exactly that, confirmed in the file's own bytes.
//
// Only /Type is judged: both CIDFont subtypes are already valid, so t2 cannot
// fire, and t7 reads a descriptor [document.ResolveFontDescriptor] already
// reaches through the descendant, so repeating the whole check would judge one
// descriptor twice.
func TestDescendantCIDFontRequiresType(t *testing.T) {
	t.Parallel()

	fires := func(t *testing.T, descendant string) bool {
		t.Helper()
		content := "BT /F1 12 Tf 10 50 Td <0001> Tj ET"
		data := cyclePDF(
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] "+
				"/Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
			"<< /Type /Font /Subtype /Type0 /BaseFont /Test /Encoding /Identity-H "+
				"/DescendantFonts [6 0 R] >>",
			descendant,
		)
		return firesRuleProfile(t, data, models.PDFA2b, "6.2.11.2", 1)
	}

	t.Run("a descendant with no /Type fires", func(t *testing.T) {
		t.Parallel()
		if !fires(t, "<< /Subtype /CIDFontType2 /BaseFont /Test "+
			"/CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> /DW 1000 >>") {
			t.Error("ISO 32000-1 Table 117 requires /Type /Font on a CIDFont, so a descendant without it must fire")
		}
	})

	t.Run("a descendant carrying /Type does not", func(t *testing.T) {
		t.Parallel()
		if fires(t, "<< /Type /Font /Subtype /CIDFontType2 /BaseFont /Test "+
			"/CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> /DW 1000 >>") {
			t.Error("a conformant descendant must not be reported")
		}
	})

	t.Run("a null descendant is not reported", func(t *testing.T) {
		t.Parallel()
		// pdfjs issue12823.pdf writes /DescendantFonts [null]. Nothing resolves
		// to a dictionary there, so there is no CIDFont to judge and the
		// missing-/Type report would name an object the file does not contain.
		if fires(t, "null") {
			t.Error("a null descendant has no CIDFont dictionary to judge")
		}
	})
}
