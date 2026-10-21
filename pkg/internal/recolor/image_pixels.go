package recolor

import (
	"context"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"maps"
	"math"
	"math/bits"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	pdfimage "github.com/gotenberg/litho/pkg/internal/graphics/image"
	"github.com/gotenberg/litho/pkg/internal/render/imgpaint"
)

// memoBits caps the table a pixel conversion remembers colours in at 65536
// slots, and an image of fewer pixels gets a table no larger than its pixel
// count rounded up to a power of two. Anti-DoS guard, not spec-mandated: the
// table is direct-mapped and never grows, so an image of a million distinct
// colours costs a million conversions and at most a megabyte, while the
// colours of a flat illustration and most of a photograph's fit. It is not
// the renderer's colour memo (colorconv's memoBits, 4096 slots), which
// serves the few colours of one page's operators, not an image's pixels.
//
// # Corpus evidence
//
//   - pdfjs issue17289.pdf: its six JPEG 2000 photographs, 1.9 to 7.9
//     megapixels, hold 30014 to 128930 distinct colours each, of which 4096
//     slots catch 80 to 87% of the pixels and 65536 slots 95 to 98%.
const memoBits = 16

// memoMultiplier spreads a pixel's key over the memo's slots: the slot is
// the top bits of their product, Fibonacci hashing (Knuth, TAOCP vol. 3,
// 6.4), 2^64 over the golden ratio, so every bit of the key reaches the
// top bits.
//
// # Corpus evidence
//
//   - pdfjs issue17289.pdf: the FNV-1a step it replaces, a multiplication
//     by a 40-bit prime, left the blue sample of an RGB key out of the top
//     bits, and 4096 slots caught 18 to 39% of the pixels, not 80 to 87%.
const memoMultiplier = 0x9E3779B97F4A7C15

// sampleEntries are the image dictionary entries that describe the samples
// as stored, which a re-encoded image replaces (ISO 32000-1, 8.9.5, Table
// 89, and 7.3.8.2, Table 5).
var sampleEntries = []ast.PdfName{"Filter", "DecodeParms", "Decode", "Length", "BitsPerComponent", "ColorSpace", "SMaskInData", "DL"}

// convertPixels converts an image's samples one pixel at a time and
// re-encodes them losslessly under FlateDecode, or, under
// [ImageConfig.JPEGQuality], a DCTDecode or JPXDecode source as JPEG (see
// [Images.encodeJPEG] and [Images.jpegSource]). An image of one component at 8 bits or fewer is
// converted by its colour table instead, see [Images.convertToIndexed].
//
// The samples are read as the filters leave them, not through the image
// decoder's sRGB reading, and each pixel's components are taken through the
// /Decode array, which is folded into the result and dropped (8.9.5.2). The
// result has 16 bits a component when the source has 16, and 8 otherwise. A
// /SMask is kept, its /Matte converted with the samples (11.6.5.3). A
// colour-key /Mask compares the source samples, which the converted ones are
// not, so it is replaced by the stencil mask it describes (8.9.6.4, 8.9.6.3).
func (im *Images) convertPixels(ctx context.Context, label ast.PdfName, stream ast.PdfStream, sp imageSpace, resources, page ast.PdfDict, inline bool) (ast.PdfStream, ImageOutcome, error) {
	dict := stream.Dict
	if inline && im.cfg.InlineSpace == nil {
		return stream, ImageUnchanged, fmt.Errorf("%w: an inline image cannot name the target space", ErrImageNotConverted)
	}
	width, height, err := im.dimensions(ctx, dict)
	if err != nil {
		return stream, ImageUnchanged, err
	}
	if err := im.checkSMaskInData(ctx, dict); err != nil {
		return stream, ImageUnchanged, err
	}
	filterName := lastFilter(ctx, im.doc, dict)
	bpc := 8
	if filterName != "DCTDecode" && filterName != "JPXDecode" {
		v, err := document.ResolveInt(ctx, im.doc, dict["BitsPerComponent"])
		if err != nil {
			return stream, ImageUnchanged, fmt.Errorf("%w: /BitsPerComponent: %w", ErrImageNotConverted, err)
		}
		bpc = int(v)
	}
	if out, ok, err := im.convertToIndexed(ctx, stream, sp, resources, page, inline, filterName, width, height, bpc); err != nil {
		return stream, ImageUnchanged, err
	} else if ok {
		return out, ImageConverted, nil
	}
	raw, err := readRawSamples(ctx, im.doc, stream, width, height, sp.comps, bpc)
	if err != nil {
		return stream, ImageUnchanged, fmt.Errorf("%w: %w", ErrImageNotConverted, err)
	}
	if filterName == "DCTDecode" {
		if sp, raw, err = measureDCT(sp, raw); err != nil {
			return stream, ImageUnchanged, fmt.Errorf("%w: %w", ErrImageNotConverted, err)
		}
	}
	if raw.rows == 0 {
		return stream, ImageUnchanged, fmt.Errorf("%w: the samples hold no whole row of the %dx%d image", ErrImageNotConverted, width, height)
	}
	dec := imageDecode(ctx, im.doc, dict, raw.comps, sp.decodeDefault(raw.bpc))
	conv, err := im.cfg.Colors.Converter(ctx, sp.obj, resources, page, im.imageIntent(ctx, dict))
	if err != nil {
		return stream, ImageUnchanged, fmt.Errorf("%w: %w", ErrImageNotConverted, err)
	}
	matte, err := im.readMatte(ctx, label, dict, raw.comps, width, height)
	if err != nil {
		return stream, ImageUnchanged, err
	}

	outBPC := 8
	if raw.bpc == 16 {
		outBPC = 16
	}
	px := pixelConverter{conv: conv, raw: raw, dec: dec, maps: decodeMaps(dec, raw.comps, raw.bpc), tc: im.cfg.Components, outBytes: outBPC / 8}
	if matte != nil {
		px.matte = matte
		px.matteTarget = make([]float64, px.tc)
		conv.Convert(matte.color, px.matteTarget)
	}
	out, err := px.run(ctx, im.targetBlack(ctx))
	if err != nil {
		return stream, ImageUnchanged, err
	}
	encoded, parms, outFilter, err := im.encodeImage(ctx, out, width, height, px.tc, outBPC, filterName, outBPC == 8 && im.jpegSource(ctx, stream, filterName))
	if err != nil {
		return stream, ImageUnchanged, err
	}

	newDict := maps.Clone(dict)
	for _, key := range sampleEntries {
		delete(newDict, key)
	}
	newDict["ColorSpace"] = im.cfg.Space
	if inline {
		newDict["ColorSpace"] = im.cfg.InlineSpace
	}
	newDict["BitsPerComponent"] = ast.PdfInt(outBPC)
	newDict["Filter"] = outFilter
	if parms != nil {
		newDict["DecodeParms"] = parms
	}
	if err := im.rewriteMasks(ctx, label, newDict, raw, matte, px.matteTarget); err != nil {
		return stream, ImageUnchanged, err
	}
	im.recordSize(ctx, stream, filterName, len(encoded))
	return ast.NewStream(newDict, encoded), ImageConverted, nil
}

// dimensions reads an image's /Width and /Height and holds them to the
// document's pixel budget.
func (im *Images) dimensions(ctx context.Context, dict ast.PdfDict) (int, int, error) {
	w, err := document.ResolveInt(ctx, im.doc, dict["Width"])
	if err != nil {
		return 0, 0, fmt.Errorf("%w: /Width: %w", ErrImageNotConverted, err)
	}
	h, err := document.ResolveInt(ctx, im.doc, dict["Height"])
	if err != nil {
		return 0, 0, fmt.Errorf("%w: /Height: %w", ErrImageNotConverted, err)
	}
	maxPixels := im.doc.Limits().MaxImagePixels
	if w <= 0 || h <= 0 || int64(w) > maxPixels/int64(h) {
		return 0, 0, fmt.Errorf("%w: %dx%d pixels, past the image budget of %d", ErrImageNotConverted, w, h, maxPixels)
	}
	return int(w), int(h), nil
}

// checkSMaskInData refuses a JPXDecode image whose JPEG 2000 data carries
// its soft mask (ISO 32000-1, 8.9.5, Table 89 /SMaskInData): re-encoded
// under FlateDecode or DCTDecode, the mask would be lost, since only
// JPXDecode data carries one (7.4.9) and the colour channels alone are
// converted. The refusal holds under [ImageConfig.JPEGQuality] too.
func (im *Images) checkSMaskInData(ctx context.Context, dict ast.PdfDict) error {
	if v, err := document.ResolveInt(ctx, im.doc, dict["SMaskInData"]); err == nil && v != 0 {
		return fmt.Errorf("%w: its JPEG 2000 data carries its soft mask (/SMaskInData %d)", ErrImageNotConverted, v)
	}
	return nil
}

// measureDCT takes a JPEG's component count from its decoded samples, as
// the image decoder does: the codestream, not the dictionary, says how many
// components it holds, and a greyscale JPEG declared DeviceRGB is common. A
// count that differs from the space's reads in the device space of that
// count; a Lab JPEG keeps its space.
func measureDCT(sp imageSpace, raw rawSamples) (imageSpace, rawSamples, error) {
	pixels := raw.width * raw.height
	if len(raw.data)%pixels != 0 {
		return sp, raw, fmt.Errorf("the decoded JPEG is %d bytes, not whole pixels of %dx%d", len(raw.data), raw.width, raw.height)
	}
	n := len(raw.data) / pixels
	if n == sp.comps {
		return sp, raw, nil
	}
	device := map[int]ast.PdfName{1: familyGray, 3: familyRGB, 4: familyCMYK}[n]
	if device == "" || sp.family == familyLab {
		return sp, raw, fmt.Errorf("the decoded JPEG holds %d components a pixel for a space of %d", n, sp.comps)
	}
	raw.comps = n
	raw.rows = raw.height
	return imageSpace{obj: device, family: device, comps: n, ranges: unitRanges(n)}, raw, nil
}

// matteData is a soft mask's /Matte (ISO 32000-1, 11.6.5.3): the colour the
// image's samples were preblended with, in the image's space, and the mask's
// opacity per pixel when its grid is the image's.
type matteData struct {
	ref   ast.PdfObject
	mask  ast.PdfStream
	color []float64
	alpha []uint8
}

// readMatte reads the /Matte of an image's soft mask, or returns nil when it
// has none.
func (im *Images) readMatte(ctx context.Context, label ast.PdfName, dict ast.PdfDict, comps, width, height int) (*matteData, error) {
	ref, ok := dict["SMask"]
	if !ok {
		return nil, nil
	}
	resolved, err := document.ResolveObject(ctx, im.doc, ref)
	if err != nil {
		return nil, fmt.Errorf("%w: its soft mask: %w", ErrImageNotConverted, err)
	}
	mask, isStream := resolved.(ast.PdfStream)
	if !isStream || mask.Dict == nil {
		// /SMask /None, or anything that is no stream, masks nothing.
		return nil, nil
	}
	obj, ok := mask.Dict["Matte"]
	if !ok {
		return nil, nil
	}
	arr, err := document.ResolveArray(ctx, im.doc, obj)
	if err != nil || len(arr) != comps {
		return nil, fmt.Errorf("%w: its soft mask's /Matte does not hold the %d components of its space", ErrImageNotConverted, comps)
	}
	m := &matteData{ref: ref, mask: mask, color: make([]float64, comps)}
	for i, v := range arr {
		r, err := document.ResolveObject(ctx, im.doc, v)
		if err != nil {
			return nil, fmt.Errorf("%w: /Matte: %w", ErrImageNotConverted, err)
		}
		m.color[i] = document.Float(r)
	}
	// 11.6.5.3 requires a mask with /Matte to have the image's dimensions.
	// One that does not is converted without undoing the preblending.
	img, err := pdfimage.DecodeMask(ctx, im.doc, label+" soft mask", mask)
	if err != nil {
		return nil, fmt.Errorf("%w: its soft mask: %w", ErrImageNotConverted, err)
	}
	if b := img.Bounds(); b.Dx() == width && b.Dy() == height {
		m.alpha = grayPlane(img)
	}
	return m, nil
}

// grayPlane returns the grey level of each pixel of img, rows top to bottom.
func grayPlane(img image.Image) []uint8 {
	b := img.Bounds()
	out := make([]uint8, 0, b.Dx()*b.Dy())
	g, isGray := img.(*image.Gray)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		if isGray {
			i := g.PixOffset(b.Min.X, y)
			out = append(out, g.Pix[i:i+b.Dx()]...)
			continue
		}
		for x := b.Min.X; x < b.Max.X; x++ {
			out = append(out, color.GrayModel.Convert(img.At(x, y)).(color.Gray).Y)
		}
	}
	return out
}

// rewriteMasks fixes the masks of a converted image, whose dictionary dict
// is a copy carrying the source's /SMask and /Mask.
//
// A soft mask with /Matte is copied with the matte converted, its encoded
// samples kept: the matte is in the parent image's colour space (ISO
// 32000-1, 11.6.5.3, Table 146). The source's mask object is not edited,
// since another image may share it.
//
// A colour-key /Mask names ranges of the source's raw samples (8.9.6.4),
// which no longer exist. The image's /SMask overrides it (Table 89), so with
// one present it is dropped. Otherwise it becomes the stencil mask it
// describes, an explicit mask (8.9.6.3), which every PDF version since 1.3
// reads and PDF/A-1 permits, where a soft mask would need PDF 1.4.
func (im *Images) rewriteMasks(ctx context.Context, label ast.PdfName, dict ast.PdfDict, raw rawSamples, matte *matteData, matteTarget []float64) error {
	if matte != nil {
		md := maps.Clone(matte.mask.Dict)
		arr := make(ast.PdfArray, len(matteTarget))
		for i, v := range matteTarget {
			arr[i] = ast.PdfReal(v)
		}
		md["Matte"] = arr
		copied := matte.mask
		copied.Dict = md
		dict["SMask"] = im.alloc.Alloc(copied)
	}
	obj, ok := dict["Mask"]
	if !ok {
		return nil
	}
	resolved, err := document.ResolveObject(ctx, im.doc, obj)
	if err != nil {
		return fmt.Errorf("%w: its /Mask: %w", ErrImageNotConverted, err)
	}
	ranges, isKey := resolved.(ast.PdfArray)
	if !isKey {
		// A stencil /Mask is a shape, which the conversion keeps.
		return nil
	}
	if _, hasSMask := dict["SMask"]; hasSMask {
		delete(dict, "Mask")
		return nil
	}
	keys := make([]int, len(ranges))
	for i, v := range ranges {
		n, err := document.ResolveInt(ctx, im.doc, v)
		if err != nil {
			return fmt.Errorf("%w: colour-key range %d: %w", ErrImageNotConverted, i, err)
		}
		keys[i] = int(n)
	}
	plane, err := imgpaint.ColorKey(raw.data, raw.width, raw.height, raw.comps, raw.bpc, keys, im.doc.Limits().MaxImagePixels)
	if err != nil {
		return fmt.Errorf("%w: %s colour key: %w", ErrImageNotConverted, label, err)
	}
	stencil, masked := packStencil(plane)
	if !masked {
		// No sample falls in the ranges: the key masks nothing.
		delete(dict, "Mask")
		return nil
	}
	compressed, _, err := flateCompress(stencil, -1)
	if err != nil {
		return fmt.Errorf("%w: compressing the stencil mask: %w", ErrImageNotConverted, err)
	}
	dict["Mask"] = im.alloc.Alloc(ast.NewStream(ast.PdfDict{
		"Type":             ast.PdfName("XObject"),
		"Subtype":          ast.PdfName("Image"),
		"Width":            ast.PdfInt(plane.Width),
		"Height":           ast.PdfInt(plane.Height),
		"ImageMask":        ast.PdfBool(true),
		"BitsPerComponent": ast.PdfInt(1),
		"Filter":           ast.PdfName("FlateDecode"),
	}, compressed))
	return nil
}

// packStencil packs an opacity plane as the samples of a stencil mask, one
// bit a pixel, rows padded to a byte: under the default /Decode [0 1] a 1
// masks the image out and a 0 lets it paint (ISO 32000-1, 8.9.6.3). It
// reports whether any pixel is masked.
func packStencil(plane *imgpaint.Alpha) ([]byte, bool) {
	rowBytes := (plane.Width + 7) / 8
	out := make([]byte, rowBytes*plane.Height)
	masked := false
	for y := range plane.Height {
		for x := range plane.Width {
			if plane.A[y*plane.Width+x] == 0 {
				out[y*rowBytes+x/8] |= 0x80 >> (x % 8)
				masked = true
			}
		}
	}
	return out, masked
}

// targetBlack returns the target's black, which rows short data lacks are
// painted in, as the image decoder paints them.
func (im *Images) targetBlack(ctx context.Context) []float64 {
	black := make([]float64, im.cfg.Components)
	conv, err := im.cfg.Colors.Converter(ctx, familyGray, nil, nil, "")
	if err != nil {
		return black
	}
	conv.Convert([]float64{0}, black)
	return black
}

// recordSize adds a converted image's encoded sizes to the report.
func (im *Images) recordSize(ctx context.Context, stream ast.PdfStream, filterName ast.PdfName, after int) {
	before, err := stream.Bytes(ctx)
	if err != nil {
		before = nil
	}
	e := im.report.Encoded[string(filterName)]
	e.Images++
	e.Before += int64(len(before))
	e.After += int64(after)
	im.report.Encoded[string(filterName)] = e
}

// pixelConverter converts the pixels of one image.
type pixelConverter struct {
	conv     Converter
	raw      rawSamples
	dec      []float64
	maps     [][]float64
	tc       int
	outBytes int

	matte       *matteData
	matteTarget []float64
}

// run returns the converted samples, rows of width x tc components at
// outBytes bytes each.
func (p *pixelConverter) run(ctx context.Context, black []float64) ([]byte, error) {
	r := p.raw
	pixelBytes := p.tc * p.outBytes
	out := make([]byte, r.width*r.height*pixelBytes)
	in := make([]float64, r.comps)
	res := make([]float64, p.tc)
	values := make([]uint32, r.comps)

	// A pixel of fewer than 64 bits of samples is its own memo key; the
	// preblended samples of a matted image depend on its opacity too, so it
	// is converted pixel by pixel.
	useMemo := p.matte == nil && r.comps*r.bpc < 64
	var memoKeys []uint64
	var memoVals []byte
	// The slot is the top shift bits of the hash.
	shift := 64
	if useMemo {
		slotBits := min(memoBits, bits.Len(uint(r.width*r.height-1)))
		shift -= slotBits
		memoKeys = make([]uint64, 1<<slotBits)
		memoVals = make([]byte, (1<<slotBits)*pixelBytes)
	}

	blackBytes := make([]byte, pixelBytes)
	p.quantize(black, blackBytes)

	rowBytes := r.rowBytes()
	for y := range r.height {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		dst := out[y*r.width*pixelBytes : (y+1)*r.width*pixelBytes]
		if y >= r.rows {
			for x := range r.width {
				copy(dst[x*pixelBytes:], blackBytes)
			}
			continue
		}
		row := r.data[y*rowBytes : (y+1)*rowBytes]
		for x := range r.width {
			var key uint64
			for c := range r.comps {
				values[c] = r.sample(row, x*r.comps+c)
				key = key<<r.bpc | uint64(values[c])
			}
			px := dst[x*pixelBytes : (x+1)*pixelBytes]
			if useMemo {
				// The key is stored plus one so the zero key is told from
				// an empty slot.
				slot := int((key * memoMultiplier) >> shift) //nolint:gosec // Below 1 << memoBits.
				if memoKeys[slot] == key+1 {
					copy(px, memoVals[slot*pixelBytes:(slot+1)*pixelBytes])
					continue
				}
				p.convertPixel(values, in, res, y*r.width+x)
				p.quantize(res, px)
				memoKeys[slot] = key + 1
				copy(memoVals[slot*pixelBytes:], px)
				continue
			}
			p.convertPixel(values, in, res, y*r.width+x)
			p.quantize(res, px)
		}
	}
	return out, nil
}

// convertPixel converts the raw samples values of pixel i into res.
func (p *pixelConverter) convertPixel(values []uint32, in, res []float64, i int) {
	top := float64(int(1)<<p.raw.bpc - 1)
	for c, v := range values {
		if p.maps != nil {
			in[c] = p.maps[c][v]
		} else {
			lo, hi := p.dec[2*c], p.dec[2*c+1]
			in[c] = lo + float64(v)*(hi-lo)/top
		}
	}
	if p.matte == nil || p.matte.alpha == nil {
		p.conv.Convert(in, res)
		return
	}
	// ISO 32000-1, 11.6.5.3: a preblended sample is c' = m + a(c - m).
	// The colour is recovered, converted, and blended again with the
	// converted matte, so the result is preblended in the target.
	a := float64(p.matte.alpha[i]) / 255
	m := p.matte.color
	for c := range in {
		if a > 0 {
			in[c] = m[c] + (in[c]-m[c])/a
		} else {
			in[c] = m[c]
		}
	}
	p.conv.Convert(in, res)
	for c := range res {
		res[c] = p.matteTarget[c] + a*(res[c]-p.matteTarget[c])
	}
}

// quantize writes target components on [0, 1] as samples of outBytes bytes,
// big-endian (ISO 32000-1, 8.9.3), clipping to the range.
func (p *pixelConverter) quantize(res []float64, dst []byte) {
	for c, v := range res {
		v = min(max(v, 0), 1)
		if math.IsNaN(v) {
			v = 0
		}
		if p.outBytes == 2 {
			binary.BigEndian.PutUint16(dst[2*c:], uint16(math.Round(v*65535)))
			continue
		}
		dst[c] = byte(math.Round(v * 255))
	}
}

// convertJPXColour converts a JPXDecode image that takes its colour from its
// own data (ISO 32000-1, 7.4.9). Its samples are read through the image
// decoder, which colour-manages that data to sRGB, and converted from sRGB,
// at 8 bits a component.
func (im *Images) convertJPXColour(ctx context.Context, label ast.PdfName, stream ast.PdfStream, resources, page ast.PdfDict, inline bool) (ast.PdfStream, ImageOutcome, error) {
	dict := stream.Dict
	if inline && im.cfg.InlineSpace == nil {
		return stream, ImageUnchanged, fmt.Errorf("%w: an inline image cannot name the target space", ErrImageNotConverted)
	}
	width, height, err := im.dimensions(ctx, dict)
	if err != nil {
		return stream, ImageUnchanged, err
	}
	if err := im.checkSMaskInData(ctx, dict); err != nil {
		return stream, ImageUnchanged, err
	}
	// The decoder undoes a /Matte against the JPEG 2000 data's own colour,
	// and a /Mask colour key compares samples in it: neither carries over
	// to sRGB exactly.
	if sm, err := document.ResolveStream(ctx, im.doc, dict["SMask"]); err == nil && sm.Dict != nil {
		if _, ok := sm.Dict["Matte"]; ok {
			return stream, ImageUnchanged, fmt.Errorf("%w: a JPEG 2000 image in its own colour space with a matted soft mask", ErrImageNotConverted)
		}
	}
	if m, err := document.ResolveObject(ctx, im.doc, dict["Mask"]); err == nil {
		if _, isKey := m.(ast.PdfArray); isKey {
			return stream, ImageUnchanged, fmt.Errorf("%w: a JPEG 2000 image in its own colour space with a colour-key mask", ErrImageNotConverted)
		}
	}
	img, err := pdfimage.DecodePainted(ctx, im.doc, label, stream, resources, page)
	if err != nil {
		return stream, ImageUnchanged, fmt.Errorf("%w: %w", ErrImageNotConverted, err)
	}
	if b := img.Bounds(); b.Dx() != width || b.Dy() != height {
		return stream, ImageUnchanged, fmt.Errorf("%w: the decoded image is %dx%d, not %dx%d", ErrImageNotConverted, b.Dx(), b.Dy(), width, height)
	}
	b := img.Bounds()
	samples := make([]byte, 0, width*height*3)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			samples = append(samples, c.R, c.G, c.B)
		}
	}
	conv, err := im.cfg.Colors.Converter(ctx, srgbSpace(), nil, nil, im.imageIntent(ctx, dict))
	if err != nil {
		return stream, ImageUnchanged, fmt.Errorf("%w: %w", ErrImageNotConverted, err)
	}
	raw := rawSamples{data: samples, width: width, height: height, comps: 3, bpc: 8, rows: height}
	dec := unitRanges(3)
	px := pixelConverter{conv: conv, raw: raw, dec: dec, maps: decodeMaps(dec, 3, 8), tc: im.cfg.Components, outBytes: 1}
	out, err := px.run(ctx, im.targetBlack(ctx))
	if err != nil {
		return stream, ImageUnchanged, err
	}
	encoded, parms, outFilter, err := im.encodeImage(ctx, out, width, height, px.tc, 8, "JPXDecode", im.jpegSource(ctx, stream, "JPXDecode"))
	if err != nil {
		return stream, ImageUnchanged, err
	}
	newDict := maps.Clone(dict)
	for _, key := range sampleEntries {
		delete(newDict, key)
	}
	newDict["ColorSpace"] = im.cfg.Space
	if inline {
		newDict["ColorSpace"] = im.cfg.InlineSpace
	}
	newDict["BitsPerComponent"] = ast.PdfInt(8)
	newDict["Filter"] = outFilter
	if parms != nil {
		newDict["DecodeParms"] = parms
	}
	im.recordSize(ctx, stream, "JPXDecode", len(encoded))
	return ast.NewStream(newDict, encoded), ImageConverted, nil
}

// convertToIndexed converts an image of one component at 8 bits or fewer a
// sample, a greyscale scan or a one-colorant tint, by writing its samples as
// indices into a colour table of the target (ISO 32000-1, 8.6.6.3): entry i
// is the conversion of what sample value i stands for under the image's
// /Decode, which the table then carries. At most 256 colours exist, so the
// table holds every one exactly, and the samples, their filters and their
// masks are kept as encoded: a JBIG2 or CCITT scan stays one bit a pixel
// rather than growing to 8 bits a component of the target. A colour-key
// /Mask compares the same raw samples, which are unchanged (8.9.6.4).
//
// It reports false, to convert pixel by pixel, for a DCTDecode or JPXDecode
// image, whose codestream rather than its dictionary says how many
// components it has, and for one whose soft mask has a /Matte, which is in
// the image's own space.
func (im *Images) convertToIndexed(ctx context.Context, stream ast.PdfStream, sp imageSpace, resources, page ast.PdfDict, inline bool, filterName ast.PdfName, width, height, bpc int) (ast.PdfStream, bool, error) {
	dict := stream.Dict
	if sp.comps != 1 || bpc > 8 || filterName == "DCTDecode" || filterName == "JPXDecode" {
		return stream, false, nil
	}
	switch bpc {
	case 1, 2, 4, 8:
	default:
		return stream, false, nil
	}
	if sm, err := document.ResolveStream(ctx, im.doc, dict["SMask"]); err == nil && sm.Dict != nil {
		if _, matted := sm.Dict["Matte"]; matted {
			return stream, false, nil
		}
	}
	// The samples are kept, but only samples that decode are: what the
	// pixel path refuses, this path refuses too.
	if raw, err := readRawSamples(ctx, im.doc, stream, width, height, 1, bpc); err != nil || raw.rows == 0 {
		if err == nil {
			err = fmt.Errorf("the samples hold no whole row of the %dx%d image", width, height)
		}
		return stream, false, fmt.Errorf("%w: %w", ErrImageNotConverted, err)
	}
	target := im.cfg.Space
	if inline {
		target = im.cfg.InlineSpace
	}
	conv, err := im.cfg.Colors.Converter(ctx, sp.obj, resources, page, im.imageIntent(ctx, dict))
	if err != nil {
		return stream, false, fmt.Errorf("%w: %w", ErrImageNotConverted, err)
	}
	entries := 1 << bpc
	values := decodeMaps(imageDecode(ctx, im.doc, dict, 1, sp.decodeDefault(bpc)), 1, bpc)[0]
	tc := im.cfg.Components
	table := make([]byte, entries*tc)
	res := make([]float64, tc)
	for i, v := range values {
		conv.Convert([]float64{v}, res)
		for c, r := range res {
			r = min(max(r, 0), 1)
			if math.IsNaN(r) {
				r = 0
			}
			table[i*tc+c] = byte(math.Round(r * 255))
		}
	}
	out := stream
	out.Dict = maps.Clone(dict)
	delete(out.Dict, "Decode")
	out.Dict["ColorSpace"] = ast.PdfArray{familyIndexed, target, ast.PdfInt(entries - 1), ast.PdfString(table)}
	encoded, err := stream.Bytes(ctx)
	if err != nil {
		return stream, false, fmt.Errorf("%w: %w", ErrImageNotConverted, err)
	}
	im.recordSize(ctx, stream, filterName, len(encoded))
	return out, true, nil
}
