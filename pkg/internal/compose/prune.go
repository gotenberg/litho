package compose

// PruneStructure drops from doc's structure tree the elements no fragment,
// link or form field of its pages hangs from, directly or through their
// kids, once a page selection left pages out or culling left content out,
// so the tree
// written (ISO 32000-1, 14.7.2) names no element without content but the
// empty cells of a row that keeps content. The root stays, and a
// destination whose element is dropped keeps its page position alone. A
// nil tree is left alone.
func PruneStructure(doc *Document) {
	if doc.Structure == nil {
		return
	}
	used := map[*StructElem]bool{}
	var frags []Fragment
	for _, pg := range doc.Pages {
		frags = append(frags, pg.Content...)
		for _, l := range pg.Links {
			if l.Parent != nil {
				used[l.Parent] = true
			}
		}
		for _, f := range pg.Fields {
			if f.Parent != nil {
				used[f.Parent] = true
			}
		}
	}
	for len(frags) > 0 {
		f := frags[len(frags)-1]
		frags = frags[:len(frags)-1]
		switch v := f.(type) {
		case *Group:
			frags = append(frags, v.Content...)
		case *Text:
			if v.Parent != nil {
				used[v.Parent] = true
			}
		case *Image:
			if v.Parent != nil {
				used[v.Parent] = true
			}
		case *Path:
			if v.Parent != nil {
				used[v.Parent] = true
			}
		}
	}
	PruneTree(doc.Structure, used)
	dropDanglingDests(doc)
}

// gridRoles are the table parts of a table's grid (ISO 32000-1, 14.8.4.3.4,
// Table 337). Immutable lookup table.
var gridRoles = map[string]bool{"THead": true, "TBody": true, "TFoot": true, "TR": true, "TH": true, "TD": true}

// PruneTree drops from the tree under root the elements used does not
// hold, directly or through their kids; root stays. A table that keeps
// content keeps its whole grid, its row groups, rows and cells empty or
// not: with their spans, its rows count the same columns and its columns
// the same rows (ISO 14289-1, 7.2). The tree has passed
// [Document.Validate] or was built within its caps.
func PruneTree(root *StructElem, used map[*StructElem]bool) {
	// Post-order over an explicit stack: an element reaches content when
	// it is used or a kid does.
	reach := map[*StructElem]bool{}
	type frame struct {
		el   *StructElem
		next int
	}
	stack := []*frame{{el: root}}
	for len(stack) > 0 {
		top := stack[len(stack)-1]
		if top.next < len(top.el.Kids) {
			top.next++
			stack = append(stack, &frame{el: top.el.Kids[top.next-1]})
			continue
		}
		stack = stack[:len(stack)-1]
		if used[top.el] || reach[top.el] {
			reach[top.el] = true
			if len(stack) > 0 {
				reach[stack[len(stack)-1].el] = true
			}
		}
	}
	// Preorder: grid reports an element of the grid of a table that
	// reaches content.
	type item struct {
		el   *StructElem
		grid bool
	}
	todo := []item{{el: root}}
	for len(todo) > 0 {
		it := todo[len(todo)-1]
		todo = todo[:len(todo)-1]
		grid := it.grid || it.el.Role == "Table" && reach[it.el]
		kids := make([]*StructElem, 0, len(it.el.Kids))
		for _, k := range it.el.Kids {
			inGrid := grid && gridRoles[k.Role]
			if reach[k] || inGrid {
				kids = append(kids, k)
				todo = append(todo, item{el: k, grid: inGrid})
			}
		}
		it.el.Kids = kids
	}
}

// dropDanglingDests clears the [Dest.Elem] of every link and outline entry
// of doc that names an element doc.Structure no longer reaches. A link's
// destination is copied before it changes: the model may share it.
func dropDanglingDests(doc *Document) {
	kept := map[*StructElem]bool{}
	stack := []*StructElem{doc.Structure}
	for len(stack) > 0 {
		e := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		kept[e] = true
		stack = append(stack, e.Kids...)
	}
	for _, pg := range doc.Pages {
		for i, l := range pg.Links {
			if l.Dest != nil && l.Dest.Elem != nil && !kept[l.Dest.Elem] {
				d := *l.Dest
				d.Elem = nil
				pg.Links[i].Dest = &d
			}
		}
	}
	var outline func([]Outline)
	outline = func(items []Outline) {
		for i := range items {
			if e := items[i].Dest.Elem; e != nil && !kept[e] {
				items[i].Dest.Elem = nil
			}
			outline(items[i].Kids)
		}
	}
	outline(doc.Outline)
}
