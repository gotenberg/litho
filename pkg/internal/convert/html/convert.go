package html

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	dom "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
	"golang.org/x/net/html/charset"
	"golang.org/x/text/encoding"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/text"
)

// layoutFunc is the layout engine [Convert] calls.
type layoutFunc func(ctx context.Context, root *layout.Node, fonts *text.Fonts, opts layout.Options) (*layout.Result, error)

// Convert lays the HTML document src out as a Document.
//
// It reads src to its end, at most o.MaxInputBytes bytes, and returns an
// error wrapping [ErrInputTooLarge] past them; src is never closed. It
// returns an error wrapping [ErrInvalidOptions] for opts out of range or a
// nil fonts, [text.ErrNoFonts] when fonts indexed no face,
// [ErrTooManyElements] for a document of more than [MaxElements] elements,
// [ErrTooDeep] for one nested deeper than the parser follows,
// [css.ErrTooComplex] for a cascade past its work cap,
// [layout.ErrTooManyBoxes] for a layout past its box cap,
// [ErrTooManyPages] when the layout needs more than o.MaxPages pages, and
// ctx's error when ctx ends first.
//
// The document always has at least one page. Its output depends on the
// bytes of src, opts, the files of opts.Resources it reads and the faces
// fonts indexed alone. See the package documentation for what is drawn.
func Convert(ctx context.Context, src io.Reader, fonts *text.Fonts, opts Options) (*compose.Document, error) {
	return convert(ctx, src, fonts, opts, layout.Layout)
}

// convert is [Convert] with the layout engine as a parameter, for tests.
func convert(ctx context.Context, src io.Reader, fonts *text.Fonts, opts Options, lay layoutFunc) (*compose.Document, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	o, err := opts.withDefaults()
	if err != nil {
		return nil, err
	}
	if fonts == nil {
		return nil, fmt.Errorf("%w: no font index", ErrInvalidOptions)
	}
	if fonts.Len() == 0 {
		return nil, fmt.Errorf("converting HTML: %w", text.ErrNoFonts)
	}
	data, err := readCapped(src, o.MaxInputBytes)
	if err != nil {
		return nil, err
	}
	p, err := prepare(ctx, data, o, fonts)
	if err != nil {
		return nil, err
	}
	res, err := lay(ctx, p.root, fonts, p.layout)
	if err != nil {
		if errors.Is(err, layout.ErrTooManyPages) {
			return nil, fmt.Errorf("%w: %w", ErrTooManyPages, err)
		}
		return nil, fmt.Errorf("laying out the HTML document: %w", err)
	}
	return p.assemble(res), nil
}

// readCapped reads src to its end, failing with [ErrInputTooLarge] past max
// bytes.
func readCapped(src io.Reader, maxBytes int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(src, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading the HTML source: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("%w: the source is longer than %d bytes", ErrInputTooLarge, maxBytes)
	}
	return data, nil
}

// decode returns data as UTF-8: the encoding of a byte order mark, else
// the one a <meta charset> or <meta http-equiv=Content-Type> in the first
// 1024 bytes names (HTML Standard, 13.2.3.2, the prescan), else UTF-8. The
// HTML Standard falls back on a locale-dependent legacy encoding;
// litho picks UTF-8, what every document written this century uses, so
// the output does not depend on where it runs. An invalid sequence reads
// as U+FFFD.
func decode(data []byte) string {
	enc, name, certain := charset.DetermineEncoding(data, "")
	if !certain && name == "windows-1252" && !declaresCharset(data) {
		// DetermineEncoding's own fallback, not a declaration.
		enc = encoding.Nop
	}
	if enc != encoding.Nop && enc != nil {
		// The decoders replace what they cannot map and never fail on
		// bytes; a BOM is consumed by the decoder of its encoding.
		if out, err := enc.NewDecoder().Bytes(data); err == nil {
			data = out
		}
	}
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	if utf8.Valid(data) {
		return string(data)
	}
	return strings.ToValidUTF8(string(data), "�")
}

// declaresCharset reports whether the prescan window names a charset, the
// only way the prescan returns an encoding.
func declaresCharset(data []byte) bool {
	return bytes.Contains(bytes.ToLower(data[:min(len(data), 1024)]), []byte("charset"))
}

// prepared is a parsed and styled document ready for layout.
type prepared struct {
	root     *layout.Node
	layout   layout.Options
	meta     compose.Metadata
	warnings []compose.Warning
	headings []heading
}

// heading is an h1 to h6 of the content tree, for the outline.
type heading struct {
	node  *layout.Node
	level int
}

// prepare parses data, cascades its stylesheets and builds the content
// tree.
func prepare(ctx context.Context, data []byte, o Options, fonts *text.Fonts) (*prepared, error) {
	doc, err := dom.Parse(strings.NewReader(decode(data)))
	if err != nil {
		// A strings.Reader never fails, so the parser fails only on its
		// own nesting cap.
		return nil, fmt.Errorf("%w: %w", ErrTooDeep, err)
	}
	return prepareDoc(ctx, doc, o, fonts)
}

// prepareDoc cascades the stylesheets of the parsed document doc and
// builds its content tree. fonts resolves the ex and ch units; nil takes
// their half-em fallback.
func prepareDoc(ctx context.Context, doc *dom.Node, o Options, fonts *text.Fonts) (*prepared, error) {
	scan, err := scanDocument(ctx, doc)
	if err != nil {
		return nil, err
	}
	res := newResources(o.Resources)
	c := &collector{res: res, base: scan.resourceBase, logger: o.Logger}
	sheets := c.stylesheets(ctx, scan.sheets)

	media := css.Media{Type: o.Media, Features: o.MediaFeatures}
	media.Width, media.Height = paperArea(o)
	resolver := css.NewResolver(media, sheets)
	// The direction that decides whether the first page is a left or a
	// right one is not known before the cascade: the viewport takes a
	// left-to-right document's.
	first, _ := pageBox(resolver, o, pageSel(0, false))
	if w, h := areaOf(first); w != media.Width || h != media.Height {
		// The viewport units measure the page area of the first page, the
		// size and margins @page sets (CSS Page 3, 3.1 and 5.1), while the
		// media features keep the paper's ([paperArea]).
		media.ViewportWidth, media.ViewportHeight = w, h
		resolver = css.NewResolver(media, sheets)
	}

	computed, err := resolver.Compute(ctx, doc, css.ComputeOptions{Hints: css.HTMLHints, Lang: scan.lang, BaseURL: scan.resourceBase, FontUnits: fontUnits(ctx, fonts)})
	if err != nil {
		return nil, fmt.Errorf("computing the HTML document's styles: %w", err)
	}
	rootStyle := computed.Style(scan.root)
	page, pageAt, pageWarns := pageBoxes(resolver, o, rootStyle != nil && rootStyle.Direction == css.RTL)

	b := &builder{
		computed: computed, res: res, scan: scan,
		warned: map[string]bool{},
	}
	root, err := b.buildRoot(ctx, scan.root)
	if err != nil {
		return nil, err
	}

	p := &prepared{
		root: root,
		layout: layout.Options{
			Page:     page,
			PageAt:   pageAt,
			Lang:     scan.lang,
			Image:    res.picture,
			MaxPages: o.MaxPages,
			Logger:   o.Logger,
		},
		meta:     scan.meta,
		headings: b.headings,
	}
	for _, w := range computed.Warnings() {
		p.warnings = append(p.warnings, compose.Warning{Code: w.Code, Message: displayURL(w.Message)})
	}
	p.warnings = append(p.warnings, c.warnings...)
	p.warnings = append(p.warnings, pageWarns...)
	p.warnings = append(p.warnings, b.warnings...)
	return p, nil
}

// assemble returns the document of the layout res.
func (p *prepared) assemble(res *layout.Result) *compose.Document {
	doc := &compose.Document{
		Pages:     res.Pages,
		Metadata:  p.meta,
		Structure: res.Structure,
		Outline:   outline(p.headings, res.Anchors),
		Warnings:  p.warnings,
	}
	for _, w := range res.Warnings {
		w.Message = displayURL(w.Message)
		doc.Warnings = append(doc.Warnings, w)
	}
	return doc
}

// scan is what one pass over the document finds before styling.
type scan struct {
	root *dom.Node
	// lang is the root element's language, "" for unknown.
	lang string
	meta compose.Metadata
	// sheets lists the <style> and <link> elements in document order.
	sheets []*dom.Node
	// ids holds every id attribute and a[name] of the document.
	ids map[string]bool
	// resourceBase resolves resource references: the document's URL, or
	// the relative <base href> resolved against it. linkBase resolves
	// links, "" without a <base>.
	resourceBase, linkBase string
}

// scanDocument walks doc once, counting its elements against
// [MaxElements] and collecting the root, the metadata, the stylesheet
// elements, the ids and the base URL.
func scanDocument(ctx context.Context, doc *dom.Node) (*scan, error) {
	s := &scan{ids: map[string]bool{}, resourceBase: documentURL}
	seenBase := false
	elements := 0
	var walkErr error
	walk(doc, func(n *dom.Node) bool {
		if n.Type != dom.ElementNode {
			return true
		}
		elements++
		if elements > MaxElements {
			walkErr = fmt.Errorf("%w: more than %d elements", ErrTooManyElements, MaxElements)
			return false
		}
		if elements%1024 == 0 {
			if err := ctx.Err(); err != nil {
				walkErr = err
				return false
			}
		}
		if id, ok := attr(n, "id"); ok && id != "" {
			s.ids[id] = true
		}
		if n.Namespace != "" {
			return true
		}
		switch n.DataAtom { //nolint:exhaustive // every other element takes the default path
		case atom.Html:
			if s.root == nil {
				s.root = n
				s.lang, _ = attr(n, "lang")
				s.lang = strings.TrimSpace(s.lang)
			}
		case atom.A:
			if name, ok := attr(n, "name"); ok && name != "" {
				s.ids[name] = true
			}
		case atom.Template:
			// A template's content is inert (HTML Standard, 4.12.3).
			return false
		case atom.Title:
			if s.meta.Title == "" {
				s.meta.Title = collapse(textContent(n))
			}
		case atom.Meta:
			s.metaElement(n)
		case atom.Base:
			if href, ok := attr(n, "href"); ok && !seenBase {
				// Only the first <base href> counts (HTML Standard,
				// 4.2.3).
				seenBase = true
				s.setBase(href)
			}
		case atom.Style, atom.Link:
			s.sheets = append(s.sheets, n)
		}
		return true
	})
	if walkErr != nil {
		return nil, walkErr
	}
	s.meta.Lang = s.lang
	if s.root == nil {
		// The parser always makes an html element; a document without one
		// cannot come out of it.
		return nil, fmt.Errorf("parsing the HTML source: %w: no root element", ErrInvalidOptions)
	}
	return s, nil
}

// metaElement reads the document metadata a <meta name> carries (HTML
// Standard, 4.2.5.1).
func (s *scan) metaElement(n *dom.Node) {
	name, _ := attr(n, "name")
	content, ok := attr(n, "content")
	if !ok {
		return
	}
	content = collapse(content)
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "author":
		if s.meta.Author == "" {
			s.meta.Author = content
		}
	case "description":
		if s.meta.Subject == "" {
			s.meta.Subject = content
		}
	case "keywords":
		if s.meta.Keywords == "" {
			s.meta.Keywords = content
		}
	}
}

// setBase records the document's base URL (HTML Standard, 4.2.3). A
// relative one moves the resources' base inside the resources root; an
// absolute one resolves links alone, since no absolute URL is fetched.
func (s *scan) setBase(href string) {
	u := resolveRef(documentURL, href)
	if u == "" {
		return
	}
	s.linkBase = u
	if !isAbsolute(u) {
		s.resourceBase = u
	}
}

// isAbsolute reports a URL with a scheme or a host.
func isAbsolute(u string) bool {
	return strings.HasPrefix(u, "//") || hasScheme(u)
}

// hasScheme reports a URL that starts with a scheme (RFC 3986, 3.1).
func hasScheme(u string) bool {
	for i := 0; i < len(u); i++ {
		c := u[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		case i > 0 && (c >= '0' && c <= '9' || c == '+' || c == '-' || c == '.'):
		case c == ':':
			return i > 0
		default:
			return false
		}
	}
	return false
}

// walk visits n and its descendants in document order without recursion,
// skipping the descendants of a node for which visit returns false.
func walk(n *dom.Node, visit func(*dom.Node) bool) {
	stack := []*dom.Node{n}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if !visit(cur) {
			continue
		}
		mark := len(stack)
		for c := cur.FirstChild; c != nil; c = c.NextSibling {
			stack = append(stack, c)
		}
		// Children pop in document order.
		for i, j := mark, len(stack)-1; i < j; i, j = i+1, j-1 {
			stack[i], stack[j] = stack[j], stack[i]
		}
	}
}

// attr returns the value of n's attribute key, in no namespace.
func attr(n *dom.Node, key string) (string, bool) {
	for _, a := range n.Attr {
		if a.Namespace == "" && a.Key == key {
			return a.Val, true
		}
	}
	return "", false
}

// textContent returns the text of n's descendants in document order.
func textContent(n *dom.Node) string {
	var b strings.Builder
	walk(n, func(c *dom.Node) bool {
		if c.Type == dom.TextNode {
			b.WriteString(c.Data)
		}
		return true
	})
	return b.String()
}

// collapse trims s and collapses its runs of ASCII whitespace to one
// space (HTML Standard, 2.3, strip and collapse ASCII whitespace).
func collapse(s string) string {
	return strings.Join(strings.FieldsFunc(s, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\f' || r == '\r'
	}), " ")
}

// fontUnitSize is the font size fontUnits measures at: the ratios do not
// depend on it.
const fontUnitSize = 100

// fontUnits returns the [css.ComputeOptions.FontUnits] of fonts: the
// x-height of the face a style selects and the advance of its "0" (CSS
// Values 4, 6.1.1), nil without fonts.
func fontUnits(ctx context.Context, fonts *text.Fonts) func(*css.Style) (float64, float64) {
	if fonts == nil {
		return nil
	}
	return func(st *css.Style) (float64, float64) {
		ts := text.Style{Families: text.BrowserFamilies(st.FontFamilies), Weight: st.FontWeight, Italic: st.FontStyle != css.FontNormal, Size: fontUnitSize}
		fc := fonts.Match(ts)
		if fc == nil {
			return 0, 0
		}
		ex := fc.Metrics(fontUnitSize).XHeight / fontUnitSize
		ch := 0.0
		if p, _, err := fonts.Shape(ctx, []text.ParagraphRun{{Text: "0", Style: ts}}, text.ParagraphOptions{}); err == nil && p.Len() == 1 {
			ch = p.Advance(0) / fontUnitSize
		}
		return ex, ch
	}
}
