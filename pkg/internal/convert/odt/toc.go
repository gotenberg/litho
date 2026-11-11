package odt

import (
	"encoding/xml"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/odf"
)

// maxIndexPasses bounds the layouts a document with a table of contents
// takes: 3. Anti-DoS guard, not spec-mandated: the second layout numbers
// the entries, a third one settles a table that moved its own headings
// to other pages, and a table that keeps moving them is left as the last
// pass drew it.
const maxIndexPasses = 3

// toc is a table of contents of the body, regenerated from the
// document's headings before printing, as Writer does when asked to
// update its indexes.
type toc struct {
	node *layout.Node
	src  *odf.Element
	ctx  blockCtx
	// sig is the entries the node holds, to tell a pass that changed
	// nothing.
	sig string
}

// tableOfContent builds a text:table-of-content (ODF 1.3, Part 3, 8.3):
// its saved index body, which [builder.updateTOCs] replaces once the
// headings' pages are known.
func (b *builder) tableOfContent(c *odf.Element, ctx blockCtx) []*layout.Node {
	st := baseStyle()
	st.Display = css.Block
	n := b.node(&layout.Node{Kind: layout.Element, Style: st, Role: "TOC"})
	n.Children = b.blocks(c.Child(odf.NSText, "index-body"), ctx)
	for _, k := range n.Children {
		tocRole(k, "TOCI")
	}
	b.tocs = append(b.tocs, &toc{node: n, src: c.Child(odf.NSText, "table-of-content-source"), ctx: ctx})
	return []*layout.Node{n}
}

// tocRole tags n, a paragraph of a table of contents, as role: an entry
// is a TOCI and the title a Caption, as a TOC holds only TOC, TOCI and
// Caption elements (ISO 32000-1, 14.8.4.3.2, Table 333; ISO 14289-1,
// 7.2). A node of another role, such as a section, keeps it.
func tocRole(n *layout.Node, role string) {
	if n.Role == "P" || (len(n.Role) == 2 && n.Role[0] == 'H') {
		n.Role = role
	}
}

// headingPages returns the page number each heading landed on, in the
// number format of its page, "" for one that landed nowhere.
func (b *builder) headingPages(res *layout.Result) []string {
	out := make([]string, len(b.headings))
	for i, h := range b.headings {
		d, ok := res.Anchors[h.node.ID]
		if !ok || d.Page < 1 || d.Page > len(b.pages.entries) {
			continue
		}
		e := b.pages.entries[d.Page-1]
		format := "1"
		if e.geom != nil {
			format = e.geom.format
		}
		out[i] = formatNumber(e.number, format)
	}
	return out
}

// updateTOCs rebuilds every table of contents from the headings and the
// pages the layout res put them on, as Writer's index update does (ODF
// 1.3, Part 3, 8.3.2 and 8.6): the title of its title template, then one
// paragraph per heading of an outline level the source takes, laid out
// by the entry template of its level. It reports whether any table
// changed.
func (b *builder) updateTOCs(res *layout.Result) bool {
	pages := b.headingPages(res)
	changed := false
	for _, t := range b.tocs {
		kids, sig := b.tocBody(t, pages)
		if sig == t.sig {
			continue
		}
		t.sig = sig
		t.node.Children = kids
		changed = true
	}
	return changed
}

// tocBody returns the paragraphs of the table t and their signature.
func (b *builder) tocBody(t *toc, pages []string) ([]*layout.Node, string) {
	maxLevel := odf.MaxListLevels
	if lv, err := strconv.Atoi(t.src.A(odf.NSText, "outline-level")); err == nil && lv > 0 {
		maxLevel = lv
	}
	var sig strings.Builder
	var paras []*odf.Element
	// Without a title template Writer titles the table "Table of
	// Contents" in its "Contents Heading" style.
	title, titleStyle := "Table of Contents", "Contents_20_Heading"
	if tt := t.src.Child(odf.NSText, "index-title-template"); tt != nil {
		title, titleStyle = strings.TrimSpace(tt.TextContent()), tt.A(odf.NSText, "style-name")
	}
	if title != "" {
		paras = append(paras, para(titleStyle, &odf.Element{Text: title}))
		sig.WriteString(title)
	}
	templates := map[int]*odf.Element{}
	for _, tm := range t.src.All(odf.NSText, "table-of-content-entry-template") {
		if lv, err := strconv.Atoi(tm.A(odf.NSText, "outline-level")); err == nil {
			templates[lv] = tm
		}
	}
	useOutline := t.src.A(odf.NSText, "use-outline-level") != "false"
	for i, h := range b.headings {
		if !useOutline || h.level > maxLevel {
			continue
		}
		tm := templates[h.level]
		style := "Contents_20_" + strconv.Itoa(h.level)
		if tm != nil && tm.A(odf.NSText, "style-name") != "" {
			style = tm.A(odf.NSText, "style-name")
		}
		kids := b.tocEntry(tm, h, pages[i])
		paras = append(paras, para(style, kids...))
		sig.WriteString("\x00" + h.label + h.text + "\x01" + pages[i])
	}
	ctx := t.ctx
	out := make([]*layout.Node, 0, len(paras))
	for i, p := range paras {
		role := "TOCI"
		if i == 0 && title != "" {
			role = "Caption"
		}
		for _, k := range b.block(p, &ctx) {
			tocRole(k, role)
			out = append(out, k)
		}
	}
	b.spacing(out)
	b.rightTabs(out)
	return out, sig.String()
}

// para returns a text:p of the style name holding kids.
func para(style string, kids ...*odf.Element) *odf.Element {
	p := &odf.Element{Name: xmlName(odf.NSText, "p"), Children: kids}
	if style != "" {
		p.Attr = append(p.Attr, xmlAttr(odf.NSText, "style-name", style))
	}
	return p
}

// tocEntry returns the content of the entry of heading h on page from the
// entry template tm (ODF 1.3, Part 3, 8.6.4 to 8.6.10): the chapter
// number, the text, the tab stop, the page number and the literal spans,
// linked to the heading between the link start and end; nil tm takes
// Writer's default, chapter, text, a right tab with dots, page.
func (b *builder) tocEntry(tm *odf.Element, h heading, page string) []*odf.Element {
	parts := []*odf.Element{
		{Name: xmlName(odf.NSText, "index-entry-link-start")},
		{Name: xmlName(odf.NSText, "index-entry-chapter")},
		{Name: xmlName(odf.NSText, "index-entry-text")},
		{Name: xmlName(odf.NSText, "index-entry-tab-stop"), Attr: []xml.Attr{xmlAttr(odf.NSStyle, "type", "right"), xmlAttr(odf.NSStyle, "leader-char", ".")}},
		{Name: xmlName(odf.NSText, "index-entry-page-number")},
		{Name: xmlName(odf.NSText, "index-entry-link-end")},
	}
	if tm != nil {
		parts = tm.Children
	}
	var out []*odf.Element
	var link *odf.Element
	add := func(e *odf.Element) {
		if link != nil {
			link.Children = append(link.Children, e)
		} else {
			out = append(out, e)
		}
	}
	span := func(style, s string) *odf.Element {
		if s == "" {
			return nil
		}
		el := &odf.Element{Name: xmlName(odf.NSText, "span"), Children: []*odf.Element{{Text: s}}}
		if style != "" {
			el.Attr = append(el.Attr, xmlAttr(odf.NSText, "style-name", style))
		}
		return el
	}
	for _, p := range parts {
		if p.IsText() || p.Name.Space != odf.NSText {
			continue
		}
		var el *odf.Element
		style := p.A(odf.NSText, "style-name")
		switch p.Name.Local {
		case "index-entry-link-start":
			link = &odf.Element{Name: xmlName(odf.NSText, "a"), Attr: []xml.Attr{xmlAttr(odf.NSXLink, "href", "#"+h.node.ID)}}
			if style != "" {
				link.Attr = append(link.Attr, xmlAttr(odf.NSText, "style-name", style))
			}
			out = append(out, link)
		case "index-entry-link-end":
			link = nil
		case "index-entry-chapter":
			el = span(style, strings.TrimRight(h.label, "\t"))
		case "index-entry-text":
			el = span(style, h.text)
		case "index-entry-page-number":
			el = span(style, page)
		case "index-entry-span":
			el = span(style, p.TextContent())
		case "index-entry-tab-stop":
			el = &odf.Element{Name: xmlName(odf.NSText, "tab")}
		}
		if el != nil {
			add(el)
		}
	}
	return out
}

// rightTabs gives each entry paragraph of kids a right tab stop at its
// end edge, dotted, for the tab Writer sets there: the page area's width
// less the paragraph's end margin, from the start of its containing
// block.
func (b *builder) rightTabs(kids []*layout.Node) {
	area := b.areaWidth()
	for _, n := range kids {
		if !hasRightTab(n) {
			continue
		}
		st := *n.Style
		pos := area - st.Margin[css.Right].Value
		stops := []css.TabStop{}
		for _, s := range st.TabStops {
			if s.Pos < pos {
				stops = append(stops, s)
			}
		}
		stops = append(stops, css.TabStop{Pos: pos, Align: css.TabRight, Leader: css.TabLeaderDots})
		st.TabStops = stops
		n.Style = &st
	}
}

// hasRightTab reports a paragraph built from an entry with a right tab:
// one whose content holds a tab character.
func hasRightTab(n *layout.Node) bool {
	stack := []*layout.Node{n}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if cur.Kind == layout.Text && strings.Contains(cur.Text, "\t") {
			return true
		}
		stack = append(stack, cur.Children...)
	}
	return false
}

// areaWidth returns the width of the first page's area.
func (b *builder) areaWidth() float64 {
	if len(b.pages.instances) == 0 {
		return defaultPageWidth - 2*defaultPageMargin
	}
	g := b.pages.geom(b.pages.instances[0].master, false, true)
	return g.box.Width - g.box.Margin[css.Left] - g.box.Margin[css.Right]
}
