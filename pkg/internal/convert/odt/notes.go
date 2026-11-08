package odt

import (
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/odf"
)

// noteState is the notes of the document: the numbers given and the
// endnote bodies, laid out after the text.
type noteState struct {
	counts map[string]int
	ends   []*layout.Node
}

// noteAnchorRaise and noteAnchorScale are the superscript a note citation
// takes in the text when its notes configuration names no character
// style: Writer's "Footnote Anchor", raised a third, 58% of the size.
const (
	noteAnchorRaise = 0.33
	noteAnchorScale = 0.58
)

// note appends a text:note (ODF 1.3, Part 3, 6.3): its citation in the
// text, a superscript in the citation style the notes configuration
// names, and its body, a footnote laid out at the bottom of the page the
// citation lands on, or an endnote laid out after the text, as
// text:notes-configuration's footnotes-position says.
func (b *builder) note(c *odf.Element, t textState, out *[]*layout.Node, ws *wsState, depth int) {
	if b.notes == nil {
		b.notes = &noteState{counts: map[string]int{}}
	}
	class := c.A(odf.NSText, "note-class")
	if class != "endnote" {
		class = "footnote"
	}
	cfg := b.styles.Notes[class]
	b.notes.counts[class]++
	cit := c.Child(odf.NSText, "note-citation")
	label := cit.A(odf.NSText, "label")
	if label == "" {
		label = strings.TrimSpace(cit.TextContent())
	}
	if label == "" {
		start, _ := strconv.Atoi(cfg.A(odf.NSText, "start-value"))
		format := cfg.A(odf.NSStyle, "num-format")
		if format == "" {
			format = map[string]string{"footnote": "1", "endnote": "i"}[class]
		}
		label = cfg.A(odf.NSStyle, "num-prefix") + formatNumber(b.notes.counts[class]+start, format) + cfg.A(odf.NSStyle, "num-suffix")
	}
	at := t
	at.under, at.through, at.over = decoLine{}, decoLine{}, decoLine{}
	if name := cfg.A(odf.NSText, "citation-body-style-name"); name != "" && b.styles.Style(b.scope, odf.FamilyText, name) != nil {
		at = b.spanText(at, name)
	} else {
		at.pos, at.rel, at.auto = noteAnchorRaise, noteAnchorScale, 1
	}
	call := b.run(label, at)
	ws.space = false

	// The number in the note area takes the text properties of the
	// note's first paragraph, then the citation style the configuration
	// names for it.
	bodyEl := c.Child(odf.NSText, "note-body")
	para := b.notePara(cfg, class)
	for _, p := range bodyEl.Children {
		if p.Is(odf.NSText, "p") || p.Is(odf.NSText, "h") {
			if p.A(odf.NSText, "style-name") != "" {
				para = b.paraStyleName(p)
			}
			break
		}
	}
	bt := b.paraText(para)
	if name := cfg.A(odf.NSText, "citation-style-name"); name != "" {
		bt = b.spanText(bt, name)
	}
	saved := b.hf
	b.hf = nil
	body := b.blocks(bodyEl, blockCtx{depth: depth, defStyle: b.notePara(cfg, class)})
	b.hf = saved
	prefix := b.run(label, bt)
	// The call links to its note's number and the number back to the
	// call, as Writer exports a note.
	callID, noteID := b.newID("call"), b.newID("note")
	call.ID, call.Href = callID, "#"+noteID
	prefix.ID, prefix.Href = noteID, "#"+callID
	if len(body) > 0 && body[0].Style.Display != css.Table && body[0].Style.TextIndent.Value < 0 {
		// Writer sets the number in a hanging first-line indent and
		// starts the text at the paragraph's indent, as a list label.
		ps := baseStyle()
		applyText(ps, bt, false)
		ps.Display = css.InlineBlock
		ps.MinWidth = css.Pts(-body[0].Style.TextIndent.Value)
		prefix = b.node(&layout.Node{Kind: layout.Element, Style: ps, Children: []*layout.Node{prefix}})
	}
	if len(body) > 0 && body[0].Style.Display != css.Table {
		body[0].Children = append([]*layout.Node{prefix}, body[0].Children...)
	} else {
		body = append([]*layout.Node{b.node(&layout.Node{Kind: layout.Element, Style: blockOf(bt), Children: []*layout.Node{prefix}})}, body...)
	}
	bst := baseStyle()
	bst.Display = css.Block
	nb := b.node(&layout.Node{Kind: layout.Element, Style: bst, Role: "Note", Children: body})
	if class == "endnote" || cfg.A(odf.NSText, "footnotes-position") == "document" {
		b.notes.ends = append(b.notes.ends, nb)
		*out = append(*out, call)
		return
	}
	call.Footnote = nb
	*out = append(*out, call)
}

// notePara returns the default paragraph style of a note class's bodies:
// text:default-style-name of its configuration, else Writer's "Footnote"
// or "Endnote".
func (b *builder) notePara(cfg *odf.Element, class string) string {
	if n := cfg.A(odf.NSText, "default-style-name"); n != "" {
		return n
	}
	if class == "endnote" {
		return "Endnote"
	}
	return "Footnote"
}

// blockOf returns a block of the text properties t.
func blockOf(t textState) *css.Style {
	st := baseStyle()
	applyText(st, t, false)
	st.Display = css.Block
	return st
}

// endnotes returns the block of the endnotes, laid out on a page of their
// own after the text as Writer does, nil when there are none.
func (b *builder) endnotes() *layout.Node {
	if b.notes == nil || len(b.notes.ends) == 0 {
		return nil
	}
	st := baseStyle()
	st.Display = css.Block
	st.BreakBefore = css.Page
	return b.node(&layout.Node{Kind: layout.Element, Style: st, Children: b.notes.ends})
}
