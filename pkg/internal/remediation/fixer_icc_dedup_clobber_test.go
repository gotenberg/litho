package remediation

import (
	"compress/zlib"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// indirectCS0PDF stores the page's /CS0 colour space as an INDIRECT array
// (object 8), which dedupPDF does not: it writes /CS0 inline as
// [/ICCBased 6 0 R]. That difference decides everything here. With an inline
// array the `ref, isRef := entry.(ast.PdfIndirectRef)` test in
// dedupICCInResources is false, the AllocAt at fixer_icc_dedup.go:267 never
// fires, and the code falls through to rewriting the /ColorSpace dict, the
// /Resources dict or the page instead. Both existing suites assert
// alloc.Pending()[3], the PAGE, which is exactly that fallback.
//
// Object 5 is the OutputIntent profile, 6 the matching page profile, 8 the
// indirect colour-space array, 9 a DIFFERENT profile used by the peer's edit.
func indirectCS0PDF(t *testing.T) []byte {
	t.Helper()

	const content = "q /CS0 cs 0 0 0 1 sc 10 10 100 100 re f Q\n"
	cmyk := deflateAt(t, cmykICCProfile, zlib.BestCompression)
	srgb := deflateAt(t, sRGBICCProfile, zlib.BestCompression)

	return assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /OutputIntents [4 0 R] >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 7 0 R" +
			" /Resources << /ColorSpace << /CS0 8 0 R >> >> >>",
		"<< /Type /OutputIntent /S /GTS_PDFA1 /OutputConditionIdentifier (Coated FOGRA39)" +
			" /DestOutputProfile 5 0 R >>",
		iccObj(cmyk, 4),
		iccObj(cmyk, 4),
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"[/ICCBased 6 0 R]",
		iccObj(srgb, 3),
	})
}

// [dedupICCInResources] reads the colour-space array with document.ResolveArray
// (fixer_icc_dedup.go:247) and writes the substitute back at that array's own
// object number (:267). It consults no alloc-aware helper, so an earlier
// fixer's edit to the same colour-space object is invisible to it.
//
// Peer and ordering are measured: colour-space objects are written earlier by
// output-intent (fixer.go:68) and output-intent-cleanup (:70), and by graphics
// (:106), transparency (:107) and group-cs (:108). icc-dedup runs at :117,
// after all of them.
//
// THIS DEFECT SHOWS UP AS AN UNWANTED SUBSTITUTION, NOT A LOST EDIT, which is
// why the test is shaped differently from the other clobber tests. Reading the
// document copy, the fixer still sees the CMYK profile that duplicates the
// OutputIntent and collapses /CS0 to /DeviceCMYK. Reading the allocator, it
// would see the peer's sRGB profile, find no duplicate, and correctly leave the
// colour space alone. So the failure replaces a colour space the document no
// longer uses.
//
// The CONTROL runs the same fixture unseeded to prove the substitution path
// actually reaches object 8; without it, "nothing was written" could mean the
// fixture never fired rather than that the repair declined.
func TestDedupICCInResources_KeepsAnEarlierFixersPendingColourSpaceWrite(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	const csObj = 8

	// CONTROL: unseeded, the fixer must substitute at object 8 itself.
	doc, alloc := newTestDoc(t, indirectCS0PDF(t))
	if _, err := fixICCDedup(ctx, alloc, doc, Options{}, nil, ""); err != nil {
		t.Fatalf("control fixICCDedup: %v", err)
	}
	control, present := alloc.Pending()[csObj]
	if !present {
		t.Fatalf("control: object %d was never written, so the fixture does not reach "+
			"the substitution and this test proves nothing; pending = %v",
			csObj, alloc.Pending())
	}
	if name, _ := control.(ast.PdfName); name != "DeviceCMYK" {
		t.Fatalf("control: object %d became %v, want /DeviceCMYK; the fixture does not "+
			"exercise the path under test", csObj, control)
	}

	// SEEDED: an earlier colour-space fixer repointed /CS0 at a DIFFERENT
	// profile. That profile does not duplicate the OutputIntent, so a
	// correctly alloc-aware read leaves the colour space untouched.
	doc2, alloc2 := newTestDoc(t, indirectCS0PDF(t))
	peer := ast.PdfArray{ast.PdfName("ICCBased"), ast.PdfIndirectRef{ObjectNum: 9}}
	if !alloc2.AllocAt(csObj, peer) {
		t.Fatalf("AllocAt(%d) refused the earlier fixer's pending write", csObj)
	}

	if _, err := fixICCDedup(ctx, alloc2, doc2, Options{}, nil, ""); err != nil {
		t.Fatalf("fixICCDedup: %v", err)
	}

	got := alloc2.Pending()[csObj]
	if name, isName := got.(ast.PdfName); isName && name == "DeviceCMYK" {
		t.Errorf("object %d was collapsed to /DeviceCMYK: dedupICCInResources read the "+
			"on-disk array, matched the OutputIntent profile the document no longer "+
			"uses, and discarded the earlier fixer's write; pending = %v", csObj, got)
	}
}
