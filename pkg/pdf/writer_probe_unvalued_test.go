package pdf

import (
	"bytes"
	"context"
	"fmt"
	"testing"
)

// unvaluedCodePDF is a page that shows "AB" in Helvetica, then code 5A20 in
// a Type 0 font over the Adobe-Japan1 collection whose embedded encoding CMap
// is mappings. With mappings "/Adobe-Japan1-99 usecmap", a base no reader
// holds, the base might map the code, so default extraction reads its own
// value, U+5A20. With "1 begincidchar <0001> 1 endcidchar" litho holds every
// mapping and none covers the code, which selects CID 0, .notdef, and reads
// as nothing (ISO 32000-1, 9.7.6.3). Either way 9.10.2 gives it no Unicode
// value. withLatin false leaves the Helvetica show out.
func unvaluedCodePDF(mappings string, withLatin bool) []byte {
	cmap := "/CIDInit /ProcSet findresource begin\n12 dict begin\nbegincmap\n" +
		"/CIDSystemInfo << /Registry (Adobe) /Ordering (Japan1) /Supplement 7 >> def\n" +
		"/CMapName /LithoProbeUnvalued def\n/CMapType 1 def\n" + mappings + "\n" +
		"1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n" +
		"endcmap\nCMapName currentdict /CMap defineresource pop\nend\nend"
	content := "BT /F2 12 Tf 72 680 Td <5A20> Tj ET\n"
	if withLatin {
		content = "BT /F1 12 Tf 72 700 Td (AB) Tj ET\n" + content
	}
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R " +
			"/Resources << /Font << /F1 5 0 R /F2 6 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		"<< /Type /Font /Subtype /Type0 /BaseFont /LithoProbe /Encoding 7 0 R /DescendantFonts [8 0 R] >>",
		fmt.Sprintf("<< /Type /CMap /CMapName /LithoProbeUnvalued "+
			"/CIDSystemInfo << /Registry (Adobe) /Ordering (Japan1) /Supplement 7 >> /Length %d >>\n"+
			"stream\n%s\nendstream", len(cmap), cmap),
		"<< /Type /Font /Subtype /CIDFontType0 /BaseFont /LithoProbe " +
			"/CIDSystemInfo << /Registry (Adobe) /Ordering (Japan1) /Supplement 7 >> /FontDescriptor 9 0 R /DW 1000 >>",
		"<< /Type /FontDescriptor /FontName /LithoProbe /Flags 4 /FontBBox [0 0 1000 1000] " +
			"/ItalicAngle 0 /Ascent 880 /Descent -120 /CapHeight 700 /StemV 80 >>",
	})
}

// probeSnap opens data and samples it as the remediation probe does.
func probeSnap(t *testing.T, data []byte) integritySnap {
	t.Helper()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	snap, err := probeIntegrity(ctx, r)
	if err != nil {
		t.Fatalf("probeIntegrity: %v", err)
	}
	return snap
}

// TestProbeIntegrityIgnoresUnvaluedReading pins that the remediation probe
// weighs a code ISO 32000-1, 9.10.2 gives no Unicode value the same whatever
// litho reads for it. A repair that sends such a code to .notdef shows the
// same glyph and loses no character a reader is given, yet default
// extraction reads the code's own value before and nothing after, so the
// probe once counted the change as lost text and rolled the repair back.
func TestProbeIntegrityIgnoresUnvaluedReading(t *testing.T) {
	t.Parallel()

	baseline := probeSnap(t, unvaluedCodePDF("/Adobe-Japan1-99 usecmap", true))
	post := probeSnap(t, unvaluedCodePDF("1 begincidchar\n<0001> 1\nendcidchar", true))
	if baseline.textLen == 0 {
		t.Fatal("probe counted no text on a page showing AB")
	}
	if reason := evaluateRegression(baseline, post, DefaultRegressionTolerance); reason != "" {
		t.Errorf("probe flagged a code with no Unicode value read as .notdef: %s", reason)
	}
}

// TestProbeIntegrityCatchesLostText pins that the probe still catches text a
// repair removes, beside a code with no Unicode value: the planted post drops
// the "AB" show.
func TestProbeIntegrityCatchesLostText(t *testing.T) {
	t.Parallel()

	baseline := probeSnap(t, unvaluedCodePDF("/Adobe-Japan1-99 usecmap", true))
	for name, post := range map[string][]byte{
		"own value kept": unvaluedCodePDF("/Adobe-Japan1-99 usecmap", false),
		"read as notdef": unvaluedCodePDF("1 begincidchar\n<0001> 1\nendcidchar", false),
	} {
		if reason := evaluateRegression(baseline, probeSnap(t, post), DefaultRegressionTolerance); reason == "" {
			t.Errorf("%s: probe accepted a post that lost the AB show", name)
		}
	}
}
