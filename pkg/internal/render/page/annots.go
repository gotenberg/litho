package page

import (
	"fmt"
	"math"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/render/annot"
	"github.com/gotenberg/litho/pkg/internal/render/geom"
)

// annotations draws the page's annotation appearances over its content, in
// /Annots order (ISO 32000-1, 12.5.5), as [annot.Collect] selects them for
// mode. Widgets whose appearances the form marks stale are drawn from the
// stream the file holds and counted ([renderer.needAppearances]). Each runs as a form XObject from the initial
// graphics state, placed by Algorithm 8.1 and clipped to its /BBox, unless
// optional content hides the annotation (its /OC, 8.11.3.3) or its
// appearance stream (the stream's /OC).
func (r *renderer) annotations(g Geometry, mode annot.Mode) error {
	apps, skips, err := annot.Collect(r.ctx, r.doc, r.page, annot.Options{Mode: mode})
	if err != nil {
		return fmt.Errorf("collecting annotations: %w", err)
	}
	for _, sk := range skips {
		if sk.Reason == annot.ReasonNoAppearance {
			r.report.count(UnsupportedAnnotation)
		}
	}
	stale := r.needAppearances()
	for _, a := range apps {
		if stale && a.Subtype == "Widget" {
			r.report.count(StaleAppearance)
		}
		if !r.visible(a.OC) || !r.visible(a.FormOC) {
			continue
		}
		if err := r.annotation(a, g); err != nil {
			return err
		}
	}
	return nil
}

// needAppearances reads /NeedAppearances of the document's interactive form
// (ISO 32000-1, 12.7.2, Table 218): true asks the viewer to construct the
// appearance streams of every widget annotation, so the streams the file
// holds are stale. A viewer draws the ones it builds; litho builds none and
// draws the stored stream, as a viewer that cannot construct appearances
// does, rather than drop visible form content. poppler 26.09.0 builds new
// streams, so its rendering of such a widget can differ.
func (r *renderer) needAppearances() bool {
	catalog, err := document.ResolveCatalog(r.ctx, r.doc)
	if err != nil || catalog == nil {
		return false
	}
	form, err := document.ResolveDict(r.ctx, r.doc, catalog.Dict["AcroForm"])
	if err != nil || form == nil {
		return false
	}
	need, err := document.ResolveBool(r.ctx, r.doc, form["NeedAppearances"])
	return err == nil && bool(need)
}

// annotation draws one appearance.
func (r *renderer) annotation(a annot.Appearance, g Geometry) error {
	r.m = graphics.NewMachine([4]float64{})
	r.m.Doc = r.doc
	r.clips = []*clip{newClip(r.canvas.Width, r.canvas.Height)}
	r.chain = r.chain[:0]

	place := a.A
	if a.NoRotate {
		place = place.Mul(upright(a.Rect, g.Matrix))
	}
	r.m.Current().CTM = [6]float64(place)

	// 7.8.3: an appearance with no /Resources names the page's; one whose
	// /Resources does not read draws with none, as annot.Collect decides.
	resources := r.page.Resources
	if !a.PageResources {
		resources = a.Resources
		if resources == nil {
			resources = ast.PdfDict{}
		}
	}
	s := &scope{resources: resources, base: g.Matrix, floor: len(r.m.Stack)}
	return r.form(s, a.StreamRef, a.Stream)
}

// upright returns the default-space matrix that undoes the page's rotation
// about the upper-left corner of rect, for an annotation with /NoRotate set
// (ISO 32000-1, 12.5.3, Table 165): its upper-left corner stays where the
// page puts it, and its appearance keeps the page's unrotated orientation.
// The render draws at a magnification of 1, the page's own size at the
// resolution asked (8.3.2.3), so /NoZoom leaves the appearance as it is.
func upright(rect geom.Rect, device geom.Matrix) geom.Matrix {
	inv, ok := device.Invert()
	if !ok {
		return geom.Identity
	}
	pivot := geom.Point{X: rect.Min.X, Y: rect.Max.Y}
	at := device.Apply(pivot)
	scale := math.Sqrt(math.Abs(device.Det()))
	unturned := geom.Matrix{1, 0, 0, 1, -pivot.X, -pivot.Y}.Mul(geom.Matrix{scale, 0, 0, -scale, at.X, at.Y})
	return unturned.Mul(inv)
}
