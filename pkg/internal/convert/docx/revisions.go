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
// after it, as Word prints a document: the joined paragraph holds the
// content of both and takes the properties of the paragraph whose mark
// ends it, the mark that holds them (ECMA-376 Part 1, 17.3.1.26 pPr is the
// paragraph mark's). A paragraph carrying a section break is never joined.
//
// # Corpus evidence
//
//   - libreoffice/docx layout/merge_hidden_redline: Word prints the text
//     of a centred paragraph whose mark is deleted or hidden at the start
//     edge, as the next paragraph aligns; LibreOffice keeps it centred.
//   - libreoffice/docx layout/CT-formatted-deletion: joined so, layout
//     rises from 0.70 to 0.95.
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
			joined := &ooxml.Element{Space: next.Space, Local: next.Local, Attrs: next.Attrs}
			if ppr := next.Child(w, "pPr"); ppr != nil {
				joined.Children = append(joined.Children, ppr)
			}
			for _, c := range p.Children {
				if !c.Is(w, "pPr") {
					joined.Children = append(joined.Children, c)
				}
			}
			for _, c := range next.Children {
				if !c.Is(w, "pPr") {
					joined.Children = append(joined.Children, c)
				}
			}
			p = joined
			i++
		}
		out = append(out, p)
	}
	return out
}
