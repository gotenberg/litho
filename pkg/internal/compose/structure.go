package compose

import (
	"context"
	"fmt"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// structItem is one piece of content an element owns: a marked-content
// sequence on a page, or an annotation (ISO 32000-1, 14.7.4).
type structItem struct {
	page int
	mcid int
	// annot is the annotation an object reference names, zero for a
	// marked-content sequence.
	annot ast.PdfIndirectRef
}

// structState gathers what the structure tree needs while the pages are
// written. It is used by the one goroutine writing the document.
//
// The tree itself arrives with the document's end, after the pages: what a
// page records names elements by pointer, and the elements get their object
// numbers when the tree is written.
type structState struct {
	// order lists the elements in preorder, the order they are written in,
	// once the tree has arrived.
	order  []*StructElem
	refs   map[*StructElem]ast.PdfIndirectRef
	parent map[*StructElem]*StructElem
	items  map[*StructElem][]structItem
	// pageParents lists, per page, the element of each MCID in MCID order:
	// the page's ParentTree entry.
	pageParents [][]*StructElem
	// annotParents lists the element of each annotation that has one, in
	// /StructParent key order after the pages' keys.
	annotParents []*StructElem
}

func newStructState() *structState {
	return &structState{items: make(map[*StructElem][]structItem)}
}

// addPage opens the ParentTree entry of the next page.
func (st *structState) addPage() {
	st.pageParents = append(st.pageParents, nil)
}

func (st *structState) addMCID(page, mcid int, e *StructElem) {
	st.items[e] = append(st.items[e], structItem{page: page, mcid: mcid})
	st.pageParents[page] = append(st.pageParents[page], e)
}

// addAnnot records that the annotation at ref on page belongs to e and
// returns its index among the annotations: its /StructParent key is the
// page count plus that index, keys 0 to len(pages)-1 being the pages'
// /StructParents.
func (st *structState) addAnnot(page int, ref ast.PdfIndirectRef, e *StructElem) int {
	st.items[e] = append(st.items[e], structItem{page: page, annot: ref})
	st.annotParents = append(st.annotParents, e)
	return len(st.annotParents) - 1
}

// number gives every element of the tree under root, in preorder, the
// reference [elemRef] stands for it with. root has passed the checks of
// [structElems], so the walk meets a tree within the caps.
func (st *structState) number(root *StructElem) {
	st.refs = make(map[*StructElem]ast.PdfIndirectRef)
	st.parent = make(map[*StructElem]*StructElem)
	stack := []*StructElem{root}
	for len(stack) > 0 {
		e := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		st.refs[e] = elemRef(len(st.order))
		st.order = append(st.order, e)
		for _, v := range slices.Backward(e.Kids) {
			st.parent[v] = e
			stack = append(stack, v)
		}
	}
}

// pdf2Namespace is the URI of the PDF 2.0 standard structure namespace
// (ISO 32000-2, 14.8.6.1).
const pdf2Namespace = "http://iso.org/pdf2/ssn"

// write writes every element, the parent tree and the StructTreeRoot (ISO
// 32000-1, 14.7.2, Tables 322 and 323) for a file of version v.
//
// An element's /K lists its own content first, in paint order page by page,
// then its child elements: the model orders the kids but does not say where
// an element's own content falls among them. /Pg names the page of the
// element's first content, and each item on that page is an MCID integer;
// an item on another page is a marked-content reference naming its page
// (14.7.4.3, Table 324), an annotation an object reference (Table 325).
//
// A PDF 2.0 file names the PDF 2.0 namespace on every element whose type
// it holds, writing a removed PDF 1.7 type as its replacement
// ([replaced20]); a role neither namespace holds stays in the default
// namespace and is mapped by /RoleMap. Every other version maps by
// /RoleMap each role it does not define.
func (st *structState) write(ctx context.Context, w *docWriter, v Version) error {
	var nsRef ast.PdfIndirectRef
	if v == Version20 {
		nsRef = w.alloc.Alloc(ast.PdfDict{
			"Type": ast.PdfName("Namespace"),
			"NS":   ast.PdfString(pdf2Namespace),
		})
	}
	written := map[string]bool{}
	for i, e := range st.order {
		if i%ctxCheckInterval == 0 {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("writing structure tree: %w", err)
			}
		}
		dict := ast.PdfDict{
			"Type": ast.PdfName("StructElem"),
			"S":    ast.PdfName(e.Role),
		}
		p, hasParent := st.parent[e]
		if hasParent {
			dict["P"] = st.refs[p]
		} else {
			dict["P"] = structRootRef
		}
		role := e.Role
		if v == Version20 {
			parentRole := ""
			if hasParent {
				parentRole = p.Role
			}
			if r := replaced20(e.Role, parentRole); r != "" {
				role = r
				dict["S"] = ast.PdfName(r)
				dict["NS"] = nsRef
			} else {
				written[e.Role] = true
			}
		} else {
			written[e.Role] = true
		}
		if a := attributes(e, role, v); a != nil {
			dict["A"] = a
		}
		items := st.items[e]
		kids := make(ast.PdfArray, 0, len(items)+len(e.Kids))
		if len(items) > 0 {
			pg := items[0].page
			dict["Pg"] = pageRef(pg)
			for _, it := range items {
				switch {
				case it.annot.ObjectNum != 0:
					kids = append(kids, ast.PdfDict{
						"Type": ast.PdfName("OBJR"),
						"Obj":  it.annot,
						"Pg":   pageRef(it.page),
					})
				case it.page == pg:
					kids = append(kids, ast.PdfInt(it.mcid))
				default:
					kids = append(kids, ast.PdfDict{
						"Type": ast.PdfName("MCR"),
						"Pg":   pageRef(it.page),
						"MCID": ast.PdfInt(it.mcid),
					})
				}
			}
		}
		for _, k := range e.Kids {
			kids = append(kids, st.refs[k])
		}
		if len(kids) > 0 {
			dict["K"] = kids
		}
		if e.Lang != "" {
			dict["Lang"] = textString(e.Lang)
		}
		if e.Alt != "" {
			dict["Alt"] = textString(e.Alt)
		}
		if e.ActualText != "" {
			dict["ActualText"] = textString(e.ActualText)
		}
		w.reserved[st.refs[e].ObjectNum] = dict
	}

	// The parent tree is a number tree (7.9.7) keyed by /StructParents and
	// /StructParent: a page's value is the array of the elements of its
	// MCIDs, an annotation's its element (14.7.4.4).
	nums := make(ast.PdfArray, 0, 2*(len(st.pageParents)+len(st.annotParents)))
	for i, parents := range st.pageParents {
		refs := make(ast.PdfArray, len(parents))
		for k, e := range parents {
			refs[k] = st.refs[e]
		}
		nums = append(nums, ast.PdfInt(i), refs)
	}
	for i, e := range st.annotParents {
		nums = append(nums, ast.PdfInt(len(st.pageParents)+i), st.refs[e])
	}
	parentTree := w.alloc.Alloc(ast.PdfDict{"Nums": nums})
	root := ast.PdfDict{
		"Type":              ast.PdfName("StructTreeRoot"),
		"K":                 st.refs[st.order[0]],
		"ParentTree":        parentTree,
		"ParentTreeNextKey": ast.PdfInt(len(st.pageParents) + len(st.annotParents)),
	}
	if rm := roleMap(written, v); rm != nil {
		root["RoleMap"] = rm
	}
	if v == Version20 {
		// ISO 32000-2, 14.7.2, Table 355: /Namespaces lists the namespaces
		// the elements name.
		root["Namespaces"] = ast.PdfArray{nsRef}
	}
	w.reserved[structRootRef.ObjectNum] = root
	return nil
}

// attributes returns the attribute object of e, written as type role in a
// file of version v, nil for none: the Table attributes of a TH or TD cell
// (ISO 32000-1, 14.8.5.7, Table 349), its spans past one and a header
// cell's Scope, and the List attribute of an L (14.8.5.5, Table 347). Scope
// is a PDF 1.5 attribute and is left out of an older file.
func attributes(e *StructElem, role string, v Version) ast.PdfObject {
	switch role {
	case "TH", "TD":
		a := ast.PdfDict{}
		if e.RowSpan > 1 {
			a["RowSpan"] = ast.PdfInt(e.RowSpan)
		}
		if e.ColSpan > 1 {
			a["ColSpan"] = ast.PdfInt(e.ColSpan)
		}
		if role == "TH" && v != Version14 {
			switch e.Scope {
			case ScopeRow:
				a["Scope"] = ast.PdfName("Row")
			case ScopeColumn:
				a["Scope"] = ast.PdfName("Column")
			case ScopeBoth:
				a["Scope"] = ast.PdfName("Both")
			case ScopeNone:
			}
		}
		if len(a) == 0 {
			return nil
		}
		a["O"] = ast.PdfName("Table")
		return a
	case "L":
		if n := listNumbering(e.Numbering, v); n != "" {
			return ast.PdfDict{"O": ast.PdfName("List"), "ListNumbering": ast.PdfName(n)}
		}
	}
	return nil
}

// listNumbering returns the ListNumbering value of n in a file of version
// v, "" for [NumberingUnset]. PDF 2.0 adds Ordered, Unordered and
// Description (ISO 32000-2, 14.8.5.5, Table 382); an older file writes the
// nearest value ISO 32000-1, Table 347 defines: Decimal, Disc and None.
func listNumbering(n ListNumbering, v Version) string {
	names := [...]string{
		NumberingNone: "None", NumberingDisc: "Disc", NumberingCircle: "Circle",
		NumberingSquare: "Square", NumberingDecimal: "Decimal",
		NumberingUpperRoman: "UpperRoman", NumberingLowerRoman: "LowerRoman",
		NumberingUpperAlpha: "UpperAlpha", NumberingLowerAlpha: "LowerAlpha",
		NumberingOrdered: "Ordered", NumberingUnordered: "Unordered",
		NumberingDescription: "Description",
	}
	if n == NumberingUnset || int(n) >= len(names) {
		return ""
	}
	if v != Version20 {
		switch n { //nolint:exhaustive // the ISO 32000-1 values write as they are
		case NumberingOrdered:
			return "Decimal"
		case NumberingUnordered:
			return "Disc"
		case NumberingDescription:
			return "None"
		}
	}
	return names[n]
}
