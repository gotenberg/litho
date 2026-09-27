package mutations

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

func box(v ...float64) ast.PdfArray {
	arr := make(ast.PdfArray, len(v))
	for i, x := range v {
		arr[i] = ast.PdfReal(x)
	}
	return arr
}

// TestMaterializeInheritedAttrs_CarriesCropBox pins that a page inheriting a
// /CropBox keeps its visible region when the page tree is flattened.
//
// /CropBox is inheritable (ISO 32000-1, 7.7.3.4) and defines the region a
// viewer displays (14.11.2). Materialising only Resources, MediaBox and Rotate
// dropped it, so a page cropped on its /Pages node was redisplayed at its
// /MediaBox after a merge or split.
//
// # Corpus evidence
//
//   - verapdf 6-1-13-t09-fail-c: crops a 2x2 /MediaBox to a 4x4 region on its
//     /Pages node, and merging or splitting the file redisplayed the page at
//     2x2.
func TestMaterializeInheritedAttrs_CarriesCropBox(t *testing.T) {
	page := &document.Page{
		MediaBox: box(0, 0, 2, 2),
		CropBox:  box(0, 0, 4, 4),
	}
	dict := ast.PdfDict{"Type": ast.PdfName("Page")}
	materializeInheritedAttrs(context.Background(), nil, nil, dict, page)

	got, ok := dict["CropBox"].(ast.PdfArray)
	if !ok {
		t.Fatalf("/CropBox = %v (%T), want the inherited 4x4 region", dict["CropBox"], dict["CropBox"])
	}
	if !sameBox(got, box(0, 0, 4, 4)) {
		t.Errorf("/CropBox = %v, want [0 0 4 4]", got)
	}
}

// TestMaterializeInheritedAttrs_SkipsRedundantCropBox pins that a page whose
// visible region already equals its /MediaBox gains no entry.
//
// [document.Page.CropBox] is the EFFECTIVE region and falls back to the
// /MediaBox, so writing it unconditionally would add a redundant /CropBox to
// every copied page in every document.
func TestMaterializeInheritedAttrs_SkipsRedundantCropBox(t *testing.T) {
	page := &document.Page{
		MediaBox: box(0, 0, 612, 792),
		CropBox:  box(0, 0, 612, 792),
	}
	dict := ast.PdfDict{"Type": ast.PdfName("Page")}
	materializeInheritedAttrs(context.Background(), nil, nil, dict, page)

	if _, has := dict["CropBox"]; has {
		t.Errorf("/CropBox = %v was written though it matches the /MediaBox", dict["CropBox"])
	}
}

// TestMaterializeInheritedAttrs_KeepsOwnCropBox pins that a page carrying its
// own /CropBox is not overwritten by the inherited one.
func TestMaterializeInheritedAttrs_KeepsOwnCropBox(t *testing.T) {
	page := &document.Page{
		MediaBox: box(0, 0, 2, 2),
		CropBox:  box(0, 0, 4, 4),
	}
	dict := ast.PdfDict{"Type": ast.PdfName("Page"), "CropBox": box(0, 0, 1, 1)}
	materializeInheritedAttrs(context.Background(), nil, nil, dict, page)

	got, _ := dict["CropBox"].(ast.PdfArray)
	if !sameBox(got, box(0, 0, 1, 1)) {
		t.Errorf("/CropBox = %v, want the page's own [0 0 1 1]", got)
	}
}

// TestMaterializeInheritedAttrs_ReplacesNullEntries pins that a page stating
// an inheritable attribute as null gets the inherited value in a flat tree. A
// null dictionary value is an absent entry (ISO 32000-1, 7.3.7), so the page
// inherited the attribute (7.7.3.4), and keeping the null left it with none.
func TestMaterializeInheritedAttrs_ReplacesNullEntries(t *testing.T) {
	page := &document.Page{
		MediaBox: box(0, 0, 200, 300),
		CropBox:  box(0, 0, 100, 100),
		Rotate:   90,
	}
	dict := ast.PdfDict{
		"Type":     ast.PdfName("Page"),
		"MediaBox": ast.PdfNull{},
		"CropBox":  ast.PdfNull{},
		"Rotate":   ast.PdfNull{},
	}
	materializeInheritedAttrs(context.Background(), nil, nil, dict, page)

	if got, _ := dict["MediaBox"].(ast.PdfArray); !sameBox(got, page.MediaBox) {
		t.Errorf("/MediaBox = %v, want the inherited %v", dict["MediaBox"], page.MediaBox)
	}
	if got, _ := dict["CropBox"].(ast.PdfArray); !sameBox(got, page.CropBox) {
		t.Errorf("/CropBox = %v, want the inherited %v", dict["CropBox"], page.CropBox)
	}
	if got, _ := dict["Rotate"].(ast.PdfInt); got != 90 {
		t.Errorf("/Rotate = %v, want the inherited 90", dict["Rotate"])
	}
}
