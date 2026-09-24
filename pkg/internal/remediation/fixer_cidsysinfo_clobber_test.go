package remediation

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestFixCIDSystemInfo_KeepsTheNameFixersPendingFontWrite pins that the
// /Identity relabelling rebuilds the Type 0 font from the dictionary name-utf8
// queued, not from the document.
//
// fixCIDSystemInfo walks every object in the file and writes a relabelled Type
// 0 font back at its own object number. It read each one with doc.Resolve.
// name-utf8 runs earlier, in the resource tier, and rewrites every object that
// carries a name ISO 19005-2, 6.1.8 rejects, font dictionaries included: a
// /BaseFont that is not valid UTF-8 is exactly such a name. The relabelled font
// went out with the invalid /BaseFont restored.
//
// Object 4 has no /Encoding at all, which the fixer relabels /Identity-H.
func TestFixCIDSystemInfo_KeepsTheNameFixersPendingFontWrite(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F0 4 0 R >> >> >>",
		"<< /Type /Font /Subtype /Type0 /BaseFont /Bad#80Name /DescendantFonts [5 0 R] >>",
		"<< /Type /Font /Subtype /CIDFontType0 /BaseFont /Probe "+
			"/CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> >>",
	))

	const fontObj = 4

	if _, err := fixNameUTF8(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixNameUTF8: %v", err)
	}
	if got := pendingDict(t, alloc, fontObj)["BaseFont"]; got != ast.PdfName("Bad_Name") {
		t.Fatalf("control: name-utf8 queued /BaseFont %v at object %d, want /Bad_Name, so there "+
			"is no earlier edit to lose", got, fontObj)
	}

	if _, err := fixCIDSystemInfo(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixCIDSystemInfo: %v", err)
	}

	got := pendingDict(t, alloc, fontObj)
	if enc := got["Encoding"]; enc != ast.PdfName("Identity-H") {
		t.Fatalf("/Encoding is %v, want /Identity-H: the relabelling did not run, so the check "+
			"below proves nothing", enc)
	}
	if bf := got["BaseFont"]; bf != ast.PdfName("Bad_Name") {
		t.Errorf("/BaseFont is %q, want /Bad_Name: fixCIDSystemInfo rebuilt the font from the "+
			"document and discarded name-utf8's write", bf)
	}
}

// TestAlignCMapWMode_KeepsTheNameFixersPendingCMapWrite pins that the /WMode
// alignment rewrites the embedded CMap stream name-utf8 queued, not the
// document's copy.
//
// alignCMapWMode writes the CMap stream back at its own object number with the
// program's /WMode. It read the stream with document.ResolveStream, so an
// earlier rewrite of that stream (here name-utf8 repairing a /CMapName that is
// not valid UTF-8) went back out as the on-disk copy.
func TestAlignCMapWMode_KeepsTheNameFixersPendingCMapWrite(t *testing.T) {
	t.Parallel()

	const program = "%!PS-Adobe-3.0 Resource-CMap\n/WMode 0 def\n"
	ctx := context.Background()
	doc, alloc := newTestDoc(t, buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F0 4 0 R >> >> >>",
		"<< /Type /Font /Subtype /Type0 /BaseFont /Probe /Encoding 5 0 R /DescendantFonts [6 0 R] >>",
		fmt.Sprintf("<< /Type /CMap /CMapName /Bad#80-H /WMode 1 "+
			"/CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> /Length %d >>\n"+
			"stream\n%s\nendstream", len(program), program),
		"<< /Type /Font /Subtype /CIDFontType0 /BaseFont /Probe "+
			"/CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> >>",
	))

	const cmapObj = 5

	if _, err := fixNameUTF8(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixNameUTF8: %v", err)
	}
	seeded, ok := pendingStream(t, alloc, cmapObj)
	if !ok || seeded.Dict["CMapName"] != ast.PdfName("Bad_-H") {
		t.Fatalf("control: name-utf8 did not queue /CMapName /Bad_-H at object %d, so there is "+
			"no earlier edit to lose; pending = %v", cmapObj, alloc.Pending()[cmapObj])
	}

	if _, err := fixCIDSystemInfo(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixCIDSystemInfo: %v", err)
	}

	got, ok := pendingStream(t, alloc, cmapObj)
	if !ok {
		t.Fatalf("object %d is no longer pending", cmapObj)
	}
	if w := got.Dict["WMode"]; w != ast.PdfInt(0) {
		t.Fatalf("/WMode is %v, want 0 from the program: the alignment did not run, so the "+
			"check below proves nothing", w)
	}
	if name := got.Dict["CMapName"]; name != ast.PdfName("Bad_-H") {
		t.Errorf("/CMapName is %q, want /Bad_-H: alignCMapWMode rewrote the document's CMap "+
			"stream and discarded name-utf8's write", name)
	}
	body, err := got.Bytes(ctx)
	if err != nil {
		t.Fatalf("reading the rewritten CMap body: %v", err)
	}
	if string(body) != program {
		t.Errorf("the CMap body is %q, want %q", body, program)
	}
}
