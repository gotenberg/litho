package mutations

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

func TestScanOutputForTokens_DropsTheContentStreamTheRewriteOrphans(t *testing.T) {
	// The load-bearing case, and the whole reason the scan resolves through the
	// allocator. An excision leaves the page's original content stream in the
	// document referenced by nothing, so the parsed document still holds
	// "SECRET" at object 6 while the file a full rewrite writes does not.
	//
	// Asserting the contrast rather than just the result is what makes this
	// meaningful: the same document scanned without the allocator still
	// reports object 6, so a bare "no hit on 6" cannot pass by the scan never
	// reaching the page.
	ctx := context.Background()
	doc, page := redactScanFixture(t)
	alloc := writer.NewObjAllocator(20)

	report, err := RedactPageRegions(ctx, alloc, doc, page, [][4]float64{{90, 690, 300, 730}})
	if err != nil {
		t.Fatalf("RedactPageRegions: %v", err)
	}
	if len(report.RemovedText) == 0 {
		t.Fatalf("nothing was removed, so there is nothing to scan for")
	}

	// The redaction queued its edits and left doc as it was, so a nil
	// allocator scans the document before them.
	before, err := ScanOutputForTokens(ctx, nil, doc, []string{"SECRET"})
	if err != nil {
		t.Fatalf("ScanOutputForTokens without the allocator: %v", err)
	}
	if !hasHit(before, 6, "stream") {
		t.Fatalf("the unedited document did not report object 6, so the premise of this test is gone: %+v", before.Hits)
	}

	out, err := ScanOutputForTokens(ctx, alloc, doc, []string{"SECRET"})
	if err != nil {
		t.Fatalf("ScanOutputForTokens: %v", err)
	}
	if hasHit(out, 6, "stream") {
		t.Errorf("object 6 was reported, but a full rewrite never copies it: %+v", out.Hits)
	}
	// /Info is a root of its own: nothing in the catalog graph reaches it, and
	// the rewrite copies it, so its copy of the word is a real leak.
	if !hasHit(out, 7, "string") {
		t.Errorf("the /Info copy was not reported: %+v", out.Hits)
	}
	if len(out.Unreadable) != 0 {
		t.Errorf("Unreadable = %v, want none: a skipped object makes empty hits meaningless", out.Unreadable)
	}
}

func TestScanOutputForTokens_ReadsAnObjectThatExistsOnlyInTheAllocator(t *testing.T) {
	// The other half of the defect. The rewritten content stream is a brand-new
	// object number that appears in no xref entry, so a scan driven by the
	// document's object table cannot reach it however it resolves references.
	// "KEEP" survives the redaction and is drawn by that new stream.
	ctx := context.Background()
	doc, page := redactScanFixture(t)
	alloc := writer.NewObjAllocator(20)

	if _, err := RedactPageRegions(ctx, alloc, doc, page, [][4]float64{{90, 690, 300, 730}}); err != nil {
		t.Fatalf("RedactPageRegions: %v", err)
	}

	out, err := ScanOutputForTokens(ctx, alloc, doc, []string{"KEEP"})
	if err != nil {
		t.Fatalf("ScanOutputForTokens: %v", err)
	}
	if len(out.Hits) == 0 {
		t.Fatalf("the surviving text was not found at all, so no pending object was read: %+v", out)
	}
	if hasHit(out, 6, "stream") {
		t.Errorf("object 6 was reported: the answer came from the original stream, not the replacement")
	}
	// The fixture's trailer says /Size 8, so anything past it was allocated by
	// the redaction and lives only in the allocator.
	found := false
	for _, hit := range out.Hits {
		if hit.ObjectNum > 8 && hit.Where == "stream" {
			found = true
		}
	}
	if !found {
		t.Errorf("no hit landed on a newly allocated object: %+v", out.Hits)
	}
}

func TestScanOutputForTokens_NilAllocatorReadsTheDocumentAsItStands(t *testing.T) {
	// With no edits queued, the output is the document itself, so the live
	// content stream is reachable and its copy counts.
	ctx := context.Background()
	doc, _ := redactScanFixture(t)

	out, err := ScanOutputForTokens(ctx, nil, doc, []string{"SECRET"})
	if err != nil {
		t.Fatalf("ScanOutputForTokens: %v", err)
	}
	if !hasHit(out, 6, "stream") {
		t.Errorf("the page's own content stream was not reported: %+v", out.Hits)
	}
	if !hasHit(out, 7, "string") {
		t.Errorf("the /Info copy was not reported: %+v", out.Hits)
	}
}

func TestScanOutputForTokens_TerminatesOnAReferenceCycle(t *testing.T) {
	// Following references is what this scan does differently, so a file whose
	// objects point at each other has to end the walk rather than recurse until
	// the stack goes. The token is in the cycle, so reaching it also proves the
	// walk went round once rather than stopping at the first reference.
	ctx := context.Background()
	body := "BT /F1 12 Tf 100 700 Td (KEEP) Tj ET"
	data := []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R /Loop 9 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n" +
		"5 0 obj\n<< /Font << /F1 4 0 R >> >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 5 0 R /Contents 6 0 R >>\nendobj\n" +
		"6 0 obj\n<< /Length " + strconv.Itoa(len(body)) + " >>\nstream\n" + body + "\nendstream\nendobj\n" +
		"9 0 obj\n<< /Back 1 0 R /Note (CYCLESECRET) >>\nendobj\n" +
		"trailer\n<< /Size 10 /Root 1 0 R >>\n%%EOF\n")

	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}

	out, err := ScanOutputForTokens(ctx, nil, doc, []string{"CYCLESECRET"})
	if err != nil {
		t.Fatalf("ScanOutputForTokens: %v", err)
	}
	if !hasHit(out, 9, "string") {
		t.Errorf("the token inside the cycle was not reported: %+v", out.Hits)
	}
}

func TestScanOutputForTokens_RejectsAnEmptyToken(t *testing.T) {
	doc, _ := redactScanFixture(t)

	_, err := ScanOutputForTokens(context.Background(), nil, doc, []string{"SECRET", ""})
	if !errors.Is(err, ErrScanTokenEmpty) {
		t.Errorf("err = %v, want ErrScanTokenEmpty", err)
	}
}

// hasHit reports whether the result names this object and location.
func hasHit(res ScanResult, objNum int, where string) bool {
	for _, hit := range res.Hits {
		if hit.ObjectNum == objNum && hit.Where == where {
			return true
		}
	}
	return false
}
