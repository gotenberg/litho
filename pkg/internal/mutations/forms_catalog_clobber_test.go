package mutations

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// TestFlattenForm_KeepsAnEarlierMutationsCatalogWrite pins that a flatten
// queued after an attachment keeps the catalog entries the attachment wrote.
//
// Mutations queued on one pkg/pdf Writer share ONE allocator and are applied
// in order with no flush between them, so a mutation that rewrites a whole
// dictionary must read it through the allocator or it discards whatever an
// earlier mutation wrote to the same object.
//
// The catalog is where this matters most: twelve mutations write it.
// AddAttachment registers the file there (/Names, and /AF when asked) and
// FlattenForm removes /AcroForm from it, so attaching a file and then
// flattening the form is a pipeline a caller can reasonably build.
//
// Worth pinning because nothing else catches it. Remediation survives this
// class through the fix-to-fixpoint loop in pkg/pdf, which reopens the output
// and reapplies a discarded repair; mutations have no such loop, so a clobber
// here is permanent and reaches the output file.
//
// # Corpus evidence
//
//   - All corpora: the gate scores neither attachments nor flatten, so it
//     cannot see this either.
func TestFlattenForm_KeepsAnEarlierMutationsCatalogWrite(t *testing.T) {
	ctx := context.Background()
	doc := attachDoc(t,
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] /NeedAppearances true >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (Text1) /V (value) "+
			"/Rect [10 10 100 30] /F 4 >>",
	)
	alloc := writer.NewObjAllocator(4)

	if err := AddAttachmentWithOptions(ctx, alloc, doc, "att.txt", []byte("litho"),
		AttachmentOptions{MimeType: "text/plain", AFRelationship: "Data", AddToCatalogAF: true}); err != nil {
		t.Fatalf("AddAttachmentWithOptions: %v", err)
	}
	// The clobber is only meaningful if the attachment really did write the
	// catalog, so the test would be vacuous without this.
	before, pending := alloc.Pending()[1].(ast.PdfDict)
	if !pending {
		t.Fatalf("precondition: the attachment left no pending catalog")
	}
	if _, ok := before["Names"]; !ok {
		t.Fatalf("precondition: the attachment wrote no /Names, got %v", before)
	}

	if err := FlattenForm(ctx, alloc, doc); err != nil {
		t.Fatalf("FlattenForm: %v", err)
	}

	catalog, ok := alloc.Pending()[1].(ast.PdfDict)
	if !ok {
		t.Fatalf("catalog is %T, want a pending dict", alloc.Pending()[1])
	}

	// The flatten's own job must still be done: this is a composition test,
	// not an assertion that FlattenForm does nothing.
	if _, still := catalog["AcroForm"]; still {
		t.Errorf("/AcroForm survived the flatten, so the mutation did not run")
	}

	if _, ok := catalog["Names"]; !ok {
		t.Errorf("catalog /Names is gone: FlattenForm rebuilt the catalog from the on-disk "+
			"copy and discarded the attachment registration; catalog = %v", catalog)
	}
	if _, ok := catalog["AF"]; !ok {
		t.Errorf("catalog /AF is gone: the attachment's associated-file entry was discarded")
	}
}
