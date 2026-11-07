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
	"strings"

	_ "golang.org/x/image/bmp"  // registers BMP with image.Decode
	_ "golang.org/x/image/tiff" // registers TIFF with image.Decode

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/odf"
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
	vpos       string
	props      odf.Props
}

// geometry reads a drawing element's geometry and graphic style.
func (b *builder) geometry(c *odf.Element) frameGeom {
	g := frameGeom{anchor: c.A(odf.NSText, "anchor-type")}
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
	g.vpos = g.props.Get("style:vertical-pos")
	return g
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
			if g.props.Get("style:vertical-rel") == "line" || g.props.Get("style:vertical-rel") == "text" {
				n.Style.VerticalAlign = css.VerticalAlign{Kind: css.TextTop}
			}
		}
		*out = append(*out, n)
	case "page":
		b.place(n, g, true)
	case "frame":
		b.place(n, g, false)
		*out = append(*out, n)
	default:
		b.place(n, g, false)
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
	b.place(n, g, g.anchor == "page" || g.hrel == "page" || g.props.Get("style:vertical-rel") == "page")
	if n.Style.Position == css.Static && n.Style.Float == css.FloatNone && n.Style.Display != css.Block {
		st := *n.Style
		st.Display = css.Block
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
		st.Left, st.Top = css.Pts(g.x), css.Pts(g.y)
		if page {
			b.pageAnchor(&st, g.x, g.y)
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
	if g.vpos == "from-top" || g.vpos == "" {
		st.Margin[css.Top] = css.Pts(margins[css.Top] + max(g.y, 0))
	}
	if g.wrap == "none" {
		st.Display = css.Block
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
		if st.Border[css.Top].Style == css.BorderNone && g.props.Get("fo:border") == "" {
			// A control draws its own frame, which Writer prints as a thin
			// sunken border.
			bd := css.Border{Width: 0.75, Style: css.Solid, Color: css.Color{R: 0.5, G: 0.5, B: 0.5, A: 1}}
			st.Border = [4]css.Border{bd, bd, bd, bd}
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
		return b.shape(c, g)
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
	n := b.node(&layout.Node{Kind: layout.Element, Style: st, Role: "Div"})
	n.Children = b.blocks(k, blockCtx{depth: depth + 1})
	return n
}

// shapeSVG returns an SVG drawing of a shape (ODF 1.3, Part 3, 10.3) in
// its own box: rectangles, ellipses, circles, lines, polylines, polygons
// and paths with their viewBox, and the custom shapes whose preset
// geometry is a rectangle or an ellipse; "" for a shape it does not draw.
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
	case "custom-shape":
		geo := c.Child(odf.NSDraw, "enhanced-geometry")
		switch geo.A(odf.NSDraw, "type") {
		case "rectangle", "flowchart-process":
			body = fmt.Sprintf(`<rect x="0" y="0" width="%g" height="%g" %s/>`, w, h, paint)
		case "round-rectangle", "flowchart-alternate-process":
			body = fmt.Sprintf(`<rect x="0" y="0" width="%g" height="%g" rx="%g" %s/>`, w, h, math.Min(w, h)/6, paint)
		case "ellipse", "circle", "flowchart-connector":
			body = fmt.Sprintf(`<ellipse cx="%g" cy="%g" rx="%g" ry="%g" %s/>`, w/2, h/2, w/2, h/2, paint)
		default:
			return ""
		}
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
func (b *builder) shape(c *odf.Element, g frameGeom) *layout.Node {
	src := shapeSVG(c, g)
	if c.Child(odf.NSText, "p") != nil {
		b.warn("shape-text", "the text of a drawing shape is left out")
	}
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
	st := baseStyle()
	st.Display = css.Inline
	st.Width, st.Height = css.Pts(pic.Width), css.Pts(pic.Height)
	if c.A(odf.NSDraw, "transform") != "" {
		b.warn("shape-transform", "a shape's draw:transform is not applied")
	}
	return b.node(&layout.Node{Kind: layout.Replaced, Style: st, Picture: pic, Role: "Figure"})
}
