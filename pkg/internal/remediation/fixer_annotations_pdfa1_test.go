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
// survived, unrepaired even in aggressive mode.
//
// # Corpus evidence
//
//   - All corpora: 44 corpus cells reported the surviving FileAttachment,
//     unrepaired even in aggressive mode.
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
// forbids every type not defined in PDF Reference, Polygon, PolyLine, Caret,
// Watermark and Redact among them.
//
// # Corpus evidence
//
//   - All corpora: a census of the corpus cells that stayed broken found
//     exactly Polygon, PolyLine, Caret, Watermark and Redact: 40 cells whose
//     only remaining violation was this rule.
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

// TestFixAnnotations_LaterPartsKeepTheNewerSubtypes pins the types parts 2 and
// 3 permit. Polygon, Redact and Watermark are all in compliance's
// annotWhitelistPart23, so those profiles must leave them be.
//
// Projection is absent from this list because parts 2 and 3 FORBID it: it is in
// compliance's part-4 set, not the part-2/3 one. The discrepancy that once made
// it untestable (remediation permitting it for every part) is what
// [permittedAnnotSubtypesFor] now resolves, and a dedicated test covers it.
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

// sameSubtypeSet reports whether two subtype tables hold the same names.
func sameSubtypeSet(a, b map[ast.PdfName]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for name := range a {
		if !b[name] {
			return false
		}
	}
	return true
}

// TestPermittedAnnotSubtypesFor_SelectsThePartAndLevel pins the selection
// against compliance's checkForbiddenAnnotType, which is what raises the rule.
//
// The bare "PDF/A-4" case is the one worth stating: [compliance.PDFALevel]
// returns the identifier's raw last byte, so that profile yields '4' rather than
// an absent level, and the default arm has to catch it.
func TestPermittedAnnotSubtypesFor_SelectsThePartAndLevel(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		profile string
		want    map[ast.PdfName]bool
	}{
		{"PDF/A-1a", pdfa1PermittedAnnotSubtypes},
		{"PDF/A-1b", pdfa1PermittedAnnotSubtypes},
		{"PDF/A-2b", pdfa23PermittedAnnotSubtypes},
		{"PDF/A-3u", pdfa23PermittedAnnotSubtypes},
		{"PDF/A-4", pdfa4PermittedAnnotSubtypes},
		{"PDF/A-4e", pdfa4ePermittedAnnotSubtypes},
		{"PDF/A-4f", pdfa4fPermittedAnnotSubtypes},
	} {
		if got := permittedAnnotSubtypesFor(tc.profile); !sameSubtypeSet(got, tc.want) {
			t.Errorf("%s selected the wrong table (%d entries)", tc.profile, len(got))
		}
	}

	// A non-PDF/A profile must yield nil, or the strict check would fire where
	// no PDF/A part governs the annotation set at all.
	for _, profile := range []string{"PDF/UA-1", "PDF/UA-2", "WTPDF-1.0-Reuse", ""} {
		if permittedAnnotSubtypesFor(profile) != nil {
			t.Errorf("%s: expected nil, the strict check must not apply", profile)
		}
	}
}

// TestPermittedAnnotSubtypes_MirrorTheCompliancePartTables pins the copies
// against their sources by size and by the differences that distinguish them.
// Drift toward over-removal destroys content, so these are asserted rather than
// sampled.
func TestPermittedAnnotSubtypes_MirrorTheCompliancePartTables(t *testing.T) {
	t.Parallel()

	sizes := map[string]struct {
		set  map[ast.PdfName]bool
		want int
	}{
		"pdfa1":  {pdfa1PermittedAnnotSubtypes, 16},
		"part23": {pdfa23PermittedAnnotSubtypes, 22},
		"part4":  {pdfa4PermittedAnnotSubtypes, 22},
		"part4e": {pdfa4ePermittedAnnotSubtypes, 24},
		"part4f": {pdfa4fPermittedAnnotSubtypes, 23},
	}
	for name, s := range sizes {
		if len(s.set) != s.want {
			t.Errorf("%s has %d entries, want %d", name, len(s.set), s.want)
		}
	}

	// The part-2/3 and part-4 sets are NOT nested: each permits one type the
	// other forbids. A superset assumption would silently keep FileAttachment
	// under part 4, which is the defect this table set exists to repair.
	if !pdfa23PermittedAnnotSubtypes["FileAttachment"] || pdfa4PermittedAnnotSubtypes["FileAttachment"] {
		t.Error("FileAttachment must be permitted by parts 2/3 and forbidden by part 4")
	}
	if pdfa23PermittedAnnotSubtypes["Projection"] || !pdfa4PermittedAnnotSubtypes["Projection"] {
		t.Error("Projection must be forbidden by parts 2/3 and permitted by part 4")
	}

	// The two level variants differ from part 4 by exactly one addition each.
	if !pdfa4ePermittedAnnotSubtypes["3D"] || !pdfa4ePermittedAnnotSubtypes["RichMedia"] {
		t.Error("level E must add 3D and RichMedia")
	}
	if !pdfa4fPermittedAnnotSubtypes["FileAttachment"] {
		t.Error("level F must re-add FileAttachment")
	}
	// The delta helper must not alias its base.
	if pdfa4PermittedAnnotSubtypes["3D"] || pdfa4PermittedAnnotSubtypes["FileAttachment"] {
		t.Error("the part-4 base was mutated by a delta")
	}
}

// TestFixAnnotations_Part4DropsFileAttachment covers ISO 19005-4 6.3.1 t1,
// which stayed unrepaired: part 4 and level E forbid FileAttachment, but
// remediation held one parts-2+ table that permits it.
//
// # Corpus evidence
//
//   - All corpora: 24 corpus cells reported ISO 19005-4 6.3.1 t1 unrepaired.
func TestFixAnnotations_Part4DropsFileAttachment(t *testing.T) {
	t.Parallel()

	for _, profile := range []string{"PDF/A-4", "PDF/A-4e"} {
		if subtypeSurvives(t, "FileAttachment", profile, false) {
			t.Errorf("%s: FileAttachment survived although the profile forbids it", profile)
		}
	}
}

// TestFixAnnotations_Part4fKeepsFileAttachment is the counterweight: level F
// re-permits the subtype, so the same repair must leave it alone.
//
// Level E's own additions, 3D and RichMedia, are deliberately NOT asserted here.
// They are removed by the two generic terms in the predicate, which run ungated
// and predate this table set: "3D" is in [forbiddenAnnotSubtypes], which the
// predicate applies without consulting the level though level E permits it,
// and "RichMedia" is simply absent from [permittedAnnotSubtypes].
// That over-removal is real but LATENT: nothing can falsify a fix for it yet,
// and an unfalsifiable guard is worse than a recorded defect.
//
// # Corpus evidence
//
//   - verapdf: no fixture carries either subtype, so the corpus gate cannot
//     falsify a fix for the over-removal.
func TestFixAnnotations_Part4fKeepsFileAttachment(t *testing.T) {
	t.Parallel()

	if !subtypeSurvives(t, "FileAttachment", "PDF/A-4f", false) {
		t.Error("PDF/A-4f: FileAttachment was removed although level F permits it")
	}
}

// TestFixAnnotations_Parts23DropProjection covers the other side of the
// two-way difference: parts 2 and 3 forbid Projection while permitting
// FileAttachment, the reverse of part 4.
func TestFixAnnotations_Parts23DropProjection(t *testing.T) {
	t.Parallel()

	for _, profile := range []string{"PDF/A-2b", "PDF/A-3b"} {
		if subtypeSurvives(t, "Projection", profile, false) {
			t.Errorf("%s: Projection survived although the profile forbids it", profile)
		}
		if !subtypeSurvives(t, "FileAttachment", profile, false) {
			t.Errorf("%s: FileAttachment was removed although the profile permits it", profile)
		}
	}
}

// TestFixAnnotations_StrictCheckIsGatedAndPDFAOnly is the pair of controls for
// the gate itself: standard mode declines the removal, and a non-PDF/A profile
// never reaches the strict set at all.
func TestFixAnnotations_StrictCheckIsGatedAndPDFAOnly(t *testing.T) {
	t.Parallel()

	if !subtypeSurvives(t, "FileAttachment", "PDF/A-4", true) {
		t.Error("PreserveContent: FileAttachment was removed in standard mode")
	}
	// Widget is permitted everywhere, so this asserts the non-PDF/A path runs at
	// all rather than that nothing is ever removed.
	if !subtypeSurvives(t, "Widget", "PDF/UA-1", false) {
		t.Error("PDF/UA-1: a permitted subtype was removed")
	}
}
