package mutations

import (
	"context"
	"fmt"
	"maps"
	"math"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/heuristics"
	"github.com/gotenberg/litho/pkg/internal/render/annot"
)

// maxLinkAreaPieces caps the rectangles a Link's activation area is cut into
// by [clipLinkArea]. Anti-DoS guard, not spec-mandated: each region cuts a
// rectangle into at most four, so a caller passing many regions over one link
// could multiply them past any size a /QuadPoints array is read at. A link a
// handful of regions cut into more than 64 pieces is refused instead.
const maxLinkAreaPieces = 64

// quadPointsPerQuad is the number of coordinates one quadrilateral of
// /QuadPoints takes: four points of two numbers (ISO 32000-1, 12.5.6.5,
// Table 173).
const quadPointsPerQuad = 8

// annotPaintsNothing reports whether a viewer draws nothing for the
// annotation dict, in any mode: it has no appearance dictionary at all, and
// its type is one whose look ISO 32000-1 builds from the dictionary only where
// [annot.SynthesizedBox] builds one, which it does not for this one.
//
// Only two types qualify, the others being drawn by viewers from entries
// litho does not read (a FreeText's /DA, a Text annotation's icon): a Link,
// whose look is its border alone (12.5.6.5, Table 173), so one of width 0
// draws nothing, and a pop-up that is not open (12.5.6.14, Table 183), which
// is drawn only as the window a viewer opens over its parent. An appearance
// dictionary rules an annotation out even when no state of it is drawn: its
// streams are content in the file, which keeping the annotation would keep.
func annotPaintsNothing(ctx context.Context, doc *document.Document, dict ast.PdfDict, rotate int) bool {
	if ap, err := document.ResolveObject(ctx, doc, dict["AP"]); err != nil || ap != nil {
		return false
	}
	subtype, err := document.ResolveName(ctx, doc, dict["Subtype"])
	if err != nil {
		return false
	}
	switch subtype {
	case "Link":
		_, _, drawn := annot.SynthesizedBox(ctx, doc, dict, rotate)
		return !drawn
	case "Popup":
		open, err := document.ResolveObject(ctx, doc, dict["Open"])
		if err != nil {
			return false
		}
		b, isBool := open.(ast.PdfBool)
		return open == nil || isBool && !bool(b)
	}
	return false
}

// areaRect is an axis-aligned rectangle, x0 <= x1 and y0 <= y1.
type areaRect struct{ x0, y0, x1, y1 float64 }

// clipLinkArea returns a copy of the Link dict whose activation area leaves
// out the zones, and false when nothing of it is left outside them.
//
// A link is activated over /QuadPoints when it states them and over /Rect
// otherwise (ISO 32000-1, 12.5.6.5, Table 173). Each rectangle of that area
// loses what the zones cover, and the pieces left make the new area: a single
// piece becomes /Rect and the link states no /QuadPoints, and several become
// /QuadPoints, one quadrilateral each, with /Rect their bounding box, which
// Table 173 requires to hold every quadrilateral. A reader that ignores
// /QuadPoints activates the link over that box, the regions' part of it
// included: what lies under it there is the redacted page, so the link marks
// nothing the page still shows.
//
// A quadrilateral that is not an axis-aligned rectangle cannot be cut by the
// zones exactly, and [ErrAnnotationStraddlesRegion] reports it; so does an
// area cut into more than [maxLinkAreaPieces] pieces. A /QuadPoints that does
// not read as whole quadrilaterals within /Rect is ignored, as Table 173 says
// a reader ignores it, and /Rect is cut instead.
func clipLinkArea(ctx context.Context, doc *document.Document, dict ast.PdfDict, rect heuristics.Zone, zones []heuristics.Zone) (ast.PdfDict, bool, error) {
	rx0, ry0, rx1, ry1 := heuristics.ZoneBounds(rect)
	area := []areaRect{{rx0, ry0, rx1, ry1}}
	quads, hasQuads, err := linkQuads(ctx, doc, dict, area[0])
	if err != nil {
		return nil, false, err
	}
	if hasQuads {
		area = quads
	}
	for _, z := range zones {
		zx0, zy0, zx1, zy1 := heuristics.ZoneBounds(z)
		cut := areaRect{zx0, zy0, zx1, zy1}
		var next []areaRect
		for _, r := range area {
			next = append(next, subtractRect(r, cut)...)
		}
		if len(next) > maxLinkAreaPieces {
			return nil, false, fmt.Errorf("%w: the regions cut a link into %d pieces", ErrAnnotationStraddlesRegion, len(next))
		}
		area = next
	}
	if len(area) == 0 {
		return nil, false, nil
	}

	out := maps.Clone(dict)
	delete(out, "QuadPoints")
	if len(area) == 1 && !hasQuads {
		out["Rect"] = areaArray(area[0])
		return out, true, nil
	}
	bounds := area[0]
	quadPoints := make(ast.PdfArray, 0, quadPointsPerQuad*len(area))
	for _, r := range area {
		bounds = areaRect{min(bounds.x0, r.x0), min(bounds.y0, r.y0), max(bounds.x1, r.x1), max(bounds.y1, r.y1)}
		// Counter-clockwise from the lower left, the order ISO 32000-1,
		// 12.5.6.10 gives text markup quadrilaterals, and the order the
		// producers of link quadrilaterals follow.
		for _, v := range []float64{r.x0, r.y0, r.x1, r.y0, r.x1, r.y1, r.x0, r.y1} {
			quadPoints = append(quadPoints, pdfNumber(v))
		}
	}
	out["Rect"] = areaArray(bounds)
	out["QuadPoints"] = quadPoints
	return out, true, nil
}

// linkQuads reads a Link's /QuadPoints as the rectangles they describe. It
// reports false when the link has none a reader uses: absent, not whole
// quadrilaterals of numbers, or reaching outside rect (ISO 32000-1, 12.5.6.5,
// Table 173: the entry is then ignored). A quadrilateral that is not an
// axis-aligned rectangle reports [ErrAnnotationStraddlesRegion].
func linkQuads(ctx context.Context, doc *document.Document, dict ast.PdfDict, rect areaRect) ([]areaRect, bool, error) {
	arr, err := document.ResolveArray(ctx, doc, dict["QuadPoints"])
	if err != nil || len(arr) == 0 || len(arr)%quadPointsPerQuad != 0 {
		return nil, false, nil //nolint:nilerr // An unreadable /QuadPoints is ignored, as Table 173 ignores a malformed one.
	}
	values := make([]float64, len(arr))
	for i, e := range arr {
		obj, err := document.ResolveObject(ctx, doc, e)
		if err != nil {
			return nil, false, nil //nolint:nilerr // As above: the entry is not read, so /Rect stands.
		}
		v, ok := document.Number(obj)
		if !ok {
			return nil, false, nil
		}
		values[i] = v
	}
	const eps = 1e-6
	quads := make([]areaRect, 0, len(values)/quadPointsPerQuad)
	for q := 0; q < len(values); q += quadPointsPerQuad {
		pts := values[q : q+quadPointsPerQuad]
		r := areaRect{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
		for i := 0; i < quadPointsPerQuad; i += 2 {
			r = areaRect{min(r.x0, pts[i]), min(r.y0, pts[i+1]), max(r.x1, pts[i]), max(r.y1, pts[i+1])}
		}
		if r.x0 < rect.x0-eps || r.y0 < rect.y0-eps || r.x1 > rect.x1+eps || r.y1 > rect.y1+eps {
			return nil, false, nil
		}
		for i := 0; i < quadPointsPerQuad; i += 2 {
			onX := math.Abs(pts[i]-r.x0) < eps || math.Abs(pts[i]-r.x1) < eps
			onY := math.Abs(pts[i+1]-r.y0) < eps || math.Abs(pts[i+1]-r.y1) < eps
			if !onX || !onY {
				return nil, false, fmt.Errorf("%w: a link quadrilateral is not an axis-aligned rectangle", ErrAnnotationStraddlesRegion)
			}
		}
		quads = append(quads, r)
	}
	return quads, true, nil
}

// subtractRect returns the parts of r that cut does not cover, as at most four
// rectangles: the full-height strips left and right of cut, and the parts
// above and below it between them. A piece of no area is dropped, so cutting
// an area already cut leaves it as it is.
func subtractRect(r, cut areaRect) []areaRect {
	if cut.x0 >= r.x1 || cut.x1 <= r.x0 || cut.y0 >= r.y1 || cut.y1 <= r.y0 {
		return []areaRect{r}
	}
	var out []areaRect
	add := func(p areaRect) {
		if p.x1 > p.x0 && p.y1 > p.y0 {
			out = append(out, p)
		}
	}
	add(areaRect{r.x0, r.y0, cut.x0, r.y1})
	add(areaRect{cut.x1, r.y0, r.x1, r.y1})
	midX0, midX1 := max(r.x0, cut.x0), min(r.x1, cut.x1)
	add(areaRect{midX0, r.y0, midX1, cut.y0})
	add(areaRect{midX0, cut.y1, midX1, r.y1})
	return out
}

// areaArray writes r as a /Rect array.
func areaArray(r areaRect) ast.PdfArray {
	return ast.PdfArray{pdfNumber(r.x0), pdfNumber(r.y0), pdfNumber(r.x1), pdfNumber(r.y1)}
}

// pdfNumber writes v as an integer when it is one, and as a real otherwise.
func pdfNumber(v float64) ast.PdfObject {
	if v == math.Trunc(v) && math.Abs(v) < math.MaxInt32 {
		return ast.PdfInt(int64(v))
	}
	return ast.PdfReal(v)
}
