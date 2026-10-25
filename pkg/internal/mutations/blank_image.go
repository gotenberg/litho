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

// keptImageEntries are the entries of an image dictionary (ISO 32000-1,
// Table 89; ISO 32000-2, Table 87) that stay true of the image once
// [blankImage] has cleared some of its samples, and that its replacement
// therefore carries over. They describe where and how the image paints, not
// what its samples show:
//
//   - /OC puts the image in an optional content group or membership (8.11.2):
//     dropping it made a hidden image visible, its blank included.
//   - /Interpolate (8.9.5.3) and /Intent (8.6.5.8) say how the samples
//     render, and the samples the region did not reach render as before only
//     under the same smoothing and the same rendering intent.
//   - /Name (Table 89, PDF 1.0) is the resource name the image was known by.
//   - /Measure (ISO 32000-2, 12.10) is the coordinate system of the image's
//     placement, which blanking leaves where it was.
//
// /StructParent (14.7.4.4) is carried apart, see [carryImageEntries]. Every
// other entry describes the picture as it was, and blanking drops it
// deliberately: /Alternates are other versions of the same picture (8.9.5.4),
// /OPI names its high-resolution original (14.11.7), /ID is the digital
// identifier of the resource it was captured from (14.10.6), /Metadata may
// describe or carry a thumbnail of it (14.3.2), /AF associates files with it
// (ISO 32000-2, 14.13), and /PtData holds point data read off it (ISO
// 32000-2, 12.10.4). Each would keep in the file what the region removed.
// /SMask, /Decode, /Mask and /SMaskInData are handled where the samples are.
var keptImageEntries = []ast.PdfName{"OC", "Interpolate", "Intent", "Name", "Measure"}

// carryImageEntries copies onto dst, the dictionary of a blanked image's
// replacement, the entries of src, the dictionary it replaces, that
// [keptImageEntries] lists, and /StructParent unless private is set.
//
// /StructParent is the image's key in the structure parent tree, whose value
// is the one structure element the image belongs to, and that element's
// object reference names the image by its object number (ISO 32000-1,
// 14.7.4.3 and 14.7.4.4). A replacement written over the image keeps the
// object number, so the key and the reference still name it. A private copy
// (see [blankPrivateCopies]) is a new object no object reference names, and a
// parent tree key maps one object to its parent: carrying the key would make
// two objects claim an entry whose element names only the original, which
// still paints at the placements the regions did not cover. The copy carries
// no key.
func carryImageEntries(dst, src ast.PdfDict, private bool) {
	for _, key := range keptImageEntries {
		if v, present := src[key]; present {
			dst[key] = v
		}
	}
	if v, present := src["StructParent"]; present && !private {
		dst["StructParent"] = v
	}
}

// placementLeftAsIs runs the checks [blankImage] makes on the image stream a
// placement paints before it writes anything, and reports whether the image
// is a stencil mask. It returns [errSamplesAlreadyBlank] when the zones meet
// the placement over samples that already hold the blank only
// ([touchedSamplesAlreadyBlank]), and the refusal when the image cannot be
// blanked.
//
// It is the one rule saying which placement a region meets the redaction
// leaves painting the image as it was: [CopiedImagesPaintedInRegions] reads
// a placement of an image blanked in a copy by it, so the check a writer runs
// before writing agrees with the redaction it checks.
func placementLeftAsIs(
	ctx context.Context,
	doc *document.Document,
	page *document.Page,
	box graphics.ImageBox,
	zones []heuristics.Zone,
	stream ast.PdfStream,
) (bool, error) {
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
			return false, fmt.Errorf("%w: %q", ErrImageIsStencilMask, box.Name)
		}
	}
	for _, key := range maskEntries {
		if _, present := stream.Dict[key]; present {
			return false, fmt.Errorf("%w: %q carries %s", ErrImageCarriesSampleEntries, box.Name, key)
		}
	}
	// /Decode is only a problem when the decode did not already fold it into
	// the pixels. Where it did, the blanked samples carry the mapping and the
	// replacement dictionary simply omits the entry, so the image renders as it
	// did before. Where it did not, the pixels still need it and dropping it
	// would change the picture outside the region as well as inside.
	if _, present := stream.Dict["Decode"]; present &&
		!pdfimage.AppliesDecodeArray(ctx, doc, stream.Dict) {
		return false, fmt.Errorf("%w: %q carries a /Decode the decode does not apply",
			ErrImageCarriesSampleEntries, box.Name)
	}

	alreadyBlank, err := touchedSamplesAlreadyBlank(ctx, doc, page, box, zones, stream, isStencil)
	if err != nil {
		return false, err
	}
	if alreadyBlank {
		return false, errSamplesAlreadyBlank
	}
	return isStencil, nil
}

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
// filter litho can produce. The samples are written losslessly, through the
// PNG predictors where that is smaller (see [encodeBlankedSamples]). A picture in DeviceGray, DeviceRGB, DeviceCMYK, an
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
// Every replacement keeps the entries that stay true of the blanked image,
// its optional content membership, rendering keys and structure parent key
// among them ([carryImageEntries]).
//
// With private set, the result goes to a new object rather than over the
// image, which stays as it was for the placements that still paint it; see
// [blankPrivateCopies], which repoints the covered Do at it. It returns the
// number of the object written and, for a picture the source held as JPEG or
// JPEG 2000, what [EncodeBlankedPhotographs] needs to re-encode it as one,
// nil for any other image.
func blankImage(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	page *document.Page,
	box graphics.ImageBox,
	zones []heuristics.Zone,
	private bool,
) (int, *BlankedPhotograph, error) {
	if box.Name == graphics.InlineImageName {
		return 0, nil, ErrInlineImageInRegion
	}

	ref, ok := imageXObjectRef(ctx, doc, page, box)
	if !ok {
		return 0, nil, fmt.Errorf("%w: %q", ErrImageNotAddressable, box.Name)
	}
	// Alloc-first: an image an earlier placement or an earlier Redact already
	// blanked is queued there, and blanking the source's copy instead would
	// write the original samples back over that work. Two regions clearing two
	// placements of one image kept only the second.
	stream, err := latestImageStream(ctx, alloc, doc, ref)
	if err != nil {
		return 0, nil, fmt.Errorf("resolving image %q: %w", box.Name, err)
	}

	// Before anything is allocated, so an image left as it is leaves nothing
	// behind in the file.
	isStencil, err := placementLeftAsIs(ctx, doc, page, box, zones, stream)
	if err != nil {
		return 0, nil, err
	}

	dest := ref.ObjectNum
	if private {
		// Reserved now and written last: the soft mask and the colour space
		// the replacement names are allocated in between.
		dest = alloc.Alloc(ast.PdfNull{}).ObjectNum
	}

	// Decoding reads the image as the file holds it, its own soft mask
	// included; the replacements below copy /SMask from stream.
	source := stream
	var dataMask *pdfimage.DataSoftMask
	if !isStencil {
		smask, err := blankSoftMask(ctx, alloc, doc, page, box, zones, stream.Dict)
		if err != nil {
			return 0, nil, err
		}
		if smask == nil {
			if smask, dataMask, err = blankDataSoftMask(ctx, alloc, doc, box, zones, stream); err != nil {
				return 0, nil, err
			}
		}
		if smask != nil {
			// A copy: the source's dictionary is not edited.
			stream.Dict = maps.Clone(stream.Dict)
			stream.Dict["SMask"] = smask
		}
		native, ok, err := pdfimage.DecodeStreamNative(ctx, doc, page, ast.PdfName(box.Name), source)
		if err != nil {
			return 0, nil, fmt.Errorf("%w: %q: %w", ErrImageNotDecodable, box.Name, err)
		}
		if ok {
			if dataMask != nil && dataMask.Premultiplied {
				// The explicit /SMask composites straight colour (11.6.5.3).
				pdfimage.Unpremultiply(native.Data, dataMask.Alpha, native.Width*native.Height, native.Components)
			}
			if err := blankNative(ctx, alloc, doc, page, dest, stream, box, zones, native, private); err != nil {
				return 0, nil, err
			}
			return dest, blankedPhotograph(source.Dict, box, zones, native.Width, native.Height), nil
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
		return 0, nil, fmt.Errorf("%w: %q: %w", ErrImageNotDecodable, box.Name, err)
	}

	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w <= 0 || h <= 0 {
		return 0, nil, fmt.Errorf("%w: %q has no samples", ErrImageNotDecodable, box.Name)
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
		return 0, nil, fmt.Errorf("%w: %q does not paint every sample it holds", ErrImageNotDecodable, box.Name)
	}

	fill := blankedPicture
	if isStencil {
		fill = blankedStencil
	}

	blanked, err := blankCoveredSamples(canvas, box, zones, fill)
	if err != nil {
		return 0, nil, fmt.Errorf("%q: %w", box.Name, err)
	}
	if blanked == 0 {
		// The region met the image and cleared none of it. That is not "nothing
		// to do": one sample can be stretched across a large area, so a region
		// inside that area covers picture the caller asked to clear while no
		// sample centre falls in it. Returning nil here would report success
		// over content still in the file.
		return 0, nil, fmt.Errorf("%w: %q", ErrImageRegionCoversNoSample, box.Name)
	}

	var (
		dict       ast.PdfDict
		compressed []byte
		parms      ast.PdfDict
	)
	if isStencil {
		compressed, parms, err = encodeBlankedSamples(ctx, packStencilBits(canvas, w, h), w, 1, 1, false)
		if err != nil {
			return 0, nil, fmt.Errorf("compressing blanked image mask %q: %w", box.Name, err)
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
		compressed, parms, err = encodeBlankedSamples(ctx, rgb, w, 3, 8, photographicSource(stream.Dict))
		if err != nil {
			return 0, nil, fmt.Errorf("compressing blanked image %q: %w", box.Name, err)
		}
		cs, err := srgbColorSpace(alloc)
		if err != nil {
			return 0, nil, fmt.Errorf("blanked image %q: %w", box.Name, err)
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
	if parms != nil {
		dict["DecodeParms"] = parms
	}
	carryImageEntries(dict, stream.Dict, private)

	if !alloc.AllocAt(dest, ast.NewStream(dict, compressed)) {
		return 0, nil, fmt.Errorf("%w: %q", ErrImageNotAddressable, box.Name)
	}
	if isStencil {
		return dest, nil, nil
	}
	return dest, blankedPhotograph(source.Dict, box, zones, w, h), nil
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
	return blankedMaskStream(ctx, alloc, box, zones, gray, mask.Dict["Matte"])
}

// blankedMaskStream allocates the soft mask gray, whose origin is (0, 0),
// with the samples the zones cover made opaque (see [blankSoftMask]), and
// returns its reference. matte, when not nil, is the /Matte it keeps.
func blankedMaskStream(ctx context.Context, alloc *writer.ObjAllocator, box graphics.ImageBox, zones []heuristics.Zone, gray *image.Gray, matte ast.PdfObject) (ast.PdfObject, error) {
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
	compressed, parms, err := encodeBlankedSamples(ctx, samples, w, 1, 8, false)
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
	if parms != nil {
		out["DecodeParms"] = parms
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
	ref, err := blankedMaskStream(ctx, alloc, box, zones, gray, nil)
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
// region as well as inside it. A cleared sample is the space's black
// ([pdfimage.NativeBlack]): 0 on every component, or full black ink with no
// colour ink for four components, unless the space shows that sample as a
// colour, as a YCC or Lab-like ICC profile does, where it is the sample whose
// colour lies nearest black. Zero samples in such a space painted the blank a
// saturated green. The replacement keeps the entries [carryImageEntries]
// names; private says it is a copy, as for [blankImage].
//
// # Corpus evidence
//
//   - pdfjs issue2856: an image in a version 4 YCC ICC space, redacted
//     whole, painted 26,226 green pixels where the blank stood; the inside
//     check read them as content and the pixel check refused the redaction.
func blankNative(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	page *document.Page,
	dest int,
	stream ast.PdfStream,
	box graphics.ImageBox,
	zones []heuristics.Zone,
	native pdfimage.NativeSamples,
	private bool,
) error {
	n := native.Components
	black, err := pdfimage.NativeBlack(ctx, doc, page, stream.Dict, n)
	if err != nil {
		return fmt.Errorf("%w: %q: %w", ErrImageNotDecodable, box.Name, err)
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
	compressed, parms, err := encodeBlankedSamples(ctx, native.Data, native.Width, n, 8, photographicSource(stream.Dict))
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
	if parms != nil {
		dict["DecodeParms"] = parms
	}
	if smask, present := stream.Dict["SMask"]; present {
		dict["SMask"] = smask
	}
	carryImageEntries(dict, stream.Dict, private)
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

// blankCoveredSamples paints with fill every sample [forCoveredSamples]
// clears, and reports how many it painted: 0 when the zones hold no sample
// centre.
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

// forCoveredSamples calls clear, when the zones hold the centre of a sample
// of a w x h image, for every sample whose centre a zone holds or whose
// cell's interior a zone meets, and reports how many it cleared: 0 when the
// zones hold no centre.
//
// A sample paints the whole cell of the unit square it maps onto (ISO
// 32000-1, 8.9.3 and 8.9.4), and a viewer that does not interpolate fills
// every device pixel whose centre the cell covers with its colour (package
// imgpaint; MuPDF's redaction rounds the region outward to whole samples
// for the same reason). A sample whose centre lies just outside a region
// but whose cell reaches into it therefore still paints its picture inside
// the region, up to half a sample deep. Clearing such a sample clears up to
// a sample's width outside the region, which is what keeps the region
// showing nothing but the blank.
//
// A region holding no sample centre clears nothing, and its caller refuses
// ([ErrImageRegionCoversNoSample]) unless the samples it meets hold the
// blank already ([touchedSamplesAlreadyBlank]): no sample it meets is
// centred in it, and clearing one could take a stretched sample's whole
// picture over an area the caller never named.
//
// # Corpus evidence
//
//   - litho cutcell_2b p1-quarter: a 12 by 10 image drawn 30 by 36 points a
//     sample kept 6,016 pixels of picture inside the region, 9 to 11 points
//     deep, and the pixel check refused; clearing the cut samples changes
//     18,864 pixels outside it, all image blanking.
//   - litho blankblack_2b, imagecolour_2b and jpx_2b p1-band,
//     iccalternate_2b p1-quarter, imageintent_2b p2-quarter: the cells the
//     region's edges cut reach less than two pixels into it at 72 dpi, and
//     are now cleared, 80 to 15,724 pixels outside the region; no scenario
//     on pdfcpu, pdfjs or govdocs moves.
func forCoveredSamples(box graphics.ImageBox, zones []heuristics.Zone, w, h int, clear func(px, py int)) (int, error) {
	minPx, minPy, maxPx, maxPy, ok := candidateSamples(box, zones, w, h)
	if !ok {
		return 0, ErrImagePlacementDegenerate
	}

	holdsCentre := false
	for py := minPy; py <= maxPy && !holdsCentre; py++ {
		for px := minPx; px <= maxPx; px++ {
			if sampleCentreInZones(box, zones, w, h, px, py) {
				holdsCentre = true
				break
			}
		}
	}
	if !holdsCentre {
		return 0, nil
	}

	cells, ok := zonesInSampleSpace(box, zones, w, h)
	if !ok {
		return 0, ErrImagePlacementDegenerate
	}
	cleared := 0
	for py := minPy; py <= maxPy; py++ {
		for px := minPx; px <= maxPx; px++ {
			if sampleCentreInZones(box, zones, w, h, px, py) || sampleCellInteriorMeets(cells, px, py) {
				clear(px, py)
				cleared++
			}
		}
	}
	return cleared, nil
}

// sampleCentreInZones reports whether a zone holds the centre of sample
// (px, py) of a w x h image, edges included.
func sampleCentreInZones(box graphics.ImageBox, zones []heuristics.Zone, w, h, px, py int) bool {
	// The image's top row is the top of the unit square, so v runs the other
	// way from the row index (ISO 32000-1, 8.9.5.2).
	u := (float64(px) + 0.5) / float64(w)
	v := 1 - (float64(py)+0.5)/float64(h)

	x := box.CTM[0]*u + box.CTM[2]*v + box.CTM[4]
	y := box.CTM[1]*u + box.CTM[3]*v + box.CTM[5]

	point := heuristics.ZoneFromRect([4]float64{x, y, x, y})
	for _, z := range zones {
		if heuristics.ZonesOverlap(point, z) {
			return true
		}
	}
	return false
}

// sampleCellSlack is how far, in samples, a zone must reach into a sample's
// cell before [sampleCellInteriorMeets] counts the cell as met. Carrying a
// zone's corners into sample space rounds, so an edge the producer drew on
// a sample boundary lands a few ulps either side of it; the slack keeps the
// sample beyond that boundary, which the zone does not reach, as it was. A
// zone reaching a billionth of a sample into a cell covers no device pixel
// of it at any resolution a page renders at.
const sampleCellSlack = 1e-9

// zonesInSampleSpace returns each zone's corners carried into the sample
// space of a w x h image, where sample (px, py) fills [px, px+1] x
// [py, py+1] and rows run down from the top of the unit square, in the
// order the zone's rectangle runs round them. It reports false when the
// placement cannot be inverted.
func zonesInSampleSpace(box graphics.ImageBox, zones []heuristics.Zone, w, h int) ([][4][2]float64, bool) {
	out := make([][4][2]float64, 0, len(zones))
	for _, z := range zones {
		x0, y0, x1, y1 := heuristics.ZoneBounds(z)
		var quad [4][2]float64
		for i, c := range [4][2]float64{{x0, y0}, {x1, y0}, {x1, y1}, {x0, y1}} {
			u, v, ok := box.ImageSpace(c[0], c[1])
			if !ok {
				return nil, false
			}
			quad[i] = [2]float64{u * float64(w), (1 - v) * float64(h)}
		}
		out = append(out, quad)
	}
	return out, true
}

// sampleCellInteriorMeets reports whether one of quads, zones carried into
// sample space by [zonesInSampleSpace], reaches more than [sampleCellSlack]
// into the cell of sample (px, py). A placement maps the zone's rectangle
// to a parallelogram there, and two convex shapes share area exactly when no
// axis among their edge normals separates them, so the test is exact under
// rotation and skew.
func sampleCellInteriorMeets(quads [][4][2]float64, px, py int) bool {
	cell := [4][2]float64{
		{float64(px), float64(py)},
		{float64(px + 1), float64(py)},
		{float64(px + 1), float64(py + 1)},
		{float64(px), float64(py + 1)},
	}
	for _, q := range quads {
		axes := [][2]float64{{1, 0}, {0, 1}}
		for _, e := range [2][2]float64{{q[1][0] - q[0][0], q[1][1] - q[0][1]}, {q[3][0] - q[0][0], q[3][1] - q[0][1]}} {
			// A zone of no width has no edge along that side; the other
			// normal still places it.
			if n := math.Hypot(e[0], e[1]); n > 0 {
				axes = append(axes, [2]float64{-e[1] / n, e[0] / n})
			}
		}
		meets := true
		for _, a := range axes {
			qLo, qHi := projectQuad(q, a)
			cLo, cHi := projectQuad(cell, a)
			if qHi <= cLo+sampleCellSlack || cHi <= qLo+sampleCellSlack {
				meets = false
				break
			}
		}
		if meets {
			return true
		}
	}
	return false
}

// projectQuad returns the interval the corners of q span along the unit
// axis a.
func projectQuad(q [4][2]float64, a [2]float64) (lo, hi float64) {
	lo, hi = math.Inf(1), math.Inf(-1)
	for _, p := range q {
		d := p[0]*a[0] + p[1]*a[1]
		lo, hi = math.Min(lo, d), math.Max(hi, d)
	}
	return lo, hi
}

// touchedSamplesAlreadyBlank reports whether the zones meet the image between
// its sample centres only, over samples that already hold the blank: a region
// with nothing left to clear.
//
// [ErrImageRegionCoversNoSample] refuses a region falling between sample
// centres because the stretched samples it lies on still show picture. A
// sample holding the blank shows none: it paints what blanking would write
// there, so the region is clear as it stands, and succeeding over it leaves no
// content the caller asked to remove. That is what a redaction meets on its
// own output: the samples an earlier region blanked are stretched past it, so
// a later region on them reaches no centre. It also meets one within a single
// redaction, an image painted several times whose placement covering a centre
// blanked it in place first. The image is then left as it is.
//
// Every sample whose cell can reach a zone is held to the blank, not only the
// nearest: each cell is widened to the bounding box of its four corners, which
// takes in more samples than the zone touches under a rotated placement and so
// errs towards refusing. An image with a soft mask is not judged here: the
// mask, not the samples, says what such a picture shows, and blankImage's own
// path rules on it. An inline image is judged by the same rule, on the image
// stream its dictionary describes ([inlineCoverageError]).
//
// # Corpus evidence
//
//   - govdocs thread_300_300873, thread_400_400390: a background 1 sample wide
//     and 9004 high stretched over the page. A first redaction blanks the
//     samples whose centres it covers, which paint the page's full height, and
//     a second region on them below or above that centre line refused.
//   - govdocs thread_300_300227, pdfjs bug857031: the p1-band blanks an image
//     at one placement and meets another between its centres. thread_300_300227
//     stops at a later refusal (a structure element); bug857031 succeeds
//     since the write-time check reads its placements by this rule too
//     ([copiedPlacementLeftAsIs]).
//   - litho, pdfcpu, pdfjs, govdocs: of 27 such refusals, 21 in the
//     redact-render scenarios and 6 in them run again after a lossy
//     p1-quarter, those 3 pass this check and 24 stand, 10 of them inline
//     images, which [inlineCoverageError] brings to this check.
func touchedSamplesAlreadyBlank(
	ctx context.Context,
	doc *document.Document,
	page *document.Page,
	box graphics.ImageBox,
	zones []heuristics.Zone,
	stream ast.PdfStream,
	isStencil bool,
) (bool, error) {
	for _, key := range []ast.PdfName{"SMask", "SMaskInData"} {
		if _, present := stream.Dict[key]; present {
			return false, nil
		}
	}
	w, errW := document.ResolveInt(ctx, doc, stream.Dict["Width"])
	h, errH := document.ResolveInt(ctx, doc, stream.Dict["Height"])
	sized := errW == nil && errH == nil && w > 0 && h > 0 && w <= math.MaxInt32 && h <= math.MaxInt32
	if !sized {
		// The decode in blankImage rules on an image with no size.
		return false, nil
	}
	centres, err := forCoveredSamples(box, zones, int(w), int(h), func(int, int) {})
	if err != nil {
		return false, fmt.Errorf("%q: %w", box.Name, err)
	}
	if centres > 0 {
		// A covered centre is blanked by blankImage.
		return false, nil
	}

	var (
		blank  func(px, py int) bool
		sw, sh int
		native pdfimage.NativeSamples
		ok     bool
	)
	if !isStencil {
		native, ok, err = pdfimage.DecodeStreamNative(ctx, doc, page, ast.PdfName(box.Name), stream)
		if err != nil {
			return false, fmt.Errorf("%w: %q: %w", ErrImageNotDecodable, box.Name, err)
		}
	}
	if ok {
		n := native.Components
		black, err := pdfimage.NativeBlack(ctx, doc, page, stream.Dict, n)
		if err != nil {
			return false, fmt.Errorf("%w: %q: %w", ErrImageNotDecodable, box.Name, err)
		}
		sw, sh = native.Width, native.Height
		blank = func(px, py int) bool {
			i := (py*native.Width + px) * n
			return slices.Equal(native.Data[i:i+n], black)
		}
	} else {
		img, err := pdfimage.DecodeStreamSamples(ctx, doc, page, ast.PdfName(box.Name), stream)
		if err != nil {
			return false, fmt.Errorf("%w: %q: %w", ErrImageNotDecodable, box.Name, err)
		}
		bounds := img.Bounds()
		canvas := image.NewNRGBA(bounds)
		draw.Draw(canvas, bounds, img, bounds.Min, draw.Src)
		fill := blankedPicture
		if isStencil {
			fill = blankedStencil
		}
		sw, sh = bounds.Dx(), bounds.Dy()
		blank = func(px, py int) bool {
			i := canvas.PixOffset(bounds.Min.X+px, bounds.Min.Y+py)
			// A picture's sample must be opaque too: one the blank would
			// make opaque still changes what the page shows.
			return slices.Equal(canvas.Pix[i:i+3], fill) && (isStencil || canvas.Pix[i+3] == 0xff)
		}
	}
	if sw <= 0 || sh <= 0 {
		return false, nil
	}

	minPx, minPy, maxPx, maxPy, ok := candidateSamples(box, zones, sw, sh)
	if !ok {
		return false, nil
	}
	touched := 0
	for py := minPy; py <= maxPy; py++ {
		for px := minPx; px <= maxPx; px++ {
			if !sampleCellMeetsZones(box, zones, sw, sh, px, py) {
				continue
			}
			if !blank(px, py) {
				return false, nil
			}
			touched++
		}
	}
	return touched > 0, nil
}

// sampleCellMeetsZones reports whether the bounding box of the area sample
// (px, py) of a w x h image paints meets a zone.
func sampleCellMeetsZones(box graphics.ImageBox, zones []heuristics.Zone, w, h, px, py int) bool {
	u0, u1 := float64(px)/float64(w), float64(px+1)/float64(w)
	// The image's top row is the top of the unit square (ISO 32000-1,
	// 8.9.5.2).
	v0, v1 := 1-float64(py+1)/float64(h), 1-float64(py)/float64(h)
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, c := range [4][2]float64{{u0, v0}, {u1, v0}, {u0, v1}, {u1, v1}} {
		x := box.CTM[0]*c[0] + box.CTM[2]*c[1] + box.CTM[4]
		y := box.CTM[1]*c[0] + box.CTM[3]*c[1] + box.CTM[5]
		minX, maxX = math.Min(minX, x), math.Max(maxX, x)
		minY, maxY = math.Min(minY, y), math.Max(maxY, y)
	}
	cell := heuristics.ZoneFromRect([4]float64{minX, minY, maxX, maxY})
	for _, z := range zones {
		if heuristics.ZonesOverlap(cell, z) {
			return true
		}
	}
	return false
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
