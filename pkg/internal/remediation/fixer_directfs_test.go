package remediation

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// TestFixDirectFileSpecInAnnotWritesThroughAnnotation pins that a file
// specification written inline is repaired through the annotation holding it.
//
// ISO 32000-1, 7.11.3 does not require a file specification to be an indirect
// object, and an inline one has no object number the writer can address. The
// annotation is the nearest object that has one, so the whole annotation is
// rewritten with the patched /FS.
func TestFixDirectFileSpecInAnnotWritesThroughAnnotation(t *testing.T) {
	t.Parallel()

	alloc := writer.NewObjAllocator(10)
	fsDict := ast.PdfDict{"Type": ast.PdfName("Filespec"), "F": ast.PdfString("a.tex")}
	annotDict := ast.PdfDict{"Subtype": ast.PdfName("FileAttachment"), "FS": fsDict}
	annotRef := ast.PdfIndirectRef{ObjectNum: 403}

	if got := fixDirectFileSpecInAnnot(alloc, annotRef, annotDict, fsDict); got != 1 {
		t.Fatalf("count = %d, want 1", got)
	}

	written, ok := alloc.Pending()[403].(ast.PdfDict)
	if !ok {
		t.Fatalf("object 403 = %T, want the rewritten annotation", alloc.Pending()[403])
	}
	newFS, ok := written["FS"].(ast.PdfDict)
	if !ok {
		t.Fatalf("/FS = %T, want a dictionary", written["FS"])
	}
	if newFS["AFRelationship"] != ast.PdfName("Unspecified") {
		t.Errorf("/AFRelationship = %v, want /Unspecified", newFS["AFRelationship"])
	}
	// The annotation's other entries must survive the rewrite.
	if written["Subtype"] != ast.PdfName("FileAttachment") {
		t.Errorf("/Subtype = %v, want /FileAttachment", written["Subtype"])
	}
	if _, leaked := fsDict["AFRelationship"]; leaked {
		t.Error("the source file specification was mutated in place")
	}
}

// TestFixDirectFileSpecInAnnotSkipsUnwritableAnnotation checks that an
// annotation with no object number of its own reports no repair. Claiming a fix
// that was discarded makes remediation report progress it never made, and the
// defect is still there on the next run.
func TestFixDirectFileSpecInAnnotSkipsUnwritableAnnotation(t *testing.T) {
	t.Parallel()

	alloc := writer.NewObjAllocator(10)
	fsDict := ast.PdfDict{"Type": ast.PdfName("Filespec"), "F": ast.PdfString("a.tex")}
	annotDict := ast.PdfDict{"Subtype": ast.PdfName("FileAttachment"), "FS": fsDict}

	if got := fixDirectFileSpecInAnnot(alloc, annotDict, annotDict, fsDict); got != 0 {
		t.Errorf("count = %d, want 0 for an annotation that cannot be written", got)
	}
	if len(alloc.Pending()) != 0 {
		t.Errorf("%d objects written, want none", len(alloc.Pending()))
	}
}

// TestFixDirectFileSpecInAnnotLeavesNamedSpecsAlone checks the repair is a no-op
// once /AFRelationship is present, which is what keeps remediation idempotent.
func TestFixDirectFileSpecInAnnotLeavesNamedSpecsAlone(t *testing.T) {
	t.Parallel()

	alloc := writer.NewObjAllocator(10)
	fsDict := ast.PdfDict{
		"Type":           ast.PdfName("Filespec"),
		"F":              ast.PdfString("a.tex"),
		"AFRelationship": ast.PdfName("Data"),
	}
	annotDict := ast.PdfDict{"Subtype": ast.PdfName("FileAttachment"), "FS": fsDict}

	if got := fixDirectFileSpecInAnnot(alloc, ast.PdfIndirectRef{ObjectNum: 403}, annotDict, fsDict); got != 0 {
		t.Errorf("count = %d, want 0 when /AFRelationship is already present", got)
	}
	if len(alloc.Pending()) != 0 {
		t.Errorf("%d objects written, want none", len(alloc.Pending()))
	}
}
