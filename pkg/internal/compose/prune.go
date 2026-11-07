package compose

// PruneStructure drops from doc's structure tree the elements no fragment
// or link of its pages hangs from, directly or through their kids, once a
// page selection left pages out or culling left content out, so the tree
// written (ISO 32000-1, 14.7.2) names no element without content. The
// root stays. A nil tree is left alone.
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
}
