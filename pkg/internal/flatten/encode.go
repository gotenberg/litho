package flatten

import (
	"bytes"
	"compress/zlib"
	"context"
	"errors"
	"fmt"
	"image"
	"log/slog"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/filter/dct"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	pdfimage "github.com/gotenberg/litho/pkg/internal/graphics/image"
)

// subsampleBelowQuality is the JPEG quality below which a composite's chroma
// is sampled at half resolution (4:2:0), the rule
// recolor.subsampleBelowQuality applies to the images it re-encodes, with the
// same value so both writers encode a quality alike.
const subsampleBelowQuality = 90

// minPhotoColors is how many distinct colours a composite must hold to read as
// photographic. A region of images and shadings with fewer is flat art, a
// logo or a scanned drawing on a plain ground, whose edges JPEG would ring and
// which Flate holds in few bytes anyway. 256 is the size of a palette: an
// image drawn in fewer colours can be indexed losslessly.
const minPhotoColors = 256

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

// photographic reports whether samples, 8-bit RGB, hold at least
// [minPhotoColors] distinct colours.
func photographic(samples []byte, comps int) bool {
	seen := make(map[uint32]struct{}, minPhotoColors)
	for i := 0; i+comps <= len(samples); i += comps {
		var key uint32
		for _, v := range samples[i : i+comps] {
			key = key<<8 | uint32(v)
		}
		seen[key] = struct{}{}
		if len(seen) >= minPhotoColors {
			return true
		}
	}
	return false
}

// encodeComposite compresses a composite's samples, w x h pixels of comps
// 8-bit components (grey, RGB or CMYK, as [flattener.compositeSamples] gives
// them): as a baseline JPEG at f's quality when photo says the region's content
// is photographic and its samples are too ([photographic]), unless Flate
// holds them in no more bytes, and under Flate otherwise. The JPEG encodes
// padded, the samples with the pixels outside the region's box replaced (see
// [padOutside]). It returns the data and its filter.
func (f *flattener) encodeComposite(ctx context.Context, samples, padded []byte, w, h, comps int, photo bool) ([]byte, string, error) {
	flate, err := deflate(samples)
	if err != nil {
		return nil, "", err
	}
	if f.quality == 0 || !photo || !photographic(samples, comps) {
		return flate, "FlateDecode", nil
	}
	opts := dct.EncodeOptions{Quality: f.quality, OptimizeHuffman: true}
	if f.quality < subsampleBelowQuality {
		opts.Subsampling = dct.Subsample420
	}
	data, err := dct.Encode(ctx, padded, w, h, comps, opts)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, "", ctxErr
		}
		if errors.Is(err, dct.ErrInvalidInput) {
			// Flate holds what a JPEG frame cannot (65535 pixels a side).
			f.doc.Logger.Debug("flatten: keeping a composite lossless that JPEG cannot hold", slog.Any("err", err))
			return flate, "FlateDecode", nil
		}
		return nil, "", fmt.Errorf("encoding a composite as JPEG: %w", err)
	}
	if len(data) >= len(flate) {
		return flate, "FlateDecode", nil
	}
	return data, "DCTDecode", nil
}

// deflate compresses samples under Flate (RFC 1950).
func deflate(samples []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	if _, err := zw.Write(samples); err != nil {
		return nil, fmt.Errorf("compressing a composite: %w", err)
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("compressing a composite: %w", err)
	}
	return buf.Bytes(), nil
}

// tileSize is the side, in device pixels of the composite, of the grid a
// composite mixing photographs with vector art is cut along, so the cells
// only images and shadings reach are encoded as JPEG and the others stay
// lossless. A grid rather than the boxes of the region's units: those boxes
// overlap, and a photograph a rule crosses keeps most of its area away from
// the rule, which a grid cell finds and a box does not. 128 is a multiple of
// the 16-pixel minimum coded unit of a 4:2:0 JPEG (ITU-T T.81, A.2), so no
// cell but the last of a row or column pads a block, and under half an inch
// at the default 300 dpi, fine enough to part a photograph from the captions
// and rules beside it while the few hundred bytes of header and tables each
// image costs stay small beside a cell's samples. Runs of cells of one kind
// are joined into one image, so the grid's fineness costs few images.
//
// # Corpus evidence
//
//   - govdocs, PDF/A-1b remediation of the 34 files it flattens: the outputs
//     weigh 163.4 MB with every mixed composite lossless, and 150.9, 138.5,
//     134.2 and 133.5 MB with the grid at 512, 256, 128 and 64 pixels, 64
//     growing 4 files, measured before the units under a region and filled
//     rectangles were read for edges. With them, 128 gives 134.4 MB.
const tileSize = 128

// tileOverlap is how many device pixels of the composite a tile reaches into
// the tile right of it and the one below it. A tile holds exactly the device
// pixels of the composite it covers, so drawn at the composite's resolution
// the tiles abut; at any other, a renderer maps each tile's edges to the
// device on its own, and a rounding that leaves a pixel centre on neither
// side of the shared edge, or an antialiased image edge, would draw a seam.
// A tile reaching past the edge covers it. The overlapped pixels are the same
// composite pixels in the JPEG tile and in the underlay under it
// ([flattener.encodeUnderlay]), which the JPEG tile, drawn later, paints.
const tileOverlap = 1

// compositeTile is one image of a composite: the device pixels it draws, its
// data and filter.
type compositeTile struct {
	px     image.Rectangle
	data   []byte
	filter string
}

// encodeTiles encodes the composite of the device pixels px, samples holding
// comps 8-bit components per pixel. When f's quality is 0, or vector, the
// device pixels the region's non-photographic units paint ([vectorBoxes]),
// is empty, it returns one image, as [flattener.encodeComposite] encodes it.
// Otherwise it cuts the composite along a [tileSize] grid, encodes each run
// of cells vector does not reach and whose pixels are [photographic] as a
// JPEG tile (unless Flate holds the run in no more bytes), and returns the
// composite as one lossless underlay with those tiles over it
// ([flattener.encodeUnderlay]) when some run is JPEG and the images take
// fewer bytes than the composite under Flate as one image, which it returns
// otherwise. inner holds the pixels of px the region's box reaches: the
// others are its margin.
func (f *flattener) encodeTiles(ctx context.Context, samples []byte, px, inner image.Rectangle, comps int, vector []image.Rectangle) ([]compositeTile, error) {
	w, h := px.Dx(), px.Dy()
	if f.quality == 0 {
		data, filterName, err := f.encodeComposite(ctx, samples, samples, w, h, comps, false)
		if err != nil {
			return nil, err
		}
		return []compositeTile{{px: px, data: data, filter: filterName}}, nil
	}
	if len(vector) == 0 {
		data, filterName, err := f.encodeComposite(ctx, samples, padOutside(samples, px, inner, comps, px), w, h, comps, true)
		if err != nil {
			return nil, err
		}
		return []compositeTile{{px: px, data: data, filter: filterName}}, nil
	}
	whole, err := deflate(samples)
	if err != nil {
		return nil, err
	}
	single := []compositeTile{{px: px, data: whole, filter: "FlateDecode"}}
	cols, rows := (w+tileSize-1)/tileSize, (h+tileSize-1)/tileSize
	cell := func(c, r int) image.Rectangle {
		return image.Rect(px.Min.X+c*tileSize, px.Min.Y+r*tileSize, px.Min.X+(c+1)*tileSize, px.Min.Y+(r+1)*tileSize).Intersect(px)
	}
	photo := make([]bool, cols*rows)
	found := false
	for r := range rows {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for c := range cols {
			t := cell(c, r)
			reached := slices.ContainsFunc(vector, func(v image.Rectangle) bool { return v.Overlaps(t) })
			if !reached && photographic(cropSamples(samples, px, comps, t), comps) {
				photo[r*cols+c] = true
				found = true
			}
		}
	}
	if !found {
		return single, nil
	}
	return f.encodeUnderlay(ctx, samples, px, inner, comps, cellRuns(photo, cols, rows), photo, cols, cell, single)
}

// underlayBand is how many device pixels along the inside of each JPEG tile's
// edges the underlay of [flattener.encodeUnderlay] keeps the composite's own
// samples in. A renderer places each image on whole device pixels on its own,
// and one rounds an edge a pixel outward or inward (poppler and PDFium both
// draw an image a pixel wider than its device box at times), so a JPEG tile
// may leave a pixel of the underlay showing along its edges: two pixels keep
// what shows there the page's own.
const underlayBand = 2

// encodeUnderlay lays out the images of a composite [flattener.encodeTiles]
// cuts into tiles: the runs of photographic cells are encoded as JPEG tiles, each reaching
// [tileOverlap] pixels into the cells right of and below it as the tiles of
// [flattener.encodeTiles] do (poppler draws them closer to the lossless
// composite so), and the composite as one lossless image under them, drawn first, in
// which each JPEG tile's pixels, but for a band of [underlayBand] pixels
// along its edges, repeat the last pixel of the band to their left, which
// Flate holds in few bytes. The pixels no JPEG tile covers are then drawn by
// one image placed as the lossless composite would be, so a renderer that
// resamples each image on its own resamples them as it resamples the
// lossless composite, and only the JPEG tiles differ from it. Abutting
// lossless tiles each resampled on their own move a column or a row of
// pixels where the lossless composite moves none. A run Flate holds in no
// more bytes than JPEG stays in the underlay. It returns single, the
// composite as one lossless image, when no run is JPEG or the images would
// take no fewer bytes.
//
// # Corpus evidence
//
//   - pdfjs bug1140761, the quarter and band redactions at q 90 against the
//     lossless fallback, by the redact-render outside rule: as abutting
//     tiles, 23 to 3043 pixels changed where no JPEG tile lies, at 72 and
//     300 dpi by PDFium 4.30.0 (pypdfium2) and poppler 26.09.0, a column or
//     row of a lossless tile drawn a pixel off; with the underlay, none, and
//     every pixel changed lies in a JPEG tile or within 2 pixels of its
//     edge (PDFium 190 and 213 at 72 dpi, 624 and 905 at 300 dpi; poppler
//     140 and 169, 1184 and 1527). No two images leave a gap between them.
//   - pdfjs, redact-render with the fallback at q 90: poppler's outside
//     check counts 158 to 1063 changed pixels on the 8 scenarios that differ
//     from lossless, against 239 to 1261 as abutting tiles.
//   - litho, govdocs and pdfjs flattened at q 90 against lossless, the 69
//     pages of the first 3 of the 66 files that hold JPEG tiles, by the
//     render cell's pixel rule: as abutting tiles, PDFium drew 3886 and
//     22286 pixels changed where no JPEG tile lies on 11 and 8 pages at 72
//     and 300 dpi, poppler 4975 and 25754 on 11 and 10 (govdocs
//     thread_400_400019 13569 at 300 dpi on page 2); with the underlay, 6,
//     133, 54 and 132, all on page 3 of govdocs thread_200_200414, as many
//     at 300 dpi as before. The flatten cell at q 90 matches as lossless
//     does (litho 32/0, govdocs 34/0, pdfjs 267/9).
func (f *flattener) encodeUnderlay(ctx context.Context, samples []byte, px, inner image.Rectangle, comps int, runs []image.Rectangle, photo []bool, cols int, cell func(c, r int) image.Rectangle, single []compositeTile) ([]compositeTile, error) {
	var jpegTiles []compositeTile
	total := 0
	for _, run := range runs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !photo[run.Min.Y*cols+run.Min.X] {
			continue
		}
		t := cell(run.Min.X, run.Min.Y).Union(cell(run.Max.X-1, run.Max.Y-1))
		if t.Max.X < px.Max.X {
			t.Max.X += tileOverlap
		}
		if t.Max.Y < px.Max.Y {
			t.Max.Y += tileOverlap
		}
		data, filterName, err := f.encodeComposite(ctx, cropSamples(samples, px, comps, t), padOutside(samples, px, inner, comps, t), t.Dx(), t.Dy(), comps, true)
		if err != nil {
			return nil, err
		}
		if filterName != "DCTDecode" {
			continue
		}
		total += len(data)
		jpegTiles = append(jpegTiles, compositeTile{px: t, data: data, filter: filterName})
	}
	if len(jpegTiles) == 0 {
		return single, nil
	}
	base := slices.Clone(samples)
	stride := px.Dx() * comps
	for _, t := range jpegTiles {
		fill := t.px.Inset(underlayBand)
		if fill.Empty() {
			continue
		}
		for y := fill.Min.Y; y < fill.Max.Y; y++ {
			row := (y - px.Min.Y) * stride
			src := row + (fill.Min.X-1-px.Min.X)*comps
			for x := fill.Min.X; x < fill.Max.X; x++ {
				i := row + (x-px.Min.X)*comps
				copy(base[i:i+comps], base[src:src+comps])
			}
		}
	}
	under, err := deflate(base)
	if err != nil {
		return nil, err
	}
	total += len(under)
	if total >= len(single[0].data) {
		return single, nil
	}
	return append([]compositeTile{{px: px, data: under, filter: "FlateDecode"}}, jpegTiles...), nil
}

// padOutside returns the samples of the device pixels t, inside px, of
// samples, the composite of px with comps components per pixel, each pixel
// outside inner taking the samples of the nearest pixel inside it. A JPEG
// spreads a block's error over the whole block (ITU-T T.81, A.3.3), so the
// sharp step between a region and the page beyond its box, which the margin
// holds and the clip to the box hides, would ring into the pixels just inside
// the box, which the clip shows. Under Flate the margin keeps its own pixels,
// which a renderer antialiasing the clip blends in.
func padOutside(samples []byte, px, inner image.Rectangle, comps int, t image.Rectangle) []byte {
	if inner.Empty() || t.In(inner) {
		return cropSamples(samples, px, comps, t)
	}
	stride := px.Dx() * comps
	out := make([]byte, 0, t.Dx()*t.Dy()*comps)
	for y := t.Min.Y; y < t.Max.Y; y++ {
		sy := min(max(y, inner.Min.Y), inner.Max.Y-1)
		for x := t.Min.X; x < t.Max.X; x++ {
			sx := min(max(x, inner.Min.X), inner.Max.X-1)
			i := (sy-px.Min.Y)*stride + (sx-px.Min.X)*comps
			out = append(out, samples[i:i+comps]...)
		}
	}
	return out
}

// cropSamples returns the samples of the device pixels t, inside px, of
// samples, the composite of px with comps components per pixel.
func cropSamples(samples []byte, px image.Rectangle, comps int, t image.Rectangle) []byte {
	if t == px {
		return samples
	}
	stride := px.Dx() * comps
	out := make([]byte, 0, t.Dx()*t.Dy()*comps)
	for y := t.Min.Y; y < t.Max.Y; y++ {
		row := (y-px.Min.Y)*stride + (t.Min.X-px.Min.X)*comps
		out = append(out, samples[row:row+t.Dx()*comps]...)
	}
	return out
}

// cellRuns returns rectangles of cells, in cell units, covering the cols x
// rows grid once, each holding cells photo marks alike: runs along each row,
// the same run on consecutive rows joined into one rectangle, in the order of
// their top-left cells, row by row.
func cellRuns(photo []bool, cols, rows int) []image.Rectangle {
	type run struct {
		x0, x1 int
		photo  bool
	}
	var out []image.Rectangle
	open := make(map[run]int)
	for y := range rows {
		next := make(map[run]int)
		for x := 0; x < cols; {
			key := run{x0: x, photo: photo[y*cols+x]}
			for x < cols && photo[y*cols+x] == key.photo {
				x++
			}
			key.x1 = x
			if k, ok := open[key]; ok {
				out[k].Max.Y = y + 1
				next[key] = k
				continue
			}
			out = append(out, image.Rect(key.x0, y, key.x1, y+1))
			next[key] = len(out) - 1
		}
		open = next
	}
	return out
}
