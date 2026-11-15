package odt

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/gotenberg/litho/pkg/internal/convert/formfield"
	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/formula"
	"github.com/gotenberg/litho/pkg/internal/intconv"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/odf"
	"github.com/gotenberg/litho/pkg/internal/symbolfont"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// maxNodes bounds the layout nodes one document builds: 4,000,000.
// Anti-DoS guard, not spec-mandated: the XML caps bound the elements, but
// repeated table rows and columns multiply them; a 1,000-page report
// builds some 500,000 nodes.
const maxNodes = 4_000_000

// nodeCheckEvery is how many layout nodes the builder makes between two
// checks of its context.
const nodeCheckEvery = 4096

// builder turns the body of a text document into a layout tree.
type builder struct {
	// ctx is the context of the Convert call the builder serves: a
	// builder does not outlive it.
	ctx    context.Context
	pkg    *odf.Package
	styles *odf.Styles
	// shared holds the styles of the tree's nodes: paragraphs, runs and
	// table parts share theirs from the moment the node is made
	// ([builder.share]), the other nodes once the tree is finished
	// ([layout.ShareStyles]). A shared style is written no more.
	shared *css.Styles
	scope  odf.Scope
	fonts  *text.Fonts
	// symbols maps what the symbol runs and bullets draw back to their
	// codes.
	symbols *office.SymbolCodes
	// metricsCache holds the faces' ascents and descents, by family list,
	// weight and slant.
	metricsCache map[string][2]float64

	// floats holds the geometry of the frames [builder.place] floats
	// beside their paragraph's text, which a paragraph without text
	// places at their offsets instead.
	floats map[*layout.Node]frameGeom
	// topFrames holds the frames [builder.place] stands at the top of
	// their paragraph, the paragraph's text below them
	// ([builder.frameFirst]).
	topFrames map[*layout.Node]bool
	// chains maps the frames of chains of linked text boxes onto their
	// place in their chain, by frame name ([scanChains]).
	chains map[string]chainLink
	// frameNames holds the place and size of the first frame of the body
	// by each draw:name, lastFrame the name of the frame read last since
	// the last element of a text body that is no frame
	// ([builder.duplicateFrame]).
	frameNames map[string][4]int64
	lastFrame  string
	// data reads the data styles of table cells' values, built on first
	// use ([builder.cellValueText]).
	data *odf.DataStyles

	warnings []typeset.Warning
	warned   map[string]bool
	headings []heading
	nextID   int
	nodes    int
	err      error

	lists *listState
	notes *noteState
	pics  map[string]picResult
	// formulas lays the document's formulas out, made at the first.
	formulas *formula.Setter
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
	// absoluteAlign reads fo:text-align start and end as left and right
	// in every writing mode ([odf.AbsoluteStartEnd]).
	absoluteAlign bool
	// changeMarks is how the marked view draws each tracked change, by
	// id, nil for the other views; openChanges are the ids of the
	// changes open where the builder is, innermost last.
	changeMarks map[string]changeMark
	openChanges []string
	// date is the conversion date, which the date and time fields that
	// are not fixed show; zero for none.
	date time.Time
	// tocs are the tables of contents, regenerated once the layout
	// placed the headings.
	tocs []*toc
	// lineBreak is the line breaking rule of the paragraph being built,
	// which its runs take ([lineBreak]).
	lineBreak css.LineBreak
	// scriptRuns records the runs of the paragraph being built, which
	// [builder.splitScripts] cuts by script; nil outside a paragraph.
	scriptRuns *[]scriptRun
	// meta records the paragraph blocks' spacing properties.
	meta map[*layout.Node]paraMeta
	// listPath maps each block a list built to the lists it sits in,
	// outermost first, and listNums each list to its numbering, for the
	// L elements [builder.wrapLists] groups the blocks in.
	listPath map[*layout.Node][]int
	listNums []typeset.ListNumbering
	// formFields is office.Options FormFields, and controls the form
	// controls made fields ([builder.fieldControl]).
	formFields bool
	// captions maps the frame of a picture to the caption its caption
	// frame gives it ([captionOf]), nil when none does or
	// office.Options.DerivedAltText is off.
	captions map[*odf.Element]string
	// derivedAlt is office.Options DerivedAltText, and defaultLang its
	// DefaultLang, the language of a document that names none.
	derivedAlt  bool
	defaultLang string
	controls    []*formfield.Control
	// formIDs indexes the form elements by id for [builder.controlElem],
	// built on first use.
	formIDs map[string]*odf.Element
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

// warn records a warning once per construct: what is a fixed word that
// names it, never the document's data, which goes in msg. Several
// constructs share a code, each warned of once per effect.
func (b *builder) warn(what string, code warn.Code, effect warn.Effect, msg string) {
	b.warnEach(what, "", code, effect, msg)
}

// warnEach records a warning once per construct, key and effect: once per
// element name for a construct of elements of any name.
func (b *builder) warnEach(what, key string, code warn.Code, effect warn.Effect, msg string) {
	k := what + "\x00" + key + "\x00" + effect.String()
	if b.warned[k] {
		return
	}
	b.warned[k] = true
	b.warnings = append(b.warnings, typeset.Warning{Code: code, Effect: effect, Message: msg})
}

// stop records err as the builder's error when it ends the conversion (see
// limits.Stops) and reports whether it does. A construct whose error does
// not stop the conversion is left out with a warning.
func (b *builder) stop(err error) bool {
	if !limits.Stops(err) {
		return false
	}
	if b.err == nil {
		b.err = err
	}
	return true
}

// node counts a new node against [maxNodes], recording the error.
func (b *builder) node(n *layout.Node) *layout.Node {
	b.nodes++
	if b.nodes > maxNodes && b.err == nil {
		b.err = fmt.Errorf("%w: more than %d layout nodes", limits.ErrInputTooLarge, maxNodes)
	}
	if b.nodes%nodeCheckEvery == 0 && b.err == nil {
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

// wrapLists groups the runs of nodes a list built, those whose list path
// is deeper than depth, into L elements, the ones a nested list built in
// an L of their own inside their list's (ISO 32000-1, 14.8.4.3.3: an L
// holds its LI elements and the lists nested in them). It runs once
// Writer's spacing is applied, so that it spaces the paragraphs of a list
// and those around it as siblings: an L is a block with no box of its
// own, whose margins collapse with its first and last child's. The
// builders of the containers of blocks call it once they have done with
// their blocks as siblings: the body, a section, a table cell, a text box
// or a shape, a note, a header or a footer.
func (b *builder) wrapLists(nodes []*layout.Node, depth int) []*layout.Node {
	if len(b.listPath) == 0 {
		return nodes
	}
	var out []*layout.Node
	for i := 0; i < len(nodes); {
		path := b.listPath[nodes[i]]
		if len(path) <= depth {
			out = append(out, nodes[i])
			i++
			continue
		}
		id := path[depth]
		j := i + 1
		for j < len(nodes) {
			if p := b.listPath[nodes[j]]; len(p) <= depth || p[depth] != id {
				break
			}
			j++
		}
		st := baseValue()
		st.Display = css.Block
		out = append(out, &layout.Node{Kind: layout.Element, Style: b.share(&st), Role: "L", Numbering: b.listNums[id], Children: b.wrapLists(nodes[i:j], depth+1)})
		i = j
	}
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
		prev.Style, cur.Style = b.share(&ps), b.share(&cs)
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
	if c.Name.Local != "" && !c.Is(odf.NSDraw, "frame") {
		b.lastFrame = ""
	}
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
		case "change-start", "change-end":
			// A change of the marked view may open or close between
			// paragraphs.
			b.changeMark(c)
			return nil
		case "change", "bookmark", "bookmark-start", "bookmark-end":
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
		b.warnEach("unknown-element", c.Name.Local, warn.ContentUnsupported, warn.Omitted, fmt.Sprintf("the element %s is not drawn", c.Name.Local))
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
	t := b.foldText(newTextState(b.pkg.Flat), b.styles.Default(odf.FamilyParagraph, odf.TextProps), percentIgnored)
	t.base = [3]float64{t.size, t.scripts[0].size, t.scripts[1].size}
	for _, st := range b.styles.Chain(b.scope, odf.FamilyParagraph, name) {
		t = b.foldText(t, st.Props(odf.TextProps), stylePercent(st))
	}
	for _, l := range b.builtinLayers(name, odf.TextProps) {
		t = b.foldText(t, l, percentOfBelow)
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
	if hiddenParagraph(el) {
		return nil
	}
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
	// The paragraph's style is built here and shared once its children
	// are made: n and its line breaks hold it from then on.
	sv := baseValue()
	st := &sv
	applyText(st, t, false)
	b.applyPara(st, p, t)
	if p.dropCap != nil {
		b.warn("drop-cap", warn.ContentUnsupported, warn.Approximated, "drop caps are drawn as the paragraph's first letters in line")
	}
	n := b.node(&layout.Node{Kind: layout.Element, Role: "P", Lang: t.langTag()})
	n.ID = b.paragraphID(el)
	b.meta[n] = paraMeta{style: b.commonStyle(name), contextual: p.contextual, join: p.joinBorder}
	ws := &wsState{space: true}
	var kids []*layout.Node
	outer, outerBreak := b.scriptRuns, b.lineBreak
	var runs []scriptRun
	b.scriptRuns, b.lineBreak = &runs, st.LineBreak
	b.inline(el, t, &kids, ws, ctx.depth)
	b.scriptRuns = outer
	kids = b.splitScripts(kids, runs, strings.HasPrefix(p.writing, "rl"))
	b.lineBreak = outerBreak
	if st.TextAlign == css.AlignJustify && slices.ContainsFunc(runs, func(r scriptRun) bool { return hasArabic(r.text) }) {
		b.warn("kashida", warn.ContentUnsupported, warn.Approximated, "justified Arabic text is stretched at its spaces, without kashida")
	}
	if !hasText(kids) && b.floatCount(kids) > 1 {
		b.pinFloats(kids, p)
	}
	kids, top := b.frameFirst(kids, st)
	// An empty paragraph keeps a line; a line break that ends the
	// paragraph opens an empty last line, as Writer draws it. Either
	// line break takes the paragraph's style.
	lineBreak := visible(kids[top:]) == 0 || kids[len(kids)-1].Kind == layout.LineBreak
	for _, k := range kids {
		if k.Style.Position == css.Absolute && k.Kind != layout.Text || lineBreak && st.Position == css.Absolute {
			// A run-through frame anchored to the paragraph is placed from
			// the paragraph's corner: the paragraph is its containing
			// block.
			st.Position = css.Relative
			break
		}
	}
	n.Style = b.share(st)
	if lineBreak {
		kids = append(kids, b.node(&layout.Node{Kind: layout.LineBreak, Style: n.Style}))
	}
	n.Children = kids
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
					kids := make([]*layout.Node, 0, 2+len(n.Children))
					if text, ok := strings.CutSuffix(lbl, "\n"); ok {
						kids = append(kids, b.run(text, t), b.node(&layout.Node{Kind: layout.LineBreak, Style: n.Style}))
						lbl = text + " "
					} else {
						kids = append(kids, b.run(lbl, t))
					}
					n.Children = append(kids, n.Children...)
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

// hiddenParagraph reports a text:p or text:h that a hidden paragraph
// field hides: a text:hidden-paragraph in its inline content whose
// text:is-hidden, the result of its condition the document saved, is
// true (ODF 1.3, Part 3, 7.7.11). One such field hides the paragraph
// whatever the others say. Writer prints no hidden paragraph, its frames
// and line breaks included. Fields inside a frame, a note or an
// annotation belong to their own paragraphs and are not read.
//
// # Corpus evidence
//
//   - libreoffice/odt layout/U-min.fodt, layout/fly_hidden_paragraph.fodt,
//     layout/hidden-para-as-char-fly.fodt,
//     layout/hidden-para-follow-frame.fodt: Writer's PDF holds none of the
//     paragraphs whose field has text:is-hidden="true", and the paragraph
//     of U-min.fodt whose field states no text:is-hidden prints.
func hiddenParagraph(el *odf.Element) bool {
	for _, c := range el.Children {
		switch {
		case c.IsText():
		case c.Is(odf.NSText, "hidden-paragraph"):
			if c.A(odf.NSText, "is-hidden") == "true" {
				return true
			}
		case c.Name.Space == odf.NSText && c.Name.Local != "note":
			// Spans, links, ruby and meta hold the paragraph's own
			// content. The parser bounds the depth (odf.MaxDepth).
			if hiddenParagraph(c) {
				return true
			}
		}
	}
	return false
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
	if lv, err := intconv.Atoi(el.A(odf.NSText, "outline-level")); err == nil {
		return max(min(lv, odf.MaxListLevels), 0)
	}
	v := b.styles.Inherited(b.scope, odf.FamilyParagraph, style, func(s *odf.Style) (string, bool) {
		if s.OutlineLevel > 0 {
			return strconv.Itoa(s.OutlineLevel), true
		}
		return "", false
	})
	if lv, err := intconv.Atoi(v); err == nil {
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
// style that would reset the paragraph's). A size in percent of a common
// text style multiplies its chain's size, which starts from the default
// paragraph style's ([stylePercent]).
func (b *builder) spanText(t textState, name string) textState {
	t.chain = t.base
	for _, st := range b.styles.Chain(b.scope, odf.FamilyText, name) {
		t = b.foldText(t, st.Props(odf.TextProps), stylePercent(st))
	}
	return t
}

// run returns the inline node of the text s in the text properties t.
func (b *builder) run(s string, t textState) *layout.Node {
	if t.outline {
		b.warn("text-outline", warn.ContentUnsupported, warn.Approximated, "outlined text is drawn filled")
	}
	if t.relief != "" {
		b.warn("text-relief", warn.ContentUnsupported, warn.Approximated, "embossed and engraved text is approximated by a grey shadow")
	}
	changed := len(b.openChanges) > 0
	if changed {
		markText(&t, b.changeMarks[b.openChanges[len(b.openChanges)-1]])
	}
	n := b.runNode(s, t, changed)
	if b.scriptRuns != nil {
		*b.scriptRuns = append(*b.scriptRuns, scriptRun{node: n, text: s, t: t, changed: changed})
	}
	return n
}

// runNode returns the inline node of the text s in the text properties t,
// marked as changed text when changed.
func (b *builder) runNode(s string, t textState, changed bool) *layout.Node {
	sv := baseValue()
	applyText(&sv, t, true)
	sv.LineBreak = b.lineBreak
	if t.auto != 0 {
		sv.VerticalAlign = css.VerticalAlign{Kind: css.AlignLength, Length: css.Pts(b.raise(t))}
		// Automatic escapement keeps the run inside the full-size line:
		// Writer's line does not grow for it, so its inline box takes no
		// height.
		sv.LineHeight = css.Pts(0)
	}
	st := b.share(&sv)
	n := b.node(&layout.Node{Kind: layout.Element, Style: st, Lang: t.langTag(), ChangeBar: changed})
	tn := &layout.Node{Kind: layout.Text, Style: st, Text: s}
	office.SymbolNode(tn, b.fonts, symbolfont.RuleLibreOffice, b.symbols)
	n.Children = []*layout.Node{b.node(tn)}
	return n
}

// hasArabic reports a text holding an Arabic letter, which Writer
// stretches with kashida in a justified paragraph, where litho widens the
// spaces alone.
//
// # Corpus evidence
//
//   - litho/odt: ar-rtl prints the lines of its justified Arabic
//     paragraph with kashida glyphs inserted between joined letters.
func hasArabic(s string) bool {
	for _, r := range s {
		if r >= 0x0600 && unicode.Is(unicode.Arabic, r) && unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

// scriptRun is a run [builder.run] made in the paragraph being built: its
// node, its text and its text properties.
type scriptRun struct {
	node    *layout.Node
	text    string
	t       textState
	changed bool
}

// splitScripts cuts the runs of a paragraph, made in runs and laid in
// kids, at each change of script, rtl when the paragraph runs right to
// left: each part draws in the text properties of its script (ODF 1.3,
// Part 3, 20.270 to 20.281; [odf.Scripts]). The script of a character
// depends on the paragraph's text around it, across runs.
func (b *builder) splitScripts(kids []*layout.Node, runs []scriptRun, rtl bool) []*layout.Node {
	if !rtl && !slices.ContainsFunc(runs, func(r scriptRun) bool { return odf.MayMixScripts([]string{r.text}, false) }) {
		return kids
	}
	byNode := make(map[*layout.Node]int, len(runs))
	for i, r := range runs {
		byNode[r.node] = i
	}
	var order []int
	var find func(nodes []*layout.Node, depth int)
	find = func(nodes []*layout.Node, depth int) {
		for _, n := range nodes {
			if i, ok := byNode[n]; ok {
				order = append(order, i)
			} else if depth < odf.MaxDepth {
				find(n.Children, depth+1)
			}
		}
	}
	find(kids, 0)
	texts := make([]string, len(order))
	for k, i := range order {
		texts[k] = runs[i].text
	}
	parts, err := odf.SplitScripts(b.ctx, texts, rtl)
	if err != nil {
		b.stop(err)
		return kids
	}
	if parts == nil {
		return kids
	}
	split := make(map[*layout.Node][]*layout.Node, len(order))
	for k, i := range order {
		r := runs[i]
		if len(parts[k]) == 1 && parts[k][0].Script == odf.Western {
			continue
		}
		nodes := make([]*layout.Node, 0, len(parts[k]))
		for _, p := range parts[k] {
			nodes = append(nodes, b.runNode(p.Text, r.t.forScript(p.Script), r.changed))
		}
		split[r.node] = nodes
	}
	var rewrite func(nodes []*layout.Node, depth int) []*layout.Node
	rewrite = func(nodes []*layout.Node, depth int) []*layout.Node {
		var out []*layout.Node
		for i, n := range nodes {
			if repl, ok := split[n]; ok {
				if out == nil {
					out = append(make([]*layout.Node, 0, len(nodes)+len(repl)), nodes[:i]...)
				}
				out = append(out, repl...)
				continue
			}
			if depth < odf.MaxDepth && len(n.Children) > 0 {
				n.Children = rewrite(n.Children, depth+1)
			}
			if out != nil {
				out = append(out, n)
			}
		}
		if out == nil {
			return nodes
		}
		return out
	}
	return rewrite(kids, 0)
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
			if v, err := intconv.Atoi(c.A(odf.NSText, "c")); err == nil && v > 1 {
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
			st := baseValue()
			applyText(&st, t, true)
			*out = append(*out, b.node(&layout.Node{Kind: layout.LineBreak, Style: b.share(&st)}))
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
			"user-index-mark-end", "change":
		case "change-start", "change-end":
			b.changeMark(c)
			if n := b.changeBound(c); n != nil {
				*out = append(*out, n)
			}
		case "page-number", "page-count":
			b.pageField(c, t, out, ws, depth)
		case "date", "time":
			if v, ok := b.dateField(c); ok {
				*out = append(*out, b.run(v, t))
				ws.space = false
				return
			}
			b.inline(c, t, out, ws, depth)
		case "hidden-text":
			if c.A(odf.NSText, "is-hidden") != "true" {
				b.inline(c, t, out, ws, depth)
			}
		case "hidden-paragraph":
		case "bookmark-ref", "reference-ref", "sequence-ref":
			b.crossReference(c, t, out, ws, depth)
		case "sequence":
			if name := c.A(odf.NSText, "ref-name"); name != "" {
				*out = append(*out, b.anchor(name))
			}
			b.inline(c, t, out, ws, depth)
		case "ruby":
			b.ruby(c, t, out, ws, depth)
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

// rubySize is the font size of ruby text whose style sets none: 6
// points, the size of Writer's Rubies character style, whatever the size
// of its base.
const rubySize = 6

// rubySlack is the room an annotation's box takes past its measured
// width, so that rounding does not wrap its last character.
const rubySlack = 0.01

// ruby appends a text:ruby (ODF 1.3, Part 3, 6.4): its base in line, its
// annotation drawn above it, or below it under style:ruby-position below
// (20.343), centred on it, or from its start under style:ruby-align left
// (20.342), in a box as wide as the base. The annotation overhangs the
// text beside the base when it is wider; the line grows by the
// annotation's height. The base does not break across lines.
//
// # Corpus evidence
//
//   - litho/odt: ja-ruby prints its ruby text at 6 points over 12 and 20
//     point bases, centred on the base, "びょうどう" overhanging "平等" by 3
//     points on each side without moving it, the ruby's descent on the
//     base's ascent (15.65 points above a 12 point Noto Sans CJK base
//     line).
func (b *builder) ruby(c *odf.Element, t textState, out *[]*layout.Node, ws *wsState, depth int) {
	var base []*layout.Node
	b.inline(c.Child(odf.NSText, "ruby-base"), t, &base, ws, depth)
	rt := c.Child(odf.NSText, "ruby-text")
	annot := ""
	if rt != nil {
		annot = strings.TrimSpace(rt.TextContent())
	}
	if annot == "" || len(base) == 0 || t.hidden {
		*out = append(*out, base...)
		return
	}
	rs := t
	rs.size, rs.scripts[0].size, rs.scripts[1].size = rubySize, rubySize, rubySize
	rs.under, rs.through, rs.over = decoLine{}, decoLine{}, decoLine{}
	if name := rt.A(odf.NSText, "style-name"); name != "" {
		rs = b.spanText(rs, name)
	}
	props := b.styles.Resolve(b.scope, odf.FamilyRuby, c.A(odf.NSText, "style-name"), odf.RubyProps)
	var baseText strings.Builder
	for _, n := range base {
		walkText(n, &baseText)
	}
	baseW := b.scriptWidth(baseText.String(), t)
	annotNodes, annotW, annotH := b.scriptParts(annot, rs)
	left := (baseW - annotW) / 2
	if props.Get("style:ruby-align") == "left" {
		left = 0
	}
	// The annotation's block takes the annotation's font, so that no
	// strut of the body's size lowers its line.
	ann := baseStyle()
	applyText(ann, rs, false)
	ann.Display = css.Block
	ann.WhiteSpace = css.Pre
	ann.Position = css.Absolute
	ann.Left, ann.Top = css.Pts(left), css.Pts(0)
	// An annotation wider than its base keeps to one line.
	ann.Width = css.Pts(annotW + rubySlack)
	band := baseStyle()
	band.Display = css.Block
	band.Position = css.Relative
	band.Height = css.Pts(annotH)
	bandNode := b.node(&layout.Node{Kind: layout.Element, Style: band, Children: []*layout.Node{
		b.node(&layout.Node{Kind: layout.Element, Style: ann, Children: annotNodes}),
	}})
	bst := baseStyle()
	bst.Display = css.Block
	bst.WhiteSpace = css.Pre
	baseNode := b.node(&layout.Node{Kind: layout.Element, Style: bst, Children: base})
	wst := baseStyle()
	wst.Display = css.InlineBlock
	kids := []*layout.Node{bandNode, baseNode}
	if props.Get("style:ruby-position") == "below" {
		kids = []*layout.Node{baseNode, bandNode}
	}
	*out = append(*out, b.node(&layout.Node{Kind: layout.Element, Style: wst, Children: kids}))
}

// walkText appends the text of the text nodes of n to sb, in tree order.
func walkText(n *layout.Node, sb *strings.Builder) {
	if n.Kind == layout.Text {
		sb.WriteString(n.Text)
	}
	for _, c := range n.Children {
		walkText(c, sb)
	}
}

// scriptWidth returns the width of the text s in the text properties t,
// each part in its script's ([odf.SplitScripts]).
func (b *builder) scriptWidth(s string, t textState) float64 {
	parts, err := odf.SplitScripts(b.ctx, []string{s}, false)
	if err != nil || parts == nil {
		st := baseStyle()
		applyText(st, t, true)
		return b.textWidth(s, st)
	}
	w := 0.0
	for _, p := range parts[0] {
		st := baseStyle()
		applyText(st, t.forScript(p.Script), true)
		w += b.textWidth(p.Text, st)
	}
	return w
}

// scriptParts returns the run nodes of the text s in the text properties
// t, each part of a script in its script's, with their width and the
// height of their line: the tallest ascent and descent of their faces.
func (b *builder) scriptParts(s string, t textState) ([]*layout.Node, float64, float64) {
	parts := [][]odf.ScriptPart{{{Text: s, Script: odf.Western}}}
	if p, err := odf.SplitScripts(b.ctx, []string{s}, false); err == nil && p != nil {
		parts = p
	}
	nodes := make([]*layout.Node, 0, len(parts[0]))
	w, h := 0.0, 0.0
	for _, p := range parts[0] {
		pt := t.forScript(p.Script)
		n := b.runNode(p.Text, pt, false)
		nodes = append(nodes, n)
		w += b.textWidth(p.Text, n.Style)
		asc, desc := b.faceMetrics(pt)
		h = max(h, (asc+desc)*pt.size)
	}
	return nodes, w, h
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

// crossReference appends a text:bookmark-ref, text:reference-ref or
// text:sequence-ref (ODF 1.3, Part 3, 7.7.6, 7.7.5 and 7.7.8): the content
// the document saved, a link to the bookmark, reference mark or sequence
// field its text:ref-name names, as Writer exports a cross-reference. A
// name nothing in the document carries links nowhere.
//
// # Corpus evidence
//
//   - libreoffice/odt odfexport/CrossRefHeadingBookmark.fodt,
//     odfexport/referencelanguage.odt: Writer's PDF links each of the 18
//     references, bookmark and sequence references, every reference
//     format among them, to the page of its target.
//   - probe, odt: through gotenberg/gotenberg:9.4.1, Writer links a
//     reference-ref, the text of a bookmark-ref and a sequence-ref of
//     category and value; it leaves unlinked the page of a bookmark, of a
//     sequence and of a note, a sequence-ref of value alone in another
//     document, and every note-ref, which this converter does not link.
func (b *builder) crossReference(c *odf.Element, t textState, out *[]*layout.Node, ws *wsState, depth int) {
	var kids []*layout.Node
	b.inline(c, t, &kids, ws, depth)
	name := c.A(odf.NSText, "ref-name")
	if name == "" || len(kids) == 0 {
		*out = append(*out, kids...)
		return
	}
	*out = append(*out, b.node(&layout.Node{Kind: layout.Element, Style: baseStyle(), Href: "#" + name, Role: "Link", Children: kids}))
}

// changeMark opens or closes, at a text:change-start or text:change-end
// (ODF 1.3, Part 3, 5.5.7), the change of the marked view whose mark the
// runs between are drawn with ([builder.run]); the other views have
// rewritten the text already ([resolveChanges]).
func (b *builder) changeMark(c *odf.Element) {
	id := c.A(odf.NSText, "change-id")
	if _, ok := b.changeMarks[id]; !ok {
		return
	}
	b.openChanges = slices.DeleteFunc(b.openChanges, func(s string) bool { return s == id })
	if c.Name.Local == "change-start" {
		b.openChanges = append(b.openChanges, id)
	}
}

// pageField appends a text:page-number or text:page-count (ODF 1.3, Part
// 3, 7.3.4 and 7.5.18): in a header or footer the number of the page it
// is drawn on, in the page layout's number format, else the content the
// document saved.
func (b *builder) pageField(c *odf.Element, t textState, out *[]*layout.Node, ws *wsState, depth int) {
	if b.hf == nil {
		b.inline(c, t, out, ws, depth)
		return
	}
	n := b.hf.number
	if c.Name.Local == "page-count" {
		n = b.hf.count
	} else if adj, err := intconv.Atoi(c.A(odf.NSText, "page-adjust")); err == nil {
		n += max(-intconv.MaxCountStart, min(adj, intconv.MaxCountStart))
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
	n.Children = b.wrapLists(b.blocks(c, ctx), 0)
	return []*layout.Node{n}
}

// columns sets the style:columns of a section or page (ODF 1.3, Part 3,
// 17.12): the count, the gap and the separator; columns of relative
// widths are laid out equal.
func (b *builder) columns(st *css.Style, cols *odf.Element, width float64) {
	if cols == nil {
		return
	}
	count, err := intconv.Atoi(cols.A(odf.NSFO, "column-count"))
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
// when a column's share does not read as a finite, non-negative number.
func (b *builder) columnTracks(kids []*odf.Element, width float64) (tracks []columnTrack, first, last float64, ok bool) {
	rel := make([]float64, len(kids))
	sum := 0.0
	for i, k := range kids {
		v, err := strconv.ParseFloat(strings.TrimSuffix(k.A(odf.NSStyle, "rel-width"), "*"), 64)
		if err != nil || !numeric.NonNegative(v) || math.IsInf(v, 0) {
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
		t := columnTrack{Width: max(float64(rel[i]/sum*width)-starts[i]-ends[i], 0)}
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
