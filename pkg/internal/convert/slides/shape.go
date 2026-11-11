package slides

import (
	"fmt"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/chart"
	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/shape"
)

// shape returns the fragments of s in its parent's coordinates, m mapping
// those to the slide's, depth the groups it sits in, recording its links
// and hanging its structure from parent.
func (b *builder) shape(s *Shape, m compose.Matrix, depth int, parent *compose.StructElem) []compose.Fragment {
	if s == nil || !finite(s.Box) {
		return nil
	}
	if s.Decorative && parent != nil {
		return b.decorative(s, m, depth, parent)
	}
	b.shapes++
	if b.shapes > maxSlideShapes {
		b.warn(CodeTooManyShapes, fmt.Sprintf("a slide has more than %d shapes; the rest are left out", maxSlideShapes))
		return nil
	}
	if !b.mem.Take(1, shapeCost, "shapes") {
		// The refusal stops the conversion, which reports it.
		return nil
	}
	sm := skewedAboutCentre(s.Box, s.Rotation, s.SkewX, s.FlipH, s.FlipV)
	var out []compose.Fragment
	box := s.Box
	switch s.Kind {
	case KindGeometry:
		return b.geometry(s, m, parent)
	case KindPicture:
		if s.Picture == nil || (s.Picture.Source == nil && s.Picture.Vector == nil) {
			b.warn(CodePicture, "picture "+quote(s.Name)+" has nothing to draw")
			return nil
		}
		fig := &compose.StructElem{Role: "Figure", Alt: s.Alt}
		frags := b.picture(s.Picture, box)
		for _, f := range frags {
			if img, ok := f.(*compose.Image); ok {
				img.Parent = fig
			}
		}
		if len(frags) > 0 {
			parent.Kids = append(parent.Kids, fig)
		}
		// A picture's own fill paints under it and its line over it, as
		// Impress draws a picture whose shape properties fill and stroke
		// it (oox/tdf163803_image_with_fill.pptx).
		ops := rectOps(box)
		frags = append(b.fill(&s.Fill, box, ops, shape.FillNorm), frags...)
		frags = append(frags, b.stroke(&s.Line, ops)...)
		out = group(sm, b.contentEffects(s, frags, m))
	case KindChart:
		if s.Chart == nil {
			return nil
		}
		frags := chart.Render(b.ctx, b.fonts, s.Chart, box)
		if len(frags) > 0 && parent != nil {
			fig := &compose.StructElem{Role: "Figure", Alt: chartAlt(s)}
			adopt(frags, fig)
			parent.Kids = append(parent.Kids, fig)
		}
		out = group(sm, frags)
	case KindTable:
		out = group(sm, b.contentEffects(s, b.table(s, parent), m))
	case KindGroup:
		if depth >= maxGroupDepth {
			b.warn(CodeGroupDepth, fmt.Sprintf("groups nest deeper than %d levels; the deeper ones are left out", maxGroupDepth))
			return nil
		}
		child := s.Child
		if !(child.Width > 0) || !(child.Height > 0) || !finite(child) {
			child = box
		}
		sx, sy := box.Width/child.Width, box.Height/child.Height
		inner := then(sm, m)
		var frags []compose.Fragment
		for _, k := range s.Kids {
			if k == nil {
				continue
			}
			// The kid's box mapped from the child space onto the group's
			// box; the kid keeps its own turn and flips.
			kk := *k
			kk.Box = compose.Rect{
				X: box.X + (k.Box.X-child.X)*sx, Y: box.Y + (k.Box.Y-child.Y)*sy,
				Width: k.Box.Width * sx, Height: k.Box.Height * sy,
			}
			frags = append(frags, b.shape(&kk, inner, depth+1, parent)...)
		}
		out = group(sm, frags)
	}
	b.link(s, then(sm, m), box, parent)
	return out
}

// decorative returns the fragments of s, a shape its source marks as
// carrying no meaning, as artifacts (ISO 32000-1, 14.8.2.2.2; ISO 14289-1,
// 7.1): drawn as any shape, with no structure element of its own. The Link
// elements of its links still hang from parent, since every link
// annotation needs one (ISO 14289-1, 7.18.1).
func (b *builder) decorative(s *Shape, m compose.Matrix, depth int, parent *compose.StructElem) []compose.Fragment {
	kept := *s
	kept.Decorative = false
	scratch := &compose.StructElem{}
	out := b.shape(&kept, m, depth, scratch)
	adopt(out, nil)
	parent.Kids = append(parent.Kids, links(scratch, nil)...)
	return out
}

// links appends to out the Link elements under e, depth first.
func links(e *compose.StructElem, out []*compose.StructElem) []*compose.StructElem {
	for _, k := range e.Kids {
		if k == nil {
			continue
		}
		if k.Role == "Link" {
			k.Kids = nil
			out = append(out, k)
			continue
		}
		out = links(k, out)
	}
	return out
}

// adopt hangs every fragment of frags, inside groups too, from parent; a
// nil parent makes them artifacts.
func adopt(frags []compose.Fragment, parent *compose.StructElem) {
	for _, fr := range frags {
		switch v := fr.(type) {
		case *compose.Text:
			v.Parent = parent
		case *compose.Image:
			v.Parent = parent
		case *compose.Path:
			v.Parent = parent
		case *compose.Group:
			adopt(v.Content, parent)
		}
	}
}

// chartAlt returns what a chart's Figure says of it: the shape's
// description, else the chart's title (ISO 14289-1, 7.3), "" when the
// source gives neither.
func chartAlt(s *Shape) string {
	if s.Alt != "" || s.Chart == nil || s.Chart.Title == nil {
		return s.Alt
	}
	return strings.Join(strings.Fields(s.Chart.Title.Text), " ")
}

// link records the link of s over box, mapped by m to the slide: its
// hyperlink, else its jump to the page of the slide it names when that
// slide prints. The annotation hangs from a Link element of its own under
// parent, as ISO 14289-1, 7.18.1 and ISO 32000-1, 14.8.4.4.2 ask of every
// link annotation.
func (b *builder) link(s *Shape, m compose.Matrix, box compose.Rect, parent *compose.StructElem) {
	if box.Width <= 0 || box.Height <= 0 {
		return
	}
	l := compose.Link{Rect: transformRect(m, box)}
	switch {
	case s.Href != "":
		l.URI = s.Href
	case s.Jump > 0 && s.Jump <= len(b.doc.Slides):
		pg, ok := b.pages[b.doc.Slides[s.Jump-1]]
		if !ok {
			return
		}
		l.Dest = &compose.Dest{Page: pg}
	default:
		return
	}
	if parent != nil {
		l.Parent = &compose.StructElem{Role: "Link"}
		parent.Kids = append(parent.Kids, l.Parent)
	}
	b.links = append(b.links, l)
}

// geometry returns the fragments of the geometry shape s: its paths
// filled and stroked, then its text, turned about its box's centre.
func (b *builder) geometry(s *Shape, m compose.Matrix, parent *compose.StructElem) []compose.Fragment {
	box := s.Box
	var txt *textResult
	if s.Text != nil && len(s.Text.Paragraphs) > 0 {
		txt = b.shapeText(s.Text, s.Geometry, box)
		box = txt.box
	}
	if !shape.Known(s.Geometry) {
		b.warn(CodeGeometry, "the geometry "+quote(s.Geometry.Preset)+" is drawn as its bounding rectangle")
	}
	sm := skewedAboutCentre(box, s.Rotation, s.SkewX, s.FlipH, s.FlipV)
	paths := shape.Paths(s.Geometry, box)
	frags, paths := b.effects(s, paths, box, m, txt)
	b.hatchTurn = s.Rotation
	for _, p := range paths {
		if p.Fill {
			frags = append(frags, b.fill(&s.Fill, box, p.Ops, p.Mode)...)
		}
		if p.Stroke {
			frags = append(frags, b.stroke(&s.Line, p.Ops)...)
		}
	}
	b.hatchTurn = 0
	out := group(sm, frags)
	if txt != nil {
		// Impress turns a shape's text with the shape but never mirrors
		// it: a vertical flip turns it half a turn.
		rot := s.Rotation
		if s.FlipV {
			rot += 180
		}
		tm := skewedAboutCentre(box, rot, s.SkewX, false, false)
		out = append(out, group(tm, txt.frags)...)
		for _, l := range txt.links {
			l.Rect = transformRect(then(tm, m), l.Rect)
			b.links = append(b.links, l)
		}
		parent.Kids = append(parent.Kids, txt.structure...)
	}
	b.link(s, then(sm, m), box, parent)
	return out
}
