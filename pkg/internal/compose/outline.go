package compose

import (
	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// outlines writes the outline tree (ISO 32000-1, 12.3.3, Tables 152 and
// 153) and returns its root. Every entry is open, so each /Count is the
// number of its descendants. The entries have passed [Document.Validate]:
// the recursion is bounded by [maxTreeDepth].
func (w *docWriter) outlines(entries []Outline) ast.PdfIndirectRef {
	root := w.alloc.Alloc(ast.PdfNull{})
	first, last, count := w.outlineLevel(entries, root)
	w.alloc.AllocAt(root.ObjectNum, ast.PdfDict{
		"Type":  ast.PdfName("Outlines"),
		"First": first,
		"Last":  last,
		"Count": ast.PdfInt(count),
	})
	return root
}

// outlineLevel writes entries as the children of parent, linked through
// /Prev and /Next, and returns the first, the last and the number of
// entries written, descendants included.
func (w *docWriter) outlineLevel(entries []Outline, parent ast.PdfIndirectRef) (first, last ast.PdfIndirectRef, count int) {
	refs := make([]ast.PdfIndirectRef, len(entries))
	for i := range entries {
		refs[i] = w.alloc.Alloc(ast.PdfNull{})
	}
	for i, e := range entries {
		dict := ast.PdfDict{
			"Title":  textString(e.Title),
			"Parent": parent,
			"Dest":   w.dest(e.Dest),
		}
		if i > 0 {
			dict["Prev"] = refs[i-1]
		}
		if i < len(entries)-1 {
			dict["Next"] = refs[i+1]
		}
		count++
		if len(e.Kids) > 0 {
			f, l, c := w.outlineLevel(e.Kids, refs[i])
			dict["First"], dict["Last"], dict["Count"] = f, l, ast.PdfInt(c)
			count += c
		}
		w.alloc.AllocAt(refs[i].ObjectNum, dict)
	}
	return refs[0], refs[len(refs)-1], count
}

// dest returns d as an explicit /XYZ destination (ISO 32000-1, 12.3.2.2,
// Table 151): the page, the point at the window's top-left corner in PDF
// coordinates, y flipped from the model's, and a null zoom that keeps the
// viewer's.
func (w *docWriter) dest(d Dest) ast.PdfArray {
	page := w.doc.Pages[d.Page-1]
	return ast.PdfArray{
		w.pageRefs[d.Page-1],
		ast.PdfName("XYZ"),
		pdfReal(d.X),
		pdfReal(page.Height - d.Y),
		ast.PdfNull{},
	}
}
