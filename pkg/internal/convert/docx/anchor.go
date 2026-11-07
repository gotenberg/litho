package docx

import (
	"strings"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
)

// placement is where an anchored drawing goes (ECMA-376 Part 1, 20.4.2.3
// anchor; for VML, Part 4, 14.1.2.19 style and 14.3.4 w10:wrap): its
// wrapping, its offsets or alignments and what they are relative to, in
// DrawingML's names (20.4.3.4 ST_RelFromH, 20.4.3.5 ST_RelFromV).
type placement struct {
	// wrap is wrapSquare, wrapTight, wrapThrough, wrapTopAndBottom or
	// wrapNone.
	wrap           string
	behind         bool
	alignH, alignV string
	posH, posV     float64
	relH, relV     string
	// dist is the distance kept from the text, top, right, bottom, left.
	dist [4]float64
	// w and h are the drawing's size in points.
	w, h float64
	// z orders the drawings in front of the text (20.4.2.3
	// relativeHeight), 0 for none.
	z int
}

// maxZ bounds the stack level read from a drawing. Anti-DoS guard, not
// spec-mandated: relativeHeight is an unsigned int, Word writes values
// near 2^28.
const maxZ = 1 << 30

// pendingAnchor is a drawing whose position needs its section's page,
// which the section's properties, at its end, give.
type pendingAnchor struct {
	n  *layout.Node
	st *css.Style
	p  placement
	// page anchors the box to the page box; else only its horizontal
	// offset is measured from the page, the box positioned from its
	// paragraph or floated.
	page bool
	// float sets the offset as the box's left margin.
	float bool
	// kids are the shapes of a group anchored to the page, laid out
	// beside it at their offsets from it.
	kids []*layout.Node
}

// anchor places the anchored drawing n by its wp:anchor k (20.4.2.3).
func (d *document) anchor(k *ooxml.Element, n *layout.Node, w, h float64, sink *inlineSink) {
	dist := func(name string) float64 {
		v, _ := ooxml.EMU(attrOr(k, name))
		return max(0, v)
	}
	p := placement{w: w, h: h}
	p.posH, p.alignH, p.relH = position(k.Child(ooxml.NSWP, "positionH"))
	p.posV, p.alignV, p.relV = position(k.Child(ooxml.NSWP, "positionV"))
	p.behind = hasAttr(k, "behindDoc") && ooxml.OnOff(attrOr(k, "behindDoc"), true)
	if v, ok := ooxml.Int(attrOr(k, "relativeHeight")); ok {
		p.z = max(0, min(v, maxZ))
	}
	p.dist = [4]float64{dist("distT"), dist("distR"), dist("distB"), dist("distL")}
	p.wrap = "wrapNone"
	for _, c := range k.Kids() {
		if c.Space == ooxml.NSWP && strings.HasPrefix(c.Local, "wrap") {
			p.wrap = c.Local
			break
		}
	}
	d.place(p, n, sink)
}

// fromPageH reports a horizontal position relative to the page or one of
// its margins (20.4.3.4).
func fromPageH(rel string) bool {
	switch rel {
	case "page", "margin", "leftMargin", "rightMargin", "insideMargin", "outsideMargin":
		return true
	}
	return false
}

// fromPageV reports a vertical position relative to the page or one of
// its margins (20.4.3.5).
func fromPageV(rel string) bool {
	switch rel {
	case "page", "margin", "topMargin", "bottomMargin", "insideMargin", "outsideMargin":
		return true
	}
	return false
}

// place lays the anchored drawing n out by its placement p: square, tight
// and through wrapping float it on the side its horizontal position says;
// top-and-bottom makes it a block of its own line; no wrapping positions
// it absolutely, in front of or behind the text. A drawing positioned
// vertically from the page or its margins in the body is anchored to the
// page box of its anchor's page (css.Style.PageAnchor), its offsets
// measured from its section's page; any other is positioned from its
// paragraph, a horizontal offset from the page measured from the
// paragraph's start at the margin.
func (d *document) place(p placement, n *layout.Node, sink *inlineSink) {
	st := n.Style
	pageH := fromPageH(p.relH) && d.pageAnchors()
	if !p.behind && p.z > 0 {
		// The drawings in front of the text stack in the order of their
		// relative heights, floats among them, as Word and Writer layer
		// them.
		st.ZIndex = css.ZIndex{Level: p.z}
		if st.Position == css.Static {
			st.Position = css.Relative
		}
	}
	switch p.wrap {
	case "wrapSquare", "wrapTight", "wrapThrough":
		st.Float = css.FloatLeft
		if p.alignH == "right" || p.alignH == "outside" {
			st.Float = css.FloatRight
		}
		st.Margin = [4]css.Dimension{css.Pts(p.dist[0]), css.Pts(p.dist[1]), css.Pts(p.dist[2]), css.Pts(p.dist[3])}
		// LibreOffice keeps a drawing anchored in the body inside the
		// page its paragraph is on.
		st.PageClamp = d.pageAnchors()
		if !fromPageV(p.relV) && p.alignV == "" && (p.posV > 0 || st.PageClamp) {
			// A float lower than its paragraph's top starts that much
			// lower, an approximation of its vertical offset; one higher
			// starts that much higher, inside its page.
			st.Margin[css.Top] = css.Pts(p.posV)
		}
		if st.Float == css.FloatLeft && p.alignH == "" {
			switch {
			case pageH:
				d.pending = append(d.pending, pendingAnchor{n: n, st: st, p: p, float: true})
			case p.posH > 0 && !fromPageH(p.relH):
				st.Margin[css.Left] = css.Pts(p.posH)
			}
		}
		if st.Display == css.Inline || st.Display == css.InlineBlock {
			st.Display = css.Block
		}
	case "wrapTopAndBottom":
		st.Display = css.Block
		st.Margin = [4]css.Dimension{css.Pts(p.dist[0]), css.Pts(0), css.Pts(p.dist[2]), css.Pts(0)}
		switch p.alignH {
		case "center":
			st.Margin[css.Left], st.Margin[css.Right] = css.Dimension{Kind: css.Auto}, css.Dimension{Kind: css.Auto}
		case "right", "outside":
			st.Margin[css.Left] = css.Dimension{Kind: css.Auto}
		default:
			switch {
			case pageH:
				d.pending = append(d.pending, pendingAnchor{n: n, st: st, p: p, float: true})
			case p.posH > 0:
				st.Margin[css.Left] = css.Pts(p.posH)
			}
		}
	default:
		// An absolutely positioned box is blockified (CSS 2.1, 9.7).
		st.Position = css.Absolute
		if st.Display == css.Inline || st.Display == css.InlineBlock {
			st.Display = css.Block
		}
		if p.behind {
			st.ZIndex = css.ZIndex{Level: -1}
		}
		if fromPageV(p.relV) && d.pageAnchors() {
			st.PageAnchor = true
			kids := detachKids(n, 0, 0, 0)
			for _, k := range kids {
				k.Style.PageAnchor = true
				sink.add(k)
			}
			d.pending = append(d.pending, pendingAnchor{n: n, st: st, p: p, page: true, kids: kids})
			return
		}
		st.Left, st.Top = css.Pts(p.posH), css.Pts(p.posV)
		// LibreOffice keeps a drawing positioned from its paragraph
		// inside the page its paragraph is on.
		st.PageClamp = d.pageAnchors()
		if fromPageV(p.relV) {
			d.warn("approximated", "a drawing positioned from the page in a header, footer, note, text box or table is positioned from its paragraph")
			st.Top = css.Pts(0)
		}
		switch {
		case pageH:
			d.pending = append(d.pending, pendingAnchor{n: n, st: st, p: p})
		case p.alignH == "center":
			st.Left = css.Dimension{Kind: css.Percent, Value: 0.5}
		case p.alignH == "right" || p.alignH == "outside":
			st.Left, st.Right = css.Dimension{Kind: css.Auto}, css.Pts(0)
		}
		sink.positioned = true
	}
}

// pageAnchors reports whether a drawing being built can be positioned
// from its page: in the body, outside tables and text boxes, whose
// drawings Word lays out in their cell or box.
func (d *document) pageAnchors() bool { return d.inBody && d.nested == 0 }

// resolve sets the offsets of the pending drawings of section s from its
// page: the page box's left and top for a page-anchored box, the offset
// from the margin for one positioned from its paragraph.
func (a pendingAnchor) resolve(s *section) {
	p, st := a.p, a.st
	left := s.margin[css.Left] + s.gutter
	x0, aw := hArea(s, p.relH)
	x := x0 + p.posH
	switch p.alignH {
	case "left", "inside":
		x = x0
	case "center":
		x = x0 + (aw-p.w)/2
	case "right", "outside":
		x = x0 + aw - p.w
	}
	switch {
	case a.float:
		st.Margin[css.Left] = css.Pts(max(0, x-left))
		return
	case !a.page:
		st.Left = css.Pts(x - left)
		return
	}
	if !fromPageH(p.relH) {
		x = left + p.posH
		switch p.alignH {
		case "left", "inside":
			x = left
		case "center":
			x = left + (s.contentWidth()-p.w)/2
		case "right", "outside":
			x = left + s.contentWidth() - p.w
		}
	}
	y0, ah := vArea(s, p.relV)
	y := y0 + p.posV
	switch p.alignV {
	case "top", "inside":
		y = y0
	case "center":
		y = y0 + (ah-p.h)/2
	case "bottom", "outside":
		y = y0 + ah - p.h
	}
	st.Left, st.Top = css.Pts(x), css.Pts(y)
	st.Right, st.Bottom = css.Dimension{Kind: css.Auto}, css.Dimension{Kind: css.Auto}
	for _, k := range a.kids {
		k.Style.Left, k.Style.Top = css.Pts(x+k.Style.Left.Value), css.Pts(y+k.Style.Top.Value)
	}
}

// detachKids takes the shapes of the group n, and of the groups in it,
// out of it and returns them in paint order, their offsets made relative
// to n. A group anchored to the page lays its shapes out as boxes anchored
// to the page beside it, each on the group's page whole.
func detachKids(n *layout.Node, dx, dy float64, depth int) []*layout.Node {
	if depth > maxNesting {
		return nil
	}
	var out []*layout.Node
	kept := n.Children[:0]
	for _, k := range n.Children {
		st := k.Style
		if st == nil || st.Position != css.Absolute || st.Left.Kind != css.Points || st.Top.Kind != css.Points {
			kept = append(kept, k)
			continue
		}
		st.Left, st.Top = css.Pts(dx+st.Left.Value), css.Pts(dy+st.Top.Value)
		out = append(out, k)
		out = append(out, detachKids(k, st.Left.Value, st.Top.Value, depth+1)...)
	}
	n.Children = kept
	return out
}

// contentWidth returns the width of the section's text area.
func (s *section) contentWidth() float64 {
	return max(minPageArea, s.width-s.margin[css.Left]-s.margin[css.Right]-s.gutter)
}

// hArea returns the left edge and the width of the area a horizontal
// position is relative to (20.4.3.4) on a page of section s: the page,
// a margin, else the text area.
func hArea(s *section, rel string) (float64, float64) {
	left := s.margin[css.Left] + s.gutter
	switch rel {
	case "page":
		return 0, s.width
	case "leftMargin", "insideMargin":
		return 0, left
	case "rightMargin", "outsideMargin":
		return s.width - s.margin[css.Right], s.margin[css.Right]
	}
	return left, s.contentWidth()
}

// vArea returns the top edge and the height of the area a vertical
// position is relative to (20.4.3.5) on a page of section s.
func vArea(s *section, rel string) (float64, float64) {
	top, bottom := abs(s.margin[css.Top]), abs(s.margin[css.Bottom])
	switch rel {
	case "page":
		return 0, s.height
	case "topMargin", "insideMargin":
		return 0, top
	case "bottomMargin", "outsideMargin":
		return s.height - bottom, bottom
	}
	return top, max(minPageArea, s.height-top-bottom)
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
		if kid.Style.Display == css.Inline || kid.Style.Display == css.InlineBlock {
			kid.Style.Display = css.Block
		}
		n.Children = append(n.Children, kid)
	}
	return n
}

// frame lays a paragraph with a w:framePr (17.3.1.11) out as a float: a
// drop cap's letter floats at the start of the next paragraph's lines, a
// text frame floats on the side its xAlign says, as wide as its w. Frames
// positioned at absolute page coordinates float too, an approximation.
func (d *document) frame(n *layout.Node, pp paraProps) {
	f := pp.framePr
	if f == nil || len(f.Attrs) == 0 {
		// A frame without properties is a plain paragraph, as LibreOffice
		// imports it.
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
