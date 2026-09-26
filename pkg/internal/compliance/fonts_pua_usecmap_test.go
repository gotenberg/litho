package compliance

import (
	"strings"
	"testing"
)

// TestPUAActualText_BaseCMap pins that a glyph's Private Use value comes from
// its /ToUnicode CMap together with the CMaps that CMap builds on, merged the
// way [resolveUsedCMaps] merges them for the other /ToUnicode rules.
//
// ISO 19005-2, 6.2.11.7.3 and ISO 14289-2, 8.4.3 ask for an /ActualText
// around a glyph whose Unicode value lies in a Private Use Area, and ISO
// 32000-1, 9.7.5 lets a /ToUnicode CMap take its values from a base: through
// the usecmap operator, whose single-code mappings veraPDF merges, or a
// /UseCMap entry, whose every mapping supplies a code the program leaves out.
// litho read Private Use values from the bytes of the program alone, so it
// missed a value only the base supplies and kept one the base replaces.
//
// Every case was measured with veraPDF 1.30.2 at PDF/A-2a and PDF/UA-2 on the
// PDFs these objects build. Adobe's CNS1 resource maps CID 44DF to U+F538
// through bfchar, and Adobe's Japan1 resource maps CID 3C57 to U+F87A through
// bfchar and CID 3C54 to U+F860 through bfrange, which usecmap does not merge.
// Adobe-Korea1-UCS2 maps code 60 through bfchar, which replaces a value the
// program gives before its usecmap, or anywhere under /UseCMap, and yields to
// one the program gives after its usecmap.
func TestPUAActualText_BaseCMap(t *testing.T) {
	t.Parallel()

	const wide = "<0000> <FFFF>"
	composite := func(t *testing.T, body, dict, show string) []string {
		t.Helper()
		font := subsetType0Font(t, "")
		font[0] = strings.TrimSuffix(font[0], " >>") + " /ToUnicode 21 0 R >>"
		objs := glyphPagesObjs(font, show)
		return append(objs, exponentStream(dict, strings.Replace(toUnicodeProgram(body), "<00> <FF>", wide, 1)))
	}
	simple := func(t *testing.T, body, dict, code string, bases ...string) []string {
		t.Helper()
		return glyphPagesObjs(type3Font(0x41, []string{"zzq", "zzr"}, toUnicodeProgram(body), dict, bases...),
			glyphText("<"+code+"> Tj"))
	}
	for _, tc := range []struct {
		name string
		objs func(t *testing.T) []string
		want int
	}{
		{"a bfchar Private Use value usecmap merges", func(t *testing.T) []string {
			return composite(t, "/Adobe-CNS1-UCS2 usecmap\n", "", glyphText("<355044DF> Tj"))
		}, 1},
		{"the same glyph under /ActualText", func(t *testing.T) []string {
			return composite(t, "/Adobe-CNS1-UCS2 usecmap\n", "",
				"/Span << /ActualText (x) >> BDC "+glyphText("<355044DF> Tj")+" EMC")
		}, 0},
		{"a glyph the same base maps outside the Private Use Area", func(t *testing.T) []string {
			return composite(t, "/Adobe-CNS1-UCS2 usecmap\n", "", glyphText("<3550> Tj"))
		}, 0},
		{"a Japan1 bfchar Private Use value usecmap merges", func(t *testing.T) []string {
			return composite(t, "/Adobe-Japan1-UCS2 usecmap\n", "", glyphText("<3C57> Tj"))
		}, 1},
		{"a Japan1 bfrange Private Use value usecmap leaves out", func(t *testing.T) []string {
			return composite(t, "/Adobe-Japan1-UCS2 usecmap\n", "", glyphText("<3C54> Tj"))
		}, 0},
		{"the same bfrange value under /UseCMap", func(t *testing.T) []string {
			return composite(t, "", "/UseCMap /Adobe-Japan1-UCS2", glyphText("<3C54> Tj"))
		}, 1},
		{"a CNS1 Private Use value under /UseCMap", func(t *testing.T) []string {
			return composite(t, "", "/UseCMap /Adobe-CNS1-UCS2", glyphText("<44DF> Tj"))
		}, 1},
		{"an embedded /UseCMap base", func(t *testing.T) []string {
			return simple(t, "", "/UseCMap 7 0 R", "41", exponentStream("", toUnicodeProgram(bfchars("41", "E000"))))
		}, 1},
		{"an embedded /UseCMap base's code the page does not show", func(t *testing.T) []string {
			return simple(t, bfchars("42", "0042"), "/UseCMap 7 0 R", "42",
				exponentStream("", toUnicodeProgram(bfchars("41", "E000"))))
		}, 0},
		{"a /UseCMap chain two streams deep", func(t *testing.T) []string {
			return simple(t, "", "/UseCMap 7 0 R", "41",
				exponentStream("/UseCMap 8 0 R", toUnicodeProgram(bfchars("42", "0042"))),
				exponentStream("", toUnicodeProgram(bfchars("41", "E000"))))
		}, 1},
		{"a program value a /UseCMap base replaces", func(t *testing.T) []string {
			return glyphPagesObjs(type3Font(0x60, []string{"zzr"}, toUnicodeProgram(bfchars("60", "E000")),
				"/UseCMap /Adobe-Korea1-UCS2"), glyphText("<60> Tj"))
		}, 0},
		{"a program value before usecmap the base replaces", func(t *testing.T) []string {
			return glyphPagesObjs(type3Font(0x60, []string{"zzr"},
				toUnicodeProgram(bfchars("60", "E000")+"/Adobe-Korea1-UCS2 usecmap\n"), ""), glyphText("<60> Tj"))
		}, 0},
		{"a program value after usecmap the base yields to", func(t *testing.T) []string {
			return glyphPagesObjs(type3Font(0x61, []string{"zzs"},
				toUnicodeProgram("/Adobe-Korea1-UCS2 usecmap\n"+bfchars("61", "E001")), ""), glyphText("<61> Tj"))
		}, 1},
	} {
		for _, profile := range []string{"PDF/A-2a", "PDF/A-3a", "PDF/UA-2"} {
			t.Run(tc.name+" at "+profile, func(t *testing.T) {
				t.Parallel()
				rule := puaActualTextRule(profile)
				if got := validateCount(t, tc.objs(t), profile, rule); got != tc.want {
					t.Errorf("%s test %d fired %d times, want %d", rule.Clause, rule.TestNumber, got, tc.want)
				}
			})
		}
	}
}
