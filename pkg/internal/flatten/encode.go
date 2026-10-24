package flatten

import (
	"context"
	"fmt"
	"image"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	pdfimage "github.com/gotenberg/litho/pkg/internal/graphics/image"
	"github.com/gotenberg/litho/pkg/internal/imagetiles"
)

// vectorBoxes returns the boxes, in default user space, where what the
// region's image holds has a sharp edge, which a JPEG would blur and ring:
// the box of a filled or stroked path, or the sides of a rectangle filled in
// a colour, and inside a form the same ([flattener.formEdges]). The image
// holds what the units it takes over paint, removed or clipped out of it,
// and what the units drawn before it paint inside it: they stay under the
// image, which covers them. Images, inline images, shadings and paths that
// only clip are photographic. Text is left out of the image and drawn above
// it. A region with no such box is photographic throughout.
func (f *flattener) vectorBoxes(ctx context.Context, a *Analysis, ops []content.Operation, r Region) []Rect {
	held := slices.Concat(r.Removed, r.Clipped)
	for i, unit := range a.Units {
		if unit.First < r.Insert && unit.Kind != KindText && unit.Box.Overlaps(r.Box) {
			held = append(held, i)
		}
	}
	var boxes []Rect
	for _, u := range held {
		unit := a.Units[u]
		switch unit.Kind {
		case KindImage, KindInlineImage, KindShading:
		case KindPath:
			p, _ := pathPaints(ops[unit.Last].Operator)
			switch {
			case !p.fill && !p.stroke:
				// A path ended by n paints nothing (ISO 32000-2, 8.5.3.1):
				// it only sets the clip, which leaves an edge only where
				// the images and shadings it clips end.
			case !p.stroke && f.flatRect(ctx, a, ops, unit):
				boxes = append(boxes, rectEdges(unit.Box, 0)...)
			default:
				boxes = append(boxes, unit.Box)
			}
		case KindForm:
			boxes = append(boxes, f.formEdges(ctx, a, ops, unit)...)
		case KindText:
			// Text objects never join a region's Removed or Clipped, nor
			// the units held under it; one that did would put its edges in.
			boxes = append(boxes, unit.Box)
		}
	}
	// An edge along the region's own border, the side of a transparent
	// rectangle the region is the box of, is where the image ends: the clip
	// to the region's box draws it, and no JPEG block straddles it.
	inner := r.Box.Outset(-2 * edgeWidth)
	if inner.Empty() {
		return boxes
	}
	return slices.DeleteFunc(boxes, func(b Rect) bool { return !b.Overlaps(inner) })
}

// flatRect reports whether the KindPath unit fills one rectangle under a CTM
// that keeps it axis-aligned, in a colour: the page names no pattern a fill
// could paint in (ISO 32000-2, 8.7.3.1), which needs a /Pattern resource. Its
// box is then the rectangle, cut by the clip in force when that is a
// rectangle too, which leaves the cut edges on the box.
func (f *flattener) flatRect(ctx context.Context, a *Analysis, ops []content.Operation, unit Unit) bool {
	rects := 0
	for _, op := range ops[unit.First:unit.Last] {
		switch op.Operator {
		case "re":
			rects++
		case "m", "l", "c", "v", "y", "h":
			return false
		}
	}
	pos := a.positions[unit.First]
	if rects != 1 || !axisAligned(pos.ctm) || (pos.clipped && !pos.exact) {
		return false
	}
	patterns, err := document.ResolveDict(ctx, f.doc, f.scan.pageResources["Pattern"])
	return err == nil && len(patterns) == 0
}

// formEdges returns the boxes, in default user space, where the form a
// KindForm unit draws puts a sharp edge in a composite drawn without text
// ([contentInfo.edges]), each cut to the unit's box: the unit's box whole
// when the form does not resolve or an edge's extent is unmeasured.
func (f *flattener) formEdges(ctx context.Context, a *Analysis, ops []content.Operation, unit Unit) []Rect {
	obj, stream, subtype, ok := f.scan.xobject(ctx, f.scan.pageResources, ops[unit.First])
	if !ok || subtype != "Form" {
		return []Rect{unit.Box}
	}
	info := f.scan.form(ctx, obj, stream, f.scan.pageResources, 1, nil)
	if info.edgesUnbounded {
		return []Rect{unit.Box}
	}
	matrix, _ := formGeometry(ctx, f.doc, stream.Dict)
	toUser := matrix.Mul(a.positions[unit.First].ctm)
	var out []Rect
	for _, e := range info.edges {
		if b := transformRect(toUser, e).Intersect(unit.Box); !b.Empty() {
			out = append(out, b)
		}
	}
	return out
}

// droppedImages returns how many images fewer the page lists, as
// [pdfimage.ExtractImagesRecursive] lists them, with after, the resource
// dictionary the run writes, than with before, the source's: the images whose
// pixels a composite now holds, net of the composites themselves. after is
// read with the objects the run queued on f.alloc in place of their
// references, as the output holds them.
func (f *flattener) droppedImages(ctx context.Context, before, after ast.PdfDict) int {
	was := pdfimage.CountResourceImages(ctx, f.doc, before)
	inlined, _ := f.inlineQueued(after, 0).(ast.PdfDict)
	return max(0, was-pdfimage.CountResourceImages(ctx, f.doc, inlined))
}

// inlineQueued returns obj with every reference to an object queued on
// f.alloc replaced by that object, read the same way, followed at most the
// document's form-depth budget deep: the copies the run made of forms and
// graphics states nest no deeper than the forms they copy.
func (f *flattener) inlineQueued(obj ast.PdfObject, depth int) ast.PdfObject {
	switch v := obj.(type) {
	case ast.PdfIndirectRef:
		queued, ok := f.alloc.Pending()[v.ObjectNum]
		if !ok || depth >= f.scan.maxDepth {
			return v
		}
		return f.inlineQueued(queued, depth+1)
	case ast.PdfDict:
		out := make(ast.PdfDict, len(v))
		for k, e := range v {
			out[k] = f.inlineQueued(e, depth)
		}
		return out
	case ast.PdfArray:
		out := make(ast.PdfArray, len(v))
		for i, e := range v {
			out[i] = f.inlineQueued(e, depth)
		}
		return out
	case ast.PdfStream:
		v.Dict, _ = f.inlineQueued(v.Dict, depth).(ast.PdfDict)
		return v
	}
	return obj
}

// encodeTiles encodes the composite of the device pixels px, samples holding
// comps 8-bit components per pixel, as [imagetiles.Encoder.Tiles] cuts it at
// f's quality: vector holds the device pixels the region's non-photographic
// units paint ([flattener.vectorBoxes]), and inner the pixels of px the
// region's box reaches, the others being its margin.
func (f *flattener) encodeTiles(ctx context.Context, samples []byte, px, inner image.Rectangle, comps int, vector []image.Rectangle) ([]imagetiles.Tile, error) {
	enc := imagetiles.Encoder{Quality: f.quality, Logger: f.doc.Logger}
	tiles, err := enc.Tiles(ctx, samples, px, inner, comps, vector)
	if err != nil {
		return nil, fmt.Errorf("encoding a composite: %w", err)
	}
	return tiles, nil
}
