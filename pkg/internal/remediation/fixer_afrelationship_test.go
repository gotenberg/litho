package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

func TestMatchAFRelationship(t *testing.T) {
	t.Parallel()
	for _, rule := range []compliance.Rule{
		compliance.Rule_19005_3_6_8_t03,        // PDF/A-3 §6.8 associated-file relationship
		compliance.Rule_14289_2_8_9_2_4_10_t01, // PDF/UA-2 FileAttachment /FS /AFRelationship
	} {
		if !matchAFRelationship(compliance.Violation{Rule: rule}) {
			t.Errorf("must match /AFRelationship rule %s/%s", rule.Specification, rule.Clause)
		}
	}
	// Must not claim an unrelated UA-2 annotation rule (e.g. Sound forbidden).
	if matchAFRelationship(compliance.Violation{Rule: compliance.Rule_14289_2_8_9_2_4_11_t01}) {
		t.Error("must not claim the Sound-annotation rule")
	}
}

// fileAttachmentPDF builds a single-page PDF with one FileAttachment annotation
// (object 4) whose /FS points at a file specification (object 5).
func fileAttachmentPDF(fileSpec string) []byte {
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /FileAttachment /Rect [0 0 20 20] /FS 5 0 R >>",
		fileSpec,
	)
}

func TestFixAFRelationship_AnnotationFileSpec(t *testing.T) {
	// FileAttachment /FS filespec lacking /AFRelationship gets /Unspecified.
	pdf := fileAttachmentPDF("<< /Type /Filespec /F (data.bin) /EF << /F 6 0 R >> >>")
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixAFRelationship(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-2")
	if err != nil {
		t.Fatalf("fixAFRelationship: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 fix description, got %v", descs)
	}
	if pendingDict(t, alloc, 5)["AFRelationship"] != ast.PdfName("Unspecified") {
		t.Errorf("/AFRelationship = %v, want Unspecified", pendingDict(t, alloc, 5)["AFRelationship"])
	}
}

func TestFixAFRelationship_AnnotationConformantUntouched(t *testing.T) {
	// A filespec that already carries /AFRelationship is not rewritten.
	pdf := fileAttachmentPDF("<< /Type /Filespec /F (data.bin) /AFRelationship /Data >>")
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixAFRelationship(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-2")
	if err != nil {
		t.Fatalf("fixAFRelationship: %v", err)
	}
	if len(descs) != 0 {
		t.Errorf("conformant filespec: got %v, want no fixes", descs)
	}
	if _, queued := alloc.Pending()[5]; queued {
		t.Error("conformant filespec must not be queued for replacement")
	}
}

// TestFixAFRelationship_LooseFilespecPart3 pins the PDF/A-3 association
// repair: a loose /Type /Filespec dict (no /EF, outside any name tree or
// annotation) gets /AFRelationship and a catalog /AF entry under a part-3
// target, and stays out of /AF under a part-2 target.
func TestFixAFRelationship_LooseFilespecPart3(t *testing.T) {
	build := func() (*document.Document, *writer.ObjAllocator) {
		pdf := buildStructPDF(
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
			"<< /Type /Filespec /F (loose.pdf) >>",
		)
		return newTestDoc(t, pdf)
	}

	doc, alloc := build()
	fixes, err := fixAFRelationship(context.Background(), alloc, doc, Options{}, nil, "PDF/A-3b")
	if err != nil {
		t.Fatalf("fixAFRelationship: %v", err)
	}
	if len(fixes) != 2 {
		t.Fatalf("part 3: got fixes %v, want AFRelationship + catalog /AF", fixes)
	}
	fs, ok := alloc.Pending()[4].(ast.PdfDict)
	if !ok || fs["AFRelationship"] != ast.PdfName("Unspecified") {
		t.Errorf("loose filespec not repaired: %v", alloc.Pending()[4])
	}
	cat, ok := pendingDict(t, alloc, 1)["AF"].(ast.PdfArray)
	if !ok || len(cat) != 1 {
		t.Fatalf("catalog /AF not wired: %v", pendingDict(t, alloc, 1)["AF"])
	}
	if ref, ok := cat[0].(ast.PdfIndirectRef); !ok || ref.ObjectNum != 4 {
		t.Errorf("catalog /AF entry should reference object 4, got %v", cat[0])
	}

	doc2, alloc2 := build()
	if _, err := fixAFRelationship(context.Background(), alloc2, doc2, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixAFRelationship part 2: %v", err)
	}
	if obj, ok := alloc2.Pending()[1]; ok {
		if d, ok := obj.(ast.PdfDict); ok {
			if _, has := d["AF"]; has {
				t.Error("part 2 target must not gain a catalog /AF array")
			}
		}
	}
}
