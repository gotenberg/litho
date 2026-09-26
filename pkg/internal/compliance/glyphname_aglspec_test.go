package compliance

import (
	"fmt"
	"testing"
)

// TestGlyphNameHasUnicode_ListedNamesOnly pins that a glyph name yields a
// Unicode value only when a published glyph list holds it (ISO 32000-1,
// 9.10.2): the Adobe Glyph List, the Symbol font set or the ZapfDingbats list.
// A spelling the AGL Specification lets a consumer derive (uniXXXX, uXXXX, a
// .suffix, an underscore ligature) and a spelling a producer invents (G4F) are
// in no list.
//
// Every verdict was measured with veraPDF 1.30.2 at PDF/A-1a, 2u, 2a and
// PDF/UA-1 on a one-code fixture through a Type 1, a TrueType and a Type 3
// font; see [glyphNameHasUnicode].
//
// # Corpus evidence
//
//   - govdocs thread_300_300084.pdf: shows codes its embedded Type 1 program
//     names G4F and G56. litho read them as U+004F and U+0056 and excused the
//     font, while veraPDF reports every such glyph.
func TestGlyphNameHasUnicode_ListedNamesOnly(t *testing.T) {
	t.Parallel()

	for name, want := range map[string]bool{
		"A":            true,
		"aacute":       true,
		"ccaron":       true,
		"Acyrillic":    true, // AGL, not AGLFN
		"Aacutesmall":  true, // AGL, not AGLFN
		"afii10017":    true,
		"Kacute":       true,
		"Tcommaaccent": true,
		"nabla":        true,
		"Alpha":        true,
		"alpha":        true, // Symbol
		"a71":          true, // ZapfDingbats
		"uni0041":      false,
		"uni00E9":      false,
		"uni00410042":  false,
		"u0044":        false,
		"u1F600":       false,
		"a.sc":         false,
		"D.001":        false,
		"f_f":          false,
		"A_B":          false,
		"Qcaron":       false,
		"circlecopyrt": false,
		"G4F":          false,
		"g0053":        false,
		"C65":          false,
		"C2c":          false,
		"iSQP0041":     false,
	} {
		if got := glyphNameHasUnicode(name); got != want {
			t.Errorf("glyphNameHasUnicode(%q) = %v, want %v", name, got, want)
		}
	}
}

// oneNameFontObjs returns a one-page PDF showing code 65 through a font of kind
// ("type1", "truetype" or "type3") whose /Differences names that code name: an
// unembedded Helvetica, an unembedded non-symbolic Arial, or a Type 3 font.
func oneNameFontObjs(kind, name string) []string {
	const content = "BT /F1 24 Tf 72 700 Td (A) Tj ET"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
		exponentStream("", content),
	}
	enc := fmt.Sprintf("<< /Type /Encoding /Differences [65 /%s] >>", name)
	switch kind {
	case "type1":
		objs = append(objs, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding "+enc+" >>")
	case "truetype":
		objs = append(objs,
			"<< /Type /Font /Subtype /TrueType /BaseFont /Arial /FirstChar 65 /LastChar 65 /Widths [667] "+
				"/FontDescriptor 6 0 R /Encoding "+enc+" >>",
			"<< /Type /FontDescriptor /FontName /Arial /Flags 32 /FontBBox [-665 -325 2000 1006] /ItalicAngle 0 "+
				"/Ascent 905 /Descent -212 /CapHeight 716 /StemV 80 >>")
	default:
		objs = append(objs,
			fmt.Sprintf("<< /Type /Font /Subtype /Type3 /FontBBox [0 0 1000 1000] /FontMatrix [0.001 0 0 0.001 0 0] "+
				"/CharProcs << /%s 6 0 R >> /Encoding %s /FirstChar 65 /LastChar 65 /Widths [1000] /Resources << >> >>",
				name, enc),
			exponentStream("", "1000 0 0 0 1000 1000 d1 0 0 1000 1000 re f"))
	}
	return objs
}

// TestGlyphNameHasUnicode_ThroughEachFontKind pins the Unicode rule on a
// single code whose /Differences name is or is not in a published glyph list,
// through a Type 1, a TrueType and a Type 3 font at PDF/A-1a, PDF/A-2u,
// PDF/A-2a and PDF/UA-1. Every verdict was measured with veraPDF 1.30.2 on
// these PDFs; see [glyphNameHasUnicode].
//
// Two verdicts are not the name's. PDF/A-1 admits no glyph name for a Type 3
// font (ISO 19005-1, 6.3.8), so the rule fails there whatever the name. And
// veraPDF's PDF/A-1 Type 1 path (PDType1Font.toUnicodePDFA1) consults no
// ZapfDingbats list, so it fails a71 through Helvetica at PDF/A-1a, where
// litho, keeping one set of lists for every part, passes it; that case is left
// out.
func TestGlyphNameHasUnicode_ThroughEachFontKind(t *testing.T) {
	t.Parallel()

	profiles := []struct {
		profile string
		rule    Rule
	}{
		{"PDF/A-1a", Rule_19005_1_6_3_8_t01},
		{"PDF/A-2u", Rule_19005_2_6_2_11_7_2_t01},
		{"PDF/A-2a", Rule_19005_2_6_2_11_7_2_t01},
		{"PDF/UA-1", Rule_14289_1_7_21_7_t01},
	}
	for _, tc := range []struct {
		name   string
		listed bool
	}{
		{"A", true},
		{"Acyrillic", true},
		{"Aacutesmall", true},
		{"afii10017", true},
		{"alpha", true},
		{"a71", true},
		{"uni0044", false},
		{"uni00410042", false},
		{"u0044", false},
		{"u1F600", false},
		{"A.sc", false},
		{"D.001", false},
		{"A_B", false},
		{"Qcaron", false},
	} {
		for _, kind := range []string{"type1", "truetype", "type3"} {
			for _, p := range profiles {
				if tc.name == "a71" && kind == "type1" && p.profile == "PDF/A-1a" {
					continue
				}
				t.Run(fmt.Sprintf("%s %s at %s", kind, tc.name, p.profile), func(t *testing.T) {
					t.Parallel()
					want := !tc.listed || (kind == "type3" && p.profile == "PDF/A-1a")
					got := validateCount(t, oneNameFontObjs(kind, tc.name), p.profile, p.rule) > 0
					if got != want {
						t.Errorf("%s test %d fired: %v, want %v", p.rule.Clause, p.rule.TestNumber, got, want)
					}
				})
			}
		}
	}
}
