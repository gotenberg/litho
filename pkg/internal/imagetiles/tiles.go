package imagetiles

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
	"github.com/gotenberg/litho/pkg/internal/filter/dct"
)

// SubsampleBelowQuality is the JPEG quality below which a tile's chroma is
// sampled at half resolution (4:2:0), the rule recolor.subsampleBelowQuality
// applies to the images it re-encodes, with the same value so both writers
// encode a quality alike.
const SubsampleBelowQuality = 90

// MinPhotoColors is how many distinct colours samples must hold to read as
// photographic. A raster of fewer is flat art, a logo or a scanned drawing on
// a plain ground, whose edges JPEG would ring and which Flate holds in few
// bytes anyway. 256 is the size of a palette: an image drawn in fewer colours
// can be indexed losslessly.
const MinPhotoColors = 256

// Size is the side, in pixels of the raster, of the grid a raster mixing
// photographs with sharp edges is cut along, so the cells only photographs
// reach are encoded as JPEG and the others stay lossless. A grid rather than
// the boxes of what the raster holds: those boxes overlap, and a photograph a
// rule crosses keeps most of its area away from the rule, which a grid cell
// finds and a box does not. 128 is a multiple of the 16-pixel minimum coded
// unit of a 4:2:0 JPEG (ITU-T T.81, A.2), so no cell but the last of a row or
// column pads a block, and under half an inch at flattening's default 300
// dpi, fine enough to part a photograph from the captions and rules beside it
// while the few hundred bytes of header and tables each image costs stay
// small beside a cell's samples. Runs of cells of one kind are joined into
// one image, so the grid's fineness costs few images.
//
// # Corpus evidence
//
//   - govdocs, PDF/A-1b remediation of the 34 files it flattens: the outputs
//     weigh 163.4 MB with every mixed composite lossless, and 150.9, 138.5,
//     134.2 and 133.5 MB with the grid at 512, 256, 128 and 64 pixels, 64
//     growing 4 files, measured before the units under a region and filled
//     rectangles were read for edges. With them, 128 gives 134.4 MB.
const Size = 128

// Overlap is how many pixels of the raster a JPEG tile reaches into the tile
// right of it and the one below it. A tile holds exactly the pixels of the
// raster it covers, so drawn at the raster's resolution the tiles abut; at
// any other, a renderer maps each tile's edges to the device on its own, and
// a rounding that leaves a pixel centre on neither side of the shared edge,
// or an antialiased image edge, would draw a seam. A tile reaching past the
// edge covers it. The overlapped pixels are the same raster pixels in the
// JPEG tile and in the underlay under it ([Encoder.Tiles]), which the JPEG
// tile, drawn later, paints.
const Overlap = 1

// UnderlayBand is how many pixels along the inside of each JPEG tile's edges
// the underlay of [Encoder.Tiles] keeps the raster's own samples in. A
// renderer places each image on whole device pixels on its own, and one
// rounds an edge a pixel outward or inward (poppler and PDFium both draw an
// image a pixel wider than its device box at times), so a JPEG tile may leave
// a pixel of the underlay showing along its edges: two pixels keep what shows
// there the raster's own.
const UnderlayBand = 2

// Tile is one image of an encoded raster: the pixels of the raster it draws,
// its data, its filter, and the /DecodeParms that data needs, nil when none.
type Tile struct {
	Px     image.Rectangle
	Data   []byte
	Filter string
	Parms  ast.PdfDict
}

// JPEG reports whether t is encoded as JPEG.
func (t Tile) JPEG() bool { return t.Filter == "DCTDecode" }

// LosslessFunc compresses samples, rows w pixels wide of comps 8-bit
// components, losslessly under FlateDecode, and returns the data and the
// /DecodeParms it needs, nil when none.
type LosslessFunc func(ctx context.Context, samples []byte, w, comps int) ([]byte, ast.PdfDict, error)

// Encoder encodes rasters as [Tile] images.
type Encoder struct {
	// Quality is the JPEG quality, 1 to 100, on the scale of the
	// Independent JPEG Group's libjpeg; 0 keeps every raster lossless.
	Quality int
	// Lossless compresses the lossless images; nil is plain Flate.
	Lossless LosslessFunc
	// Logger receives what the encoder logs; nil discards it.
	Logger *slog.Logger
}

// lossless compresses samples as [Encoder.Lossless] says.
func (e Encoder) lossless(ctx context.Context, samples []byte, w, comps int) ([]byte, ast.PdfDict, error) {
	if e.Lossless != nil {
		return e.Lossless(ctx, samples, w, comps)
	}
	data, err := Deflate(samples)
	return data, nil, err
}

// Photographic reports whether samples, of comps 8-bit components per pixel,
// hold at least [MinPhotoColors] distinct colours.
func Photographic(samples []byte, comps int) bool {
	seen := make(map[uint32]struct{}, MinPhotoColors)
	for i := 0; i+comps <= len(samples); i += comps {
		var key uint32
		for _, v := range samples[i : i+comps] {
			key = key<<8 | uint32(v)
		}
		seen[key] = struct{}{}
		if len(seen) >= MinPhotoColors {
			return true
		}
	}
	return false
}

// Composite compresses a raster's samples, w x h pixels of comps 8-bit
// components (grey, RGB or CMYK): as a baseline JPEG at e's quality when
// photo says its content is photographic and its samples are too
// ([Photographic]), unless the lossless encoding holds them in no more bytes,
// and losslessly otherwise. The JPEG encodes padded, the samples with any
// pixels a clip hides replaced (see [PadOutside]). It returns the data, its
// filter and its /DecodeParms.
func (e Encoder) Composite(ctx context.Context, samples, padded []byte, w, h, comps int, photo bool) ([]byte, string, ast.PdfDict, error) {
	flate, parms, err := e.lossless(ctx, samples, w, comps)
	if err != nil {
		return nil, "", nil, err
	}
	if e.Quality == 0 || !photo || !Photographic(samples, comps) {
		return flate, "FlateDecode", parms, nil
	}
	opts := dct.EncodeOptions{Quality: e.Quality, OptimizeHuffman: true}
	if e.Quality < SubsampleBelowQuality {
		opts.Subsampling = dct.Subsample420
	}
	data, err := dct.Encode(ctx, padded, w, h, comps, opts)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, "", nil, ctxErr
		}
		if errors.Is(err, dct.ErrInvalidInput) {
			// Flate holds what a JPEG frame cannot (65535 pixels a side).
			if e.Logger != nil {
				e.Logger.Debug("imagetiles: keeping a raster lossless that JPEG cannot hold", slog.Any("err", err))
			}
			return flate, "FlateDecode", parms, nil
		}
		return nil, "", nil, fmt.Errorf("encoding a raster as JPEG: %w", err)
	}
	if len(data) >= len(flate) {
		return flate, "FlateDecode", parms, nil
	}
	return data, "DCTDecode", nil, nil
}

// Deflate compresses samples under Flate (RFC 1950).
func Deflate(samples []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	if _, err := zw.Write(samples); err != nil {
		return nil, fmt.Errorf("compressing a raster: %w", err)
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("compressing a raster: %w", err)
	}
	return buf.Bytes(), nil
}

// Tiles encodes the raster of the pixels px, samples holding comps 8-bit
// components per pixel. When e's quality is 0 it returns one lossless image.
// When vector, the pixels holding sharp edges or anything else no JPEG may
// touch, is empty, it returns one image, as [Encoder.Composite] encodes it.
// Otherwise it cuts the raster along a [Size] grid, encodes each run of cells
// vector does not reach and whose pixels are [Photographic] as a JPEG tile
// (unless the lossless encoding holds the run in no more bytes), and returns
// the raster as one lossless underlay with those tiles over it when some run
// is JPEG and the images take fewer bytes than the raster as one lossless
// image, which it returns otherwise. inner holds the pixels of px a clip
// shows: the others are a margin, which a JPEG tile encodes padded (see
// [PadOutside]).
//
// A JPEG tile reaches [Overlap] pixels into the cells right of and below it,
// so no pixel of vector, widened by that many pixels to its left and above,
// lies in a JPEG tile.
func (e Encoder) Tiles(ctx context.Context, samples []byte, px, inner image.Rectangle, comps int, vector []image.Rectangle) ([]Tile, error) {
	w, h := px.Dx(), px.Dy()
	if e.Quality == 0 {
		data, filterName, parms, err := e.Composite(ctx, samples, samples, w, h, comps, false)
		if err != nil {
			return nil, err
		}
		return []Tile{{Px: px, Data: data, Filter: filterName, Parms: parms}}, nil
	}
	if len(vector) == 0 {
		data, filterName, parms, err := e.Composite(ctx, samples, PadOutside(samples, px, inner, comps, px), w, h, comps, true)
		if err != nil {
			return nil, err
		}
		return []Tile{{Px: px, Data: data, Filter: filterName, Parms: parms}}, nil
	}
	whole, wholeParms, err := e.lossless(ctx, samples, w, comps)
	if err != nil {
		return nil, err
	}
	single := []Tile{{Px: px, Data: whole, Filter: "FlateDecode", Parms: wholeParms}}
	cols, rows := (w+Size-1)/Size, (h+Size-1)/Size
	cell := func(c, r int) image.Rectangle {
		return image.Rect(px.Min.X+c*Size, px.Min.Y+r*Size, px.Min.X+(c+1)*Size, px.Min.Y+(r+1)*Size).Intersect(px)
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
			if !reached && Photographic(CropSamples(samples, px, comps, t), comps) {
				photo[r*cols+c] = true
				found = true
			}
		}
	}
	if !found {
		return single, nil
	}
	return e.underlay(ctx, samples, px, inner, comps, CellRuns(photo, cols, rows), photo, cols, cell, single)
}

// underlay lays out the images of a raster [Encoder.Tiles] cuts into tiles:
// the runs of photographic cells are encoded as JPEG tiles, each reaching
// [Overlap] pixels into the cells right of and below it (poppler draws them
// closer to the lossless raster so), and the raster as one lossless image
// under them, drawn first, in which each JPEG tile's pixels, but for a band of
// [UnderlayBand] pixels along its edges, repeat the last pixel of the band to
// their left, which Flate holds in few bytes. The pixels no JPEG tile covers
// are then drawn by one image placed as the lossless raster would be, so a
// renderer that resamples each image on its own resamples them as it
// resamples the lossless raster, and only the JPEG tiles differ from it.
// Abutting lossless tiles each resampled on their own move a column or a row
// of pixels where the lossless raster moves none. A run the lossless encoding
// holds in no more bytes than JPEG stays in the underlay. It returns single,
// the raster as one lossless image, when no run is JPEG or the images would
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
func (e Encoder) underlay(ctx context.Context, samples []byte, px, inner image.Rectangle, comps int, runs []image.Rectangle, photo []bool, cols int, cell func(c, r int) image.Rectangle, single []Tile) ([]Tile, error) {
	var jpegTiles []Tile
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
			t.Max.X += Overlap
		}
		if t.Max.Y < px.Max.Y {
			t.Max.Y += Overlap
		}
		data, filterName, _, err := e.Composite(ctx, CropSamples(samples, px, comps, t), PadOutside(samples, px, inner, comps, t), t.Dx(), t.Dy(), comps, true)
		if err != nil {
			return nil, err
		}
		if filterName != "DCTDecode" {
			continue
		}
		total += len(data)
		jpegTiles = append(jpegTiles, Tile{Px: t, Data: data, Filter: filterName})
	}
	if len(jpegTiles) == 0 {
		return single, nil
	}
	base := slices.Clone(samples)
	stride := px.Dx() * comps
	for _, t := range jpegTiles {
		fill := t.Px.Inset(UnderlayBand)
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
	under, parms, err := e.lossless(ctx, base, px.Dx(), comps)
	if err != nil {
		return nil, err
	}
	total += len(under)
	if total >= len(single[0].Data) {
		return single, nil
	}
	return append([]Tile{{Px: px, Data: under, Filter: "FlateDecode", Parms: parms}}, jpegTiles...), nil
}

// PadOutside returns the samples of the pixels t, inside px, of samples, the
// raster of px with comps components per pixel, each pixel outside inner
// taking the samples of the nearest pixel inside it. A JPEG spreads a block's
// error over the whole block (ITU-T T.81, A.3.3), so a sharp step between
// what a clip shows and the margin beyond it, which the clip hides, would
// ring into the pixels just inside it, which the clip shows. Under Flate the
// margin keeps its own pixels, which a renderer antialiasing the clip blends
// in.
func PadOutside(samples []byte, px, inner image.Rectangle, comps int, t image.Rectangle) []byte {
	if inner.Empty() || t.In(inner) {
		return CropSamples(samples, px, comps, t)
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

// CropSamples returns the samples of the pixels t, inside px, of samples, the
// raster of px with comps components per pixel.
func CropSamples(samples []byte, px image.Rectangle, comps int, t image.Rectangle) []byte {
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

// CellRuns returns rectangles of cells, in cell units, covering the cols x
// rows grid once, each holding cells photo marks alike: runs along each row,
// the same run on consecutive rows joined into one rectangle, in the order of
// their top-left cells, row by row.
func CellRuns(photo []bool, cols, rows int) []image.Rectangle {
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
