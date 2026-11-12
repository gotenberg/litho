package docx

import (
	"encoding/xml"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
)

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

// revisions rewrites the revisions of a part into the view
// office.Options.TrackedChanges asks for (ECMA-376 Part 1, 17.13.5).
type revisions struct {
	view office.TrackedChanges
	// authors numbers the authors of the marked view in the order the
	// converter meets them, the main part first.
	authors map[string]int
	// marked holds the runs a revision marked already: an inner one
	// keeps its own mark. The lines that hold them draw a change bar.
	marked map[*ooxml.Element]bool
	// markEnd and markStart hold the paragraphs of the marked view whose
	// mark a revision inserted or deleted, and the paragraphs after them:
	// the last line of the first and the first line of the second draw a
	// change bar.
	markEnd, markStart map[*ooxml.Element]bool
	// added counts the elements the view made, which the caller charges
	// to the conversion's memory budget as parsed ones.
	added int64
}

// resolve rewrites the content of el, a part's root or an element in it,
// in place:
//
//   - w:ins and w:moveTo (17.13.5.18, 17.13.5.23) keep their content in
//     the accepted and marked views and drop it in the rejected one;
//     w:del and w:moveFrom (17.13.5.14, 17.13.5.22) the other way round,
//     their w:delText and w:delInstrText (17.3.3.7, 17.16.13) read as
//     w:t and w:instrText where they print.
//   - A paragraph mark deleted in the view (17.13.5.15, 17.13.5.21: w:del
//     or w:moveFrom in the mark's run properties for the accepted view,
//     w:ins or w:moveTo, 17.13.5.16 and 17.13.5.24, for the rejected
//     one) joins its paragraph to the next ([mergeDeleted]); the marked
//     view keeps every paragraph.
//   - A table row inserted (17.13.5.17) or deleted (17.13.5.12) goes in
//     the view that drops it.
//   - The marked view draws an insertion's runs underlined and a
//     deletion's struck through, in their author's colour.
//
// Property changes (w:rPrChange, w:pPrChange, w:sectPrChange,
// w:tblPrChange and the rest, 17.13.5.29 to 17.13.5.38) print the new
// properties in every view: the converter reads the properties around
// them, never the old ones they hold. A cell insertion, deletion or merge
// (w:cellIns, w:cellDel, w:cellMerge, 17.13.5.4 to 17.13.5.6) prints its
// cell as it stands.
//
// # Corpus evidence
//
//   - litho/docx tracked-changes: Word prints the accepted text, the
//     deleted row out and the cell marked deleted in, and the formatting
//     of every property change.
func (r *revisions) resolve(el *ooxml.Element, depth int) {
	if depth > ooxml.MaxDepth || len(el.Children) == 0 {
		return
	}
	if strings.HasSuffix(el.Local, "Pr") || strings.HasSuffix(el.Local, "PrChange") {
		// Properties hold revision marks, not revised content.
		return
	}
	var kids []*ooxml.Element
	changed := false
	markChanged := false
	for _, k := range el.Children {
		if k.Space != w {
			r.resolve(k, depth+1)
			kids = append(kids, k)
			continue
		}
		switch k.Local {
		case "ins", "moveTo":
			changed = true
			if r.view == office.ChangesRejected {
				continue
			}
			r.resolve(k, depth+1)
			if r.view == office.ChangesMarked {
				kind := office.MarkInsert
				if k.Local == "moveTo" {
					kind = office.MarkMoveTo
				}
				r.mark(k, attrOr(k, "author"), kind, depth+1)
			}
			kids = append(kids, k.Children...)
		case "del", "moveFrom":
			changed = true
			if r.view == office.ChangesAccepted {
				continue
			}
			r.resolve(k, depth+1)
			undelete(k, depth+1)
			if r.view == office.ChangesMarked {
				kind := office.MarkDelete
				if k.Local == "moveFrom" {
					kind = office.MarkMoveFrom
				}
				r.mark(k, attrOr(k, "author"), kind, depth+1)
			}
			kids = append(kids, k.Children...)
		case "tr":
			if t := k.Child(w, "trPr"); r.view == office.ChangesAccepted && t.Child(w, "del") != nil ||
				r.view == office.ChangesRejected && t.Child(w, "ins") != nil {
				changed = true
				continue
			}
			r.resolve(k, depth+1)
			if r.view == office.ChangesMarked {
				r.rowGround(k)
			}
			kids = append(kids, k)
		case "p":
			if markChanged {
				r.markStart[k] = true
			}
			markChanged = r.paragraphMark(k) && r.view == office.ChangesMarked
			if markChanged {
				r.markEnd[k] = true
			}
			r.resolve(k, depth+1)
			if r.view == office.ChangesMarked {
				r.markAttributes(k, k.Child(w, "pPr").Child(w, "pPrChange"), depth+1)
			}
			kids = append(kids, k)
		default:
			r.resolve(k, depth+1)
			kids = append(kids, k)
		}
	}
	if changed {
		el.Children = kids
	}
}

// paragraphMark leaves in the run properties of p's mark a w:del when the
// view deletes the mark, and no other revision mark. It reports a mark a
// revision inserted or deleted.
func (r *revisions) paragraphMark(p *ooxml.Element) bool {
	rpr := p.Child(w, "pPr").Child(w, "rPr")
	if rpr == nil {
		return false
	}
	var deleted, any bool
	kids := rpr.Children[:0:0]
	for _, c := range rpr.Children {
		if c.Space == w {
			switch c.Local {
			case "del", "moveFrom":
				any = true
				deleted = deleted || r.view == office.ChangesAccepted
				continue
			case "ins", "moveTo":
				any = true
				deleted = deleted || r.view == office.ChangesRejected
				continue
			}
		}
		kids = append(kids, c)
	}
	if !any {
		return false
	}
	if deleted {
		kids = append(kids, &ooxml.Element{Space: w, Local: "del"})
	}
	rpr.Children = kids
	return true
}

// undelete renames the deleted text and field instructions under el to
// the text and instructions they were.
func undelete(el *ooxml.Element, depth int) {
	if depth > ooxml.MaxDepth {
		return
	}
	for _, k := range el.Children {
		switch {
		case k.Is(w, "delText"):
			k.Local = "t"
		case k.Is(w, "delInstrText"):
			k.Local = "instrText"
		default:
			undelete(k, depth+1)
		}
	}
}

// mark draws the runs under el as the marked view draws a change of kind
// by author ([office.MarkKind]), as direct run properties that override
// the run's own: an insertion underlined, a deletion struck through, a
// move doubly so, an attribute change bold (17.3.2.6 color, 17.3.2.40 u,
// 17.3.2.37 strike, 17.3.2.9 dstrike, 17.3.2.1 b). A run an inner
// revision marked keeps its mark.
func (r *revisions) mark(el *ooxml.Element, author string, kind office.MarkKind, depth int) {
	if depth > ooxml.MaxDepth {
		return
	}
	if _, ok := r.authors[author]; !ok {
		r.authors[author] = len(r.authors)
	}
	for _, k := range el.Children {
		if !k.Is(w, "r") {
			r.mark(k, author, kind, depth+1)
			continue
		}
		r.markRun(k, author, kind)
	}
}

// markRun draws the run k as [revisions.mark] does, unless a revision
// marked it already.
func (r *revisions) markRun(k *ooxml.Element, author string, kind office.MarkKind) {
	if r.marked[k] {
		return
	}
	n, ok := r.authors[author]
	if !ok {
		n = len(r.authors)
		r.authors[author] = n
	}
	r.marked[k] = true
	r.added += 3
	rpr := k.Child(w, "rPr")
	if rpr == nil {
		rpr = &ooxml.Element{Space: w, Local: "rPr"}
		k.Children = append([]*ooxml.Element{rpr}, k.Children...)
	}
	props := rpr.Children[:0:0]
	for _, c := range rpr.Children {
		if c.Space == w && (c.Local == "color" || c.Local == "u" || c.Local == "strike" || c.Local == "dstrike" ||
			kind == office.MarkAttribute && (c.Local == "b" || c.Local == "bCs")) {
			continue
		}
		props = append(props, c)
	}
	props = append(props, valElement("color", office.Hex(office.MarkColor(kind, n))))
	switch kind {
	case office.MarkInsert:
		props = append(props, valElement("u", "single"))
	case office.MarkDelete:
		props = append(props, valElement("strike", "1"))
	case office.MarkMoveTo:
		props = append(props, valElement("u", "double"))
	case office.MarkMoveFrom:
		props = append(props, valElement("dstrike", "1"))
	case office.MarkAttribute:
		props = append(props, valElement("b", "1"))
	}
	rpr.Children = props
}

// markAttributes draws, in the marked view, the runs of paragraph p that
// no insertion or deletion marked as an attribute change: every run when
// the paragraph's properties changed (pPrChange, 17.13.5.29, by its
// author), else each run whose properties changed (w:rPrChange,
// 17.13.5.31).
//
// # Corpus evidence
//
//   - probe, docx: LibreOffice 26.8 draws litho/docx tracked-changes's
//     w:rPrChange runs and the paragraphs of its w:pPrChange, the
//     centred paragraph and the numbered one, bold in the author's
//     colour, each line with a change bar; its w:sectPrChange paragraph
//     unmarked.
func (r *revisions) markAttributes(p, pChange *ooxml.Element, depth int) {
	if depth > ooxml.MaxDepth {
		return
	}
	for _, k := range p.Children {
		switch {
		case k.Is(w, "r"):
			if pChange != nil {
				r.markRun(k, attrOr(pChange, "author"), office.MarkAttribute)
			} else if c := k.Child(w, "rPr").Child(w, "rPrChange"); c != nil {
				r.markRun(k, attrOr(c, "author"), office.MarkAttribute)
			}
		case k.Is(w, "pPr"), k.Is(w, "tbl"), k.Is(w, "p"):
		default:
			r.markAttributes(k, pChange, depth+1)
		}
	}
}

// rowGround gives the cells of the table row tr the ground of the marked
// view when its properties hold an insertion or a deletion (w:ins,
// 17.13.5.17; w:del, 17.13.5.12): a cell shading (17.4.32 shd) over the
// cell's own ([office.RowInsertedFill]).
func (r *revisions) rowGround(tr *ooxml.Element) {
	t := tr.Child(w, "trPr")
	fill := uint32(0)
	switch {
	case t.Child(w, "ins") != nil:
		fill = office.RowInsertedFill
	case t.Child(w, "del") != nil:
		fill = office.RowDeletedFill
	default:
		return
	}
	shd := &ooxml.Element{Space: w, Local: "shd", Attrs: []xml.Attr{
		{Name: xml.Name{Space: w, Local: "val"}, Value: "clear"},
		{Name: xml.Name{Space: w, Local: "color"}, Value: "auto"},
		{Name: xml.Name{Space: w, Local: "fill"}, Value: office.Hex(fill)},
	}}
	for _, tc := range tr.Children {
		if !tc.Is(w, "tc") {
			continue
		}
		tcPr := tc.Child(w, "tcPr")
		if tcPr == nil {
			tcPr = &ooxml.Element{Space: w, Local: "tcPr"}
			tc.Children = append([]*ooxml.Element{tcPr}, tc.Children...)
			r.added++
		}
		kids := tcPr.Children[:0:0]
		for _, c := range tcPr.Children {
			if !c.Is(w, "shd") {
				kids = append(kids, c)
			}
		}
		tcPr.Children = append(kids, shd)
		r.added++
	}
}

// changed reports a run the marked view marked: the lines that hold it
// draw a change bar.
func (r *revisions) changed(run *ooxml.Element) bool {
	return r.marked[run]
}

// valElement returns a WordprocessingML element local with a w:val.
func valElement(local, val string) *ooxml.Element {
	return &ooxml.Element{Space: w, Local: local, Attrs: []xml.Attr{{Name: xml.Name{Space: w, Local: "val"}, Value: val}}}
}

// bound adds to sink, at the start of paragraph p when start, else at its
// end, an empty node that draws a change bar beside the line it lands on,
// when a revision inserted or deleted the mark of p or of the paragraph
// before it ([revisions.markEnd]).
//
// # Corpus evidence
//
//   - probe, docx: LibreOffice 26.8 draws a change bar beside the
//     paragraph of litho/docx tracked-changes whose mark is inserted and
//     beside the first line of the paragraph after it, and beside both
//     lines its deleted mark joins.
func (r *revisions) bound(p *ooxml.Element, ps *css.Style, sink *inlineSink, start bool) {
	if start && !r.markStart[p] || !start && !r.markEnd[p] {
		return
	}
	sink.add(&layout.Node{Kind: layout.Element, Style: inlineStyle(ps), ChangeBar: true})
}
