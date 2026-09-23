package mutations

import (
	"context"
	"fmt"
	"image"
	"image/draw"
	"math"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	pdfimage "github.com/gotenberg/litho/pkg/internal/graphics/image"
	"github.com/gotenberg/litho/pkg/internal/heuristics"
)

// maskEntries are the image dictionary entries that say which samples are
// painted at all. Blanking writes a new dictionary that carries neither, and
// [pdfimage.DecodeImage] does not apply them, so an image carrying one is
// refused rather than returned with its masked-out picture made opaque
// (ISO 32000-1, Table 89).
var maskEntries = []ast.PdfName{"Mask", "ColorKeyMask"}

// blankedPicture and blankedStencil are the sample values a cleared pixel takes.
//
// A picture is cleared to opaque black, which replaces the samples that were
// there. A stencil has no colour of its own: ISO 32000-1, 8.9.6.2 paints its
// unmasked samples with the current fill colour, so the value that REMOVES its
// shape is the one leaving the pixel unpainted, which the decode reports as
// white. Clearing a stencil to black would set every covered sample to "paint
// me" and mark the page in whatever colour was in force, adding ink instead of
// taking the shape away.
var (
	blankedPicture = []byte{0, 0, 0}
	blankedStencil = []byte{255, 255, 255}
)

// blankImage overwrites the samples of one image that the zones cover and
// replaces the image's stream object with the result.
//
// It blanks rather than removes because an image is drawn by a single Do whose
// bytes carry the whole picture: removing the operator would take the parts of
// the image the caller never selected. Overwriting the covered samples clears
// exactly what was asked for and leaves the rest of the picture where it was.
//
// A picture's samples come back as 8-bit DeviceRGB under /FlateDecode whatever
// they were before, which is what lets a JPX or DCT image be cleared at all:
// there is no encoder for those filters, so the blanked image has to be written
// in a filter litho can produce. Any /SMask is carried over untouched, because a
// soft mask is a separate grayscale image that stays valid against the new
// samples and dropping it would change transparency outside the region.
//
// A stencil mask is rewritten as a stencil mask instead. It supplies a shape
// rather than a picture, so transcoding it to DeviceRGB would stop it taking the
// current fill colour and change what the page renders rather than only how its
// samples are stored. The replacement keeps /ImageMask at one bit a sample and
// carries no /Decode, because the decode already folded that array into the
// pixels; see [packStencilBits].
func blankImage(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	page *document.Page,
	box graphics.ImageBox,
	zones []heuristics.Zone,
) error {
	if box.Name == graphics.InlineImageName {
		return ErrInlineImageInRegion
	}

	ref, ok := imageXObjectRef(ctx, doc, page, box.Name)
	if !ok {
		return fmt.Errorf("%w: %q", ErrImageNotAddressable, box.Name)
	}

	stream, err := document.ResolveStream(ctx, doc, ref)
	if err != nil {
		return fmt.Errorf("resolving image %q: %w", box.Name, err)
	}

	// The error is discarded deliberately: an /ImageMask that cannot be resolved
	// is treated as absent, leaving the image on the picture path, which is the
	// behaviour that held before stencils were handled at all.
	maskFlag, _ := document.ResolveBool(ctx, doc, stream.Dict["ImageMask"])
	isStencil := bool(maskFlag)

	// A soft mask on a stencil has no defined meaning: ISO 32000-1, Table 89
	// gives /SMask a stream of alpha values describing a picture's samples, and
	// a mask has none to describe. Carrying it onto the one-bit replacement
	// would assert that meaning, and dropping it would change rendering for a
	// viewer that honoured it, so this refuses rather than choose. A picture
	// keeps its /SMask, which is the case the entry is defined for.
	if isStencil {
		if _, present := stream.Dict["SMask"]; present {
			return fmt.Errorf("%w: %q", ErrImageIsStencilMask, box.Name)
		}
	}
	for _, key := range maskEntries {
		if _, present := stream.Dict[key]; present {
			return fmt.Errorf("%w: %q carries /%s", ErrImageCarriesSampleEntries, box.Name, key)
		}
	}
	// /Decode is only a problem when the decode did not already fold it into
	// the pixels. Where it did, the blanked samples carry the mapping and the
	// replacement dictionary simply omits the entry, so the image renders as it
	// did before. Where it did not, the pixels still need it and dropping it
	// would change the picture outside the region as well as inside.
	if _, present := stream.Dict["Decode"]; present &&
		!pdfimage.AppliesDecodeArray(ctx, doc, stream.Dict) {
		return fmt.Errorf("%w: %q carries a /Decode the decode does not apply",
			ErrImageCarriesSampleEntries, box.Name)
	}

	img, err := pdfimage.DecodeImage(ctx, doc, page, ast.PdfName(box.Name))
	if err != nil {
		return fmt.Errorf("%w: %q: %w", ErrImageNotDecodable, box.Name, err)
	}

	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w <= 0 || h <= 0 {
		return fmt.Errorf("%w: %q has no samples", ErrImageNotDecodable, box.Name)
	}

	// Work on NRGBA for a predictable byte order, matching buildPNGXObject.
	canvas := image.NewNRGBA(bounds)
	draw.Draw(canvas, bounds, img, bounds.Min, draw.Src)

	fill := blankedPicture
	if isStencil {
		fill = blankedStencil
	}

	blanked, err := blankCoveredSamples(canvas, box, zones, fill)
	if err != nil {
		return fmt.Errorf("%q: %w", box.Name, err)
	}
	if blanked == 0 {
		// The region met the image and cleared none of it. That is not "nothing
		// to do": one sample can be stretched across a large area, so a region
		// inside that area covers picture the caller asked to clear while no
		// sample centre falls in it. Returning nil here would report success
		// over content still in the file.
		return fmt.Errorf("%w: %q", ErrImageRegionCoversNoSample, box.Name)
	}

	var (
		dict       ast.PdfDict
		compressed []byte
	)
	if isStencil {
		compressed, err = flateCompress(packStencilBits(canvas, w, h))
		if err != nil {
			return fmt.Errorf("compressing blanked image mask %q: %w", box.Name, err)
		}
		// No /ColorSpace: ISO 32000-1, Table 89 does not allow the entry on a
		// mask, which paints with the fill colour in force rather than one of
		// its own. No /Decode either, for the reason packStencilBits states.
		dict = ast.PdfDict{
			"Type":             ast.PdfName("XObject"),
			"Subtype":          ast.PdfName("Image"),
			"Width":            ast.PdfInt(w),
			"Height":           ast.PdfInt(h),
			"ImageMask":        ast.PdfBool(true),
			"BitsPerComponent": ast.PdfInt(1),
			"Filter":           ast.PdfName("FlateDecode"),
		}
	} else {
		rgb := make([]byte, 0, w*h*3)
		for y := range h {
			for x := range w {
				i := canvas.PixOffset(bounds.Min.X+x, bounds.Min.Y+y)
				rgb = append(rgb, canvas.Pix[i], canvas.Pix[i+1], canvas.Pix[i+2])
			}
		}
		compressed, err = flateCompress(rgb)
		if err != nil {
			return fmt.Errorf("compressing blanked image %q: %w", box.Name, err)
		}
		dict = ast.PdfDict{
			"Type":             ast.PdfName("XObject"),
			"Subtype":          ast.PdfName("Image"),
			"Width":            ast.PdfInt(w),
			"Height":           ast.PdfInt(h),
			"ColorSpace":       ast.PdfName("DeviceRGB"),
			"BitsPerComponent": ast.PdfInt(8),
			"Filter":           ast.PdfName("FlateDecode"),
		}
		// A soft mask is its own image and still describes these samples.
		if smask, present := stream.Dict["SMask"]; present {
			dict["SMask"] = smask
		}
	}

	if !alloc.AllocAt(ref.ObjectNum, ast.NewStream(dict, compressed)) {
		return fmt.Errorf("%w: %q", ErrImageNotAddressable, box.Name)
	}
	return nil
}

// packStencilBits re-packs the canvas as one-bit stencil samples, inverting the
// unpacking [pdfimage.DecodeImage] applied.
//
// Per ISO 32000-1, 8.9.3 samples are packed with the high-order bit first and
// each row padded to a byte boundary, which is the layout filter.BitSample
// reads. The decode folded any /Decode array into the pixels and reported a
// painted sample as 0 and an unpainted one as 255, so the replacement carries no
// /Decode and writes the default [0 1] meaning directly: a 0 bit paints, a 1 bit
// does not. A source mask whose /Decode was [1 0] therefore comes back the other
// way round, which is why the array must not be carried over.
func packStencilBits(canvas *image.NRGBA, w, h int) []byte {
	bounds := canvas.Bounds()
	rowBytes := (w + 7) / 8
	out := make([]byte, rowBytes*h)

	for y := range h {
		for x := range w {
			i := canvas.PixOffset(bounds.Min.X+x, bounds.Min.Y+y)
			// A one-bit sample decodes to exactly 0 or 255, so nothing lands in
			// between; the comparison rounds rather than truncates so a future
			// source of intermediate values degrades sensibly.
			if canvas.Pix[i] < 128 {
				continue
			}
			out[y*rowBytes+x/8] |= 0x80 >> (x % 8)
		}
	}
	return out
}

// blankCoveredSamples paints every sample whose centre falls inside a zone with
// fill, and reports how many it painted.
//
// fill is the caller's because what clears a sample depends on what the image
// is: opaque black replaces a picture's colour, while a stencil is cleared by
// leaving the sample unpainted. See [blankedPicture] and [blankedStencil].
//
// Each candidate sample is carried forward through the CTM and tested against
// the zones individually. Inverse-mapping the region and clearing its bounding
// box would be cheaper and wrong: under a rotated placement that box covers
// samples the region never touched, and blanking those destroys content outside
// what the caller asked to clear.
func blankCoveredSamples(canvas *image.NRGBA, box graphics.ImageBox, zones []heuristics.Zone, fill []byte) (int, error) {
	bounds := canvas.Bounds()
	w, h := bounds.Dx(), bounds.Dy()

	minPx, minPy, maxPx, maxPy, ok := candidateSamples(box, zones, w, h)
	if !ok {
		return 0, ErrImagePlacementDegenerate
	}

	blanked := 0
	for py := minPy; py <= maxPy; py++ {
		for px := minPx; px <= maxPx; px++ {
			// The image's top row is the top of the unit square, so v runs the
			// other way from the row index (ISO 32000-1, 8.9.5.2).
			u := (float64(px) + 0.5) / float64(w)
			v := 1 - (float64(py)+0.5)/float64(h)

			x := box.CTM[0]*u + box.CTM[2]*v + box.CTM[4]
			y := box.CTM[1]*u + box.CTM[3]*v + box.CTM[5]

			point := heuristics.ZoneFromRect([4]float64{x, y, x, y})
			covered := false
			for _, z := range zones {
				if heuristics.ZonesOverlap(point, z) {
					covered = true
					break
				}
			}
			if !covered {
				continue
			}

			i := canvas.PixOffset(bounds.Min.X+px, bounds.Min.Y+py)
			copy(canvas.Pix[i:i+3], fill)
			blanked++
		}
	}
	return blanked, nil
}

// candidateSamples narrows the search to the samples the zones can reach, by
// carrying each zone's corners back into image space. The result is inclusive
// and clamped to the image, and is only a bound: every sample inside it is
// still tested individually.
func candidateSamples(box graphics.ImageBox, zones []heuristics.Zone, w, h int) (minPx, minPy, maxPx, maxPy int, ok bool) {
	minU, minV := math.Inf(1), math.Inf(1)
	maxU, maxV := math.Inf(-1), math.Inf(-1)

	for _, z := range zones {
		zMinX, zMinY, zMaxX, zMaxY := heuristics.ZoneBounds(z)
		for _, corner := range [4][2]float64{
			{zMinX, zMinY}, {zMaxX, zMinY}, {zMinX, zMaxY}, {zMaxX, zMaxY},
		} {
			u, v, invertible := box.ImageSpace(corner[0], corner[1])
			if !invertible {
				return 0, 0, 0, 0, false
			}
			minU, maxU = math.Min(minU, u), math.Max(maxU, u)
			minV, maxV = math.Min(minV, v), math.Max(maxV, v)
		}
	}
	if math.IsInf(minU, 1) {
		return 0, 0, 0, 0, false
	}

	minPx = clampSample(int(math.Floor(minU*float64(w)-0.5)), w)
	maxPx = clampSample(int(math.Ceil(maxU*float64(w)-0.5)), w)
	// v runs opposite to the row index, so the largest v gives the first row.
	minPy = clampSample(int(math.Floor((1-maxV)*float64(h)-0.5)), h)
	maxPy = clampSample(int(math.Ceil((1-minV)*float64(h)-0.5)), h)
	return minPx, minPy, maxPx, maxPy, true
}

// clampSample holds an index inside [0, size).
func clampSample(i, size int) int {
	if i < 0 {
		return 0
	}
	if i > size-1 {
		return size - 1
	}
	return i
}

// imageXObjectRef returns the indirect reference the page's resources use for
// the named image, which is the object the replacement has to take the place
// of. An image written directly into the resource dictionary has none.
func imageXObjectRef(ctx context.Context, doc *document.Document, page *document.Page, name string) (ast.PdfIndirectRef, bool) {
	xobjObj, present := page.Resources["XObject"]
	if !present {
		return ast.PdfIndirectRef{}, false
	}
	xobjDict, err := document.ResolveDict(ctx, doc, xobjObj)
	if err != nil || xobjDict == nil {
		return ast.PdfIndirectRef{}, false
	}
	ref, isRef := xobjDict[ast.PdfName(name)].(ast.PdfIndirectRef)
	if !isRef || ref.ObjectNum == 0 {
		return ast.PdfIndirectRef{}, false
	}
	return ref, true
}
