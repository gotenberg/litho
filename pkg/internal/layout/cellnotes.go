package layout

// maxCellNotePasses bounds the layouts of one table that place the
// footnotes its cells call: 4. Anti-DoS guard, not spec-mandated: each
// pass lays the whole table out again, and a footnote area that pushes
// its call's line onto the next page settles in the second pass, the
// third confirming it.
const maxCellNotePasses = 4

// layoutTable lays out the table box t in the containing block cb, its
// border top at y, and places the bodies of the footnote calls its cells
// hold on the pages their lines land on (CSS GCPM 3, 3.8): the cells are
// laid out off the page cuts, so the table is laid out again, its rows
// broken against the footnote areas, until every call's page is stable.
func (e *engine) layoutTable(t *box, cb cblock, y float64) {
	e.layoutTableOnce(t, cb, y)
	if e.foot == nil || !e.paginating() || e.cols != nil || e.err != nil {
		return
	}
	calls := cellCalls(t)
	if len(calls) == 0 {
		return
	}
	for range maxCellNotePasses {
		if !e.placeCellNotes(t, calls) || e.err != nil || !e.relayout(y) {
			return
		}
		e.layoutTableOnce(t, cb, y)
	}
}

// cellCalls returns the footnote calls inside the table box t, in tree
// order.
func cellCalls(t *box) []*box {
	var out []*box
	var walk func(b *box, depth int)
	walk = func(b *box, depth int) {
		if depth > maxTreeDepth {
			return
		}
		if b.fnBody != nil {
			out = append(out, b)
		}
		for _, k := range b.kids {
			walk(k, depth+1)
		}
	}
	walk(t, 0)
	return out
}

// callLines maps each footnote call of calls to the first line box under
// t that holds a piece of it.
func callLines(t *box, calls []*box) map[*box]*lineBox {
	want := make(map[*box]bool, len(calls))
	for _, c := range calls {
		want[c] = true
	}
	out := map[*box]*lineBox{}
	owner := func(b, root *box) *box {
		for d := 0; b != nil && b != root && d <= maxTreeDepth; d++ {
			if want[b] {
				return b
			}
			b = b.parent
		}
		return nil
	}
	var walk func(b *box, depth int)
	walk = func(b *box, depth int) {
		if depth > maxTreeDepth {
			return
		}
		for _, l := range b.lines {
			for _, it := range l.items {
				if c := owner(it.box, b); c != nil && out[c] == nil {
					out[c] = l
				}
			}
			for _, fr := range l.frags {
				if c := owner(fr.box, b); c != nil && out[c] == nil {
					out[c] = l
				}
			}
		}
		for _, k := range b.kids {
			walk(k, depth+1)
		}
	}
	walk(t, 0)
	return out
}

// placeCellNotes places the body of each call of calls, inside the table
// box t, on the page of the line holding the call, as [engine.noteLine]
// places the calls of the flow, and reports whether a body moved, which
// changes the page cuts the table's rows break at. A body that finds no
// room waits for a later page.
func (e *engine) placeCellNotes(t *box, calls []*box) bool {
	f := e.foot
	lines := callLines(t, calls)
	moved := false
	for _, call := range calls {
		l := lines[call]
		if l == nil {
			continue
		}
		page := e.pageOf(l.y + eps)
		if p, ok := f.where[call]; ok && p == page {
			continue
		}
		waiting := false
		if p, ok := f.where[call]; ok && p < 0 {
			waiting = true
		}
		e.unplace(call)
		if len(f.pending) == 0 {
			if ok, _ := e.placeNote(page, call, l.y+l.h, false); ok {
				moved = true
				continue
			}
		}
		f.where[call] = -1
		f.pending = append(f.pending, pendingNote{call: call, from: page})
		if !waiting {
			moved = true
		}
	}
	return moved
}
