package compose

import (
	"context"
	"fmt"

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
// written. It is used by the one goroutine running [Write].
type structState struct {
	rootRef ast.PdfIndirectRef
	// order lists the elements in preorder, the order they are written in.
	order  []*StructElem
	refs   map[*StructElem]ast.PdfIndirectRef
	parent map[*StructElem]*StructElem
	items  map[*StructElem][]structItem
	// pageParents lists, per page, the element of each MCID in MCID order:
	// the page's ParentTree entry.
	pageParents [][]ast.PdfObject
	// annotParents lists the element of each annotation that has one, in
	// /StructParent key order after the pages' keys.
	annotParents []ast.PdfIndirectRef
}

// newStructState reserves an object for the StructTreeRoot and for every
// element of the tree under root, in preorder. root has passed
// [Document.Validate], so the walk meets a tree within the caps.
func newStructState(w *docWriter, root *StructElem) *structState {
	st := &structState{
		rootRef:     w.alloc.Alloc(ast.PdfNull{}),
		refs:        make(map[*StructElem]ast.PdfIndirectRef),
		parent:      make(map[*StructElem]*StructElem),
		items:       make(map[*StructElem][]structItem),
		pageParents: make([][]ast.PdfObject, len(w.doc.Pages)),
	}
	stack := []*StructElem{root}
	for len(stack) > 0 {
		e := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		st.order = append(st.order, e)
		st.refs[e] = w.alloc.Alloc(ast.PdfNull{})
		for i := len(e.Kids) - 1; i >= 0; i-- {
			st.parent[e.Kids[i]] = e
			stack = append(stack, e.Kids[i])
		}
	}
	return st
}

func (st *structState) addMCID(page, mcid int, e *StructElem) {
	st.items[e] = append(st.items[e], structItem{page: page, mcid: mcid})
	st.pageParents[page] = append(st.pageParents[page], st.refs[e])
}

// addAnnot records that the annotation at ref on page belongs to e and
// returns its /StructParent key: keys 0 to len(pages)-1 are the pages'
// /StructParents, the annotations' follow.
func (st *structState) addAnnot(page int, ref ast.PdfIndirectRef, e *StructElem) int {
	st.items[e] = append(st.items[e], structItem{page: page, annot: ref})
	st.annotParents = append(st.annotParents, st.refs[e])
	return len(st.pageParents) + len(st.annotParents) - 1
}

// write writes every element, the parent tree and the StructTreeRoot (ISO
// 32000-1, 14.7.2, Tables 322 and 323).
//
// An element's /K lists its own content first, in paint order page by page,
// then its child elements: the model orders the kids but does not say where
// an element's own content falls among them. /Pg names the page of the
// element's first content, and each item on that page is an MCID integer;
// an item on another page is a marked-content reference naming its page
// (14.7.4.3, Table 324), an annotation an object reference (Table 325).
func (st *structState) write(ctx context.Context, w *docWriter) error {
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
		if p, ok := st.parent[e]; ok {
			dict["P"] = st.refs[p]
		} else {
			dict["P"] = st.rootRef
		}
		items := st.items[e]
		kids := make(ast.PdfArray, 0, len(items)+len(e.Kids))
		if len(items) > 0 {
			pg := items[0].page
			dict["Pg"] = w.pageRefs[pg]
			for _, it := range items {
				switch {
				case it.annot.ObjectNum != 0:
					kids = append(kids, ast.PdfDict{
						"Type": ast.PdfName("OBJR"),
						"Obj":  it.annot,
						"Pg":   w.pageRefs[it.page],
					})
				case it.page == pg:
					kids = append(kids, ast.PdfInt(it.mcid))
				default:
					kids = append(kids, ast.PdfDict{
						"Type": ast.PdfName("MCR"),
						"Pg":   w.pageRefs[it.page],
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
		w.alloc.AllocAt(st.refs[e].ObjectNum, dict)
	}

	// The parent tree is a number tree (7.9.7) keyed by /StructParents and
	// /StructParent: a page's value is the array of the elements of its
	// MCIDs, an annotation's its element (14.7.4.4).
	nums := make(ast.PdfArray, 0, 2*(len(st.pageParents)+len(st.annotParents)))
	for i, parents := range st.pageParents {
		if parents == nil {
			parents = ast.PdfArray{}
		}
		nums = append(nums, ast.PdfInt(i), ast.PdfArray(parents))
	}
	for i, ref := range st.annotParents {
		nums = append(nums, ast.PdfInt(len(st.pageParents)+i), ref)
	}
	parentTree := w.alloc.Alloc(ast.PdfDict{"Nums": nums})
	w.alloc.AllocAt(st.rootRef.ObjectNum, ast.PdfDict{
		"Type":              ast.PdfName("StructTreeRoot"),
		"K":                 st.refs[st.order[0]],
		"ParentTree":        parentTree,
		"ParentTreeNextKey": ast.PdfInt(len(st.pageParents) + len(st.annotParents)),
	})
	return nil
}
