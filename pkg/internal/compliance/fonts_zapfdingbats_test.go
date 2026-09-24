package compliance

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// TestToUnicode_ZapfDingbatsNeedsToUnicode pins that ZapfDingbats does not get
// the standard-14 exemption from the per-glyph Unicode requirement.
//
// ISO 32000-1, 9.10.2 derives Unicode without a /ToUnicode CMap only for glyph
// names taken from "the Adobe standard Latin character set or the set of named
// characters in the Symbol font". ZapfDingbats is neither: its built-in encoding
// names its glyphs a1 to a191, which are in no AGL litho consults. litho
// exempted every standard-14 font alike, so a ZapfDingbats glyph with no
// /ToUnicode had no Unicode value and nothing said so. veraPDF reports it under
// ISO 14289-1 §7.21.7 t1.
//
// # Corpus evidence
//
//   - pdfjs: veraPDF reports it on 27 files.
//   - pdfjs js-buttons.pdf: one of them, showing code 0x34 through an
//     unembedded ZapfDingbats inside a widget appearance.
func TestToUnicode_ZapfDingbatsNeedsToUnicode(t *testing.T) {
	t.Parallel()

	// fires reports whether the per-glyph Unicode rule fails for a simple font
	// named baseFont, carrying toUnicode (empty for none), showing one code.
	fires := func(t *testing.T, baseFont, extra string, extraObjs ...string) bool {
		t.Helper()
		objs := []string{
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] " +
				"/Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
			"<< /Length 34 >>\nstream\nBT /F1 12 Tf 10 50 Td (4) Tj ET\nendstream",
			"<< /Type /Font /Subtype /Type1 /BaseFont /" + baseFont + " " + extra + ">>",
		}
		objs = append(objs, extraObjs...)
		data := cyclePDF(objs...)
		doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
		if err != nil {
			t.Fatalf("NewDocument: %v", err)
		}
		viols, err := ValidatePDFUA(context.Background(), doc, models.PDFUA1)
		if err != nil {
			t.Fatalf("ValidatePDFUA: %v", err)
		}
		for _, e := range viols {
			if v, ok := e.(Violation); ok && v.Rule.Clause == "7.21.7" && v.Rule.TestNumber == 1 {
				return true
			}
		}
		return false
	}

	// A minimal ToUnicode CMap mapping code 0x34 to U+2714 (a tick).
	const cmap = "/CIDInit /ProcSet findresource begin 12 dict begin begincmap\n" +
		"1 begincodespacerange <00> <ff> endcodespacerange\n" +
		"1 beginbfchar <34> <2714> endbfchar\n" +
		"endcmap CMapName currentdict /CMap defineresource pop end end"

	t.Run("ZapfDingbats with no ToUnicode fires", func(t *testing.T) {
		t.Parallel()
		if !fires(t, "ZapfDingbats", "") {
			t.Error("a ZapfDingbats glyph has no Unicode value without /ToUnicode")
		}
	})

	t.Run("a subset-prefixed ZapfDingbats fires too", func(t *testing.T) {
		t.Parallel()
		if !fires(t, "ABCDEF+ZapfDingbats", "") {
			t.Error("the subset prefix of ISO 32000-1 9.6.4 must not hide the base font")
		}
	})

	t.Run("ZapfDingbats with a ToUnicode does not", func(t *testing.T) {
		t.Parallel()
		if fires(t, "ZapfDingbats", "/ToUnicode 6 0 R ",
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(cmap), cmap)) {
			t.Error("a /ToUnicode CMap supplies the Unicode value")
		}
	})

	t.Run("a standard Latin font still does not", func(t *testing.T) {
		t.Parallel()
		if fires(t, "Helvetica", "") {
			t.Error("Helvetica's built-in encoding is Adobe standard Latin and stays exempt")
		}
	})

	// ISO 32000-1 9.10.2 admits the Symbol font's named characters only through
	// a /Differences array, so a Symbol with no /Encoding names nothing its
	// glyphs could draw Unicode from; veraPDF reports it on a one-font fixture.
	t.Run("Symbol with no Encoding fires", func(t *testing.T) {
		t.Parallel()
		if !fires(t, "Symbol", "") {
			t.Error("an unembedded Symbol with no /Encoding gives its glyphs no Unicode value")
		}
	})

	t.Run("Symbol naming its glyphs through Differences does not", func(t *testing.T) {
		t.Parallel()
		if fires(t, "Symbol", "/Encoding << /Differences [52 /four] >> ") {
			t.Error("ISO 32000-1 9.10.2 admits Symbol names in a /Differences array")
		}
	})
}
