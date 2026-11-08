package pptx

import "github.com/gotenberg/litho/pkg/internal/ooxml"

// effectList reads the effect list of the shape properties pr (ECMA-376
// Part 1, 20.1.8.26 effectLst): the shadows, glows, reflections and soft
// edges it holds print nothing, with a warning, until a reader fills
// [slides.Shape.Shadow], [slides.Shape.Glow] and [slides.Shape.SoftEdge].
func (r *reader) effectList(pr *ooxml.Element) {
	if e := pr.Child(ooxml.NSA, "effectLst"); e != nil && len(e.Children) > 0 {
		r.warn("unsupported-content", "shape effects (shadows, glows, reflections, soft edges) are not drawn")
	}
}
