package docx

import (
	"strings"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
)

// anchor places the anchored drawing n by its wrapping (ECMA-376 Part 1,
// 20.4.2.3 anchor, 20.4.2.10 to 20.4.2.17 wrap*): square, tight and
// through wrapping float it on the side its horizontal position says;
// top-and-bottom makes it a block of its own line; none and behindDoc
// position it absolutely from its paragraph, in front of or behind the
// text. Vertical offsets from the page or the margin are taken from the
// paragraph, an approximation the warnings name.
func (d *document) anchor(k *ooxml.Element, n *layout.Node, sink *inlineSink) {
	st := n.Style
	dist := func(name string) float64 {
		v, _ := ooxml.EMU(attrOr(k, name))
		return max(0, v)
	}
	posH, alignH, relH := position(k.Child(ooxml.NSWP, "positionH"))
	posV, _, relV := position(k.Child(ooxml.NSWP, "positionV"))
	behind := hasAttr(k, "behindDoc") && ooxml.OnOff(attrOr(k, "behindDoc"), true)
	wrap := ""
	for _, c := range k.Kids() {
		if c.Space == ooxml.NSWP && strings.HasPrefix(c.Local, "wrap") {
			wrap = c.Local
			break
		}
	}
	switch wrap {
	case "wrapSquare", "wrapTight", "wrapThrough":
		st.Float = css.FloatLeft
		if alignH == "right" || alignH == "outside" {
			st.Float = css.FloatRight
		}
		st.Margin = [4]css.Dimension{css.Pts(dist("distT")), css.Pts(dist("distR")), css.Pts(dist("distB")), css.Pts(dist("distL"))}
		if st.Float == css.FloatLeft && alignH == "" && posH > 0 && relH != "page" {
			st.Margin[css.Left] = css.Pts(posH)
		}
		if st.Display == css.Inline || st.Display == css.InlineBlock {
			st.Display = css.Block
		}
	case "wrapTopAndBottom":
		st.Display = css.Block
		st.Margin = [4]css.Dimension{css.Pts(dist("distT")), css.Pts(0), css.Pts(dist("distB")), css.Pts(0)}
		switch alignH {
		case "center":
			st.Margin[css.Left], st.Margin[css.Right] = css.Dimension{Kind: css.Auto}, css.Dimension{Kind: css.Auto}
		case "right", "outside":
			st.Margin[css.Left] = css.Dimension{Kind: css.Auto}
		default:
			if posH > 0 {
				st.Margin[css.Left] = css.Pts(posH)
			}
		}
	default:
		st.Position = css.Absolute
		if st.Display == css.Inline {
			st.Display = css.Block
		}
		st.Left, st.Top = css.Pts(posH), css.Pts(posV)
		fromPage := relV == "page" || relV == "margin" || relV == "topMargin" || relV == "bottomMargin"
		if fromPage && !anchorToPage(st) {
			d.warn("approximated", "a drawing positioned from the page is positioned from its paragraph")
			st.Top = css.Pts(0)
		}
		switch alignH {
		case "center":
			st.Left = css.Dimension{Kind: css.Percent, Value: 0.5}
		case "right", "outside":
			st.Left, st.Right = css.Dimension{Kind: css.Auto}, css.Pts(0)
		}
		if behind {
			st.ZIndex = css.ZIndex{Level: -1}
		}
		sink.positioned = true
	}
}

// position reads a wp:positionH or wp:positionV (20.4.2.10, 20.4.2.11):
// its offset in points, its alignment and what it is relative to. An
// element with neither child, which the schema forbids but documents
// carry, positions at offset zero.
func position(e *ooxml.Element) (float64, string, string) {
	if e == nil {
		return 0, "", ""
	}
	rel := attrOr(e, "relativeFrom")
	if a := e.Child(ooxml.NSWP, "align"); a != nil {
		return 0, strings.TrimSpace(a.Text), rel
	}
	var off float64
	if o := e.Child(ooxml.NSWP, "posOffset"); o != nil {
		off, _ = ooxml.EMU(strings.TrimSpace(o.Text))
	}
	return off, "", rel
}

// group builds a wpg:wgp (MS-ODRAWXML, 2.3.4.3): an inline block of w by h
// points holding its shapes and pictures positioned by their transforms in
// the group's child coordinates.
func (d *document) group(s *story, g *ooxml.Element, st *css.Style, w, h float64, depth int) *layout.Node {
	gs := inlineStyle(st)
	gs.Display = css.InlineBlock
	gs.Width, gs.Height = css.Pts(w), css.Pts(h)
	gs.Position = css.Relative
	gs.LineHeight = css.Dimension{Kind: css.Normal}
	var offX, offY, extX, extY float64
	if x := g.Child(ooxml.NSWPG, "grpSpPr").Child(ooxml.NSA, "xfrm"); x != nil {
		offX, _ = ooxml.EMU(attrOr(x.Child(ooxml.NSA, "chOff"), "x"))
		offY, _ = ooxml.EMU(attrOr(x.Child(ooxml.NSA, "chOff"), "y"))
		extX, _ = ooxml.EMU(attrOr(x.Child(ooxml.NSA, "chExt"), "cx"))
		extY, _ = ooxml.EMU(attrOr(x.Child(ooxml.NSA, "chExt"), "cy"))
	}
	sx, sy := 1.0, 1.0
	if extX > 0 && w > 0 {
		sx = w / extX
	}
	if extY > 0 && h > 0 {
		sy = h / extY
	}
	n := &layout.Node{Kind: layout.Element, Style: gs}
	if depth > maxNesting {
		return n
	}
	for _, c := range g.Kids() {
		var xfrm *ooxml.Element
		switch {
		case c.Is(ooxml.NSWPS, "wsp"):
			xfrm = c.Child(ooxml.NSWPS, "spPr").Child(ooxml.NSA, "xfrm")
		case c.Is(ooxml.NSPic, "pic"):
			xfrm = c.Child(ooxml.NSPic, "spPr").Child(ooxml.NSA, "xfrm")
		case c.Is(ooxml.NSWPG, "grpSp"):
			xfrm = c.Child(ooxml.NSWPG, "grpSpPr").Child(ooxml.NSA, "xfrm")
		default:
			continue
		}
		x, _ := ooxml.EMU(attrOr(xfrm.Child(ooxml.NSA, "off"), "x"))
		y, _ := ooxml.EMU(attrOr(xfrm.Child(ooxml.NSA, "off"), "y"))
		cw, _ := ooxml.EMU(attrOr(xfrm.Child(ooxml.NSA, "ext"), "cx"))
		ch, _ := ooxml.EMU(attrOr(xfrm.Child(ooxml.NSA, "ext"), "cy"))
		cw, ch = max(cw*sx, 0), max(ch*sy, 0)
		var kid *layout.Node
		switch {
		case c.Is(ooxml.NSWPS, "wsp"):
			kid = d.shape(s, c, st, cw, ch, depth+1)
		case c.Is(ooxml.NSPic, "pic"):
			kid = d.picture(s, ooxml.ParsePicture(c), st, cw, ch, "")
		default:
			kid = d.group(s, c, st, cw, ch, depth+1)
		}
		if kid == nil {
			continue
		}
		kid.Style.Position = css.Absolute
		kid.Style.Left, kid.Style.Top = css.Pts((x-offX)*sx), css.Pts((y-offY)*sy)
		if kid.Style.Display == css.Inline {
			kid.Style.Display = css.Block
		}
		n.Children = append(n.Children, kid)
	}
	return n
}

// vml builds the VML fallback of a w:pict or w:object (ECMA-376 Part 4,
// 14.1): a text box's content, else the picture of its v:imagedata, sized
// by the shape's style.
func (d *document) vml(s *story, c *ooxml.Element, st *css.Style, sink *inlineSink, depth int) {
	shape := c.Find(ooxml.NSV, "shape")
	if shape == nil {
		shape = c.Find(ooxml.NSV, "rect")
	}
	if shape == nil {
		if c.Local == "object" {
			d.warn("unsupported-content", "an embedded object is left out")
		}
		return
	}
	w, h := vmlSize(attrOr(shape, "style"))
	if tb := shape.Child(ooxml.NSV, "textbox"); tb != nil {
		if content := tb.Child(ooxml.NSW, "txbxContent"); content != nil {
			sink.add(d.textBox(s, content, w, h, ooxml.ShapeStyle{}, nil, depth))
			return
		}
	}
	if img := shape.Child(ooxml.NSV, "imagedata"); img != nil {
		id, _ := img.AttrNS(ooxml.NSR, "id")
		if n := d.picture(s, ooxml.PictureRef{Embed: id}, st, w, h, attrOr(img, "title")); n != nil {
			sink.add(n)
		}
		return
	}
	if c.Local == "object" {
		d.warn("unsupported-content", "an embedded object without a picture is left out")
	}
}

// vmlSize reads the width and height of a VML shape's CSS-like style
// (ECMA-376 Part 4, 14.1.2.19), 0 when absent.
func vmlSize(style string) (float64, float64) {
	var w, h float64
	for _, decl := range strings.Split(style, ";") {
		name, val, ok := strings.Cut(decl, ":")
		if !ok {
			continue
		}
		v, ok := ooxml.Measure(strings.TrimSpace(val), 0.75)
		if !ok {
			continue
		}
		switch strings.TrimSpace(name) {
		case "width":
			w = max(0, v)
		case "height":
			h = max(0, v)
		}
	}
	return w, h
}

// frame lays a paragraph with a w:framePr (17.3.1.11) out as a float: a
// drop cap's letter floats at the start of the next paragraph's lines, a
// text frame floats on the side its xAlign says, as wide as its w. Frames
// positioned at absolute page coordinates float too, an approximation.
func (d *document) frame(n *layout.Node, pp paraProps) {
	f := pp.framePr
	if f == nil {
		return
	}
	st := n.Style
	switch attrOr(f, "wrap") {
	case "none", "notBeside":
		if attrOr(f, "dropCap") == "" {
			return
		}
	}
	st.Float = css.FloatLeft
	switch attrOr(f, "xAlign") {
	case "right", "outside":
		st.Float = css.FloatRight
	case "center":
		st.Float = css.FloatNone
		st.Margin[css.Left], st.Margin[css.Right] = css.Dimension{Kind: css.Auto}, css.Dimension{Kind: css.Auto}
	}
	if v, ok := ooxml.Twips(attrOr(f, "w")); ok && v > 0 {
		st.Width = css.Pts(v)
	}
	if v, ok := ooxml.Twips(attrOr(f, "hSpace")); ok && v > 0 {
		st.Margin[css.Right] = css.Pts(v)
	}
	if attrOr(f, "dropCap") == "" && (hasAttr(f, "x") || hasAttr(f, "y")) {
		d.warn("approximated", "a text frame at a page position floats beside its anchor")
	}
}
