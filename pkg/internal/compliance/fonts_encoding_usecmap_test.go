package compliance

import (
	"fmt"
	"os"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// encodingCMapProgram is an embedded encoding CMap program over the two-byte
// code space, carrying body after it.
func encodingCMapProgram(name, body string) string {
	return "/CIDInit /ProcSet findresource begin\n12 dict begin\nbegincmap\n" +
		"/CIDSystemInfo 3 dict dup begin /Registry (Adobe) def /Ordering (Identity) def " +
		"/Supplement 0 def end def\n/CMapName /" + name + " def\n/CMapType 1 def\n" +
		"1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n" + body +
		"endcmap\nCMapName currentdict /CMap defineresource pop\nend\nend\n"
}

// useCMapEntryPDF builds a one-page PDF whose Type 0 font, over the embedded
// CIDFontType2 program of testdata/cidfont_notdef_min.ttf, shows content
// through the embedded encoding CMap object 8. useCMap is that stream's
// /UseCMap value, and bases are further CMap streams, objects 10 onwards, each
// given as its /UseCMap entry and its program body.
func useCMapEntryPDF(t *testing.T, content, body, useCMap string, bases ...[2]string) []byte {
	t.Helper()
	ttf, err := os.ReadFile("testdata/cidfont_notdef_min.ttf")
	if err != nil {
		t.Fatalf("read TTF fixture: %v", err)
	}
	stream := func(name, entry, body string) string {
		program := encodingCMapProgram(name, body)
		if entry != "" {
			entry = "/UseCMap " + entry + " "
		}
		return fmt.Sprintf("<< /Type /CMap /CMapName /%s %s/Length %d >>\nstream\n%s\nendstream", name, entry, len(program), program)
	}
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type0 /BaseFont /Arial /Encoding 8 0 R /DescendantFonts [6 0 R] >>",
		"<< /Type /Font /Subtype /CIDFontType2 /BaseFont /Arial /DW 1000 /CIDToGIDMap /Identity /FontDescriptor 7 0 R " +
			"/CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> >>",
		"<< /Type /FontDescriptor /FontName /Arial /Flags 32 /Ascent 700 /Descent -200 /CapHeight 700 /StemV 80 " +
			"/FontBBox [0 0 500 700] /ItalicAngle 0 /FontFile2 9 0 R >>",
		stream("Custom", useCMap, body),
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(ttf), ttf),
	}
	for i, b := range bases {
		objs = append(objs, stream(fmt.Sprintf("Base%d", i), b[0], b[1]))
	}
	return cyclePDF(objs...)
}

// TestEncodingCMapUseCMapEntry pins the base an embedded encoding CMap's
// /UseCMap entry names (ISO 32000-1, 9.7.5.3, Table 120), which veraPDF's
// PDCMap.getCMapFile adds once the program has run. A base that maps the shown
// code decides its CID; a chain that loops, on which veraPDF raises "Loop
// inside CMap" and abandons the run, or that runs past maxUseCMapChainDepth,
// leaves the code unknown rather than unmapped. The cases with at most one
// base were measured against veraPDF 1.30.2; the depth cap is litho's own
// anti-DoS guard, which veraPDF does not have.
func TestEncodingCMapUseCMapEntry(t *testing.T) {
	t.Parallel()

	const bfchar = "1 beginbfchar\n<0043> <0043>\nendbfchar\n"
	const showC = "BT /F1 12 Tf 10 50 Td <0043> Tj ET"
	// chain is n empty bases, objects 10 onwards, each naming the next.
	chain := func(n int) [][2]string {
		out := make([][2]string, n)
		for i := range out {
			out[i] = [2]string{fmt.Sprintf("%d 0 R", 11+i), ""}
		}
		out[n-1][0] = ""
		return out
	}

	tests := []struct {
		name       string
		useCMap    string
		bases      [][2]string
		wantGlyph  bool
		wantNotdef bool
	}{
		{"a predefined base maps the code past the program", "/Identity-H", nil, true, true},
		{"an embedded base maps the code to a glyph", "10 0 R", [][2]string{{"", "1 begincidchar\n<0043> 5\nendcidchar\n"}}, false, false},
		{"an embedded base's notdef range maps the code to CID 0", "10 0 R",
			[][2]string{{"", "1 beginnotdefrange\n<0000> <00FF> 0\nendnotdefrange\n"}}, true, true},
		{"a base no reader bundles names no base", "/No-Such-H", nil, true, true},
		{"a chain that loops leaves the code unknown", "8 0 R", nil, false, false},
		{"a chain at the depth cap is read whole", "10 0 R", chain(maxUseCMapChainDepth), true, true},
		{"a chain past the depth cap leaves the code unknown", "10 0 R", chain(maxUseCMapChainDepth + 1), false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data := useCMapEntryPDF(t, showC, bfchar, tc.useCMap, tc.bases...)
			if got := firesRuleProfile(t, data, models.PDFA2b, "6.2.11.4.1", 2); got != tc.wantGlyph {
				t.Errorf("6.2.11.4.1 t2 fired=%v, want %v", got, tc.wantGlyph)
			}
			if got := firesRuleProfile(t, data, models.PDFA2b, "6.2.11.8", 1); got != tc.wantNotdef {
				t.Errorf("6.2.11.8 t1 fired=%v, want %v", got, tc.wantNotdef)
			}
		})
	}
}
