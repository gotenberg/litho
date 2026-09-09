package compliance

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// type1DiffPDF builds a one-page PDF showing a single character code through a
// non-symbolic Type 1 font whose /Encoding /Differences binds code 65 to
// usedName and code 66, which nothing shows, to unusedName. /CharSet lists both.
//
// /Flags 32 (non-symbolic) keeps the font clear of the symbolic-font exemption
// so the Type 1 glyph-name path is the one under test.
func type1DiffPDF(usedName, unusedName string) []byte {
	const stream = "BT /F1 12 Tf 10 50 Td (A) Tj ET"
	return cyclePDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] "+
			"/Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
		"<< /Type /Font /Subtype /Type1 /BaseFont /ABCDEF+CMSY10 "+
			"/FirstChar 65 /LastChar 66 /Widths [500 500] /FontDescriptor 6 0 R "+
			fmt.Sprintf("/Encoding << /Differences [65 /%s 66 /%s] >> >>", usedName, unusedName),
		"<< /Type /FontDescriptor /FontName /ABCDEF+CMSY10 /Flags 32 "+
			"/ItalicAngle 0 /Ascent 700 /Descent -200 /CapHeight 700 /StemV 80 "+
			fmt.Sprintf("/FontBBox [0 0 1000 1000] /CharSet (/%s/%s) >>", usedName, unusedName),
	)
}

// firesUnicodeRule reports whether data fails the per-glyph Unicode rule,
// ISO 14289-1 §7.21.7 t1.
func firesUnicodeRule(t *testing.T, data []byte) bool {
	t.Helper()
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

// TestType1Unicode_SymbolGlyphNamesResolve pins that a Symbol-font glyph name
// carries a Unicode value in the Type 1 exemption.
//
// ISO 32000-1, 9.10.2 derives Unicode from glyph names taken from "the Adobe
// standard Latin character set or the set of named characters in the Symbol
// font", and litho holds the second set in encodings.SymbolGlyphsUnicode. The
// Type 1 exemption consulted encodings.GlyphToRuneFull alone, which covers only
// the first: its Greek and math entries were deliberately dropped from the
// extended AGL. Every Symbol name was therefore read as having no Unicode value
// at all. pdfjs bug1260585.pdf embeds CMMI12 whose /CharSet names alpha and pi,
// and veraPDF, whose ua1 run on it reports six other rules, reports no 7.21.7.
func TestType1Unicode_SymbolGlyphNamesResolve(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"alpha", "pi", "nabla"} {
		t.Run(name+" carries a Unicode value", func(t *testing.T) {
			t.Parallel()
			if firesUnicodeRule(t, type1DiffPDF(name, name)) {
				t.Errorf("%s is a named character in the Symbol font", name)
			}
		})
	}

	t.Run("a name in neither set still fires", func(t *testing.T) {
		t.Parallel()
		if !firesUnicodeRule(t, type1DiffPDF("negationslash", "negationslash")) {
			t.Error("negationslash is in neither the standard Latin nor the Symbol set")
		}
	})
}

// TestType1Unicode_ScopedToShownCodes pins that the Type 1 Unicode exemption
// judges the codes a page shows, not every glyph an embedded subset carries.
//
// ISO 14289-1, 7.21.7 requires a Unicode value for each character code "used",
// which is also veraPDF's model: the rule is raised per Glyph object and a
// Glyph exists only for a rendered code. litho walked the whole /Differences
// array and the whole /CharSet, so one unreachable glyph condemned the font.
// pdfjs bug1260585.pdf embeds CMSY10 listing negationslash in /CharSet while
// the page shows only codes 48 and 92, prime and intersection.
func TestType1Unicode_ScopedToShownCodes(t *testing.T) {
	t.Parallel()

	t.Run("an unshown unresolvable glyph does not fire", func(t *testing.T) {
		t.Parallel()
		if firesUnicodeRule(t, type1DiffPDF("alpha", "negationslash")) {
			t.Error("nothing shows code 66, so its glyph name is out of scope")
		}
	})

	t.Run("a shown unresolvable glyph still fires", func(t *testing.T) {
		t.Parallel()
		if !firesUnicodeRule(t, type1DiffPDF("negationslash", "alpha")) {
			t.Error("code 65 is shown and its glyph name has no Unicode value")
		}
	})
}

// TestType1Unicode_TeXNamesAreNotAGL pins that a TeX glyph name does not earn
// the Type 1 exemption.
//
// litho's typography tables serve text extraction and reach past the Adobe
// Glyph List into the TeX math repertoire, so encodings.GlyphToRuneFull maps
// integraldisplay to U+222B. ISO 32000-1, 9.10.2 admits only the Adobe standard
// Latin set and the Symbol font, and the AGL itself carries no such name, so a
// consumer holding the AGL cannot recover the character. veraPDF reports both
// 6-3-8-t01-fail-a.pdf and 6-2-11-7-2-t01-fail-a.pdf, whose CMEX10 subset holds
// integraldisplay alone, as "The glyph can not be mapped to Unicode".
func TestType1Unicode_TeXNamesAreNotAGL(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"integraldisplay", "parenleftBigg", "summationtext", "radicalbt"} {
		t.Run(name+" is not an AGL name", func(t *testing.T) {
			t.Parallel()
			if !firesUnicodeRule(t, type1DiffPDF(name, name)) {
				t.Errorf("%s is a TeX glyph name the Adobe Glyph List does not define", name)
			}
		})
	}

	t.Run("the AGL spelling still resolves", func(t *testing.T) {
		t.Parallel()
		if firesUnicodeRule(t, type1DiffPDF("integral", "integral")) {
			t.Error("integral is in the Adobe Glyph List")
		}
	})

	// Adobe Technical Note #5946 resolves an underscore-joined name by mapping
	// each component, but that algorithm governs OpenType glyph naming rather
	// than the glyph lists 9.10.2 names: glyphlist.txt carries ff and ffi and no
	// f_f or f_f_i. veraPDF reads it the same way and reports pdfjs
	// issue11016_reduced.pdf, whose Calibre-Medium subset binds code 31 to f_f.
	for _, name := range []string{"f_f", "f_f_i"} {
		t.Run(name+" is not a glyph list spelling", func(t *testing.T) {
			t.Parallel()
			if !firesUnicodeRule(t, type1DiffPDF(name, name)) {
				t.Errorf("no published glyph list carries %s", name)
			}
		})
	}

	t.Run("the glyph list spelling resolves", func(t *testing.T) {
		t.Parallel()
		if firesUnicodeRule(t, type1DiffPDF("ffi", "ffi")) {
			t.Error("ffi is in the Adobe Glyph List")
		}
	})

	// The ZapfDingbats glyph list (zapfdingbats.txt) is published beside the
	// AGL and names the a1 to a191 series, so a /Differences entry binding a
	// code to one of them does yield a Unicode value. govdocs
	// thread_000_000859.pdf binds code 1 to a71 on an unembedded ZapfDingbats
	// and veraPDF reports nothing. This is the companion to the rule that a
	// ZapfDingbats code left to the built-in encoding, which names nothing litho
	// can read, still fires.
	t.Run("a ZapfDingbats name resolves", func(t *testing.T) {
		t.Parallel()
		if firesUnicodeRule(t, type1DiffPDF("a71", "a71")) {
			t.Error("a71 is in the Adobe ZapfDingbats glyph list")
		}
	})
}
