package html

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	dom "golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
)

// maxListValue bounds a list ordinal read from an attribute: 2^30, the
// largest marker value the layout engine numbers. Anti-DoS guard, not
// spec-mandated: the HTML Standard allows any integer.
const maxListValue = 1 << 30

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
	// The engine writes the Lbl and LBody of a list item.
	atom.Li: "LI", atom.Dt: "LI", atom.Dd: "LI",
	atom.Blockquote: "BlockQuote",
	atom.Pre:        "Code", atom.Code: "Code", atom.Kbd: "Code", atom.Samp: "Code",
	atom.Q: "Quote", atom.Cite: "Quote",
	atom.Figure: "Figure", atom.Figcaption: "Caption",
	atom.Table: "Table", atom.Thead: "THead", atom.Tbody: "TBody", atom.Tfoot: "TFoot",
	atom.Tr: "TR", atom.Th: "TH", atom.Td: "TD", atom.Caption: "Caption",
	atom.Span: "Span", atom.Em: "Span", atom.Strong: "Span", atom.B: "Span", atom.I: "Span",
	atom.U: "Span", atom.Small: "Span", atom.Sub: "Span", atom.Sup: "Span", atom.Abbr: "Span",
	atom.Mark: "Span",
}

// skipped are the elements that draw nothing in a static rendering, with
// their content: metadata, scripts, inert templates, and noscript, which a
// browser running scripts hides (HTML Standard, 4.12.2). Immutable lookup
// table.
var skipped = map[atom.Atom]bool{
	atom.Head: true, atom.Script: true, atom.Style: true, atom.Template: true, atom.Noscript: true,
	atom.Title: true, atom.Meta: true, atom.Link: true, atom.Base: true,
	atom.Source: true, atom.Track: true, atom.Param: true, atom.Datalist: true, atom.Area: true,
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
	warnings  []compose.Warning
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
	n := &layout.Node{Kind: layout.Element, Style: st, Lang: rootLanguage(root)}
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
	if isListItem(st.Display) {
		node.ListValue = v
		node.Marker = b.marker(ctx, n, st, v)
	}
	b.counters.open()
	defer b.counters.close()
	before := b.pseudo(ctx, n, css.PseudoBefore)
	if before != nil {
		node.Children = append(node.Children, before)
	}
	kids, err := b.children(ctx, n, st, depth)
	if err != nil {
		return err
	}
	node.Children = append(node.Children, kids...)
	after := b.pseudo(ctx, n, css.PseudoAfter)
	if after != nil {
		node.Children = append(node.Children, after)
	}
	if len(st.StringSet) > 0 {
		b.setStrings(n, node, st, before, after)
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
		b.warnOnce(CodeUnsupportedElement, n.Data, fmt.Sprintf("the content of <%s> is not drawn", n.Data))
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
	if b.omitBackgrounds && n.Namespace == "" && n.DataAtom == atom.Body {
		st = withoutBackground(st)
	}
	if depth >= maxTreeDepth {
		b.warnOnce(CodeTooDeep, "depth", fmt.Sprintf("content nested deeper than %d elements was left out", maxTreeDepth))
		return nil, nil
	}
	v := b.applyCounters(n, st)
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
			b.warnOnce(CodeFormControl, "form", "form controls are drawn as their text, without their widgets")
		case atom.Object:
			b.warnOnce(CodeUnsupportedElement, n.Data, "the content of <object> is not drawn; its fallback content is")
			return b.children(ctx, n, parent, depth)
		case atom.Canvas:
			// Without scripts a canvas shows its fallback content (HTML
			// Standard, 4.12.5).
			return b.children(ctx, n, parent, depth)
		}
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
		if keep := b.runAway(node, st); keep != nil {
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
		node.RowSpan = spanAttr(n, "rowspan", maxRowSpan)
		if n.DataAtom == atom.Th {
			node.Scope = headerScope(n)
		}
	case atom.Ul, atom.Ol:
		if node.Style != nil {
			node.Numbering = listNumbering(node.Style.ListStyleType, n.DataAtom == atom.Ol)
		}
	case atom.Dl:
		node.Numbering = compose.NumberingDescription
	case atom.Col, atom.Colgroup:
		node.Span = spanAttr(n, "span", maxColSpan)
	case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
		if node.ID == "" {
			node.ID = b.syntheticID()
		}
		b.headings = append(b.headings, heading{node: node, level: int(n.Data[1] - '0')})
	}
}

// headerScope returns the cells a th heads (ISO 32000-1, 14.8.5.7, Table
// 349): its scope attribute's (HTML Standard, 4.9.10), else its columns'
// in a thead, its row's when it starts a row that holds a td, and its
// columns' otherwise, as the HTML table model's auto state assigns a
// header cell to the cells right of it and below it (HTML Standard,
// 4.9.12.2).
func headerScope(n *dom.Node) compose.TableScope {
	if v, ok := attr(n, "scope"); ok {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "row", "rowgroup":
			return compose.ScopeRow
		case "col", "colgroup":
			return compose.ScopeColumn
		}
	}
	tr := n.Parent
	if tr == nil || tr.DataAtom != atom.Tr {
		return compose.ScopeColumn
	}
	if g := tr.Parent; g != nil && g.DataAtom == atom.Thead {
		return compose.ScopeColumn
	}
	first, data := true, false
	for c := tr.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != dom.ElementNode {
			continue
		}
		if c == n && !first {
			return compose.ScopeColumn
		}
		first = false
		if c.DataAtom == atom.Td {
			data = true
		}
	}
	if data {
		return compose.ScopeRow
	}
	return compose.ScopeColumn
}

// listNumbering returns the numbering system of a list whose items' marker
// is t (ISO 32000-1, 14.8.5.5, Table 347; CSS Counter Styles 3, 6),
// ordered for an ol.
func listNumbering(t css.ListStyleType, ordered bool) compose.ListNumbering {
	switch t {
	case css.Disc:
		return compose.NumberingDisc
	case css.Circle:
		return compose.NumberingCircle
	case css.Square:
		return compose.NumberingSquare
	case css.Decimal, css.DecimalLeadingZero:
		return compose.NumberingDecimal
	case css.LowerRoman:
		return compose.NumberingLowerRoman
	case css.UpperRoman:
		return compose.NumberingUpperRoman
	case css.LowerAlpha:
		return compose.NumberingLowerAlpha
	case css.UpperAlpha:
		return compose.NumberingUpperAlpha
	case css.ListNone:
		return compose.NumberingNone
	case css.LowerGreek, css.Armenian, css.Georgian, css.ListString:
	}
	if ordered {
		return compose.NumberingOrdered
	}
	return compose.NumberingUnordered
}

// syntheticID returns an anchor ID no element of the document carries, for
// a heading the outline points at.
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
// drawn, its alt text with a warning.
func (b *builder) image(ctx context.Context, n *dom.Node, st, parent *css.Style) []*layout.Node {
	alt, hasAlt := attr(n, "alt")
	src, density := b.imageSource(n)
	fallback := func() []*layout.Node {
		if alt == "" {
			return nil
		}
		return []*layout.Node{{Kind: layout.Text, Style: parent, Text: alt}}
	}
	if strings.Trim(src, "\t\n\f\r ") == "" {
		return fallback()
	}
	pic, err := b.res.picture(ctx, resolveRef(b.scan.resourceBase, src))
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		b.warnings = append(b.warnings, compose.Warning{Code: warningCode(err), Message: fmt.Sprintf(
			"the image %s was left out: %v", displayURL(strings.TrimSpace(src)), err)})
		return fallback()
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

// control builds a form control styled st as an inline-block holding the
// text it shows (HTML Standard, 15.5): an input's value or placeholder, a
// select's selected option, a textarea's text.
func (b *builder) control(n *dom.Node, st *css.Style) []*layout.Node {
	b.warnOnce(CodeFormControl, "form", "form controls are drawn as their text, without their widgets")
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
// after them ignored. The value is clamped to ±maxListValue.
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
	if err != nil || v > maxListValue {
		v = maxListValue
	}
	if neg {
		v = -v
	}
	return int(v), true // fits int: clamped to ±2^30 above
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

// warnOnce records a warning the first time key is met.
func (b *builder) warnOnce(code, key, msg string) {
	if b.warned[code+"\x00"+key] {
		return
	}
	b.warned[code+"\x00"+key] = true
	b.warnings = append(b.warnings, compose.Warning{Code: code, Message: msg})
}
