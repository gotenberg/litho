package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

func TestMatchNoteID(t *testing.T) {
	t.Parallel()
	if !matchNoteID(compliance.Violation{Rule: compliance.Rule_14289_1_7_9_t01}) {
		t.Error("must match the Note /ID presence rule (§7.9 t1)")
	}
	// t2 (uniqueness) is intentionally not claimed: renaming an existing /ID is unsafe.
	if matchNoteID(compliance.Violation{Rule: compliance.Rule_14289_1_7_9_t02}) {
		t.Error("must not claim the duplicate-/ID rule (§7.9 t2)")
	}
}

func TestFixNoteID_AddsMissingID(t *testing.T) {
	// StructTreeRoot(4) -> Document(5) -> two Note elements (6 has no /ID, 7 does).
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K 5 0 R >>",
		"<< /Type /StructElem /S /Document /K [6 0 R 7 0 R] >>",
		"<< /Type /StructElem /S /Note >>",
		"<< /Type /StructElem /S /Note /ID (existing-id) >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixNoteID(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixNoteID: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 fix description, got %v", descs)
	}
	id, ok := pendingDict(t, alloc, 6)["ID"].(ast.PdfString)
	if !ok || id == "" {
		t.Fatalf("Note 6 /ID not added or empty: %v", pendingDict(t, alloc, 6)["ID"])
	}
	if string(id) == "existing-id" {
		t.Error("generated /ID collides with the existing Note /ID")
	}
	// The Note that already had an /ID must not be rewritten.
	if _, queued := alloc.Pending()[7]; queued {
		t.Error("Note with an existing /ID must not be modified")
	}
}

func TestFixNoteID_AllPresentUntouched(t *testing.T) {
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K 5 0 R >>",
		"<< /Type /StructElem /S /Note /ID (note-a) >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixNoteID(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixNoteID: %v", err)
	}
	if len(descs) != 0 {
		t.Errorf("all Notes have /ID: got %v, want no fixes", descs)
	}
	if _, queued := alloc.Pending()[5]; queued {
		t.Error("a Note that already has /ID must not be queued")
	}
}

func TestFixNoteID_NonNoteUntouched(t *testing.T) {
	// A non-Note element without /ID is not given one.
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K 5 0 R >>",
		"<< /Type /StructElem /S /P >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixNoteID(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixNoteID: %v", err)
	}
	if len(descs) != 0 {
		t.Errorf("non-Note element: got %v, want no fixes", descs)
	}
	if _, queued := alloc.Pending()[5]; queued {
		t.Error("non-Note element must not receive an /ID")
	}
}
