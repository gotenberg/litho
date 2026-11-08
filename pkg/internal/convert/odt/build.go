package odt

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/odf"
	"github.com/gotenberg/litho/pkg/internal/text"
)

// maxNodes bounds the layout nodes one document builds: 4,000,000.
// Anti-DoS guard, not spec-mandated: the XML caps bound the elements, but
// repeated table rows and columns multiply them; a 1,000-page report
// builds some 500,000 nodes.
const maxNodes = 4_000_000

// builder turns the body of a text document into a layout tree.
type builder struct {
	ctx    context.Context
	pkg    *odf.Package
	styles *odf.Styles
	scope  odf.Scope
	fonts  *text.Fonts
	// metricsCache holds the faces' ascents and descents, by family list,
	// weight and slant.
	metricsCache map[string][2]float64

	// floats holds the geometry of the frames [builder.place] floats
	// beside their paragraph's text, which a paragraph without text
	// places at their offsets instead.
	floats map[*layout.Node]frameGeom
	// chains maps the frames of chains of linked text boxes onto their
	// place in their chain, by frame name ([scanChains]).
	chains map[string]chainLink

	warnings []compose.Warning
	warned   map[string]bool
	headings []heading
	nextID   int
	nodes    int
	err      error

	lists *listState
	notes *noteState
	pics  map[string]picResult
	// pixels counts the decoded pictures' pixels against maxTotalPixels.
	pixels int64
	// pageOrigin is the first page body's top-left corner on its page,
	// which the offsets of a header's or footer's page-relative frames
	// are moved by.
	pageOrigin [2]float64
	pages      *pageFlow
	// hf is the header or footer being built, nil in the body: its page
	// number and count fields are computed.
	hf *hfFields
	// tabsRelative is settings.xml's TabsRelativeToIndent: tab positions
	// count from the paragraph's indent, Writer's default.
	tabsRelative bool
	// tocs are the tables of contents, regenerated once the layout
	// placed the headings.
	tocs []*toc
	// meta records the paragraph blocks' spacing properties.
	meta map[*layout.Node]paraMeta
}

// hfFields is the page a header or footer is built for.
type hfFields struct {
	number, count int
	format        string
}

// heading is a text:h of the content tree, for the outline.
type heading struct {
	node  *layout.Node
	level int
	// label is the outline number the heading draws first, text its
	// text: what a table of contents entry shows.
	label, text string
}

// warn records a warning once per code.
func (b *builder) warn(code, msg string) {
	if b.warned[code] {
		return
	}
	b.warned[code] = true
	b.warnings = append(b.warnings, compose.Warning{Code: "odt-" + code, Message: msg})
}

// node counts a new node against [maxNodes], recording the error.
func (b *builder) node(n *layout.Node) *layout.Node {
	b.nodes++
	if b.nodes > maxNodes && b.err == nil {
		b.err = fmt.Errorf("%w: more than %d layout nodes", office.ErrInputTooLarge, maxNodes)
	}
	if b.nodes%4096 == 0 && b.err == nil {
		b.err = b.ctx.Err()
	}
	return n
}

// newID returns a fresh anchor ID for a node the converter refers to.
func (b *builder) newID(kind string) string {
	b.nextID++
	return "odt-" + kind + "-" + strconv.Itoa(b.nextID)
}

// blockCtx is what a block knows of its container.
type blockCtx struct {
	// list is the list the block sits in, nil outside one.
	list *listCtx
	// item is the list item whose first paragraph takes its label, nil
	// outside a list or once the label is placed; indent is the item
	// whose level indents every paragraph of it.
	item, indent *itemCtx
	// defStyle is the paragraph style of a paragraph that names none: a
	// note's default style in a note body, "" elsewhere.
	defStyle string
	// depth is the nesting of the content, against odf.MaxDepth.
	depth int
}

// blocks builds the block content of el's children (ODF 1.3, Part 3, 5:
// the text body's paragraphs, headings, lists, tables, sections and
// indexes).
func (b *builder) blocks(el *odf.Element, ctx blockCtx) []*layout.Node {
	var out []*layout.Node
	if ctx.depth > odf.MaxDepth || b.err != nil {
		return nil
	}
	ctx.depth++
	for _, c := range el.Children {
		if b.err != nil {
			return out
		}
		out = append(out, b.block(c, &ctx)...)
	}
	b.spacing(out)
	return out
}

// paraMeta is what the spacing of sibling paragraphs needs to know of a
// paragraph block.
type paraMeta struct {
	style      string
	contextual bool
	join       bool
}

// commonStyle returns the common paragraph style of the style name: the
// parent of an automatic style, which carries a paragraph's direct
// formatting, else name. Writer's contextual spacing compares these
// (ODF 1.3, Part 3, 16.2: an automatic style is the formatting of the
// paragraph, not a style of its own).
func (b *builder) commonStyle(name string) string {
	if st := b.styles.Style(b.scope, odf.FamilyParagraph, name); st != nil && st.Auto && st.Parent != "" {
		return st.Parent
	}
	return name
}

// spacing applies Writer's vertical spacing to the sibling blocks kids:
// the space between two paragraphs is the bottom margin of the first plus
// the top margin of the second, not the larger of them as CSS collapses
// margins (Writer's default for its own documents, the ParaSpaceMax
// compatibility option off), none of it when contextual spacing is on
// for two paragraphs of one style (ODF 1.3, Part 3, 20.315); and two
// paragraphs of the same borders and padding that join their borders
// draw one box (20.318, style:join-border). The bottom margin moves onto
// the next sibling's top, where CSS cannot collapse it.
func (b *builder) spacing(kids []*layout.Node) {
	for i := 1; i < len(kids); i++ {
		prev, cur := kids[i-1], kids[i]
		if !inFlowBlock(prev) || !inFlowBlock(cur) {
			continue
		}
		pb, ct := prev.Style.Margin[css.Bottom], cur.Style.Margin[css.Top]
		if pb.Kind != css.Points || ct.Kind != css.Points {
			continue
		}
		pm, pok := b.meta[prev]
		cm, cok := b.meta[cur]
		same := pok && cok && pm.style == cm.style
		bottom, top := pb.Value, ct.Value
		if same && pm.contextual {
			bottom = 0
		}
		if same && cm.contextual {
			top = 0
		}
		if forced(cur.Style.BreakBefore) || forced(prev.Style.BreakAfter) {
			// The bottom margin ends the page before a forced break;
			// Writer keeps the top margin of the paragraph that starts the
			// next.
			bottom = 0
		}
		ps, cs := *prev.Style, *cur.Style
		if pok && cok && pm.join && cm.join && joinable(&ps, &cs) {
			ps.Border[css.Bottom], cs.Border[css.Top] = css.Border{}, css.Border{}
			ps.Padding[css.Bottom], cs.Padding[css.Top] = css.Pts(0), css.Pts(0)
		}
		ps.Margin[css.Bottom] = css.Pts(0)
		cs.Margin[css.Top] = css.Pts(top + bottom)
		prev.Style, cur.Style = &ps, &cs
	}
}

// forced reports a forced page or column break.
func forced(br css.Break) bool {
	return br == css.Page || br == css.LeftPage || br == css.RightPage || br == css.Column
}

// inFlowBlock reports a block box in the normal flow.
func inFlowBlock(n *layout.Node) bool {
	st := n.Style
	return n.Kind == layout.Element && st.Float == css.FloatNone && st.Position == css.Static &&
		(st.Display == css.Block || st.Display == css.ListItem || st.Display == css.Table)
}

// joinable reports two paragraphs whose borders join: borders drawn, of
// equal sides, padding and side margins.
func joinable(a, c *css.Style) bool {
	return hasBorder(a.Border) && a.Border == c.Border && a.Padding == c.Padding &&
		a.Margin[css.Left] == c.Margin[css.Left] && a.Margin[css.Right] == c.Margin[css.Right]
}

// block builds one child of a text body.
func (b *builder) block(c *odf.Element, ctx *blockCtx) []*layout.Node {
	switch c.Name.Space {
	case odf.NSText:
		switch c.Name.Local {
		case "p", "h":
			n := b.paragraph(c, c.Name.Local == "h", ctx)
			if n == nil {
				return nil
			}
			return []*layout.Node{n}
		case "list":
			return b.list(c, ctx)
		case "numbered-paragraph":
			return b.numberedParagraph(c, ctx)
		case "section":
			return b.section(c, *ctx)
		case "table-of-content":
			return b.tableOfContent(c, *ctx)
		case "alphabetical-index", "illustration-index",
			"table-index", "object-index", "user-index", "bibliography":
			return b.blocks(c.Child(odf.NSText, "index-body"), *ctx)
		case "index-title":
			return b.blocks(c, *ctx)
		case "tracked-changes", "sequence-decls", "variable-decls", "user-field-decls",
			"dde-connection-decls", "alphabetical-index-auto-mark-file", "soft-page-break":
			return nil
		case "change", "change-start", "change-end", "bookmark", "bookmark-start", "bookmark-end":
			// Change marks and bookmarks between paragraphs carry no text.
			return nil
		}
	case odf.NSTable:
		if c.Name.Local == "table" {
			return b.table(c, *ctx)
		}
	case odf.NSDraw:
		// A frame anchored to the page or a paragraph sits among the body's
		// blocks.
		return b.blockFrame(c, *ctx)
	case odf.NSOffice:
		if c.Name.Local == "annotation" || c.Name.Local == "forms" {
			return nil
		}
	}
	if !c.IsText() && c.Name.Local != "" {
		b.warn("unknown-"+c.Name.Local, fmt.Sprintf("the element %s is not drawn", c.Name.Local))
	}
	return nil
}

// paraStyleName returns the paragraph style a text:p or text:h names,
// the default paragraph style "Standard" when it names none, as Writer
// reads it.
func (b *builder) paraStyleName(el *odf.Element) string {
	if n := el.A(odf.NSText, "style-name"); n != "" {
		return n
	}
	return "Standard"
}

// paraText returns the text properties of the paragraph style name.
func (b *builder) paraText(name string) textState {
	t := textState{size: defaultFontSize, weight: 400}
	for _, l := range b.styles.Layers(b.scope, odf.FamilyParagraph, name, odf.TextProps) {
		t = b.foldText(t, l)
	}
	for _, l := range b.builtinLayers(name, odf.TextProps) {
		t = b.foldText(t, l)
	}
	return t
}

// paraProps returns the paragraph properties of the paragraph style name.
func (b *builder) paraProps(name string) paraState {
	p := paraState{joinBorder: true}
	for _, l := range b.styles.Layers(b.scope, odf.FamilyParagraph, name, odf.ParagraphProps) {
		p = foldPara(p, l)
	}
	for _, l := range b.builtinLayers(name, odf.ParagraphProps) {
		p = foldPara(p, l)
	}
	return p
}

// paragraph builds a text:p or text:h (ODF 1.3, Part 3, 5.1): a block of
// its paragraph style holding its inline content, a heading's outline
// level giving it its structure role, a list item's label first.
func (b *builder) paragraph(el *odf.Element, isHeading bool, ctx *blockCtx) *layout.Node {
	name := b.paraStyleName(el)
	if ctx.defStyle != "" && el.A(odf.NSText, "style-name") == "" {
		name = ctx.defStyle
	}
	if isHeading && el.A(odf.NSText, "style-name") == "" {
		// Writer gives a heading without a style the heading style of
		// its outline level.
		name = "Heading_20_" + strconv.Itoa(max(b.outlineLevel(el, name), 1))
	}
	t := b.paraText(name)
	p := b.paraProps(name)
	if ctx.indent != nil {
		p = ctx.indent.indent(p, b.styles.Style(b.scope, odf.FamilyParagraph, name))
	}
	st := baseStyle()
	applyText(st, t, false)
	b.applyPara(st, p, t)
	if p.dropCap != nil {
		b.warn("drop-cap", "drop caps are drawn as the paragraph's first letters in line")
	}
	n := b.node(&layout.Node{Kind: layout.Element, Style: st, Role: "P", Lang: t.langTag()})
	n.ID = b.paragraphID(el)
	b.meta[n] = paraMeta{style: b.commonStyle(name), contextual: p.contextual, join: p.joinBorder}
	ws := &wsState{space: true}
	var kids []*layout.Node
	b.inline(el, t, &kids, ws, ctx.depth)
	if !hasText(kids) && b.floatCount(kids) > 1 {
		b.pinFloats(kids, p)
	}
	if visible(kids) == 0 {
		kids = append(kids, b.node(&layout.Node{Kind: layout.LineBreak, Style: st}))
	} else if last := kids[len(kids)-1]; last.Kind == layout.LineBreak {
		// A line break that ends the paragraph opens an empty last line,
		// as Writer draws it.
		kids = append(kids, b.node(&layout.Node{Kind: layout.LineBreak, Style: st}))
	}
	n.Children = kids
	for _, k := range kids {
		if k.Style.Position == css.Absolute && k.Kind != layout.Text {
			// A run-through frame anchored to the paragraph is placed from
			// the paragraph's corner: the paragraph is its containing
			// block.
			st.Position = css.Relative
			break
		}
	}
	if isHeading {
		level := b.outlineLevel(el, name)
		if level > 0 {
			n.Role = "H" + strconv.Itoa(min(level, 6))
			if n.ID == "" {
				n.ID = b.newID("h")
			}
			h := heading{node: n, level: level, text: headingTitle(n)}
			// A heading that is a list header takes no number and leaves
			// the outline counters as they are (ODF 1.3, Part 3, 5.1.2
			// text:is-list-header).
			if el.A(odf.NSText, "is-list-header") != "true" {
				if lbl := b.outlineLabel(level); lbl != "" {
					n.Children = append([]*layout.Node{b.run(lbl, t)}, n.Children...)
					h.label = lbl
				}
			}
			b.headings = append(b.headings, h)
		}
	}
	if ctx.item != nil {
		ctx.item.attach(b, n, t)
		ctx.item = nil
	}
	return n
}

// paragraphID returns the xml:id of a paragraph, which text:a and
// bookmark references may name, "" for none.
func (b *builder) paragraphID(el *odf.Element) string {
	return el.A(odf.NSXML, "id")
}

// outlineLevel returns a heading's outline level: text:outline-level,
// else its style's style:default-outline-level, else 1 (ODF 1.3, Part 3,
// 19.844.4: a text:h without a level is at level 1).
func (b *builder) outlineLevel(el *odf.Element, style string) int {
	if lv, err := strconv.Atoi(el.A(odf.NSText, "outline-level")); err == nil {
		return max(min(lv, odf.MaxListLevels), 0)
	}
	v := b.styles.Inherited(b.scope, odf.FamilyParagraph, style, func(s *odf.Style) (string, bool) {
		if s.OutlineLevel > 0 {
			return strconv.Itoa(s.OutlineLevel), true
		}
		return "", false
	})
	if lv, err := strconv.Atoi(v); err == nil {
		return lv
	}
	return 1
}

// visible counts the nodes of kids that take room on the paragraph's
// lines: everything but the empty anchors bookmarks leave and the
// positioned and floating frames, beside which Writer keeps the
// paragraph's empty line.
func visible(kids []*layout.Node) int {
	n := 0
	for _, k := range kids {
		if k.Style != nil && (k.Style.Position == css.Absolute || k.Style.Float != css.FloatNone) {
			continue
		}
		if k.Kind != layout.Element || len(k.Children) > 0 || k.Footnote != nil || k.Style.Display != css.Inline {
			n++
		}
	}
	return n
}

// wsState is the white-space processing of one paragraph (ODF 1.3, Part
// 3, 6.1.2): space reports that the last character emitted is a space, or
// that the paragraph has not started, so the next white space collapses
// away.
type wsState struct {
	space bool
}

// collapse returns s with its white space collapsed as ODF 1.3, Part 3,
// 6.1.2 asks: each run of space, tab, carriage return and line feed is
// one space, none at the start of the paragraph.
func (ws *wsState) collapse(s string) string {
	var sb strings.Builder
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			if !ws.space {
				sb.WriteByte(' ')
				ws.space = true
			}
			continue
		}
		sb.WriteRune(r)
		ws.space = false
	}
	return sb.String()
}

// spanText returns t with the text properties of the text style name laid
// over it, its chain from the farthest (the text family has no default
// style that would reset the paragraph's).
func (b *builder) spanText(t textState, name string) textState {
	for _, st := range b.styles.Chain(b.scope, odf.FamilyText, name) {
		t = b.foldText(t, st.Props(odf.TextProps))
	}
	return t
}

// run returns the inline node of the text s in the text properties t.
func (b *builder) run(s string, t textState) *layout.Node {
	if t.outline {
		b.warn("text-outline", "outlined text is drawn filled")
	}
	if t.relief != "" {
		b.warn("text-relief", "embossed and engraved text is approximated by a grey shadow")
	}
	st := baseStyle()
	applyText(st, t, true)
	if t.auto != 0 {
		st.VerticalAlign = css.VerticalAlign{Kind: css.AlignLength, Length: css.Pts(b.raise(t))}
		// Automatic escapement keeps the run inside the full-size line:
		// Writer's line does not grow for it, so its inline box takes no
		// height.
		st.LineHeight = css.Pts(0)
	}
	n := b.node(&layout.Node{Kind: layout.Element, Style: st, Lang: t.langTag()})
	n.Children = []*layout.Node{b.node(&layout.Node{Kind: layout.Text, Style: st, Text: s})}
	return n
}

// inline appends the inline content of el's children, in the text
// properties t, to out (ODF 1.3, Part 3, 6).
func (b *builder) inline(el *odf.Element, t textState, out *[]*layout.Node, ws *wsState, depth int) {
	if depth > odf.MaxDepth || b.err != nil {
		return
	}
	depth++
	for _, c := range el.Children {
		if c.IsText() {
			if t.hidden {
				continue
			}
			if s := ws.collapse(c.Text); s != "" {
				*out = append(*out, b.run(s, t))
			}
			continue
		}
		b.inlineElement(c, t, out, ws, depth)
	}
}

// inlineElement appends one inline element.
func (b *builder) inlineElement(c *odf.Element, t textState, out *[]*layout.Node, ws *wsState, depth int) {
	switch c.Name.Space {
	case odf.NSText:
		switch c.Name.Local {
		case "span":
			b.inline(c, b.spanText(t, c.A(odf.NSText, "style-name")), out, ws, depth)
		case "s":
			count := 1
			if v, err := strconv.Atoi(c.A(odf.NSText, "c")); err == nil && v > 1 {
				count = min(v, maxSpaces)
			}
			if !t.hidden {
				*out = append(*out, b.run(strings.Repeat(" ", count), t))
			}
			ws.space = false
		case "tab":
			if !t.hidden {
				*out = append(*out, b.run("\t", t))
			}
			ws.space = false
		case "line-break":
			st := baseStyle()
			applyText(st, t, true)
			*out = append(*out, b.node(&layout.Node{Kind: layout.LineBreak, Style: st}))
			ws.space = true
		case "a":
			b.link(c, t, out, ws, depth)
		case "note":
			b.note(c, t, out, ws, depth)
		case "bookmark", "bookmark-start", "reference-mark", "reference-mark-start":
			if name := c.A(odf.NSText, "name"); name != "" {
				*out = append(*out, b.anchor(name))
			}
		case "bookmark-end", "reference-mark-end", "soft-page-break", "toc-mark", "toc-mark-start",
			"toc-mark-end", "alphabetical-index-mark", "alphabetical-index-mark-start",
			"alphabetical-index-mark-end", "user-index-mark", "user-index-mark-start",
			"user-index-mark-end", "change-end", "change":
		case "change-start":
			b.changeStart(c)
		case "page-number", "page-count":
			b.pageField(c, t, out, ws)
		case "hidden-text":
			if c.A(odf.NSText, "is-hidden") != "true" {
				b.inline(c, t, out, ws, depth)
			}
		case "hidden-paragraph":
		case "ruby":
			b.warn("ruby", "ruby annotations are left out; the base text is drawn")
			b.inline(c.Child(odf.NSText, "ruby-base"), t, out, ws, depth)
		case "conditional-text":
			b.inline(c, t, out, ws, depth)
		default:
			// Every other field, text:date, text:author-name, text:title,
			// text:chapter, variables, sequences, references and the
			// rest, shows the content the document saved (ODF 1.3, Part
			// 3, 7).
			b.inline(c, t, out, ws, depth)
		}
	case odf.NSDraw:
		b.inlineFrame(c, out, ws, depth)
	case odf.NSOffice:
		// office:annotation and office:annotation-end are not printed.
	case odf.NSTable, odf.NSForm:
	default:
		b.inline(c, t, out, ws, depth)
	}
}

// maxSpaces bounds the spaces one text:s inserts: 10,000. Anti-DoS guard,
// not spec-mandated: a page line holds a few hundred.
const maxSpaces = 10_000

// anchor returns an empty inline node carrying the link target name.
func (b *builder) anchor(name string) *layout.Node {
	st := baseStyle()
	return b.node(&layout.Node{Kind: layout.Element, Style: st, ID: name})
}

// link appends a text:a (ODF 1.3, Part 3, 6.1.8): its content as a link
// to xlink:href, a "#name" reference staying inside the document.
func (b *builder) link(c *odf.Element, t textState, out *[]*layout.Node, ws *wsState, depth int) {
	href := c.A(odf.NSXLink, "href")
	t = b.spanText(t, c.A(odf.NSText, "style-name"))
	var kids []*layout.Node
	b.inline(c, t, &kids, ws, depth)
	if href == "" {
		*out = append(*out, kids...)
		return
	}
	if strings.HasPrefix(href, "#") {
		// Writer's references to headings and frames name them
		// "#name|outline"; the name is the target.
		if i := strings.LastIndexByte(href, '|'); i > 0 {
			href = href[:i]
		}
	}
	st := baseStyle()
	n := b.node(&layout.Node{Kind: layout.Element, Style: st, Href: href, Role: "Link", Children: kids})
	*out = append(*out, n)
}

// changeStart notes a tracked change in the text: Writer prints the
// document with its changes as recorded, the deletions out of the text
// (they live in text:tracked-changes) and the insertions in it.
func (b *builder) changeStart(*odf.Element) {
	b.warn("tracked-changes", "tracked changes print as the accepted text, without change marks")
}

// pageField appends a text:page-number or text:page-count (ODF 1.3, Part
// 3, 7.3.4 and 7.5.18): in a header or footer the number of the page it
// is drawn on, in the page layout's number format, else the content the
// document saved.
func (b *builder) pageField(c *odf.Element, t textState, out *[]*layout.Node, ws *wsState) {
	if b.hf == nil {
		b.inline(c, t, out, ws, 0)
		return
	}
	n := b.hf.number
	if c.Name.Local == "page-count" {
		n = b.hf.count
	} else if adj, err := strconv.Atoi(c.A(odf.NSText, "page-adjust")); err == nil {
		n += adj
	}
	format := c.A(odf.NSStyle, "num-format")
	if format == "" {
		format = b.hf.format
	}
	if c.Name.Local == "page-count" && c.A(odf.NSStyle, "num-format") == "" {
		format = "1"
	}
	*out = append(*out, b.run(formatNumber(n, format), t))
	ws.space = false
}

// section builds a text:section (ODF 1.3, Part 3, 5.4.1): a block of its
// section style, its columns, background and margins, left out when
// text:display is none.
func (b *builder) section(c *odf.Element, ctx blockCtx) []*layout.Node {
	if c.A(odf.NSText, "display") == "none" || c.A(odf.NSText, "condition") != "" && c.A(odf.NSText, "is-hidden") == "true" {
		return nil
	}
	st := baseStyle()
	st.Display = css.Block
	props := b.styles.Resolve(b.scope, odf.FamilySection, c.A(odf.NSText, "style-name"), odf.SectionProps)
	if v := props.Get("fo:background-color"); v != "" {
		if col, ok := odf.ParseColor(v); ok {
			st.BackgroundColor = cssColor(col)
		}
	}
	for i, s := range []string{"fo:margin-top", "fo:margin-right", "fo:margin-bottom", "fo:margin-left"} {
		if pt, ok := odf.Length(props.Get(s)); ok {
			st.Margin[i] = css.Pts(pt)
		}
	}
	b.columns(st, props.Child("style:columns"), b.currentAreaWidth()-st.Margin[css.Left].Value-st.Margin[css.Right].Value)
	n := b.node(&layout.Node{Kind: layout.Element, Style: st, Role: "Sect", ID: c.A(odf.NSText, "name")})
	n.Children = b.blocks(c, ctx)
	return []*layout.Node{n}
}

// columns sets the style:columns of a section or page (ODF 1.3, Part 3,
// 17.12): the count, the gap and the separator; columns of relative
// widths are laid out equal.
func (b *builder) columns(st *css.Style, cols *odf.Element, width float64) {
	if cols == nil {
		return
	}
	count, err := strconv.Atoi(cols.A(odf.NSFO, "column-count"))
	if err != nil || count < 2 {
		return
	}
	st.Columns.Count = min(count, maxColumns)
	st.Columns.Width = css.Dimension{Kind: css.Auto}
	gap, _ := odf.Length(cols.A(odf.NSFO, "column-gap"))
	kids := cols.All(odf.NSStyle, "column")
	if len(kids) > 0 {
		// Each style:column carries its own spacing: the gap is the end
		// indent of one plus the start indent of the next.
		end, _ := odf.Length(kids[0].A(odf.NSFO, "end-indent"))
		start := 0.0
		if len(kids) > 1 {
			start, _ = odf.Length(kids[1].A(odf.NSFO, "start-indent"))
		}
		if end+start > 0 {
			gap = end + start
		}
		if tracks, first, last, ok := b.columnTracks(kids, width); ok {
			applyColumnTracks(st, tracks, first, last)
		}
	}
	st.ColumnGap = css.Pts(max(gap, 0))
	if sep := cols.Child(odf.NSStyle, "column-sep"); sep != nil && sep.A(odf.NSStyle, "style") != "none" {
		w, ok := odf.Length(sep.A(odf.NSStyle, "width"))
		if !ok {
			w = 0.75
		}
		st.Columns.RuleWidth = w
		st.Columns.RuleStyle = css.Solid
		switch sep.A(odf.NSStyle, "style") {
		case "dotted":
			st.Columns.RuleStyle = css.Dotted
		case "dashed":
			st.Columns.RuleStyle = css.Dashed
		}
		st.Columns.RuleColor = css.Color{A: 1}
		if c, ok := odf.ParseColor(sep.A(odf.NSStyle, "color")); ok {
			st.Columns.RuleColor = cssColor(c)
		}
	}
}

// currentAreaWidth returns the width of the page area of the master page
// the body being built is on: the content width of what fills it. A
// section in a frame or a table cell is narrower; the column tracks are
// shares of the width, which the engine lays out on the container it
// finds.
func (b *builder) currentAreaWidth() float64 {
	pg := b.currentGeom()
	return pg.box.Width - pg.box.Margin[css.Left] - pg.box.Margin[css.Right]
}

// columnTracks returns the columns kids, the style:column elements of a
// style:columns (ODF 1.3, Part 3, 17.13), in a container whose content is
// width wide: each column takes its style:rel-width share of the width
// less its fo:start-indent and fo:end-indent, the gap after it being its
// end indent and the next one's start indent; first and last are the
// first column's start indent and the last one's end indent. ok is false
// when a column's share does not read.
func (b *builder) columnTracks(kids []*odf.Element, width float64) (tracks []columnTrack, first, last float64, ok bool) {
	rel := make([]float64, len(kids))
	sum := 0.0
	for i, k := range kids {
		v, err := strconv.ParseFloat(strings.TrimSuffix(k.A(odf.NSStyle, "rel-width"), "*"), 64)
		if err != nil || v < 0 || math.IsInf(v, 0) {
			return nil, 0, 0, false
		}
		rel[i] = v
		sum += v
	}
	if sum <= 0 {
		return nil, 0, 0, false
	}
	starts, ends := make([]float64, len(kids)), make([]float64, len(kids))
	for i, k := range kids {
		starts[i], _ = odf.Length(k.A(odf.NSFO, "start-indent"))
		ends[i], _ = odf.Length(k.A(odf.NSFO, "end-indent"))
	}
	for i := range kids {
		t := columnTrack{Width: max(rel[i]/sum*width-starts[i]-ends[i], 0)}
		if i+1 < len(kids) {
			t.Gap = ends[i] + starts[i+1]
		}
		tracks = append(tracks, t)
	}
	return tracks, starts[0], ends[len(ends)-1], true
}

// maxColumns bounds the columns of a section or page: 99, Writer's own
// limit.
const maxColumns = 99
