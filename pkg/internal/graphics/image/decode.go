package image

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"io"
	"log/slog"
	"math"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/filter"
	"github.com/gotenberg/litho/pkg/internal/graphics/function"
	"github.com/gotenberg/litho/pkg/internal/graphics/icc"
)

// Anti-DoS bounds on what an image dictionary may make the decoder allocate
// or evaluate.
//
// # Corpus evidence
//
//   - All corpora: the widest DeviceN image has seven colorants, CMYK plus
//     three spots, which maxTintColorants leaves room beyond.
//   - All corpora: the largest DeviceN image is 540x534 over seven colorants,
//     36.9 million worst-case corner visits, which maxDeviceNWork accepts
//     with room.
//   - All corpora: the worst DeviceN image holds 31,400 distinct tuples, which
//     maxTintMemo covers.
const (
	// maxBytesPerPixel is how many bytes of decoded samples a pixel may
	// take, the RGBA worst case. The pixel budget,
	// limits.Limits.MaxImagePixels, times it caps the decoded byte stream
	// and bounds [io.ReadAll] on the streaming decoder output. Anti-DoS, not
	// spec-mandated.
	maxBytesPerPixel = 4

	// maxTintColorants bounds the colorant count of a DeviceN image.
	// [decodeDeviceN] evaluates the tint transform once per distinct pixel
	// tuple and the sampled evaluator visits 2^N corners each time, so the
	// count drives the work exponentially. Twelve leaves room beyond the
	// widest real images while keeping a single evaluation to 4096 corners.
	// Anti-DoS, not spec-mandated: ISO 32000-1, 8.6.6.5 puts no limit on
	// /DeviceN colorants.
	maxTintColorants = 12

	// maxDeviceNWork bounds pixels x 2^colorants, the worst-case corner
	// visits when every pixel holds a distinct tuple and the memo below never
	// hits. 1<<28 accepts the largest real DeviceN image with room while
	// refusing a megapixel image over twelve colorants outright. Anti-DoS,
	// not spec-mandated.
	maxDeviceNWork = 1 << 28

	// maxTintMemo bounds the tuple cache [decodeDeviceN] keeps. Distinct
	// tuples are bounded only by the pixel count, so an unbounded map on
	// adversarial input is a memory hazard; past this size the decoder keeps
	// evaluating but stops growing the cache. 65536 covers the distinct tuple
	// counts real images hold. Anti-DoS, not spec-mandated.
	maxTintMemo = 1 << 16
)

// validateImageDimensions rejects degenerate or out-of-policy image
// sizes. Width and height must both be positive and their product must
// fit under maxPixels, the document's pixel budget; an image past it fails
// with [ErrImageTooLarge].
func validateImageDimensions(width, height int, maxPixels int64) error {
	if width <= 0 || height <= 0 {
		return fmt.Errorf("invalid image dimensions: %dx%d", width, height)
	}
	if int64(width)*int64(height) > maxPixels {
		return fmt.Errorf("%w: %dx%d is more than %d pixels", ErrImageTooLarge, width, height, maxPixels)
	}
	return nil
}

// unpackSamples expands packed image samples so each occupies one byte,
// returning 8-bit data untouched.
//
// Per ISO 32000-1, 8.9.3 samples are packed MSB-first and every row begins on a
// byte boundary, so one row occupies ceil(width*samples*bpc/8) bytes and the
// padding bits at its end are skipped rather than read as the next row's first
// samples. Unpacking row by row rather than straight down the buffer is what
// keeps a width that is not a multiple of 8 from shifting every row after the
// first.
//
// scale separates a colour value from a palette index. A colour value is
// stretched to the full 8-bit range, which leaves [buildDecodeLUTs] exact: those
// tables are linear across 0..255, so stretching first and mapping after gives
// the same answer as mapping the raw [0, 2^bpc-1] range ISO 32000-1, 8.9.5.2
// defines, inverted /Decode arrays included. A palette index must NOT be
// stretched, because [decodeIndexed] looks it up directly and a stretched index
// addresses the wrong colour.
//
// The expansion is width*height*samplesPerPixel bytes. For a device colour
// space samplesPerPixel is at most 4, so [validateImageDimensions] alone holds
// the result under maxBytesPerPixel bytes a pixel however small the packed
// input was. A DeviceN
// image breaks that arithmetic: it carries one sample per colorant, up to
// maxTintColorants, so [decodeDeviceN] bounds its own pixel count before
// calling here rather than relying on the four-component reasoning.
//
// # Corpus evidence
//
//   - All corpora: one image declares /BitsPerComponent 3, which is why the
//     unsupported depths are refused rather than strided.
func unpackSamples(data []byte, width, height, samplesPerPixel, bpc int, scale bool) ([]byte, error) {
	if bpc == 8 {
		return data, nil
	}

	perRow := width * samplesPerPixel
	var rowBytes int
	switch bpc {
	case 1, 2, 4:
		rowBytes = (perRow*bpc + 7) / 8
	case 16:
		rowBytes = perRow * 2
	default:
		// ISO 32000-1, 8.9.5.1 Table 89 permits 1, 2, 4, 8 and 16 only. A
		// depth such as 3 has no defined packing at all, so it is refused
		// rather than strided as though it had one.
		return nil, fmt.Errorf("unsupported BitsPerComponent %d for image decode", bpc)
	}

	if len(data) < rowBytes*height {
		return nil, fmt.Errorf("decoded data too short for %d-bit image: got %d, want %d",
			bpc, len(data), rowBytes*height)
	}

	out := make([]byte, 0, perRow*height)

	if bpc == 16 {
		// The high byte alone: this decoder and its /Decode tables are 8-bit,
		// so the low byte has nowhere to land.
		for y := range height {
			row := data[y*rowBytes : (y+1)*rowBytes]
			for i := range perRow {
				out = append(out, row[i*2])
			}
		}
		return out, nil
	}

	maxVal := byte(1<<bpc - 1)
	// Bit replication. 255/maxVal is exact for 1, 2 and 4 bits (255, 85, 17),
	// so a stretched sample is the raw value repeated across the byte and the
	// brightest sample lands on exactly 255 rather than one short of it.
	mul := byte(255) / maxVal

	for y := range height {
		row := data[y*rowBytes : (y+1)*rowBytes]
		for i := range perRow {
			v := filter.BitSample(row, i, bpc, maxVal)
			if scale {
				v *= mul
			}
			out = append(out, v)
		}
	}
	return out, nil
}

// padShortSamples returns data padded with zeros to the packed rows of a
// width x height image, and how many whole rows data held. An unsupported
// depth leaves data as it is, for unpackSamples to refuse. Data short of one
// whole row is returned as it is, with no rows, since the caller refuses it:
// padding it first allocated the whole image for nothing, and the refusal then
// reported the padded length instead of the stream's.
func padShortSamples(data []byte, width, height, samplesPerPixel, bpc int) ([]byte, int) {
	var rowBytes int
	switch bpc {
	case 1, 2, 4:
		rowBytes = (width*samplesPerPixel*bpc + 7) / 8
	case 8:
		rowBytes = width * samplesPerPixel
	case 16:
		rowBytes = width * samplesPerPixel * 2
	default:
		return data, height
	}
	need := rowBytes * height
	if rowBytes == 0 || len(data) >= need {
		return data, height
	}
	rows := len(data) / rowBytes
	if rows == 0 {
		return data, 0
	}
	padded := make([]byte, need)
	copy(padded, data)
	return padded, rows
}

// indexDecode maps the indices of an indexed image through its /Decode array
// in place (ISO 32000-1, 8.9.5.2), rounding each to the nearest index. The
// default [0 2^n-1] leaves them as they are; an inverted array such as
// [255 0] reverses the table, and reading the indices unmapped paints every
// pixel from the wrong end of it.
//
// # Corpus evidence
//
//   - pdfjs issue10339_reduced.pdf: states /Decode [255 0], and reading its
//     indices unmapped painted every pixel from the wrong end of the table.
func indexDecode(ctx context.Context, doc *document.Document, dict ast.PdfDict, indices []byte, bpc int) {
	dec := resolveDecodeArray(ctx, doc, dict, 1)
	if dec == nil || bpc < 1 || bpc > 8 {
		return
	}
	top := float64(int(1)<<bpc - 1)
	var lut [256]byte
	for s := range lut {
		v := dec[0] + float64(s)*(dec[1]-dec[0])/top
		lut[s] = byte(min(max(math.Round(v), 0), 255))
	}
	for i, v := range indices {
		indices[i] = lut[v]
	}
}

// finishRows paints the rows of img from row on black, and returns img.
func finishRows(img image.Image, row int) image.Image {
	b := img.Bounds()
	for y := b.Min.Y + row; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			switch m := img.(type) {
			case *image.Gray:
				m.SetGray(x, y, color.Gray{})
			case *image.RGBA:
				m.SetRGBA(x, y, color.RGBA{A: 255})
			}
		}
	}
	return img
}

// imageFilterName returns the name of the first filter an image declares, or
// "" when it declares none.
func imageFilterName(ctx context.Context, doc *document.Document, dict ast.PdfDict) string {
	filterObj, ok := dict["Filter"]
	if !ok {
		return ""
	}
	filterRes, err := document.ResolveObject(ctx, doc, filterObj)
	if err != nil {
		doc.Logger.Debug("resolving image filter", slog.Any("err", err))
	}
	if fn, ok := filterRes.(ast.PdfName); ok {
		return string(fn)
	}
	if fArr, ok := filterRes.(ast.PdfArray); ok && len(fArr) > 0 {
		if fn, ok := fArr[0].(ast.PdfName); ok {
			return string(fn)
		}
	}
	return ""
}

// jpegSampleSpace names the colour space of the samples decodeDCT produced,
// deduced from how many bytes it returned per pixel.
//
// The count has to be measured rather than taken from the image dictionary.
// decodeDCT reports whatever the JPEG's own colour transform yielded
// (*image.Gray as one byte, *image.CMYK as four, anything else converted to
// three bytes of RGB), and that need not agree with the /ColorSpace the
// dictionary declares. A grayscale JPEG declared /DeviceRGB is the common
// disagreement, and believing the declaration there asks the pixel loop for
// three bytes a pixel when one arrived, which reads as "decoded data too
// short" on a file every other reader opens.
//
// ok is false when the length is not a whole number of 1, 3 or 4 byte pixels,
// which means the samples are not shaped like anything this decoder builds.
func jpegSampleSpace(decodedLen, width, height int) (string, bool) {
	pixels := width * height
	if pixels <= 0 || decodedLen%pixels != 0 {
		return "", false
	}
	switch decodedLen / pixels {
	case 1:
		return "DeviceGray", true
	case 3:
		return "DeviceRGB", true
	case 4:
		return "DeviceCMYK", true
	}
	return "", false
}

// AppliesDecodeArray reports whether [DecodeImage] applies the image's /Decode
// array to the pixels it returns.
//
// A caller that re-encodes decoded pixels needs this to know whether /Decode
// has already been folded into them. Where it has, the entry may be dropped
// from the new image and the result renders the same; where it has not, the
// pixels still need it and dropping it changes the picture.
//
// An indexed image's /Decode remaps palette INDICES rather than colour
// components (ISO 32000-1, 8.9.5.2), and the decode applies it to the indices
// before looking them up, so its pixels carry it too. One whose colour table
// cannot be read is the exception: [DecodeImage] refuses it rather than
// returning pixels, so answering "the array was applied" would describe pixels
// that were never produced.
//
// A JPEG was excluded too, while the DCTDecode branch returned image/jpeg's
// output without building the /Decode tables. That branch is gone: a JPEG now
// takes the ordinary sample path, where the array is applied like any other
// image's. An Adobe-inverted CMYK JPEG carrying /Decode [1 0 1 0 1 0 1 0]
// therefore arrives already inverted, and the entry is redundant on re-encode.
func AppliesDecodeArray(ctx context.Context, doc *document.Document, dict ast.PdfDict) bool {
	if _, _, indexed := indexedColorSpace(ctx, doc, dict); indexed {
		_, readable := resolveIndexedPalette(ctx, doc, dict, colorContext{})
		return readable
	}
	return true
}

// imageComponents returns the samples-per-pixel for the color spaces the
// 8-bit decoder handles, or 0 for anything else.
func imageComponents(colorSpace string) int {
	switch colorSpace {
	case "DeviceRGB":
		return 3
	case "DeviceGray":
		return 1
	case "DeviceCMYK":
		return 4
	}
	return 0
}

// resolveDecodeArray reads the image /Decode array as 2*components
// numbers, or returns nil when it is absent or too short. Per ISO
// 32000-1, 8.9.5.2 the /Decode array remaps each sample from the raw
// [0, 2^bpc-1] range onto [Dmin, Dmax]; honoring it renders inverted or
// remapped images (e.g. Adobe-inverted CMYK, /Decode [1 0 ...]) with
// correct color instead of the raw samples.
//
// # Corpus evidence
//
//   - pdfjs issue20668.pdf: gives its CMYK JPEGs /Decode
//     [1 0 1 0 1 0 1 0 0], and PDF.js inverts them by the leading pairs, so
//     ignoring the over-long array rendered the images as their negatives.
func resolveDecodeArray(ctx context.Context, doc *document.Document, dict ast.PdfDict, components int) []float64 {
	obj, ok := dict["Decode"]
	if !ok {
		return nil
	}
	arr, err := document.ResolveArray(ctx, doc, obj)
	if err != nil {
		doc.Logger.Debug("resolving image /Decode array", slog.Any("err", err))
		return nil
	}
	// ISO 32000-1, Table 89 wants exactly two numbers per component. A longer
	// array keeps its leading pairs, which name every component unambiguously,
	// and PDF.js inverts an Adobe CMYK JPEG by those pairs, so ignoring the
	// array renders such an image as its negative. A shorter array leaves
	// components without a range and stays ignored.
	if len(arr) < 2*components {
		doc.Logger.Debug("ignoring image /Decode array with unexpected length",
			slog.Int("len", len(arr)), slog.Int("want", 2*components))
		return nil
	}
	dec := make([]float64, 2*components)
	for i := range dec {
		dec[i] = document.Float(arr[i])
	}
	return dec
}

// buildDecodeLUTs returns one 256-entry lookup table per component that
// maps an 8-bit sample to its decoded byte. With no /Decode array the
// tables are the exact identity, so decoded output is byte-for-byte
// unchanged for the common case.
func buildDecodeLUTs(dec []float64, components int) [][256]byte {
	luts := make([][256]byte, components)
	for j := range luts {
		if dec == nil {
			for s := range 256 {
				luts[j][s] = byte(s)
			}
			continue
		}
		dmin, dmax := dec[2*j], dec[2*j+1]
		for s := range 256 {
			v := dmin + float64(s)*(dmax-dmin)/255.0
			switch {
			case v < 0:
				v = 0
			case v > 1:
				v = 1
			}
			luts[j][s] = byte(v*255 + 0.5)
		}
	}
	return luts
}

// maxPaletteEntries bounds the colour table of an indexed image. ISO 32000-1,
// 8.6.6.3 caps /hival at 255, so a conforming palette never holds more; the
// bound is here because the value is read from the file.
const maxPaletteEntries = 256

// indexedColorSpace reports the [/Indexed base hival lookup] array of an image's
// colour space and the name of its base space, or ok=false when the image is not
// in an indexed space at all. The base is empty when the array is too short to
// name one.
//
// Whether a space IS indexed is a different question from whether its colour
// table could be READ, and one flag used to answer both. Per ISO 32000-1, 8.6.6.3
// an indexed image holds one index per pixel whatever its base is, so an indexed
// image with an unreadable table must not be re-read in its base space: that
// strides the samples by the base's component count and reports complete data as
// truncated.
func indexedColorSpace(ctx context.Context, doc *document.Document, dict ast.PdfDict) (ast.PdfArray, string, bool) {
	csObj, ok := dict["ColorSpace"]
	if !ok {
		return nil, "", false
	}
	resolved, err := document.ResolveObject(ctx, doc, csObj)
	if err != nil {
		doc.Logger.Debug("resolving indexed color space", slog.Any("err", err))
		return nil, "", false
	}
	arr, ok := resolved.(ast.PdfArray)
	if !ok || len(arr) == 0 {
		return nil, "", false
	}
	family, err := document.ResolveObject(ctx, doc, arr[0])
	if err != nil {
		return nil, "", false
	}
	if name, ok := family.(ast.PdfName); !ok || name != "Indexed" {
		return nil, "", false
	}

	var base string
	if len(arr) >= 2 {
		base = resolveColorSpace(ctx, doc, ast.PdfDict{"ColorSpace": arr[1]})
	}
	return arr, base, true
}

// tintedColorSpace reports the family and colorant count of a Separation or
// DeviceN colour space, or ok=false for anything else.
//
// Per ISO 32000-1, 8.6.6.4 a Separation space has exactly one component, and per
// 8.6.6.5 a DeviceN space has one per name in its colorant array. Both hold INK
// TINTS rather than device colours, and [resolveColorSpace] answers "DeviceRGB"
// for them so the samples can be read for non-visual extraction.
//
// That substitution silently assumes three components. Where the real count is
// three the stride is right by accident; where it is not, the samples are strided
// wrongly. A seven-colorant DeviceN image holds seven bytes a pixel, so a
// three-byte stride passes the length check on the first 3/7 of the stream and
// reads colorants across pixel boundaries, with no error to show for it.
//
// # Corpus evidence
//
//   - govdocs: 135 images in one document were strided across pixel
//     boundaries this way, and every one of them decoded without error.
func tintedColorSpace(ctx context.Context, doc *document.Document, dict ast.PdfDict) (ast.PdfArray, string, int, bool) {
	csObj, ok := dict["ColorSpace"]
	if !ok {
		return nil, "", 0, false
	}
	resolved, err := document.ResolveObject(ctx, doc, csObj)
	if err != nil {
		doc.Logger.Debug("resolving image colour space", slog.Any("err", err))
		return nil, "", 0, false
	}
	arr, ok := resolved.(ast.PdfArray)
	if !ok || len(arr) < 2 {
		return nil, "", 0, false
	}
	family, err := document.ResolveObject(ctx, doc, arr[0])
	if err != nil {
		return nil, "", 0, false
	}
	name, ok := family.(ast.PdfName)
	if !ok {
		return nil, "", 0, false
	}

	switch name {
	case "Separation":
		return arr, "Separation", 1, true
	case "DeviceN":
		names, err := document.ResolveArray(ctx, doc, arr[1])
		if err != nil || len(names) == 0 {
			doc.Logger.Debug("resolving DeviceN colorant array", slog.Any("err", err))
			return nil, "", 0, false
		}
		return arr, "DeviceN", len(names), true
	}
	return nil, "", 0, false
}

// paintsNothing reports whether a Separation or DeviceN colour space names
// only the colorant None.
//
// Per ISO 32000-1, 8.6.6.4 the special colorant name None never produces
// visible output: painting in a Separation space over it has no effect on the
// page. Per 8.6.6.5 a DeviceN space whose colorant names are all None
// discards its output the same way and never invokes its tint transform. A
// DeviceN space that names None alongside real colorants still paints, its
// None components ignored on separations only, so it is not one of these.
func paintsNothing(ctx context.Context, doc *document.Document, cs ast.PdfArray, family string) bool {
	if len(cs) < 2 {
		return false
	}
	switch family {
	case "Separation":
		name, err := document.ResolveName(ctx, doc, cs[1])
		return err == nil && name == "None"
	case "DeviceN":
		names, err := document.ResolveArray(ctx, doc, cs[1])
		if err != nil || len(names) == 0 {
			return false
		}
		for _, obj := range names {
			name, err := document.ResolveName(ctx, doc, obj)
			if err != nil || name != "None" {
				return false
			}
		}
		return true
	}
	return false
}

// decodeNoneColorant decodes an image in a colour space that paints nothing
// (see [paintsNothing]). The picture is fully transparent: every pixel has
// zero alpha, which is what the page shows of it. The samples still exist in
// the file, so a native read returns them, one byte per colorant, with the
// /Decode array applied (ISO 32000-1, 8.9.5.2): a caller that writes them back under the same
// /ColorSpace keeps the image painting nothing, where an RGB rewrite of the
// transparent picture would paint it black.
func decodeNoneColorant(
	ctx context.Context,
	doc *document.Document,
	dict ast.PdfDict,
	data []byte,
	width, height, bitsPerComp, colorants int,
	jpeg bool,
	native *NativeSamples,
) (image.Image, error) {
	if native == nil {
		return image.NewNRGBA(image.Rect(0, 0, width, height)), nil
	}
	if colorants < 1 || colorants > maxTintColorants {
		return nil, errNotNative
	}
	// A JPEG's shape is its codestream's (see jpegSampleSpace): one that does
	// not hold one component per colorant has no tints to return.
	if jpeg && len(data) != width*height*colorants {
		return nil, errNotNative
	}
	data, validRows := padShortSamples(data, width, height, colorants, bitsPerComp)
	if validRows == 0 {
		return nil, fmt.Errorf("decoded data too short for one row of the %dx%d image: got %d bytes", width, height, len(data))
	}
	unpacked, err := unpackSamples(data, width, height, colorants, bitsPerComp, true)
	if err != nil {
		return nil, err
	}
	dec := buildDecodeLUTs(resolveDecodeArray(ctx, doc, dict, colorants), colorants)
	return nil, captureNative(native, unpacked, dec, colorants, width, height, validRows)
}

// decodeSeparation builds the image of a Separation colour space by mapping
// each one-byte tint through the space's tint transform into its alternate
// space, then to RGB.
//
// Per ISO 32000-1, 8.6.6.4 a Separation image carries ONE component whose value
// is ink coverage, and the tint transform of the colour space array converts it
// to the alternate space. litho substitutes "DeviceRGB" for the space itself so
// samples can be read for non-visual extraction, and that substitution is wrong
// twice over here: it strides three bytes where the image holds one, and where a
// JPEG's shape is measured instead it emits the tint as a grey LEVEL. A real tint
// transform maps tint 0 to paper white and full tint to near black, so the grey
// reading renders the image inverted.
//
// The transform is evaluated once per possible byte rather than once per pixel:
// the samples are 8-bit after unpacking, so 256 evaluations cover any image.
//
// # Corpus evidence
//
//   - All corpora: every tint transform measured maps tint 0 to paper white and
//     full tint to near black.
//   - All corpora: no Separation image declares a /Decode array, so its
//     /Decode tables are the identity in practice.
func decodeSeparation(
	ctx context.Context,
	doc *document.Document,
	dict ast.PdfDict,
	cs ast.PdfArray,
	data []byte,
	width, height, bitsPerComp int,
	cc colorContext,
) (image.Image, error) {
	if len(cs) < 4 {
		return nil, fmt.Errorf("separation colour space has %d entries, want [/Separation name alternate tint]", len(cs))
	}

	alternate := resolveColorSpace(ctx, doc, ast.PdfDict{"ColorSpace": cs[2]})
	altComps := imageComponents(alternate)
	if altComps == 0 {
		return nil, fmt.Errorf("separation image over unsupported alternate colour space %q", alternate)
	}

	tint, err := function.New(ctx, doc, cs[3])
	if err != nil {
		return nil, fmt.Errorf("separation image tint transform: %w", err)
	}
	if tint.Outputs() != altComps {
		return nil, fmt.Errorf("separation tint transform yields %d values for a %d-component alternate space %q",
			tint.Outputs(), altComps, alternate)
	}

	unpacked, err := unpackSamples(data, width, height, 1, bitsPerComp, true)
	if err != nil {
		return nil, err
	}
	if len(unpacked) < width*height {
		return nil, fmt.Errorf("decoded data too short for separation image: got %d, want %d", len(unpacked), width*height)
	}

	// /Decode remaps the tint before the transform sees it (ISO 32000-1,
	// 8.9.5.2). These tables are usually the identity, but a file may declare
	// one.
	dec := buildDecodeLUTs(resolveDecodeArray(ctx, doc, dict, 1), 1)

	conv := newConverter(colorTransform(ctx, doc, cs[2], alternate, altComps, renderingIntent(ctx, doc, dict), cc))
	cal := alternateCal(ctx, doc, cs[2], altComps)
	var lut [256]color.RGBA
	for s := range 256 {
		out, err := tint.Eval([]float64{float64(dec[0][s]) / 255})
		if err != nil {
			return nil, fmt.Errorf("evaluating separation tint transform: %w", err)
		}
		lut[s] = alternateToRGB(alternate, conv, cal, out)
	}

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	idx := 0
	for y := range height {
		for x := range width {
			img.Set(x, y, lut[unpacked[idx]])
			idx++
		}
	}
	return img, nil
}

// decodeDeviceN builds the image of a DeviceN colour space by mapping each
// pixel's colorant tuple through the space's tint transform into its alternate
// space, then to RGB.
//
// Per ISO 32000-1, 8.6.6.5 a DeviceN image carries one component per colorant
// and the tint transform converts the whole tuple at once, so unlike a
// Separation it cannot be reduced to a 256-entry table: the tuple space is
// 256^N. The transform is evaluated once per DISTINCT tuple instead, which is
// what makes this affordable: a real image holds far fewer distinct tuples
// than pixels.
//
// A JPEG is decoded here only when its codestream holds one component per
// colorant; [jpegSampleSpace] measures that shape.
//
// # Corpus evidence
//
//   - All corpora: the largest DeviceN image is 540x534 over seven colorants
//     yet holds only 31,400 distinct tuples, so the cache turns 36.9 million
//     corner visits into 4.0 million, a factor of nine.
//   - All corpora: no DeviceN image declares a /Decode array, so its /Decode
//     tables are the identity in practice.
func decodeDeviceN(
	ctx context.Context,
	doc *document.Document,
	dict ast.PdfDict,
	cs ast.PdfArray,
	data []byte,
	width, height, bitsPerComp, colorants int,
	cc colorContext,
) (image.Image, error) {
	if len(cs) < 4 {
		return nil, fmt.Errorf("devicen colour space has %d entries, want [/DeviceN names alternate tint]", len(cs))
	}
	if colorants < 1 || colorants > maxTintColorants {
		return nil, fmt.Errorf("devicen image declares %d colorants, outside the 1 to %d this decoder evaluates",
			colorants, maxTintColorants)
	}
	if work := int64(width) * int64(height) * int64(1) << uint(colorants); work > maxDeviceNWork {
		return nil, fmt.Errorf("devicen image of %dx%d over %d colorants needs up to %d tint evaluations, above the %d safety cap",
			width, height, colorants, work, int64(maxDeviceNWork))
	}

	alternate := resolveColorSpace(ctx, doc, ast.PdfDict{"ColorSpace": cs[2]})
	altComps := imageComponents(alternate)
	if altComps == 0 {
		return nil, fmt.Errorf("devicen image over unsupported alternate colour space %q", alternate)
	}

	tint, err := function.New(ctx, doc, cs[3])
	if err != nil {
		return nil, fmt.Errorf("devicen image tint transform: %w", err)
	}
	// Per ISO 32000-1, 8.6.6.5 the transform takes one input per colorant.
	// Checking here names the colour space and both counts; letting the
	// mismatch reach function.Function.Eval instead reports a bare arity
	// error that mentions neither.
	if tint.Inputs() != colorants {
		return nil, fmt.Errorf("devicen tint transform takes %d inputs for a %d-colorant space",
			tint.Inputs(), colorants)
	}
	if tint.Outputs() != altComps {
		return nil, fmt.Errorf("devicen tint transform yields %d values for a %d-component alternate space %q",
			tint.Outputs(), altComps, alternate)
	}

	unpacked, err := unpackSamples(data, width, height, colorants, bitsPerComp, true)
	if err != nil {
		return nil, err
	}
	if len(unpacked) < width*height*colorants {
		return nil, fmt.Errorf("decoded data too short for devicen image: got %d, want %d",
			len(unpacked), width*height*colorants)
	}

	// /Decode remaps each colorant before the transform sees it (ISO 32000-1,
	// 8.9.5.2). These tables are usually the identity, but a file may declare
	// one.
	dec := buildDecodeLUTs(resolveDecodeArray(ctx, doc, dict, colorants), colorants)

	conv := newConverter(colorTransform(ctx, doc, cs[2], alternate, altComps, renderingIntent(ctx, doc, dict), cc))
	cal := alternateCal(ctx, doc, cs[2], altComps)
	memo := make(map[string]color.RGBA)
	inputs := make([]float64, colorants)
	tuple := make([]byte, colorants)

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	offset := 0
	for y := range height {
		for x := range width {
			for c := range colorants {
				tuple[c] = dec[c][unpacked[offset+c]]
			}
			offset += colorants

			key := string(tuple)
			rgb, seen := memo[key]
			if !seen {
				for c := range colorants {
					inputs[c] = float64(tuple[c]) / 255
				}
				out, err := tint.Eval(inputs)
				if err != nil {
					return nil, fmt.Errorf("evaluating devicen tint transform: %w", err)
				}
				rgb = alternateToRGB(alternate, conv, cal, out)
				if len(memo) < maxTintMemo {
					memo[key] = rgb
				}
			}
			img.Set(x, y, rgb)
		}
	}
	return img, nil
}

// tintTable converts the colour table of an indexed space over a Separation or
// DeviceN space, whose entries hold one tint per colorant, into RGB entries
// through the space's tint transform (ISO 32000-1, 8.6.6.4 and 8.6.6.5).
// Reading the tints as RGB, three bytes an entry, misreads every entry of a
// table whose colorant count is not three.
//
// # Corpus evidence
//
//   - govdocs thread_300_300659, thread_400_400932: reading the tints as RGB
//     misread every entry of their five-colorant tables.
func tintTable(ctx context.Context, doc *document.Document, cs ast.PdfArray, colorants int, table []byte, intent icc.Intent, cc colorContext) ([]byte, error) {
	if len(cs) < 4 {
		return nil, fmt.Errorf("tinted colour space has %d entries, want [family names alternate tint]", len(cs))
	}
	alternate := resolveColorSpace(ctx, doc, ast.PdfDict{"ColorSpace": cs[2]})
	altComps := imageComponents(alternate)
	if altComps == 0 {
		return nil, fmt.Errorf("tinted colour space over unsupported alternate colour space %q", alternate)
	}
	tint, err := function.New(ctx, doc, cs[3])
	if err != nil {
		return nil, fmt.Errorf("tint transform: %w", err)
	}
	if tint.Inputs() != colorants || tint.Outputs() != altComps {
		return nil, fmt.Errorf("tint transform maps %d inputs to %d values for %d colorants over %q",
			tint.Inputs(), tint.Outputs(), colorants, alternate)
	}
	conv := newConverter(colorTransform(ctx, doc, cs[2], alternate, altComps, intent, cc))
	cal := alternateCal(ctx, doc, cs[2], altComps)
	out := make([]byte, 0, len(table)/colorants*3)
	in := make([]float64, colorants)
	for i := 0; i+colorants <= len(table); i += colorants {
		for c := range colorants {
			in[c] = float64(table[i+c]) / 255
		}
		v, err := tint.Eval(in)
		if err != nil {
			return nil, fmt.Errorf("evaluating tint transform: %w", err)
		}
		rgb := alternateToRGB(alternate, conv, cal, v)
		out = append(out, rgb.R, rgb.G, rgb.B)
	}
	return out, nil
}

// alternateCal returns the CalGray or CalRGB space the alternate obj of a
// Separation or DeviceN space names, or nil for any other alternate or one
// whose component count is not comps.
//
// ISO 32000-1, 8.6.6.4 lets the alternate be any device or CIE-based space,
// and [resolveColorSpace] names a CalRGB or CalGray one DeviceRGB or
// DeviceGray for its component count. Reading the transform's output in that
// device space drops the gamma and the matrix, the colour the alternate
// actually states.
//
// # Corpus evidence
//
//   - pdfjs issue9940.pdf: two images indexed over a DeviceN space whose
//     alternate is a CalRGB with XYZ for its primaries came out up to 17
//     levels off PDF.js read as DeviceRGB, and within 3 read through it.
//   - govdocs thread_200_200169.pdf: Separation /Black images over an sRGB-like
//     CalRGB alternate moved from 3.4 levels off PDF.js to within 3.
func alternateCal(ctx context.Context, doc *document.Document, obj ast.PdfObject, comps int) *calSpace {
	cal, ok := resolveCalSpace(ctx, doc, obj)
	if !ok || calComponents(cal) != comps {
		return nil
	}
	return cal
}

// alternateToRGB converts one colour expressed in an alternate space to RGB,
// through conv when the alternate space is colour managed and through cal
// when it is CalGray or CalRGB. Values arrive in [0, 1] as the tint
// transform's /Range defines them.
func alternateToRGB(alternate string, conv *converter, cal *calSpace, v []float64) color.RGBA {
	unit := func(f float64) uint8 {
		return uint8(min(max(f, 0), 1)*255 + 0.5)
	}
	if conv != nil {
		var px [4]byte
		for i := range px[:conv.t.Components()] {
			px[i] = unit(v[i])
		}
		r, g, b := conv.convert(px[:conv.t.Components()])
		return color.RGBA{R: r, G: g, B: b, A: 255}
	}
	if cal != nil {
		if cal.grey {
			g := cal.convertGrey(unit(v[0]))
			return color.RGBA{R: g, G: g, B: g, A: 255}
		}
		r, g, b := cal.convertRGB(unit(v[0]), unit(v[1]), unit(v[2]))
		return color.RGBA{R: r, G: g, B: b, A: 255}
	}
	switch alternate {
	case "DeviceGray":
		g := unit(v[0])
		return color.RGBA{R: g, G: g, B: g, A: 255}
	case "DeviceCMYK":
		c, m, yy, k := min(max(v[0], 0), 1), min(max(v[1], 0), 1), min(max(v[2], 0), 1), min(max(v[3], 0), 1)
		return color.RGBA{
			R: uint8((1 - c) * (1 - k) * 255),
			G: uint8((1 - m) * (1 - k) * 255),
			B: uint8((1 - yy) * (1 - k) * 255),
			A: 255,
		}
	default:
		return color.RGBA{R: unit(v[0]), G: unit(v[1]), B: unit(v[2]), A: 255}
	}
}

// indexedBaseComponents returns the samples-per-pixel of an indexed image's
// BASE colour space.
//
// It is deliberately not [imageComponents]. That one answers for the space the
// samples are written in, and a base litho cannot read must keep answering 0
// there so a direct image in it is refused rather than misread. An indexed
// image is different: its samples are indices, and the base count sizes the
// colour TABLE, not the sample stride. Per ISO 32000-1, 8.6.5 Lab takes three
// components, which is the count remediation.colorSpaceComponents already
// assigns it.
func indexedBaseComponents(base string) int {
	if base == "Lab" {
		return 3
	}
	return imageComponents(base)
}

// labParams carries the parts of a [/Lab] colour space dictionary the palette
// needs, per ISO 32000-1, 8.6.5.4.
//
// # Corpus evidence
//
//   - pdfjs: the two documents that exposed the /Range handling both declare
//     [-128 127 -128 127].
type labParams struct {
	// whiteX, whiteY, whiteZ are /WhitePoint, the diffuse white the L*a*b*
	// values are relative to. Required by the spec, so an absent one leaves
	// the palette unresolved rather than guessing an illuminant.
	whiteX, whiteY, whiteZ float64
	// aMin, aMax, bMin, bMax are /Range. Its default is [-100 100 -100 100],
	// but real files carry other spans, so the declared one is read.
	aMin, aMax, bMin, bMax float64
}

// palette holds a resolved indexed colour space: the device space its entries
// are written in, and the lookup table itself.
type palette struct {
	base   string
	comps  int
	lookup []byte
	// lab is set only when base is "Lab". Its entries are not device values,
	// so they cannot be written out as RGB the way every other base can.
	lab labParams
}

// resolveIndexedPalette reports the palette of an [/Indexed base hival lookup]
// colour space, and whether the image has one at all.
//
// Per ISO 32000-1, 8.6.6.3 an indexed image stores one sample per pixel, and
// that sample is an index into the table rather than a colour. Reading it as
// though it were a colour in the base space is what [resolveColorSpace] implies
// on its own, and the decoder then wanted three or four bytes per pixel for
// data that holds one: every indexed image failed with "decoded data too short",
// so no palette image could be decoded at all.
//
// # Corpus evidence
//
//   - pdfjs 160F-2019.pdf, S2.pdf: among the indexed images that failed with
//     "decoded data too short" before the palette was resolved.
func resolveIndexedPalette(ctx context.Context, doc *document.Document, dict ast.PdfDict, cc colorContext) (palette, bool) {
	arr, base, ok := indexedColorSpace(ctx, doc, dict)
	if !ok || len(arr) < 4 {
		return palette{}, false
	}
	comps := indexedBaseComponents(base)
	if comps == 0 {
		return palette{}, false
	}
	// A Separation or DeviceN base, which resolveColorSpace names DeviceRGB,
	// holds one tint per colorant in each entry (ISO 32000-1, 8.6.6.3).
	tintArr, _, colorants, tinted := tintedColorSpace(ctx, doc, ast.PdfDict{"ColorSpace": arr[1]})
	if tinted {
		comps = colorants
	}

	var lab labParams
	if base == "Lab" {
		lab, ok = resolveLabParams(ctx, doc, arr[1])
		if !ok {
			return palette{}, false
		}
	}

	hival, isNum := document.Number(mustResolve(ctx, doc, arr[2]))
	if !isNum || hival < 0 || int(hival) >= maxPaletteEntries {
		return palette{}, false
	}

	lookupObj, err := document.ResolveObject(ctx, doc, arr[3])
	if err != nil {
		doc.Logger.Debug("resolving indexed lookup table", slog.Any("err", err))
		return palette{}, false
	}
	var lookup []byte
	switch v := lookupObj.(type) {
	case ast.PdfString:
		lookup = []byte(v)
	case ast.PdfStream:
		lookup, err = doc.DecodeStream(ctx, v)
		if err != nil {
			doc.Logger.Debug("decoding indexed lookup stream", slog.Any("err", err))
			return palette{}, false
		}
	default:
		return palette{}, false
	}

	// A short table is not fatal: an index past its end reads as black, which
	// is what a viewer shows for an out-of-range index.
	want := (int(hival) + 1) * comps
	if len(lookup) > want {
		lookup = lookup[:want]
	}
	intent := renderingIntent(ctx, doc, dict)
	if tinted {
		rgb, err := tintTable(ctx, doc, tintArr, colorants, lookup, intent, cc)
		if err != nil {
			doc.Logger.Debug("converting an indexed colour table of tints", slog.Any("err", err))
			return palette{}, false
		}
		return palette{base: "DeviceRGB", comps: 3, lookup: rgb}, true
	}
	// An ICCBased or DeviceCMYK base holds entries in a profiled space, which
	// are converted once here. A short table is padded in the base space
	// first, so its missing entries read as the base's zero, as they did
	// before conversion.
	if conv := newConverter(colorTransform(ctx, doc, arr[1], base, comps, intent, cc)); conv != nil {
		if len(lookup) < want {
			padded := make([]byte, want)
			copy(padded, lookup)
			lookup = padded
		}
		return palette{base: "DeviceRGB", comps: 3, lookup: conv.convertTable(lookup)}, true
	}
	// A CalRGB or CalGray base, which resolveColorSpace names DeviceRGB or
	// DeviceGray, holds CIE-based entries: convert them once here, into a copy,
	// since a decoded lookup stream may be shared.
	if cal, ok := resolveCalSpace(ctx, doc, arr[1]); ok && comps == calComponents(cal) {
		lookup = cal.convertTable(lookup)
	}
	return palette{base: base, comps: comps, lookup: lookup, lab: lab}, true
}

// mustResolve resolves obj, returning it unchanged when resolution fails. The
// callers below treat a non-numeric result as "not an indexed space" anyway.
func mustResolve(ctx context.Context, doc *document.Document, obj ast.PdfObject) ast.PdfObject {
	resolved, err := document.ResolveObject(ctx, doc, obj)
	if err != nil {
		return obj
	}
	return resolved
}

// entry returns the base-space components of palette index i, padded with zero
// (black) when the table is shorter than the index demands.
func (p palette) entry(i byte) [4]byte {
	var out [4]byte
	off := int(i) * p.comps
	for c := range p.comps {
		if off+c < len(p.lookup) {
			out[c] = p.lookup[off+c]
		}
	}
	return out
}

// resolveLabParams reads the /WhitePoint and /Range of a [/Lab dict] colour
// space, and reports whether it is usable as an indexed base.
//
// /WhitePoint is required by ISO 32000-1, Table 65, and the conversion is
// meaningless without an illuminant, so an absent or malformed one leaves the
// palette unresolved rather than substituting a guess. /Range is optional and
// defaults to [-100 100 -100 100] per the same table.
func resolveLabParams(ctx context.Context, doc *document.Document, obj ast.PdfObject) (labParams, bool) {
	arr, err := document.ResolveArray(ctx, doc, obj)
	if err != nil || len(arr) < 2 {
		doc.Logger.Debug("resolving indexed /Lab base colour space", slog.Any("err", err))
		return labParams{}, false
	}
	dict, err := document.ResolveDict(ctx, doc, arr[1])
	if err != nil || dict == nil {
		doc.Logger.Debug("resolving /Lab colour space dictionary", slog.Any("err", err))
		return labParams{}, false
	}

	white, err := document.ResolveArray(ctx, doc, dict["WhitePoint"])
	if err != nil || len(white) != 3 {
		doc.Logger.Debug("ignoring /Lab colour space with no usable /WhitePoint",
			slog.Int("len", len(white)))
		return labParams{}, false
	}
	p := labParams{
		whiteX: document.Float(white[0]),
		whiteY: document.Float(white[1]),
		whiteZ: document.Float(white[2]),
		aMin:   -100, aMax: 100, bMin: -100, bMax: 100,
	}
	// A zero or negative tristimulus scales every component to zero or flips
	// its sign, so the whole image would read as black or as nonsense.
	if p.whiteX <= 0 || p.whiteY <= 0 || p.whiteZ <= 0 {
		doc.Logger.Debug("ignoring /Lab colour space with a non-positive /WhitePoint")
		return labParams{}, false
	}

	if rng, err := document.ResolveArray(ctx, doc, dict["Range"]); err == nil && len(rng) == 4 {
		p.aMin, p.aMax = document.Float(rng[0]), document.Float(rng[1])
		p.bMin, p.bMax = document.Float(rng[2]), document.Float(rng[3])
	}
	return p, true
}

// labEntryToRGB converts one palette entry written in a Lab space to sRGB.
//
// The entry bytes are scaled onto the component ranges per ISO 32000-1, 8.6.6.3
// (L* over [0, 100], a* and b* over /Range), turned into XYZ by the g() of
// 8.6.5.4, and then matrixed to sRGB. The matrix is chosen by the declared
// white rather than adapted to one: no chromatic adaptation is applied, which
// is what PDF.js does for the same construct.
//
// Clamping before the transfer function is load-bearing rather than tidy. The
// matrix yields negative channels for colours outside the sRGB gamut, which
// real palettes hold, math.Sqrt of a negative is NaN, and a float-to-uint8
// conversion outside [0, 255] is not defined to saturate: uint8(-1.0) is 255,
// so an out-of-gamut entry would come out as its opposite.
func labEntryToRGB(e [4]byte, p labParams) (uint8, uint8, uint8) {
	decode := func(v byte, lo, hi float64) float64 {
		return lo + float64(v)*(hi-lo)/255
	}
	lStar := decode(e[0], 0, 100)
	aStar := decode(e[1], p.aMin, p.aMax)
	bStar := decode(e[2], p.bMin, p.bMax)

	g := func(x float64) float64 {
		if x >= 6.0/29.0 {
			return x * x * x
		}
		return (108.0 / 841.0) * (x - 4.0/29.0)
	}
	m := (lStar + 16) / 116
	x := p.whiteX * g(m+aStar/500)
	y := p.whiteY * g(m)
	z := p.whiteZ * g(m-bStar/200)

	var r, gr, b float64
	if p.whiteZ < 1 {
		r = x*3.1339 + y*-1.617 + z*-0.4906
		gr = x*-0.9785 + y*1.916 + z*0.0333
		b = x*0.072 + y*-0.229 + z*1.4057
	} else {
		r = x*3.2406 + y*-1.5372 + z*-0.4986
		gr = x*-0.9689 + y*1.8758 + z*0.0415
		b = x*0.0557 + y*-0.204 + z*1.057
	}

	channel := func(v float64) uint8 {
		v = min(max(v, 0), 1)
		return uint8(math.Sqrt(v) * 255)
	}
	return channel(r), channel(gr), channel(b)
}

// decodeIndexed builds the image of an indexed colour space, mapping each
// one-byte sample through the palette into the base space.
func decodeIndexed(data []byte, width, height int, pal palette) (image.Image, error) {
	if len(data) < width*height {
		return nil, fmt.Errorf("decoded data too short for indexed image: got %d, want %d", len(data), width*height)
	}
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	idx := 0
	for y := range height {
		for x := range width {
			e := pal.entry(data[idx])
			idx++
			switch pal.base {
			case "DeviceGray":
				img.Set(x, y, color.RGBA{R: e[0], G: e[0], B: e[0], A: 255})
			case "DeviceCMYK":
				c, m, yy, k := float64(e[0])/255, float64(e[1])/255, float64(e[2])/255, float64(e[3])/255
				img.Set(x, y, color.RGBA{
					R: uint8((1 - c) * (1 - k) * 255),
					G: uint8((1 - m) * (1 - k) * 255),
					B: uint8((1 - yy) * (1 - k) * 255),
					A: 255,
				})
			case "Lab":
				r, g, b := labEntryToRGB(e, pal.lab)
				img.Set(x, y, color.RGBA{R: r, G: g, B: b, A: 255})
			default:
				img.Set(x, y, color.RGBA{R: e[0], G: e[1], B: e[2], A: 255})
			}
		}
	}
	return img, nil
}

// DecodeImage decodes a named Image XObject a page shows into a Go
// image.Image. The name is a resource key, such as "Im0".
//
// A name the page's own /Resources /XObject dictionary holds names that image.
// Otherwise the name is looked up as [ExtractImagesRecursive] lists the page's
// images, through form XObjects, patterns, soft masks and annotation
// appearances, and the first image of that name is decoded: callers pick names
// from that listing, and an image drawn through a form is not in the page's
// own dictionary.
func DecodeImage(ctx context.Context, doc *document.Document, page *document.Page, name ast.PdfName) (image.Image, error) {
	stream, cc, err := lookupImage(ctx, doc, page, name)
	if err != nil {
		return nil, err
	}
	return decodeImageStream(ctx, doc, name, stream, cc)
}

// DecodeImageSamples decodes a named Image XObject as [DecodeImage] does, but
// leaves an image whose soft mask carries a /Matte as its samples hold it,
// preblended with the matte colour. A caller that writes the pixels back
// beside the same soft mask needs them that way: undone, a viewer would undo
// the preblending a second time.
func DecodeImageSamples(ctx context.Context, doc *document.Document, page *document.Page, name ast.PdfName) (image.Image, error) {
	stream, cc, err := lookupImage(ctx, doc, page, name)
	if err != nil {
		return nil, err
	}
	return decodeImagePixels(ctx, doc, name, stream, cc)
}

// DecodeStreamSamples is [DecodeImageSamples] for an image stream the caller
// already holds, read against page's own resources.
//
// A caller that edits an image and then edits it again has to read its own
// earlier result, which exists only in its allocator and which a lookup by name
// through the document would miss: it would return the original, and the
// second edit would silently undo the first.
func DecodeStreamSamples(ctx context.Context, doc *document.Document, page *document.Page, name ast.PdfName, stream ast.PdfStream) (image.Image, error) {
	return decodeImagePixels(ctx, doc, name, stream, pageColorContext(page))
}

// NativeSamples are the samples of an image in its own colour space: one byte
// per component per pixel, in rows, with the image's /Decode array applied.
type NativeSamples struct {
	Data                      []byte
	Components, Width, Height int
}

// errNotNative reports an image whose samples do not come out in the colour
// space its dictionary names.
var errNotNative = errors.New("image samples are not in a device colour space")

// DecodeImageNative decodes a named Image XObject into the samples of its own
// colour space, where that space is DeviceGray, DeviceRGB, DeviceCMYK, an
// ICCBased space over one, three or four components, CalGray or CalRGB, or a
// Separation or DeviceN space naming only the colorant None, and reports false
// for any other image: indexed and Lab images, any other Separation or DeviceN
// image, stencil masks, and an image whose data does not hold the components
// its space names. A caller that writes the samples back under the same
// /ColorSpace changes no colour, where going through [DecodeImage]'s RGB
// converts them.
func DecodeImageNative(ctx context.Context, doc *document.Document, page *document.Page, name ast.PdfName) (NativeSamples, bool, error) {
	stream, cc, err := lookupImage(ctx, doc, page, name)
	if err != nil {
		return NativeSamples{}, false, err
	}
	return decodeNative(ctx, doc, name, stream, cc)
}

// DecodeStreamNative is [DecodeImageNative] for an image stream the caller
// already holds, read against page's own resources. See [DecodeStreamSamples]
// for why a caller needs it.
func DecodeStreamNative(ctx context.Context, doc *document.Document, page *document.Page, name ast.PdfName, stream ast.PdfStream) (NativeSamples, bool, error) {
	return decodeNative(ctx, doc, name, stream, pageColorContext(page))
}

// pageColorContext reads an image against the resources and dictionary of the
// page that paints it.
func pageColorContext(page *document.Page) colorContext {
	return colorContext{resources: page.Resources, page: page.Dict}
}

func decodeNative(ctx context.Context, doc *document.Document, name ast.PdfName, stream ast.PdfStream, cc colorContext) (NativeSamples, bool, error) {
	want, ok := nativeComponents(ctx, doc, stream.Dict)
	if !ok {
		return NativeSamples{}, false, nil
	}
	var native NativeSamples
	if _, err := decodeImageInto(ctx, doc, name, stream, cc, &native); err != nil {
		if errors.Is(err, errNotNative) {
			return NativeSamples{}, false, nil
		}
		return NativeSamples{}, false, err
	}
	if native.Components != want {
		return NativeSamples{}, false, nil
	}
	return native, true, nil
}

// nativeComponents returns the component count of an image dictionary's
// /ColorSpace when it is a space whose samples DecodeImageNative returns.
func nativeComponents(ctx context.Context, doc *document.Document, dict ast.PdfDict) (int, bool) {
	// An /ImageMask that cannot be resolved counts as absent, as the decode
	// treats it.
	if mask, _ := document.ResolveBool(ctx, doc, dict["ImageMask"]); mask {
		return 0, false
	}
	obj, ok := dict["ColorSpace"]
	if !ok {
		return 0, false
	}
	switch cs := mustResolve(ctx, doc, obj).(type) {
	case ast.PdfName:
		n := imageComponents(string(cs))
		return n, n > 0
	case ast.PdfArray:
		if len(cs) < 2 {
			return 0, false
		}
		family, _ := mustResolve(ctx, doc, cs[0]).(ast.PdfName)
		switch family {
		case "ICCBased":
			n := imageComponents(iccBasedSpace(ctx, doc, cs))
			return n, n > 0
		case "CalRGB":
			return 3, true
		case "CalGray":
			return 1, true
		case "Separation", "DeviceN":
			// A space that paints nothing keeps its samples as tints, and
			// writing them back under the same /ColorSpace keeps it painting
			// nothing (see decodeNoneColorant).
			if arr, family, colorants, ok := tintedColorSpace(ctx, doc, dict); ok && paintsNothing(ctx, doc, arr, family) {
				return colorants, colorants <= maxTintColorants
			}
		}
	}
	return 0, false
}

// captureNative fills native with the samples of a device reading, /Decode
// applied, and paints the rows short data lacked black in that space.
func captureNative(native *NativeSamples, data []byte, dec [][256]byte, components, width, height, validRows int) error {
	if components == 0 || len(data) < width*height*components {
		return errNotNative
	}
	out := make([]byte, width*height*components)
	for i := range out {
		out[i] = dec[i%components][data[i]]
	}
	black := make([]byte, components)
	if components == 4 {
		black[3] = 255
	}
	for i := validRows * width * components; i < len(out); i += components {
		copy(out[i:i+components], black)
	}
	*native = NativeSamples{Data: out, Components: components, Width: width, Height: height}
	return nil
}

// lookupImage finds the image stream DecodeImage decodes.
func lookupImage(ctx context.Context, doc *document.Document, page *document.Page, name ast.PdfName) (ast.PdfStream, colorContext, error) {
	if xobjDict, err := document.ResolveDict(ctx, doc, page.Resources["XObject"]); err == nil && xobjDict != nil {
		if obj, ok := xobjDict[name]; ok {
			stream, err := document.ResolveStream(ctx, doc, obj)
			if err != nil {
				return ast.PdfStream{}, colorContext{}, err
			}
			if stream.Dict == nil {
				return ast.PdfStream{}, colorContext{}, fmt.Errorf("image %q is not a stream", string(name))
			}
			return stream, pageColorContext(page), nil
		}
	}
	if stream, resources, ok := findImage(ctx, doc, page, string(name)); ok {
		return stream, colorContext{resources: resources, page: page.Dict}, nil
	}
	if err := ctx.Err(); err != nil {
		return ast.PdfStream{}, colorContext{}, err
	}
	return ast.PdfStream{}, colorContext{}, fmt.Errorf("image %q: %w", string(name), ErrImageNotFound)
}

// decodeImageStream decodes the Image XObject stream that name refers to.
func decodeImageStream(ctx context.Context, doc *document.Document, name ast.PdfName, stream ast.PdfStream, cc colorContext) (image.Image, error) {
	img, err := decodeImagePixels(ctx, doc, name, stream, cc)
	if err != nil {
		return nil, err
	}
	return unpreblend(ctx, doc, name, stream.Dict, cc, img), nil
}

// decodeImagePixels decodes the samples of an Image XObject stream into
// pixels, taking no account of its soft mask.
func decodeImagePixels(ctx context.Context, doc *document.Document, name ast.PdfName, stream ast.PdfStream, cc colorContext) (image.Image, error) {
	return decodeImageInto(ctx, doc, name, stream, cc, nil)
}

// decodeImageInto decodes the samples of an Image XObject stream. With native
// nil it returns them as pixels. Otherwise it stops before any colour
// conversion and fills native with the samples of a DeviceGray, DeviceRGB or
// DeviceCMYK reading, /Decode applied, or reports errNotNative when the
// samples do not come out that way.
//
// # Corpus evidence
//
//   - pdfcpu Wonderwall.pdf: a 2312x3307 stencil mask carries 955,723 bytes
//     and was asked for 2,867,169 while a missing /ColorSpace drove the
//     unpacking.
//   - litho jpx_2b.pdf: holds seven 8x8 JPXDecode images of 212 to 235
//     encoded bytes against the 192 an RGB read wants, and while the filter
//     pipeline skipped JPXDecode each decoded into a picture made of
//     codestream bytes.
//   - pdfjs: 4 images whose indexed base is /Lab were reported "too short for
//     RGB image" when read in the base space.
//   - All corpora: every tint transform measured maps tint 0 to paper white
//     and full tint to near black.
//   - govdocs thread_000_000340: reading its one-colorant /Black JPEGs as
//     DeviceGray inverted both images against PDF.js, which applies the tint
//     transform.
//   - govdocs: the one direct Lab image in the corpora is a JPEG, whose white
//     came out pink when read as RGB.
//   - govdocs: painting the rows short data holds, with the filters keeping
//     what a truncated stream holds, took the pages PDF.js paints images on
//     that litho failed to decode from 375 to 3.
func decodeImageInto(ctx context.Context, doc *document.Document, name ast.PdfName, stream ast.PdfStream, cc colorContext, native *NativeSamples) (image.Image, error) {
	// A default colour space in the painting resources stands in for a device
	// space from here on, for every reading of the colour space below.
	stream.Dict = cc.withDefaults(ctx, doc, stream.Dict)

	width, err := resolveInt(ctx, doc, stream.Dict, "Width")
	if err != nil {
		return nil, err
	}

	height, err := resolveInt(ctx, doc, stream.Dict, "Height")
	if err != nil {
		return nil, err
	}

	lim := doc.Limits()
	if err := validateImageDimensions(width, height, lim.MaxImagePixels); err != nil {
		return nil, err
	}

	colorSpace := resolveColorSpace(ctx, doc, stream.Dict)
	bitsPerComp := statedBitsPerComponent(ctx, doc, stream.Dict)

	// resolveColorSpace answers DeviceRGB for a missing /ColorSpace, but a
	// JPXDecode image may leave it out and take its colour from the JPEG 2000
	// data instead (ISO 32000-1, 7.4.9).
	var jpxCol jpxColour
	if _, stated := stream.Dict["ColorSpace"]; !stated && usesJPXDecode(ctx, doc, stream.Dict) {
		jpxCol, err = jpxColorSpace(ctx, doc, stream, renderingIntent(ctx, doc, stream.Dict))
		if err != nil {
			return nil, fmt.Errorf("image %q: %w", string(name), err)
		}
		colorSpace = jpxCol.space
	}

	// A stencil mask is not a picture. ISO 32000-1, 8.9.6.2 gives an image mask
	// exactly one component at one bit per sample and forbids /ColorSpace, so
	// neither an absent entry nor a wrongly present one may drive the unpacking.
	// resolveColorSpace answers "DeviceRGB" when the entry is missing, which
	// strides the samples three times too far, so every complete stencil
	// failed as "decoded data too short" while one padded to the larger size
	// decoded with its mask bits read as colour components.
	//
	// DeviceGray is the decode-side answer, not a claim about the image's colour:
	// under the default /Decode [0 1] a 0 sample reads as painted and a 1 as
	// unpainted, which is the bitmap an OCR engine wants. What the image REPORTS
	// is untouched, since imageMeta reads the dictionary itself.
	//
	// The error is discarded deliberately: an /ImageMask that cannot be resolved
	// is treated as absent, leaving the image decoded as its dictionary
	// describes, which is exactly the behaviour before this branch existed.
	isMask, _ := document.ResolveBool(ctx, doc, stream.Dict["ImageMask"])
	if isMask {
		colorSpace, bitsPerComp = "DeviceGray", 1
	}

	filterName := imageFilterName(ctx, doc, stream.Dict)

	// Stream through filters and read all decoded bytes for pixel construction
	// below. Streaming decompression avoids holding both compressed and
	// decompressed copies in memory simultaneously.
	//
	// The checked reader refuses a filter litho cannot decode, where the
	// filter pipeline skips it and returns its input still encoded, so every
	// length check below would compare codestream bytes against a sample
	// count. A short codestream was reported as "decoded data too short",
	// blaming the file for a decoder litho lacks. One long enough passed and
	// was read as pixels with no error at all, giving a picture made of
	// codestream bytes. A built-in decoder that meets a feature it lacks, a
	// JPEG 2000 construct among them, is refused the same way.
	//
	// A JPEG comes through here too. It used to take a branch of its own that
	// handed the stream to image/jpeg, on the premise that the filter pipeline
	// passed DCTDecode through untouched. It does not: filter.Decodable lists
	// DCTDecode, and the pipeline runs decodeDCT, which returns decoded samples.
	// image/jpeg was therefore being asked to decode already-decoded bytes and
	// reported a missing SOI marker, so no JPEG decoded at all.
	rc, err := doc.NewStreamReaderChecked(ctx, stream)
	if errors.Is(err, filter.ErrUnsupportedFilter) {
		return nil, fmt.Errorf("image %q cannot be decoded: %w", string(name), err)
	}
	if err != nil {
		return nil, fmt.Errorf("opening image stream: %w", err)
	}
	// Cap the decoded read so a flate/lzw bomb cannot blow memory
	// before the dimension check below catches the mismatch. The +1 makes
	// "exactly maxImageBytes" valid while detecting overflow.
	maxImageBytes := maxBytesPerPixel * lim.MaxImagePixels
	decodedData, err := io.ReadAll(io.LimitReader(rc, maxImageBytes+1))
	if closeErr := rc.Close(); closeErr != nil {
		doc.Logger.Debug("closing image stream reader", slog.Any("err", closeErr))
	}
	if err != nil {
		return nil, fmt.Errorf("reading decoded image stream: %w", err)
	}
	if int64(len(decodedData)) > maxImageBytes {
		return nil, fmt.Errorf("%w: its decoded samples exceed %d bytes", ErrImageTooLarge, maxImageBytes)
	}
	if jpxCol.ycc && !bool(isMask) {
		yccToRGB(decodedData, jpxCol.neutral)
	}
	if jpxCol.convert != nil && !bool(isMask) {
		jpxCol.convert(decodedData)
	}

	// Resolved before the samples are unpacked, because an indexed image holds
	// ONE sample per pixel whatever its base space is, while resolveColorSpace
	// reports that base: [/Indexed /DeviceRGB ...] reads back as "DeviceRGB".
	// Taking the sample count from the base would stride packed samples three
	// or four times too far and mis-read every sub-byte indexed image.
	//
	// An indexed image's /Decode array remaps the index range, not colour
	// components (ISO 32000-1, 8.9.5.2): see indexDecode.
	pal, isIndexed := resolveIndexedPalette(ctx, doc, stream.Dict, cc)
	if !isIndexed {
		// An unreadable colour table leaves an indexed image with nothing to
		// decode, and its samples are indices either way (ISO 32000-1, 8.6.6.3).
		// Reading them in the base space instead strides three or four times too
		// far, so complete data was reported as "too short for RGB image: got N,
		// want 3N". That blames the file for a base colour space litho does not
		// support. A /Lab base is the case that exposed it: it collapses to
		// DeviceRGB in resolveColorSpace because no component count is known
		// for it.
		if _, base, indexed := indexedColorSpace(ctx, doc, stream.Dict); indexed {
			return nil, fmt.Errorf("indexed image over base color space %q has no readable color table", base)
		}
	}

	// The samples of a JPEG are described by the codestream, not the dictionary,
	// so their shape is measured before anything is built from it. Their depth
	// is likewise fixed: decodeDCT returns one byte per component, and
	// ISO 32000-1, Table 89 requires 8 for a DCTDecode image in any case, so a
	// dictionary claiming another depth must not drive the unpacking.
	if filterName == "DCTDecode" {
		if isIndexed {
			// An indexed image holds one index per pixel, but decodeDCT returns
			// whatever the codestream held. A colour JPEG yields three bytes a
			// pixel, and decodeIndexed would then read colour components as
			// indices: its own length check passes, so the picture would come
			// out wrong with no error at all. Only a one-byte-per-pixel
			// codestream can be carrying indices.
			if len(decodedData) != width*height {
				return nil, fmt.Errorf("indexed JPEG decoded to %d bytes, which is not one index per pixel of %dx%d",
					len(decodedData), width, height)
			}
		} else {
			measured, ok := jpegSampleSpace(len(decodedData), width, height)
			if !ok {
				return nil, fmt.Errorf("decoded JPEG is %d bytes, which is not 1, 3 or 4 per pixel of %dx%d",
					len(decodedData), width, height)
			}
			// A Lab JPEG holds its three components as they are: the shape
			// confirms the space rather than replacing it.
			if colorSpace != "Lab" || measured != "DeviceRGB" {
				colorSpace = measured
			}
		}
		bitsPerComp = 8
	}

	// A Separation or DeviceN image holds ink tints per pixel, not colours, so
	// it is decoded through its tint transform into the alternate space rather
	// than through the "DeviceRGB" resolveColorSpace substitutes for it
	// (ISO 32000-1, 8.6.6.4 and 8.6.6.5). This runs before the sample stride is
	// taken from colorSpace because that substitution is what the transform
	// replaces: it assumes three components, which strides a one-colorant image
	// three times too far and reads a seven-colorant one across pixel
	// boundaries.
	//
	// The two families differ on JPEGs. A Separation is decoded whatever its
	// filter, because jpegSampleSpace relabels a one-byte-per-pixel codestream
	// "DeviceGray" and would otherwise emit the tint as luminance, rendering the
	// image inverted: a real tint transform maps tint 0 to paper white and full
	// tint to near black. A DeviceN JPEG is decoded here when its codestream
	// holds one component per colorant, and left to the ordinary path
	// otherwise, since the samples cannot then be tints. Reading a
	// one-colorant /Black JPEG as DeviceGray inverts it against PDF.js, which
	// applies the tint transform.
	//
	// An indexed image is excluded throughout: its samples are indices and the
	// base count sizes the colour TABLE, not the stride.
	if !isIndexed {
		if arr, family, colorants, ok := tintedColorSpace(ctx, doc, stream.Dict); ok {
			if paintsNothing(ctx, doc, arr, family) {
				return decodeNoneColorant(ctx, doc, stream.Dict, decodedData, width, height, bitsPerComp, colorants, filterName == "DCTDecode", native)
			}
			switch {
			case family == "Separation":
				return decodeSeparation(ctx, doc, stream.Dict, arr, decodedData, width, height, bitsPerComp, cc)
			case family == "DeviceN" && (filterName != "DCTDecode" || imageComponents(colorSpace) == colorants):
				return decodeDeviceN(ctx, doc, stream.Dict, arr, decodedData, width, height, bitsPerComp, colorants, cc)
			}
		}
	}

	// A Lab image holds L*, a* and b* per pixel (ISO 32000-1, 8.6.5.4), which
	// the palette path already converts to sRGB for an indexed image. Read as
	// RGB, the white of a direct Lab JPEG comes out pink.
	var lab labParams
	isLab := false
	if !isIndexed && colorSpace == "Lab" {
		if lab, isLab = resolveLabParams(ctx, doc, stream.Dict["ColorSpace"]); !isLab {
			return nil, fmt.Errorf("image %q is in a Lab colour space with no usable /WhitePoint", string(name))
		}
	}

	samplesPerPixel := 1
	switch {
	case isLab:
		samplesPerPixel = 3
	case !isIndexed:
		samplesPerPixel = imageComponents(colorSpace)
		if samplesPerPixel == 0 {
			return nil, fmt.Errorf("unsupported color space %q for image decode", colorSpace)
		}
	}

	// Data that stops short of the rows the dictionary states is not refused
	// while it holds a whole row: the rows it holds are painted and the rest
	// left black, which is what PDF.js paints. Nothing in ISO 32000-1, 8.9.5
	// says what a short image shows, and refusing it lost the whole picture
	// for a few missing bytes. With the filters keeping what a truncated
	// stream holds, this recovers nearly every image litho failed to decode
	// on a page PDF.js paints.
	validRows := height
	if filterName != "DCTDecode" {
		decodedData, validRows = padShortSamples(decodedData, width, height, samplesPerPixel, bitsPerComp)
		if validRows == 0 {
			return nil, fmt.Errorf("decoded data too short for one row of the %dx%d image: got %d bytes", width, height, len(decodedData))
		}
		if validRows < height {
			doc.Logger.Debug("painting the rows short image data lacks black",
				slog.String("image", string(name)), slog.Int("rows", validRows), slog.Int("height", height))
		}
	}

	decodedData, err = unpackSamples(decodedData, width, height, samplesPerPixel, bitsPerComp, !isIndexed)
	if err != nil {
		return nil, err
	}

	if isIndexed {
		indexDecode(ctx, doc, stream.Dict, decodedData, bitsPerComp)
		img, err := decodeIndexed(decodedData, width, height, pal)
		if err != nil {
			return nil, err
		}
		return finishRows(img, validRows), nil
	}
	if isLab {
		img := image.NewRGBA(image.Rect(0, 0, width, height))
		for i := range width * height {
			r, g, b := labEntryToRGB([4]byte{decodedData[3*i], decodedData[3*i+1], decodedData[3*i+2]}, lab)
			img.SetRGBA(i%width, i/width, color.RGBA{R: r, G: g, B: b, A: 255})
		}
		return finishRows(img, validRows), nil
	}

	// Apply the /Decode array (ISO 32000-1, 8.9.5.2) via per-component
	// lookup tables. Absent a /Decode array the tables are the identity,
	// so output is unchanged for images that do not declare one.
	components := imageComponents(colorSpace)
	dec := buildDecodeLUTs(resolveDecodeArray(ctx, doc, stream.Dict, components), components)

	// A CalRGB or CalGray image holds CIE-based samples, which resolveColorSpace
	// names DeviceRGB or DeviceGray for their component count. A JPEG whose
	// codestream disagrees with the space's count keeps the device reading.
	cal, isCal := resolveCalSpace(ctx, doc, stream.Dict["ColorSpace"])
	if isCal && calComponents(cal) != components {
		isCal = false
	}

	if native != nil {
		return nil, captureNative(native, decodedData, dec, components, width, height, validRows)
	}

	// An ICCBased image is read through its profile, a JPEG 2000 image with
	// no /ColorSpace through the profile its own data carries, and a
	// DeviceCMYK one through the document's CMYK profile (see
	// colorTransform). A stencil mask holds coverage, not colour, whatever
	// /ColorSpace it wrongly states.
	var conv *converter
	if !isMask {
		t := jpxCol.transform
		if t == nil {
			t = colorTransform(ctx, doc, stream.Dict["ColorSpace"], colorSpace, components, renderingIntent(ctx, doc, stream.Dict), cc)
		}
		conv = newConverter(t)
	}
	var px [4]byte

	switch colorSpace {
	case "DeviceRGB":
		img := image.NewRGBA(image.Rect(0, 0, width, height))
		expectedLen := width * height * 3
		if len(decodedData) < expectedLen {
			return nil, fmt.Errorf("decoded data too short for RGB image: got %d, want %d", len(decodedData), expectedLen)
		}
		idx := 0
		for y := range height {
			for x := range width {
				r := dec[0][decodedData[idx]]
				g := dec[1][decodedData[idx+1]]
				b := dec[2][decodedData[idx+2]]
				switch {
				case conv != nil:
					px[0], px[1], px[2] = r, g, b
					r, g, b = conv.convert(px[:3])
				case isCal:
					r, g, b = cal.convertRGB(r, g, b)
				}
				img.Set(x, y, color.RGBA{R: r, G: g, B: b, A: 255})
				idx += 3
			}
		}
		return finishRows(img, validRows), nil

	case "DeviceGray":
		img := image.NewGray(image.Rect(0, 0, width, height))
		expectedLen := width * height
		if len(decodedData) < expectedLen {
			return nil, fmt.Errorf("decoded data too short for grayscale image: got %d, want %d", len(decodedData), expectedLen)
		}
		grey := dec[0]
		switch {
		case conv != nil:
			// A grey profile maps onto the neutral axis, where the three sRGB
			// channels agree to within a level.
			for s, v := range grey {
				px[0] = v
				_, grey[s], _ = conv.convert(px[:1])
			}
		case isCal:
			for s, v := range grey {
				grey[s] = cal.convertGrey(v)
			}
		}
		idx := 0
		for y := range height {
			for x := range width {
				img.Set(x, y, color.Gray{Y: grey[decodedData[idx]]})
				idx++
			}
		}
		return finishRows(img, validRows), nil

	case "DeviceCMYK":
		img := image.NewRGBA(image.Rect(0, 0, width, height))
		expectedLen := width * height * 4
		if len(decodedData) < expectedLen {
			return nil, fmt.Errorf("decoded data too short for CMYK image: got %d, want %d", len(decodedData), expectedLen)
		}
		idx := 0
		for y := range height {
			for x := range width {
				for c := range 4 {
					px[c] = dec[c][decodedData[idx+c]]
				}
				idx += 4
				if conv != nil {
					r, g, b := conv.convert(px[:])
					img.Set(x, y, color.RGBA{R: r, G: g, B: b, A: 255})
					continue
				}
				c, m, yy, k := float64(px[0])/255, float64(px[1])/255, float64(px[2])/255, float64(px[3])/255
				r := uint8((1 - c) * (1 - k) * 255)
				g := uint8((1 - m) * (1 - k) * 255)
				b := uint8((1 - yy) * (1 - k) * 255)
				img.Set(x, y, color.RGBA{R: r, G: g, B: b, A: 255})
			}
		}
		return finishRows(img, validRows), nil

	default:
		return nil, fmt.Errorf("unsupported color space %q for image decode", colorSpace)
	}
}
