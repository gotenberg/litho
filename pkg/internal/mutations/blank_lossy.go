package mutations

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"log/slog"
	"maps"
	"math"
	"slices"
	"strconv"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/heuristics"
	"github.com/gotenberg/litho/pkg/internal/imagetiles"
)

// lossyFormLevels is how many form XObjects [EncodeBlankedPhotographs] puts
// between a placement and the images it draws: the form that takes the
// image's place, and the form placing each JPEG tile.
const lossyFormLevels = 2

// lossyTileOverhead is a bound on the bytes each image and form
// [EncodeBlankedPhotographs] writes costs beside its data: its dictionary,
// its object header and its cross-reference entry. A photograph whose tiles
// save less than that per object stays one lossless image.
const lossyTileOverhead = 400

// BlankedPhotograph is what [EncodeBlankedPhotographs] needs of one blanked
// picture the source held as JPEG or JPEG 2000: the rectangles of its
// samples, in its own pixel grid with the top row first, that a region could
// reach, and the deepest form nesting it was drawn at.
type BlankedPhotograph struct {
	Reach     []image.Rectangle
	FormDepth int
}

// blankedPhotograph returns what [EncodeBlankedPhotographs] needs of a picture
// of w x h samples blankImage just blanked, whose source dictionary is dict,
// placed by box under zones: nil when the source is no photograph
// ([photographicSource]). Each zone that meets the placement reaches the
// samples [candidateSamples] bounds, a superset of those it blanked.
func blankedPhotograph(dict ast.PdfDict, box graphics.ImageBox, zones []heuristics.Zone, w, h int) *BlankedPhotograph {
	if !photographicSource(dict) {
		return nil
	}
	painted := heuristics.ZoneFromRect([4]float64{box.MinX, box.MinY, box.MaxX, box.MaxY})
	photo := &BlankedPhotograph{FormDepth: box.FormDepth}
	for _, z := range zones {
		if !heuristics.ZonesOverlap(painted, z) {
			continue
		}
		minPx, minPy, maxPx, maxPy, ok := candidateSamples(box, []heuristics.Zone{z}, w, h)
		if !ok {
			// No bound: the whole picture stays lossless.
			minPx, minPy, maxPx, maxPy = 0, 0, w-1, h-1
		}
		photo.Reach = append(photo.Reach, image.Rect(minPx, minPy, maxPx+1, maxPy+1))
	}
	return photo
}

// addBlankedPhotograph records photo, blanked at objNum, in m, which it
// allocates on first use, and returns m. Nil photo leaves m as it was.
func addBlankedPhotograph(m map[int]BlankedPhotograph, objNum int, photo *BlankedPhotograph) map[int]BlankedPhotograph {
	if photo == nil {
		return m
	}
	if m == nil {
		m = make(map[int]BlankedPhotograph)
	}
	have := m[objNum]
	have.Reach = append(have.Reach, photo.Reach...)
	have.FormDepth = max(have.FormDepth, photo.FormDepth)
	m[objNum] = have
	return m
}

// MergeBlankedPhotographs adds the pictures of from to into, which it
// allocates on first use, and returns into: two placements of one picture
// blanked in place reach the samples either reaches.
func MergeBlankedPhotographs(into, from map[int]BlankedPhotograph) map[int]BlankedPhotograph {
	for _, objNum := range slices.Sorted(maps.Keys(from)) {
		photo := from[objNum]
		into = addBlankedPhotograph(into, objNum, &photo)
	}
	return into
}

// EncodeBlankedPhotographs re-encodes the blanked pictures photos names,
// each queued on alloc at its object number, so that the parts of each no
// region reaches are baseline JPEG (ITU-T T.81) at quality q, and returns
// how many it re-encoded.
//
// Blanking writes a picture losslessly ([encodeBlankedSamples]), so a JPEG
// or JPEG 2000 photograph grows several times over. A single image cannot
// hold its covered samples exactly under a lossy filter: a JPEG spreads each
// 8 by 8 block's error over the whole block (ITU-T T.81, A.3.3), so a blank
// next to the picture decodes with traces of it. So the picture becomes a
// form XObject at the image's own object number, and every placement, every
// resource entry and every structure reference (an object reference names
// any XObject, ISO 32000-1, 14.7.4.3) draws or names the form instead. The
// form draws the samples as [imagetiles.Encoder.Tiles] cuts them, which
// flattening and the raster fallback use too: one lossless image of the
// whole picture, and over it JPEG tiles of the runs of grid cells no region
// reaches whose samples are photographic. Every cell a region's samples
// reach, widened by [imagetiles.UnderlayBand] samples, is no JPEG tile, so
// no JPEG holds a covered sample: the covered samples are the blank exactly,
// in the file as on the page, and the JPEG tiles only approximate samples
// the redaction kept. An image paints the unit square (ISO 32000-1,
// 8.9.5.2), and so does a form painting its tiles there under no /Matrix
// (8.10.1), each JPEG tile placed by a form of its own whose /Matrix maps
// the unit square onto the tile's samples, so no content stream holds a
// number a scan of removed digits could match.
//
// Each image keeps the blanked picture's colour space, and with it its
// colour: DCTDecode codes grey, RGB and CMYK samples (ISO 32000-1, 7.4.8).
// A picture stays one lossless image when the re-encoding saves no bytes,
// when its colour space is not one JPEG codes, when it has a soft mask (the
// tiles would composite twice over the underlay where the mask is not
// opaque, ISO 32000-1, 11.3.6), or when the two forms would nest its tiles
// deeper than the document's form depth budget.
func EncodeBlankedPhotographs(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, photos map[int]BlankedPhotograph, q int) (int, error) {
	n := 0
	for _, objNum := range slices.Sorted(maps.Keys(photos)) {
		if err := ctx.Err(); err != nil {
			return n, err
		}
		done, err := encodeBlankedPhotograph(ctx, alloc, doc, objNum, photos[objNum], q)
		if err != nil {
			return n, fmt.Errorf("re-encoding blanked image object %d: %w", objNum, err)
		}
		if done {
			n++
		}
	}
	return n, nil
}

// encodeBlankedPhotograph re-encodes the blanked picture queued at objNum as
// [EncodeBlankedPhotographs] describes, and reports whether it did.
func encodeBlankedPhotograph(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, objNum int, photo BlankedPhotograph, q int) (bool, error) {
	stream, ok := alloc.Pending()[objNum].(ast.PdfStream)
	if !ok {
		return false, nil
	}
	keep := func(why string) (bool, error) {
		doc.Logger.Debug("redact: keeping a blanked photograph lossless", slog.Int("object", objNum), slog.String("why", why))
		return false, nil
	}
	dict := stream.Dict
	if photo.FormDepth+lossyFormLevels > doc.Limits().MaxFormDepth {
		return keep("its tiles would nest past the form depth budget")
	}
	if _, masked := dict["SMask"]; masked {
		return keep("it has a soft mask")
	}
	cs, err := resolveAllocFirst(ctx, alloc, doc, dict["ColorSpace"])
	if err != nil || !jpegColorSpace(cs) {
		return keep("JPEG does not code its colour space")
	}
	w, wOK := dict["Width"].(ast.PdfInt)
	h, hOK := dict["Height"].(ast.PdfInt)
	bpc, _ := dict["BitsPerComponent"].(ast.PdfInt)
	if !wOK || !hOK || w <= 0 || h <= 0 || bpc != 8 {
		return keep("it is no 8-bit picture")
	}
	samples, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		return false, fmt.Errorf("decoding the blanked samples: %w", err)
	}
	pixels := int(w) * int(h)
	comps := len(samples) / pixels
	if (comps != 1 && comps != 3 && comps != 4) || len(samples) != comps*pixels {
		return keep("its samples are not 1, 3 or 4 components a pixel")
	}

	px := image.Rect(0, 0, int(w), int(h))
	var reach []image.Rectangle
	for _, r := range photo.Reach {
		if wide := r.Inset(-imagetiles.UnderlayBand).Intersect(px); !wide.Empty() {
			reach = append(reach, wide)
		}
	}
	if len(reach) == 0 {
		// The reaches are what tells the covered samples apart; without
		// one, no tile is known to hold none of them.
		return keep("no region reaches it")
	}
	enc := imagetiles.Encoder{
		Quality: q,
		Lossless: func(ctx context.Context, samples []byte, w, comps int) ([]byte, ast.PdfDict, error) {
			return encodeBlankedSamples(ctx, samples, w, comps, 8, true)
		},
		Logger: doc.Logger,
	}
	tiles, err := enc.Tiles(ctx, samples, px, px, comps, reach)
	if err != nil {
		return false, err
	}
	total := 0
	for _, t := range tiles {
		total += len(t.Data) + lossyTileOverhead
		if t.JPEG() {
			total += lossyTileOverhead
		}
	}
	stored, err := stream.Bytes(ctx)
	if err != nil {
		return false, fmt.Errorf("reading the blanked samples: %w", err)
	}
	if len(tiles) < 2 || total >= len(stored) {
		return keep("its JPEG tiles save no bytes")
	}

	resources := make(ast.PdfDict, len(tiles))
	var drawn bytes.Buffer
	for i, t := range tiles {
		tileDict := ast.PdfDict{
			"Type":             ast.PdfName("XObject"),
			"Subtype":          ast.PdfName("Image"),
			"Width":            ast.PdfInt(t.Px.Dx()),
			"Height":           ast.PdfInt(t.Px.Dy()),
			"ColorSpace":       dict["ColorSpace"],
			"BitsPerComponent": ast.PdfInt(8),
			"Filter":           ast.PdfName(t.Filter),
		}
		if t.Parms != nil {
			tileDict["DecodeParms"] = t.Parms
		}
		ref := alloc.Alloc(ast.NewStream(tileDict, t.Data))
		name := lossyTileName(i)
		if i > 0 {
			// The unit square maps onto the tile's samples, the image's top
			// row at the top of the unit square (ISO 32000-1, 8.9.5.2).
			ref = alloc.Alloc(ast.NewStream(ast.PdfDict{
				"Type":    ast.PdfName("XObject"),
				"Subtype": ast.PdfName("Form"),
				"BBox":    clipFreeBox(),
				"Matrix": ast.PdfArray{
					unitFraction(t.Px.Dx(), int(w)), ast.PdfInt(0), ast.PdfInt(0),
					unitFraction(t.Px.Dy(), int(h)),
					unitFraction(t.Px.Min.X, int(w)),
					unitFraction(int(h)-t.Px.Max.Y, int(h)),
				},
				"Resources": ast.PdfDict{"XObject": ast.PdfDict{"I": ref}},
			}, []byte("/I Do")))
		}
		resources[name] = ref
		if i > 0 {
			drawn.WriteByte(' ')
		}
		drawn.WriteString("/" + string(name) + " Do")
	}
	form := ast.PdfDict{
		"Type":      ast.PdfName("XObject"),
		"Subtype":   ast.PdfName("Form"),
		"BBox":      clipFreeBox(),
		"Resources": ast.PdfDict{"XObject": resources},
	}
	if !alloc.AllocAt(objNum, ast.NewStream(form, drawn.Bytes())) {
		return false, fmt.Errorf("%w: object %d", ErrImageNotAddressable, objNum)
	}
	return true, nil
}

// lossyTileName returns the resource name of the i-th image of a
// re-encoded photograph: U for the underlay, then T and the tile's index
// with its digits apart, so no name holds a run of three digits a removed
// number could match.
func lossyTileName(i int) ast.PdfName {
	if i == 0 {
		return "U"
	}
	var b bytes.Buffer
	b.WriteByte('T')
	for k, d := range strconv.Itoa(i - 1) {
		if k > 0 {
			b.WriteByte('.')
		}
		b.WriteRune(d)
	}
	return ast.PdfName(b.String())
}

// unitFractionDigits is how finely [unitFraction] rounds: a millionth of
// the unit square is a hundredth of a sample of a picture under 10,000
// samples a side, so a tile lands within that of its samples, against the
// [imagetiles.UnderlayBand] samples that part a JPEG tile from the samples a
// region reaches.
const unitFractionDigits = 1e6

// unitFraction returns n/d rounded to a [unitFractionDigits]th.
func unitFraction(n, d int) ast.PdfReal {
	return ast.PdfReal(math.Round(float64(n)/float64(d)*unitFractionDigits) / unitFractionDigits)
}

// clipFreeBox returns the /BBox of the forms [EncodeBlankedPhotographs]
// writes, [-1 -1 2 2]: the unit square their images paint, a unit square
// wider on every side. A form clips what it draws to its box (ISO 32000-1,
// 8.10.1), and a viewer antialiases the clip where an edge falls inside a
// device pixel, which it does not do to an image's edge, so the paper
// showed through along the picture's edges. The images paint nothing
// outside the unit square, so the wider box clips nothing they paint.
//
// # Corpus evidence
//
//   - pdfcpu pkg_testdata__BuildingWebappsWithGo, the centred quarter at
//     q 90: with the unit square as the box, poppler 26.09.0 drew the last
//     column of the page, a quarter pixel the picture fills, paler than the
//     lossless blanking does, 225 pixels at 72 dpi outside every JPEG tile;
//     with this box, none.
func clipFreeBox() ast.PdfArray {
	return ast.PdfArray{ast.PdfInt(-1), ast.PdfInt(-1), ast.PdfInt(2), ast.PdfInt(2)}
}

// jpegColorSpace reports whether cs, the colour space a blanked picture is
// written in, is one whose samples DCTDecode codes (ISO 32000-1, 7.4.8):
// DeviceGray, DeviceRGB, DeviceCMYK, or an ICCBased, CalGray or CalRGB space,
// whose component counts are those. A Separation or DeviceN space a blanked
// picture keeps names only the colorant None and paints nothing, and Lab and
// indexed pictures are written in sRGB already.
func jpegColorSpace(cs ast.PdfObject) bool {
	switch v := cs.(type) {
	case ast.PdfName:
		switch v {
		case "DeviceGray", "DeviceRGB", "DeviceCMYK":
			return true
		}
	case ast.PdfArray:
		if len(v) == 0 {
			return false
		}
		family, _ := v[0].(ast.PdfName)
		switch family {
		case "ICCBased", "CalGray", "CalRGB":
			return true
		}
	}
	return false
}
