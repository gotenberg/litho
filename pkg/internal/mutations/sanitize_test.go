package mutations

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strconv"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// sanitizeFixture builds a one-page document whose page draws "SECRET" and
// whose catalog carries the given extra entries and objects, so a test can put
// a surviving copy wherever it needs one.
func sanitizeFixture(t *testing.T, catalogExtra, extraObjects, trailerExtra string) *document.Document {
	t.Helper()
	ctx := context.Background()

	body := "BT /F1 12 Tf 100 700 Td (SECRET) Tj ET"
	raw := "%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R " + catalogExtra + " >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n" +
		"5 0 obj\n<< /Font << /F1 4 0 R >> >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 5 0 R" +
		" /Contents 6 0 R /Annots [8 0 R] >>\nendobj\n" +
		"6 0 obj\n<< /Length " + strconv.Itoa(len(body)) + " >>\nstream\n" + body + "\nendstream\nendobj\n" +
		"8 0 obj\n<< /Type /Annot /Subtype /Link /Rect [10 10 40 40]" +
		" /Contents (SECRET) >>\nendobj\n" +
		extraObjects +
		"trailer\n<< /Size 30 /Root 1 0 R " + trailerExtra + " >>\n%%EOF\n"

	doc, err := document.NewDocument(ctx, bytes.NewReader([]byte(raw)), int64(len(raw)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	return doc
}

func TestSanitizeDocument_ClearsTheCopiesAndComesBackClean(t *testing.T) {
	// The whole workflow, because the gate verifies the WHOLE document. Writing
	// this without the redaction first is what proved the gate honest: the page
	// still spelled the word in its content stream and sanitization refused,
	// exactly as it should have.
	//
	// The annotation sits at [10 10 40 40], clear of the region, so the
	// geometric pass leaves it and the token-driven one has to catch it. That is
	// the composition being demonstrated.
	ctx := context.Background()
	doc := sanitizeFixture(t, "", "", "")
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil || len(pages) == 0 {
		t.Fatalf("ResolvePages: %v (%d pages)", err, len(pages))
	}
	alloc := writer.NewObjAllocator(30)

	redacted, err := RedactPageRegions(ctx, alloc, doc, pages[0], [][4]float64{{90, 690, 300, 730}})
	if err != nil {
		t.Fatalf("RedactPageRegions: %v", err)
	}
	if len(redacted.RemovedText) == 0 {
		t.Fatalf("nothing was removed, so there is nothing to sanitize for")
	}

	report, err := SanitizeDocument(ctx, alloc, doc, redacted.RemovedText)
	if err != nil {
		t.Fatalf("SanitizeDocument: %v", err)
	}
	if !slices.Contains(report.Annotations, 8) {
		t.Errorf("Annotations = %v, want object 8", report.Annotations)
	}
	// Clean also means the ORIGINAL content stream, left unreferenced by the
	// excision, was not counted: the verification reaches out from the roots a
	// full rewrite copies rather than sweeping the object table.
	if !report.Scan.Clean() {
		t.Errorf("scan = %+v, want clean", report.Scan)
	}
}

func TestSanitizeDocument_FailsClosedOnACopyNoPassReaches(t *testing.T) {
	// /Info is not a home any token-driven pass touches, so the word survives
	// there. Reporting success would be the exact failure this gate exists to
	// prevent: a caller writing a document it was told was sanitized.
	doc := sanitizeFixture(t, "", "7 0 obj\n<< /Title (SECRET) >>\nendobj\n", "/Info 7 0 R")
	alloc := writer.NewObjAllocator(30)

	report, err := SanitizeDocument(context.Background(), alloc, doc, []string{"SECRET"})
	if !errors.Is(err, ErrSanitizationIncomplete) {
		t.Fatalf("err = %v, want ErrSanitizationIncomplete", err)
	}

	// The report still has to name where it survived, or the caller cannot act.
	found := false
	for _, hit := range report.Scan.Hits {
		if hit.ObjectNum == 7 {
			found = true
		}
	}
	if !found {
		t.Errorf("Scan.Hits = %+v, want the /Info copy at object 7", report.Scan.Hits)
	}
	// The annotation scrub was correct, so its work stands rather than being
	// thrown away along with the finding.
	if len(alloc.Pending()) == 0 {
		t.Errorf("the queued scrubs were rolled back, losing correct work and the finding")
	}
}

func TestSanitizeDocument_ARefusalRollsBackTheEarlierPasses(t *testing.T) {
	// The load-bearing case. Annotations are scrubbed BEFORE outlines, so the
	// annotation rewrite is already queued when the outline pass refuses over an
	// item written directly into /First with no object number to replace.
	//
	// Ordering them this way is what makes a missing rollback visible: with one
	// refusing subject and nothing before it, an absent Restore looks identical
	// to a working one.
	doc := sanitizeFixture(t, "/Outlines 20 0 R",
		"20 0 obj\n<< /Type /Outlines /First << /Title (SECRET) >> >>\nendobj\n", "")
	alloc := writer.NewObjAllocator(30)

	_, err := SanitizeDocument(context.Background(), alloc, doc, []string{"SECRET"})
	if !errors.Is(err, ErrOutlineItemNotAddressable) {
		t.Fatalf("err = %v, want ErrOutlineItemNotAddressable", err)
	}
	if len(alloc.Pending()) != 0 {
		t.Errorf("allocator holds %d objects after a refusal, want none: the annotation scrub was not rolled back",
			len(alloc.Pending()))
	}
}

func TestSanitizeDocument_NoTokensIsNotAFailure(t *testing.T) {
	// A redaction that removed nothing leaves nothing to clear and nothing to
	// verify. Refusing here would make an empty RedactReport look like a leak.
	doc := sanitizeFixture(t, "", "", "")
	alloc := writer.NewObjAllocator(30)

	report, err := SanitizeDocument(context.Background(), alloc, doc, nil)
	if err != nil {
		t.Fatalf("SanitizeDocument: %v", err)
	}
	if len(report.Annotations) != 0 || len(report.Scan.Hits) != 0 {
		t.Errorf("report = %+v, want empty", report)
	}
	if len(alloc.Pending()) != 0 {
		t.Errorf("allocator holds %d objects, want none", len(alloc.Pending()))
	}
}
