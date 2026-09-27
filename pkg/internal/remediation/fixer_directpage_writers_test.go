package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// TestPageWriters_WriteAPageWrittenDirectlyInKids pins that the fixers
// rewriting a page dictionary write a page written directly in its parent's
// /Kids back into that array ([writeTreeNode]), and report only what landed.
//
// ISO 32000-1, Table 29 makes every element of /Kids an indirect reference.
// Each of these fixers wrote the page at its object number, which a direct page
// does not have, so the write was skipped or refused and the repair dropped,
// while the fixer still reported it. [pdf.Writer] numbers such a page before
// any fixer runs; these fixers must not depend on it.
func TestPageWriters_WriteAPageWrittenDirectlyInKids(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	const annot = "<< /Type /Annot /Subtype /Text /Rect [10 10 30 30] /Contents (note)"
	for _, tc := range []struct {
		name       string
		directPage string
		fix        func(*writer.ObjAllocator, *document.Document) ([]string, error)
		check      func(*testing.T, ast.PdfDict)
	}{
		{
			name:       "annotations remove the page /AA",
			directPage: "/AA << /O << /S /JavaScript /JS (app.alert\\(1\\)) >> >>",
			fix: func(alloc *writer.ObjAllocator, doc *document.Document) ([]string, error) {
				return fixAnnotations(ctx, alloc, doc, Options{}, nil, "PDF/A-1b")
			},
			check: func(t *testing.T, page ast.PdfDict) {
				if _, ok := page["AA"]; ok {
					t.Errorf("the page keeps its /AA: %v", page)
				}
			},
		},
		{
			name:       "annotations rebuild the page /Annots",
			directPage: "/Annots [<< /Type /Annot /Subtype /Sound /Rect [10 10 30 30] /F 4 >> " + annot + " /F 4 >>]",
			fix: func(alloc *writer.ObjAllocator, doc *document.Document) ([]string, error) {
				return fixAnnotations(ctx, alloc, doc, Options{}, nil, "PDF/A-2b")
			},
			check: func(t *testing.T, page ast.PdfDict) {
				assertAnnotSubtypes(t, page, "Text")
			},
		},
		{
			name:       "annot-flags set the print flag",
			directPage: "/Annots [" + annot + " >>]",
			fix: func(alloc *writer.ObjAllocator, doc *document.Document) ([]string, error) {
				return fixAnnotFlags(ctx, alloc, doc, Options{}, nil, "PDF/A-2b")
			},
			check: func(t *testing.T, page ast.PdfDict) {
				annots, _ := page["Annots"].(ast.PdfArray)
				if len(annots) != 1 {
					t.Fatalf("the page holds /Annots %v, want the one annotation", page["Annots"])
				}
				dict, _ := annots[0].(ast.PdfDict)
				if flags, _ := dict["F"].(ast.PdfInt); flags&4 == 0 {
					t.Errorf("the annotation's /F is %v, want the print flag set", dict["F"])
				}
			},
		},
		{
			name:       "annot-flags remove an annotation that shows nowhere",
			directPage: "/Annots [" + annot + " /F 6 >> " + annot + " /F 4 >>]",
			fix: func(alloc *writer.ObjAllocator, doc *document.Document) ([]string, error) {
				return fixAnnotFlags(ctx, alloc, doc, Options{}, nil, "PDF/A-2b")
			},
			check: func(t *testing.T, page ast.PdfDict) {
				annots, _ := page["Annots"].(ast.PdfArray)
				if len(annots) != 1 {
					t.Errorf("the page holds /Annots %v, want the shown annotation alone", page["Annots"])
				}
			},
		},
		{
			name:       "forbidden-annots remove a Sound annotation",
			directPage: "/Annots [<< /Type /Annot /Subtype /Sound /Rect [10 10 30 30] /F 4 >> " + annot + " /F 4 >>]",
			fix: func(alloc *writer.ObjAllocator, doc *document.Document) ([]string, error) {
				return fixForbiddenAnnots(ctx, alloc, doc, Options{}, nil, "PDF/UA-2")
			},
			check: func(t *testing.T, page ast.PdfDict) {
				assertAnnotSubtypes(t, page, "Text")
			},
		},
		{
			name:       "javascript cuts the page /AA action",
			directPage: "/AA << /O << /S /JavaScript /JS (app.alert\\(1\\)) >> >>",
			fix: func(alloc *writer.ObjAllocator, doc *document.Document) ([]string, error) {
				return fixJavaScript(ctx, alloc, doc, Options{}, nil, "PDF/A-2b")
			},
			check: func(t *testing.T, page ast.PdfDict) {
				if aa, ok := page["AA"].(ast.PdfDict); ok && aa["O"] != nil {
					t.Errorf("the page keeps its JavaScript /AA action: %v", page["AA"])
				}
			},
		},
		{
			name:       "page-boxes drop a degenerate box",
			directPage: "/CropBox [0 0 0 0]",
			fix: func(alloc *writer.ObjAllocator, doc *document.Document) ([]string, error) {
				return fixPageBoxes(ctx, alloc, doc, Options{}, nil, "")
			},
			check: func(t *testing.T, page ast.PdfDict) {
				if _, ok := page["CropBox"]; ok {
					t.Errorf("the page keeps its degenerate /CropBox: %v", page["CropBox"])
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			doc, alloc := newTestDoc(t, reachTreePDF(reachTree{directPage: tc.directPage}))
			fixes, err := tc.fix(alloc, doc)
			if err != nil {
				t.Fatalf("fix: %v", err)
			}
			if len(fixes) == 0 {
				t.Errorf("the fixer reports no repair")
			}
			assertPending(t, alloc, "the node listing the page", reachDirectNode)
			page := liveDirectPage(t, alloc, doc)
			tc.check(t, page)
			if _, ok := page["Contents"]; !ok {
				t.Errorf("the page lost its /Contents: %v", page)
			}
		})
	}
}

// assertAnnotSubtypes fails unless the /Annots page holds, each written
// inline, are annotations of the subtypes want, in order.
func assertAnnotSubtypes(t *testing.T, page ast.PdfDict, want ...ast.PdfName) {
	t.Helper()

	annots, _ := page["Annots"].(ast.PdfArray)
	got := make([]ast.PdfName, 0, len(annots))
	for _, a := range annots {
		dict, _ := a.(ast.PdfDict)
		subtype, _ := dict["Subtype"].(ast.PdfName)
		got = append(got, subtype)
	}
	if len(got) != len(want) {
		t.Fatalf("the page holds annotations %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("the page holds annotations %v, want %v", got, want)
		}
	}
}
