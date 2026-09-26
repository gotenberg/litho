package compliance

import (
	"strings"
	"testing"
)

// adobeUCS2Cases are the fixtures of [TestToUnicode_AdobeUCS2Base]: a composite
// font whose /ToUnicode maps no code of its own and builds on an Adobe-*-UCS2
// CMap, through the usecmap operator or a /UseCMap entry, showing CIDs chosen
// where Adobe's current resources and the 2009 PDF.js copies disagree, with
// the failures veraPDF 1.30.2 states at PDF/A-2u.
var adobeUCS2Cases = []struct {
	name, base, dict string
	show             string
	want             int
}{
	// Single mappings the current resource adds (the PDF.js copy maps these
	// CIDs through bfrange, which usecmap does not merge), then CID 0 and a
	// CID neither maps. veraPDF fails the last alone.
	{"GB1 through usecmap", "Adobe-GB1-UCS2", "", "<0254 0255 0258 0259 5610 5617 0000 0100>", 1},
	{"CNS1 through usecmap", "Adobe-CNS1-UCS2", "", "<3550 44DF 44E0 4A90 4A91 4A92 0000 0106>", 1},
	// 00E7 to 15FC gain a single mapping; 018A, 018B and 23BF lose theirs.
	{"Japan1 through usecmap", "Adobe-Japan1-UCS2", "", "<00E7 0143 0144 0F1B 0F1C 15FC 018A 018B 23BF 0000 0100>", 4},
	// PDF.js ships no Adobe-KR-UCS2 at all.
	{"KR through usecmap", "Adobe-KR-UCS2", "", "<0000 0064 0065 006A 006B 0084 0104>", 1},
	// Mappings the current resource adds anywhere, bfrange included.
	{"CNS1 through /UseCMap", "Adobe-CNS1-UCS2", "/UseCMap /Adobe-CNS1-UCS2", "<4A90 4A91 4A92 4A93 4A94 4A95 0000 4AEB>", 1},
	// Adobe's resource maps CIDs 5A0D and 5A0E through the last bfrange entry
	// of a block it counts as 42 entries. veraPDF's bundled copy counts 41, and
	// its count-bound reader drops that entry and states both CIDs as well
	// (3 failures): an artifact of its copy, not mirrored.
	{"Japan1 through /UseCMap", "Adobe-Japan1-UCS2", "/UseCMap /Adobe-Japan1-UCS2", "<5A12 5A13 0000 5A14 5A0D 5A0E>", 1},
}

// TestToUnicode_AdobeUCS2Base pins that a /ToUnicode CMap built on an
// Adobe-*-UCS2 CMap is judged against Adobe's current mapping resources, which
// veraPDF bundles, rather than the 2009 copies PDF.js ships and text
// extraction keeps reading. ISO 32000-1, 9.10.2 names those CMaps as the
// source of a CIDFont's Unicode values; see [resolveUsedCMaps] and
// cmap.PredefinedUnicode. Every count was measured with veraPDF 1.30.2 on
// these PDFs.
func TestToUnicode_AdobeUCS2Base(t *testing.T) {
	t.Parallel()

	for _, tc := range adobeUCS2Cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := ""
			if tc.dict == "" {
				body = "/" + tc.base + " usecmap\n"
			}
			font := subsetType0Font(t, "")
			font[0] = strings.TrimSuffix(font[0], " >>") + " /ToUnicode 21 0 R >>"
			objs := glyphPagesObjs(font, glyphText(tc.show+" Tj"))
			objs = append(objs, exponentStream(tc.dict, strings.Replace(toUnicodeProgram(body),
				"<00> <FF>", "<0000> <FFFF>", 1)))
			if got := validateCount(t, objs, "PDF/A-2u", Rule_19005_2_6_2_11_7_2_t01); got != tc.want {
				t.Errorf("6.2.11.7.2 test 1 fired %d times, want %d", got, tc.want)
			}
		})
	}
}
