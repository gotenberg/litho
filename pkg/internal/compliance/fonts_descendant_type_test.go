package compliance

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestDescendantCIDFontRequiresType pins that a Type0's descendant CIDFont is
// judged for /Type of its own.
//
// ISO 32000-1, 9.7.4, Table 117 requires /Type /Font on a CIDFont dictionary,
// and veraPDF models the descendant as a PDFont, reporting §6.2.11.2 t1 against
// it. litho ran the required-key checks on the Type0 parent and returned before
// reaching the descendant, so a descendant with no /Type went unreported.
//
// Only /Type is judged: both CIDFont subtypes are already valid, so t2 cannot
// fire, and t7 reads a descriptor [document.ResolveFontDescriptor] already
// reaches through the descendant, so repeating the whole check would judge one
// descriptor twice.
//
// # Corpus evidence
//
//   - pdfjs issue7544.pdf: writes a descendant with no /Type, confirmed in the
//     file's own bytes.
//   - pdfjs issue12823.pdf: writes /DescendantFonts [null], the shape the null
//     descendant case covers.
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
		// /DescendantFonts [null] resolves to no dictionary, so there is no
		// CIDFont to judge and the missing-/Type report would name an object
		// the file does not contain.
		if fires(t, "null") {
			t.Error("a null descendant has no CIDFont dictionary to judge")
		}
	})
}

// TestDescendantTypeCounts pins how often ISO 19005-1, 6.3.2 t1 and ISO
// 19005-2, 6.2.11.2 t1 fire over a descendant CIDFont with no /Type: once for
// every Type0 font over it, which veraPDF reads /Type from it for, and once
// for the descendant. A Type0 dictionary with no /Type of its own fires once
// for it, as ISO 32000-1, 9.7.5, Table 121 requires the entry there, although
// veraPDF, reading the descendant's, reports nothing.
//
// Every count but the last was measured with veraPDF 1.30 at PDF/A-1b and
// PDF/A-2b.
func TestDescendantTypeCounts(t *testing.T) {
	t.Parallel()

	noType := func(objs []string) []string {
		objs[5] = strings.Replace(objs[5], "/Type /Font ", "", 1)
		return objs
	}
	twoType0 := func(objs []string) []string {
		objs = withObject(objs, 9, "<< /Type /Font /Subtype /Type0 /BaseFont /ABCDEF+Arial /Encoding /Identity-H /DescendantFonts [6 0 R] >>")
		objs[10] = strings.Replace(objs[10], "/F1 5 0 R", "/F1 9 0 R", 1)
		return objs
	}
	for _, tc := range []struct {
		name string
		objs []string
		want int
	}{
		{"one Type0 font", noType(glyphPagesObjs(programScopeType0Font(t), glyphText("<0025> Tj"))), 2},
		{"two Type0 fonts", twoType0(noType(glyphPagesObjs(programScopeType0Font(t), glyphText("<0025> Tj"), glyphText("<0025> Tj")))), 3},
		{"a descendant naming another type", func() []string {
			objs := glyphPagesObjs(programScopeType0Font(t), glyphText("<0025> Tj"))
			objs[5] = strings.Replace(objs[5], "/Type /Font ", "/Type /FontX ", 1)
			return objs
		}(), 2},
		{"both dictionaries", func() []string {
			objs := noType(glyphPagesObjs(programScopeType0Font(t), glyphText("<0025> Tj")))
			objs[4] = strings.Replace(objs[4], "/Type /Font ", "", 1)
			return objs
		}(), 2},
		{"the Type0 dictionary alone", func() []string {
			objs := glyphPagesObjs(programScopeType0Font(t), glyphText("<0025> Tj"))
			objs[4] = strings.Replace(objs[4], "/Type /Font ", "", 1)
			return objs
		}(), 1},
	} {
		for _, p := range []struct {
			profile models.PDFAProfile
			rule    Rule
		}{
			{models.PDFA1b, Rule_19005_1_6_3_2_t01},
			{models.PDFA2b, Rule_19005_2_6_2_11_2_t01},
		} {
			t.Run(string(p.profile)+" "+tc.name, func(t *testing.T) {
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
