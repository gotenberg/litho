package docx

import "github.com/gotenberg/litho/pkg/internal/ooxml"

// markDeleted reports a paragraph whose mark is a tracked deletion
// (ECMA-376 Part 1, 17.13.5.15 del in the paragraph mark's run
// properties) or hidden (17.3.2.41 vanish), which joins it to the next
// one.
func markDeleted(p *ooxml.Element) bool {
	r := p.Child(w, "pPr").Child(w, "rPr")
	if !p.Is(w, "p") || r == nil {
		return false
	}
	if r.Child(w, "del") != nil {
		return true
	}
	v := r.Child(w, "vanish")
	return v != nil && ooxml.Toggle(v)
}

// hiddenAll reports a paragraph whose mark and every run are hidden
// (17.3.2.41 vanish set directly).
func hiddenAll(p *ooxml.Element) bool {
	v := p.Child(w, "pPr").Child(w, "rPr").Child(w, "vanish")
	if !p.Is(w, "p") || v == nil || !ooxml.Toggle(v) {
		return false
	}
	for _, c := range p.Kids() {
		if !c.Is(w, "r") {
			continue
		}
		rv := c.Child(w, "rPr").Child(w, "vanish")
		if rv == nil || !ooxml.Toggle(rv) {
			return false
		}
	}
	return true
}

// sectionMark reports a paragraph that carries a section's properties.
func sectionMark(p *ooxml.Element) bool {
	return p.Child(w, "pPr").Child(w, "sectPr") != nil
}

// mergeDeleted returns the block-level elements kids with every paragraph
// whose mark is a tracked deletion or hidden joined to the paragraph
// after it, as LibreOffice prints a document: the joined
// paragraph keeps the first one's properties and holds the content of
// both. A paragraph carrying a section break is never joined.
func mergeDeleted(kids []*ooxml.Element) []*ooxml.Element {
	merged := false
	for i, k := range kids {
		if markDeleted(k) && !sectionMark(k) && i+1 < len(kids) && kids[i+1].Is(w, "p") && !sectionMark(kids[i+1]) {
			merged = true
			break
		}
	}
	if !merged {
		return kids
	}
	out := make([]*ooxml.Element, 0, len(kids))
	for i := 0; i < len(kids); i++ {
		p := kids[i]
		if hiddenAll(p) && !sectionMark(p) && i+1 < len(kids) && kids[i+1].Is(w, "p") {
			// A hidden paragraph whose runs are all hidden is left out
			// whole: the next paragraph keeps its own properties.
			continue
		}
		for markDeleted(p) && !sectionMark(p) && i+1 < len(kids) && kids[i+1].Is(w, "p") && !sectionMark(kids[i+1]) {
			next := kids[i+1]
			joined := &ooxml.Element{Space: p.Space, Local: p.Local, Attrs: p.Attrs}
			joined.Children = append(joined.Children, p.Children...)
			for _, c := range next.Children {
				if !c.Is(w, "pPr") {
					joined.Children = append(joined.Children, c)
				}
			}
			// The joined paragraph ends with the second one's mark.
			if !markDeleted(next) {
				joined.Children = withMark(joined.Children, next)
			}
			p = joined
			i++
		}
		out = append(out, p)
	}
	return out
}

// withMark returns the children of a joined paragraph with its pPr's run
// properties replaced by those of next's mark, so that the joined
// paragraph's mark is no longer deleted.
func withMark(children []*ooxml.Element, next *ooxml.Element) []*ooxml.Element {
	out := make([]*ooxml.Element, len(children))
	copy(out, children)
	for i, c := range out {
		if !c.Is(w, "pPr") {
			continue
		}
		ppr := &ooxml.Element{Space: c.Space, Local: c.Local, Attrs: c.Attrs}
		for _, k := range c.Children {
			if !k.Is(w, "rPr") {
				ppr.Children = append(ppr.Children, k)
			}
		}
		if r := next.Child(w, "pPr").Child(w, "rPr"); r != nil {
			ppr.Children = append(ppr.Children, r)
		}
		out[i] = ppr
	}
	return out
}
