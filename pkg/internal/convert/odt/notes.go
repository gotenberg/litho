package odt

import (
	"strings"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/intconv"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/odf"
)

// noteState is the notes of the document: the numbers given and the
// endnote bodies, laid out after the text.
type noteState struct {
	counts map[string]int
	ends   []*layout.Node
	// sections are the notes collected at the end of the sections being
	// built, innermost last ([builder.sectionNotes]).
	sections []*sectionNotes
}

// sectionNotes are the notes a section collects at its end: the note
// classes it collects and their bodies, in order.
type sectionNotes struct {
	classes map[string]bool
	bodies  []*layout.Node
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
		// An absent or unreadable text:start-value counts from 0, Atoi's
		// result on an error.
		start, _ := intconv.Atoi(cfg.A(odf.NSText, "start-value"))
		start = max(-intconv.MaxCountStart, min(start, intconv.MaxCountStart))
		format := cfg.A(odf.NSStyle, "num-format")
		if format == "" {
			// Writer numbers footnotes in arabic and endnotes in lower-case
			// roman numerals.
			format = "1"
			if class == "endnote" {
				format = "i"
			}
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
	restore := b.comments.apart()
	b.boxed++
	cell := b.cellStyle
	b.cellStyle = ""
	body := b.blocks(bodyEl, blockCtx{depth: depth, defStyle: b.notePara(cfg, class)})
	b.cellStyle = cell
	b.boxed--
	restore()
	b.hf = saved
	prefix := b.run(label, bt)
	// The call links to its note's number and the number back to the
	// call, as Writer exports a note.
	callID, noteID := b.newID("call"), b.newID("note")
	call.ID, call.Href = callID, "#"+noteID
	prefix.ID, prefix.Href = noteID, "#"+callID
	// Each link is a Link element, as ISO 14289-1, 7.18.1 asks of every
	// link annotation (ISO 32000-1, 14.8.4.4.2).
	for _, l := range []*layout.Node{call, prefix} {
		if l.Kind == layout.Element && l.Role == "" {
			l.Role = "Link"
		}
	}
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
	nb := b.node(&layout.Node{Kind: layout.Element, Style: bst, Role: "Note", Children: b.wrapLists(body, 0)})
	for i := len(b.notes.sections) - 1; i >= 0; i-- {
		if sn := b.notes.sections[i]; sn.classes[class] {
			sn.bodies = append(sn.bodies, nb)
			*out = append(*out, call)
			return
		}
	}
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

// sectionNotes starts collecting the notes of the section of style name,
// for the note classes its section properties hold a
// text:notes-configuration of (ODF 1.3 Part 3, 16.29.3 and 17.11: Writer
// collects such notes at the end of the section), and returns the
// function that stops and returns the blocks to set after the section's
// text: the separator of the page layout's footnotes, then the notes. It
// returns nil when the section collects nothing.
//
// # Corpus evidence
//
//   - libreoffice/odt: odfexport/footnote-collect-at-end-of-section.fodt
//     prints the two footnotes of each of its two sections under the
//     section's line, a 0.5 point rule a quarter of the area wide 2.85
//     points below that line and the notes 2.85 points below the rule,
//     the second section's line right after the first's notes.
func (b *builder) sectionNotes(name string) func() []*layout.Node {
	st := b.styles.Style(b.scope, odf.FamilySection, name)
	if st == nil || st.El == nil {
		return nil
	}
	classes := map[string]bool{}
	for _, pr := range st.El.All(odf.NSStyle, "section-properties") {
		for _, cfg := range pr.All(odf.NSText, "notes-configuration") {
			if c := cfg.A(odf.NSText, "note-class"); c == "endnote" {
				classes[c] = true
			} else {
				classes["footnote"] = true
			}
		}
	}
	if len(classes) == 0 {
		return nil
	}
	if b.notes == nil {
		b.notes = &noteState{counts: map[string]int{}}
	}
	sn := &sectionNotes{classes: classes}
	b.notes.sections = append(b.notes.sections, sn)
	return func() []*layout.Node {
		b.notes.sections = b.notes.sections[:len(b.notes.sections)-1]
		if len(sn.bodies) == 0 {
			return nil
		}
		r := b.pageRule()
		rule := baseStyle()
		rule.Display = css.Block
		rule.Width = css.Dimension{Kind: css.Percent, Value: r.Width}
		rule.Margin[css.Top], rule.Margin[css.Bottom] = css.Pts(r.SpaceAbove), css.Pts(r.SpaceBelow)
		col := r.Color
		if col.A == 0 {
			col = css.Color{A: 1}
		}
		rule.Border[css.Top] = css.Border{Width: r.Thickness, Style: r.Line, Color: col}
		switch r.Align {
		case css.AlignCenter:
			rule.Margin[css.Left], rule.Margin[css.Right] = css.Dimension{Kind: css.Auto}, css.Dimension{Kind: css.Auto}
		case css.AlignRight:
			rule.Margin[css.Left] = css.Dimension{Kind: css.Auto}
		case css.AlignLeft, css.AlignStart, css.AlignEnd, css.AlignJustify, css.AlignJustifyAll, css.AlignAuto:
			// footnoteRule sets left, centre or right alone.
		}
		return append([]*layout.Node{b.node(&layout.Node{Kind: layout.Element, Style: rule})}, sn.bodies...)
	}
}

// pageRule returns the separator of the footnotes of the page being built,
// Writer's default rule when it names none.
func (b *builder) pageRule() *layout.FootnoteRule {
	if g := b.currentGeom(); g != nil && g.box.FootnoteRule != nil {
		return g.box.FootnoteRule
	}
	return footnoteRule(odf.Props{})
}
