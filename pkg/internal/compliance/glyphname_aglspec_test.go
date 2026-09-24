package compliance

import "testing"

// TestGlyphNameHasUnicode_AGLSpecOnly pins that a glyph name yields a Unicode
// value only under the Adobe Glyph List Specification (ISO 32000-1, 9.10.2):
// a list name, a uniXXXX or uXXXX spelling, a list name with a suffix, or a
// Symbol-set name.
//
// litho borrowed the text-extraction resolver, which also decodes spellings
// producers invent, while veraPDF reports every such glyph under ISO 14289-2,
// 8.4.5.8.
//
// # Corpus evidence
//
//   - govdocs thread_300_300084.pdf: shows codes its embedded Type 1 program
//     names G4F and G56. litho read them as U+004F and U+0056 and excused the
//     font, while veraPDF reports every such glyph.
func TestGlyphNameHasUnicode_AGLSpecOnly(t *testing.T) {
	t.Parallel()

	for name, want := range map[string]bool{
		"A":        true,
		"aacute":   true,
		"ccaron":   true,
		"uni0041":  true,
		"u1F600":   true,
		"a.sc":     true,
		"Alpha":    true,
		"G4F":      false,
		"g0053":    false,
		"C65":      false,
		"C2c":      false,
		"iSQP0041": false,
	} {
		if got := glyphNameHasUnicode(name); got != want {
			t.Errorf("glyphNameHasUnicode(%q) = %v, want %v", name, got, want)
		}
	}
}
