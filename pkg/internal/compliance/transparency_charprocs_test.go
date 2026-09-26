package compliance

import (
	"fmt"
	"testing"
)

// charProcTransparencyPDF builds a one-page document whose only transparency is
// declared on a Type 3 glyph procedure's OWN /Resources.
//
// onFont controls where the offending /ExtGState lives:
//
//	true  - on the font dictionary's /Resources, which a glyph with no
//	        resources of its own reads (see [drawnStreamWalk])
//	false - on the CharProc stream's own /Resources, and NOWHERE else
//
// The false case is the one under test, and the font dictionary deliberately
// declares no /Resources at all. If it did, the existing recursion would find
// the state there and the test would pass without ever reaching a glyph
// procedure, which is the dead-fixture failure this suite has hit before.
//
// The glyph is actually painted: the page content selects /F1 and shows code
// 97, which /Encoding maps to /square. A state no content selects is reported
// nowhere (see [drawnResources]), so an unpainted glyph would prove nothing.
func charProcTransparencyPDF(onFont bool) []byte {
	const glyph = "1000 0 d0\n/GS0 gs\n0 0 100 100 re f"
	const page = "BT /F1 12 Tf 0 0 Td (a) Tj ET"

	extGState := "<< /ExtGState << /GS0 << /Type /ExtGState /ca 0.5 >> >> >>"

	fontRes, procRes := "", ""
	if onFont {
		fontRes = " /Resources " + extGState
	} else {
		procRes = " /Resources " + extGState
	}

	return cyclePDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R"+
			" /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(page), page),
		"<< /Type /Font /Subtype /Type3 /FontBBox [0 0 100 100]"+
			" /FontMatrix [0.001 0 0 0.001 0 0]"+
			" /CharProcs << /square 6 0 R >>"+
			" /Encoding << /Differences [97 /square] >>"+
			" /FirstChar 97 /LastChar 97 /Widths [1000]"+fontRes+" >>",
		fmt.Sprintf("<<%s /Length %d >>\nstream\n%s\nendstream", procRes, len(glyph), glyph),
	)
}

// TestCharProcResources_TransparencyDetected pins that ISO 19005-1 §6.4 reaches
// an /ExtGState a Type 3 GLYPH PROCEDURE declares in its own /Resources.
//
// [drawnStreamWalk] reaches every glyph description of a Type 3 font text is
// shown with, and under a profile built on ISO 32000-1 a glyph carrying its own
// /Resources reads them in its font's stead. Per ISO 32000-1 §9.6.5 a glyph
// procedure is a content stream that paints through the resources in scope for
// it, so a transparency group or constant alpha reached only that way is still
// rendered content and §6.4 forbids it.
//
// The repair side already covers this shape: fixType3CharProcTransparency
// resolves fontDict["CharProcs"] and normalises each glyph stream, and
// matchTransparency claims every ISO_19005_1 clause 6.4 violation. So litho can
// already FIX what it cannot yet SEE.
//
// The control is the point of the test's structure. "Did not fire" is
// indistinguishable from "the fixture never parsed", so the same document with
// the state moved onto the font's /Resources must fire first; only then does
// silence on the CharProc variant mean the walk missed it.
func TestCharProcResources_TransparencyDetected(t *testing.T) {
	t.Parallel()

	// CONTROL: the shape litho already walks. If this does not fire, the
	// fixture is broken and the assertion below proves nothing.
	if !firesRule(t, charProcTransparencyPDF(true), "6.4", 6) {
		t.Fatal("control: an /ExtGState on a Type 3 font's own /Resources must fire " +
			"§6.4 t6; the fixture does not reach the transparency walk at all")
	}

	// THE GAP: identical document, the state moved to the glyph procedure's
	// own /Resources and declared nowhere else.
	if !firesRule(t, charProcTransparencyPDF(false), "6.4", 6) {
		t.Error("an /ExtGState declared on a Type 3 CharProc's own /Resources must fire " +
			"§6.4 t6: a glyph procedure's resources are invisible to the transparency survey")
	}
}
