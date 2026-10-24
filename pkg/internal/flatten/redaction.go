package flatten

import (
	"context"
	"fmt"
	"image"
	"maps"
	"math"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/mutations"
	"github.com/gotenberg/litho/pkg/internal/render/geom"
	"github.com/gotenberg/litho/pkg/internal/render/page"
	"github.com/gotenberg/litho/pkg/internal/render/surface"
)

// Redaction is the raster fallback of redaction ([mutations.RasterFunc]): it
// draws the units of pg that begin at the operations seeds, each the Do of a
// form XObject or an sh whose content the regions select, as opaque images of
// the page with the regions cut out, and removes them from the content.
//
// It reuses the flattening's machinery with the seeds in place of
// transparency: [Analyze] grows a region around each seed holding everything
// an opaque image there must take with it, the page is rendered at
// [DefaultDPI] without its text and annotations, each region is cropped into
// an image drawn before the region's units, clipped to the region less the
// redaction's regions, and the units the image takes over are removed or
// clipped out of it as [Document] removes them. A region the redaction's
// regions cover whole gets no image: its units are removed. What it
// rasterises is that and nothing else: the seeds, and the units drawn among
// them that the image would otherwise hide or draw out of order. Text is never
// rasterised: a form showing text is replaced by a copy holding only its
// text, one per placement, which [mutations.RedactPageRegionsWith] then edits
// for the regions, and text objects stay where they are, above the image.
// Where a seed drawn after a text object hid its glyphs, the image is drawn
// again over them, as the flattening keeps such text hidden; where it only
// tinted them, they show untinted. Nothing else of the flattening runs: the
// page's transparency, its graphics states and its group stay as they are,
// and each image, which holds what those states composited, is drawn under
// one of its own that undoes them ([opaqueState]).
//
// Inside the redaction's regions an image paints nothing: its clip cuts them
// out by the even-odd rule (ISO 32000-2, 8.5.3.3.3), so what the page shows
// there is what the redaction leaves of the content under the image, as
// where it removes a path. Its samples there are the paper's colour too, a
// pixel the regions reach painted out whole, so the file holds no ink of a
// rasterised unit inside them. ISO 32000-2, 8.10.1 gives a form's content no
// bytes in the page's stream, and 8.7.4.2 gives a shading paint no geometry
// to cut, so an image of what they painted is the only way to keep their ink
// outside the regions while removing it inside.
//
// It reads pg as the document holds it, so alloc must hold no edit of pg:
// [ErrPageStaged] otherwise.
//
// # Corpus evidence
//
//   - pdfcpu (all 51) and govdocs (1 in 8, 137), the whole of page 1: the
//     fallback lifts all 11 form and shading refusals and the 4 of images a
//     form draws; 11 redactions then succeed and 4 stop at the scan, for
//     copies the page did not hold (another page's stream, a JPEG's bytes).
//   - litho (228), pdfjs (79), pdfcpu, govdocs, the four redact-render
//     scenarios: 117 scenarios refused without the fallback succeed with it,
//     none leaving ink inside a region by litho's or poppler's rendering.
//   - pdfjs issue2642, issue12752, issue17289, pdfcpu text_annotations:
//     poppler draws pixels outside the region changed where the images
//     paint; all but 1 to 7 a scenario show litho's rendering of the
//     source or a blend of it, and PDFium draws those few unchanged (the
//     redact-render cell adjudicates them).
//   - litho usedcs_2b: the image drawn again over a heading a shading hid
//     covered only the hollow frames litho draws for its unembedded
//     Helvetica, and the glyphs showed through for a reader with the font,
//     146 pixels by litho's rendering with the goldens' fonts, until the
//     coverage read them whole.
//   - litho transparency_2b: the images were drawn under the constant
//     alpha and Screen blend mode the page had set, which they already
//     held: PDFium drew 663 pixels faded outside the region, until they
//     were drawn under an opaque state.
func Redaction(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, pg *document.Page, seeds []int, regions [][4]float64) (mutations.RasterResult, error) {
	return redaction(ctx, alloc, doc, pg, seeds, regions, 0)
}

// RedactionJPEG returns [Redaction] encoding its images as [Document] does
// under [Options.JPEGQuality] q: a region's composite is cut into tiles, a
// tile only images and shadings reach whose pixels are photographic is
// encoded as a baseline JPEG at quality q, and the others stay lossless
// ([flattener.encodeTiles]). A tile a redaction region reaches, widened by a
// pixel, stays lossless ([flattener.regionImage]): the region's pixels hold
// the paper's colour exactly, in the file as on the page, and a JPEG block
// holding them and ink beside them would decode a trace of that ink into
// them. q 0 is [Redaction].
func RedactionJPEG(q int) mutations.RasterFunc {
	return func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, pg *document.Page, seeds []int, regions [][4]float64) (mutations.RasterResult, error) {
		return redaction(ctx, alloc, doc, pg, seeds, regions, q)
	}
}

// redaction is [Redaction] encoding photographic tiles as JPEG at quality,
// lossless throughout at 0.
func redaction(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, pg *document.Page, seeds []int, regions [][4]float64, quality int) (mutations.RasterResult, error) {
	var out mutations.RasterResult
	if pg.Ref.ObjectNum == 0 {
		return out, mutations.ErrPageNotAddressable
	}
	if _, staged := alloc.Pending()[pg.Ref.ObjectNum]; staged {
		return out, ErrPageStaged
	}
	f := &flattener{
		doc:       doc,
		alloc:     alloc,
		dpi:       DefaultDPI,
		quality:   quality,
		states:    make(map[int]ast.PdfObject),
		forms:     make(map[copyKey]ast.PdfIndirectRef),
		ungrouped: make(map[int]ast.PdfObject),
		patterns:  make(map[patternKey]ast.PdfObject),
		images:    make(map[int]ast.PdfObject),
		pool:      &surface.Pool{},
		private:   true,
		// An image drawn outside marked content is marked as an artifact
		// under the rule the annotation bakes follow
		// ([mutations.RasterArtifacts]): in a tagged document, or an
		// untagged one claiming PDF/UA or WTPDF, as ISO 14289-1, 7.1 wants
		// untagged content marked; plain content otherwise, so its
		// /Artifact tag is not a word the redaction's scan matches.
		unmarked: !mutations.RasterArtifacts(ctx, alloc, doc),
	}
	data, err := mutations.LatestPageContents(ctx, alloc, doc, pg)
	if err != nil {
		return out, fmt.Errorf("assembling page content: %w", err)
	}
	ops, ranges, err := content.ParseStreamDataWithOffsets(ctx, data, content.SplitGluedOperators())
	if err != nil {
		return out, fmt.Errorf("parsing page content: %w", err)
	}
	seedSet := make(map[int]bool, len(seeds))
	for _, s := range seeds {
		seedSet[s] = true
	}
	a, err := analyzeSeeded(ctx, doc, pg, ops, seedSet)
	if err != nil {
		return out, err
	}
	f.scan = newScanner(doc, pg.DrawResources)
	resources := maps.Clone(pg.DrawResources)
	if resources == nil {
		resources = ast.PdfDict{}
	}
	xobjects, err := f.xobjects(ctx, resources)
	if err != nil {
		return out, err
	}
	states, err := f.extGStates(ctx, resources)
	if err != nil {
		return out, err
	}
	// The page's graphics states stay as they are, so an image is drawn
	// where a constant alpha, a blend mode, a soft mask or a transfer
	// function the content set may be in force. The image already holds
	// their effect: it is drawn under a state that undoes them.
	f.opaque = f.newName(states, "Rs")
	states[f.opaque] = opaqueState()
	zones := make([]Rect, 0, len(regions))
	for _, r := range regions {
		zones = append(zones, rectOf(r[0], r[1], r[2], r[3]))
	}
	// A region the zones cover whole keeps nothing of what its image would
	// show, so it gets none, and the page is rendered only for the others.
	drawn := slices.ContainsFunc(a.Regions, func(r Region) bool { return !covers(zones, r.Box) })

	edits := make(map[int]*opEdit)
	edit := func(i int) *opEdit {
		e := edits[i]
		if e == nil {
			e = &opEdit{}
			edits[i] = e
		}
		return e
	}
	removed := make(map[int]bool)
	clippedBy := make(map[int][]Rect)
	var cover *coverage
	if drawn {
		shared := page.NewShared(doc.Limits().WithDefaults().MaxRenderPixels)
		res, g, dpi, _, err := f.render(ctx, pg, shared)
		if err != nil {
			return out, err
		}
		cover = &coverage{f: f, pg: pg, g: g, dpi: dpi, result: res, shared: shared, holes: disjoint(zones)}
		// The renderings are read no further than this page.
		defer cover.release()
	}
	for _, r := range a.Regions {
		if !covers(zones, r.Box) {
			var holes []Rect
			for _, z := range cover.holes {
				if hole := z.Intersect(r.Box); !hole.Empty() {
					holes = append(holes, hole)
				}
			}
			// The image's samples reach a pixel past the region on each side,
			// where the clip hides them, so every zone is painted out of
			// them whole, not only the part inside the region.
			var vector []Rect
			if f.quality > 0 {
				vector = f.vectorBoxes(ctx, a, ops, r)
			}
			img, ok, err := f.regionImage(ctx, cover.result, cover.g, a.crop, r.Box, xobjects, vector, zones)
			if err != nil {
				return out, err
			}
			if ok {
				e := edit(r.Insert)
				e.before = append(e.before, f.drawImage(a.positions[r.Insert], r.Box, holes, img.tiles)...)
				for _, t := range img.tiles {
					if ref, isRef := xobjects[t.name].(ast.PdfIndirectRef); isRef {
						out.Images = append(out.Images, ref.ObjectNum)
					}
				}
				// Text drawn under a seed stays above the image, as the
				// flattening keeps it: where the seed hid it, the image is
				// drawn again over its glyph pixels, so it stays hidden.
				if _, err := cover.hideCovered(ctx, a, r, img, edit); err != nil {
					return out, err
				}
			}
		}
		for _, u := range r.Removed {
			removed[u] = true
		}
		for _, u := range r.Clipped {
			clippedBy[u] = append(clippedBy[u], r.Box)
		}
	}
	for _, u := range a.Hidden {
		removed[u] = true
	}
	for _, u := range slices.Sorted(maps.Keys(removed)) {
		unit := a.Units[u]
		ref, err := f.removeUnit(ctx, ops, unit, pg.DrawResources, xobjects, edit)
		if err != nil {
			return out, err
		}
		if ref.ObjectNum != 0 {
			out.TextForms = append(out.TextForms, mutations.TextFormPlacement{Ref: ref, CTM: [6]float64(a.positions[unit.First].ctm)})
		}
	}
	out.Units = len(removed)
	for _, u := range slices.Sorted(maps.Keys(clippedBy)) {
		if removed[u] {
			continue
		}
		unit := a.Units[u]
		before, after := clipOut(a.positions[unit.First].ctm, a.crop, clippedBy[u])
		edit(unit.First).before = append(edit(unit.First).before, before...)
		edit(unit.Last).after = append(edit(unit.Last).after, after...)
	}
	if len(edits) == 0 {
		return out, nil
	}
	dropUnused(ops, edits, xobjects)
	if err := mutations.EditPageSpans(ctx, alloc, doc, pg, spanEdits(edits, ranges)); err != nil {
		return out, fmt.Errorf("splicing page content: %w", err)
	}
	dict := maps.Clone(mutations.LatestPageDict(alloc, pg))
	if len(xobjects) > 0 {
		resources["XObject"] = xobjects
	}
	if f.opaqueUsed {
		resources["ExtGState"] = states
	}
	dict["Resources"] = resources
	alloc.AllocAt(pg.Ref.ObjectNum, dict)
	return out, nil
}

// dropUnused removes from xobjects, the page's /XObject resources, each name
// only removed operations of ops drew. The form or image it names is what
// the images and the text copies took over, the content the regions selected
// among it: left in the resources, it stays reachable from the page, and the
// redaction's scan finds the removed text there. A name another operation
// still draws stays.
func dropUnused(ops []content.Operation, edits map[int]*opEdit, xobjects ast.PdfDict) {
	drawn := make(map[ast.PdfName]bool)
	removed := make(map[ast.PdfName]bool)
	for i, op := range ops {
		if op.Operator != "Do" || len(op.Operands) != 1 {
			continue
		}
		name, ok := op.Operands[0].(ast.PdfName)
		if !ok {
			continue
		}
		if e := edits[i]; e != nil && e.remove {
			removed[name] = true
		} else {
			drawn[name] = true
		}
	}
	for name := range removed {
		if !drawn[name] {
			delete(xobjects, name)
		}
	}
}

// disjoint returns boxes covering what the union of boxes covers, no two of
// them sharing area: the bands between the distinct heights of their edges,
// each split into the runs the boxes crossing it cover. The even-odd rule
// (ISO 32000-2, 8.5.3.3.3) cuts an area two holes share back into the clip,
// so the holes of a clip are made disjoint first.
func disjoint(boxes []Rect) []Rect {
	var ys []float64
	for _, b := range boxes {
		if !b.Empty() {
			ys = append(ys, b.MinY, b.MaxY)
		}
	}
	slices.Sort(ys)
	ys = slices.Compact(ys)
	var out []Rect
	for k := 0; k+1 < len(ys); k++ {
		y0, y1 := ys[k], ys[k+1]
		var runs [][2]float64
		for _, b := range boxes {
			if !b.Empty() && b.MinY <= y0 && b.MaxY >= y1 {
				runs = append(runs, [2]float64{b.MinX, b.MaxX})
			}
		}
		slices.SortFunc(runs, func(p, q [2]float64) int {
			switch {
			case p[0] < q[0]:
				return -1
			case p[0] > q[0]:
				return 1
			}
			return 0
		})
		for i := 0; i < len(runs); {
			x0, x1 := runs[i][0], runs[i][1]
			for i++; i < len(runs) && runs[i][0] <= x1; i++ {
				x1 = max(x1, runs[i][1])
			}
			out = append(out, Rect{MinX: x0, MinY: y0, MaxX: x1, MaxY: y1})
		}
	}
	return out
}

// covers reports whether a zone of zones holds box whole.
func covers(zones []Rect, box Rect) bool {
	return slices.ContainsFunc(zones, func(z Rect) bool { return z.Contains(box) })
}

// paintOut sets to the paper's colour every pixel of samples, the
// comps-component pixels px addresses, that a box of boxes reaches through m,
// the matrix from default user space to device pixels: white, 1 on every
// component, or for four components (CMYK) no ink (ISO 32000-2, 8.6.4.4). A
// pixel the box only partly covers is painted out whole. A box with a
// coordinate that is not a number reaches no pixel, as it selects nothing to
// redact.
func paintOut(samples []byte, px image.Rectangle, comps int, m geom.Matrix, boxes []Rect) {
	if comps <= 0 {
		return
	}
	paper := make([]byte, comps)
	if comps != 4 { //nolint:mnd // four components are CMYK, whose paper is no ink
		for k := range paper {
			paper[k] = 255
		}
	}
	for _, b := range boxes {
		hole, ok := holeRect(m, b, px)
		if !ok {
			continue
		}
		for y := hole.Min.Y; y < hole.Max.Y; y++ {
			row := (y - px.Min.Y) * px.Dx()
			for x := hole.Min.X; x < hole.Max.X; x++ {
				i := (row + x - px.Min.X) * comps
				copy(samples[i:i+comps], paper)
			}
		}
	}
}

// holeRect returns the device pixels of px that b, a box in default user
// space, reaches through m: every pixel it covers even in part, the pixels
// [paintOut] paints out. A box with a coordinate that is not a number reaches
// none.
func holeRect(m geom.Matrix, b Rect, px image.Rectangle) (image.Rectangle, bool) {
	d := transformRect(m, b)
	if math.IsNaN(d.MinX) || math.IsNaN(d.MinY) || math.IsNaN(d.MaxX) || math.IsNaN(d.MaxY) {
		return image.Rectangle{}, false
	}
	x0 := int(max(float64(px.Min.X), math.Floor(d.MinX)))
	y0 := int(max(float64(px.Min.Y), math.Floor(d.MinY)))
	x1 := int(min(float64(px.Max.X), math.Ceil(d.MaxX)))
	y1 := int(min(float64(px.Max.Y), math.Ceil(d.MaxY)))
	if x0 >= x1 || y0 >= y1 {
		return image.Rectangle{}, false
	}
	return image.Rect(x0, y0, x1, y1), true
}

// holePixels returns the device pixels of px each box of holes paints out
// ([holeRect]), widened by [pixelMargin] on each side: the pixels a JPEG tile
// of the composite of px must not reach (see [flattener.regionImage]).
func holePixels(m geom.Matrix, holes []Rect, px image.Rectangle) []image.Rectangle {
	var out []image.Rectangle
	for _, b := range holes {
		if hole, ok := holeRect(m, b, px); ok {
			out = append(out, image.Rect(hole.Min.X-pixelMargin, hole.Min.Y-pixelMargin, hole.Max.X+pixelMargin, hole.Max.Y+pixelMargin))
		}
	}
	return out
}
