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

// sampleEncodingEntries are the image dictionary entries that describe how the
// current samples are to be read. Blanking re-encodes the samples, so an image
// carrying one of these is refused rather than silently re-interpreted against
// an encoding it no longer has (ISO 32000-1, Table 89).
var sampleEncodingEntries = []ast.PdfName{"Decode", "Mask", "ColorKeyMask"}

// blankImage overwrites the samples of one image that the zones cover and
// replaces the image's stream object with the result.
//
// It blanks rather than removes because an image is drawn by a single Do whose
// bytes carry the whole picture: removing the operator would take the parts of
// the image the caller never selected. Overwriting the covered samples clears
// exactly what was asked for and leaves the rest of the picture where it was.
//
// The samples come back as 8-bit DeviceRGB under /FlateDecode whatever they
// were before, which is what lets a JPX or DCT image be cleared at all: there
// is no encoder for those filters, so the blanked image has to be written in a
// filter litho can produce. Any /SMask is carried over untouched, because a
// soft mask is a separate grayscale image that stays valid against the new
// samples and dropping it would change transparency outside the region.
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
	for _, key := range sampleEncodingEntries {
		if _, present := stream.Dict[key]; present {
			return fmt.Errorf("%w: %q carries /%s", ErrImageCarriesSampleEntries, box.Name, key)
		}
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

	blanked, err := blankCoveredSamples(canvas, box, zones)
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

	rgb := make([]byte, 0, w*h*3)
	for y := range h {
		for x := range w {
			i := canvas.PixOffset(bounds.Min.X+x, bounds.Min.Y+y)
			rgb = append(rgb, canvas.Pix[i], canvas.Pix[i+1], canvas.Pix[i+2])
		}
	}
	compressed, err := flateCompress(rgb)
	if err != nil {
		return fmt.Errorf("compressing blanked image %q: %w", box.Name, err)
	}

	dict := ast.PdfDict{
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

	if !alloc.AllocAt(ref.ObjectNum, ast.NewStream(dict, compressed)) {
		return fmt.Errorf("%w: %q", ErrImageNotAddressable, box.Name)
	}
	return nil
}

// blankCoveredSamples paints every sample whose centre falls inside a zone
// opaque black, and reports how many it painted.
//
// Each candidate sample is carried forward through the CTM and tested against
// the zones individually. Inverse-mapping the region and clearing its bounding
// box would be cheaper and wrong: under a rotated placement that box covers
// samples the region never touched, and blanking those destroys content outside
// what the caller asked to clear.
func blankCoveredSamples(canvas *image.NRGBA, box graphics.ImageBox, zones []heuristics.Zone) (int, error) {
	bounds := canvas.Bounds()
	w, h := bounds.Dx(), bounds.Dy()

	minPx, minPy, maxPx, maxPy, ok := candidateSamples(box, zones, w, h)
	if !ok {
		return 0, ErrImagePlacementDegenerate
	}

	black := []byte{0, 0, 0}
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
			copy(canvas.Pix[i:i+3], black)
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
