package mutations

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestUntagRemovedAnnotationsLeavesAnUntaggedDocument pins that a document with
// no structure tree has nothing to untag, and nothing is written.
func TestUntagRemovedAnnotationsLeavesAnUntaggedDocument(t *testing.T) {
	t.Parallel()

	ctx, doc, alloc := setFormDoc(t, []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /Square /Rect [0 0 10 10] /F 2 >>",
	})
	annot := ast.PdfDict{"Type": ast.PdfName("Annot"), "Subtype": ast.PdfName("Square")}
	err := UntagRemovedAnnotations(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: 1},
		[]RemovedAnnotation{{Obj: ast.PdfIndirectRef{ObjectNum: 4}, Dict: annot}})
	if err != nil {
		t.Fatalf("UntagRemovedAnnotations: %v", err)
	}
	if n := len(alloc.Pending()); n != 0 {
		t.Errorf("%d objects written for an untagged document, want none", n)
	}
}

// TestUntagRemovedAnnotationsDropsTheKeyOfAnInlineAnnotation pins the untagging
// of an annotation written directly into /Annots. No object reference can name
// it, so its /StructParent key is what ties it to an element, and the key
// leaves the /ParentTree while the other keys stay.
func TestUntagRemovedAnnotationsDropsTheKeyOfAnInlineAnnotation(t *testing.T) {
	t.Parallel()

	ctx, doc, alloc := setFormDoc(t, []string{
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " +
			"/Annots [<< /Type /Annot /Subtype /Square /Rect [0 0 10 10] /F 2 /StructParent 7 >> 8 0 R] >>",
		"<< /Type /StructTreeRoot /K 5 0 R /ParentTree << /Nums [7 6 0 R 9 5 0 R] >> >>",
		"<< /Type /StructElem /S /Document /P 4 0 R /K [6 0 R] >>",
		"<< /Type /StructElem /S /Annot /P 5 0 R >>",
		"<< /Type /Annot /Subtype /Square /Rect [0 0 10 10] /F 4 >>",
		"<< /Type /Annot /Subtype /Square /Rect [0 0 10 10] /F 4 >>",
	})
	inline := ast.PdfDict{
		"Type": ast.PdfName("Annot"), "Subtype": ast.PdfName("Square"),
		"F": ast.PdfInt(2), "StructParent": ast.PdfInt(7),
	}
	if err := UntagRemovedAnnotations(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: 1},
		[]RemovedAnnotation{{Obj: inline, Dict: inline}}); err != nil {
		t.Fatalf("UntagRemovedAnnotations: %v", err)
	}

	root, ok := alloc.Pending()[4].(ast.PdfDict)
	if !ok {
		t.Fatalf("the structure tree root was not rewritten: %T", alloc.Pending()[4])
	}
	tree, ok := root["ParentTree"].(ast.PdfDict)
	if !ok {
		t.Fatalf("/ParentTree = %T, want the rewritten tree", root["ParentTree"])
	}
	nums, _ := tree["Nums"].(ast.PdfArray)
	for i := 0; i+1 < len(nums); i += 2 {
		if nums[i] == ast.PdfInt(7) {
			t.Errorf("/ParentTree still lists key 7, the removed annotation's: %v", nums)
		}
	}
	if len(nums) != 2 {
		t.Errorf("/ParentTree /Nums = %v, want key 9 alone", nums)
	}
}
