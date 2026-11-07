package docx

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
)

// story is one flow of text being built: the body, a header or footer, a
// note. It carries what spans paragraphs: the complex fields open and a
// page break deferred to the next paragraph.
type story struct {
	// part is the part the story's relationships resolve against.
	part string
	// fields gives the values of the page fields in a header or footer,
	// nil in the body, where they show their cached result.
	fields *fieldValues
	// open is the stack of complex fields being read (17.16.18).
	open []*field
	// breakNext defers a page break that ended a paragraph to the next
	// one, as LibreOffice imports it; breakColumn says it was a column
	// break.
	breakNext, breakColumn bool
	// note is the note a footnoteRef or endnoteRef run inside a note body
	// numbers, "" outside one.
	note string
}

// fieldValues are the page fields of one page (17.16.5: PAGE and
// NUMPAGES): the page's number, the page count, and the section's page
// number format.
type fieldValues struct {
	page, pages int
	format      string
}

// cond is what a table style's conditional formatting adds to the
// paragraphs of a cell (17.7.6), nil outside tables.
type cond struct {
	ppr paraProps
	rpr runProps
}

// paraInfo is what the post-pass over a container's paragraphs needs.
type paraInfo struct {
	style             string
	before, after     float64
	contextual        bool
	borders           map[string]*borderSpec
	indLeft, indRight float64
}

// blockNode is a block-level node with the paragraph data the post-pass
// reads, nil info for a table or anything else.
type blockNode struct {
	node *layout.Node
	info *paraInfo
}

// blocks builds the block-level content of a container element (w:body,
// w:tc, w:hdr, w:txbxContent, w:footnote...): paragraphs, tables and the
// content controls and revisions wrapping them.
func (d *document) blocks(s *story, c *cond, parent *ooxml.Element, depth int) []blockNode {
	var out []blockNode
	d.blocksInto(s, c, parent, depth, &out)
	return out
}

// blocksInto appends the block-level content of parent to out.
func (d *document) blocksInto(s *story, c *cond, parent *ooxml.Element, depth int, out *[]blockNode) {
	if depth > maxNesting {
		d.warn("too-deep", "content nested too deep is left out")
		return
	}
	for _, e := range parent.Kids() {
		if e.Space != w || d.ctx.Err() != nil {
			continue
		}
		switch e.Local {
		case "p":
			*out = append(*out, d.paragraph(s, c, e, depth)...)
		case "tbl":
			if n := d.table(s, e, depth+1); n != nil {
				*out = append(*out, blockNode{node: n})
			}
		case "sdt":
			d.blocksInto(s, c, e.Child(w, "sdtContent"), depth+1, out)
		case "customXml", "ins", "moveTo", "smartTag":
			d.blocksInto(s, c, e, depth+1, out)
		case "altChunk":
			d.warn("unsupported-content", "an imported chunk (altChunk) is left out")
		case "br":
			// A break outside a run, which the schema does not allow and
			// LibreOffice reads: a page or column break moves the next
			// paragraph.
			if t := attrOr(e, "type"); t == "page" || t == "column" {
				s.breakNext, s.breakColumn = true, t == "column"
			}
		}
	}
}

// maxNesting caps the nesting of tables, content controls and text boxes
// the converter follows. Anti-DoS guard, not spec-mandated: well under
// [ooxml.MaxDepth], and Word nests tables at most a few levels.
const maxNesting = 64

// finish applies what depends on neighbouring paragraphs: the spacing
// between them, the larger of the space after one and before the next as
// LibreOffice lays a DOCX document out (which the margins collapsing
// gives), the contextual spacing that drops it between paragraphs of one
// style (17.3.1.9), and the borders paragraphs with the same borders
// share (17.3.1.24: the group draws one box, the between border inside
// it).
func finish(bs []blockNode) []*layout.Node {
	out := make([]*layout.Node, 0, len(bs))
	var list *layout.Node
	for i, b := range bs {
		// Consecutive list items share an L structure element (ISO
		// 32000-1, 14.8.4.3.3), a block with no box of its own.
		switch {
		case b.node.Role != "LI":
			list = nil
			out = append(out, b.node)
		case list == nil:
			list = &layout.Node{Kind: layout.Element, Style: listStyle(b.node.Style), Role: "L", Children: []*layout.Node{b.node}}
			out = append(out, list)
		default:
			list.Children = append(list.Children, b.node)
		}
		info := b.info
		if info == nil {
			continue
		}
		st := b.node.Style
		var prev, next *paraInfo
		if i > 0 {
			prev = bs[i-1].info
		}
		if i+1 < len(bs) {
			next = bs[i+1].info
		}
		before, after := info.before, info.after
		if info.contextual && prev != nil && prev.style == info.style {
			before = 0
		}
		if info.contextual && next != nil && next.style == info.style {
			after = 0
		}
		st.Margin[css.Top], st.Margin[css.Bottom] = css.Pts(before), css.Pts(after)
		if len(info.borders) > 0 {
			if prev != nil && sameBorders(prev, info) {
				st.Border[css.Top] = css.Border{}
				if b := info.borders["between"]; b != nil {
					st.Border[css.Top] = b.Border
				}
				st.Padding[css.Top] = css.Pts(0)
			}
			if next != nil && sameBorders(info, next) {
				st.Border[css.Bottom] = css.Border{}
				st.Padding[css.Bottom] = css.Pts(0)
			}
		}
	}
	return out
}

// listStyle returns the style of the block grouping list items styled
// like item: their inherited text properties and no box of its own.
func listStyle(item *css.Style) *css.Style {
	st := *inlineStyle(item)
	st.Display = css.Block
	st.TabStops = nil
	return &st
}

// sameBorders reports two paragraphs that share their border group: the
// same borders and indents (17.3.1.24).
func sameBorders(a, b *paraInfo) bool {
	if len(a.borders) != len(b.borders) || a.indLeft != b.indLeft || a.indRight != b.indRight {
		return false
	}
	for k, v := range a.borders {
		o := b.borders[k]
		if o == nil || *o != *v {
			return false
		}
	}
	return true
}

// resolvedPara is a paragraph's properties after the style hierarchy:
// document defaults, table style, paragraph style, numbering, direct
// formatting (17.7.2).
type resolvedPara struct {
	pp      paraProps
	run     runProps // the run properties the paragraph's runs start from
	mark    runProps // the paragraph mark's, for empty lines and labels
	styleID string
	num     *numInstance
	ilvl    int
}

// resolvePara resolves the properties of a paragraph with direct
// properties direct inside the conditional formatting c.
func (d *document) resolvePara(direct *paraProps, c *cond) resolvedPara {
	ss := d.styles
	styleID := direct.style
	if st, ok := ss.byID[styleID]; !ok || st.typ != "paragraph" {
		styleID = ss.defaultPara
	}
	sp, sr := ss.paraStyle(styleID)
	pp := ss.defPara
	run := ss.defRun
	if c != nil {
		pp = pp.merge(&c.ppr)
		run = run.merge(&c.rpr)
	}
	pp = pp.merge(&sp)
	run = run.merge(&sr)
	rp := resolvedPara{styleID: styleID}
	// The numbering a paragraph names directly, else its style's.
	numID, ilvl := sp.numID, sp.ilvl
	if direct.numID != nil {
		numID = direct.numID
	}
	if direct.ilvl != nil {
		ilvl = direct.ilvl
	}
	if numID != nil && *numID != 0 {
		if ni := d.instance(*numID); ni != nil {
			rp.num = ni
			if ilvl != nil {
				rp.ilvl = *ilvl
			}
			pp = pp.merge(ni.levelOf(rp.ilvl).ppr)
		}
	}
	pp = pp.merge(direct)
	rp.pp = pp
	rp.run = run
	rp.mark = run.merge(pp.mark)
	if rp.mark.style != "" {
		rp.mark = run.merge(ptr(ss.charStyle(rp.mark.style))).merge(pp.mark)
	}
	return rp
}

// paragraph builds a w:p: one block per piece its page breaks cut it into
// (a page break inside a paragraph splits it, as LibreOffice imports it),
// numbered as a list item when its numbering says so.
func (d *document) paragraph(s *story, c *cond, p *ooxml.Element, depth int) []blockNode {
	direct := d.parseParaProps(p.Child(w, "pPr"))
	rp := d.resolvePara(direct, c)
	pp := rp.pp
	if pp.mark != nil && boolOf(pp.mark.vanish) && len(p.Children) <= 1 {
		// A hidden paragraph mark on an empty paragraph hides it.
		return nil
	}
	st, lineMult := d.paraStyle(pp, rp.mark)
	sink := &inlineSink{}
	d.inlines(s, rp, st, lineMult, p, sink, depth)
	segs := sink.finish()
	var out []blockNode
	breaksOnly := true
	for _, seg := range segs {
		if len(seg.nodes) > 0 || seg.id != "" {
			breaksOnly = false
		}
	}
	for i, seg := range segs {
		if len(segs) > 1 && len(seg.nodes) == 0 && seg.id == "" {
			if i == len(segs)-1 {
				// A page break ending the paragraph moves the next one.
				s.breakNext, s.breakColumn = true, seg.column
				break
			}
			if i == 0 && !s.breakNext && !breaksOnly {
				// Nor does a page break opening it leave an empty line,
				// unless a break before it already moves it or the
				// paragraph holds nothing else: then it keeps its empty
				// line.
				continue
			}
		}
		bst := *st
		pst := &bst
		pst.Margin = st.Margin
		n := &layout.Node{Kind: layout.Element, Style: pst, Role: "P", ID: seg.id}
		// A hard page break drops the space before the text it moves, as
		// LibreOffice lays DOCX documents out.
		hardBreak := i > 0 || s.breakNext
		column := i > 0 && seg.column || i == 0 && s.breakNext && s.breakColumn
		switch {
		case hardBreak && column:
			// A column break is a page break in a section of one column
			// (17.18.3 ST_BrType column); the section's properties, at its
			// end, say which.
			d.columnBreaks = append(d.columnBreaks, pst)
		case hardBreak || boolOf(pp.pageBreakBefore):
			pst.BreakBefore = css.Page
		}
		if boolOf(pp.pageBreakBefore) {
			pst.BreakBefore = css.Page
		}
		s.breakNext, s.breakColumn = false, false
		if i == 0 && rp.num != nil {
			d.number(n, rp, st)
		}
		kids := seg.nodes
		if !hasContent(kids) {
			// An empty paragraph keeps one line, as tall as its mark's text
			// would be, and draws nothing a reader would extract.
			ms := inlineStyle(pst)
			d.applyRun(ms, rp.mark, lineMult)
			pst.MinHeight = css.Pts(d.lineHeight(ms))
		}
		n.Children = kids
		if sink.positioned {
			pst.Position = css.Relative
		}
		if pp.outline != nil && *pp.outline < 9 {
			lvl := *pp.outline
			if lvl < 6 {
				n.Role = "H" + strconv.Itoa(lvl+1)
			}
			if n.ID == "" {
				n.ID = d.newID("h")
			}
			d.headings = append(d.headings, heading{node: n, level: lvl + 1})
		}
		d.frame(n, pp)
		info := &paraInfo{style: rp.styleID, contextual: boolOf(pp.contextual), borders: pp.borders}
		info.before, info.after = spacingOf(pp)
		if hardBreak {
			info.before = 0
		}
		if pp.indLeft != nil {
			info.indLeft = *pp.indLeft
		}
		if pp.indRight != nil {
			info.indRight = *pp.indRight
		}
		out = append(out, blockNode{node: n, info: info})
	}
	return out
}

// spacingOf returns the space before and after a paragraph in points
// (17.3.1.33); automatic spacing takes the 14 points of HTML paragraphs,
// as Word and LibreOffice size it.
func spacingOf(pp paraProps) (float64, float64) {
	var before, after float64
	if pp.before != nil {
		before = *pp.before
	}
	if pp.after != nil {
		after = *pp.after
	}
	if boolOf(pp.beforeAuto) {
		before = 14
	}
	if boolOf(pp.afterAuto) {
		after = 14
	}
	return before, after
}

// hasContent reports whether nodes draw something on a line.
func hasContent(nodes []*layout.Node) bool {
	for _, n := range nodes {
		switch n.Kind {
		case layout.Text:
			if n.Text != "" {
				return true
			}
		case layout.LineBreak, layout.Replaced:
			return true
		case layout.Element:
			if n.Footnote != nil || n.Style != nil && (n.Style.Display != css.Inline || n.Style.Float != css.FloatNone || n.Style.Position == css.Absolute) {
				return true
			}
			if hasContent(n.Children) {
				return true
			}
		}
	}
	return false
}

// paraStyle returns the style of a paragraph block from its resolved
// properties and the run properties of its mark, and the proportional line
// spacing its runs scale (0 for none).
func (d *document) paraStyle(pp paraProps, mark runProps) (*css.Style, float64) {
	st := d.blockStyle()
	lineMult := 0.0
	rule := css.LineRuleCSS
	if pp.line != nil {
		kind := "auto"
		if pp.lineRule != nil {
			kind = *pp.lineRule
		}
		switch kind {
		case "exact":
			st.LineHeight = css.Pts(max(1, *pp.line*ooxml.TwipPt))
			rule = css.LineRuleExact
		case "atLeast":
			st.MinLineHeight = max(0, *pp.line*ooxml.TwipPt)
		default:
			// 240ths of a line (17.18.48, ST_LineSpacingRule auto).
			if m := *pp.line / 240; m > 0 && abs(m-1) > 0.001 {
				lineMult = min(m, 10)
				rule = css.LineRuleMultiple
			}
		}
	}
	d.applyRun(st, mark, lineMult)
	// Exact and proportional spacing place the lines as Writer does
	// (css.LineRule): an exact height with the baseline at four fifths of
	// it, a multiple of the natural height. Single spacing keeps the
	// face's normal line height, which is Writer's single line. The runs
	// keep their proportional line height, for the empty lines sized from
	// them.
	st.LineRule = rule
	if rule == css.LineRuleMultiple {
		st.LineHeight = css.Dimension{Kind: css.Number, Value: lineMult}
	}
	st.TextDecoration = 0
	st.BackgroundColor = css.Transparent
	st.VerticalAlign = css.VerticalAlign{}
	st.TextShadow = nil
	rtl := boolOf(pp.bidi)
	if rtl {
		st.Direction = css.RTL
	}
	left, right := 0.0, 0.0
	if pp.indLeft != nil {
		left = *pp.indLeft
	}
	if pp.indRight != nil {
		right = *pp.indRight
	}
	start, end := css.Left, css.Right
	if rtl {
		start, end = css.Right, css.Left
	}
	st.Margin[start], st.Margin[end] = css.Pts(left), css.Pts(right)
	if pp.firstLine != nil {
		st.TextIndent = css.Pts(*pp.firstLine)
	}
	st.TextAlign = css.AlignStart
	if pp.jc != nil {
		switch *pp.jc {
		case "center":
			st.TextAlign = css.AlignCenter
		case "right", "end":
			st.TextAlign = css.AlignEnd
		case "both", "lowKashida", "mediumKashida", "highKashida", "thaiDistribute":
			st.TextAlign = css.AlignJustify
		case "distribute":
			st.TextAlign = css.AlignJustify
			st.TextAlignLast = css.AlignJustify
		case "left":
			st.TextAlign = css.AlignLeft
			if rtl {
				// left in a right-to-left paragraph is its end (17.3.1.13).
				st.TextAlign = css.AlignEnd
			}
		}
	}
	if boolOf(pp.keepNext) {
		st.BreakAfter = css.BreakAvoidPage
	}
	if boolOf(pp.keepLines) {
		st.BreakInside = css.InsideAvoidPage
	}
	st.Orphans, st.Widows = 1, 1
	if pp.widow == nil || *pp.widow {
		st.Orphans, st.Widows = 2, 2
	}
	for name, b := range pp.borders {
		var side css.Side
		switch name {
		case "top":
			side = css.Top
		case "bottom":
			side = css.Bottom
		case "left":
			side = css.Left
		case "right":
			side = css.Right
		default:
			continue
		}
		if b.Style == css.BorderNone {
			continue
		}
		st.Border[side] = b.Border
		st.Padding[side] = css.Pts(b.space)
		if side == css.Left || side == css.Right {
			// Side borders sit outside the indents, their space and width
			// taken from the margin (17.3.4).
			m := st.Margin[side].Value - b.space - b.Width
			st.Margin[side] = css.Pts(m)
		}
	}
	if pp.shading != nil {
		st.BackgroundColor = *pp.shading
	}
	if len(pp.borders) > 0 {
		// Writer draws a bordered paragraph's border whole on every page
		// it spans.
		st.BoxDecorationBreak = css.DecorationCloneAll
	}
	// Word measures tab stops from the margin, the layout from the
	// paragraph's content box: a stop moves left by the start indent, and
	// one before the content box is dropped. A hanging indent is a stop of
	// its own (17.3.1.37). The slice is never nil, so that the paragraph
	// lays its tabs out the Office way.
	stops := make([]css.TabStop, 0, len(pp.tabs)+1)
	for _, t := range pp.tabs {
		s := t.stop
		s.Pos -= left
		if s.Pos < 0 {
			continue
		}
		stops = append(stops, s)
	}
	if pp.firstLine != nil && *pp.firstLine < 0 {
		stops = append(stops, css.TabStop{Pos: 0})
	}
	sortStops(stops)
	st.TabStops = stops
	return st, lineMult
}

// sortStops sorts tab stops by position, insertion sort over a short list.
func sortStops(s []css.TabStop) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j].Pos < s[j-1].Pos; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// number makes the paragraph n a list item of its numbering: its ordinal
// and its label as a marker (17.9). The label of a hanging indent followed
// by a tab sits in a marker box as wide as the hanging indent, aligned on
// the number position by its lvlJc, the text at the indent; any other
// label opens the first line, followed by its suffix, as LibreOffice
// draws a label followed by a space.
func (d *document) number(n *layout.Node, rp resolvedPara, st *css.Style) {
	label, ordinal := rp.num.next(rp.ilvl)
	lv := rp.num.levelOf(rp.ilvl)
	if label == "" {
		return
	}
	mr := rp.mark
	mr.underline = nil
	mr = mr.merge(lv.rpr)
	ms := inlineStyle(st)
	d.applyRun(ms, mr, 0)
	ms.LineHeight = st.LineHeight
	if fam := d.families(mr); len(fam) > 0 {
		if mapped, drop := mapSymbolText(label, fam[0]); drop {
			label = mapped
			ms.FontFamilies = d.families(rp.mark)
		}
	}
	n.Style.Display = css.ListItem
	n.Role = "LI"
	n.ListValue = max(ordinal, 1)
	switch lv.jc {
	case "center":
		ms.TextAlign = css.AlignCenter
	case "right", "end":
		ms.TextAlign = css.AlignEnd
	default:
		ms.TextAlign = css.AlignStart
	}
	hang := 0.0
	if st.TextIndent.Kind == css.Points && st.TextIndent.Value < 0 {
		hang = -st.TextIndent.Value
	}
	if hang > 0 && lv.suffix == "tab" {
		// The marker box spans the hanging indent: the label aligned on the
		// number position, the text at the indent on every line.
		n.Style.ListStylePosition = css.Outside
		n.Style.TextIndent = css.Pts(0)
		ms.Width = css.Pts(hang)
		ms.Display = css.Inline
		n.Marker = &layout.Node{Kind: layout.Element, Style: ms, Children: []*layout.Node{{Kind: layout.Text, Style: ms, Text: label}}}
		return
	}
	suffix := "\t"
	switch lv.suffix {
	case "space":
		suffix = " "
	case "nothing":
		suffix = ""
	}
	n.Style.ListStylePosition = css.Inside
	n.Marker = &layout.Node{Kind: layout.Element, Style: ms, Children: []*layout.Node{{Kind: layout.Text, Style: ms, Text: label + suffix}}}
}

// inlineSink collects a paragraph's inline nodes, cut into segments at its
// page breaks.
type inlineSink struct {
	segs []*segment
	cur  *segment
	// links collects the nodes of the open hyperlinks.
	links []*linkFrame
	// positioned is set when a drawing is positioned from the paragraph,
	// which then becomes its containing block.
	positioned bool
}

// segment is one piece of a paragraph between page breaks.
type segment struct {
	nodes []*layout.Node
	id    string
	// column is set when a column break, not a page break, starts it.
	column bool
}

// linkFrame is a hyperlink being collected.
type linkFrame struct {
	href  string
	nodes []*layout.Node
}

// add appends n to the open hyperlink, else the current segment.
func (k *inlineSink) add(n *layout.Node) {
	if len(k.links) > 0 {
		f := k.links[len(k.links)-1]
		f.nodes = append(f.nodes, n)
		return
	}
	if k.cur == nil {
		k.cur = &segment{}
		k.segs = append(k.segs, k.cur)
	}
	k.cur.nodes = append(k.cur.nodes, n)
}

// pageBreak starts a new segment, after a column break when column.
func (k *inlineSink) pageBreak(column bool) {
	if k.cur == nil {
		k.cur = &segment{}
		k.segs = append(k.segs, k.cur)
	}
	k.cur = &segment{column: column}
	k.segs = append(k.segs, k.cur)
}

// anchor names the current segment, for a bookmark.
func (k *inlineSink) anchor(id string) bool {
	if k.cur == nil {
		k.cur = &segment{}
		k.segs = append(k.segs, k.cur)
	}
	if k.cur.id == "" && len(k.cur.nodes) == 0 {
		k.cur.id = id
		return true
	}
	return false
}

// openLink starts collecting a hyperlink.
func (k *inlineSink) openLink(href string) { k.links = append(k.links, &linkFrame{href: href}) }

// closeLink ends the innermost hyperlink and adds it as a Link element.
func (k *inlineSink) closeLink() {
	if len(k.links) == 0 {
		return
	}
	f := k.links[len(k.links)-1]
	k.links = k.links[:len(k.links)-1]
	if len(f.nodes) == 0 {
		return
	}
	st := *f.nodes[0].Style
	st.Display = css.Inline
	k.add(&layout.Node{Kind: layout.Element, Style: inlineStyle(&st), Href: f.href, Role: "Link", Children: f.nodes})
}

// finish closes the open hyperlinks and returns the segments, at least
// one.
func (k *inlineSink) finish() []*segment {
	for len(k.links) > 0 {
		k.closeLink()
	}
	if len(k.segs) == 0 {
		k.segs = append(k.segs, &segment{})
	}
	return k.segs
}

// inlines builds the inline content of parent, a w:p or an element nested
// in one (17.3.2, 17.16, 17.13).
func (d *document) inlines(s *story, rp resolvedPara, ps *css.Style, lineMult float64, parent *ooxml.Element, sink *inlineSink, depth int) {
	if depth > maxNesting {
		return
	}
	for _, e := range parent.Kids() {
		switch {
		case e.Is(w, "r"):
			d.run(s, rp, ps, lineMult, e, sink, depth)
		case e.Is(w, "hyperlink"):
			href := d.hyperlink(s, e)
			sink.openLink(href)
			d.inlines(s, rp, ps, lineMult, e, sink, depth+1)
			sink.closeLink()
		case e.Is(w, "fldSimple"):
			d.simpleField(s, rp, ps, lineMult, e, sink, depth)
		case e.Is(w, "sdt"):
			d.inlines(s, rp, ps, lineMult, e.Child(w, "sdtContent"), sink, depth+1)
		case e.Is(w, "ins"), e.Is(w, "moveTo"), e.Is(w, "smartTag"), e.Is(w, "customXml"), e.Is(w, "dir"), e.Is(w, "bdo"):
			// Insertions print as the final text, as LibreOffice prints a
			// document with its changes shown (17.13.5).
			d.inlines(s, rp, ps, lineMult, e, sink, depth+1)
		case e.Is(w, "bookmarkStart"):
			name := attrOr(e, "name")
			if name == "" || name == "_GoBack" {
				continue
			}
			if !sink.anchor(name) {
				sink.add(&layout.Node{Kind: layout.Element, Style: inlineStyle(ps), ID: name})
			}
		case e.Is(ooxml.NSM, "oMath"), e.Is(ooxml.NSM, "oMathPara"):
			d.math(ps, e, sink)
		}
	}
}

// hyperlink returns the target of a w:hyperlink (17.16.22): its external
// relationship, else "#" and its anchor.
func (d *document) hyperlink(s *story, e *ooxml.Element) string {
	if id, ok := e.AttrNS(ooxml.NSR, "id"); ok {
		if rel, ok := d.pkg.RelationshipByID(s.part, id); ok && rel.External {
			href := rel.Target
			if a := attrOr(e, "anchor"); a != "" {
				href += "#" + a
			}
			return href
		}
	}
	if a := attrOr(e, "anchor"); a != "" {
		return "#" + a
	}
	return ""
}

// math writes an Office Math zone as its text, with a warning.
func (d *document) math(ps *css.Style, e *ooxml.Element, sink *inlineSink) {
	var b strings.Builder
	var walk func(*ooxml.Element, int)
	walk = func(x *ooxml.Element, depth int) {
		if depth > maxNesting {
			return
		}
		if x.Is(ooxml.NSM, "t") {
			b.WriteString(x.Text)
		}
		for _, c := range x.Kids() {
			walk(c, depth+1)
		}
	}
	walk(e, 0)
	if b.Len() == 0 {
		return
	}
	d.warn("unsupported-content", "an equation is drawn as its plain text")
	st := inlineStyle(ps)
	st.FontStyle = css.FontItalic
	st.FontFamilies = append([]string{"Cambria Math", "DejaVu Serif"}, st.FontFamilies...)
	sink.add(&layout.Node{Kind: layout.Element, Style: st, Children: []*layout.Node{{Kind: layout.Text, Style: st, Text: b.String()}}})
}

// run builds a w:r (17.3.2.25): its text, tabs, breaks, symbols, fields,
// note references and drawings, in the run's style.
func (d *document) run(s *story, rp resolvedPara, ps *css.Style, lineMult float64, r *ooxml.Element, sink *inlineSink, depth int) {
	d.nodes++
	if d.nodes > maxNodes {
		d.warn("too-large", fmt.Sprintf("the document builds more than %d nodes; the rest is left out", maxNodes))
		return
	}
	direct := d.parseRunProps(r.Child(w, "rPr"))
	props := rp.run
	if direct.style != "" {
		props = props.merge(ptr(d.styles.charStyle(direct.style)))
	}
	props = props.merge(direct)
	hidden := boolOf(props.vanish)
	st := inlineStyle(ps)
	d.applyRun(st, props, lineMult)
	var lang string
	if props.lang != nil {
		lang = *props.lang
	}
	el := &layout.Node{Kind: layout.Element, Style: st}
	var buf strings.Builder
	flush := func() {
		if buf.Len() > 0 {
			t := buf.String()
			if props.langEastAsia != nil && hasCJK(t) {
				el.Lang = *props.langEastAsia
			} else if el.Lang == "" {
				el.Lang = lang
			}
			el.Children = append(el.Children, &layout.Node{Kind: layout.Text, Style: st, Text: strings.ToValidUTF8(t, "�")})
			buf.Reset()
		}
	}
	emit := func() {
		flush()
		if len(el.Children) > 0 {
			sink.add(el)
			el = &layout.Node{Kind: layout.Element, Style: st}
		}
	}
	for _, c := range r.Kids() {
		switch {
		case c.Is(w, "fldChar"):
			emit()
			d.fieldChar(s, c, st, sink)
			continue
		case c.Is(w, "instrText"):
			if f := s.top(); f != nil && !f.result {
				f.instr.WriteString(c.Text)
			}
			continue
		}
		if hidden || s.hiding() {
			continue
		}
		switch {
		case c.Is(w, "t"):
			buf.WriteString(c.Text)
		case c.Is(w, "tab"), c.Is(w, "ptab"):
			buf.WriteByte('\t')
		case c.Is(w, "br"):
			switch attrOr(c, "type") {
			case "page", "column":
				emit()
				sink.pageBreak(attrOr(c, "type") == "column")
			default:
				flush()
				el.Children = append(el.Children, &layout.Node{Kind: layout.LineBreak, Style: st})
			}
		case c.Is(w, "cr"):
			flush()
			el.Children = append(el.Children, &layout.Node{Kind: layout.LineBreak, Style: st})
		case c.Is(w, "sym"):
			buf.WriteString(symbol(c))
		case c.Is(w, "noBreakHyphen"):
			buf.WriteRune('‑')
		case c.Is(w, "softHyphen"):
			buf.WriteRune('­')
		case c.Is(w, "footnoteReference"), c.Is(w, "endnoteReference"):
			emit()
			d.noteCall(s, c, st, sink)
		case c.Is(w, "footnoteRef"), c.Is(w, "endnoteRef"):
			// The number opening a note's text is raised unless its style
			// places it, as LibreOffice draws a note's anchor.
			emit()
			ns := st
			if props.vertAlign == nil && props.position == nil {
				ns = inlineStyle(st)
				d.applyRun(ns, props.merge(&runProps{vertAlign: ptr("superscript")}), lineMult)
			}
			sink.add(&layout.Node{Kind: layout.Element, Style: ns, Children: []*layout.Node{{Kind: layout.Text, Style: ns, Text: s.note}}})
		case c.Is(w, "drawing"):
			emit()
			d.drawing(s, c, st, sink, depth)
		case c.Is(w, "pict"), c.Is(w, "object"):
			emit()
			d.vml(s, c, st, sink, depth)
		}
	}
	emit()
}

// symbol returns the character of a w:sym (17.3.3.30): its char in its
// font, symbol fonts mapped to Unicode.
func symbol(c *ooxml.Element) string {
	v, err := strconv.ParseUint(attrOr(c, "char"), 16, 21)
	if err != nil || v == 0 || !utf8.ValidRune(rune(v)) { //nolint:gosec // at most 21 bits, parsed above
		return ""
	}
	s, _ := mapSymbolText(string(rune(v)), attrOr(c, "font")) //nolint:gosec // at most 21 bits, parsed above
	return s
}
