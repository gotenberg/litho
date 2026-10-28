package writer

import (
	"bytes"
	"maps"
	"sort"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/intconv"
)

// maxIndexedColors is the most colours an Indexed colour space can hold: its
// hival is at most 255 (ISO 32000-1, 8.6.6.3).
const maxIndexedColors = 256

// rgbComponents is the number of components of the base colour spaces
// [indexedForm] converts from.
const rgbComponents = 3

// sampleForm is one way of writing an image's samples that
// [planRecompression] weighs: the samples, the dictionary that describes them
// (its filter keys replaced by the encoding chosen), the layout a PNG
// predictor applies to them under, or nil, and for an Indexed form the
// palette it maps its indices through.
type sampleForm struct {
	samples []byte
	dict    ast.PdfDict
	layout  *predictorLayout
	indexed *indexedPalette
}

// indexedPalette is the palette of an image [indexedForm] converted to an
// Indexed colour space: its lookup table, three bytes per entry, and the bits
// each index takes in a row of width indices.
type indexedPalette struct {
	lookup []byte
	bits   int
	width  int
}

// indexedForm returns the image's samples rewritten as indices into an
// Indexed colour space over its own base (ISO 32000-1, 8.6.6.3), or false
// when the image does not qualify. Every pixel keeps its colour: an entry of
// the lookup table holds the very bytes a pixel held, and an Indexed lookup
// byte maps onto its base component's range as an 8-bit sample does under
// the default /Decode (ISO 32000-1, 8.6.6.3 and Table 90).
//
// It qualifies when it is an 8-bit image over DeviceRGB or a three-component
// ICCBased space whose /Range is the default, its decoded data is exactly
// width x height pixels, and those pixels use at most 256 colours. The
// indices take 1, 2, 4 or 8 bits, the fewest that number the colours. It is
// left alone when:
//
//   - its /Decode is not the default, which an Indexed image would read as
//     index ranges instead (ISO 32000-1, Table 90);
//   - /Interpolate is true: interpolation is the viewer's to apply (ISO
//     32000-1, 8.9.5.3), and one that interpolates the samples before the
//     lookup would blend unrelated palette entries;
//   - its /SMask carries /Matte, whose components are the parent's colour
//     space's (ISO 32000-1, 11.6.5.3) and an Indexed parent has one, or the
//     /SMask is not a stream this rewrite holds;
//   - its /Mask is a colour-key array that is not six integers.
//
// A colour-key /Mask names ranges of base components (ISO 32000-1, 8.9.6.4),
// which an Indexed image reads as one range of indices. The colours it masks
// take the first indices, so [0 k-1] masks exactly them; when it masks no
// colour the entry goes, since it masked no pixel.
//
// DeviceGray is not converted: an 8-bit index is as wide as the sample it
// replaces, so only an image of at most 16 levels could gain, by narrower
// indices, and Flate already codes such an image's few distinct bytes
// compactly. Other spaces' samples do not map one to one onto lookup bytes:
// CalRGB and Lab images read through the default /Decode of their own family
// and are rare enough not to be worth a separate proof.
//
// The base colour space object is reused as it is, a reference included, so
// a space other images share is never edited: only this image's dictionary
// changes.
func indexedForm(pending map[int]ast.PdfObject, dict ast.PdfDict, decoded []byte, width, height int64) (sampleForm, bool) {
	if mask, _, _ := followPending(pending, dict["ImageMask"]); mask == ast.PdfBool(true) {
		return sampleForm{}, false
	}
	if bpc, ok := pendingInt(pending, dict["BitsPerComponent"]); !ok || bpc != 8 {
		return sampleForm{}, false
	}
	if interp, _, _ := followPending(pending, dict["Interpolate"]); interp == ast.PdfBool(true) {
		return sampleForm{}, false
	}
	base, ok := rgbBase(pending, dict["ColorSpace"])
	if !ok {
		return sampleForm{}, false
	}
	if decode, present := dict["Decode"]; present && !unitRanges(pending, decode, rgbComponents) {
		return sampleForm{}, false
	}
	if smask, present := dict["SMask"]; present {
		ref, isRef := smask.(ast.PdfIndirectRef)
		if !isRef {
			return sampleForm{}, false
		}
		s, isStream := pending[ref.ObjectNum].(ast.PdfStream)
		if !isStream {
			return sampleForm{}, false
		}
		if _, matte := s.Dict["Matte"]; matte {
			return sampleForm{}, false
		}
	}
	var keyRanges []int64
	if m, present := dict["Mask"]; present {
		resolved, _, ok := followPending(pending, m)
		if !ok {
			return sampleForm{}, false
		}
		if arr, isArr := resolved.(ast.PdfArray); isArr {
			if keyRanges, ok = colorKeyRanges(pending, arr); !ok {
				return sampleForm{}, false
			}
		}
	}
	if int64(len(decoded)) != width*height*rgbComponents {
		return sampleForm{}, false
	}

	colors := distinctColors(decoded)
	if colors == nil {
		return sampleForm{}, false
	}
	masked := func(c rgbColor) bool {
		if keyRanges == nil {
			return false
		}
		for i, v := range c {
			if int64(v) < keyRanges[2*i] || int64(v) > keyRanges[2*i+1] {
				return false
			}
		}
		return true
	}
	// The masked colours come first, so they take a contiguous range of
	// indices; within each group colours sort by value, which keeps the
	// palette, and so the output, a function of the samples alone.
	sort.Slice(colors, func(i, j int) bool {
		mi, mj := masked(colors[i]), masked(colors[j])
		if mi != mj {
			return mi
		}
		return bytes.Compare(colors[i][:], colors[j][:]) < 0
	})
	lookup := make([]byte, 0, len(colors)*rgbComponents)
	nMasked := 0
	for _, c := range colors {
		lookup = append(lookup, c[:]...)
		if masked(c) {
			nMasked++
		}
	}
	palette := &indexedPalette{lookup: lookup, bits: indexBits(len(colors)), width: intconv.Clamp(width)}
	samples, ok := palette.indices(decoded)
	if !ok || !bytes.Equal(palette.expand(samples, intconv.Clamp(height)), decoded) {
		return sampleForm{}, false
	}

	out := maps.Clone(dict)
	out["ColorSpace"] = ast.PdfArray{
		ast.PdfName("Indexed"), base, ast.PdfInt(len(colors) - 1), ast.PdfString(lookup),
	}
	out["BitsPerComponent"] = ast.PdfInt(palette.bits)
	delete(out, "Decode")
	switch {
	case keyRanges == nil:
	case nMasked == 0:
		delete(out, "Mask")
	default:
		out["Mask"] = ast.PdfArray{ast.PdfInt(0), ast.PdfInt(nMasked - 1)}
	}
	return sampleForm{
		samples: samples,
		dict:    out,
		layout:  &predictorLayout{colors: 1, bpc: palette.bits, columns: intconv.Clamp(width)},
		indexed: palette,
	}, true
}

// rgbBase returns the object an Indexed space names as its base for an image
// whose /ColorSpace is cs, or false when cs is not DeviceRGB or a
// three-component ICCBased space with the default /Range. A name is returned
// direct; an array is returned as the image named it, a reference included,
// so a space other images share is shared again rather than copied.
func rgbBase(pending map[int]ast.PdfObject, cs ast.PdfObject) (ast.PdfObject, bool) {
	resolved, _, ok := followPending(pending, cs)
	if !ok {
		return nil, false
	}
	switch v := resolved.(type) {
	case ast.PdfName:
		if v == "DeviceRGB" || v == "RGB" {
			return ast.PdfName("DeviceRGB"), true
		}
	case ast.PdfArray:
		if len(v) != 2 {
			return nil, false
		}
		if family, _ := v[0].(ast.PdfName); family != "ICCBased" {
			return nil, false
		}
		ref, isRef := v[1].(ast.PdfIndirectRef)
		if !isRef {
			return nil, false
		}
		profile, isStream := pending[ref.ObjectNum].(ast.PdfStream)
		if !isStream {
			return nil, false
		}
		if n, ok := pendingInt(pending, profile.Dict["N"]); !ok || n != rgbComponents {
			return nil, false
		}
		if r, present := profile.Dict["Range"]; present && !unitRanges(pending, r, rgbComponents) {
			return nil, false
		}
		return cs, true
	}
	return nil, false
}

// unitRanges reports whether obj is an array of n [0 1] ranges, the default
// /Decode of an RGB image and the default /Range of an ICCBased space (ISO
// 32000-1, Table 90 and Table 66).
func unitRanges(pending map[int]ast.PdfObject, obj ast.PdfObject, n int) bool {
	resolved, _, ok := followPending(pending, obj)
	arr, isArr := resolved.(ast.PdfArray)
	if !ok || !isArr || len(arr) != 2*n {
		return false
	}
	for i, elem := range arr {
		var v float64
		switch x := elem.(type) {
		case ast.PdfInt:
			v = float64(x)
		case ast.PdfReal:
			v = float64(x)
		default:
			return false
		}
		if v != float64(i%2) {
			return false
		}
	}
	return true
}

// colorKeyRanges returns the six bounds of an 8-bit RGB image's colour-key
// mask (ISO 32000-1, 8.9.6.4), or false when arr is not six integers.
func colorKeyRanges(pending map[int]ast.PdfObject, arr ast.PdfArray) ([]int64, bool) {
	if len(arr) != 2*rgbComponents {
		return nil, false
	}
	out := make([]int64, len(arr))
	for i, elem := range arr {
		v, ok := pendingInt(pending, elem)
		if !ok {
			return nil, false
		}
		out[i] = v
	}
	return out, true
}

// rgbColor is one 8-bit RGB sample.
type rgbColor [rgbComponents]byte

// distinctColors returns the distinct colours of 8-bit RGB samples, or nil
// when there are more than [maxIndexedColors]. It stops at the first colour
// past that, so an image of many colours costs a fraction of its pixels.
func distinctColors(decoded []byte) []rgbColor {
	seen := make(map[rgbColor]struct{}, maxIndexedColors)
	var (
		last     rgbColor
		haveLast bool
	)
	for i := 0; i+rgbComponents <= len(decoded); i += rgbComponents {
		c := rgbColor(decoded[i : i+rgbComponents])
		if haveLast && c == last {
			continue
		}
		last, haveLast = c, true
		if _, ok := seen[c]; ok {
			continue
		}
		if len(seen) == maxIndexedColors {
			return nil
		}
		seen[c] = struct{}{}
	}
	out := make([]rgbColor, 0, len(seen))
	for c := range seen {
		out = append(out, c)
	}
	return out
}

// indexBits returns the fewest bits per index, among those ISO 32000-1,
// 8.6.6.3 allows an Indexed image (1, 2, 4 or 8), that number n colours.
func indexBits(n int) int {
	switch {
	case n <= 2:
		return 1
	case n <= 4:
		return 2
	case n <= 16:
		return 4
	default:
		return 8
	}
}

// indices returns the RGB samples rewritten as rows of p.width indices into
// p's lookup table, each row padded to a whole byte (ISO 32000-1, 8.9.5), or
// false when a colour is missing from the table or the samples are not whole
// rows.
func (p *indexedPalette) indices(decoded []byte) ([]byte, bool) {
	rowSamples := p.width * rgbComponents
	if p.width < 1 || len(decoded)%rowSamples != 0 || len(p.lookup) > maxIndexedColors*rgbComponents {
		return nil, false
	}
	index := make(map[rgbColor]byte, len(p.lookup)/rgbComponents)
	var next byte
	for i := 0; i+rgbComponents <= len(p.lookup); i += rgbComponents {
		index[rgbColor(p.lookup[i:i+rgbComponents])] = next
		next++ // wraps only past the last of at most 256 entries
	}
	rowBytes := (p.width*p.bits + 7) / 8
	rows := len(decoded) / rowSamples
	out := make([]byte, rows*rowBytes)
	for y := range rows {
		row := out[y*rowBytes : (y+1)*rowBytes]
		for x := range p.width {
			at := y*rowSamples + x*rgbComponents
			idx, ok := index[rgbColor(decoded[at:at+rgbComponents])]
			if !ok {
				return nil, false
			}
			bit := x * p.bits
			row[bit/8] |= idx << (8 - p.bits - bit%8)
		}
	}
	return out, true
}

// expand returns rows of indices mapped back through p's lookup table to RGB
// samples, as a viewer reads an Indexed image (ISO 32000-1, 8.6.6.3). An
// index past the table maps to nothing, which makes the result differ from
// any samples it is compared with.
func (p *indexedPalette) expand(samples []byte, rows int) []byte {
	rowBytes := (p.width*p.bits + 7) / 8
	if len(samples) != rows*rowBytes {
		return nil
	}
	mask := byte(1<<p.bits - 1)
	out := make([]byte, 0, rows*p.width*rgbComponents)
	for y := range rows {
		row := samples[y*rowBytes : (y+1)*rowBytes]
		for x := range p.width {
			bit := x * p.bits
			at := int(row[bit/8]>>(8-p.bits-bit%8)&mask) * rgbComponents
			if at+rgbComponents > len(p.lookup) {
				return nil
			}
			out = append(out, p.lookup[at:at+rgbComponents]...)
		}
	}
	return out
}
