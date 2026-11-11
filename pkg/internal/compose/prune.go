package compose

// PruneStructure drops from doc's structure tree the elements no fragment
// or link of its pages hangs from, directly or through their kids, once a
// page selection left pages out or culling left content out, so the tree
// written (ISO 32000-1, 14.7.2) names no element without content. The
// root stays, and a destination whose element is dropped keeps its page
// position alone. A nil tree is left alone.
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
	// Post-order over an explicit stack: an element is kept when it is
	// used or keeps a kid.
	type frame struct {
		el   *StructElem
		next int
		kept []*StructElem
	}
	stack := []*frame{{el: doc.Structure}}
	for len(stack) > 0 {
		top := stack[len(stack)-1]
		if top.next < len(top.el.Kids) {
			k := top.el.Kids[top.next]
			top.next++
			stack = append(stack, &frame{el: k})
			continue
		}
		top.el.Kids = top.kept
		stack = stack[:len(stack)-1]
		if len(stack) > 0 && (used[top.el] || len(top.kept) > 0) {
			parent := stack[len(stack)-1]
			parent.kept = append(parent.kept, top.el)
		}
	}
	dropDanglingDests(doc)
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
