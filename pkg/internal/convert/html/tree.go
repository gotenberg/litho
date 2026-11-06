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

	elements   int
	synthetic  int
	headings   []heading
	warnings   []compose.Warning
	warned     map[string]bool
	listValues map[*dom.Node]int
}

// buildRoot builds the content tree of the root element.
func (b *builder) buildRoot(ctx context.Context, root *dom.Node) (*layout.Node, error) {
	st := b.computed.Style(root)
	if st == nil {
		st = css.Initial()
	}
	n := &layout.Node{Kind: layout.Element, Style: st}
	if st.Display == css.DisplayNone {
		return n, nil
	}
	kids, err := b.children(ctx, root, st, 1)
	n.Children = kids
	return n, err
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
			b.warnOnce(CodeUnsupportedElement, "svg", "inline SVG is not drawn yet")
			return nil, nil
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
	if depth >= maxTreeDepth {
		b.warnOnce(CodeTooDeep, "depth", fmt.Sprintf("content nested deeper than %d elements was left out", maxTreeDepth))
		return nil, nil
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
			b.warnOnce(CodeFormControl, "form", "form controls are drawn as their text, without their widgets")
		case atom.Object:
			b.warnOnce(CodeUnsupportedElement, n.Data, "the content of <object> is not drawn; its fallback content is")
			return b.children(ctx, n, parent, depth)
		case atom.Canvas:
			// Without scripts a canvas shows its fallback content (HTML
			// Standard, 4.12.5).
			return b.children(ctx, n, parent, depth)
		case atom.Ol:
			b.numberList(n)
		}
	}
	node := &layout.Node{Kind: layout.Element, Style: st}
	b.semantics(node, n)
	kids, err := b.children(ctx, n, st, depth+1)
	if err != nil {
		return nil, err
	}
	node.Children = kids
	return []*layout.Node{node}, nil
}

// semantics sets what node says about the element n beyond its style: its
// role, language, anchor, link, list value and spans.
func (b *builder) semantics(node *layout.Node, n *dom.Node) {
	if n.Namespace != "" {
		return
	}
	node.Role = roles[n.DataAtom]
	if lang, ok := attr(n, "lang"); ok {
		node.Lang = strings.TrimSpace(lang)
	}
	node.ID, _ = attr(n, "id")
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
	case atom.Li:
		if v, ok := attr(n, "value"); ok {
			if i, ok := parseInteger(v); ok {
				node.ListValue = i
			}
		}
		if v, ok := b.listValues[n]; ok && node.ListValue == 0 {
			node.ListValue = v
		}
	case atom.Td, atom.Th:
		node.ColSpan = spanAttr(n, "colspan", maxColSpan)
		node.RowSpan = spanAttr(n, "rowspan", maxRowSpan)
	case atom.Col, atom.Colgroup:
		node.Span = spanAttr(n, "span", maxColSpan)
	case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
		if node.ID == "" {
			node.ID = b.syntheticID()
		}
		b.headings = append(b.headings, heading{node: node, level: int(n.Data[1] - '0')})
	}
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

// image builds the nodes of an <img> styled st: a replaced node with its
// picture, or, when the picture cannot be drawn, its alt text with a
// warning.
func (b *builder) image(ctx context.Context, n *dom.Node, st, parent *css.Style) []*layout.Node {
	alt, hasAlt := attr(n, "alt")
	src, _ := attr(n, "src")
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

// numberList records the ordinals of an <ol>'s li children that its start
// and reversed attributes decide (HTML Standard, 4.4.8 and 4.4.5). A
// child li with a value attribute keeps it and numbering goes on from it.
func (b *builder) numberList(ol *dom.Node) {
	start, hasStart := 0, false
	if v, ok := attr(ol, "start"); ok {
		start, hasStart = parseInteger(v)
	}
	_, reversed := attr(ol, "reversed")
	var items []*dom.Node
	for c := ol.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == dom.ElementNode && c.DataAtom == atom.Li && c.Namespace == "" {
			if st := b.computed.Style(c); st != nil && st.Display == css.ListItem {
				items = append(items, c)
			}
		}
	}
	if len(items) == 0 || !reversed && (!hasStart || start == 1) {
		return
	}
	if b.listValues == nil {
		b.listValues = map[*dom.Node]int{}
	}
	if !reversed {
		if _, ok := attr(items[0], "value"); !ok {
			b.listValues[items[0]] = start
		}
		return
	}
	if !hasStart {
		start = min(len(items), maxListValue)
	}
	cur := start
	for _, li := range items {
		if v, ok := attr(li, "value"); ok {
			if i, ok := parseInteger(v); ok {
				cur = i
			}
		}
		b.listValues[li] = cur
		cur = max(cur-1, -maxListValue)
	}
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
