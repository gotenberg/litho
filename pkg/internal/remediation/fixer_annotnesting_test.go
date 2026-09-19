package remediation

import (
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// annotNestingPDF builds a tagged single-page PDF whose page carries a Link
// (object 6), a Text (7) and a PrinterMark (8) annotation, none of which has a
// /StructParent. Object 5 is the Document element and object 9 the ParentTree.
func annotNestingPDF() []byte {
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [6 0 R 7 0 R 8 0 R] >>",
		"<< /Type /StructTreeRoot /K 5 0 R /ParentTree 9 0 R >>",
		"<< /Type /StructElem /S /Document /P 4 0 R >>",
		"<< /Type /Annot /Subtype /Link /Rect [0 0 10 10] >>",
		"<< /Type /Annot /Subtype /Text /Rect [0 0 10 10] >>",
		"<< /Type /Annot /Subtype /PrinterMark /Rect [0 0 10 10] >>",
		"<< /Nums [] >>",
	)
}

// pendingStructElem finds the queued structure element of the given standard
// type, or fails. The fixer allocates fresh object numbers, so the element is
// located by shape rather than by a number the test would have to predict.
func pendingStructElem(t *testing.T, alloc *writer.ObjAllocator, role ast.PdfName) ast.PdfDict {
	t.Helper()
	for _, obj := range alloc.Pending() {
		d, ok := obj.(ast.PdfDict)
		if !ok {
			continue
		}
		if d["Type"] == ast.PdfName("StructElem") && d["S"] == role {
			return d
		}
	}
	t.Fatalf("no queued structure element with /S /%s", role)
	return nil
}

func TestMatchAnnotNesting(t *testing.T) {
	t.Parallel()
	for _, r := range []compliance.Rule{
		{Specification: "ISO_14289_1", Clause: "7.18.1", TestNumber: 1},
		{Specification: "ISO_14289_1", Clause: "7.18.4", TestNumber: 1},
		{Specification: "ISO_14289_1", Clause: "7.18.5", TestNumber: 1},
	} {
		if !matchAnnotNesting(compliance.Violation{Rule: r}) {
			t.Errorf("must match %s t%d", r.Clause, r.TestNumber)
		}
	}
	// §7.18.1 t2 asks for a description, which litho does not fabricate, and
	// the other t2 rules are separate requirements this fixer does not meet.
	for _, r := range []compliance.Rule{
		{Specification: "ISO_14289_1", Clause: "7.18.1", TestNumber: 2},
		{Specification: "ISO_14289_1", Clause: "7.18.4", TestNumber: 2},
		{Specification: "ISO_14289_1", Clause: "7.18.5", TestNumber: 2},
		{Specification: "ISO_14289_1", Clause: "7.18.3", TestNumber: 1},
		{Specification: "ISO_14289_2", Clause: "8.9.2.3", TestNumber: 1},
	} {
		if matchAnnotNesting(compliance.Violation{Rule: r}) {
			t.Errorf("must not match %s %s t%d", r.Specification, r.Clause, r.TestNumber)
		}
	}
}

// TestFixAnnotNesting_NestsBySubtype pins the per-subtype target type of
// ISO 14289-1 §7.18.1 t1, §7.18.4 t1 and §7.18.5 t1, and the OBJR child that
// keeps a Form conformant with §7.18.4 t2.
func TestFixAnnotNesting_NestsBySubtype(t *testing.T) {
	t.Parallel()
	doc, alloc := newTestDoc(t, annotNestingPDF())

	descs, err := fixAnnotNesting(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixAnnotNesting: %v", err)
	}
	if len(descs) != 1 || !strings.Contains(descs[0], "2 annotation") {
		t.Fatalf("expected one description covering 2 annotations, got %v", descs)
	}

	// The Link goes in a Link element, the Text in an Annot element.
	link := pendingStructElem(t, alloc, "Link")
	annot := pendingStructElem(t, alloc, "Annot")
	for role, elem := range map[ast.PdfName]ast.PdfDict{"Link": link, "Annot": annot} {
		kids, ok := elem["K"].(ast.PdfArray)
		if !ok || len(kids) != 1 {
			t.Errorf("/%s element /K: got %v, want a one-element array", role, elem["K"])
			continue
		}
		objr, ok := kids[0].(ast.PdfDict)
		if !ok || objr["Type"] != ast.PdfName("OBJR") {
			t.Errorf("/%s element child: got %v, want an OBJR dict", role, kids[0])
		}
	}

	// Both repaired annotations point back through /StructParent; the
	// PrinterMark is untouched, since §7.18.8 keeps it out of the structure.
	if sp := pendingDict(t, alloc, 6)["StructParent"]; sp != ast.PdfInt(0) {
		t.Errorf("Link /StructParent: got %v, want 0", sp)
	}
	if sp := pendingDict(t, alloc, 7)["StructParent"]; sp != ast.PdfInt(1) {
		t.Errorf("Text /StructParent: got %v, want 1", sp)
	}
	if _, queued := alloc.Pending()[8]; queued {
		t.Error("PrinterMark must not be given a structure parent (ISO 14289-1 §7.18.8)")
	}
}

// TestFixAnnotNesting_PDFUA2IsUntouched pins the profile scope. ISO 14289-1
// §7.18.1 requires a Popup to be nested while ISO 14289-2 §8.9.2.4.9 requires
// it to be an artifact, so running under PDF/UA-2 would fight [fixAnnotArtifact].
func TestFixAnnotNesting_PDFUA2IsUntouched(t *testing.T) {
	t.Parallel()
	doc, alloc := newTestDoc(t, annotNestingPDF())

	descs, err := fixAnnotNesting(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-2")
	if err != nil {
		t.Fatalf("fixAnnotNesting: %v", err)
	}
	if len(descs) != 0 || len(alloc.Pending()) != 0 {
		t.Errorf("PDF/UA-2 must be left alone: descs=%v pending=%d", descs, len(alloc.Pending()))
	}
}
