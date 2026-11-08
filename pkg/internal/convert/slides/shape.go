package slides

import (
	"fmt"

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
	b.shapes++
	if b.shapes > maxSlideShapes {
		b.warn(CodeTooManyShapes, fmt.Sprintf("a slide has more than %d shapes; the rest are left out", maxSlideShapes))
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
		fig := &compose.StructElem{Role: "Figure", Alt: s.Name}
		frags := b.picture(s.Picture, box)
		for _, f := range frags {
			if img, ok := f.(*compose.Image); ok {
				img.Parent = fig
			}
		}
		if len(frags) > 0 {
			parent.Kids = append(parent.Kids, fig)
		}
		out = group(sm, b.contentEffects(s, frags, m))
	case KindChart:
		if s.Chart == nil {
			return nil
		}
		out = group(sm, chart.Render(b.ctx, b.fonts, s.Chart, box))
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
	b.link(s, then(sm, m), box)
	return out
}

// link records the link of s over box, mapped by m to the slide: its
// hyperlink, else its jump to the page of the slide it names when that
// slide prints.
func (b *builder) link(s *Shape, m compose.Matrix, box compose.Rect) {
	if box.Width <= 0 || box.Height <= 0 {
		return
	}
	switch {
	case s.Href != "":
		b.links = append(b.links, compose.Link{Rect: transformRect(m, box), URI: s.Href})
	case s.Jump > 0 && s.Jump <= len(b.doc.Slides):
		if pg, ok := b.pages[b.doc.Slides[s.Jump-1]]; ok {
			b.links = append(b.links, compose.Link{Rect: transformRect(m, box), Dest: &compose.Dest{Page: pg}})
		}
	}
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
	for _, p := range paths {
		if p.Fill {
			frags = append(frags, b.fill(&s.Fill, box, p.Ops, p.Mode)...)
		}
		if p.Stroke {
			frags = append(frags, b.stroke(&s.Line, p.Ops)...)
		}
	}
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
	b.link(s, then(sm, m), box)
	return out
}
