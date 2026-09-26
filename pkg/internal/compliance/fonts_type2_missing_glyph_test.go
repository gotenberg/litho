package compliance

import (
	"context"
	"encoding/binary"
	"os"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// zeroNotdefAdvance returns a copy of the TrueType program ttf whose glyph 0
// advances 0.
func zeroNotdefAdvance(t *testing.T, ttf []byte) []byte {
	t.Helper()
	out := append([]byte(nil), ttf...)
	numTables := int(binary.BigEndian.Uint16(out[4:6]))
	for i := range numTables {
		rec := out[12+16*i:]
		if string(rec[:4]) == "hmtx" {
			off := binary.BigEndian.Uint32(rec[8:12])
			out[off], out[off+1] = 0, 0
			return out
		}
	}
	t.Fatal("TTF fixture has no hmtx table")
	return nil
}

// TestType2MissingGlyphStatesEveryRule pins what a CID a TrueType CIDFont's
// program holds no glyph for answers to: the glyph-presence rule, the .notdef
// rule, and the width rule, weighed at the advance of glyph 0; see
// [missingType2CIDs] and [checkType2MissingGlyphWidths].
//
// Every count was measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b on a
// 38-glyph program whose glyph 0 advances 908 of 2048 units, 443.36.
func TestType2MissingGlyphStatesEveryRule(t *testing.T) {
	t.Parallel()

	ttf, err := os.ReadFile("testdata/cidfont_notdef_min.ttf")
	if err != nil {
		t.Fatalf("read TTF fixture: %v", err)
	}
	zeroed := zeroNotdefAdvance(t, ttf)
	// gidMap is a /CIDToGIDMap stream of two entries: CID 0 to GID 0 and CID 1
	// to gid.
	gidMap := func(gid uint16) string {
		return exponentStream("", string([]byte{0, 0, byte(gid >> 8), byte(gid)}))
	}
	type want struct{ presence, notdef, width int }
	cases := []struct {
		name    string
		cidFont string
		program []byte
		gidMap  string
		shows   string
		pdfa1b  want // PDF/A-1 states no .notdef rule
		pdfa2b  want
	}{
		{"past the program, /DW 1000", "/DW 1000 /CIDToGIDMap /Identity", ttf, "", "<0100> Tj", want{1, 0, 1}, want{1, 1, 1}},
		{"past the program, /DW matching glyph 0", "/DW 443 /CIDToGIDMap /Identity", ttf, "", "<0100> Tj", want{1, 0, 0}, want{1, 1, 0}},
		{"past the program, /W matching glyph 0", "/DW 1000 /W [256 [443]] /CIDToGIDMap /Identity", ttf, "", "<0100> Tj", want{1, 0, 0}, want{1, 1, 0}},
		{"past the program, /W not matching", "/DW 443 /W [256 [500]] /CIDToGIDMap /Identity", ttf, "", "<0100> Tj", want{1, 0, 1}, want{1, 1, 1}},
		{"past the program, no /DW", "/CIDToGIDMap /Identity", ttf, "", "<0100> Tj", want{1, 0, 1}, want{1, 1, 1}},
		{"past the program, no /CIDToGIDMap", "/DW 1000", ttf, "", "<0100> Tj", want{1, 0, 1}, want{1, 1, 1}},
		{"first CID past the program", "/DW 1000 /CIDToGIDMap /Identity", ttf, "", "<0026> Tj", want{1, 0, 1}, want{1, 1, 1}},
		{"only in rendering mode 3", "/DW 1000 /CIDToGIDMap /Identity", ttf, "", "3 Tr <0100> Tj", want{0, 0, 0}, want{0, 1, 0}},
		{"glyph 0 advancing 0", "/DW 1000 /CIDToGIDMap /Identity", zeroed, "", "<0100> Tj", want{1, 0, 1}, want{1, 1, 1}},
		{"glyph 0 advancing 0, /DW 0", "/DW 0 /CIDToGIDMap /Identity", zeroed, "", "<0100> Tj", want{1, 0, 0}, want{1, 1, 0}},
		{"a map stream sending the CID past the program", "/DW 1000 /CIDToGIDMap 9 0 R", ttf, gidMap(100), "<0001> Tj", want{1, 0, 1}, want{1, 1, 1}},
		{"a map stream ending before the CID", "/DW 1000 /CIDToGIDMap 9 0 R", ttf, gidMap(37), "<0005> Tj", want{1, 0, 1}, want{1, 1, 1}},
		{"the last glyph of the program", "/DW 1000 /CIDToGIDMap /Identity", ttf, "", "<0025> Tj", want{0, 0, 1}, want{0, 0, 1}},
	}
	for _, p := range []struct {
		profile                 models.PDFAProfile
		presence, notdef, width Rule
	}{
		{models.PDFA1b, Rule_19005_1_6_3_5_t01, Rule{}, Rule_19005_1_6_3_6_t01},
		{models.PDFA2b, Rule_19005_2_6_2_11_4_1_t02, Rule_19005_2_6_2_11_8_t01, Rule_19005_2_6_2_11_5_t01},
	} {
		for _, tc := range cases {
			t.Run(string(p.profile)+" "+tc.name, func(t *testing.T) {
				t.Parallel()
				font := []string{
					"<< /Type /Font /Subtype /Type0 /BaseFont /ABCDEF+Arial /Encoding /Identity-H /DescendantFonts [6 0 R] >>",
					"<< /Type /Font /Subtype /CIDFontType2 /BaseFont /ABCDEF+Arial " + tc.cidFont +
						" /FontDescriptor 7 0 R /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> >>",
					"<< /Type /FontDescriptor /FontName /ABCDEF+Arial /Flags 32 /Ascent 700 /Descent -200 /CapHeight 700 " +
						"/StemV 80 /FontBBox [0 0 500 700] /ItalicAngle 0 /FontFile2 8 0 R >>",
					exponentStream("", string(tc.program)),
				}
				if tc.gidMap != "" {
					font = append(font, tc.gidMap)
				}
				doc, _ := objsDocPDF(t, glyphPagesObjs(font, glyphText(tc.shows))...)
				viols, err := ValidatePDFA(context.Background(), doc, p.profile)
				if err != nil {
					t.Fatalf("ValidatePDFA: %v", err)
				}
				w := tc.pdfa1b
				if p.profile == models.PDFA2b {
					w = tc.pdfa2b
				}
				for _, c := range []struct {
					what string
					rule Rule
					want int
				}{{"glyph presence", p.presence, w.presence}, {".notdef", p.notdef, w.notdef}, {"width", p.width, w.width}} {
					if c.rule.Specification == "" {
						continue
					}
					if got := countRuleErrs(t, viols, c.rule); got != c.want {
						t.Errorf("%s (%s test %d) fired %d times, want %d", c.what, c.rule.Clause, c.rule.TestNumber, got, c.want)
					}
				}
			})
		}
	}
}
