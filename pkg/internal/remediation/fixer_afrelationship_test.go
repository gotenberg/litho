package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
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
