package odt

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"  // registers GIF with image.Decode
	_ "image/jpeg" // registers JPEG with image.Decode
	_ "image/png"  // registers PNG with image.Decode
	"math"
	"strconv"
	"strings"

	_ "golang.org/x/image/bmp"  // registers BMP with image.Decode
	_ "golang.org/x/image/tiff" // registers TIFF with image.Decode

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/odf"
	"github.com/gotenberg/litho/pkg/internal/shape"
	"github.com/gotenberg/litho/pkg/internal/svg"
)

// The picture caps.
const (
	// maxImagePixels bounds one picture's pixels, 50 megapixels, and
	// maxTotalPixels a document's, 200 megapixels. Anti-DoS guard, not
	// spec-mandated: a decoded pixel costs four bytes, and a full-page
	// photograph at 300 dpi holds nine megapixels.
	maxImagePixels = 50_000_000
	maxTotalPixels = 200_000_000
	// maxPictures bounds the distinct pictures one document decodes:
	// 2,000. Anti-DoS guard, not spec-mandated: an illustrated manual
	// holds a few hundred.
	maxPictures = 2000
)

// picResult is one decoded picture or the reason it is not drawn.
type picResult struct {
	pic *layout.Picture
}

// pictureURL is the URL the layout's image loader resolves for a
// background picture in the package.
func pictureURL(href string) string { return "odf:" + href }

// loadPicture is the layout's image loader: a background picture of the
// package by its [pictureURL].
func (b *builder) loadPicture(_ context.Context, u string) (*layout.Picture, error) {
	href, ok := strings.CutPrefix(u, "odf:")
	if !ok {
		return nil, fmt.Errorf("the picture %s is outside the package", u)
	}
	if pic := b.picture(href, nil); pic != nil {
		return pic, nil
	}
	return nil, fmt.Errorf("the picture %s is not drawn", href)
}

// picture decodes the picture of a draw:image: the package entry href,
// or the base64 office:binary-data bin when href is empty. nil, with a
// warning, for a picture it does not draw.
func (b *builder) picture(href string, bin *odf.Element) *layout.Picture {
	key := href
	if href == "" {
		if bin == nil {
			return nil
		}
		key = fmt.Sprintf("binary:%p", bin)
	}
	if r, ok := b.pics[key]; ok {
		return r.pic
	}
	if len(b.pics) >= maxPictures {
		b.warn("too-many-pictures", fmt.Sprintf("pictures past %d are not drawn", maxPictures))
		return nil
	}
	pic := b.decodePicture(href, bin)
	b.pics[key] = picResult{pic: pic}
	return pic
}

// decodePicture reads and decodes one picture.
func (b *builder) decodePicture(href string, bin *odf.Element) *layout.Picture {
	var data []byte
	switch {
	case href != "":
		if strings.Contains(href, "://") {
			b.warn("external-picture", "a linked picture outside the package is not fetched")
			return nil
		}
		if b.pkg.Flat {
			b.warn("missing-picture", fmt.Sprintf("the picture %s is not in the document", href))
			return nil
		}
		d, err := b.pkg.ReadFile(b.ctx, href)
		if err != nil {
			b.warn("missing-picture", fmt.Sprintf("the picture %s is not read: %v", href, err))
			return nil
		}
		data = d
	default:
		d, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(bin.TextContent()), ""))
		if err != nil {
			b.warn("binary-data", "an inline picture's base64 does not decode")
			return nil
		}
		data = d
	}
	if name := vectorFormat(data); name != "" {
		b.warn("picture-"+strings.ToLower(name), fmt.Sprintf("%s pictures are not drawn", name))
		return nil
	}
	if isSVG(data) {
		doc, err := svg.Parse(b.ctx, data, svg.Options{Fonts: b.fonts})
		if err != nil {
			b.warn("svg", fmt.Sprintf("an SVG picture is not drawn: %v", err))
			return nil
		}
		s := doc.Size()
		w, h := s.Width, s.Height
		if !s.HasWidth || !s.HasHeight || w <= 0 || h <= 0 {
			w, h = 300*css.Pt, 150*css.Pt
		}
		return &layout.Picture{Width: w, Height: h, Vector: doc.Draw}
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		b.warn("picture-format", "a picture in an unknown format is not drawn")
		return nil
	}
	px := int64(cfg.Width) * int64(cfg.Height)
	if px > maxImagePixels || b.pixels+px > maxTotalPixels {
		b.warn("picture-pixels", "a picture past the pixel caps is not drawn")
		return nil
	}
	b.pixels += px
	pic := &layout.Picture{Width: float64(cfg.Width) * css.Pt, Height: float64(cfg.Height) * css.Pt}
	if format == "jpeg" && dctModel(cfg.ColorModel) {
		pic.Source = &compose.ImageSource{JPEG: data}
		return pic
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		b.warn("picture-decode", fmt.Sprintf("a %s picture does not decode", format))
		return nil
	}
	pic.Source = &compose.ImageSource{Image: img}
	return pic
}

// dctModel reports a JPEG colour model the backend writes untouched.
func dctModel(m color.Model) bool {
	return m == color.GrayModel || m == color.YCbCrModel || m == color.CMYKModel
}

// vectorFormat names the Windows and StarView metafiles, which are not
// drawn: WMF (placeable or not), EMF and SVM; "" for anything else.
func vectorFormat(d []byte) string {
	switch {
	case bytes.HasPrefix(d, []byte{0xd7, 0xcd, 0xc6, 0x9a}), bytes.HasPrefix(d, []byte{0x01, 0x00, 0x09, 0x00}):
		return "WMF"
	case len(d) > 44 && bytes.Equal(d[40:44], []byte(" EMF")):
		return "EMF"
	case bytes.HasPrefix(d, []byte("VCLMTF")):
		return "SVM"
	}
	return ""
}

// isSVG reports an SVG document by its start.
func isSVG(d []byte) bool {
	head := bytes.TrimLeft(d[:min(len(d), 512)], "\xef\xbb\xbf \t\r\n")
	return bytes.HasPrefix(head, []byte("<svg")) || bytes.HasPrefix(head, []byte("<?xml")) && bytes.Contains(d[:min(len(d), 4096)], []byte("<svg"))
}

// frameGeom is a frame's size, offsets and anchoring as its attributes
// and graphic style give them (ODF 1.3, Part 3, 10.4 and 20.296 to 20.300).
type frameGeom struct {
	w, h, x, y float64
	hasW, hasH bool
	anchor     string
	wrap       string
	hpos, hrel string
	vpos, vrel string
	// page is the page a page-anchored frame sits on, counted from 1, 0
	// for the page its anchor falls on (ODF 1.3 Part 3, 19.758
	// text:anchor-page-number).
	page  int
	props odf.Props
}

// geometry reads a drawing element's geometry and graphic style.
func (b *builder) geometry(c *odf.Element) frameGeom {
	g := frameGeom{anchor: c.A(odf.NSText, "anchor-type")}
	if n, err := strconv.Atoi(c.A(odf.NSText, "anchor-page-number")); err == nil && n > 0 && g.anchor == "page" {
		g.page = n
	}
	g.w, g.hasW = odf.Length(c.A(odf.NSSVG, "width"))
	g.h, g.hasH = odf.Length(c.A(odf.NSSVG, "height"))
	if !g.hasW {
		g.w, g.hasW = odf.Length(c.A(odf.NSFO, "min-width"))
	}
	if !g.hasH {
		g.h, g.hasH = odf.Length(c.A(odf.NSFO, "min-height"))
	}
	g.x, _ = odf.Length(c.A(odf.NSSVG, "x"))
	g.y, _ = odf.Length(c.A(odf.NSSVG, "y"))
	g.props = b.styles.Resolve(b.scope, odf.FamilyGraphic, c.A(odf.NSDraw, "style-name"), odf.GraphicProps)
	g.wrap = g.props.Get("style:wrap")
	g.hpos, g.hrel = g.props.Get("style:horizontal-pos"), g.props.Get("style:horizontal-rel")
	g.vpos, g.vrel = g.props.Get("style:vertical-pos"), g.props.Get("style:vertical-rel")
	b.relativeSize(c, &g)
	return g
}

// relativeSize applies a frame's style:rel-width and style:rel-height
// (ODF 1.3, Part 3, 19.737 and 19.736): a percentage of the page when
// LibreOffice's rel-width-rel or rel-height-rel says page, else of the
// page's content area, which Writer resolves for a frame in the body
// (svg:width and svg:height hold the size it last computed, stale when
// the page changed). "scale" and "scale-min" keep the stored size.
func (b *builder) relativeSize(c *odf.Element, g *frameGeom) {
	rw, okW := odf.Percent(c.A(odf.NSStyle, "rel-width"))
	rh, okH := odf.Percent(c.A(odf.NSStyle, "rel-height"))
	if !okW && !okH || b.hf != nil {
		return
	}
	pg := b.currentGeom()
	rel := func(local string) string {
		if v, ok := g.props.Lookup("loext:" + local); ok {
			return v
		}
		return g.props.Get("style:" + local)
	}
	if okW && rw > 0 && rw <= 1 {
		w := pg.box.Width - pg.box.Margin[css.Left] - pg.box.Margin[css.Right]
		if rel("rel-width-rel") == "page" {
			w = pg.box.Width
		}
		g.w, g.hasW = rw*w, true
	}
	if okH && rh > 0 && rh <= 1 {
		h := pg.bottom - pg.top
		if rel("rel-height-rel") == "page" {
			h = pg.box.Height
		}
		g.h, g.hasH = rh*h, true
	}
}

// pageRelative reports a frame whose position Writer measures from the
// page: anchored to the page (ODF 1.3, Part 3, 19.756), or placed
// against the page or its content area vertically (20.387,
// style:vertical-rel page and page-content).
func (g frameGeom) pageRelative() bool {
	return g.anchor == "page" || g.vrel == "page" || g.vrel == "page-content"
}

// frameBox returns the style of a frame's box: its size, border,
// padding, background and margins.
func (b *builder) frameBox(g frameGeom) *css.Style {
	st := baseStyle()
	if g.hasW && g.w > 0 {
		st.Width = css.Pts(g.w)
	}
	if g.hasH && g.h > 0 {
		st.Height = css.Pts(g.h)
	}
	st.BoxSizing = css.BorderBox
	var pad [4]float64
	st.Border, pad = foldBox(st.Border, pad, g.props)
	for i := range pad {
		st.Padding[i] = css.Pts(pad[i])
	}
	if v := g.props.Get("fo:background-color"); v != "" {
		if col, ok := odf.ParseColor(v); ok {
			st.BackgroundColor = cssColor(col)
		}
	}
	if g.props.Get("draw:fill") == "solid" {
		if col, ok := odf.ParseColor(g.props.Get("draw:fill-color")); ok {
			st.BackgroundColor = cssColor(col)
		}
	}
	return st
}

// inlineFrame appends a drawing element met in a paragraph's text (ODF
// 1.3, Part 3, 10): a frame anchored as a character is an atomic inline
// box on the line; one anchored to the paragraph or a character floats
// beside the text as style:wrap says, or sits in the flow above the
// paragraph when nothing wraps around it; draw:a makes its frame a link.
func (b *builder) inlineFrame(c *odf.Element, out *[]*layout.Node, ws *wsState, depth int) {
	if c.Is(odf.NSDraw, "a") {
		var kids []*layout.Node
		for _, k := range c.Children {
			b.inlineFrame(k, &kids, ws, depth+1)
		}
		*out = append(*out, b.node(&layout.Node{Kind: layout.Element, Style: baseStyle(), Href: c.A(odf.NSXLink, "href"), Role: "Link", Children: kids}))
		return
	}
	n := b.drawing(c, depth)
	if n == nil {
		return
	}
	ws.space = false
	g := b.geometry(c)
	switch g.anchor {
	case "as-char", "":
		if n.Style.Display == css.Block {
			st := *n.Style
			st.Display = css.InlineBlock
			n.Style = &st
		}
		n.Style.VerticalAlign = css.VerticalAlign{Kind: css.Baseline}
		switch g.vpos {
		case "middle", "center":
			n.Style.VerticalAlign = css.VerticalAlign{Kind: css.Middle}
		case "top":
			if g.vrel == "line" || g.vrel == "text" {
				n.Style.VerticalAlign = css.VerticalAlign{Kind: css.TextTop}
			}
		}
		*out = append(*out, n)
	case "page":
		b.place(n, g, true)
	default:
		// A header's or footer's paragraph-anchored frame stays where
		// its paragraph puts it: Writer keeps it off the body.
		b.place(n, g, g.pageRelative() && b.hf == nil)
		*out = append(*out, n)
	}
}

// blockFrame builds a drawing element among the body's blocks: a frame
// anchored to the page.
func (b *builder) blockFrame(c *odf.Element, ctx blockCtx) []*layout.Node {
	n := b.drawing(c, ctx.depth)
	if n == nil {
		return nil
	}
	g := b.geometry(c)
	b.place(n, g, g.pageRelative() || g.hrel == "page")
	if n.Style.Float == css.FloatNone && n.Style.Display != css.Block && (n.Kind != layout.Replaced || n.Style.Position == css.Absolute) {
		st := *n.Style
		st.Display = blockified(st.Display)
		n.Style = &st
	}
	return []*layout.Node{n}
}

// place positions an anchored frame n: run-through and page-anchored
// frames are positioned boxes at their offsets, the others float to the
// side style:wrap leaves them, or stand in the flow when no text wraps.
func (b *builder) place(n *layout.Node, g frameGeom, page bool) {
	st := *n.Style
	n.Style = &st
	if page || g.wrap == "run-through" {
		st.Position = css.Absolute
		// An absolutely positioned box is block-level (CSS 2.1, 9.7): the
		// engine lays an inline-level one out in the line.
		st.Display = blockified(st.Display)
		st.Left, st.Top = css.Pts(g.x), css.Pts(g.y)
		switch {
		case page && b.hf != nil:
			// A header's or footer's frame stays in its margin box: Writer
			// keeps it off the body, which its offsets less the body's
			// origin match on the documents measured.
			st.Left, st.Top = css.Pts(g.x-b.pageOrigin[0]), css.Pts(g.y-b.pageOrigin[1])
		case page:
			st.PageAnchor = true
			st.AnchorPage = g.page
			x, y := b.pagePosition(g)
			st.Left, st.Top = css.Pts(x), css.Pts(y)
		case paraRel(g.hrel):
			st.Left = css.Pts(alignX(g, b.contentWidth()))
		}
		st.Right, st.Bottom = css.Dimension{Kind: css.Auto}, css.Dimension{Kind: css.Auto}
		st.ZIndex = css.ZIndex{Level: 0}
		return
	}
	side := css.FloatLeft
	switch g.wrap {
	case "left":
		side = css.FloatRight
	case "right":
		side = css.FloatLeft
	default:
		if g.hpos == "right" {
			side = css.FloatRight
		}
	}
	margins := frameMargins(g.props)
	for i := range margins {
		st.Margin[i] = css.Pts(margins[i])
	}
	if g.hpos == "from-left" || g.hpos == "" {
		st.Margin[css.Left] = css.Pts(margins[css.Left] + max(g.x, 0))
	}
	if g.hpos == "center" && g.wrap != "none" && g.hasW && paraRel(g.hrel) {
		// A float cannot be centred: it floats left, as far in as Writer
		// centres it, the text wrapping on its right.
		st.Margin[css.Left] = css.Pts(max(alignX(g, b.contentWidth()), 0))
	}
	if g.vpos == "from-top" || g.vpos == "" {
		st.Margin[css.Top] = css.Pts(margins[css.Top] + max(g.y, 0))
	}
	if g.wrap != "none" {
		if b.floats == nil {
			b.floats = map[*layout.Node]frameGeom{}
		}
		b.floats[n] = g
	}
	if g.wrap == "none" {
		st.Display = blockified(st.Display)
		st.Float = css.FloatNone
		st.Clear = css.ClearBoth
		switch g.hpos {
		case "center":
			st.Margin[css.Left], st.Margin[css.Right] = css.Dimension{Kind: css.Auto}, css.Dimension{Kind: css.Auto}
		case "right":
			st.Margin[css.Left] = css.Dimension{Kind: css.Auto}
		}
		return
	}
	st.Float = side
}

// pagePosition returns the top-left corner of a page-relative frame in
// the page box of the master page the body is on (ODF 1.3, Part 3,
// 20.290 style:horizontal-pos, 20.291 style:horizontal-rel, 20.386
// style:vertical-pos and 20.387 style:vertical-rel): from-left and
// from-top offset svg:x and svg:y from the edge of the area the relation
// names, left, center, right, top, middle and bottom align the frame in
// it. The page relations are the page box (page, and the margins of
// page-start-margin and page-end-margin) and its content area
// (page-content, margins, border and padding inside, the header and
// footer included); a relation to the paragraph Writer measures from the
// page's content area too when the frame is placed against the page
// vertically.
func (b *builder) pagePosition(g frameGeom) (float64, float64) {
	pg := b.currentGeom()
	pw, ph := pg.box.Width, pg.box.Height
	left, right := pg.box.Margin[css.Left], pw-pg.box.Margin[css.Right]
	w, h := 0.0, 0.0
	if g.hasW {
		w = g.w
	}
	if g.hasH {
		h = g.h
	}
	var x0, x1 float64
	switch g.hrel {
	case "page", "page-start-margin", "page-end-margin":
		x0, x1 = 0, pw
		switch g.hrel {
		case "page-start-margin":
			x1 = left
		case "page-end-margin":
			x0 = right
		}
	default:
		if g.hrel == "" && g.anchor == "page" {
			x0, x1 = 0, pw
		} else {
			x0, x1 = left, right
		}
	}
	x := x0 + g.x
	switch g.hpos {
	case "left", "inside":
		x = x0
	case "center":
		x = x0 + (x1-x0-w)/2
	case "right", "outside":
		x = x1 - w
	}
	y0, y1 := 0.0, ph
	if g.vrel == "page-content" || g.vrel != "page" && (g.vrel != "" || g.anchor != "page") {
		y0, y1 = pg.top, pg.bottom
	}
	y := y0 + g.y
	switch g.vpos {
	case "top":
		y = y0
	case "middle", "center":
		y = y0 + (y1-y0-h)/2
	case "bottom":
		y = y1 - h
	}
	return x, y
}

// currentGeom returns the geometry of the master page the body being
// built is on: the right, non-first variant of the last master page use.
func (b *builder) currentGeom() *pageGeom {
	var mp *odf.MasterPage
	if n := len(b.pages.instances); n > 0 {
		mp = b.pages.instances[n-1].master
	} else {
		mp = b.pages.defaultMaster()
	}
	return b.pages.geom(mp, false, false)
}

// blockified returns the block-level display of a box (CSS Display 3,
// 2.7): a flex container stays one.
func blockified(d css.Display) css.Display {
	if d == css.InlineFlex || d == css.Flex {
		return css.Flex
	}
	return css.Block
}

// hasText reports inline content that draws a character other than white
// space, frames left out.
func hasText(kids []*layout.Node) bool {
	for _, k := range kids {
		switch {
		case k.Kind == layout.Text && strings.TrimSpace(k.Text) != "":
			return true
		case k.Kind == layout.Element && k.Style.Float == css.FloatNone && k.Style.Position == css.Static && hasText(k.Children):
			return true
		}
	}
	return false
}

// controlKind returns the local name of the form control a draw:control
// draws (ODF 1.3, Part 3, 10.3.13 draw:control and 13.5): the
// office:forms element whose form:id or xml:id is its draw:control; ""
// when none is.
func (b *builder) controlKind(c *odf.Element) string {
	id := c.A(odf.NSDraw, "control")
	if id == "" {
		return ""
	}
	forms := odf.Section(b.pkg.Content, "body").Child(odf.NSOffice, "text").Child(odf.NSOffice, "forms")
	stack := []*odf.Element{forms}
	for steps := 0; len(stack) > 0 && steps < odf.MaxElements; steps++ {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if cur == nil {
			continue
		}
		if cur.Name.Space == odf.NSForm && (cur.A(odf.NSForm, "id") == id || cur.A(odf.NSXML, "id") == id) {
			return cur.Name.Local
		}
		for _, k := range cur.Children {
			if !k.IsText() {
				stack = append(stack, k)
			}
		}
	}
	return ""
}

// floatCount returns the floating frames among kids.
func (b *builder) floatCount(kids []*layout.Node) int {
	n := 0
	for _, k := range kids {
		if _, ok := b.floats[k]; ok {
			n++
		}
	}
	return n
}

// pinFloats places the floating frames among kids, the content of a
// paragraph p without text that anchors several, at their offsets from
// the paragraph's corner (ODF 1.3, Part 3, 20.290 and 20.386,
// style:horizontal-pos and style:vertical-pos against the paragraph):
// Writer puts each where its svg:x and svg:y say, beside or over its
// neighbours, where floats would stack below one another. A lone frame
// keeps floating: the text after it wraps around it as Writer wraps it.
func (b *builder) pinFloats(kids []*layout.Node, p paraState) {
	for _, k := range kids {
		g, ok := b.floats[k]
		if !ok {
			continue
		}
		st := *k.Style
		k.Style = &st
		st.Float = css.FloatNone
		st.Position = css.Absolute
		st.Display = blockified(st.Display)
		for i := range st.Margin {
			st.Margin[i] = css.Pts(0)
		}
		pg := b.currentGeom()
		area := pg.box.Width - pg.box.Margin[css.Left] - pg.box.Margin[css.Right] - p.margin[css.Left] - p.margin[css.Right]
		x := alignX(g, area)
		y := g.y
		if g.vpos == "top" {
			y = 0
		}
		st.Left, st.Top = css.Pts(x), css.Pts(y)
		st.Right, st.Bottom = css.Dimension{Kind: css.Auto}, css.Dimension{Kind: css.Auto}
	}
}

// paraRel reports a style:horizontal-rel that measures from the
// paragraph (ODF 1.3, Part 3, 20.291), the default for a frame anchored
// to it.
func paraRel(rel string) bool {
	switch rel {
	case "", "paragraph", "paragraph-content", "char":
		return true
	}
	return false
}

// alignX returns the left edge of a frame of geometry g in an area of
// width area that style:horizontal-pos aligns it in (ODF 1.3, Part 3,
// 20.290): svg:x for from-left.
func alignX(g frameGeom, area float64) float64 {
	w := 0.0
	if g.hasW {
		w = g.w
	}
	switch g.hpos {
	case "left", "inside":
		return 0
	case "center":
		return (area - w) / 2
	case "right", "outside":
		return area - w
	}
	return g.x
}

// contentWidth returns the width of the content area of the master page
// the body being built is on.
func (b *builder) contentWidth() float64 {
	pg := b.currentGeom()
	return pg.box.Width - pg.box.Margin[css.Left] - pg.box.Margin[css.Right]
}

// frameMargins returns the fo:margin of a graphic style, the room the
// text keeps from a wrapped frame.
func frameMargins(p odf.Props) [4]float64 {
	var m [4]float64
	if v, ok := odf.Length(p.Get("fo:margin")); ok {
		m = [4]float64{v, v, v, v}
	}
	for i, s := range sideNames {
		if v, ok := odf.Length(p.Get("fo:margin-" + s)); ok {
			m[i] = v
		}
	}
	return m
}

// drawing builds the box of one drawing element (ODF 1.3, Part 3, 10):
// a frame with its picture, text box or object replacement, a form
// control as its bordered box, or a shape; nil for one it does not draw.
func (b *builder) drawing(c *odf.Element, depth int) *layout.Node {
	if depth > odf.MaxDepth {
		return nil
	}
	g := b.geometry(c)
	switch {
	case c.Is(odf.NSDraw, "frame"):
		return b.frame(c, g, depth)
	case c.Is(odf.NSDraw, "control"):
		st := b.frameBox(g)
		st.Display = css.InlineBlock
		// A control draws its own frame whatever fo:border says: Writer
		// prints a text field, a list box or a text area with a thin
		// black border and a check box as a square of a point's border
		// (measured on the pinned image: 0.5 pt and 1 pt).
		bd := css.Border{Width: 0.5, Style: css.Solid, Color: css.Color{A: 1}}
		if b.controlKind(c) == "checkbox" {
			bd.Width = 1
		}
		st.Border = [4]css.Border{bd, bd, bd, bd}
		for i := range st.Padding {
			st.Padding[i] = css.Pts(0)
		}
		b.warn("form-control", "form controls are drawn as their empty boxes")
		return b.node(&layout.Node{Kind: layout.Element, Style: st, Role: "Form"})
	case c.Is(odf.NSDraw, "g"):
		st := baseStyle()
		st.Display = css.InlineBlock
		n := b.node(&layout.Node{Kind: layout.Element, Style: st, Role: "Figure"})
		for _, k := range c.Children {
			if k.Name.Space == odf.NSDraw {
				if kn := b.drawing(k, depth+1); kn != nil {
					n.Children = append(n.Children, kn)
				}
			}
		}
		b.warn("draw-group", "the shapes of a group are laid out side by side")
		return n
	case c.Name.Space == odf.NSDraw:
		return b.shape(c, g, depth)
	}
	return nil
}

// frame builds a draw:frame (10.4.2) from its first content it draws: a
// picture, a text box, or an object's replacement picture.
func (b *builder) frame(c *odf.Element, g frameGeom, depth int) *layout.Node {
	alt := strings.TrimSpace(c.Child(odf.NSSVG, "desc").TextContent())
	if alt == "" {
		alt = strings.TrimSpace(c.Child(odf.NSSVG, "title").TextContent())
	}
	for _, k := range c.Children {
		switch {
		case k.Is(odf.NSDraw, "image"):
			pic := b.picture(k.A(odf.NSXLink, "href"), k.Child(odf.NSOffice, "binary-data"))
			if pic == nil {
				continue
			}
			return b.imageNode(pic, g, alt)
		case k.Is(odf.NSDraw, "text-box"):
			return b.textBox(k, g, depth)
		case k.Is(odf.NSDraw, "object"), k.Is(odf.NSDraw, "object-ole"):
			// The replacement picture, a draw:image after the object, is
			// tried next.
			b.warn("object", "embedded objects are drawn as their replacement picture")
		}
	}
	st := b.frameBox(g)
	st.Display = css.InlineBlock
	return b.node(&layout.Node{Kind: layout.Element, Style: st})
}

// imageNode returns the replaced node of a picture in a frame: the
// frame's size, its fo:clip crop and its style:mirror.
func (b *builder) imageNode(pic *layout.Picture, g frameGeom, alt string) *layout.Node {
	st := b.frameBox(g)
	st.Display = css.Inline
	if clip := g.props.Get("fo:clip"); clip != "" && clip != "auto" {
		pic = cropped(pic, clip)
	}
	switch m := g.props.Get("style:mirror"); {
	case strings.Contains(m, "horizontal") && m != "none":
		st.Transform = []css.TransformFunc{{Kind: css.TransformScale, V: [6]float64{-1, 1}}}
	case m == "vertical":
		st.Transform = []css.TransformFunc{{Kind: css.TransformScale, V: [6]float64{1, -1}}}
	}
	return b.node(&layout.Node{Kind: layout.Replaced, Style: st, Picture: pic, Role: "Figure", Alt: alt})
}

// cropped returns pic cut to fo:clip, "rect(top, right, bottom, left)"
// (ODF 1.3, Part 3, 20.179; XSL 1.0, 7.21.1): the insets in the picture's
// natural size; the picture keeps the frame's size and shows the rest.
func cropped(pic *layout.Picture, clip string) *layout.Picture {
	inner, ok := strings.CutPrefix(strings.TrimSpace(clip), "rect(")
	if !ok {
		return pic
	}
	inner = strings.TrimSuffix(inner, ")")
	parts := strings.FieldsFunc(inner, func(r rune) bool { return r == ',' || r == ' ' })
	if len(parts) != 4 {
		return pic
	}
	var in [4]float64
	for i, p := range parts {
		v, ok := odf.Length(p)
		if !ok && p != "auto" {
			return pic
		}
		in[i] = max(v, 0)
	}
	w := pic.Width - in[css.Left] - in[css.Right]
	h := pic.Height - in[css.Top] - in[css.Bottom]
	if w <= 0 || h <= 0 || in == [4]float64{} {
		return pic
	}
	src := pic
	out := &layout.Picture{Width: w, Height: h}
	out.Vector = func(ctx context.Context, r compose.Rect) []compose.Fragment {
		sx, sy := r.Width/w, r.Height/h
		full := compose.Rect{X: r.X - in[css.Left]*sx, Y: r.Y - in[css.Top]*sy, Width: src.Width * sx, Height: src.Height * sy}
		var content []compose.Fragment
		if src.Vector != nil {
			content = src.Vector(ctx, full)
		} else {
			content = []compose.Fragment{&compose.Image{Rect: full, Source: src.Source}}
		}
		clip := &compose.Path{Ops: rectOps(r)}
		return []compose.Fragment{&compose.Group{Clip: clip, Content: content}}
	}
	return out
}

// rectOps returns the path of the rectangle r.
func rectOps(r compose.Rect) []compose.PathOp {
	return []compose.PathOp{
		{Kind: compose.MoveTo, P: [3]compose.Point{{X: r.X, Y: r.Y}}},
		{Kind: compose.LineTo, P: [3]compose.Point{{X: r.X + r.Width, Y: r.Y}}},
		{Kind: compose.LineTo, P: [3]compose.Point{{X: r.X + r.Width, Y: r.Y + r.Height}}},
		{Kind: compose.LineTo, P: [3]compose.Point{{X: r.X, Y: r.Y + r.Height}}},
		{Kind: compose.ClosePath},
	}
}

// textBox builds a draw:text-box (10.4.3): a block of its paragraphs in
// the frame's box, at least its minimum height.
func (b *builder) textBox(k *odf.Element, g frameGeom, depth int) *layout.Node {
	st := b.frameBox(g)
	applyWritingMode(st, g.props.Get("style:writing-mode"))
	st.Display = css.InlineBlock
	if h, ok := odf.Length(k.A(odf.NSFO, "min-height")); ok && h > 0 {
		st.MinHeight = css.Pts(h)
		if !g.hasH || g.props.Get("style:auto-grow-height") != "false" {
			st.Height = css.Dimension{Kind: css.Auto}
		}
	}
	if g.props.Get("draw:auto-grow-height") == "true" {
		st.MinHeight = st.Height
		st.Height = css.Dimension{Kind: css.Auto}
	}
	if st.Height.Kind == css.Points && k.A(odf.NSDraw, "chain-next-name") == "" {
		// A text box of a fixed height clips the text it cannot hold, as
		// Writer draws it; one linked to a next box, whose text Writer
		// continues there, shows it all, the chain not being laid out.
		st.Overflow = css.OverflowHidden
	}
	n := b.node(&layout.Node{Kind: layout.Element, Style: st, Role: "Div"})
	n.Children = b.blocks(k, blockCtx{depth: depth + 1})
	return n
}

// shapeSVG returns an SVG drawing of a shape (ODF 1.3, Part 3, 10.3) in
// its own box: rectangles, ellipses, circles, lines, polylines, polygons
// and paths with their viewBox; "" for a shape it does not draw (a custom
// shape draws through [customShape]).
func shapeSVG(c *odf.Element, g frameGeom) string {
	fill := "#729fcf"
	if g.props.Get("draw:fill") == "none" {
		fill = "none"
	} else if col, ok := odf.ParseColor(g.props.Get("draw:fill-color")); ok {
		fill = fmt.Sprintf("#%02x%02x%02x", col.R, col.G, col.B)
	}
	stroke := "#3465a4"
	if g.props.Get("draw:stroke") == "none" {
		stroke = "none"
	} else if col, ok := odf.ParseColor(g.props.Get("svg:stroke-color")); ok {
		stroke = fmt.Sprintf("#%02x%02x%02x", col.R, col.G, col.B)
	}
	sw, ok := odf.Length(g.props.Get("svg:stroke-width"))
	if !ok || sw <= 0 {
		sw = 0.75
	}
	w, h := math.Max(g.w, 1), math.Max(g.h, 1)
	paint := fmt.Sprintf(`fill="%s" stroke="%s" stroke-width="%g"`, fill, stroke, sw)
	var body string
	switch c.Name.Local {
	case "rect":
		r, _ := odf.Length(c.A(odf.NSDraw, "corner-radius"))
		body = fmt.Sprintf(`<rect x="0" y="0" width="%g" height="%g" rx="%g" %s/>`, w, h, r, paint)
	case "ellipse", "circle":
		body = fmt.Sprintf(`<ellipse cx="%g" cy="%g" rx="%g" ry="%g" %s/>`, w/2, h/2, w/2, h/2, paint)
	case "line":
		x1, _ := odf.Length(c.A(odf.NSSVG, "x1"))
		y1, _ := odf.Length(c.A(odf.NSSVG, "y1"))
		x2, _ := odf.Length(c.A(odf.NSSVG, "x2"))
		y2, _ := odf.Length(c.A(odf.NSSVG, "y2"))
		lw, lh := math.Max(math.Abs(x2-x1), 1), math.Max(math.Abs(y2-y1), 1)
		return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%gpt" height="%gpt" viewBox="0 0 %g %g"><line x1="%g" y1="%g" x2="%g" y2="%g" stroke="%s" stroke-width="%g"/></svg>`,
			lw, lh, lw, lh, x1-math.Min(x1, x2), y1-math.Min(y1, y2), x2-math.Min(x1, x2), y2-math.Min(y1, y2), stroke, sw)
	case "path", "polyline", "polygon":
		vb := c.A(odf.NSSVG, "viewBox")
		if vb == "" {
			return ""
		}
		var attr string
		switch c.Name.Local {
		case "path":
			attr = fmt.Sprintf(`<path d="%s" %s/>`, xmlEscape(c.A(odf.NSSVG, "d")), paint)
		case "polyline":
			attr = fmt.Sprintf(`<polyline points="%s" fill="none" stroke="%s" stroke-width="%g"/>`, xmlEscape(c.A(odf.NSDraw, "points")), stroke, sw)
		default:
			attr = fmt.Sprintf(`<polygon points="%s" %s/>`, xmlEscape(c.A(odf.NSDraw, "points")), paint)
		}
		return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%gpt" height="%gpt" viewBox="%s" preserveAspectRatio="none">%s</svg>`, w, h, xmlEscape(vb), attr)
	default:
		return ""
	}
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%gpt" height="%gpt" viewBox="0 0 %g %g" overflow="visible">%s</svg>`, w, h, w, h, body)
}

// xmlEscape escapes s for an XML attribute.
func xmlEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '&':
			b.WriteString("&amp;")
		case '"':
			b.WriteString("&quot;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// shape builds a drawing shape through [shapeSVG], or its bounding box
// with a warning for one it does not draw; its text, when it has
// paragraphs, is left out with a warning.
func (b *builder) shape(c *odf.Element, g frameGeom, depth int) *layout.Node {
	n := b.shapeBody(c, g)
	if text := b.shapeText(c, depth); text != nil && n != nil {
		return b.withText(n, text, g)
	}
	return n
}

// shapeText returns the text a drawing shape holds (ODF 1.3, Part 3,
// 10.3: its paragraphs, headings and lists), nil when it holds none
// that draws.
func (b *builder) shapeText(c *odf.Element, depth int) *layout.Node {
	var kids []*layout.Node
	ctx := blockCtx{depth: depth + 1}
	for _, k := range c.Children {
		if k.Name.Space == odf.NSText && (k.Name.Local == "p" || k.Name.Local == "h" || k.Name.Local == "list") {
			kids = append(kids, b.block(k, &ctx)...)
		}
	}
	if !hasText(kids) {
		return nil
	}
	b.spacing(kids)
	st := baseStyle()
	st.Display = css.Block
	return b.node(&layout.Node{Kind: layout.Element, Style: st, Children: kids})
}

// withText lays a shape's text over its drawing n: a box of the shape's
// size, its padding the text area's insets (fo:padding), the drawing
// behind at its corner and the text placed in the text area as
// draw:textarea-vertical-align says (20.137), at the top when absent.
func (b *builder) withText(n, text *layout.Node, g frameGeom) *layout.Node {
	st := baseStyle()
	st.Display = css.InlineBlock
	st.Position = css.Relative
	st.BoxSizing = css.BorderBox
	st.Width, st.Height = css.Pts(math.Max(g.w, 1)), css.Pts(math.Max(g.h, 1))
	var pad [4]float64
	_, pad = foldBox(st.Border, pad, g.props)
	for i := range pad {
		st.Padding[i] = css.Pts(max(pad[i], 0))
	}
	area := baseStyle()
	area.Display = css.Flex
	area.FlexDirection = css.FlexColumn
	area.Height = css.Dimension{Kind: css.Percent, Value: 1}
	// Positioned, the text paints over the drawing before it (CSS 2.1,
	// Appendix E).
	area.Position = css.Relative
	area.JustifyContent = css.BoxFlexStart
	switch g.props.Get("draw:textarea-vertical-align") {
	case "middle":
		area.JustifyContent = css.BoxCenter
	case "bottom":
		area.JustifyContent = css.BoxFlexEnd
	}
	pic := *n.Style
	pic.Position = css.Absolute
	pic.Display = blockified(pic.Display)
	pic.Left, pic.Top = css.Pts(0), css.Pts(0)
	pic.Right, pic.Bottom = css.Dimension{Kind: css.Auto}, css.Dimension{Kind: css.Auto}
	n.Style = &pic
	inner := b.node(&layout.Node{Kind: layout.Element, Style: area, Children: []*layout.Node{text}})
	return b.node(&layout.Node{Kind: layout.Element, Style: st, Role: "Figure", Children: []*layout.Node{n, inner}})
}

// customShape returns the picture of a draw:custom-shape (ODF 1.3, Part
// 3, 10.6.1): its enhanced geometry drawn by the shape engine with the
// shape's fill (draw:fill, draw:fill-color) and stroke (draw:stroke,
// svg:stroke-color, svg:stroke-width), Impress's default blue fill and
// darker blue line where the style says none; nil for a geometry the
// engine draws as its box.
func (b *builder) customShape(c *odf.Element, g frameGeom) *layout.Picture {
	geom := odf.ParseGeometry(c)
	if geom == nil || !shape.Known(geom) {
		return nil
	}
	if err := shape.Check(geom); err != nil {
		b.warn("shape-custom-shape", fmt.Sprintf("a custom shape's geometry is drawn in part: %v", err))
	}
	fill := &compose.Color{R: 0x72 / 255.0, G: 0x9f / 255.0, B: 0xcf / 255.0, A: 1}
	if g.props.Get("draw:fill") == "none" {
		fill = nil
	} else if col, ok := odf.ParseColor(g.props.Get("draw:fill-color")); ok {
		fill = &compose.Color{R: float64(col.R) / 255, G: float64(col.G) / 255, B: float64(col.B) / 255, A: 1}
	}
	line := shape.Line{Width: 0.75, Color: compose.Color{R: 0x34 / 255.0, G: 0x65 / 255.0, B: 0xa4 / 255.0, A: 1}, Join: compose.RoundJoin}
	if g.props.Get("draw:stroke") == "none" {
		line.Width = 0
	} else {
		if col, ok := odf.ParseColor(g.props.Get("svg:stroke-color")); ok {
			line.Color = compose.Color{R: float64(col.R) / 255, G: float64(col.G) / 255, B: float64(col.B) / 255, A: 1}
		}
		if sw, ok := odf.Length(g.props.Get("svg:stroke-width")); ok && sw > 0 {
			line.Width = sw
		}
	}
	w, h := math.Max(g.w, 1), math.Max(g.h, 1)
	pic := &layout.Picture{Width: w, Height: h}
	pic.Vector = func(_ context.Context, r compose.Rect) []compose.Fragment {
		var out, strokes []compose.Fragment
		for _, p := range shape.Paths(geom, r) {
			if p.Fill && fill != nil {
				out = append(out, shape.Fill(p.Ops, r, shape.Paint{Kind: shape.PaintSolid, Color: shape.Shade(*fill, p.Mode)})...)
			}
			if p.Stroke {
				strokes = append(strokes, shape.Stroke(p.Ops, line)...)
			}
		}
		return append(out, strokes...)
	}
	return pic
}

// shapeBody builds the drawing of a shape without its text.
func (b *builder) shapeBody(c *odf.Element, g frameGeom) *layout.Node {
	if c.Is(odf.NSDraw, "custom-shape") {
		if pic := b.customShape(c, g); pic != nil {
			return b.shapeNode(c, pic)
		}
	}
	src := shapeSVG(c, g)
	if src == "" {
		b.warn("shape-"+c.Name.Local, fmt.Sprintf("the shape %s is drawn as its empty box", c.Name.Local))
		st := b.frameBox(g)
		st.Display = css.InlineBlock
		return b.node(&layout.Node{Kind: layout.Element, Style: st})
	}
	doc, err := svg.Parse(b.ctx, []byte(src), svg.Options{})
	if err != nil {
		return nil
	}
	pic := &layout.Picture{Width: math.Max(g.w, 1), Height: math.Max(g.h, 1), Vector: doc.Draw}
	if c.Is(odf.NSDraw, "line") {
		s := doc.Size()
		pic.Width, pic.Height = s.Width, s.Height
	}
	return b.shapeNode(c, pic)
}

// shapeNode returns the replaced node drawing a shape's picture.
func (b *builder) shapeNode(c *odf.Element, pic *layout.Picture) *layout.Node {
	st := baseStyle()
	st.Display = css.Inline
	st.Width, st.Height = css.Pts(pic.Width), css.Pts(pic.Height)
	if c.A(odf.NSDraw, "transform") != "" {
		b.warn("shape-transform", "a shape's draw:transform is not applied")
	}
	return b.node(&layout.Node{Kind: layout.Replaced, Style: st, Picture: pic, Role: "Figure"})
}
