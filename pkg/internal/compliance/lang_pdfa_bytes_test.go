package compliance

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestPDFALangTagJudgedOnBytes pins that the PDF/A language-tag tests read the
// value as written, not the display form.
//
// ISO 19005-1, 6.8.4 t1 and ISO 19005-2/3, 6.7.4 t1 admit an empty value or one
// matching the subtag grammar, with nothing trimmed and no byte dropped.
// [document.DecodePdfString] drops an undefined PDFDocEncoding byte by design,
// so a tag written "EN-US" plus a NUL reached the grammar as a legal "EN-US"
// and went unreported.
//
// A BOM-prefixed value keeps its exemption from the byte test, because those
// bytes are UTF-16 code units rather than PDFDocEncoding. veraPDF passes a
// BOM'd "en-GB", and a BOM'd value whose payload really carries U+0000 still
// fails the grammar after decoding.
//
// # Corpus evidence
//
//   - govdocs thread_100_100172, thread_100_100195, thread_200_200961,
//     thread_300_300346: four files whose tag is "EN-US" plus a NUL went
//     unreported, and veraPDF fails each of them.
//   - verapdf 6-8-4-t01-pass-f: veraPDF passes the BOM'd "en-GB" of its own
//     fixture.
//   - govdocs thread_000_000153, thread_200_200295: a BOM'd value whose
//     payload carries U+0000 already fails the grammar after decoding, which
//     is what reports them.
func TestPDFALangTagJudgedOnBytes(t *testing.T) {
	t.Parallel()

	bom := func(s string) []byte {
		out := []byte{0xFE, 0xFF}
		for _, r := range s {
			out = append(out, byte(r>>8), byte(r))
		}
		return out
	}

	for _, tc := range []struct {
		name         string
		value        []byte
		part1Invalid bool
		part2Invalid bool
	}{
		{"a plain tag", []byte("EN-US"), false, false},
		{"a trailing NUL the display form hides", []byte("EN-US\x00"), true, true},
		{"a BOM tag stays valid", bom("en-GB"), false, false},
		{"a BOM tag whose payload carries U+0000", append(bom("EN-US"), 0x00, 0x00), true, true},
		{"no tag at all", []byte(""), false, false},
		{"a digit subtag, which only part 1 forbids", []byte("en-12"), true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := ast.PdfString(tc.value)
			if got := pdfaLangTagInvalid(s, 1); got != tc.part1Invalid {
				t.Errorf("part 1: invalid = %v, want %v (bytes %q)", got, tc.part1Invalid, tc.value)
			}
			if got := pdfaLangTagInvalid(s, 2); got != tc.part2Invalid {
				t.Errorf("part 2: invalid = %v, want %v (bytes %q)", got, tc.part2Invalid, tc.value)
			}
		})
	}
}
