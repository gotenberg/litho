package compose

// SelectPages keeps the pages of doc that pages names, 1-based and in that
// order, a page named twice written twice, and drops the rest: the links
// to a dropped page go, the links to a kept page lead to its new number
// (a page kept twice, to its first copy), an outline entry on a dropped
// page is replaced by its kids, a warning of a dropped page loses its
// page, and the structure tree is pruned of what the dropped pages alone
// held. Every entry of pages lies in [1, len(doc.Pages)]: the caller has
// parsed and bounded the selection. A nil or empty pages keeps the
// document as it is.
func SelectPages(doc *Document, pages []int) {
	if len(pages) == 0 {
		return
	}
	// at maps a page's old 1-based number to its new one, 0 when dropped.
	at := make([]int, len(doc.Pages)+1)
	kept := make([]*Page, 0, len(pages))
	for _, n := range pages {
		if n < 1 || n > len(doc.Pages) {
			continue
		}
		kept = append(kept, doc.Pages[n-1])
		if at[n] == 0 {
			at[n] = len(kept)
		}
	}
	if len(kept) == 0 {
		return
	}
	dest := func(d Dest) (Dest, bool) {
		if d.Page < 1 || d.Page >= len(at) || at[d.Page] == 0 {
			return d, false
		}
		d.Page = at[d.Page]
		return d, true
	}
	dropped := false
	for n := 1; n < len(at); n++ {
		if at[n] == 0 {
			dropped = true
			break
		}
	}
	seen := make(map[*Page]bool, len(kept))
	for _, pg := range kept {
		if seen[pg] {
			continue
		}
		seen[pg] = true
		links := pg.Links[:0:0]
		for _, l := range pg.Links {
			if l.Dest != nil {
				d, ok := dest(*l.Dest)
				if !ok {
					continue
				}
				l.Dest = &d
			}
			links = append(links, l)
		}
		pg.Links = links
	}
	doc.Pages = kept
	doc.Outline = selectOutline(doc.Outline, dest)
	for i := range doc.Warnings {
		if w := &doc.Warnings[i]; w.Page > 0 {
			if w.Page < len(at) {
				w.Page = at[w.Page]
			} else {
				w.Page = 0
			}
		}
	}
	if dropped {
		PruneStructure(doc)
	}
}

// selectOutline renumbers the outline entries through dest and replaces
// an entry whose page is dropped by its kids, in its place.
func selectOutline(items []Outline, dest func(Dest) (Dest, bool)) []Outline {
	out := items[:0:0]
	for _, it := range items {
		kids := selectOutline(it.Kids, dest)
		d, ok := dest(it.Dest)
		if !ok {
			out = append(out, kids...)
			continue
		}
		it.Dest, it.Kids = d, kids
		out = append(out, it)
	}
	return out
}
