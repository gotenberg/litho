package odp

import "github.com/gotenberg/litho/pkg/internal/odf"

// effects reads the graphic effects of the graphic properties p: a shadow
// (ODF 1.3, Part 3, 20.158 draw:shadow), a glow and a soft edge
// (LibreOffice's loext:glow-radius and loext:softedge-radius). They print
// nothing, with a warning, until the reader fills [slides.Shape.Shadow],
// [slides.Shape.Glow] and [slides.Shape.SoftEdge].
func (r *reader) effects(p odf.Props) {
	if p.Get("draw:shadow") == "visible" {
		r.warn("shadow", "shape shadows are not drawn")
	}
	if v, ok := odf.Length(p.Get("loext:glow-radius")); ok && v > 0 {
		r.warn("glow", "glow effects are not drawn")
	}
	if v, ok := odf.Length(p.Get("loext:softedge-radius")); ok && v > 0 {
		r.warn("soft-edge", "soft edges are not drawn")
	}
}
