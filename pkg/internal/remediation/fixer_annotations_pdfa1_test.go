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

// annotSubtypePDF builds a page carrying one annotation of the given subtype.
// The annotation is object 4.
func annotSubtypePDF(subtype string) []byte {
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /"+subtype+" /Rect [0 0 20 20] >>",
	)
}

// subtypeSurvives reports whether an annotation of that subtype is still live
// after the fixer runs. Nulling the object is how the removal path marks it, so
// this is a positive check rather than an absence of one.
func subtypeSurvives(t *testing.T, subtype, profile string, preserve bool) bool {
	t.Helper()

	doc, alloc := newTestDoc(t, annotSubtypePDF(subtype))
	if _, err := fixAnnotations(context.Background(), alloc, doc,
		Options{PreserveContent: preserve}, nil, profile); err != nil {
		t.Fatalf("fixAnnotations: %v", err)
	}
	_, nulled := alloc.Pending()[4].(ast.PdfNull)
	return !nulled
}

// TestFixAnnotations_DropsEveryPDFA1ForbiddenSubtype covers BOTH clauses of
// ISO 19005-1 6.5.2 t1, not just the second.
//
// The first attempt enumerated forbidden subtypes and named only
// FileAttachment, which is the "additionally" clause. The clause before it
// forbids every type not defined in PDF Reference, and a census of the corpus
// cells that stayed broken found exactly Polygon, PolyLine, Caret, Watermark
// and Redact: 40 cells whose only remaining violation was this rule.
func TestFixAnnotations_DropsEveryPDFA1ForbiddenSubtype(t *testing.T) {
	t.Parallel()

	for _, subtype := range []string{
		"Polygon", "PolyLine", "Caret", "Watermark", "Redact", "Projection",
		"FileAttachment",
	} {
		if subtypeSurvives(t, subtype, "PDF/A-1b", false) {
			t.Errorf("%s survived: PDF/A-1 permits only the PDF 1.4 types", subtype)
		}
	}
}

// TestFixAnnotations_KeepsThePDFA1PermittedSubtypes is the counterweight. A
// whitelist that removed everything would satisfy the test above, so the
// permitted types must demonstrably survive the same run.
func TestFixAnnotations_KeepsThePDFA1PermittedSubtypes(t *testing.T) {
	t.Parallel()

	for _, subtype := range []string{
		"Text", "Link", "FreeText", "Line", "Square", "Circle", "Highlight",
		"Underline", "Squiggly", "StrikeOut", "Stamp", "Ink", "Popup",
		"Widget", "PrinterMark", "TrapNet",
	} {
		if !subtypeSurvives(t, subtype, "PDF/A-1b", false) {
			t.Errorf("%s was removed although ISO 19005-1 6.5.2 permits it", subtype)
		}
	}
}

// TestFixAnnotations_LaterPartsKeepTheNewerSubtypes pins that the stricter set
// applies to Part 1 alone. Polygon, Redact and Watermark are all in compliance's
// annotWhitelistPart23, so parts 2 and later must leave them be.
//
// Projection is deliberately absent from this list: remediation permits it while
// compliance's part-2/3 whitelist does not, so asserting either behaviour here
// would pin a discrepancy rather than a decision.
func TestFixAnnotations_LaterPartsKeepTheNewerSubtypes(t *testing.T) {
	t.Parallel()

	for _, profile := range []string{"PDF/A-2b", "PDF/A-3b"} {
		for _, subtype := range []string{"Polygon", "Redact", "Watermark"} {
			if !subtypeSurvives(t, subtype, profile, false) {
				t.Errorf("%s: %s was removed although the profile permits it", profile, subtype)
			}
		}
	}
}

// TestFixAnnotations_PreserveContentKeepsForbiddenSubtypes is the gate control:
// removing an annotation destroys content, so standard mode must decline even
// for a type Part 1 forbids.
func TestFixAnnotations_PreserveContentKeepsForbiddenSubtypes(t *testing.T) {
	t.Parallel()

	for _, subtype := range []string{"Polygon", "Redact", "FileAttachment"} {
		if !subtypeSurvives(t, subtype, "PDF/A-1b", true) {
			t.Errorf("%s was removed while preserving content", subtype)
		}
	}
}

// TestPDFA1PermittedAnnotSubtypes_MirrorsTheComplianceSet pins the copy against
// its source. [pdfa1PermittedAnnotSubtypes] duplicates compliance's unexported
// pdfa1AnnotWhitelist, and drift toward over-removal would destroy content, so
// the membership is asserted exactly rather than sampled.
func TestPDFA1PermittedAnnotSubtypes_MirrorsTheComplianceSet(t *testing.T) {
	t.Parallel()

	want := map[ast.PdfName]bool{
		"Text": true, "Link": true, "FreeText": true, "Line": true,
		"Square": true, "Circle": true, "Highlight": true, "Underline": true,
		"Squiggly": true, "StrikeOut": true, "Stamp": true, "Ink": true,
		"Popup": true, "Widget": true, "PrinterMark": true, "TrapNet": true,
	}
	if len(pdfa1PermittedAnnotSubtypes) != len(want) {
		t.Errorf("set size = %d, want %d", len(pdfa1PermittedAnnotSubtypes), len(want))
	}
	for name := range want {
		if !pdfa1PermittedAnnotSubtypes[name] {
			t.Errorf("%s missing: ISO 19005-1 6.5.2 permits it", name)
		}
	}
	for name := range pdfa1PermittedAnnotSubtypes {
		if !want[name] {
			t.Errorf("%s present: ISO 19005-1 6.5.2 does not permit it", name)
		}
	}
}
