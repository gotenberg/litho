package mutations

import (
	"context"
	"fmt"
	"image"
	"image/draw"
	"maps"
	"math"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/icc"
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
// A picture's samples come back at 8 bits under /FlateDecode whatever filter
// they were in, which is what lets a JPX or DCT image be cleared at all: there
// is no encoder for those filters, so the blanked image has to be written in a
// filter litho can produce. A picture in DeviceGray, DeviceRGB, DeviceCMYK, an
// ICCBased, CalGray or CalRGB space keeps that space (see [blankNative]), and
// so does one in a Separation or DeviceN space naming only the colorant None,
// which paints nothing (ISO 32000-1, 8.6.6.4). Any other picture, indexed,
// Separation, DeviceN or Lab, or a JPX image taking its colour from its own
// data, is written in sRGB, the space the decode converts its pixels to, and
// states it by litho's sRGB profile ([icc.SRGBProfile]). See [srgbColorSpace]
// for why not DeviceRGB. A /SMask is replaced by a copy whose covered samples
// are opaque (see [blankSoftMask]): the mask holds the picture's shape, and
// dropping it would change transparency outside the region. The soft mask a
// JPX picture carries in its data becomes such an /SMask (see
// [blankDataSoftMask]).
//
// A stencil mask is rewritten as a stencil mask instead. It supplies a shape
// rather than a picture, so transcoding it to DeviceRGB would stop it taking the
// current fill colour and change what the page renders rather than only how its
// samples are stored. The replacement keeps /ImageMask at one bit a sample and
// carries no /Decode, because the decode already folded that array into the
// pixels; see [packStencilBits].
//
// With private set, the result goes to a new object rather than over the
// image, which stays as it was for the placements that still paint it; see
// [blankPrivateCopies], which repoints the covered Do at it. It returns the
// number of the object written.
func blankImage(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	page *document.Page,
	box graphics.ImageBox,
	zones []heuristics.Zone,
	private bool,
) (int, error) {
	if box.Name == graphics.InlineImageName {
		return 0, ErrInlineImageInRegion
	}

	ref, ok := imageXObjectRef(ctx, doc, page, box)
	if !ok {
		return 0, fmt.Errorf("%w: %q", ErrImageNotAddressable, box.Name)
	}
	dest := ref.ObjectNum
	if private {
		// Reserved now and written last: the soft mask and the colour space
		// the replacement names are allocated in between.
		dest = alloc.Alloc(ast.PdfNull{}).ObjectNum
	}

	// Alloc-first: an image an earlier placement or an earlier Redact already
	// blanked is queued there, and blanking the source's copy instead would
	// write the original samples back over that work. Two regions clearing two
	// placements of one image kept only the second.
	stream, err := latestImageStream(ctx, alloc, doc, ref)
	if err != nil {
		return 0, fmt.Errorf("resolving image %q: %w", box.Name, err)
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
			return 0, fmt.Errorf("%w: %q", ErrImageIsStencilMask, box.Name)
		}
	}
	for _, key := range maskEntries {
		if _, present := stream.Dict[key]; present {
			return 0, fmt.Errorf("%w: %q carries %s", ErrImageCarriesSampleEntries, box.Name, key)
		}
	}
	// /Decode is only a problem when the decode did not already fold it into
	// the pixels. Where it did, the blanked samples carry the mapping and the
	// replacement dictionary simply omits the entry, so the image renders as it
	// did before. Where it did not, the pixels still need it and dropping it
	// would change the picture outside the region as well as inside.
	if _, present := stream.Dict["Decode"]; present &&
		!pdfimage.AppliesDecodeArray(ctx, doc, stream.Dict) {
		return 0, fmt.Errorf("%w: %q carries a /Decode the decode does not apply",
			ErrImageCarriesSampleEntries, box.Name)
	}

	// Decoding reads the image as the file holds it, its own soft mask
	// included; the replacements below copy /SMask from stream.
	source := stream
	var dataMask *pdfimage.DataSoftMask
	if !isStencil {
		smask, err := blankSoftMask(ctx, alloc, doc, page, box, zones, stream.Dict)
		if err != nil {
			return 0, err
		}
		if smask == nil {
			if smask, dataMask, err = blankDataSoftMask(ctx, alloc, doc, box, zones, stream); err != nil {
				return 0, err
			}
		}
		if smask != nil {
			// A copy: the source's dictionary is not edited.
			stream.Dict = maps.Clone(stream.Dict)
			stream.Dict["SMask"] = smask
		}
		native, ok, err := pdfimage.DecodeStreamNative(ctx, doc, page, ast.PdfName(box.Name), source)
		if err != nil {
			return 0, fmt.Errorf("%w: %q: %w", ErrImageNotDecodable, box.Name, err)
		}
		if ok {
			if dataMask != nil && dataMask.Premultiplied {
				// The explicit /SMask composites straight colour (11.6.5.3).
				pdfimage.Unpremultiply(native.Data, dataMask.Alpha, native.Width*native.Height, native.Components)
			}
			return dest, blankNative(alloc, dest, stream, box, zones, native)
		}
	}

	// The samples as stored: the replacement keeps the image's soft mask, so a
	// /Matte on it still describes them. A picture whose data held its soft
	// mask is read as it paints, its colour straight, and its opacity is the
	// replacement's /SMask.
	var img image.Image
	if dataMask != nil {
		img, err = pdfimage.DecodePainted(ctx, doc, ast.PdfName(box.Name), source, page.Resources, page.Dict)
	} else {
		img, err = pdfimage.DecodeStreamSamples(ctx, doc, page, ast.PdfName(box.Name), source)
	}
	if err != nil {
		return 0, fmt.Errorf("%w: %q: %w", ErrImageNotDecodable, box.Name, err)
	}

	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w <= 0 || h <= 0 {
		return 0, fmt.Errorf("%w: %q has no samples", ErrImageNotDecodable, box.Name)
	}

	// Work on NRGBA for a predictable byte order, matching buildPNGXObject.
	canvas := image.NewNRGBA(bounds)
	draw.Draw(canvas, bounds, img, bounds.Min, draw.Src)
	if dataMask != nil {
		for i := 3; i < len(canvas.Pix); i += 4 {
			canvas.Pix[i] = 0xff
		}
	}

	// The RGB replacement is opaque, so a picture whose pixels are not
	// would start painting where it painted nothing: an image in a space over
	// the colorant None decodes fully transparent (ISO 32000-1, 8.6.6.4), and
	// rewritten as RGB it would cover its whole placement in black.
	if !isStencil && !canvas.Opaque() {
		return 0, fmt.Errorf("%w: %q does not paint every sample it holds", ErrImageNotDecodable, box.Name)
	}

	fill := blankedPicture
	if isStencil {
		fill = blankedStencil
	}

	blanked, err := blankCoveredSamples(canvas, box, zones, fill)
	if err != nil {
		return 0, fmt.Errorf("%q: %w", box.Name, err)
	}
	if blanked == 0 {
		// The region met the image and cleared none of it. That is not "nothing
		// to do": one sample can be stretched across a large area, so a region
		// inside that area covers picture the caller asked to clear while no
		// sample centre falls in it. Returning nil here would report success
		// over content still in the file.
		return 0, fmt.Errorf("%w: %q", ErrImageRegionCoversNoSample, box.Name)
	}

	var (
		dict       ast.PdfDict
		compressed []byte
	)
	if isStencil {
		compressed, err = flateCompress(packStencilBits(canvas, w, h))
		if err != nil {
			return 0, fmt.Errorf("compressing blanked image mask %q: %w", box.Name, err)
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
			return 0, fmt.Errorf("compressing blanked image %q: %w", box.Name, err)
		}
		cs, err := srgbColorSpace(alloc)
		if err != nil {
			return 0, fmt.Errorf("blanked image %q: %w", box.Name, err)
		}
		dict = ast.PdfDict{
			"Type":             ast.PdfName("XObject"),
			"Subtype":          ast.PdfName("Image"),
			"Width":            ast.PdfInt(w),
			"Height":           ast.PdfInt(h),
			"ColorSpace":       cs,
			"BitsPerComponent": ast.PdfInt(8),
			"Filter":           ast.PdfName("FlateDecode"),
		}
		// A soft mask is its own image and still describes these samples.
		if smask, present := stream.Dict["SMask"]; present {
			dict["SMask"] = smask
		}
	}

	if !alloc.AllocAt(dest, ast.NewStream(dict, compressed)) {
		return 0, fmt.Errorf("%w: %q", ErrImageNotAddressable, box.Name)
	}
	return dest, nil
}

// blankSoftMask returns a replacement for the /SMask of the picture dict
// describes, its samples the zones cover made opaque, or nil when the picture
// has none.
//
// A soft mask is the picture's shape (ISO 32000-1, 11.6.5.3): carried over as
// it was, it went on drawing the outline of what the blanked samples showed,
// the black of the blank painted where the picture was opaque and the page
// showing through where it was not. The litho text of a cover set as an
// image with alpha (pdfcpu WaldenFull.pdf) stayed readable after its page
// was redacted whole. A covered sample made opaque paints the blank whole.
// The mask is written anew rather than edited in place, as another picture
// may share it; its /Matte stays, since an opaque sample needs no un-matting.
func blankSoftMask(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	page *document.Page,
	box graphics.ImageBox,
	zones []heuristics.Zone,
	dict ast.PdfDict,
) (ast.PdfObject, error) {
	obj, present := dict["SMask"]
	if !present {
		return nil, nil
	}
	resolved, err := resolveAllocFirst(ctx, alloc, doc, obj)
	if err != nil {
		return nil, fmt.Errorf("%w: %q: soft mask: %w", ErrImageNotDecodable, box.Name, err)
	}
	mask, ok := resolved.(ast.PdfStream)
	if !ok {
		// /SMask /None, or anything that is no stream, masks nothing.
		return nil, nil
	}
	if _, present := mask.Dict["Decode"]; present && !pdfimage.AppliesDecodeArray(ctx, doc, mask.Dict) {
		return nil, fmt.Errorf("%w: %q: its soft mask carries a /Decode the decode does not apply",
			ErrImageCarriesSampleEntries, box.Name)
	}
	img, err := pdfimage.DecodeStreamSamples(ctx, doc, page, ast.PdfName(box.Name+" /SMask"), mask)
	if err != nil {
		return nil, fmt.Errorf("%w: %q: soft mask: %w", ErrImageNotDecodable, box.Name, err)
	}
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("%w: %q: its soft mask has no samples", ErrImageNotDecodable, box.Name)
	}
	gray := image.NewGray(image.Rect(0, 0, w, h))
	draw.Draw(gray, gray.Bounds(), img, bounds.Min, draw.Src)
	return blankedMaskStream(alloc, box, zones, gray, mask.Dict["Matte"])
}

// blankedMaskStream allocates the soft mask gray, whose origin is (0, 0),
// with the samples the zones cover made opaque (see [blankSoftMask]), and
// returns its reference. matte, when not nil, is the /Matte it keeps.
func blankedMaskStream(alloc *writer.ObjAllocator, box graphics.ImageBox, zones []heuristics.Zone, gray *image.Gray, matte ast.PdfObject) (ast.PdfObject, error) {
	w, h := gray.Bounds().Dx(), gray.Bounds().Dy()
	// The mask maps onto the unit square as the picture does, whatever its
	// own resolution (11.6.5.3), so the same sample test applies.
	if _, err := forCoveredSamples(box, zones, w, h, func(px, py int) {
		gray.Pix[gray.PixOffset(px, py)] = 0xff
	}); err != nil {
		return nil, fmt.Errorf("%q: soft mask: %w", box.Name, err)
	}
	samples := make([]byte, 0, w*h)
	for y := range h {
		i := gray.PixOffset(0, y)
		samples = append(samples, gray.Pix[i:i+w]...)
	}
	compressed, err := flateCompress(samples)
	if err != nil {
		return nil, fmt.Errorf("compressing the blanked soft mask of %q: %w", box.Name, err)
	}
	out := ast.PdfDict{
		"Type":             ast.PdfName("XObject"),
		"Subtype":          ast.PdfName("Image"),
		"Width":            ast.PdfInt(w),
		"Height":           ast.PdfInt(h),
		"ColorSpace":       ast.PdfName("DeviceGray"),
		"BitsPerComponent": ast.PdfInt(8),
		"Filter":           ast.PdfName("FlateDecode"),
	}
	if matte != nil {
		out["Matte"] = matte
	}
	return alloc.Alloc(ast.NewStream(out, compressed)), nil
}

// blankDataSoftMask returns the explicit replacement of the soft mask a
// JPXDecode picture carries in its data (ISO 32000-1, Table 89 /SMaskInData),
// its samples the zones cover made opaque as [blankSoftMask] makes them, and
// that mask; both nil when the picture carries none or names an /SMask, which
// takes its place. The replacement image is no JPXDecode image, for which
// alone Table 89 defines /SMaskInData, so the data's mask becomes the /SMask
// Table 89 lets a reader create from it. Dropping it painted the picture
// opaque around the region, the colour its data held where it was
// transparent.
//
// The replacement is nil, beside the mask, when blanking leaves the mask
// opaque everywhere, as a region covering the whole picture does: it then
// paints as no mask does. Where some opacity survives, the /SMask is kept
// although ISO 19005-1, 6.4 forbids one and does not name /SMaskInData: the
// redaction's promise is that nothing outside the region changes, and the
// source already paints that transparency, which only an /SMask carries
// outside JPEG 2000 data. A PDF/A-1 remediation flattens it afterwards.
//
// # Corpus evidence
//
//   - litho smaskindata_2b: a whole-page redaction wrote an all-opaque
//     /SMask on both images, adding 6.4 test 2 at PDF/A-1a and 1b in the
//     mutate-redact cell; none is written now.
func blankDataSoftMask(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	box graphics.ImageBox,
	zones []heuristics.Zone,
	stream ast.PdfStream,
) (ast.PdfObject, *pdfimage.DataSoftMask, error) {
	if _, present := stream.Dict["SMask"]; present {
		return nil, nil, nil
	}
	w, errW := document.ResolveInt(ctx, doc, stream.Dict["Width"])
	h, errH := document.ResolveInt(ctx, doc, stream.Dict["Height"])
	if errW != nil || errH != nil || w <= 0 || h <= 0 || w > math.MaxInt32 || h > math.MaxInt32 {
		return nil, nil, fmt.Errorf("%w: %q: no image size for the soft mask of its data", ErrImageNotDecodable, box.Name)
	}
	m, err := pdfimage.ReadSMaskInData(ctx, doc, stream, int(w), int(h))
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %q: %w", ErrImageNotDecodable, box.Name, err)
	}
	if m == nil {
		return nil, nil, nil
	}
	// Premultiplication is undone on the data's values, before /Decode
	// maps them; a /Decode would have to be undone first.
	if _, present := stream.Dict["Decode"]; present && m.Premultiplied {
		return nil, nil, fmt.Errorf("%w: %q carries a /Decode over colour premultiplied by its opacity",
			ErrImageCarriesSampleEntries, box.Name)
	}
	gray := &image.Gray{Pix: slices.Clone(m.Alpha), Stride: int(w), Rect: image.Rect(0, 0, int(w), int(h))}
	if _, err := forCoveredSamples(box, zones, int(w), int(h), func(px, py int) {
		gray.Pix[gray.PixOffset(px, py)] = 0xff
	}); err != nil {
		return nil, nil, fmt.Errorf("%q: soft mask: %w", box.Name, err)
	}
	if !slices.ContainsFunc(gray.Pix, func(a byte) bool { return a != 0xff }) {
		// A mask opaque everywhere paints as no mask does (ISO 32000-1,
		// 11.6.5.3: the shape multiplies by 1), so none is written. An
		// /SMask would add the ISO 19005-1, 6.4 test 2 violation the
		// source, whose /SMaskInData that clause does not name, lacks.
		return nil, m, nil
	}
	ref, err := blankedMaskStream(alloc, box, zones, gray, nil)
	if err != nil {
		return nil, nil, err
	}
	return ref, m, nil
}

// srgbColorSpace allocates litho's sRGB profile as an ICC profile stream and
// returns the [/ICCBased ref] colour space naming it, for a picture rewritten
// in the sRGB its decode produced.
//
// The decode turns every colour space it reads into sRGB pixels
// (IEC 61966-2-1), so an ICC profile of sRGB states exactly the colour the
// replacement holds. DeviceRGB stated the same numbers with no colour of their
// own, which a conforming PDF/A file may use only under a DefaultRGB space or
// an RGB output intent (ISO 19005-1, 6.2.3.3; ISO 19005-2, 6.2.4.3 test 2;
// ISO 19005-4, 6.2.4.3), so redacting an image the input stated in CIELab,
// ROMM-RGB, an indexed or a tinted space wrote a violation the input did not
// have. An ICCBased space is device independent under every part (ISO 19005-1,
// 6.2.3.2; ISO 19005-2, 6.2.4.2), and the profile is the version 2.1 display
// profile remediation writes for the same purpose.
//
// # Corpus evidence
//
//   - litho imagecolour_2b: redacting page 1 rewrote its four JPX images,
//     which state no /ColorSpace, as DeviceRGB, adding 6.2.3.3 test 1 at
//     PDF/A-1b and 6.2.4.3 test 2 at PDF/A-2a, 2b, 2u, 3b and 4.
func srgbColorSpace(alloc *writer.ObjAllocator) (ast.PdfArray, error) {
	profile, err := flateCompress(icc.SRGBProfile())
	if err != nil {
		return nil, fmt.Errorf("compressing the sRGB profile: %w", err)
	}
	ref := alloc.Alloc(ast.NewStream(ast.PdfDict{
		"N":      ast.PdfInt(3),
		"Filter": ast.PdfName("FlateDecode"),
	}, profile))
	return ast.PdfArray{ast.PdfName("ICCBased"), ref}, nil
}

// blankNative clears the covered samples of a picture in its own colour space
// and writes it back under the same /ColorSpace, so every sample the region
// does not cover keeps its exact value. Going through RGB instead converted
// every CMYK, ICC-based and CIE-based picture, and baked that reading of its
// colour into the file: a redacted CMYK photograph changed colour outside the
// region as well as inside it. A cleared sample is black in the space: 0 on
// every component, or full black ink with no colour ink for four components.
func blankNative(
	alloc *writer.ObjAllocator,
	dest int,
	stream ast.PdfStream,
	box graphics.ImageBox,
	zones []heuristics.Zone,
	native pdfimage.NativeSamples,
) error {
	n := native.Components
	black := make([]byte, n)
	if n == 4 {
		black[3] = 255
	}
	blanked, err := forCoveredSamples(box, zones, native.Width, native.Height, func(px, py int) {
		i := (py*native.Width + px) * n
		copy(native.Data[i:i+n], black)
	})
	if err != nil {
		return fmt.Errorf("%q: %w", box.Name, err)
	}
	if blanked == 0 {
		return fmt.Errorf("%w: %q", ErrImageRegionCoversNoSample, box.Name)
	}
	compressed, err := flateCompress(native.Data)
	if err != nil {
		return fmt.Errorf("compressing blanked image %q: %w", box.Name, err)
	}
	// No /Decode: the samples carry the array's mapping already.
	dict := ast.PdfDict{
		"Type":             ast.PdfName("XObject"),
		"Subtype":          ast.PdfName("Image"),
		"Width":            ast.PdfInt(native.Width),
		"Height":           ast.PdfInt(native.Height),
		"ColorSpace":       stream.Dict["ColorSpace"],
		"BitsPerComponent": ast.PdfInt(8),
		"Filter":           ast.PdfName("FlateDecode"),
	}
	if smask, present := stream.Dict["SMask"]; present {
		dict["SMask"] = smask
	}
	if !alloc.AllocAt(dest, ast.NewStream(dict, compressed)) {
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
	return forCoveredSamples(box, zones, bounds.Dx(), bounds.Dy(), func(px, py int) {
		i := canvas.PixOffset(bounds.Min.X+px, bounds.Min.Y+py)
		copy(canvas.Pix[i:i+3], fill)
	})
}

// forCoveredSamples calls clear for every sample of a w x h image whose centre
// falls inside a zone, and reports how many it found.
func forCoveredSamples(box graphics.ImageBox, zones []heuristics.Zone, w, h int, clear func(px, py int)) (int, error) {
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

			clear(px, py)
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

// latestImageStream resolves an image XObject, preferring the version alloc
// has queued at its object number.
func latestImageStream(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, ref ast.PdfIndirectRef) (ast.PdfStream, error) {
	obj, err := resolveAllocFirst(ctx, alloc, doc, ref)
	if err != nil {
		return ast.PdfStream{}, err
	}
	stream, ok := obj.(ast.PdfStream)
	if !ok {
		return ast.PdfStream{}, fmt.Errorf("object %d is %T, not a stream", ref.ObjectNum, obj)
	}
	return stream, nil
}

// imageXObjectRef returns the indirect reference of the image box paints,
// which is the object the replacement has to take the place of. An image
// written directly into the resource dictionary has none.
//
// The machine names the object it resolved the Do to ([graphics.ImageBox]
// ObjNum), in the resources of the stream that drew it: a form XObject's own
// for an image a form draws. Its name alone says nothing outside those
// resources, and read in the page's it named another image, or none: an
// image a form drew under the name the page gives its own picture blanked the
// page's picture where the form's lay, and left the form's image whole inside
// the region. The name is read in the page's resources only when the machine
// resolved no object.
//
// # Corpus evidence
//
//   - pdfcpu VectorApple, the centred quarter of page 1 with the raster
//     fallback: two forms drew images named /Im0 inside the region; both
//     blankings rewrote the page's own /Im0, a shadow below the region, which
//     turned black, and the forms' images kept their samples.
func imageXObjectRef(ctx context.Context, doc *document.Document, page *document.Page, box graphics.ImageBox) (ast.PdfIndirectRef, bool) {
	if box.ObjNum != 0 {
		return ast.PdfIndirectRef{ObjectNum: box.ObjNum}, true
	}
	if box.FormDepth > 0 {
		return ast.PdfIndirectRef{}, false
	}
	name := box.Name
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
