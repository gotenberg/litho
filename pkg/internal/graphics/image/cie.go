package image

import (
	"context"
	"log/slog"
	"math"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// The D65 white of sRGB and the matrix that takes XYZ relative to it to linear
// sRGB, IEC 61966-2-1.
var (
	d65White     = [3]float64{0.95047, 1.0, 1.08883}
	xyzToLinearS = [9]float64{
		3.2404542, -1.5371385, -0.4985314,
		-0.9692660, 1.8760108, 0.0415560,
		0.0556434, -0.2040259, 1.0572252,
	}
)

// bradford and bradfordInverse take XYZ to the cone response domain of the
// Bradford chromatic adaptation transform and back.
var (
	bradford = [9]float64{
		0.8951, 0.2664, -0.1614,
		-0.7502, 1.7135, 0.0367,
		0.0389, -0.0685, 1.0296,
	}
	bradfordInverse = [9]float64{
		0.9869929, -0.1470543, 0.1599627,
		0.4323053, 0.5183603, 0.0492912,
		-0.0085287, 0.0400428, 0.9684867,
	}
)

// linearSteps is the resolution of the table that encodes a linear sRGB value
// with the sRGB transfer function. Its steepest step, near black, moves the
// encoded value by under one level.
const linearSteps = 4096

// calSpace converts samples of a CalGray or CalRGB colour space to sRGB.
//
// ISO 32000-1, 8.6.5.2 and 8.6.5.3 define what the samples mean: a component
// raised to its gamma, for CalRGB mixed by the matrix, is a colour in XYZ
// relative to the space's diffuse white. Rendering it as sRGB takes that white
// to D65 with the Bradford transform, the XYZ to linear sRGB with the matrix
// of IEC 61966-2-1, and the result through the sRGB transfer function. Reading
// the samples as DeviceRGB or DeviceGray instead drops the gamma, which
// differs from the sRGB curve near black: 13,780 images of the govdocs and
// pdfjs corpora came out up to 10 levels off PDF.js, which converts CalRGB
// the same way for a D65 white.
type calSpace struct {
	// gamma[c][s] is sample s of component c, scaled to [0, 1], raised to the
	// component's gamma.
	gamma [3][256]float64
	// rgb takes the gamma-applied components to linear sRGB. A CalGray space
	// leaves it unset.
	rgb [9]float64
	// grey reports a CalGray space, whose one component is the luminance.
	grey    bool
	encoded [linearSteps + 1]uint8
}

// resolveCalSpace reads a [/CalGray dict] or [/CalRGB dict] colour space, and
// reports false for any other value, or for a dictionary that fails the
// requirements of ISO 32000-1, Tables 63 and 64.
func resolveCalSpace(ctx context.Context, doc *document.Document, obj ast.PdfObject) (*calSpace, bool) {
	arr, err := document.ResolveArray(ctx, doc, obj)
	if err != nil || len(arr) < 2 {
		return nil, false
	}
	family, ok := mustResolve(ctx, doc, arr[0]).(ast.PdfName)
	if !ok || (family != "CalGray" && family != "CalRGB") {
		return nil, false
	}
	dict, err := document.ResolveDict(ctx, doc, arr[1])
	if err != nil || dict == nil {
		doc.Logger.Debug("resolving CIE-based colour space dictionary", slog.Any("err", err))
		return nil, false
	}
	// WhitePoint is required, and its Y shall be 1.0 with positive X and Z
	// (Tables 63 and 64). A white off those terms has no adaptation to D65.
	white, ok := numbers(ctx, doc, dict["WhitePoint"], 3)
	if !ok || white[1] != 1 || white[0] <= 0 || white[2] <= 0 {
		doc.Logger.Debug("ignoring CIE-based colour space with no usable /WhitePoint")
		return nil, false
	}
	// BlackPoint defaults to [0 0 0], which leaves the conversion as it is.
	// No file in the corpora states another.
	if black, ok := numbers(ctx, doc, dict["BlackPoint"], 3); ok && (black[0] != 0 || black[1] != 0 || black[2] != 0) {
		doc.Logger.Debug("ignoring the /BlackPoint of a CIE-based colour space")
	}
	cs := &calSpace{grey: family == "CalGray"}
	gammas := [3]float64{1, 1, 1}
	switch g := mustResolve(ctx, doc, dict["Gamma"]); {
	case cs.grey:
		if v, isNum := document.Number(g); isNum {
			gammas = [3]float64{v, v, v}
		}
	default:
		if v, ok := numbers(ctx, doc, g, 3); ok {
			copy(gammas[:], v)
		}
	}
	for c, gm := range gammas {
		// A gamma shall be positive (Tables 63 and 64).
		if gm <= 0 || math.IsNaN(gm) || math.IsInf(gm, 0) {
			return nil, false
		}
		for s := range 256 {
			cs.gamma[c][s] = math.Pow(float64(s)/255, gm)
		}
	}
	if !cs.grey {
		// Matrix defaults to the identity (Table 64). Its entries are
		// XA YA ZA XB YB ZB XC YC ZC: the columns of the map from the
		// gamma-applied components to XYZ.
		m := [9]float64{1, 0, 0, 0, 1, 0, 0, 0, 1}
		if v, ok := numbers(ctx, doc, dict["Matrix"], 9); ok {
			m = [9]float64{v[0], v[3], v[6], v[1], v[4], v[7], v[2], v[5], v[8]}
		}
		cs.rgb = mul3(xyzToLinearS, mul3(adaptToD65([3]float64(white)), m))
	}
	for i := range cs.encoded {
		cs.encoded[i] = encodeSRGB(float64(i) / linearSteps)
	}
	return cs, true
}

// adaptToD65 returns the Bradford transform that takes XYZ relative to white
// to XYZ relative to D65: it scales each cone response by the ratio of the two
// whites' cone responses, so the white maps to D65 and a grey stays grey.
// PDF.js scales by the ratio of the whites' tristimulus values instead, which
// tints the greys of a D50 space red; litho does not follow it there.
func adaptToD65(white [3]float64) [9]float64 {
	src := apply3(bradford, white)
	dst := apply3(bradford, d65White)
	scale := [9]float64{dst[0] / src[0], 0, 0, 0, dst[1] / src[1], 0, 0, 0, dst[2] / src[2]}
	return mul3(bradfordInverse, mul3(scale, bradford))
}

// encodeSRGB applies the sRGB transfer function of IEC 61966-2-1 to a linear
// value, clipped to [0, 1], and scales it to a byte.
func encodeSRGB(v float64) uint8 {
	v = min(max(v, 0), 1)
	if v <= 0.0031308 {
		v *= 12.92
	} else {
		v = 1.055*math.Pow(v, 1/2.4) - 0.055
	}
	return uint8(min(max(math.Round(v*255), 0), 255))
}

// encode returns the sRGB byte of a linear value.
func (cs *calSpace) encode(v float64) uint8 {
	return cs.encoded[int(min(max(v, 0), 1)*linearSteps+0.5)]
}

// convertRGB converts one CalRGB sample, three bytes, to sRGB.
func (cs *calSpace) convertRGB(a, b, c byte) (uint8, uint8, uint8) {
	x, y, z := cs.gamma[0][a], cs.gamma[1][b], cs.gamma[2][c]
	m := &cs.rgb
	return cs.encode(m[0]*x + m[1]*y + m[2]*z),
		cs.encode(m[3]*x + m[4]*y + m[5]*z),
		cs.encode(m[6]*x + m[7]*y + m[8]*z)
}

// convertGrey converts one CalGray sample to an sRGB grey level. A grey of
// luminance Y is Y times the white, which Bradford takes to Y times D65, which
// the sRGB matrix takes to Y on every channel. PDF.js approximates the level
// with L* instead, a few levels lighter in the mid-tones.
func (cs *calSpace) convertGrey(a byte) uint8 {
	return cs.encode(cs.gamma[0][a])
}

// calComponents returns the component count of a CalRGB or CalGray space.
func calComponents(cs *calSpace) int {
	if cs.grey {
		return 1
	}
	return 3
}

// convertTable returns a copy of a colour table whose entries, one or three
// bytes each, are samples of the space, converted to sRGB.
func (cs *calSpace) convertTable(table []byte) []byte {
	out := make([]byte, len(table))
	if cs.grey {
		for i, v := range table {
			out[i] = cs.convertGrey(v)
		}
		return out
	}
	for i := 0; i+2 < len(table); i += 3 {
		out[i], out[i+1], out[i+2] = cs.convertRGB(table[i], table[i+1], table[i+2])
	}
	return out
}

// numbers resolves obj as an array of exactly n numbers.
func numbers(ctx context.Context, doc *document.Document, obj ast.PdfObject, n int) ([]float64, bool) {
	if obj == nil {
		return nil, false
	}
	arr, err := document.ResolveArray(ctx, doc, obj)
	if err != nil || len(arr) != n {
		return nil, false
	}
	out := make([]float64, n)
	for i, v := range arr {
		f, isNum := document.Number(mustResolve(ctx, doc, v))
		if !isNum {
			return nil, false
		}
		out[i] = f
	}
	return out, true
}

// mul3 multiplies two row-major 3x3 matrices.
func mul3(a, b [9]float64) [9]float64 {
	var out [9]float64
	for r := range 3 {
		for c := range 3 {
			out[r*3+c] = a[r*3]*b[c] + a[r*3+1]*b[3+c] + a[r*3+2]*b[6+c]
		}
	}
	return out
}

// apply3 multiplies a row-major 3x3 matrix by a vector.
func apply3(m [9]float64, v [3]float64) [3]float64 {
	return [3]float64{
		m[0]*v[0] + m[1]*v[1] + m[2]*v[2],
		m[3]*v[0] + m[4]*v[1] + m[5]*v[2],
		m[6]*v[0] + m[7]*v[1] + m[8]*v[2],
	}
}
