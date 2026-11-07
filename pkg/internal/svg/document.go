package svg

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"

	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/text"
)

// Errors [Parse] and [FromNode] return.
var (
	// ErrNotSVG reports a source without an svg root element.
	ErrNotSVG = errors.New("svg: no svg root element")
	// ErrTooLarge reports a source past the size cap.
	ErrTooLarge = fmt.Errorf("svg: source larger than %d bytes", maxSourceBytes)
)

// Raster is a decoded raster picture an image element draws.
type Raster struct {
	Source *compose.ImageSource
	// Width and Height are the picture's size in CSS pixels, both
	// positive.
	Width, Height float64
}

// Context is what an SVG drawing inherits from the document that embeds
// it: the root element's inherited properties start from these values
// instead of the initial ones (SVG 1.1, 6.15).
type Context struct {
	// Color is the value of color, what currentColor paints. The zero
	// value means black.
	Color compose.Color
	// Families is the font-family list, empty for the default sans-serif.
	Families []string
	// FontSize is the font size in CSS pixels, 16 when zero.
	FontSize float64
	// Weight is the font weight, 400 when zero.
	Weight int
	// Italic asks for an italic face.
	Italic bool
	// Lang is the BCP 47 language of the text, "" for unknown.
	Lang string
}

// Options parametrises [Parse] and [FromNode].
type Options struct {
	// Fonts shapes the text elements; nil leaves text out with a warning.
	Fonts *text.Fonts
	// Image loads the raster picture an image element names by its href,
	// as written in the document: the caller resolves it against the
	// document's URL. nil leaves every image element out with a warning.
	// It is called while parsing, at most once per distinct href.
	Image func(ctx context.Context, href string) (*Raster, error)
	// Context is the inherited state of the embedding document.
	Context Context
	// Sheets are style sheet texts that apply to the drawing before its
	// own style elements: the style sheets of the HTML document an inline
	// SVG sits in.
	Sheets []string
	// Logger receives diagnostics, nil for none.
	Logger *slog.Logger
}

// Size is the natural size of a drawing (CSS Images 3, 4.1), from the
// width, height and viewBox of its root element (SVG 2, 8.2).
type Size struct {
	// Width and Height are the natural width and height in points, valid
	// when HasWidth and HasHeight are set. A percentage or a missing
	// attribute gives none.
	Width, Height       float64
	HasWidth, HasHeight bool
	// Ratio is the natural aspect ratio, width over height, from the
	// viewBox or from both dimensions; 0 for none.
	Ratio float64
}

// Document is a parsed SVG drawing, ready to be drawn at any size. It is
// safe for concurrent use.
type Document struct {
	root  *html.Node
	opts  Options
	ids   map[string]*html.Node
	rules []rule
	size  Size
	// viewBox is the root's viewBox, ok false for none.
	viewBox viewBox
	par     aspect
	// transform is the root's transform, applied in the viewport after
	// the viewBox mapping (SVG 2, 8.5); background its background colour;
	// disabled marks a root that draws nothing.
	transform  compose.Matrix
	background compose.Color
	disabled   bool
	images     map[string]*Raster

	mu       sync.Mutex
	decls    map[*html.Node][]declaration
	domStyle map[*html.Node]*style
	matcher  *matcherState
	cache    map[[2]float64][]compose.Fragment
	warns    []compose.Warning
	warned   map[string]bool
}

// Parse parses a standalone SVG document. The text is read with the HTML
// parser's foreign content rules (HTML Standard, 13.2.6.5), which keep SVG
// element and attribute names in their case, honour self-closing tags and
// CDATA sections, and put xlink:href in the xlink namespace. An XML
// declaration and a DOCTYPE are skipped; entities other than HTML's named
// ones are not expanded.
//
// Parse returns [ErrTooLarge] past 16 MiB, [ErrNotSVG] when no svg element
// is found, and ctx's error.
func Parse(ctx context.Context, data []byte, opts Options) (*Document, error) {
	if len(data) > maxSourceBytes {
		return nil, ErrTooLarge
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	doc, err := html.Parse(bytes.NewReader(unprefixSVG(decodeXML(data))))
	if err != nil {
		return nil, fmt.Errorf("parsing the SVG: %w", err)
	}
	root := findSVG(doc, 0)
	if root == nil {
		return nil, ErrNotSVG
	}
	return newDocument(ctx, root, opts, true)
}

// decodeXML returns data in UTF-8: as it is, unless its XML declaration
// names another encoding the WHATWG Encoding Standard knows (XML 1.0,
// 4.3.3), which decodes it. A byte order mark decides first.
func decodeXML(data []byte) []byte {
	if bytes.HasPrefix(data, []byte("\xef\xbb\xbf")) || !bytes.HasPrefix(data, []byte("<?xml")) {
		return data
	}
	end := bytes.Index(data, []byte("?>"))
	if end < 0 {
		return data
	}
	m := xmlEncoding.FindSubmatch(data[:end])
	if m == nil {
		return data
	}
	enc, name := charset.Lookup(string(m[1]))
	if enc == nil || name == "utf-8" {
		return data
	}
	out, err := enc.NewDecoder().Bytes(data)
	if err != nil {
		// A decoder replaces what it cannot map; it fails on nothing.
		return data
	}
	return out
}

// xmlEncoding matches the encoding declaration of an XML declaration.
var xmlEncoding = regexp.MustCompile(`encoding\s*=\s*["']([A-Za-z0-9._:-]+)["']`)

// svgPrefixDecl matches a namespace declaration binding a prefix to the
// SVG namespace (Namespaces in XML 1.0, 3).
var svgPrefixDecl = regexp.MustCompile(`xmlns:([A-Za-z_][A-Za-z0-9._-]*)\s*=\s*["']http://www\.w3\.org/2000/svg["']`)

// unprefixSVG returns data with the tags of the SVG elements written with
// a namespace prefix, such as svg:rect under xmlns:svg, written without
// it: the HTML parser reads a prefixed name as an unknown HTML element,
// where an XML parser reads the SVG element the prefix names (Namespaces
// in XML 1.0, 4). A document that declares no such prefix is returned as
// it is. Only the first declared prefix is honoured, as real documents
// declare one.
func unprefixSVG(data []byte) []byte {
	m := svgPrefixDecl.FindSubmatch(data)
	if m == nil {
		return data
	}
	p := m[1]
	out := bytes.ReplaceAll(data, append([]byte("</"), append(p, ':')...), []byte("</"))
	return bytes.ReplaceAll(out, append([]byte("<"), append(p, ':')...), []byte("<"))
}

// FromNode builds a drawing from an svg element of an already parsed
// HTML document: inline SVG (HTML Standard, 4.8.16). The tree is read,
// never modified, and must not change while the Document is used.
// Selectors of [Options.Sheets] match through the HTML ancestors too.
func FromNode(ctx context.Context, n *html.Node, opts Options) (*Document, error) {
	if n == nil || n.Type != html.ElementNode || n.Namespace != "svg" || n.Data != "svg" {
		return nil, ErrNotSVG
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return newDocument(ctx, n, opts, false)
}

// findSVG returns the first svg element of the SVG namespace in n's
// subtree, in document order.
func findSVG(n *html.Node, depth int) *html.Node {
	if depth > maxDepth {
		return nil
	}
	if n.Type == html.ElementNode && n.Namespace == "svg" && n.Data == "svg" {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if r := findSVG(c, depth+1); r != nil {
			return r
		}
	}
	return nil
}

// newDocument indexes the drawing under root and draws it once at its
// default size, which loads its images and records its warnings. An image
// (image set) without a viewBox but with a natural width and height
// stretches its content over its viewport as if its viewBox were 0 0
// width height with preserveAspectRatio none, as Chromium draws such an
// SVG image at another size; inline SVG keeps its user units, its
// viewport growing around them.
func newDocument(ctx context.Context, root *html.Node, opts Options, image bool) (*Document, error) {
	d := &Document{
		root: root, opts: opts, ids: map[string]*html.Node{}, images: map[string]*Raster{},
		decls: map[*html.Node][]declaration{}, domStyle: map[*html.Node]*style{},
		cache: map[[2]float64][]compose.Fragment{}, warned: map[string]bool{},
	}
	sheets := append([]string(nil), opts.Sheets...)
	count := 0
	var walk func(n *html.Node, depth int)
	walk = func(n *html.Node, depth int) {
		if depth > maxDepth || count > maxElements {
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode || c.Namespace != "svg" {
				continue
			}
			count++
			if id := attrOf(c, "id"); id != "" {
				if _, ok := d.ids[id]; !ok {
					d.ids[id] = c
				}
			}
			if c.Data == "style" {
				if t := strings.ToLower(strings.TrimSpace(attrOf(c, "type"))); t == "" || t == "text/css" {
					sheets = append(sheets, styleText(c))
				}
				continue
			}
			walk(c, depth+1)
		}
	}
	if id := attrOf(root, "id"); id != "" {
		d.ids[id] = root
	}
	walk(root, 0)
	if count > maxElements {
		d.warn("svg-limit", fmt.Sprintf("the drawing holds more than %d elements; the rest were left out", maxElements))
	}
	for _, s := range sheets {
		d.rules = append(d.rules, parseSheet(s, maxStyleRules-len(d.rules))...)
	}
	d.matcher = newMatcherState()
	d.size, d.viewBox, d.par, d.disabled = rootGeometry(root)
	d.transform = identity
	if t, ok := attrOK(root, "transform"); ok {
		if m, good := parseTransform(t); good {
			d.transform = m
		}
	}
	d.background = d.computeStyle(root, initialStyle(opts.Context)).background
	if s := d.size; image && !d.viewBox.ok && s.HasWidth && s.HasHeight && s.Width > 0 && s.Height > 0 {
		d.viewBox = viewBox{w: s.Width / ptPerPx, h: s.Height / ptPerPx, ok: true}
		d.par = aspect{none: true}
	}
	w, h := d.defaultViewport()
	if _, err := d.render(ctx, w, h); err != nil {
		return nil, err
	}
	return d, nil
}

// Size returns the drawing's natural size.
func (d *Document) Size() Size { return d.size }

// Warnings returns what the drawing holds that is not drawn as the source
// means it, in the order met. The slice is a copy.
func (d *Document) Warnings() []compose.Warning {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]compose.Warning(nil), d.warns...)
}

// defaultViewport returns the viewport, in CSS pixels, the drawing takes
// when nothing sizes it: its natural size, the missing dimension from the
// ratio, else 300 by 150 (CSS Images 3, 4.3, the default object size).
func (d *Document) defaultViewport() (float64, float64) {
	s := d.size
	w, h := 300.0, 150.0
	switch {
	case s.HasWidth && s.HasHeight:
		w, h = s.Width/ptPerPx, s.Height/ptPerPx
	case s.HasWidth:
		w = s.Width / ptPerPx
		if s.Ratio > 0 {
			h = w / s.Ratio
		}
	case s.HasHeight:
		h = s.Height / ptPerPx
		if s.Ratio > 0 {
			w = h * s.Ratio
		}
	case s.Ratio > 0:
		h = w / s.Ratio
	}
	return w, h
}

// ptPerPx is the size of a CSS pixel in points (CSS Values 3, 6.2).
const ptPerPx = 0.75

// Draw returns the drawing placed in the viewport r, in the caller's
// coordinates in points: the root's viewBox maps onto r as its
// preserveAspectRatio asks (SVG 1.1, 7.8), and what falls outside r is
// clipped. The result is one group whose nesting is at most
// [MaxGroupDepth]. The fragments may be shared between calls: the caller
// copies one before changing it. An empty viewport, or one whose content
// cannot be drawn, yields nil.
func (d *Document) Draw(ctx context.Context, r compose.Rect) []compose.Fragment {
	if d.disabled || !(r.Width > 0) || !(r.Height > 0) || !finite(r.Width) || !finite(r.Height) || !finite(r.X) || !finite(r.Y) {
		return nil
	}
	w, h := r.Width/ptPerPx, r.Height/ptPerPx
	content, err := d.render(ctx, w, h)
	if err != nil || len(content) == 0 && d.background.A <= 0 {
		return nil
	}
	// The root's transform turns about the centre of its viewport, the
	// initial transform-origin of an element with a CSS layout box (CSS
	// Transforms 1, 6).
	m := mul(translate(w/2, h/2), mul(d.transform, translate(-w/2, -h/2)))
	if d.viewBox.ok {
		vbm, ok := viewBoxTransform(d.viewBox, d.par, w, h)
		if !ok {
			return nil
		}
		m = mul(m, vbm)
	}
	// The clip and the background are in the group's coordinates, the
	// root's user space: the viewport maps back through the inverse.
	inv, ok := invert(m)
	if !ok {
		return nil
	}
	clip := transformOps(rectOps(0, 0, w, h), inv)
	if d.background.A > 0 {
		content = append([]compose.Fragment{&compose.Path{Ops: clip, Fill: &compose.Fill{Color: d.background}}}, content...)
	}
	return []compose.Fragment{&compose.Group{
		Transform: mul(mul(translate(r.X, r.Y), scale(ptPerPx, ptPerPx)), m),
		Clip:      &compose.Path{Ops: clip, Fill: &compose.Fill{}},
		Opacity:   1,
		Content:   content,
	}}
}

// MaxGroupDepth is the deepest group nesting [Document.Draw] returns, its
// outer group included: a caller nesting the drawing under its own groups
// keeps the total within the composition model's bound.
const MaxGroupDepth = maxGroupNesting + 2

// render returns the drawing's content for a viewport of w by h CSS
// pixels, in user units, from the cache when the content does not depend
// on the viewport's size.
func (d *Document) render(ctx context.Context, w, h float64) ([]compose.Fragment, error) {
	key := [2]float64{w, h}
	vb := d.viewBox
	if vb.ok {
		key = [2]float64{-1, -1}
	}
	d.mu.Lock()
	if c, ok := d.cache[key]; ok {
		d.mu.Unlock()
		return c, nil
	}
	d.mu.Unlock()
	vw, vh := w, h
	if vb.ok {
		vw, vh = vb.w, vb.h
	}
	r := newRenderer(ctx, d, vw, vh)
	content := r.root()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	d.mu.Lock()
	if len(d.cache) < maxDrawCache {
		d.cache[key] = content
	}
	d.mu.Unlock()
	return content, nil
}

// warn records a warning once per message.
func (d *Document) warn(code, msg string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.warned[msg] || len(d.warns) >= maxWarnings {
		return
	}
	d.warned[msg] = true
	d.warns = append(d.warns, compose.Warning{Code: code, Message: msg})
}

// attrOf returns the value of n's attribute key with no namespace.
func attrOf(n *html.Node, key string) string {
	v, _ := attrOK(n, key)
	return v
}

// attrOK returns the value of n's attribute key with no namespace and
// whether it is present.
func attrOK(n *html.Node, key string) (string, bool) {
	for _, a := range n.Attr {
		if a.Namespace == "" && a.Key == key {
			return a.Val, true
		}
	}
	return "", false
}

// href returns n's reference: href, or xlink:href when href is absent
// (SVG 2, 16.1.5).
func href(n *html.Node) string {
	if v, ok := attrOK(n, "href"); ok {
		return v
	}
	for _, a := range n.Attr {
		if a.Namespace == "xlink" && a.Key == "href" {
			return a.Val
		}
	}
	return ""
}

// rectOps returns the path of the rectangle at (x, y) of size w by h.
func rectOps(x, y, w, h float64) []compose.PathOp {
	return []compose.PathOp{
		{Kind: compose.MoveTo, P: [3]compose.Point{{X: x, Y: y}}},
		{Kind: compose.LineTo, P: [3]compose.Point{{X: x + w, Y: y}}},
		{Kind: compose.LineTo, P: [3]compose.Point{{X: x + w, Y: y + h}}},
		{Kind: compose.LineTo, P: [3]compose.Point{{X: x, Y: y + h}}},
		{Kind: compose.ClosePath},
	}
}
