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
// Standard, 4.12.2). head, title, style, script and datalist are not
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
// warning: embedded documents and media. Immutable lookup table.
var unsupported = map[atom.Atom]bool{
	atom.Iframe: true, atom.Embed: true, atom.Video: true, atom.Audio: true, atom.Frame: true, atom.Frameset: true,
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
	// defDocs holds the drawing of each svg element a clip-path or a mask
	// reached, nil for one that does not parse; the layout reads it
	// through defsMu.
	defDocs map[*dom.Node]*svg.Document
	defsMu  sync.Mutex
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
	before := b.pseudo(ctx, n, css.PseudoBefore)
	if before != nil {
		b.onFirstLine(before, n, css.PseudoBefore, true)
		node.Children = append(node.Children, before)
	}
	kids, err := b.children(ctx, n, st, depth)
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
		// MathML draws as its text, which is what a reader needs most.
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
		case atom.Input, atom.Select, atom.Textarea:
			return b.control(n, st), nil
		case atom.Button:
			b.warnOnce(warn.FormWidgetOmitted, warn.Approximated, "form", "form controls are drawn as their text, without their widgets")
		case atom.Object:
			b.warnOnce(warn.ContentUnsupported, warn.Approximated, n.Data, "the content of <object> is not drawn; its fallback content is")
			return b.children(ctx, n, parent, depth)
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
	if err := b.content(ctx, node, n, st, v, depth+1); err != nil {
		return nil, err
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
	case css.LowerGreek, css.Armenian, css.Georgian, css.ListString, css.ListNamed:
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
// the document itself, "" for none or a script, and otherwise the URL as
// the author wrote it, resolved against the document's <base> when it has
// one (HTML Standard, 4.6.4).
func (b *builder) linkTarget(href string) string {
	href = strings.Trim(href, "\t\n\f\r ")
	if frag, ok := strings.CutPrefix(href, "#"); ok {
		if frag == "" {
			return ""
		}
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
			return emptyImage(n, st, failed)
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
	node := &layout.Node{Kind: layout.Replaced, Style: st, Picture: pic}
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
		}
	}
	return []*layout.Node{node}
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
// select's selected option, a textarea's text.
func (b *builder) control(n *dom.Node, st *css.Style) []*layout.Node {
	b.warnOnce(warn.FormWidgetOmitted, warn.Approximated, "form", "form controls are drawn as their text, without their widgets")
	var s string
	switch n.DataAtom { //nolint:exhaustive // every other element takes the default path
	case atom.Input:
		s = inputText(n)
	case atom.Select:
		s = selectText(n)
	default:
		s = textContent(n)
	}
	node := &layout.Node{Kind: layout.Element, Style: st}
	node.ID, _ = attr(n, "id")
	if s != "" {
		node.Children = []*layout.Node{{Kind: layout.Text, Style: st, Text: s}}
	}
	return []*layout.Node{node}
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
