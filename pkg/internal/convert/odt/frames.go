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

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/intconv"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/odf"
	"github.com/gotenberg/litho/pkg/internal/shape"
	"github.com/gotenberg/litho/pkg/internal/svg"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
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
	if len(b.pics) >= limits.MaxConvertPictures {
		b.warn("too-many-pictures", warn.LimitReached, warn.Omitted, fmt.Sprintf("pictures past %d are not drawn", limits.MaxConvertPictures))
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
			b.warn("external-picture", warn.PictureMissing, warn.Omitted, "a linked picture outside the package is not fetched")
			return nil
		}
		if b.pkg.Flat {
			b.warn("missing-picture", warn.PictureMissing, warn.Omitted, fmt.Sprintf("the picture %s is not in the document", href))
			return nil
		}
		d, err := b.pkg.ReadFile(b.ctx, href)
		if err != nil {
			if b.stop(err) {
				return nil
			}
			b.warn("missing-picture", warn.PictureMissing, warn.Omitted, fmt.Sprintf("the picture %s is not read: %v", href, err))
			return nil
		}
		data = d
	default:
		d, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(bin.TextContent()), ""))
		if err != nil {
			b.warn("binary-data", warn.PictureUnreadable, warn.Omitted, "an inline picture's base64 does not decode")
			return nil
		}
		data = d
	}
	if name := vectorFormat(data); name != "" {
		b.warn("picture-"+strings.ToLower(name), warn.PictureFormatUnsupported, warn.Omitted, fmt.Sprintf("%s pictures are not drawn", name))
		return nil
	}
	if isSVG(data) {
		doc, err := svg.Parse(b.ctx, data, svg.Options{Fonts: b.fonts})
		if err != nil {
			if b.stop(err) {
				return nil
			}
			b.warn("svg", warn.PictureUnreadable, warn.Omitted, fmt.Sprintf("an SVG picture is not drawn: %v", err))
			return nil
		}
		svg.Collect(b.ctx, doc, svg.PictureLabel(href))
		s := doc.Size()
		w, h := s.Width, s.Height
		if !s.HasWidth || !s.HasHeight || w <= 0 || h <= 0 {
			w, h = 300*css.Pt, 150*css.Pt
		}
		return &layout.Picture{Width: w, Height: h, Vector: doc.Draw}
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		b.warn("picture-format", warn.PictureFormatUnsupported, warn.Omitted, "a picture in an unknown format is not drawn")
		return nil
	}
	mem := limits.BudgetFrom(b.ctx)
	if err := mem.CheckPicture(cfg.Width, cfg.Height); err != nil {
		b.warn("picture-pixels", warn.LimitReached, warn.Omitted, fmt.Sprintf("a picture is not drawn: %v", err))
		return nil
	}
	pic := &layout.Picture{Width: float64(cfg.Width) * css.Pt, Height: float64(cfg.Height) * css.Pt}
	if format == "jpeg" && dctModel(cfg.ColorModel) {
		src, err := typeset.JPEGImage(b.ctx, data)
		if err != nil {
			if b.stop(err) {
				return nil
			}
			b.warn("picture-decode", warn.PictureUnreadable, warn.Omitted, "a jpeg picture does not decode")
			return nil
		}
		pic.Source = src
		return pic
	}
	charged, err := mem.ChargePicture(cfg.Width, cfg.Height, cfg.ColorModel)
	if err != nil {
		if b.stop(err) {
			return nil
		}
		b.warn("picture-pixels", warn.LimitReached, warn.Omitted, fmt.Sprintf("a picture is not drawn: %v", err))
		return nil
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		mem.Release(charged)
		b.warn("picture-decode", warn.PictureUnreadable, warn.Omitted, fmt.Sprintf("a %s picture does not decode", format))
		return nil
	}
	src, err := typeset.EncodeImage(b.ctx, img)
	if err != nil {
		if b.stop(err) {
			return nil
		}
		b.warn("picture-decode", warn.PictureUnreadable, warn.Omitted, fmt.Sprintf("a %s picture does not encode: %v", format, err))
		return nil
	}
	pic.Source = src
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
//
// # Corpus evidence
//
//   - libreoffice/odt: odfexport/shape-with-hyperlink.odt anchors a
//     linked custom shape to its paragraph on its draw:a alone, and
//     LibreOffice prints it 4.1 cm and 5 cm from the paragraph.
func (b *builder) geometry(c *odf.Element) frameGeom {
	g := frameGeom{anchor: c.A(odf.NSText, "anchor-type")}
	if g.anchor == "" {
		// LibreOffice writes the anchor of a drawing that is a link on
		// its draw:a, which ODF 1.3 Part 3, 10.4.12 gives no anchor, and
		// places the drawing by it.
		g.anchor = b.linkAnchors[c]
	}
	if n, err := intconv.Atoi(c.A(odf.NSText, "anchor-page-number")); err == nil && n > 0 && g.anchor == "page" {
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
	if c.Is(odf.NSDraw, "line") {
		// A line states its ends, not its box (ODF 1.3 Part 3, 10.3.3,
		// svg:x1 to svg:y2): its box spans them, placed as a frame is.
		x1, _ := odf.Length(c.A(odf.NSSVG, "x1"))
		y1, _ := odf.Length(c.A(odf.NSSVG, "y1"))
		x2, _ := odf.Length(c.A(odf.NSSVG, "x2"))
		y2, _ := odf.Length(c.A(odf.NSSVG, "y2"))
		g.x, g.y = min(x1, x2), min(y1, y2)
		g.w, g.h, g.hasW, g.hasH = math.Abs(x2-x1), math.Abs(y2-y1), true, true
	}
	g.props = b.styles.Resolve(b.scope, odf.FamilyGraphic, c.A(odf.NSDraw, "style-name"), odf.GraphicProps)
	g.wrap = g.props.Get("style:wrap")
	g.hpos, g.hrel = g.props.Get("style:horizontal-pos"), g.props.Get("style:horizontal-rel")
	g.vpos, g.vrel = g.props.Get("style:vertical-pos"), g.props.Get("style:vertical-rel")
	// LibreOffice writes a position against the page's top or bottom
	// margin area, which ODF 1.3 does not name, as loext:vertical-rel
	// beside a style:vertical-rel ODF 1.3 knows.
	if v := g.props.Get("loext:vertical-rel"); v == "page-content-top" || v == "page-content-bottom" {
		g.vrel = v
	}
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
// against the page, its content area or its top or bottom margin area
// vertically (20.387, style:vertical-rel page and page-content;
// LibreOffice's loext:vertical-rel page-content-top and
// page-content-bottom).
func (g frameGeom) pageRelative() bool {
	switch g.vrel {
	case "page", "page-content", "page-content-top", "page-content-bottom":
		return true
	}
	return g.anchor == "page"
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
	strokeBorders(st)
	for i := range pad {
		st.Padding[i] = css.Pts(pad[i])
	}
	if v := g.props.Get("fo:background-color"); v != "" {
		if col, ok := odf.ParseColor(v); ok {
			st.BackgroundColor = cssColor(col)
		}
	}
	switch g.props.Get("draw:fill") {
	case "solid", "gradient", "bitmap", "hatch":
		st.BackgroundColor = css.Color{}
		b.frameFill(st, g.props, g.w, g.h)
	}
	return st
}

// pageFill gives st, the box of a frame of geometry g holding a text box,
// the background of the page it is on, white when the page has none, when
// its graphic style fills it with nothing: Writer paints a text frame
// without a fill of its own, fo:background-color transparent and
// draw:fill none included, in the page's background. A picture's frame
// keeps no fill. ODF 1.3, Part 3,
// 20.175 and 20.106 leave such a frame transparent; the fill is Writer's.
// A gradient, hatch or bitmap fill, which litho does not paint on a
// frame, keeps no page colour under it, nor does a page filled with
// one, which the frame shows through.
//
// # Corpus evidence
//
//   - libreoffice/odt: odfimport/tdf100033_1.odt fills its text frame,
//     whose style Frame sets no background, in white.
//   - libreoffice/odt: odfimport/PageBackground.odt shows its page's
//     bitmap through four frames without a fill.
//   - probe, odt: through gotenberg/gotenberg:9.4.1, a text frame of the
//     style Frame prints filled in white, also with fo:background-color
//     transparent or draw:fill none, under the style Graphics or a
//     common style of its own, and in yellow on a page whose layout sets
//     fo:background-color #ffff00; a picture frame of the style Graphics
//     prints no fill.
func (b *builder) pageFill(st *css.Style, g frameGeom) {
	if st.BackgroundColor.A > 0 {
		return
	}
	switch g.props.Get("draw:fill") {
	case "gradient", "hatch", "bitmap":
		return
	}
	pg := b.currentGeom()
	if pg.pictureFill {
		return
	}
	st.BackgroundColor = css.Color{R: 1, G: 1, B: 1, A: 1}
	if bg := pg.box.Background; bg != nil && bg.BackgroundColor.A > 0 {
		st.BackgroundColor = bg.BackgroundColor
	}
}

// inlineFrame appends a drawing element met in a paragraph's text (ODF
// 1.3, Part 3, 10): a frame anchored as a character is an atomic inline
// box on the line; one anchored to the paragraph or a character floats
// beside the text as style:wrap says, or sits in the flow above the
// paragraph when nothing wraps around it; draw:a makes its frame a link.
func (b *builder) inlineFrame(c *odf.Element, out *[]*layout.Node, ws *wsState, depth int) {
	if c.Is(odf.NSDraw, "a") {
		var kids []*layout.Node
		anchor := c.A(odf.NSText, "anchor-type")
		for _, k := range c.Children {
			if anchor != "" && k.A(odf.NSText, "anchor-type") == "" {
				if b.linkAnchors == nil {
					b.linkAnchors = map[*odf.Element]string{}
				}
				b.linkAnchors[k] = anchor
			}
			b.inlineFrame(k, &kids, ws, depth+1)
		}
		*out = append(*out, b.node(&layout.Node{Kind: layout.Element, Style: baseStyle(), Href: c.A(odf.NSXLink, "href"), Role: "Link", Children: kids}))
		return
	}
	if b.duplicateFrame(c) {
		return
	}
	n := b.drawing(c, depth)
	b.frameRead(c)
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
	if b.duplicateFrame(c) {
		return nil
	}
	n := b.drawing(c, ctx.depth)
	b.frameRead(c)
	if n == nil {
		return nil
	}
	g := b.geometry(c)
	b.place(n, g, g.pageRelative() || g.hrel == "page")
	if g, ok := b.anchored[n]; ok {
		b.anchorFrame(n, g, nil)
	}
	if n.Style.Float == css.FloatNone && n.Style.Display != css.Block && (n.Kind != layout.Replaced || n.Style.Position == css.Absolute) {
		st := *n.Style
		st.Display = blockified(st.Display)
		n.Style = &st
	}
	return []*layout.Node{n}
}

// duplicateFrame reports a draw:frame of the body that Writer drops on
// import as a copy of the frame before it: one that repeats the draw:name
// of the frame read just before it, with no other element of a text body
// between them, when the first frame of that name has the same svg:x,
// svg:y, svg:width and svg:height. Its anchor, page, style, z-index and
// content are not compared. ODF 1.3, Part 3, 19.197 says nothing of
// repeated names: the rule is Writer's import (XMLTextImportHelper::
// IsDuplicateFrame). Shapes are never dropped, nor a frame of a header or
// footer, which litho builds once a page.
//
// # Corpus evidence
//
//   - libreoffice/odt: odfimport/tdf100033_1.odt repeats a page-anchored
//     text frame three times; the reference draws it once.
//   - probe, odt: through gotenberg/gotenberg:9.4.1, three frames of one
//     name and place holding "AAA", "BBB" and "CCC" print "AAA" alone,
//     whatever their z-index or anchor page, anchored to the page, to one
//     paragraph or as characters in it; they print all three at three x,
//     y, widths or heights, under three names, in three paragraphs, or
//     when a frame of another name stands between the first and the
//     third; three pictures of one name and place print one picture;
//     three custom shapes or rectangles of one name and place print
//     three.
func (b *builder) duplicateFrame(c *odf.Element) bool {
	name := c.A(odf.NSDraw, "name")
	if b.hf != nil || !c.Is(odf.NSDraw, "frame") || name == "" {
		return false
	}
	var key [4]int64
	for i, attr := range []string{"x", "y", "width", "height"} {
		v, _ := odf.Length(c.A(odf.NSSVG, attr))
		// Writer compares the lengths in hundredths of a millimetre.
		key[i] = int64(math.Round(v * 2540 / 72))
	}
	first, seen := b.frameNames[name]
	if !seen {
		if b.frameNames == nil {
			b.frameNames = map[string][4]int64{}
		}
		b.frameNames[name] = key
		return false
	}
	return b.lastFrame == name && first == key
}

// frameRead records that c, a drawing element of the body whose content
// is built, was read last: the elements of a text box's body come before
// the frame's end, which Writer records the frame's name at.
func (b *builder) frameRead(c *odf.Element) {
	if b.hf == nil && c.Is(odf.NSDraw, "frame") {
		b.lastFrame = c.A(odf.NSDraw, "name")
	}
}

// place positions an anchored frame n: run-through and page-anchored
// frames are positioned boxes at their offsets, the others float to the
// side style:wrap leaves them, or stand in the flow when no text wraps.
// Writer numbers a first page of left usage 2, a left page being even, so
// a frame anchored to page n of such a document lands on the n-1th page
// printed.
//
// # Corpus evidence
//
//   - libreoffice/odt: layout/stable-at-page-anchored-fly-position.odt
//     anchors frames to the pages of a document whose first page is a
//     left one.
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
			if g.page > 1 && b.firstPageLeft() {
				st.AnchorPage = g.page - 1
			}
			applyFrameWrap(&st, g.wrap, frameMargins(g.props))
			x, y := b.pagePosition(g)
			st.Left, st.Top = css.Pts(x), css.Pts(y)
			if hf, ok := b.heightFrame(n, g); ok {
				b.heightFrames = append(b.heightFrames, hf)
			}
		case paraRel(g.hrel):
			st.Left = css.Pts(alignX(g, b.contentWidth()))
		}
		st.Right, st.Bottom = css.Dimension{Kind: css.Auto}, css.Dimension{Kind: css.Auto}
		st.ZIndex = css.ZIndex{Level: 0}
		return
	}
	if b.offsetAnchored(g) {
		st.Float, st.Display = css.FloatLeft, blockified(st.Display)
		st.Margin = [4]css.Dimension{css.Pts(0), css.Pts(0), css.Pts(0), css.Pts(0)}
		st.WordAnchor = css.WordAnchor{Set: true, Writer: true}
		applyFrameWrap(&st, g.wrap, frameMargins(g.props))
		// Writer keeps a frame inside its page, a frame taller than the
		// page at its top.
		st.PageClamp = true
		if b.anchored == nil {
			b.anchored = map[*layout.Node]frameGeom{}
		}
		b.anchored[n] = g
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
		if g.vpos == "top" && g.vrel == "paragraph" && b.hf == nil {
			if b.topFrames == nil {
				b.topFrames = map[*layout.Node]bool{}
			}
			b.topFrames[n] = true
		}
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

// offsetAnchored reports a frame of geometry g that Writer places at its
// offsets from the paragraph it is anchored to, whatever else is there,
// the text wrapping it as style:wrap says (ODF 1.3 Part 3, 20.393):
// anchored to a paragraph of the body (19.758 text:anchor-type), placed
// from the top of the paragraph's area or its content area (20.386
// style:vertical-pos from-top, or top for a frame text wraps beside;
// 20.387 style:vertical-rel paragraph and paragraph-content), and across
// the paragraph, its content area, the page or its content area (20.290,
// 20.291). A frame no text wraps placed at the top of its paragraph keeps
// [builder.frameFirst]; a run-through frame is positioned in its
// paragraph.
func (b *builder) offsetAnchored(g frameGeom) bool {
	if b.hf != nil || b.boxed > 0 || g.anchor != "paragraph" {
		return false
	}
	switch g.wrap {
	case "parallel", "left", "right", "dynamic", "biggest", "none":
	default:
		return false
	}
	switch g.vpos {
	case "", "from-top", "top":
	default:
		return false
	}
	switch g.vrel {
	case "", "paragraph", "paragraph-content":
	default:
		return false
	}
	switch g.hpos {
	case "", "from-left", "left", "inside", "center", "right", "outside":
	default:
		return false
	}
	switch g.hrel {
	case "", "paragraph", "paragraph-content", "page", "page-content":
		return true
	}
	return false
}

// anchorFrames sets the offsets of the frames among kids, a paragraph's
// inline content, that [builder.offsetAnchored] places, from the
// paragraph's style st: across from the column (the page's content area),
// whatever the paragraph's indents, or from the paragraph's content area,
// its indents, borders and padding inside, for paragraph-content (ODF 1.3
// Part 3, 20.290, 20.291); down from the top of the paragraph's area, its
// top margin included and the bottom margin of the paragraph before it
// not, or from the top of its content area (20.386, 20.387). svg:x and
// svg:y place the frame's border box, its margins being the room the text
// keeps from it. Frames overlap one another freely.
//
// # Corpus evidence
//
//   - libreoffice/odt: odfimport/tdf100033_2.odt prints three frames of
//     one paragraph style at 0.333 cm below their paragraphs, overlapping,
//     the text beside them.
//   - probe, odt: through gotenberg/gotenberg:9.4.1, in a paragraph of
//     2 cm left indent and 1 cm top margin after one of 0.5 cm bottom
//     margin, a frame at svg:x 3 cm and svg:y 0 prints 5 cm from the page
//     edge, its top 0.5 cm below the paragraph before; at svg:y 1 cm
//     against paragraph-content, 3 cm right of the indent and 1 cm below
//     the first line's top; svg:y -1.5 cm above the paragraph; two frames
//     of one paragraph overlap; a frame of none wrap at svg:y 1 cm moves
//     the paragraph's first line below it; a centred frame placed at the
//     top centres in the column.
func (b *builder) anchorFrames(kids []*layout.Node, st *css.Style, depth int) {
	if len(b.anchored) == 0 || depth > odf.MaxDepth {
		return
	}
	for _, k := range kids {
		if g, ok := b.anchored[k]; ok {
			b.anchorFrame(k, g, st)
		} else if k.Kind == layout.Element && k.Style.Float == css.FloatNone && k.Style.Position == css.Static {
			b.anchorFrames(k.Children, st, depth+1)
		}
	}
}

// anchorFrame sets the offsets of the frame n of geometry g, anchored to
// a paragraph of style st, nil for a frame of the body that stands before
// the paragraph it is anchored to, whose area starts where it stands.
func (b *builder) anchorFrame(n *layout.Node, g frameGeom, st *css.Style) {
	edge := func(s css.Side) float64 {
		if st == nil {
			return 0
		}
		w := st.Padding[s].Value + st.Margin[s].Value
		if st.Border[s].Style != css.BorderNone && st.Border[s].Style != css.BorderHidden {
			w += st.Border[s].Width
		}
		return w
	}
	pg := b.currentGeom()
	col := b.contentWidth()
	x0, w := 0.0, col
	switch g.hrel {
	case "paragraph-content":
		x0, w = edge(css.Left), col-edge(css.Left)-edge(css.Right)
	case "page":
		x0, w = -pg.box.Margin[css.Left], pg.box.Width
	}
	a := css.WordAnchor{Set: true, Writer: true}
	switch {
	case g.hasW || g.hpos == "" || g.hpos == "from-left":
		a.X = x0 + alignX(g, w)
	case g.hpos == "center":
		a.X, a.AlignX = x0+float64(w/2), 0.5
	case g.hpos == "right" || g.hpos == "outside":
		a.X, a.AlignX = x0+w, 1
	default:
		a.X = x0
	}
	a.Y = g.y
	if g.vpos == "top" {
		// A frame placed at the top puts its margin box there.
		a.Y = frameMargins(g.props)[css.Top]
	}
	// The first line's top lies below the paragraph's area by its top
	// margin, border and padding.
	a.Rise = edge(css.Top)
	if g.vrel == "paragraph-content" {
		a.Y += a.Rise
	}
	fs := *n.Style
	fs.WordAnchor = a
	n.Style = &fs
}

// frameFirst moves the frames of kids, a paragraph's inline content, that
// stand at the top of the paragraph with no text beside them to its start,
// and returns kids and the count of those frames. Writer sets a frame
// anchored to a paragraph, wrapped by no text (ODF 1.3, Part 3, 20.393
// style:wrap none) and placed at the top of the paragraph's area (20.386
// style:vertical-pos top, 20.387 style:vertical-rel paragraph), its top
// margin below the paragraph's top, and the paragraph's text, the text
// before the frame and an empty paragraph's line included, below its
// bottom margin; the paragraph's own top margin is lost under the frame,
// and the frame's margins add to the spacing of the paragraphs around it.
// The paragraph's style st takes the first frame's top margin as its own,
// which [builder.spacing] adds to the bottom margin of the block before.
//
// # Corpus evidence
//
//   - litho/odt: captions sets a 3.6 cm caption frame, margins 0.201 cm,
//     at the top of an empty paragraph after one of a 0.2 cm bottom
//     margin: LibreOffice prints the picture 5.7 points below that
//     paragraph's top and the next paragraph a line and 0.2 cm below the
//     frame's bottom margin.
//   - probe, odt: through gotenberg/gotenberg:9.4.1, the frame's margins
//     at 0 move the caption up 5.7 points and the next paragraph 11.4;
//     at 1 cm and 0.5 cm they move them down by 28.35 and 42.5 points; a
//     1 cm bottom margin before the anchor paragraph moves the frame down
//     by its difference from 0.2 cm; a 1 cm top margin of the anchor
//     paragraph moves nothing; text before or after the frame in its
//     paragraph prints below the frame, 5.7 points below its bottom.
func (b *builder) frameFirst(kids []*layout.Node, st *css.Style) ([]*layout.Node, int) {
	if len(b.topFrames) == 0 {
		return kids, 0
	}
	var top, rest []*layout.Node
	for _, k := range kids {
		if b.topFrames[k] {
			top = append(top, k)
		} else {
			rest = append(rest, k)
		}
	}
	if len(top) == 0 {
		return kids, 0
	}
	first := *top[0].Style
	st.Margin[css.Top] = first.Margin[css.Top]
	first.Margin[css.Top] = css.Pts(0)
	top[0].Style = &first
	return append(top, rest...), len(top)
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
		x = x0 + float64((x1-x0-w)/2)
	case "right", "outside":
		x = x1 - w
	}
	y0, y1 := 0.0, ph
	switch {
	case g.vrel == "page-content-top":
		y1 = pg.top
	case g.vrel == "page-content-bottom":
		y0 = pg.bottom
	case g.vrel == "page-content" || g.vrel != "page" && (g.vrel != "" || g.anchor != "page"):
		y0, y1 = pg.top, pg.bottom
	}
	y := y0 + g.y
	switch g.vpos {
	case "top":
		y = y0
	case "middle", "center":
		y = y0 + float64((y1-y0-h)/2)
	case "bottom":
		y = y1 - h
	}
	return x, y
}

// heightFrame is a frame positioned from the page at a height that
// depends on the page's: centred in the page or its content area, or at
// its bottom (ODF 1.3, Part 3, 20.386 style:vertical-pos). On a single
// page it is placed first on its master page's page, pageH tall, then
// moved by share times what the single page's height changes.
type heightFrame struct {
	n            *layout.Node
	share, pageH float64
}

// heightFrame returns the page-anchored frame n of geometry g as a
// [heightFrame] when the conversion lays a single page out and its
// position depends on the page's height.
func (b *builder) heightFrame(n *layout.Node, g frameGeom) (heightFrame, bool) {
	if !b.single {
		return heightFrame{}, false
	}
	var share float64
	switch g.vpos {
	case "middle", "center":
		share = 0.5
	case "bottom":
		share = 1
	default:
		return heightFrame{}, false
	}
	return heightFrame{n: n, share: share, pageH: b.currentGeom().box.Height}, true
}

// moveTo moves the frame to where it sits on a page h points tall. Its
// style is shared, so the node takes a moved copy. It reports whether the
// frame moved.
func (hf heightFrame) moveTo(b *builder, h float64) bool {
	dy := float64(hf.share * (h - hf.pageH))
	if dy == 0 {
		return false
	}
	st := *hf.n.Style
	st.Top = css.Pts(st.Top.Value + dy)
	hf.n.Style = b.share(&st)
	return true
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
// control as LibreOffice prints it, or a shape; nil for one it does not draw.
func (b *builder) drawing(c *odf.Element, depth int) *layout.Node {
	if depth > odf.MaxDepth {
		return nil
	}
	g := b.geometry(c)
	switch {
	case c.Is(odf.NSDraw, "frame"):
		return undescribed(b.frame(c, g, depth), g)
	case c.Is(odf.NSDraw, "control"):
		return b.formControl(c, g)
	case c.Is(odf.NSDraw, "g"):
		st := baseStyle()
		st.Display = css.InlineBlock
		n := b.node(&layout.Node{Kind: layout.Element, Style: st})
		if alt := odf.Describe(c); alt != "" {
			n.Role, n.Alt = "Figure", alt
		}
		for _, k := range c.Children {
			if k.Name.Space == odf.NSDraw {
				if kn := b.drawing(k, depth+1); kn != nil {
					n.Children = append(n.Children, kn)
				}
			}
		}
		b.warn("draw-group", warn.ContentUnsupported, warn.Approximated, "the shapes of a group are laid out side by side")
		return n
	case c.Name.Space == odf.NSDraw:
		return undescribed(b.shape(c, g, depth), g)
	}
	return nil
}

// undescribed returns n, a drawing of geometry g, as an artifact when its
// graphic properties mark it decorative and it shows no text (ISO
// 32000-1, 14.8.2.2.2; ISO 14289-1, 7.1). A drawing that shows text keeps
// its Figure, which holds the text's paragraphs.
func undescribed(n *layout.Node, g frameGeom) *layout.Node {
	if n != nil && n.Kind == layout.Replaced && n.Role == "Figure" && odf.Decorative(g.props) {
		n.Role, n.Alt = "", ""
	}
	return n
}

// frame builds a draw:frame (10.4.2) from its first content it draws: a
// picture, a text box, or an object's replacement picture.
func (b *builder) frame(c *odf.Element, g frameGeom, depth int) *layout.Node {
	alt := odf.Describe(c)
	var object *odf.Element
	for _, k := range c.Children {
		switch {
		case k.Is(odf.NSDraw, "image"):
			pic := b.picture(k.A(odf.NSXLink, "href"), k.Child(odf.NSOffice, "binary-data"))
			if pic == nil {
				continue
			}
			if alt == "" {
				alt = b.captions[c]
			}
			if alt == "" && object != nil && b.derivedAlt {
				if derived, lang := b.objectAlt(object); derived != "" {
					n := b.imageNode(pic, g, derived)
					// The node holds no text: its language is its
					// description's.
					n.Lang = lang
					return n
				}
			}
			return b.imageNode(pic, g, alt)
		case k.Is(odf.NSDraw, "text-box"):
			n := b.textBox(k, g, depth)
			if hasText(n.Children) {
				n.Role, n.Alt = "Figure", alt
				if alt == "" {
					n.Alt = office.ShownText(n.Children)
				}
			}
			if l, ok := b.chains[c.A(odf.NSDraw, "name")]; ok {
				applyChain(n.Style, l)
			}
			return n
		case k.Is(odf.NSDraw, "object"):
			if n, ok := b.formula(k, g, alt); ok {
				if n == nil {
					continue
				}
				return n
			}
			// The replacement picture, a draw:image after the object, is
			// tried next.
			object = k
			b.warn("object", warn.ContentUnsupported, warn.Approximated, "embedded objects are drawn as their replacement picture")
		case k.Is(odf.NSDraw, "object-ole"):
			b.warn("object", warn.ContentUnsupported, warn.Approximated, "embedded objects are drawn as their replacement picture")
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
	out.Vector = func(ctx context.Context, r typeset.Rect) []typeset.Fragment {
		sx, sy := r.Width/w, r.Height/h
		full := typeset.Rect{X: r.X - float64(in[css.Left]*sx), Y: r.Y - float64(in[css.Top]*sy), Width: src.Width * sx, Height: src.Height * sy}
		var content []typeset.Fragment
		if src.Vector != nil {
			content = src.Vector(ctx, full)
		} else {
			content = []typeset.Fragment{&typeset.Image{Rect: full, Source: src.Source}}
		}
		clip := &typeset.Path{Ops: rectOps(r)}
		return []typeset.Fragment{&typeset.Group{Clip: clip, Content: content}}
	}
	return out
}

// rectOps returns the path of the rectangle r.
func rectOps(r typeset.Rect) []typeset.PathOp {
	return []typeset.PathOp{
		{Kind: typeset.MoveTo, P: [3]typeset.Point{{X: r.X, Y: r.Y}}},
		{Kind: typeset.LineTo, P: [3]typeset.Point{{X: r.X + r.Width, Y: r.Y}}},
		{Kind: typeset.LineTo, P: [3]typeset.Point{{X: r.X + r.Width, Y: r.Y + r.Height}}},
		{Kind: typeset.LineTo, P: [3]typeset.Point{{X: r.X, Y: r.Y + r.Height}}},
		{Kind: typeset.ClosePath},
	}
}

// textBox builds a draw:text-box (10.4.3): a block of its paragraphs in
// the frame's box, at least its minimum height. [builder.frame] tags one
// that shows text as a Figure: a frame is anchored in a paragraph and
// lies inside its P, which ISO 32005, Table 5 lets hold a Figure but not
// the P and Div elements of the box's content, while a Figure holds
// them. Its alt is the frame's description, else the text it shows, so
// that a reader hears the box either way (ISO 14289-1, 7.3).
func (b *builder) textBox(k *odf.Element, g frameGeom, depth int) *layout.Node {
	st := b.frameBox(g)
	b.pageFill(st, g)
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
	n := b.node(&layout.Node{Kind: layout.Element, Style: st})
	if b.derivedAlt {
		if frame, caption := captionOf(k); frame != nil {
			if b.captions == nil {
				b.captions = map[*odf.Element]string{}
			}
			b.captions[frame] = caption
		}
	}
	restore := b.comments.apart()
	b.boxed++
	n.Children = b.wrapLists(b.blocks(k, blockCtx{depth: depth + 1}), 0)
	b.boxed--
	restore()
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
//
// A shape that shows text is a Figure, its alt the shape's description,
// else its text, as a text box is ([builder.textBox]). A shape without
// text is a Figure when its source describes it and an artifact when it
// does not: a line or a background shape with no text and no description
// decorates the page (ISO 32000-1, 14.8.2.2.2; ISO 14289-1, 7.1).
func (b *builder) shape(c *odf.Element, g frameGeom, depth int) *layout.Node {
	n := b.shapeBody(c, g)
	alt := odf.Describe(c)
	if text := b.shapeText(c, depth); text != nil && n != nil {
		if n.Role == "Figure" {
			n.Role = ""
		}
		w := b.withText(n, text, g)
		if w.Alt = alt; alt == "" {
			w.Alt = office.ShownText(text.Children)
		}
		return w
	}
	if n != nil && n.Role == "Figure" {
		n.Alt = alt
		if alt == "" {
			n.Role = ""
		}
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
	kids = b.wrapLists(kids, 0)
	st := baseStyle()
	st.Display = css.Block
	return b.node(&layout.Node{Kind: layout.Element, Style: st, Children: kids})
}

// withText lays a shape's text over its drawing n: a box of the shape's
// size, its padding the text area's insets (fo:padding), the drawing
// behind at its corner and the text placed in the text area as
// draw:textarea-vertical-align says (20.137), at the top when absent, and
// across it as draw:textarea-horizontal-align says (20.136).
//
// # Corpus evidence
//
//   - probe, odt: on gotenberg/gotenberg:9.4.1 a page-anchored draw:rect
//     of textarea-horizontal-align center and vertical-align middle
//     prints its one line centred both ways in the rectangle.
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
	// draw:textarea-horizontal-align (20.136) places the text, as wide as
	// its longest line, across the text area.
	switch g.props.Get("draw:textarea-horizontal-align") {
	case "center":
		area.AlignItems = css.BoxCenter
	case "right":
		area.AlignItems = css.BoxFlexEnd
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
		b.warn("shape-custom-shape", warn.ContentUnsupported, warn.Approximated, fmt.Sprintf("a custom shape's geometry is drawn in part: %v", err))
	}
	return geometryPicture(geom, g)
}

// basicGeometry returns the geometry of a draw:rect without rounded
// corners, a draw:ellipse or a draw:circle drawn whole (ODF 1.3, Part
// 3, 10.3.2 to 10.3.5), nil for any other shape: the engine strokes its
// outline on the edge of its box, half the line outside it, as
// LibreOffice draws it.
//
// # Corpus evidence
//
//   - probe, odt: on gotenberg/gotenberg:9.4.1 a page-anchored draw:rect
//     of a red 0.75 point line prints the line whole on its four sides,
//     centred on the edge of its box.
//   - litho/odt: tracked-changes-bars-ref draws its change bars as
//     draw:rect of no width, which LibreOffice prints as lines.
func basicGeometry(c *odf.Element) *shape.Geometry {
	switch c.Name.Local {
	case "rect":
		if r, _ := odf.Length(c.A(odf.NSDraw, "corner-radius")); r == 0 {
			return &shape.Geometry{Preset: "rect"}
		}
	case "ellipse", "circle":
		if k := c.A(odf.NSDraw, "kind"); k == "" || k == "full" {
			return &shape.Geometry{Preset: "ellipse"}
		}
	}
	return nil
}

// geometryPicture returns the picture of the geometry geom drawn with the
// fill (draw:fill, draw:fill-color) and stroke (draw:stroke,
// svg:stroke-color, svg:stroke-width) of g, LibreOffice's default blue
// fill and darker blue line where the style says none.
func geometryPicture(geom *shape.Geometry, g frameGeom) *layout.Picture {
	fill := &typeset.Color{R: 0x72 / 255.0, G: 0x9f / 255.0, B: 0xcf / 255.0, A: 1}
	if g.props.Get("draw:fill") == "none" {
		fill = nil
	} else if col, ok := odf.ParseColor(g.props.Get("draw:fill-color")); ok {
		fill = &typeset.Color{R: float64(col.R) / 255, G: float64(col.G) / 255, B: float64(col.B) / 255, A: 1}
	}
	line := shape.Line{Width: 0.75, Color: typeset.Color{R: 0x34 / 255.0, G: 0x65 / 255.0, B: 0xa4 / 255.0, A: 1}, Join: typeset.RoundJoin}
	if g.props.Get("draw:stroke") == "none" {
		line.Width = 0
	} else {
		if col, ok := odf.ParseColor(g.props.Get("svg:stroke-color")); ok {
			line.Color = typeset.Color{R: float64(col.R) / 255, G: float64(col.G) / 255, B: float64(col.B) / 255, A: 1}
		}
		if sw, ok := odf.Length(g.props.Get("svg:stroke-width")); ok && sw > 0 {
			line.Width = sw
		}
	}
	w, h := math.Max(g.w, 1), math.Max(g.h, 1)
	pic := &layout.Picture{Width: w, Height: h}
	pic.Vector = func(_ context.Context, r typeset.Rect) []typeset.Fragment {
		// A shape narrower or shorter than a point keeps a point of room
		// in the layout and draws at its own size: a rectangle of no
		// width is the line of its outline.
		r.Width = float64(r.Width * math.Max(g.w, 0) / w)
		r.Height = float64(r.Height * math.Max(g.h, 0) / h)
		var out, strokes []typeset.Fragment
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
	if geom := basicGeometry(c); geom != nil {
		return b.shapeNode(c, geometryPicture(geom, g))
	}
	src := shapeSVG(c, g)
	if src == "" {
		b.warnEach("shape-box", c.Name.Local, warn.ContentUnsupported, warn.Approximated, fmt.Sprintf("the shape %s is drawn as its empty box", c.Name.Local))
		st := b.frameBox(g)
		st.Display = css.InlineBlock
		return b.node(&layout.Node{Kind: layout.Element, Style: st})
	}
	doc, err := svg.Parse(b.ctx, []byte(src), svg.Options{})
	if err != nil {
		// The SVG is the converter's own, its path data and viewBox the
		// document's: one that does not parse draws nothing.
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
		b.warn("shape-transform", warn.ContentUnsupported, warn.Approximated, "a shape's draw:transform is not applied")
	}
	return b.node(&layout.Node{Kind: layout.Replaced, Style: st, Picture: pic, Role: "Figure"})
}
