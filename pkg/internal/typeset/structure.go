package typeset

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// structItem is one piece of content an element owns: a marked-content
// sequence on a page or in a form XObject the page paints, or an
// annotation (ISO 32000-1, 14.7.4).
type structItem struct {
	page int
	mcid int
	// form is one past the index of the form whose stream holds the
	// sequence among the state's forms, 0 for the page's stream.
	form int
	// seq is how many sequences the pages recorded before this one.
	seq int
	// order is the sequence's place in its element's logical order on
	// its page (see [Text.Order]).
	order int
	// annot is the annotation an object reference names, zero for a
	// marked-content sequence.
	annot ast.PdfIndirectRef
}

// formMarks is the marked content of one transparency group's form
// XObject (see [pageWriter.groupForm]): its stream numbers its sequences from
// MCID 0, and the parent tree holds them under the form's /StructParents
// (ISO 32000-1, 14.7.4.4).
type formMarks struct {
	page int
	// parent is the index of the form whose stream paints this one, -1
	// for the page's stream.
	parent int
	// at is how many sequences the parent's stream held before the Do
	// that paints the form, which places the form's sequences among the
	// parent's in paint order.
	at int
	// ref and dict are the form XObject's, which takes its
	// /StructParents once the tree is written.
	ref  ast.PdfIndirectRef
	dict ast.PdfDict
	// parents lists the element of each MCID, in MCID order, and orders
	// the [Text.Order] of each.
	parents []*StructElem
	orders  []int
}

// annotItem is an annotation an element owns, and how many marked-content
// sequences the pages recorded before it.
type annotItem struct {
	structItem
	seq int
}

// structState gathers what the structure tree needs while the pages are
// written. It is used by the one goroutine writing the document.
//
// The tree itself arrives with the document's end, after the pages: what a
// page records names elements by pointer, and the elements get their object
// numbers when the tree is written.
//
// The marked-content sequences an element owns are not listed by element
// while the pages are written: pageParents lists the element of each, in
// the order the pages recorded them, and [structState.write] gathers each
// element's from it. A page records its sequences one after the other,
// MCID 0 up, and the pages one after the other, so that order is the
// order of (page, MCID); an annotation records how many sequences came
// before it, which places it among them.
type structState struct {
	// order lists the elements in preorder, the order they are written in,
	// once the tree has arrived.
	order []*StructElem
	// size is the count of elements, kept once order is let go.
	size int
	refs map[*StructElem]ast.PdfIndirectRef
	// parents holds, at each element's index in order, its parent's index
	// there, -1 for the root.
	parents []int
	// annots lists, per element, the annotations it owns in the order they
	// were recorded.
	annots map[*StructElem][]annotItem
	// marks counts the marked-content sequences recorded.
	marks int
	// pageParents lists, per page, the element of each MCID in MCID order:
	// the page's ParentTree entry.
	pageParents [][]*StructElem
	// pageOrders lists, per page, the [Text.Order] of each MCID.
	pageOrders [][]int
	// annotParents lists the element of each annotation that has one, in
	// /StructParent key order after the pages' keys.
	annotParents []*StructElem
	// forms lists the transparency group forms in the order they were
	// opened; those with marked content take /StructParents keys after
	// the annotations'.
	forms []formMarks
	// refers lists, per element, the elements its links lead to, in the
	// order the links are written: a PDF 2.0 file names them in its /Ref.
	// A destination's element is read through its pointer, so it may
	// arrive with the tree, after the page.
	refers map[*StructElem][]*StructElem
}

func newStructState() *structState {
	return &structState{annots: make(map[*StructElem][]annotItem), refers: make(map[*StructElem][]*StructElem)}
}

// addPage opens the ParentTree entry of the next page.
func (st *structState) addPage() {
	st.pageParents = append(st.pageParents, nil)
	st.pageOrders = append(st.pageOrders, nil)
}

// addMCID records that the next marked-content sequence of page
// belongs to e, at order among its content (see [Text.Order]).
func (st *structState) addMCID(page int, e *StructElem, order int) {
	st.pageParents[page] = append(st.pageParents[page], e)
	st.pageOrders[page] = append(st.pageOrders[page], order)
	st.marks++
}

// openForm records that the stream of the form parent, -1 for page's own,
// paints a transparency group's form after its at sequences, and returns
// the form's index for [structState.addFormMCID].
func (st *structState) openForm(page, parent, at int) int {
	st.forms = append(st.forms, formMarks{page: page, parent: parent, at: at})
	return len(st.forms) - 1
}

// addFormMCID records that the next marked-content sequence of the form at
// index form belongs to e, at order among its content.
func (st *structState) addFormMCID(form int, e *StructElem, order int) {
	st.forms[form].parents = append(st.forms[form].parents, e)
	st.forms[form].orders = append(st.forms[form].orders, order)
	st.marks++
}

// closeForm records the object of the form at index form: dict is its
// stream's dictionary, which takes the form's /StructParents when the
// tree is written.
func (st *structState) closeForm(form int, ref ast.PdfIndirectRef, dict ast.PdfDict) {
	st.forms[form].ref, st.forms[form].dict = ref, dict
}

// addAnnot records that the annotation at ref on page belongs to e, and
// leads to the element to when to is not nil, and returns its index among
// the annotations: its /StructParent key is the
// page count plus that index, keys 0 to len(pages)-1 being the pages'
// /StructParents.
func (st *structState) addAnnot(page int, ref ast.PdfIndirectRef, e, to *StructElem) int {
	st.annots[e] = append(st.annots[e], annotItem{structItem: structItem{page: page, annot: ref}, seq: st.marks})
	if to != nil && !slices.Contains(st.refers[e], to) {
		st.refers[e] = append(st.refers[e], to)
	}
	st.annotParents = append(st.annotParents, e)
	return len(st.annotParents) - 1
}

// number gives every element of the tree under root, in preorder, the
// reference [elemRef] stands for it with. root has passed the checks of
// [structElems], so the walk meets a tree within the caps.
func (st *structState) number(root *StructElem) {
	st.refs = make(map[*StructElem]ast.PdfIndirectRef)
	type entry struct {
		e      *StructElem
		parent int
	}
	stack := []entry{{root, -1}}
	for len(stack) > 0 {
		it := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		i := len(st.order)
		st.refs[it.e] = elemRef(i)
		st.order = append(st.order, it.e)
		st.parents = append(st.parents, it.parent)
		st.size++
		for _, v := range slices.Backward(it.e.Kids) {
			stack = append(stack, entry{v, i})
		}
	}
}

// index returns the index of e in order, false for an element the tree
// does not hold.
func (st *structState) index(e *StructElem) (int, bool) {
	ref, ok := st.refs[e]
	if !ok {
		return 0, false
	}
	return (-ref.ObjectNum - 4) / 2, true
}

// marked returns the marked-content sequences of every element: those of
// the element at index i in order are marks[at[i]:at[i+1]], page by page,
// and on each page in logical order (see [Text.Order]), in the order the
// page recorded them among equals: the order of the structure tree is the
// logical order of the content (ISO 32000-1, 14.8.2.3.1). A sequence of
// an element the tree does not hold is left out.
func (st *structState) marked() (at []int, marks []structItem) {
	at = make([]int, len(st.order)+1)
	st.walk(func(e *StructElem, _ structItem) {
		if i, ok := st.index(e); ok {
			at[i+1]++
		}
	})
	for i := range len(st.order) {
		at[i+1] += at[i]
	}
	marks = make([]structItem, at[len(st.order)])
	next := slices.Clone(at[:len(st.order)])
	st.walk(func(e *StructElem, it structItem) {
		if i, ok := st.index(e); ok {
			marks[next[i]] = it
			next[i]++
		}
	})
	for i := range len(st.order) {
		slices.SortStableFunc(marks[at[i]:at[i+1]], func(a, b structItem) int {
			if a.page != b.page {
				return a.page - b.page
			}
			return a.order - b.order
		})
	}
	return at, marks
}

// walk calls fn with every marked-content sequence recorded and its
// element, in paint order: a page's sequences in MCID order, with the
// sequences of each form its stream paints where the Do painted it, and
// the forms' forms alike. Paint order is the order the sequences were
// recorded in, which numbers them (structItem.seq).
func (st *structState) walk(fn func(*StructElem, structItem)) {
	pageForms := make([][]int, len(st.pageParents))
	formForms := make([][]int, len(st.forms))
	for i, f := range st.forms {
		if f.parent < 0 {
			pageForms[f.page] = append(pageForms[f.page], i)
		} else {
			formForms[f.parent] = append(formForms[f.parent], i)
		}
	}
	seq := 0
	var stream func(page, form int, parents []*StructElem, orders []int, forms []int)
	stream = func(page, form int, parents []*StructElem, orders []int, forms []int) {
		k := 0
		for mcid := 0; mcid <= len(parents); mcid++ {
			// The form nesting is a group nesting, which Validate bounds.
			for ; k < len(forms) && st.forms[forms[k]].at <= mcid; k++ {
				f := forms[k]
				stream(page, f+1, st.forms[f].parents, st.forms[f].orders, formForms[f])
			}
			if mcid < len(parents) {
				fn(parents[mcid], structItem{page: page, mcid: mcid, form: form, seq: seq, order: orders[mcid]})
				seq++
			}
		}
	}
	for page, parents := range st.pageParents {
		stream(page, 0, parents, st.pageOrders[page], pageForms[page])
	}
}

// items returns the content of e in the order it was
// recorded: its marked-content sequences, marks, with its annotations
// placed among them.
func (st *structState) items(e *StructElem, marks []structItem) []structItem {
	annots := st.annots[e]
	if len(annots) == 0 {
		return marks
	}
	out := make([]structItem, 0, len(marks)+len(annots))
	k := 0
	for _, a := range annots {
		// A sequence recorded before the annotation comes first: it is
		// one of the a.seq sequences of the pages before it.
		for k < len(marks) && marks[k].seq < a.seq {
			out = append(out, marks[k])
			k++
		}
		out = append(out, a.structItem)
	}
	return append(out, marks[k:]...)
}

// pdf2Namespace is the URI of the PDF 2.0 standard structure namespace
// (ISO 32000-2, 14.8.6.1).
const pdf2Namespace = "http://iso.org/pdf2/ssn"

// write writes every element, the parent tree and the StructTreeRoot (ISO
// 32000-1, 14.7.2, Tables 322 and 323) for a file of version v.
//
// An element's /K lists its own content first, page by page and on each
// page in logical order (see [structState.marked]), then its child elements: the model orders the kids but does not say where
// an element's own content falls among them. /Pg names the page of the
// element's first content, and each item on that page is an MCID integer;
// an item on another page is a marked-content reference naming its page
// (14.7.4.3, Table 324), an annotation an object reference (Table 325).
//
// A PDF 2.0 file names the PDF 2.0 namespace on every element whose type
// it holds, writing a removed PDF 1.7 type as its replacement
// ([replaced20]); a table of contents whose entries name their targets
// ([structState.tocKept]) and a role neither namespace holds stay in the
// default namespace, the second mapped by /RoleMap. Every other version maps by
// /RoleMap each role it does not define.
func (st *structState) write(ctx context.Context, w *docWriter, v Version) error {
	var nsRef ast.PdfIndirectRef
	if v == Version20 {
		nsRef = w.alloc.Alloc(ast.PdfDict{
			"Type": ast.PdfName("Namespace"),
			"NS":   ast.PdfString(pdf2Namespace),
		})
	}
	var kept map[*StructElem]bool
	if v == Version20 {
		kept = st.tocKept()
	}
	written := map[string]bool{}
	at, marks := st.marked()
	// The elements and the parent tree are written with their numbers in
	// the file, each reference made once: a tree of a cell a page element
	// is too large to copy as [docWriter.final] renumbers the rest.
	refs := newFinalRefs(w, st.size)
	elem := func(e *StructElem) ast.PdfObject {
		i, ok := st.index(e)
		if !ok {
			// No element outside the tree reaches the file; the reference
			// a placeholder of zero would get stands for it all the same.
			return refs.of(0)
		}
		return refs.elem(i)
	}
	// ids are the identifiers given the notes, each with its element, for
	// the IDTree.
	var ids ast.PdfArray
	for i, e := range st.order {
		if i%ctxCheckInterval == 0 {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("writing structure tree: %w", err)
			}
		}
		dict := ast.PdfDict{
			"Type": ast.PdfName("StructElem"),
			"S":    refs.name(e.Role),
		}
		if e.Role == "Note" {
			// ISO 14289-1, 7.9: a Note carries an ID, unique in the
			// structure hierarchy (ISO 32000-1, 14.7.2, Table 323), which
			// the element's place in the tree's order makes it.
			id := ast.PdfString("note-" + strconv.Itoa(i))
			dict["ID"] = id
			ids = append(ids, id, refs.elem(i))
		}
		var p *StructElem
		hasParent := st.parents[i] >= 0
		if hasParent {
			p = st.order[st.parents[i]]
			dict["P"] = refs.elem(st.parents[i])
		} else {
			dict["P"] = refs.of(structRootRef.ObjectNum)
		}
		role := e.Role
		if v == Version20 {
			parentRole := ""
			if hasParent {
				parentRole = p.Role
			}
			if kept[e] {
				// The PDF 1.7 namespace, the default one: no /NS.
				written[e.Role] = true
			} else if r := replaced20(e.Role, parentRole); r != "" {
				role = r
				dict["S"] = refs.name(r)
				dict["NS"] = refs.of(nsRef.ObjectNum)
			} else {
				written[e.Role] = true
			}
		} else {
			written[e.Role] = true
		}
		items := st.items(e, marks[at[i]:at[i+1]])
		widget := slices.ContainsFunc(items, func(it structItem) bool { return it.annot.ObjectNum != 0 })
		if a := attributes(e, role, v, widget); a != nil {
			dict["A"] = a
		}
		kids := make(ast.PdfArray, 0, len(items)+len(e.Kids))
		if len(items) > 0 {
			pg := items[0].page
			dict["Pg"] = refs.page(pg)
			for _, it := range items {
				switch {
				case it.annot.ObjectNum != 0:
					kids = append(kids, ast.PdfDict{
						"Type": ast.PdfName("OBJR"),
						"Obj":  refs.of(it.annot.ObjectNum),
						"Pg":   refs.page(it.page),
					})
				case it.form != 0:
					// A sequence in a form's stream names the stream
					// (14.7.4.3, Table 324, Stm).
					kids = append(kids, ast.PdfDict{
						"Type": ast.PdfName("MCR"),
						"Pg":   refs.page(it.page),
						"Stm":  refs.of(st.forms[it.form-1].ref.ObjectNum),
						"MCID": ast.PdfInt(it.mcid),
					})
				case it.page == pg:
					kids = append(kids, ast.PdfInt(it.mcid))
				default:
					kids = append(kids, ast.PdfDict{
						"Type": ast.PdfName("MCR"),
						"Pg":   refs.page(it.page),
						"MCID": ast.PdfInt(it.mcid),
					})
				}
			}
		}
		for _, k := range e.Kids {
			kids = append(kids, elem(k))
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
		if v == Version20 {
			// ISO 32000-2, 14.7.2, Table 355: /Ref names the elements the
			// content of an element refers to, a PDF 2.0 entry. A link
			// refers to the element it leads to, which is how an entry of a
			// table of contents identifies its heading (ISO 14289-2,
			// 8.2.5.8).
			var to ast.PdfArray
			for _, t := range st.refers[e] {
				if i, ok := st.index(t); ok {
					to = append(to, refs.elem(i))
				}
			}
			if len(to) > 0 {
				dict["Ref"] = to
			}
		}
		w.numbered[w.finalNumber(elemRef(i).ObjectNum)] = dict
	}

	// The parent tree is a number tree (7.9.7) keyed by /StructParents and
	// /StructParent: a page's value is the array of the elements of its
	// MCIDs, an annotation's its element (14.7.4.4).
	nums := make(ast.PdfArray, 0, 2*(len(st.pageParents)+len(st.annotParents)))
	for i, parents := range st.pageParents {
		page := make(ast.PdfArray, len(parents))
		for k, e := range parents {
			page[k] = elem(e)
		}
		nums = append(nums, ast.PdfInt(i), page)
	}
	for i, e := range st.annotParents {
		nums = append(nums, ast.PdfInt(len(st.pageParents)+i), elem(e))
	}
	// A form with marked content takes the next key as its /StructParents
	// (14.7.4.4, ISO 32000-1, 8.10.2, Table 95), its value the array of
	// the elements of its MCIDs, as a page's.
	next := len(st.pageParents) + len(st.annotParents)
	for _, f := range st.forms {
		if len(f.parents) == 0 {
			continue
		}
		arr := make(ast.PdfArray, len(f.parents))
		for k, e := range f.parents {
			arr[k] = elem(e)
		}
		f.dict["StructParents"] = ast.PdfInt(next)
		nums = append(nums, ast.PdfInt(next), arr)
		next++
	}
	// The parent tree takes the next object number, as the object it is
	// queued as would.
	parentTree := ast.PdfIndirectRef{ObjectNum: w.alloc.NextObjNum()}
	w.alloc.Reserve(parentTree.ObjectNum)
	w.numbered[w.finalNumber(parentTree.ObjectNum)] = ast.PdfDict{"Nums": nums}
	root := ast.PdfDict{
		"Type":              ast.PdfName("StructTreeRoot"),
		"K":                 st.refs[st.order[0]],
		"ParentTree":        parentTree,
		"ParentTreeNextKey": ast.PdfInt(next),
	}
	if rm := roleMap(written, v); rm != nil {
		root["RoleMap"] = rm
	}
	if len(ids) > 0 {
		// ISO 32000-1, 14.7.2, Table 322: elements with identifiers ask for
		// the IDTree, a name tree (7.9.6) whose keys sort as byte strings.
		root["IDTree"] = ast.PdfDict{"Names": sortedNames(ids)}
	}
	if v == Version20 {
		// ISO 32000-2, 14.7.2, Table 355: /Namespaces lists the namespaces
		// the elements name.
		root["Namespaces"] = ast.PdfArray{nsRef}
	}
	w.reserved[structRootRef.ObjectNum] = root
	st.release()
	return nil
}

// finalRefs makes the references of a tree written with the numbers of
// the file ([docWriter.finalNumber]), each once: an element's for its
// parent's /K, its kids' /P and the parent tree, a page's for every
// element on it, a role's name for every element of the role.
type finalRefs struct {
	w     *docWriter
	elems []ast.PdfObject
	pages []ast.PdfObject
	names map[string]ast.PdfObject
}

func newFinalRefs(w *docWriter, elems int) *finalRefs {
	return &finalRefs{w: w, elems: make([]ast.PdfObject, elems), pages: make([]ast.PdfObject, len(w.heights)), names: map[string]ast.PdfObject{}}
}

// of returns the reference of the object at the placeholder or
// provisional number num.
func (r *finalRefs) of(num int) ast.PdfObject {
	return ast.PdfIndirectRef{ObjectNum: r.w.finalNumber(num)}
}

// elem returns the reference of the element at index i in preorder.
func (r *finalRefs) elem(i int) ast.PdfObject {
	if r.elems[i] == nil {
		r.elems[i] = r.of(elemRef(i).ObjectNum)
	}
	return r.elems[i]
}

// page returns the reference of the page at index i.
func (r *finalRefs) page(i int) ast.PdfObject {
	if i < 0 || i >= len(r.pages) {
		return r.of(pageRef(i).ObjectNum)
	}
	if r.pages[i] == nil {
		r.pages[i] = r.of(pageRef(i).ObjectNum)
	}
	return r.pages[i]
}

// name returns the name n.
func (r *finalRefs) name(n string) ast.PdfObject {
	o, ok := r.names[n]
	if !ok {
		o = ast.PdfName(n)
		r.names[n] = o
	}
	return o
}

// release lets go of what the pages recorded and of the numbering once
// the tree is written: the dictionaries written hold all of it, and only
// the count of elements, size, is read after.
func (st *structState) release() {
	st.order, st.refs, st.parents, st.annots, st.refers = nil, nil, nil, nil, nil
	st.pageParents, st.pageOrders, st.annotParents, st.forms = nil, nil, nil, nil
}

// tocKept returns the TOC and TOCI elements a PDF 2.0 file writes as
// themselves, in the default standard structure namespace, the PDF 1.7 one
// that ISO 32000-2, 14.8.6.1 keeps for the types of earlier versions,
// rather than as Divs ([removed20]). A TOC is kept, with its TOCI, when
// each of its TOCI names its target through the /Ref of itself or of an
// element under it, a link's ([structState.refers]): ISO 14289-2, 8.2.5.8
// asks it of every entry, and a TOC whose entries say nothing of where
// they lead is no better than the Div that ISO 32000-2, Annex L maps it
// to. Annex L and ISO 32005, Table 5 then relate the two namespaces'
// types: a TOC under a Sect or the Document, its Caption, TOCI and nested
// TOC under it.
func (st *structState) tocKept() map[*StructElem]bool {
	refers := map[*StructElem]bool{}
	// st.order is a preorder: walking it backwards meets every kid before
	// its parent.
	for i, e := range slices.Backward(st.order) {
		for _, t := range st.refers[e] {
			if _, ok := st.refs[t]; ok {
				refers[e] = true
			}
		}
		if refers[e] && st.parents[i] >= 0 {
			refers[st.order[st.parents[i]]] = true
		}
	}
	kept := map[*StructElem]bool{}
	for i, e := range st.order {
		if e.Role != "TOC" {
			continue
		}
		if pi := st.parents[i]; pi >= 0 && st.order[pi].Role == "TOCI" && !kept[st.order[pi]] {
			// A TOC nested in an entry of a table written as a Div is one
			// too.
			continue
		}
		entries := 0
		named := true
		for _, k := range e.Kids {
			if k.Role == "TOCI" {
				entries++
				named = named && refers[k]
			}
		}
		if entries == 0 || !named {
			continue
		}
		kept[e] = true
		for _, k := range e.Kids {
			if k.Role == "TOCI" {
				kept[k] = true
			}
		}
	}
	return kept
}

// attributes returns the attribute object of e, written as type role in a
// file of version v, nil for none: the Table attributes of a TH or TD cell
// (ISO 32000-1, 14.8.5.7, Table 349), its spans past one and a header
// cell's Scope, the List attribute of an L (14.8.5.5, Table 347), and the
// PrintField attributes of a Form ([printField]) that holds no widget
// annotation. Scope is a PDF 1.5 attribute and is left out of an older
// file.
func attributes(e *StructElem, role string, v Version, widget bool) ast.PdfObject {
	switch role {
	case "Form":
		if !widget {
			return printField(e.PrintField, v)
		}
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

// printField returns the PrintField attribute object of a Form element
// that shows the non-interactive field pf in a file of version v, nil when
// pf says nothing or v predates the attributes, which ISO 32000-1, Table
// 348 marks PDF 1.7.
//
// ISO 32000-1, 14.8.4.5, Table 340 lets a Form element with a Role
// attribute hold the content that represents a non-interactive field, and
// asks one without of holding the widget annotation alone, which ISO
// 14289-1, 7.18.4 makes a requirement of PDF/UA-1. The state is written
// as checked in a PDF 1.7 file (ISO 32000-1, Table 348) and as Checked in
// a PDF 2.0 one, whose Table 383 deprecates the lower-case key; a list box
// takes the lb role there, and tv, the value shown as text, before it.
func printField(pf PrintField, v Version) ast.PdfObject {
	if pf.Role == PrintRoleNone || v != Version17 && v != Version20 {
		return nil
	}
	roles := [...]string{PrintRadio: "rb", PrintCheckBox: "cb", PrintPushButton: "pb", PrintText: "tv", PrintList: "tv"}
	if int(pf.Role) >= len(roles) {
		return nil
	}
	role := roles[pf.Role]
	if pf.Role == PrintList && v == Version20 {
		role = "lb"
	}
	a := ast.PdfDict{"O": ast.PdfName("PrintField"), "Role": ast.PdfName(role)}
	if pf.Role == PrintRadio || pf.Role == PrintCheckBox {
		key := ast.PdfName("checked")
		if v == Version20 {
			key = "Checked"
		}
		switch pf.Checked {
		case CheckOn:
			a[key] = ast.PdfName("on")
		case CheckOff:
			a[key] = ast.PdfName("off")
		case CheckNeutral:
			a[key] = ast.PdfName("neutral")
		case CheckUnset:
		}
	}
	if pf.Desc != "" {
		a["Desc"] = textString(pf.Desc)
	}
	return a
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

// sortedNames returns the key and value pairs of a name tree's Names array
// pairs in the order of their keys, byte by byte (ISO 32000-1, 7.9.6).
func sortedNames(pairs ast.PdfArray) ast.PdfArray {
	type entry struct {
		key   string
		value ast.PdfObject
	}
	entries := make([]entry, 0, len(pairs)/2)
	for k := 0; k+1 < len(pairs); k += 2 {
		key, _ := pairs[k].(ast.PdfString)
		entries = append(entries, entry{string(key), pairs[k+1]})
	}
	slices.SortFunc(entries, func(a, b entry) int { return strings.Compare(a.key, b.key) })
	out := make(ast.PdfArray, 0, len(pairs))
	for _, e := range entries {
		out = append(out, ast.PdfString(e.key), e.value)
	}
	return out
}
