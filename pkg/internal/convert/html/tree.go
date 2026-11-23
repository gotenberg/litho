package html

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	dom "golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/svg"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// roles maps an HTML element to the standard structure type it starts (ISO
// 32000-1, 14.8.4, Tables 333 to 340). An element absent from the map
// starts none: its content hangs from its nearest ancestor's element. The
// root element starts none either: the engine makes the Document element.
// a starts Link only with an href, img starts Figure only when its alt is
// not empty; both are decided in [builder.element]. Immutable lookup table.
var roles = map[atom.Atom]string{
	atom.Article: "Sect", atom.Section: "Sect", atom.Nav: "Sect", atom.Aside: "Sect",
	atom.Div: "Div", atom.Main: "Div", atom.Header: "Div", atom.Footer: "Div",
	atom.H1: "H1", atom.H2: "H2", atom.H3: "H3", atom.H4: "H4", atom.H5: "H5", atom.H6: "H6",
	atom.P:  "P",
	atom.Ul: "L", atom.Ol: "L", atom.Dl: "L",
	// The engine writes the Lbl and LBody of a list item, and groups a
	// definition list's terms and descriptions into its items.
	atom.Li: "LI", atom.Dt: "Lbl", atom.Dd: "LBody",
	atom.Blockquote: "BlockQuote",
	atom.Pre:        "Code", atom.Code: "Code", atom.Kbd: "Code", atom.Samp: "Code",
	atom.Q: "Quote", atom.Cite: "Quote",
	atom.Figure: "Figure", atom.Figcaption: "Caption",
	atom.Table: "Table", atom.Thead: "THead", atom.Tbody: "TBody", atom.Tfoot: "TFoot",
	atom.Tr: "TR", atom.Th: "TH", atom.Td: "TD", atom.Caption: "Caption",
	atom.Span: "Span", atom.Em: "Span", atom.Strong: "Span", atom.B: "Span", atom.I: "Span",
	atom.U: "Span", atom.Small: "Span", atom.Sub: "Span", atom.Sup: "Span", atom.Abbr: "Span",
	atom.Mark: "Span",
	// ISO 32000-1, 14.8.4.4.2, Table 338: the bases, annotations and
	// punctuation of a ruby; the layout engine makes a Ruby of each pair
	// of a base and its annotation, and an RB of a base written without
	// rb.
	atom.Rb: "RB", atom.Rt: "RT", atom.Rp: "RP",
}

// skipped are the elements that draw nothing in a static rendering
// whatever their style, with their content: void metadata, inert
// templates, and noscript, which a browser running scripts hides (HTML
// Standard, 4.12.2). litho runs no script and reports a document that holds
// one ([scriptWarning]); the noscript content stays out all the same, since
// the page's own layout expects its scripts to have run. head, title, style, script and datalist are not
// among them: the user agent sheet hides them with display: none (HTML
// Standard, 15.3.1), and an author who displays them sees their text, as
// Chromium draws it. Immutable lookup table.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 draws the text of
//     head, title, style and script under display: block, and of
//     datalist under display: contents; template and noscript stay
//     hidden.
var skipped = map[atom.Atom]bool{
	atom.Template: true, atom.Noscript: true, atom.Meta: true, atom.Link: true, atom.Base: true,
	atom.Source: true, atom.Track: true, atom.Param: true, atom.Area: true,
}

// unsupported are the elements whose content is not drawn, with a
// warning: audio. An iframe, a frame, and an object or an embed of an
// HTML document draw their document ([builder.frame]). A video draws its poster ([builder.video]). Immutable lookup
// table.
var unsupported = map[atom.Atom]bool{
	atom.Audio: true,
}

// builder turns the styled DOM into the layout's content tree. It lives
// for one [Convert] call.
type builder struct {
	computed *css.Computed
	res      *resources
	scan     *scan
	// media is the medium a <source media> and a sizes attribute are
	// evaluated against.
	media css.Media

	elements  int
	synthetic int
	headings  []heading
	warnings  []typeset.Warning
	warned    map[string]bool
	// counters holds the CSS counters and the quote depth of the walk.
	counters counterState
	// omitBackgrounds clears the backgrounds of the html and body
	// elements ([Options.OmitBackgrounds]).
	omitBackgrounds bool
	// derivedAlt describes the images their markup does not
	// ([Options.DerivedAltText]).
	derivedAlt bool
	// ids are anchors set on elements over their id attributes, nil for
	// none.
	ids map[*dom.Node]string
	// gcpm holds the named strings and running elements (CSS GCPM 3, 2).
	gcpm gcpmState
	// targets holds the cross-references and the elements they read (CSS
	// Generated Content 3, 6).
	targets targetState
	// counterStyles spells the counter styles the layout does not.
	counterStyles *css.CounterStyles
	// firstLetters marks the boxes cut for ::first-letter, nil for none.
	firstLetters map[*layout.Node]bool
	// lines styles the first formatted lines, nil when no element has a
	// ::first-line.
	lines *firstLines
	// legend is the rendered legend of the fieldset whose children are
	// being built.
	legend renderedLegend
	// defDocs holds the drawing of each svg element a clip-path or a mask
	// reached, nil for one that does not parse; the layout reads it
	// through defsMu.
	defDocs map[*dom.Node]*svg.Document
	defsMu  sync.Mutex
	// containers maps the node of each container for size queries to its
	// element, for the sizes the layout measures; nil for none.
	containers map[*layout.Node]*dom.Node
	// anchored maps the node of each element with position options whose
	// chosen option a cascade reads to its element ([builder.positioned]);
	// nil for none.
	anchored map[*layout.Node]*dom.Node
	// frames lays the documents of frames out, nil to load none;
	// frameOpts are the options the document is converted with,
	// frameSelf its URL, [srcdocURL] for a srcdoc, frameChain the URLs
	// of the documents that hold it and frameDepth its nesting, 0 for the
	// conversion's own document ([builder.frame]).
	frames     *frameSet
	frameOpts  Options
	frameSelf  string
	frameChain []string
	frameDepth int
}

// container records node, the box of the element n, when n is a container
// for size queries, at most [maxContainers] of them.
func (b *builder) container(node *layout.Node, n *dom.Node) {
	if !node.Style.SizeContainer() {
		return
	}
	if len(b.containers) >= maxContainers {
		b.warnOnce(warn.LimitReached, warn.Approximated, "containers", fmt.Sprintf("a document of more than %d containers for size queries: the others answer no size query", maxContainers))
		return
	}
	if b.containers == nil {
		b.containers = map[*layout.Node]*dom.Node{}
	}
	b.containers[node] = n
}

// buildRoot builds the content tree of the root element.
func (b *builder) buildRoot(ctx context.Context, root *dom.Node) (*layout.Node, error) {
	st := b.computed.Style(root)
	if st == nil {
		st = css.Initial()
	}
	if b.omitBackgrounds {
		st = withoutBackground(st)
	}
	n := &layout.Node{Kind: layout.Element, Style: st, Lang: b.scan.lang}
	if st.Display == css.DisplayNone {
		return n, nil
	}
	if b.frameOpts.mode == css.Quirks && root.Namespace == "" {
		n.Quirks = quirksOf(root, st)
	}
	b.container(n, root)
	v := b.applyCounters(root, st)
	if b.counters.innermost(footnoteCounter) == nil {
		// The footnote counter numbers the footnotes of the whole
		// document unless an element resets it (CSS GCPM 3, 3.5.2).
		_ = b.counters.instantiate(footnoteCounter, 0, false)
	}
	if b.counters.innermost(noteCounter) == nil {
		// The note counter numbers the notes alike.
		_ = b.counters.instantiate(noteCounter, 0, false)
	}
	err := b.content(ctx, n, root, st, v, 1)
	return n, err
}

// content builds the children of node, the box of the element n styled
// st, numbered v when it is a list item: its marker, its ::before, its
// children and its ::after, in the level of n's counters (CSS Lists 3,
// 4.5 and CSS Pseudo 4, 4.1).
func (b *builder) content(ctx context.Context, node *layout.Node, n *dom.Node, st *css.Style, v, depth int) error {
	if st.Display == css.Inline {
		b.onFirstLine(node, n, 0, false)
	}
	if b.enterFirstLine(node, n, st) {
		defer b.leaveFirstLine()
	}
	bookmark := -1
	if st.BookmarkLevel > 0 {
		bookmark = b.bookmark(node, st)
	}
	if isListItem(st.Display) {
		node.ListValue = v
		node.Marker = b.marker(ctx, n, st, v)
	}
	var targetVals [][]int
	if b.targets.use.Any {
		// A cross-reference reads the counters as counter() reads them at
		// the element (CSS Generated Content 3, 6.1).
		targetVals = b.targetValues()
	}
	b.counters.open()
	defer b.counters.close()
	if st.Containment()&css.ContainStyle != 0 {
		// Style containment scopes counter-increment and counter-set to
		// the element's subtree (CSS Contain 2, 3.3); counter() still
		// reads the counters around it.
		outer := b.counters.scope
		b.counters.scope = b.counters.level()
		defer func() { b.counters.scope = outer }()
	}
	before := b.pseudo(ctx, n, css.PseudoBefore)
	if before != nil {
		b.onFirstLine(before, n, css.PseudoBefore, true)
		node.Children = append(node.Children, before)
	}
	var kids []*layout.Node
	var legend *layout.Node
	var err error
	switch {
	case isDetails(n):
		kids, err = b.detailsChildren(ctx, n, st, depth)
	case b.computed.RenderedLegend(n) != nil:
		outer := b.legend
		b.legend = renderedLegend{dom: b.computed.RenderedLegend(n)}
		kids, err = b.children(ctx, n, st, depth)
		legend = b.legend.node
		b.legend = outer
	default:
		kids, err = b.children(ctx, n, st, depth)
	}
	if err != nil {
		return err
	}
	node.Children = append(node.Children, kids...)
	after := b.pseudo(ctx, n, css.PseudoAfter)
	if after != nil {
		b.onFirstLine(after, n, css.PseudoAfter, true)
		node.Children = append(node.Children, after)
	}
	if takesFirstLetter(st.Display) {
		if b.computed.Pseudo(n, css.PseudoFirstLetter) != nil {
			b.firstLetter(node, n)
		}
	}
	if len(st.StringSet) > 0 {
		b.setStrings(n, node, st, before, after)
	}
	if b.targets.use.Any {
		b.markTarget(n, node, before, after, targetVals)
	}
	if bookmark >= 0 && st.BookmarkLabel != nil {
		b.headings[bookmark].label = b.contentText(n, node, st.BookmarkLabel, before, after)
		b.headings[bookmark].labelled = true
	}
	if fs, cs := b.computed.Fieldset(n); fs != nil && cs != nil {
		fieldsetBoxes(node, fs, cs, legend)
	}
	return nil
}

// children builds the nodes of n's children, n's style being st.
func (b *builder) children(ctx context.Context, n *dom.Node, st *css.Style, depth int) ([]*layout.Node, error) {
	var out []*layout.Node
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		switch c.Type { //nolint:exhaustive // comments, doctypes and the rest draw nothing
		case dom.TextNode:
			if c.Data != "" {
				out = append(out, &layout.Node{Kind: layout.Text, Style: st, Text: c.Data})
			}
		case dom.ElementNode:
			kids, err := b.element(ctx, c, st, depth)
			if err != nil {
				return nil, err
			}
			out = append(out, kids...)
		}
	}
	return out, nil
}

// element builds the nodes of the element n, a child of an element styled
// parent, at depth below the root: none for an element that draws nothing,
// one in general, its children for a transparent fallback.
func (b *builder) element(ctx context.Context, n *dom.Node, parent *css.Style, depth int) ([]*layout.Node, error) {
	b.elements++
	if b.elements%1024 == 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	if n.Namespace != "" {
		if n.Namespace == "svg" {
			if n.Data != "svg" {
				return nil, nil
			}
			return b.inlineSVG(ctx, n, b.computed.Style(n)), nil
		}
		if n.Namespace == "math" && n.Data == "math" && b.res.formulas != nil {
			return b.mathElement(ctx, n, b.computed.Style(n)), nil
		}
		// A MathML element outside a math element, or one laid out with
		// no fonts, draws as its text.
		return b.styled(ctx, n, parent, depth)
	}
	if skipped[n.DataAtom] {
		return nil, nil
	}
	if unsupported[n.DataAtom] {
		b.warnOnce(warn.ContentUnsupported, warn.Omitted, n.Data, fmt.Sprintf("the content of <%s> is not drawn", n.Data))
		return nil, nil
	}
	return b.styled(ctx, n, parent, depth)
}

// styled builds the nodes of the element n once it is known to draw.
func (b *builder) styled(ctx context.Context, n *dom.Node, parent *css.Style, depth int) ([]*layout.Node, error) {
	st := b.computed.Style(n)
	if st == nil || st.Display == css.DisplayNone {
		return nil, nil
	}
	st = b.offsetPath(ctx, st)
	if b.omitBackgrounds && (n.Namespace == "" && n.DataAtom == atom.Body || st.PrintColorAdjust == css.ColorEconomy) {
		st = withoutBackground(st)
	}
	if depth >= maxTreeDepth {
		b.warnOnce(warn.LimitReached, warn.Omitted, "depth", fmt.Sprintf("content nested deeper than %d elements was left out", maxTreeDepth))
		return nil, nil
	}
	v := b.applyCounters(n, st)
	pushed := slices.Clone(b.counters.pushed)
	defer b.counters.leave(pushed)
	if node := b.replacedByContent(ctx, n, st); node != nil {
		return []*layout.Node{node}, nil
	}
	if n.Namespace == "" {
		switch n.DataAtom { //nolint:exhaustive // every other element takes the default path
		case atom.Br:
			return []*layout.Node{{Kind: layout.LineBreak, Style: st}}, nil
		case atom.Wbr:
			return []*layout.Node{{Kind: layout.Text, Style: parent, Text: "\u200b"}}, nil
		case atom.Img:
			return b.image(ctx, n, st, parent), nil
		case atom.Video:
			return b.video(ctx, n, st), nil
		case atom.Iframe:
			return b.frame(n, st), nil
		case atom.Frame:
			if p := n.Parent; p == nil || p.DataAtom != atom.Frameset || p.Namespace != "" {
				// A frame outside a frameset has no child navigable to
				// show (HTML Standard, 15.6).
				return nil, nil
			}
			return b.frame(n, st), nil
		case atom.Input:
			if inputType(n) == "image" {
				// An image button shows its picture, or its alternative
				// text, as an img does (HTML Standard, 4.10.5.1.20).
				return b.image(ctx, n, st, parent), nil
			}
			return b.control(n, st), nil
		case atom.Select, atom.Textarea:
			return b.control(n, st), nil
		case atom.Progress, atom.Meter:
			return b.gauge(n, st), nil
		case atom.Object:
			if doc := b.embeddedDocument(ctx, n, st, "data"); doc != nil {
				return doc, nil
			}
			b.warnOnce(warn.ContentUnsupported, warn.Approximated, n.Data, "the content of <object> is not drawn; its fallback content is")
			return b.children(ctx, n, parent, depth)
		case atom.Embed:
			if doc := b.embeddedDocument(ctx, n, st, "src"); doc != nil {
				return doc, nil
			}
			b.warnOnce(warn.ContentUnsupported, warn.Omitted, n.Data, "the content of <embed> is not drawn")
			return nil, nil
		case atom.Canvas:
			// Without scripts a canvas shows its fallback content (HTML
			// Standard, 4.12.5).
			return b.children(ctx, n, parent, depth)
		}
	}
	if st.ContentVisibility == css.VisibilityHidden {
		node := hiddenContents(st)
		b.semantics(node, n)
		return []*layout.Node{node}, nil
	}
	node := &layout.Node{Kind: layout.Element, Style: st}
	b.semantics(node, n)
	if n.DataAtom == atom.Button && n.Namespace == "" {
		// A button lays its content out in its face (HTML Standard,
		// 15.5.2).
		node.Control = &layout.Control{Kind: layout.ControlButton, Disabled: formDisabled(n)}
		formRole(node, n, typeset.PrintPushButton, false)
	}
	b.container(node, n)
	b.positioned(node, n)
	if err := b.content(ctx, node, n, st, v, depth+1); err != nil {
		return nil, err
	}
	if n == b.legend.dom && b.legend.node == nil {
		b.legend.node = legendNode(node)
	}
	if st.Footnote {
		return []*layout.Node{b.footnote(ctx, n, node, st)}, nil
	}
	if st.Note != "" {
		return []*layout.Node{b.note(ctx, n, node, st)}, nil
	}
	if st.Running != "" {
		if keep := b.runAway(n, node, st); keep != nil {
			return []*layout.Node{keep}, nil
		}
		return nil, nil
	}
	return []*layout.Node{node}, nil
}

// semantics sets what node says about the element n beyond its style: its
// role, language, anchor, link and spans.
func (b *builder) semantics(node *layout.Node, n *dom.Node) {
	if n.Namespace != "" {
		return
	}
	node.Role = roles[n.DataAtom]
	if b.frameOpts.mode == css.Quirks {
		node.Quirks = quirksOf(n, node.Style)
	}
	if lang, ok := attr(n, "lang"); ok {
		node.Lang = strings.TrimSpace(lang)
	}
	node.ID, _ = attr(n, "id")
	if id, ok := b.ids[n]; ok {
		node.ID = id
	}
	switch n.DataAtom { //nolint:exhaustive // every other element takes the default path
	case atom.A:
		if node.ID == "" {
			// A named anchor is a link target (HTML Standard, 7.4.6.4).
			node.ID, _ = attr(n, "name")
		}
		node.Role = ""
		if href, ok := attr(n, "href"); ok {
			if node.Href = b.linkTarget(href); node.Href != "" {
				node.Role = "Link"
				node.Alt, _ = attr(n, "title")
			}
		}
	case atom.Td, atom.Th:
		node.ColSpan = spanAttr(n, "colspan", maxColSpan)
		node.RowSpan = rowSpanAttr(n)
		if n.DataAtom == atom.Th {
			node.Scope = headerScope(n)
		}
	case atom.Ul, atom.Ol:
		if node.Style != nil {
			node.Numbering = listNumbering(node.Style.ListStyleType, n.DataAtom == atom.Ol)
		}
	case atom.Figure:
		node.Alt = figureName(n)
	case atom.Dl:
		node.Numbering = typeset.NumberingDescription
	case atom.Dt, atom.Dd:
		if !inDefinitionList(n) {
			// A Lbl or a LBody belongs in a list item (ISO 32000-1,
			// 14.8.4.3.3): a term or a description outside a definition
			// list starts no element.
			node.Role = ""
		}
	case atom.Div:
		if p := n.Parent; p != nil && p.DataAtom == atom.Dl && p.Namespace == "" {
			// A div that wraps a group of a definition list (HTML
			// Standard, 4.4.9) starts no element: a list holds no Div
			// (ISO/TS 32005, Table 5), and the group's LI is the
			// engine's.
			node.Role = ""
		}
	case atom.Col, atom.Colgroup:
		node.Span = spanAttr(n, "span", maxColSpan)
	}
}

// inDefinitionList reports whether n, a dt or a dd, is a child of a dl or
// of a div that is one (HTML Standard, 4.4.9).
func inDefinitionList(n *dom.Node) bool {
	p := n.Parent
	if p != nil && p.DataAtom == atom.Div && p.Namespace == "" {
		p = p.Parent
	}
	return p != nil && p.DataAtom == atom.Dl && p.Namespace == ""
}

// headerScope returns the cells a th heads (ISO 32000-1, 14.8.5.7, Table
// 349): its scope attribute's (HTML Standard, 4.9.10), else its columns'
// in a thead, its row's when it starts a row that holds a td, and its
// columns' otherwise, as the HTML table model's auto state assigns a
// header cell to the cells right of it and below it (HTML Standard,
// 4.9.12.2).
func headerScope(n *dom.Node) typeset.TableScope {
	if v, ok := attr(n, "scope"); ok {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "row", "rowgroup":
			return typeset.ScopeRow
		case "col", "colgroup":
			return typeset.ScopeColumn
		}
	}
	tr := n.Parent
	if tr == nil || tr.DataAtom != atom.Tr {
		return typeset.ScopeColumn
	}
	if g := tr.Parent; g != nil && g.DataAtom == atom.Thead {
		return typeset.ScopeColumn
	}
	first, data := true, false
	for c := tr.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != dom.ElementNode {
			continue
		}
		if c == n && !first {
			return typeset.ScopeColumn
		}
		first = false
		if c.DataAtom == atom.Td {
			data = true
		}
	}
	if data {
		return typeset.ScopeRow
	}
	return typeset.ScopeColumn
}

// listNumbering returns the numbering system of a list whose items' marker
// is t (ISO 32000-1, 14.8.5.5, Table 347; CSS Counter Styles 3, 6),
// ordered for an ol.
func listNumbering(t css.ListStyleType, ordered bool) typeset.ListNumbering {
	switch t {
	case css.Disc:
		return typeset.NumberingDisc
	case css.Circle:
		return typeset.NumberingCircle
	case css.Square:
		return typeset.NumberingSquare
	case css.Decimal, css.DecimalLeadingZero:
		return typeset.NumberingDecimal
	case css.LowerRoman:
		return typeset.NumberingLowerRoman
	case css.UpperRoman:
		return typeset.NumberingUpperRoman
	case css.LowerAlpha:
		return typeset.NumberingLowerAlpha
	case css.UpperAlpha:
		return typeset.NumberingUpperAlpha
	case css.ListNone:
		return typeset.NumberingNone
	case css.LowerGreek, css.Armenian, css.Georgian, css.ListString, css.ListNamed, css.DisclosureOpen, css.DisclosureClosed:
	}
	if ordered {
		return typeset.NumberingOrdered
	}
	return typeset.NumberingUnordered
}

// maxBookmarkLevel is the deepest bookmark-level the outline nests: 64.
// Anti-DoS guard, not spec-mandated: the draft bounds no level, an outline
// is a few levels deep, and each level is one more nesting of the
// document's outline tree; a deeper level nests as this one.
const maxBookmarkLevel = 64

// maxBookmarks bounds the outline entries a document makes: 65,536.
// Anti-DoS guard, not spec-mandated: a book has a few hundred headings,
// and each entry is an object of the PDF.
const maxBookmarks = 1 << 16

// bookmark records the element built as node, styled st, as an entry of
// the outline (CSS Generated Content 3, 8): anchored at its ID, or at one
// it is given. It returns the entry's index, -1 past [maxBookmarks].
func (b *builder) bookmark(node *layout.Node, st *css.Style) int {
	if len(b.headings) >= maxBookmarks {
		b.warnOnce(warn.LimitReached, warn.Omitted, "bookmarks", fmt.Sprintf("more than %d outline entries; the others were left out", maxBookmarks))
		return -1
	}
	if node.ID == "" {
		node.ID = b.syntheticID()
	}
	b.headings = append(b.headings, heading{node: node, level: min(st.BookmarkLevel, maxBookmarkLevel), closed: st.BookmarkClosed})
	return len(b.headings) - 1
}

// syntheticID returns an anchor ID no element of the document carries, for
// an element the outline points at.
func (b *builder) syntheticID() string {
	for {
		b.synthetic++
		id := fmt.Sprintf("litho-heading-%d", b.synthetic)
		if !b.scan.ids[id] {
			return id
		}
	}
}

// linkTarget returns the Node.Href of an <a href>: "#id" for a fragment of
// the document itself, "#" alone for the empty fragment, which is the top
// of the document (HTML Standard, 7.4.6.4), "" for none or a script, and
// otherwise the URL as the author wrote it, resolved against the
// document's <base> when it has one (HTML Standard, 4.6.4).
func (b *builder) linkTarget(href string) string {
	href = strings.Trim(href, "\t\n\f\r ")
	if frag, ok := strings.CutPrefix(href, "#"); ok {
		// The fragment is percent-decoded before it is looked up (HTML
		// Standard, 7.4.6.4).
		if dec, err := url.PathUnescape(frag); err == nil {
			frag = dec
		}
		return "#" + frag
	}
	if href == "" || len(href) >= 11 && strings.EqualFold(href[:11], "javascript:") {
		return ""
	}
	if b.scan.linkBase == "" {
		return href
	}
	u := resolveRef(b.scan.linkBase, href)
	if u == "" {
		return href
	}
	if rel, ok := strings.CutPrefix(u, rootPrefix); ok {
		return rel
	}
	return u
}

// image builds the nodes of an <img> styled st: a replaced node with the
// picture [builder.imageSource] selects, or, when the picture cannot be
// drawn, its alt text with a warning, or an empty replaced box when the
// alt text is empty or absent (see [emptyImage]).
func (b *builder) image(ctx context.Context, n *dom.Node, st, parent *css.Style) []*layout.Node {
	alt, hasAlt := attr(n, "alt")
	src, density := b.imageSource(n)
	fallback := func(failed bool) []*layout.Node {
		if alt == "" {
			out := emptyImage(n, st, failed)
			out[0].Quirks = b.stickyImage()
			return out
		}
		return []*layout.Node{{Kind: layout.Text, Style: parent, Text: alt}}
	}
	if strings.Trim(src, "\t\n\f\r ") == "" {
		return fallback(false)
	}
	pic, err := b.res.picture(ctx, resolveRef(b.scan.resourceBase, src))
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		b.warnings = append(b.warnings, typeset.Warning{Code: warningCode(err), Effect: warn.Omitted, Message: fmt.Sprintf(
			"the image %s was left out: %v", displayURL(strings.TrimSpace(src)), err)})
		return fallback(true)
	}
	if density > 0 && density != 1 {
		// The density-corrected natural size; the decoded picture is
		// shared by every element naming it.
		scaled := *pic
		scaled.Width, scaled.Height = pic.Width/density, pic.Height/density
		pic = &scaled
	}
	node := &layout.Node{Kind: layout.Replaced, Style: st, Picture: pic, Quirks: b.stickyImage()}
	node.ID, _ = attr(n, "id")
	if lang, ok := attr(n, "lang"); ok {
		node.Lang = strings.TrimSpace(lang)
	}
	// An empty alt marks a decorative image, an artifact (HTML Standard,
	// 4.8.4.4.12); any other is a Figure (ISO 32000-1, 14.8.4.5).
	if !hasAlt || alt != "" {
		node.Role = "Figure"
		node.Alt = alt
		if !hasAlt {
			node.Alt, _ = attr(n, "title")
			if node.Alt == "" && b.derivedAlt {
				node.Alt = derivedImageAlt(n)
			}
		}
	}
	return []*layout.Node{node}
}

// video builds the replaced box of a video element styled st (HTML
// Standard, 15.4.1): its poster frame, the picture its poster attribute
// names (4.8.8), drawn as an img draws its picture, which the user agent
// sheet's object-fit: contain fits and centres in the box as 15.4.1 asks.
// The poster's natural size is the box's (4.8.8, the natural width of the
// playback area). Without a poster, or with one that does not load, the
// box has no natural size and takes the default object size, 300 by 150
// CSS pixels (CSS Images 3, 4.3), and draws nothing.
//
// A frame of the video and the controls are not drawn: litho decodes no
// video, and draws no user interface. Chromium draws its controls over the
// box of a video with the controls attribute, on a black ground when no
// poster shows, and the first frame of a video whose resource loads and
// that has no poster; both are reported. The source and track children
// and the fallback content draw nothing, as in a browser that plays video
// (4.8.8).
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 prints a video
//     whose poster is a 200 by 100 picture as a 200 by 100 box showing the
//     poster, with or without a src that does not load and with source
//     children and fallback text, which it does not show; at width 400
//     and height 100 the poster 200 by 100 centred, under object-fit
//     cover 400 by 200 cut to the box, under fill stretched to it, inside
//     its padding and border over its background; at width 100px alone a
//     100 by 50 box. Without a poster, or with one that does not load, a
//     300 by 150 box drawing nothing, 100 by 150 at width 100px alone and
//     300 by 50 at height 50px alone. With controls, the same box, black
//     under its controls without a poster, the poster darkened under them
//     with one.
//   - probe, html: an audio element prints nothing and takes no room
//     without controls, and a 300 by 54 box of controls with them.
func (b *builder) video(ctx context.Context, n *dom.Node, st *css.Style) []*layout.Node {
	var pic *layout.Picture
	if poster, ok := attr(n, "poster"); ok && strings.Trim(poster, "\t\n\f\r ") != "" {
		p, err := b.res.picture(ctx, resolveRef(b.scan.resourceBase, poster))
		switch {
		case ctx.Err() != nil:
			return nil
		case err != nil:
			b.warnings = append(b.warnings, typeset.Warning{Code: warningCode(err), Effect: warn.Omitted, Message: fmt.Sprintf(
				"the poster %s of a video was left out: %v", displayURL(strings.TrimSpace(poster)), err)})
		default:
			pic = p
		}
	}
	if _, ok := attr(n, "controls"); ok {
		b.warnOnce(warn.ContentUnsupported, warn.Omitted, "video controls", "the controls of <video> are not drawn")
	}
	if pic == nil {
		if hasVideoSource(n) {
			b.warnOnce(warn.ContentUnsupported, warn.Omitted, "video frames", "the frames of <video> are not drawn; its poster is")
		}
		pic = &layout.Picture{
			Width: 300 * css.Pt, Height: 150 * css.Pt,
			Vector:         func(context.Context, typeset.Rect) []typeset.Fragment { return nil },
			NoNaturalWidth: true, NoNaturalHeight: true, NoRatio: true,
		}
	}
	node := &layout.Node{Kind: layout.Replaced, Style: st, Picture: pic}
	node.ID, _ = attr(n, "id")
	return []*layout.Node{node}
}

// hasVideoSource reports a video element that names a media resource: a
// src attribute or a source child with one (HTML Standard, 4.8.11.5).
func hasVideoSource(n *dom.Node) bool {
	if src, ok := attr(n, "src"); ok && strings.TrimSpace(src) != "" {
		return true
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == dom.ElementNode && c.DataAtom == atom.Source && c.Namespace == "" {
			if src, ok := attr(c, "src"); ok && strings.TrimSpace(src) != "" {
				return true
			}
		}
	}
	return false
}

// replacedByContent returns the replaced box of an element styled st
// whose content is one image (CSS Generated Content 3, 1.1,
// <content-replacement>): the element draws the image in place of its
// children. It returns nil for any other content, for an element that
// generates no box of its own, and for an image that cannot be drawn,
// whose element keeps its children.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 draws a div of 50 by 40 pixels whose
//     content is a url() image of 20 by 10 as that image stretched over
//     the box, its text and background hidden; without a size, the image
//     at its natural size.
func (b *builder) replacedByContent(ctx context.Context, n *dom.Node, st *css.Style) *layout.Node {
	c := &st.Content
	if c.Kind != css.ContentList || len(c.Items) != 1 || c.Items[0].Kind != css.ItemImage || st.Display == css.Contents {
		return nil
	}
	pic := b.contentPicture(ctx, c.Items[0])
	if pic == nil {
		return nil
	}
	node := &layout.Node{Kind: layout.Replaced, Style: st, Picture: pic}
	node.ID, _ = attr(n, "id")
	if c.Alt != "" {
		node.Role, node.Alt = "Figure", c.Alt
	}
	return node
}

// emptyImage returns the box of an <img> styled st that draws no picture
// and has no alt text to show: a replaced box without content, of the
// size its style gives it, zero where auto, that paints its background
// and borders. failed tells an image whose source did not load from one
// that names no source.
//
// HTML Standard, 15.4.1 treats such an element as a replaced element only
// when its dimensions are given, and as an empty inline otherwise.
// Chromium makes it a box in every case, on screen and in print, and
// litho follows Chromium: an image without a source keeps the size its
// style gives it, and one whose source failed keeps it only when its
// width and height are both given, or one of them with an aspect ratio,
// or when its display is not inline. Otherwise its width and height
// compute to auto and the box is empty.
//
// Chromium's broken-image icon, 16 pixels square and drawn when a source
// fails to load and the element has no alt attribute, is not drawn, and
// takes no room.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 draws an <img> without src, with an empty
//     src or with alt="", styled width 60px, height 40px, a background
//     and a 2px border, as a 64 by 44 box on the baseline; with width
//     alone, a 64 by 4 box; with neither, a 4 by 4 box.
//   - probe, html: Chromium 156 draws an <img alt=""> whose source fails
//     to load, with the same background and border, as a 64 by 44 box
//     under width 60px and height 40px, or width 60px and aspect-ratio
//     3/2; as a 4 by 4 box under width 60px alone, the width attribute
//     alone or height 40px alone; as a 64 by 4 box under width 60px with
//     display block or inline-block, or floated.
//   - wpt: css-tables/height-distribution/percentage-sizing-of-table-cell-replaced-children-001
//     and css-break/block-max-height-004 pass with the image drawn as
//     nothing in test and reference alike.
func emptyImage(n *dom.Node, st *css.Style, failed bool) []*layout.Node {
	if failed && st.Display == css.Inline && !sizedByStyle(st) {
		// The computed style is shared: the box takes a copy.
		s := *st
		s.Width, s.Height, s.AspectRatio = css.Dimension{}, css.Dimension{}, css.AspectRatio{}
		st = &s
	}
	node := &layout.Node{Kind: layout.Replaced, Style: st}
	node.ID, _ = attr(n, "id")
	return []*layout.Node{node}
}

// sizedByStyle reports a style that gives a box both its dimensions: a
// width and a height, or one of them and an aspect ratio.
func sizedByStyle(st *css.Style) bool {
	w, h := st.Width.Kind != css.Auto, st.Height.Kind != css.Auto
	return w && h || (w || h) && st.AspectRatio.Ratio() != 0
}

// control builds a form control styled st as an inline-block holding the
// text it shows (HTML Standard, 15.5): an input's value or placeholder, a
// select's selected option, a textarea's text or placeholder; a select
// shown as a list box holds its rows ([builder.listBox]). An input's text
// stays on one line, as a single-line text entry widget shows it
// (15.5.15). Under field-sizing: fixed, the initial value, the control
// takes its default size from its attributes and its font
// ([layout.Control]): a text field its size, 20 without one, a textarea
// its cols and rows, 20 and 2 without them, a select its options; under
// field-sizing: content its text sizes it (CSS Form Control Styling 1,
// 7.1). The layout draws the control's widget ([layout.Control]); the
// control is a Form element whose PrintField attributes say what it is
// and, for a check box or a radio button, its state (ISO 32000-1,
// 14.8.5.6), its text read inside it.
func (b *builder) control(n *dom.Node, st *css.Style) []*layout.Node {
	var s string
	var ctl *layout.Control
	disabled := formDisabled(n)
	textSt := st
	role := typeset.PrintText
	switch n.DataAtom { //nolint:exhaustive // every other element takes the default path
	case atom.Input:
		typ := inputType(n)
		switch typ {
		case "checkbox", "radio":
			_, checked := attr(n, "checked")
			node := &layout.Node{Kind: layout.Element, Style: st, Control: &layout.Control{Kind: layout.ControlCheckBox, Checked: checked, Disabled: disabled}}
			role = typeset.PrintCheckBox
			if typ == "radio" {
				node.Control.Kind, role = layout.ControlRadio, typeset.PrintRadio
			}
			node.ID, _ = attr(n, "id")
			formRole(node, n, role, checked)
			return []*layout.Node{node}
		case "range", "color":
			node := &layout.Node{Kind: layout.Element, Style: st, Control: &layout.Control{Kind: layout.ControlRange, Value: rangeValue(n), Disabled: disabled}}
			if typ == "color" {
				node.Control = &layout.Control{Kind: layout.ControlColor, Swatch: colorValue(n), Disabled: disabled}
			}
			node.ID, _ = attr(n, "id")
			return []*layout.Node{node}
		case "file":
			return b.fileControl(n, st, disabled)
		case "hidden":
			return nil
		}
		s = inputText(n)
		if st.WhiteSpace != css.Pre {
			// The computed style is shared: the text takes a copy.
			c := *st
			c.WhiteSpace = css.Pre
			textSt = &c
		}
		switch typ {
		case "submit", "reset", "button":
			ctl, role = &layout.Control{Kind: layout.ControlButton}, typeset.PrintPushButton
		case "date", "month", "week", "datetime-local", "time":
			return b.dateControl(n, typ, st, textSt, disabled)
		default:
			ctl = &layout.Control{Kind: layout.ControlTextField, Chars: positiveAttr(n, "size", defaultInputSize)}
			if v, _ := attr(n, "value"); v == "" && s != "" {
				textSt = placeholderStyle(textSt)
			}
		}
	case atom.Select:
		if rows := displaySize(n); rows > 1 {
			return b.listBox(n, st, rows, disabled)
		}
		s = selectText(n)
		ctl = &layout.Control{Kind: layout.ControlDropDown}
		for _, r := range selectRows(n) {
			if !r.heading {
				ctl.Labels = append(ctl.Labels, layout.ControlLabel{Text: r.label})
			}
		}
	default:
		s = textContent(n)
		if s == "" {
			if s, _ = attr(n, "placeholder"); s != "" {
				textSt = placeholderStyle(st)
			}
		}
		ctl = &layout.Control{Kind: layout.ControlTextArea, Chars: positiveAttr(n, "cols", defaultTextAreaCols), Rows: positiveAttr(n, "rows", defaultTextAreaRows)}
	}
	ctl.Disabled = disabled
	node := &layout.Node{Kind: layout.Element, Style: st, Control: ctl}
	node.ID, _ = attr(n, "id")
	formRole(node, n, role, false)
	if s != "" {
		node.Children = []*layout.Node{{Kind: layout.Text, Style: textSt, Text: s}}
	} else if ctl.Kind != layout.ControlButton {
		// An empty field keeps its line, whose baseline is the field's
		// (CSS 2.1, 10.8.1), as its empty editor's line is in Chromium.
		node.Children = []*layout.Node{{Kind: layout.LineBreak, Style: textSt}}
	}
	return []*layout.Node{node}
}

// dateControl builds the date or time input n of type typ styled st, its
// text in textSt: its fields, each with a pixel of padding either side,
// and the literal text between them, as Chromium lays them out
// ([dateText]); the layout draws its calendar or its clock.
func (b *builder) dateControl(n *dom.Node, typ string, st, textSt *css.Style, disabled bool) []*layout.Node {
	shown, widest := dateText(n, typ)
	ctl := &layout.Control{Kind: layout.ControlDate, Labels: []layout.ControlLabel{{Text: widest}}, Fields: countFields(dateSegments(widest)), Disabled: disabled}
	if typ == "time" {
		ctl.Kind = layout.ControlTime
	}
	fs := *partStyle(textSt)
	fs.Padding[css.Left], fs.Padding[css.Right] = css.Pts(css.Pt), css.Pts(css.Pt)
	node := &layout.Node{Kind: layout.Element, Style: st, Control: ctl}
	node.ID, _ = attr(n, "id")
	formRole(node, n, typeset.PrintText, false)
	for _, seg := range dateSegments(shown) {
		t := &layout.Node{Kind: layout.Text, Style: textSt, Text: seg.text}
		if seg.field {
			t = &layout.Node{Kind: layout.Element, Style: &fs, Children: []*layout.Node{{Kind: layout.Text, Style: &fs, Text: seg.text}}}
		}
		node.Children = append(node.Children, t)
	}
	return []*layout.Node{node}
}

// gauge builds a progress or a meter element styled st as the control
// the layout draws (HTML Standard, 15.5.18 and 15.5.19): an inline-block
// of the user agent's size showing its value, its content never shown.
func (b *builder) gauge(n *dom.Node, st *css.Style) []*layout.Node {
	ctl := &layout.Control{Kind: layout.ControlProgress, Value: progressValue(n)}
	if n.DataAtom == atom.Meter {
		ctl.Kind = layout.ControlMeter
		ctl.Value, ctl.Level = meterValue(n)
	}
	node := &layout.Node{Kind: layout.Element, Style: st, Control: ctl}
	node.ID, _ = attr(n, "id")
	return []*layout.Node{node}
}

// fileControl builds the file input n styled st (HTML Standard,
// 15.5.13): a button labelled "Choose File", "Choose Files" when it takes
// several, 4 pixels before the text "No file chosen", as Chromium shows
// an input no file was chosen for, the control as wide as 34 zeros.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 prints a file
//     input as a button "Choose File" (multiple: "Choose Files") with 1
//     by 6 pixels of padding inside a 2 pixel border, then 4 pixels, then
//     "No file chosen", the control 253 pixels wide at 13.333 pixels and
//     379 at 20, 34 times the width of 0 rounded up.
func (b *builder) fileControl(n *dom.Node, st *css.Style, disabled bool) []*layout.Node {
	label := "Choose File"
	if _, ok := attr(n, "multiple"); ok {
		label = "Choose Files"
	}
	bs := *partStyle(st)
	bs.Display = css.InlineBlock
	bs.BoxSizing = css.BorderBox
	bs.WhiteSpace = css.Pre
	bs.Margin = [4]css.Dimension{css.Pts(0), css.Pts(fileButtonGap * css.Pt), css.Pts(0), css.Pts(0)}
	bs.Padding = [4]css.Dimension{css.Pts(css.Pt), css.Pts(6 * css.Pt), css.Pts(css.Pt), css.Pts(6 * css.Pt)}
	face, text := css.Color{R: 0xef / 255.0, G: 0xef / 255.0, B: 0xef / 255.0, A: 1}, css.Color{A: 1}
	if st.DarkScheme {
		face, text = css.Color{R: 0x6b / 255.0, G: 0x6b / 255.0, B: 0x6b / 255.0, A: 1}, css.Color{R: 1, G: 1, B: 1, A: 1}
	}
	if disabled {
		text.A = 0.3
	}
	bs.BackgroundColor, bs.Color = face, text
	for side := range bs.Border {
		bs.Border[side] = css.Border{Width: 2 * css.Pt, Style: css.Outset, Color: css.Color{A: 1}}
	}
	bs.TextAlign = css.AlignCenter
	bs.Appearance = css.AppearanceAuto
	button := &layout.Node{Kind: layout.Element, Style: &bs, Control: &layout.Control{Kind: layout.ControlButton, Disabled: disabled}}
	button.Children = []*layout.Node{{Kind: layout.Text, Style: &bs, Text: label}}
	formRole(button, n, typeset.PrintPushButton, false)
	ts := *st
	ts.WhiteSpace = css.Pre
	node := &layout.Node{Kind: layout.Element, Style: st, Control: &layout.Control{Kind: layout.ControlFile, Disabled: disabled}}
	node.Children = []*layout.Node{button, {Kind: layout.Text, Style: &ts, Text: "No file chosen"}}
	node.ID, _ = attr(n, "id")
	return []*layout.Node{node}
}

// fileButtonGap is the room after a file input's button, in CSS pixels.
const fileButtonGap = 4

// The default sizes of the text entry widgets: an input's size, a
// textarea's cols and rows (HTML Standard, 4.10.5.3.2 and 4.10.11).
const (
	defaultInputSize    = 20
	defaultTextAreaCols = 20
	defaultTextAreaRows = 2
)

// maxControlChars bounds the characters a size, cols or rows attribute
// sizes a control by: 10,000. Anti-DoS guard, not spec-mandated: the
// attribute is the author's, and a page shows a hundred or so characters
// across.
const maxControlChars = 10_000

// positiveAttr returns the attribute key of n parsed with the rules for
// parsing non-negative integers (HTML Standard, 2.3.4.2), def when it is
// absent, fails or is zero (4.10.5.3.2, 4.10.11), at most
// [maxControlChars].
func positiveAttr(n *dom.Node, key string, def int) int {
	v, ok := attr(n, key)
	if !ok {
		return def
	}
	i, ok := parseInteger(v)
	if !ok || i <= 0 {
		return def
	}
	return min(i, maxControlChars)
}

// displaySize returns the display size of the select n (HTML Standard,
// 4.10.7): its size attribute, else 4 when it is multiple and 1 when not.
// A select of display size 1 is a drop-down box, any other a list box
// (15.5.16); a multiple select of size 1 is a drop-down box too, as
// Chromium prints it.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 prints a select
//     of multiple size="1" as a drop-down box showing its selected option.
func displaySize(n *dom.Node) int {
	_, multiple := attr(n, "multiple")
	def := 1
	if multiple {
		def = 4
	}
	return positiveAttr(n, "size", def)
}

// selectRow is one row a select lists: an option, or an optgroup's
// heading.
type selectRow struct {
	label    string
	heading  bool
	grouped  bool
	selected bool
	n        *dom.Node
}

// maxSelectRows bounds the rows of a select litho reads: 10,000.
// Anti-DoS guard, not spec-mandated: each is a box of a list box and a
// label shaped to size the select, and a form lists a few hundred at most.
const maxSelectRows = 10_000

// selectRows returns the rows of the select n in tree order (HTML
// Standard, 4.10.7, the list of options): its options and those of its
// optgroup children, each optgroup's label heading its options; at most
// [maxSelectRows].
func selectRows(n *dom.Node) []selectRow {
	var rows []selectRow
	add := func(o *dom.Node, grouped bool) {
		if len(rows) < maxSelectRows {
			_, sel := attr(o, "selected")
			rows = append(rows, selectRow{label: optionLabel(o), grouped: grouped, selected: sel, n: o})
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != dom.ElementNode || c.Namespace != "" {
			continue
		}
		switch c.DataAtom { //nolint:exhaustive // other children list nothing
		case atom.Option:
			add(c, false)
		case atom.Optgroup:
			if len(rows) < maxSelectRows {
				label, _ := attr(c, "label")
				rows = append(rows, selectRow{label: collapse(label), heading: true, n: c})
			}
			for o := c.FirstChild; o != nil; o = o.NextSibling {
				if o.Type == dom.ElementNode && o.Namespace == "" && o.DataAtom == atom.Option {
					add(o, true)
				}
			}
		}
	}
	return rows
}

// optionLabel returns the label of the option o (HTML Standard, 4.10.10):
// its label attribute when not empty, else its text, white space
// collapsed.
func optionLabel(o *dom.Node) string {
	if label, ok := attr(o, "label"); ok && label != "" {
		return label
	}
	return collapse(textContent(o))
}

// Chromium's list box, in CSS pixels and colours.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 prints the rows
//     of a list box with 2 pixels of padding either side and 1 below,
//     each at least 1.2 em tall, an option of an optgroup 20 pixels further
//     in, its optgroup's label bold, a selected option on #cecece (#545454
//     under color-scheme: dark), and the box on the text's bottom.
var (
	listRowPadding  = [4]css.Dimension{css.Pts(0), css.Pts(2 * css.Pt), css.Pts(1 * css.Pt), css.Pts(2 * css.Pt)}
	listGroupIndent = 20 * css.Pt
	listSelected    = css.Color{R: 0xce / 255.0, G: 0xce / 255.0, B: 0xce / 255.0, A: 1}
	// listSelectedDark marks a selected option under a dark colour
	// scheme.
	listSelectedDark = css.Color{R: 0x54 / 255.0, G: 0x54 / 255.0, B: 0x54 / 255.0, A: 1}
)

// listBox builds a select styled st shown as a list box whose display
// size is rows (HTML Standard, 15.5.16): an inline-block of one block a row, each option's
// label on a line of its own, an optgroup's label in bold above its
// options, the selected options marked, and empty rows below the last;
// the rows from the one that brings the last selected option into the
// box, as Chromium scrolls a list box to it. What its rows pass, the box
// clips ([layout.Control]).
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 prints a select
//     of size 3 whose fifth option is selected as its third to fifth
//     options; a multiple select of two options as two rows and two empty
//     ones.
func (b *builder) listBox(n *dom.Node, st *css.Style, rows int, disabled bool) []*layout.Node {
	all := selectRows(n)
	ctl := &layout.Control{Kind: layout.ControlListBox, Rows: rows, Disabled: disabled}
	first, last := 0, -1
	for i, r := range all {
		l := layout.ControlLabel{Text: r.label, Bold: r.heading}
		if r.grouped {
			l.Indent = listGroupIndent
		}
		ctl.Labels = append(ctl.Labels, l)
		if r.selected {
			last = i
		}
	}
	if last >= rows {
		first = last - rows + 1
	}
	box := st
	if st.VerticalAlign.Kind == css.Baseline {
		c := *st
		c.VerticalAlign = css.VerticalAlign{Kind: css.TextBottom}
		box = &c
	}
	node := &layout.Node{Kind: layout.Element, Style: box, Control: ctl}
	node.ID, _ = attr(n, "id")
	formRole(node, n, typeset.PrintList, false)
	for i := first; i < len(all) && i < first+rows; i++ {
		r := all[i]
		rs := b.listRowStyle(r, st)
		row := &layout.Node{Kind: layout.Element, Style: rs}
		if r.label != "" {
			row.Children = []*layout.Node{{Kind: layout.Text, Style: rs, Text: r.label}}
		} else {
			row.Children = []*layout.Node{{Kind: layout.LineBreak, Style: rs}}
		}
		node.Children = append(node.Children, row)
	}
	plain := b.listRowStyle(selectRow{}, st)
	for range rows - len(node.Children) {
		node.Children = append(node.Children, &layout.Node{Kind: layout.Element, Style: plain, Children: []*layout.Node{{Kind: layout.LineBreak, Style: plain}}})
	}
	return []*layout.Node{node}
}

// listRowStyle returns the style of the row r of a list box whose select
// is styled st: the computed style of its option or optgroup, the
// select's font and colour for an empty row, as a block on one line with
// the padding, least height and weight of Chromium's list box rows: an
// optgroup's label bold, an option in the select's weight (Chromium's
// option { font-weight: normal } under a bold optgroup); a selected option
// on Chromium's mark unless its own style gives it a background.
func (b *builder) listRowStyle(r selectRow, st *css.Style) *css.Style {
	base := st
	if r.n != nil {
		if os := b.computed.Style(r.n); os != nil {
			base = os
		}
	}
	c := *base
	if r.n == nil {
		c = *css.Initial()
		c.Color, c.FontFamilies, c.FontSize, c.FontWeight, c.FontStyle = st.Color, st.FontFamilies, st.FontSize, st.FontWeight, st.FontStyle
	}
	c.Display = css.Block
	c.WhiteSpace = css.Pre
	c.Padding = listRowPadding
	if r.grouped {
		c.Padding[css.Left] = css.Pts(float64(listRowPadding[css.Left].Value) + listGroupIndent)
	}
	c.MinHeight = css.Pts(float64(1.2 * c.FontSize))
	c.Width, c.Height = css.Dimension{Kind: css.Auto}, css.Dimension{Kind: css.Auto}
	switch {
	case r.heading && c.FontWeight < 600:
		c.FontWeight = 700
	case !r.heading:
		c.FontWeight = st.FontWeight
	}
	if r.selected && c.BackgroundColor.A == 0 {
		c.BackgroundColor = listSelected
		if st.DarkScheme {
			c.BackgroundColor = listSelectedDark
		}
	}
	return &c
}

// inputText returns the text an <input> shows.
func inputText(n *dom.Node) string {
	typ, _ := attr(n, "type")
	value, hasValue := attr(n, "value")
	switch strings.ToLower(strings.TrimSpace(typ)) {
	case "checkbox", "radio", "file", "range", "color", "image", "hidden":
		return ""
	case "password":
		return strings.Repeat("•", min(utf8.RuneCountInString(value), maxPasswordDots))
	case "submit":
		if !hasValue {
			return "Submit"
		}
	case "reset":
		if !hasValue {
			return "Reset"
		}
	}
	if value == "" {
		value, _ = attr(n, "placeholder")
	}
	return value
}

// maxPasswordDots bounds the dots a password field shows: 64. Anti-DoS
// guard, not spec-mandated: the value's length is the attacker's, and a
// field shows a few dozen characters.
const maxPasswordDots = 64

// selectText returns the label of a <select>'s first selected option, or
// of its first option.
func selectText(n *dom.Node) string {
	var first, selected *dom.Node
	walk(n, func(c *dom.Node) bool {
		if c.Type == dom.ElementNode && c.DataAtom == atom.Option {
			if first == nil {
				first = c
			}
			if _, ok := attr(c, "selected"); ok && selected == nil {
				selected = c
			}
			return false
		}
		return true
	})
	if selected == nil {
		selected = first
	}
	if selected == nil {
		return ""
	}
	if label, ok := attr(selected, "label"); ok && label != "" {
		return label
	}
	return collapse(textContent(selected))
}

// parseInteger parses an integer as the HTML Standard's rules for parsing
// integers do (2.3.4.1): leading whitespace, a sign, digits, anything
// after them ignored. The value is clamped to the range of a counter,
// [css.MinInteger] to [css.MaxInteger].
func parseInteger(s string) (int, bool) {
	s = strings.TrimLeft(s, "\t\n\f\r ")
	neg := false
	if s != "" && (s[0] == '-' || s[0] == '+') {
		neg = s[0] == '-'
		s = s[1:]
	}
	end := 0
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0, false
	}
	v, err := strconv.ParseInt(s[:end], 10, 64)
	if err != nil {
		// Only a value past the range of int64 fails: the digits are
		// checked above.
		v = math.MaxInt64
	}
	if neg {
		v = -v
	}
	return css.ClampInteger(v), true
}

// spanAttr returns a span attribute clamped to [1, maxSpan], 0 when absent
// or invalid (HTML Standard, 4.9.11).
func spanAttr(n *dom.Node, key string, maxSpan int) int {
	v, ok := attr(n, key)
	if !ok {
		return 0
	}
	i, ok := parseInteger(v)
	if !ok || i < 1 {
		return 0
	}
	return min(i, maxSpan)
}

// rowSpanAttr returns the rowspan attribute of the cell n, 0 for none: a
// rowspan of 0 spans the rest of the cell's row group (HTML Standard,
// 4.9.11, the cell grows downward), which the layout's grid clamps a
// span of maxRowSpan to.
func rowSpanAttr(n *dom.Node) int {
	if v, ok := attr(n, "rowspan"); ok {
		if i, ok := parseInteger(v); ok && i == 0 {
			return maxRowSpan
		}
	}
	return spanAttr(n, "rowspan", maxRowSpan)
}

// warnOnce records a warning the first time key is met with its code and
// effect.
func (b *builder) warnOnce(code warn.Code, effect warn.Effect, key, msg string) {
	k := code.String() + "\x00" + effect.String() + "\x00" + key
	if b.warned[k] {
		return
	}
	b.warned[k] = true
	b.warnings = append(b.warnings, typeset.Warning{Code: code, Effect: effect, Message: msg})
}

// maxFigureName caps the bytes of the name a figure's Figure carries.
// Anti-DoS guard, not spec-mandated: a caption is a sentence or two, and
// a reader is told the first few thousand characters of a longer one.
const maxFigureName = 4096

// derivedImageAlt returns what the document states of the img element n,
// which has no alt attribute, apart from alt and title: its aria-label
// (HTML-AAM 1.0, 4.1, the accessible name of img), else the caption of the
// figure it alone fills, a figure whose first figcaption child holds text
// and which holds, that figcaption aside, nothing but n, white space and
// comments: the case in which the HTML Standard, 4.8.4.4.11 lets an image
// go without alt because its figure's caption stands for it. "" for
// neither. An image beside other content in its figure, or inside another
// element of it, shares the caption with that content, and takes none.
func derivedImageAlt(n *dom.Node) string {
	if v, ok := attr(n, "aria-label"); ok {
		if v = collapse(v); v != "" {
			return bounded(v)
		}
	}
	f := n.Parent
	if f == nil || f.Type != dom.ElementNode || f.DataAtom != atom.Figure || f.Namespace != "" {
		return ""
	}
	var caption *dom.Node
	for c := f.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == dom.ElementNode && c.DataAtom == atom.Figcaption && c.Namespace == "" {
			caption = c
			break
		}
	}
	if caption == nil {
		return ""
	}
	for c := f.FirstChild; c != nil; c = c.NextSibling {
		switch {
		case c == caption || c == n || c.Type == dom.CommentNode:
		case c.Type == dom.TextNode && strings.Trim(c.Data, "\t\n\f\r ") == "":
		default:
			return ""
		}
	}
	return bounded(collapse(textContent(caption)))
}

// bounded returns s cut to [maxFigureName] bytes at a character.
func bounded(s string) string {
	for len(s) > maxFigureName {
		_, size := utf8.DecodeLastRuneInString(s)
		s = s[:len(s)-size]
	}
	return s
}

// figureName returns the name of a figure element, the Alt its Figure
// carries (ISO 14289-1, 7.3), as HTML-AAM computes a figure's accessible
// name: its aria-label, else the text of its first figcaption child,
// else its title; "" for none. A figure names its figcaption's text
// because a Figure's Alt replaces what it holds for a reader.
func figureName(n *dom.Node) string {
	name := ""
	if v, ok := attr(n, "aria-label"); ok {
		name = collapse(v)
	}
	for c := n.FirstChild; c != nil && name == ""; c = c.NextSibling {
		if c.Type == dom.ElementNode && c.DataAtom == atom.Figcaption && c.Namespace == "" {
			name = collapse(textContent(c))
			break
		}
	}
	if name == "" {
		v, _ := attr(n, "title")
		name = collapse(v)
	}
	for len(name) > maxFigureName {
		_, size := utf8.DecodeLastRuneInString(name)
		name = name[:len(name)-size]
	}
	return name
}
