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
		}
		key, target := w.target(e.Dest)
		dict[key] = target
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
	return ast.PdfArray{
		pageRef(d.Page - 1),
		ast.PdfName("XYZ"),
		pdfReal(d.X),
		pdfReal(w.heights[d.Page-1] - d.Y),
		ast.PdfNull{},
	}
}

// target returns the entry, and its value, that makes a link or an outline
// entry go to d: /Dest with the explicit destination, or, in a PDF 2.0 file
// where d names an element of the structure tree, /A with a go-to action
// carrying both the explicit destination and the structure destination
// (ISO 32000-2, 12.3.2.3 and 12.6.4.2, Table 202). ISO 14289-2, 8.8 asks
// for a structure destination wherever a destination lies inside the
// document; the explicit one keeps the target for a reader of PDF 1.7.
func (w *docWriter) target(d Dest) (ast.PdfName, ast.PdfObject) {
	explicit := w.dest(d)
	if w.version != Version20 || d.Elem == nil || w.st == nil {
		return "Dest", explicit
	}
	ref, ok := w.st.refs[d.Elem]
	if !ok {
		return "Dest", explicit
	}
	sd := append(ast.PdfArray{ref}, explicit[1:]...)
	return "A", ast.PdfDict{
		"Type": ast.PdfName("Action"),
		"S":    ast.PdfName("GoTo"),
		"D":    explicit,
		"SD":   sd,
	}
}
