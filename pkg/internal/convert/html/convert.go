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
	ranges, err := parsePageRanges(o.PageRanges)
	if err != nil {
		return nil, err
	}
	data, err := readCapped(src, o.MaxInputBytes)
	if err != nil {
		return nil, err
	}
	doc, err := parse(data)
	if err != nil {
		return nil, err
	}
	if o.SinglePage {
		// The route sizes the paper to the content it measures on screen,
		// then prints it (see [measureSinglePage]).
		w, h, err := measureSinglePage(ctx, doc, o, fonts, lay)
		if err != nil {
			return nil, err
		}
		if o.Landscape {
			o.PageWidth = min(w+o.MarginLeft+o.MarginRight, maxPageSide)
		}
		o.PageHeight = min(h+o.MarginTop+o.MarginBottom, maxPageSide)
	}
	p, err := prepareDoc(ctx, doc, o, fonts, newPrintSetup(o))
	if err != nil {
		return nil, err
	}
	res, err := layOut(ctx, lay, p, fonts)
	if err != nil {
		return nil, err
	}
	if s, again := p.refit(res); again {
		// The document is wider than its page area: lay it out again, its
		// styles too, since the viewport the media queries and the
		// viewport units measure changed (blink::LocalFrameView::
		// ForceLayoutForPagination).
		if p, err = prepareDoc(ctx, doc, o, fonts, s); err != nil {
			return nil, err
		}
		if res, err = layOut(ctx, lay, p, fonts); err != nil {
			return nil, err
		}
	}
	return p.assemble(res, ranges)
}

// layOut lays the prepared document p out with lay.
func layOut(ctx context.Context, lay layoutFunc, p *prepared, fonts *text.Fonts) (*layout.Result, error) {
	res, err := lay(ctx, p.root, fonts, p.layout)
	if err != nil {
		if errors.Is(err, layout.ErrTooManyPages) {
			return nil, fmt.Errorf("%w: %w", ErrTooManyPages, err)
		}
		return nil, fmt.Errorf("laying out the HTML document: %w", err)
	}
	return res, nil
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
	// o is the conversion's options, defaults set, setup how the page
	// boxes became the layout's and first the first page's box as the
	// @page rules give it, in points of the page.
	o     Options
	setup printSetup
	first layout.PageBox
	// svg collects the warnings of the SVG pictures, which the layout
	// decodes too.
	svg *svgState
}

// heading is an h1 to h6 of the content tree, for the outline.
type heading struct {
	node  *layout.Node
	level int
}

// parse parses the HTML source data.
func parse(data []byte) (*dom.Node, error) {
	doc, err := dom.Parse(strings.NewReader(decode(data)))
	if err != nil {
		// A strings.Reader never fails, so the parser fails only on its
		// own nesting cap.
		return nil, fmt.Errorf("%w: %w", ErrTooDeep, err)
	}
	return doc, nil
}

// prepareDoc cascades the stylesheets of the parsed document doc and
// builds its content tree, its pages laid out as setup says. fonts
// resolves the ex and ch units; nil takes their half-em fallback. It
// leaves doc as it is, so a second layout prepares it again.
func prepareDoc(ctx context.Context, doc *dom.Node, o Options, fonts *text.Fonts, setup printSetup) (*prepared, error) {
	scan, err := scanDocument(ctx, doc)
	if err != nil {
		return nil, err
	}
	res := newResources(o.Resources)
	res.svg.fonts = fonts
	c := &collector{res: res, base: scan.resourceBase, logger: o.Logger}
	sheets := c.stylesheets(ctx, scan.sheets)

	media := css.Media{Type: o.Media, Features: o.MediaFeatures}
	media.Width, media.Height = paperArea(o)
	resolver := css.NewResolver(media, sheets)
	// The direction that decides whether the first page is a left or a
	// right one is not known before the cascade: the viewport takes a
	// left-to-right document's.
	first, _ := pageBox(resolver, o, pageSel(0, false))
	setup, _ = setup.fitFactor(first)
	// The media features measure the paper's page area ([paperArea]) and
	// the viewport units the page area of the first page, the size and
	// margins @page sets (CSS Page 3, 3.1 and 5.1), both as the layout
	// sees them: scaled by the setup's factor, as Chromium's initial
	// containing block is
	// (blink::CalculateInitialContainingBlockSizeForPagination); the
	// screen measurement of a single page sees the screen (single.go).
	// Evaluate the media rules again when that changes anything.
	w, h := media.Width*setup.factor, media.Height*setup.factor
	vw, vh := areaOf(setup.layoutBox(first))
	if setup.measure {
		w, h, vw, vh = screenWidth, screenHeight, screenWidth, screenHeight
	}
	if w != media.Width || h != media.Height || vw != w || vh != h {
		media.Width, media.Height = w, h
		media.ViewportWidth, media.ViewportHeight = vw, vh
		resolver = css.NewResolver(media, sheets)
	}

	computed, err := resolver.Compute(ctx, doc, css.ComputeOptions{Hints: css.HTMLHints, Lang: scan.lang, BaseURL: scan.resourceBase, FontUnits: fontUnits(ctx, fonts)})
	if err != nil {
		return nil, fmt.Errorf("computing the HTML document's styles: %w", err)
	}
	rootStyle := computed.Style(scan.root)
	page, pageAt, pageWarns := pageBoxes(resolver, o, rootStyle != nil && rootStyle.Direction == css.RTL)
	boxes := []layout.PageBox{page}
	if pageAt != nil {
		boxes = append(boxes, pageAt(1), pageAt(2))
	}
	setup, fitWarns := setup.fitFactor(boxes...)
	pageWarns = append(pageWarns, fitWarns...)
	sourceFirst := page
	page = setup.layoutBox(page)
	if at := pageAt; at != nil {
		pageAt = func(i int) layout.PageBox { return setup.layoutBox(at(i)) }
	}

	b := &builder{
		computed: computed, res: res, scan: scan,
		warned: map[string]bool{}, omitBackgrounds: o.OmitBackgrounds,
	}
	root, err := b.buildRoot(ctx, scan.root)
	if err != nil {
		return nil, err
	}

	p := &prepared{
		root: root,
		layout: layout.Options{
			Page:         page,
			PageAt:       pageAt,
			FitFirstPage: setup.measure,
			Lang:         scan.lang,
			Image:        res.picture,
			MaxPages:     o.MaxPages,
			Logger:       o.Logger,
		},
		meta:     scan.meta,
		headings: b.headings,
		o:        o,
		setup:    setup,
		first:    sourceFirst,
		svg:      &res.svg,
	}
	for _, w := range computed.Warnings() {
		p.warnings = append(p.warnings, compose.Warning{Code: w.Code, Message: displayURL(w.Message)})
	}
	p.warnings = append(p.warnings, c.warnings...)
	p.warnings = append(p.warnings, pageWarns...)
	p.warnings = append(p.warnings, b.warnings...)
	return p, nil
}

// assemble returns the document of the layout res: the pages ranges
// selects, or the first alone for a single page, each placed on its sheet
// (see [prepared.placement]), the links, the outline and the structure
// tree kept for them. Page numbers count the whole layout before the
// selection: a page keeps the number it was laid out with in what it
// shows, and the selection only drops pages.
func (p *prepared) assemble(res *layout.Result, ranges []pageRange) (*compose.Document, error) {
	keep := []int{0}
	if !p.o.SinglePage {
		var err error
		if keep, err = selectPages(ranges, len(res.Pages)); err != nil {
			return nil, err
		}
	}
	// at maps a page of the layout to its 1-based number in the output,
	// 0 for a page left out; places holds each kept page's placement.
	at := make([]int, len(res.Pages))
	places := make([]placement, len(res.Pages))
	for i, k := range keep {
		at[k] = i + 1
		places[k] = p.placement(res.Pages[k])
	}
	dest := func(d compose.Dest) (compose.Dest, bool) {
		if d.Page < 1 || d.Page > len(at) || at[d.Page-1] == 0 {
			return d, false
		}
		k := d.Page - 1
		d.Page = at[k]
		d.X, d.Y = places[k].point(d.X, d.Y)
		return d, true
	}
	doc := &compose.Document{
		Metadata:  p.meta,
		Structure: res.Structure,
		Warnings:  p.warnings,
	}
	for _, k := range keep {
		pg := places[k].page(res.Pages[k])
		links := pg.Links[:0:0]
		for _, l := range pg.Links {
			if l.Dest != nil {
				d, ok := dest(*l.Dest)
				if !ok {
					continue
				}
				l.Dest = &d
			}
			links = append(links, l)
		}
		pg.Links = links
		doc.Pages = append(doc.Pages, pg)
	}
	if !p.o.OmitOutline {
		anchors := make(map[string]compose.Dest, len(res.Anchors))
		for id, d := range res.Anchors {
			if d, ok := dest(d); ok {
				anchors[id] = d
			}
		}
		doc.Outline = outline(p.headings, anchors)
	}
	if len(keep) < len(res.Pages) {
		pruneStructure(doc)
	}
	if p.svg != nil {
		doc.Warnings = append(doc.Warnings, p.svg.warnings()...)
	}
	for _, w := range res.Warnings {
		w.Message = displayURL(w.Message)
		if w.Page > 0 && w.Page <= len(at) {
			w.Page = at[w.Page-1]
		}
		doc.Warnings = append(doc.Warnings, w)
	}
	return doc, nil
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
