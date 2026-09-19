package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// pdfa1AttachmentFixture reuses [fileAttachmentPDF], which already builds the
// shape this repair targets: one page whose /Annots holds a single
// FileAttachment annotation (object 4) pointing at a file specification
// (object 5).
func pdfa1AttachmentFixture() []byte {
	return fileAttachmentPDF("<< /Type /Filespec /F (data.bin) /EF << /F 6 0 R >> >>")
}

// annotsAfterFix runs the whole annotations fixer and reports the page's
// rewritten /Annots array together with whether the annotation object was
// nulled. The annotation is object 4 and the page object 3, per the fixture.
func annotsAfterFix(t *testing.T, profile string, preserve bool) (ast.PdfArray, bool) {
	t.Helper()

	doc, alloc := newTestDoc(t, pdfa1AttachmentFixture())
	if _, err := fixAnnotations(context.Background(), alloc, doc,
		Options{PreserveContent: preserve}, nil, profile); err != nil {
		t.Fatalf("fixAnnotations: %v", err)
	}

	var annots ast.PdfArray
	if page, ok := alloc.Pending()[3].(ast.PdfDict); ok {
		annots, _ = page["Annots"].(ast.PdfArray)
	}
	_, nulled := alloc.Pending()[4].(ast.PdfNull)
	return annots, nulled
}

// TestFixAnnotations_DropsAPDFA1FileAttachment pins the repair of
// ISO 19005-1 6.5.2 t1. FileAttachment sits in [permittedAnnotSubtypes] because
// parts 2 and later allow it, so Part 1 was served only by accident: Sound and
// Movie were removed through the parts-2+ forbidden set while FileAttachment
// survived. 44 corpus cells reported it, unrepaired even in aggressive mode.
func TestFixAnnotations_DropsAPDFA1FileAttachment(t *testing.T) {
	t.Parallel()

	annots, nulled := annotsAfterFix(t, "PDF/A-1b", false)
	if len(annots) != 0 {
		t.Errorf("the FileAttachment annotation survived in /Annots: %v", annots)
	}
	if !nulled {
		t.Error("the annotation object was left live; an orphaned /Type /Filespec still fails the embedded-file rules")
	}
}

// TestFixAnnotations_PreserveContentKeepsTheAttachment is the first negative
// control: dropping an annotation destroys content the author attached, so
// standard mode must leave it in place.
func TestFixAnnotations_PreserveContentKeepsTheAttachment(t *testing.T) {
	t.Parallel()

	annots, nulled := annotsAfterFix(t, "PDF/A-1b", true)
	if nulled {
		t.Error("the annotation was nulled while preserving content")
	}
	if annots != nil && len(annots) == 0 {
		t.Error("/Annots was emptied while preserving content")
	}
}

// TestFixAnnotations_LaterPartsKeepTheAttachment is the control that matters
// most: parts 2, 3 and 4 permit FileAttachment, and PDF/A-3 is the basis of
// Factur-X, whose invoice payload is delivered as an attachment.
//
// The profile spellings are exact on purpose. [compliance.PDFAPart] matches
// whole identifiers, and the level-less "PDF/A-1" returns 0, which would make
// this control pass while testing nothing.
func TestFixAnnotations_LaterPartsKeepTheAttachment(t *testing.T) {
	t.Parallel()

	for _, profile := range []string{"PDF/A-2b", "PDF/A-3b", "PDF/A-4f"} {
		annots, nulled := annotsAfterFix(t, profile, false)
		if nulled {
			t.Errorf("%s: the annotation was nulled in a profile that permits it", profile)
		}
		if annots != nil && len(annots) == 0 {
			t.Errorf("%s: /Annots was emptied in a profile that permits FileAttachment", profile)
		}
	}
}
