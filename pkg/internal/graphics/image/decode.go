package image

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/filter"
)

const (
	// maxImagePixels caps how many pixels a single Image XObject may
	// declare before [DecodeImage] refuses to allocate. 100 megapixels
	// covers print-resolution photographs (8K display tops out at
	// ~33 MP) yet rejects adversarial PDFs that declare 100k×100k
	// dimensions to force ~40 GB allocations.
	maxImagePixels = 100_000_000

	// maxImageBytes caps the decoded byte stream. Four bytes per pixel
	// is the worst case (RGBA), so this matches the pixel cap and
	// bounds [io.ReadAll] on the streaming decoder output.
	maxImageBytes = 4 * maxImagePixels
)

// validateImageDimensions rejects degenerate or out-of-policy image
// sizes. Width and height must both be positive and their product must
// fit under [maxImagePixels].
func validateImageDimensions(width, height int) error {
	if width <= 0 || height <= 0 {
		return fmt.Errorf("invalid image dimensions: %dx%d", width, height)
	}
	if int64(width)*int64(height) > maxImagePixels {
		return fmt.Errorf("image %dx%d exceeds %d-pixel safety cap", width, height, maxImagePixels)
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
// The result needs no cap of its own: [validateImageDimensions] holds
// width*height to maxImagePixels and samplesPerPixel is at most 4, so the
// expansion cannot exceed maxImageBytes however small the packed input was.
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
		// corpus image declares 3, which has no defined packing at all, so it
		// is refused rather than strided as though it had one.
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

// AppliesDecodeArray reports whether [DecodeImage] applies the image's /Decode
// array to the pixels it returns.
//
// A caller that re-encodes decoded pixels needs this to know whether /Decode
// has already been folded into them. Where it has, the entry may be dropped
// from the new image and the result renders the same; where it has not, the
// pixels still need it and dropping it changes the picture.
//
// It is false in two cases. An indexed image's /Decode remaps palette INDICES
// rather than colour components (ISO 32000-1, 8.9.5.2), and [decodeIndexed]
// looks each index up directly without consulting it. And a JPEG takes the
// DCTDecode branch of [DecodeImage], which hands the stream to image/jpeg
// instead of to the sample path that builds the /Decode tables: an
// Adobe-inverted CMYK JPEG carrying /Decode [1 0 1 0 1 0 1 0] is the ordinary
// case there, and treating its pixels as already mapped would invert it.
//
// That branch is in fact broken today, which does not change the answer but
// does change what to watch: the filter pipeline already runs decodeDCT, so
// image/jpeg is handed decoded samples and reports a missing SOI marker, and no
// JPEG decodes at all. If that is fixed by routing DCTDecode through the
// ordinary sample path, the /Decode tables WOULD then be applied and this
// function must stop excluding it.
func AppliesDecodeArray(ctx context.Context, doc *document.Document, dict ast.PdfDict) bool {
	if _, indexed := resolveIndexedPalette(ctx, doc, dict); indexed {
		return false
	}
	return imageFilterName(ctx, doc, dict) != "DCTDecode"
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
// numbers, or returns nil when it is absent or malformed. Per ISO
// 32000-1 §8.9.5.2 the /Decode array remaps each sample from the raw
// [0, 2^bpc-1] range onto [Dmin, Dmax]; honoring it renders inverted or
// remapped images (e.g. Adobe-inverted CMYK, /Decode [1 0 ...]) with
// correct color instead of the raw samples.
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
	if len(arr) != 2*components {
		doc.Logger.Debug("ignoring image /Decode array with unexpected length",
			slog.Int("len", len(arr)), slog.Int("want", 2*components))
		return nil
	}
	dec := make([]float64, len(arr))
	for i, v := range arr {
		dec[i] = document.Float(v)
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

// palette holds a resolved indexed colour space: the device space its entries
// are written in, and the lookup table itself.
type palette struct {
	base   string
	comps  int
	lookup []byte
}

// resolveIndexedPalette reports the palette of an [/Indexed base hival lookup]
// colour space, and whether the image has one at all.
//
// Per ISO 32000-1, 8.6.6.3 an indexed image stores one sample per pixel, and
// that sample is an index into the table rather than a colour. Reading it as
// though it were a colour in the base space is what [resolveColorSpace] implies
// on its own, and the decoder then wanted three or four bytes per pixel for
// data that holds one: every indexed image failed with "decoded data too short"
// (pdfjs 160F-2019.pdf and S2.pdf among them), so no palette image could be
// decoded at all.
func resolveIndexedPalette(ctx context.Context, doc *document.Document, dict ast.PdfDict) (palette, bool) {
	csObj, ok := dict["ColorSpace"]
	if !ok {
		return palette{}, false
	}
	resolved, err := document.ResolveObject(ctx, doc, csObj)
	if err != nil {
		doc.Logger.Debug("resolving indexed color space", slog.Any("err", err))
		return palette{}, false
	}
	arr, ok := resolved.(ast.PdfArray)
	if !ok || len(arr) < 4 {
		return palette{}, false
	}
	family, err := document.ResolveObject(ctx, doc, arr[0])
	if err != nil {
		return palette{}, false
	}
	if name, ok := family.(ast.PdfName); !ok || name != "Indexed" {
		return palette{}, false
	}

	base := resolveColorSpace(ctx, doc, ast.PdfDict{"ColorSpace": arr[1]})
	comps := imageComponents(base)
	if comps == 0 {
		return palette{}, false
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
	return palette{base: base, comps: comps, lookup: lookup}, true
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
			default:
				img.Set(x, y, color.RGBA{R: e[0], G: e[1], B: e[2], A: 255})
			}
		}
	}
	return img, nil
}

// DecodeImage decodes a named Image XObject from a page into a Go image.Image.
// The name parameter is the resource key (e.g., "Im0") under the page's
// /Resources /XObject dictionary.
func DecodeImage(ctx context.Context, doc *document.Document, page *document.Page, name ast.PdfName) (image.Image, error) {
	xobjObj, ok := page.Resources["XObject"]
	if !ok {
		return nil, fmt.Errorf("page has no /XObject resources")
	}

	xobjDict, err := document.ResolveDict(ctx, doc, xobjObj)
	if err != nil {
		return nil, err
	}
	if xobjDict == nil {
		return nil, fmt.Errorf("/XObject is not a dictionary")
	}

	obj, ok := xobjDict[name]
	if !ok {
		return nil, fmt.Errorf("image %q not found in /XObject", name)
	}

	stream, err := document.ResolveStream(ctx, doc, obj)
	if err != nil {
		return nil, err
	}
	if stream.Dict == nil {
		return nil, fmt.Errorf("image %q is not a stream", name)
	}

	width, err := resolveInt(ctx, doc, stream.Dict, "Width")
	if err != nil {
		return nil, err
	}

	height, err := resolveInt(ctx, doc, stream.Dict, "Height")
	if err != nil {
		return nil, err
	}

	if err := validateImageDimensions(width, height); err != nil {
		return nil, err
	}

	colorSpace := resolveColorSpace(ctx, doc, stream.Dict)
	bitsPerComp := statedBitsPerComponent(ctx, doc, stream.Dict)

	filterName := imageFilterName(ctx, doc, stream.Dict)

	if filterName == "DCTDecode" {
		// Raw JPEG: stream through the filter pipeline without buffering.
		rc, err := doc.NewStreamReader(ctx, stream)
		if err != nil {
			return nil, fmt.Errorf("opening JPEG stream: %w", err)
		}
		defer func() {
			if closeErr := rc.Close(); closeErr != nil {
				doc.Logger.Debug("closing JPEG stream reader", slog.Any("err", closeErr))
			}
		}()
		img, err := jpeg.Decode(rc)
		if err != nil {
			return nil, fmt.Errorf("decoding JPEG: %w", err)
		}
		return img, nil
	}

	// Non-JPEG: stream through filters and read all decoded bytes for pixel
	// construction below. Streaming decompression avoids holding both
	// compressed and decompressed copies in memory simultaneously.
	rc, err := doc.NewStreamReader(ctx, stream)
	if err != nil {
		return nil, fmt.Errorf("opening image stream: %w", err)
	}
	// Cap the decoded read so a flate/lzw bomb cannot blow memory
	// before we discover the dimension mismatch below. The +1 makes
	// "exactly maxImageBytes" valid while detecting overflow.
	decodedData, err := io.ReadAll(io.LimitReader(rc, int64(maxImageBytes)+1))
	if closeErr := rc.Close(); closeErr != nil {
		doc.Logger.Debug("closing image stream reader", slog.Any("err", closeErr))
	}
	if err != nil {
		return nil, fmt.Errorf("reading decoded image stream: %w", err)
	}
	if len(decodedData) > maxImageBytes {
		return nil, fmt.Errorf("decoded image stream exceeds %d-byte safety cap", maxImageBytes)
	}

	// Resolved before the samples are unpacked, because an indexed image holds
	// ONE sample per pixel whatever its base space is, while resolveColorSpace
	// reports that base: [/Indexed /DeviceRGB ...] reads back as "DeviceRGB".
	// Taking the sample count from the base would stride packed samples three
	// or four times too far and mis-read every sub-byte indexed image.
	//
	// An indexed image's /Decode array remaps the index range, not colour
	// components (ISO 32000-1, 8.9.5.2), and the default [0 2^n-1] is the
	// identity, which is what every indexed image in the corpora states.
	pal, isIndexed := resolveIndexedPalette(ctx, doc, stream.Dict)

	samplesPerPixel := 1
	if !isIndexed {
		samplesPerPixel = imageComponents(colorSpace)
		if samplesPerPixel == 0 {
			return nil, fmt.Errorf("unsupported color space %q for image decode", colorSpace)
		}
	}

	decodedData, err = unpackSamples(decodedData, width, height, samplesPerPixel, bitsPerComp, !isIndexed)
	if err != nil {
		return nil, err
	}

	if isIndexed {
		return decodeIndexed(decodedData, width, height, pal)
	}

	// Apply the /Decode array (ISO 32000-1 §8.9.5.2) via per-component
	// lookup tables. Absent a /Decode array the tables are the identity,
	// so output is unchanged for images that do not declare one.
	components := imageComponents(colorSpace)
	dec := buildDecodeLUTs(resolveDecodeArray(ctx, doc, stream.Dict, components), components)

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
				img.Set(x, y, color.RGBA{R: r, G: g, B: b, A: 255})
				idx += 3
			}
		}
		return img, nil

	case "DeviceGray":
		img := image.NewGray(image.Rect(0, 0, width, height))
		expectedLen := width * height
		if len(decodedData) < expectedLen {
			return nil, fmt.Errorf("decoded data too short for grayscale image: got %d, want %d", len(decodedData), expectedLen)
		}
		idx := 0
		for y := range height {
			for x := range width {
				img.Set(x, y, color.Gray{Y: dec[0][decodedData[idx]]})
				idx++
			}
		}
		return img, nil

	case "DeviceCMYK":
		img := image.NewRGBA(image.Rect(0, 0, width, height))
		expectedLen := width * height * 4
		if len(decodedData) < expectedLen {
			return nil, fmt.Errorf("decoded data too short for CMYK image: got %d, want %d", len(decodedData), expectedLen)
		}
		idx := 0
		for y := range height {
			for x := range width {
				c := float64(dec[0][decodedData[idx]]) / 255.0
				m := float64(dec[1][decodedData[idx+1]]) / 255.0
				yy := float64(dec[2][decodedData[idx+2]]) / 255.0
				k := float64(dec[3][decodedData[idx+3]]) / 255.0
				r := uint8((1 - c) * (1 - k) * 255)
				g := uint8((1 - m) * (1 - k) * 255)
				b := uint8((1 - yy) * (1 - k) * 255)
				img.Set(x, y, color.RGBA{R: r, G: g, B: b, A: 255})
				idx += 4
			}
		}
		return img, nil

	default:
		return nil, fmt.Errorf("unsupported color space %q for image decode", colorSpace)
	}
}
