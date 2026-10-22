package page

import (
	"errors"
	"fmt"
	"image"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/function"
	"github.com/gotenberg/litho/pkg/internal/render/colorconv"
)

// transferLevels is the number of levels a transfer table samples: the 8-bit
// channel the output holds, as poppler's Splash samples its transfer tables.
const transferLevels = 256

// maxTransferCache bounds the transfer functions one render keeps compiled.
// Anti-DoS guard, not spec-mandated: a real page sets a handful of transfer
// functions, and past the bound a function is compiled again at each paint.
const maxTransferCache = 64

// errTransfer reports a transfer function that cannot be applied.
var errTransfer = errors.New("transfer function is not one function or an array of four")

// transfer holds the three transfer tables of the red, green and blue
// channels of the output, each the output of one function sampled at
// [transferLevels] levels.
type transfer struct {
	lut [3][transferLevels]float32
}

// apply maps one sRGB colour through the tables.
func (t *transfer) apply(r, g, b float32) (float32, float32, float32) {
	return t.lut[0][level(r)], t.lut[1][level(g)], t.lut[2][level(b)]
}

// level returns the table index of a channel value in [0, 1].
func level(v float32) int {
	switch {
	case !(v > 0):
		return 0
	case v >= 1:
		return transferLevels - 1
	}
	return int(v*(transferLevels-1) + 0.5)
}

// currentTransfer returns the transfer tables of the current graphics state,
// nil when the transfer is the identity (ISO 32000-1, 10.5): no /TR or /TR2
// set, /Identity, or /Default, which for litho's output, an sRGB raster with
// no transfer of its own, is the identity too. A function the file damages
// is skipped and applies the identity, as poppler applies it.
func (r *renderer) currentTransfer() *transfer {
	obj := r.m.Current().Transfer
	if obj == nil {
		return nil
	}
	key := fmt.Sprintf("%v", obj)
	if t, ok := r.transfers[key]; ok {
		return t
	}
	t, err := r.compileTransfer(obj)
	if err != nil {
		r.skip("transfer function", err)
		t = nil
	}
	if r.transfers == nil {
		r.transfers = make(map[string]*transfer)
	}
	if len(r.transfers) < maxTransferCache {
		r.transfers[key] = t
	}
	return t
}

// compileTransfer reads a /TR or /TR2 value (ISO 32000-1, 8.4.5, Table 58): a
// function of one input and one output applied to every component, or an
// array of four applied to the red, green, blue and grey components (10.5).
// The output is RGB, so the first three apply and the grey one does not.
func (r *renderer) compileTransfer(obj ast.PdfObject) (*transfer, error) {
	resolved, err := document.ResolveObject(r.ctx, r.doc, obj)
	if err != nil {
		return nil, fmt.Errorf("resolving transfer function: %w", err)
	}
	var fns [3]ast.PdfObject
	switch v := resolved.(type) {
	case ast.PdfName:
		if v == "Identity" || v == "Default" {
			return nil, nil //nolint:nilnil // nil is the identity transfer, which applies nothing
		}
		return nil, fmt.Errorf("%w: /%s", errTransfer, v)
	case ast.PdfArray:
		if len(v) != 4 {
			return nil, fmt.Errorf("%w: an array of %d", errTransfer, len(v))
		}
		fns = [3]ast.PdfObject{v[0], v[1], v[2]}
	default:
		fns = [3]ast.PdfObject{obj, obj, obj}
	}
	t := &transfer{}
	identity := true
	for c, fobj := range fns {
		if err := r.sampleTransfer(fobj, &t.lut[c]); err != nil {
			return nil, err
		}
		for i, v := range t.lut[c] {
			if v != float32(i)/(transferLevels-1) {
				identity = false
			}
		}
	}
	if identity {
		return nil, nil //nolint:nilnil // nil is the identity transfer, which applies nothing
	}
	return t, nil
}

// sampleTransfer fills lut with the function fobj sampled over [0, 1]. Each
// entry of a four-entry array may itself be /Identity.
func (r *renderer) sampleTransfer(fobj ast.PdfObject, lut *[transferLevels]float32) error {
	if name, err := document.ResolveName(r.ctx, r.doc, fobj); err == nil && name == "Identity" {
		for i := range lut {
			lut[i] = float32(i) / (transferLevels - 1)
		}
		return nil
	}
	fn, err := function.New(r.ctx, r.doc, fobj)
	if err != nil {
		return fmt.Errorf("compiling transfer function: %w", err)
	}
	if fn.Inputs() != 1 || fn.Outputs() < 1 {
		return fmt.Errorf("%w: a function of %d inputs and %d outputs", errTransfer, fn.Inputs(), fn.Outputs())
	}
	in := make([]float64, 1)
	for i := range lut {
		in[0] = float64(i) / (transferLevels - 1)
		out, err := fn.Eval(in)
		if err != nil {
			return fmt.Errorf("evaluating transfer function: %w", err)
		}
		lut[i] = float32(min(max(out[0], 0), 1))
	}
	return nil
}

// transferConverter applies the transfer tables after the colour conversion
// (ISO 32000-1, 10.5: the transfer adjusts the device colour a colour space
// produces).
type transferConverter struct {
	conv colorconv.Converter
	t    *transfer
}

func (c transferConverter) RGB(components []float64) (r, g, b float32) {
	return c.t.apply(c.conv.RGB(components))
}

// withTransfer wraps conv in the current transfer, conv itself when the
// transfer is the identity.
func (r *renderer) withTransfer(conv colorconv.Converter) colorconv.Converter {
	if t := r.currentTransfer(); t != nil {
		return transferConverter{conv: conv, t: t}
	}
	return conv
}

// transferRowsPerContextCheck is how many image rows the transfer maps
// between two checks of ctx.
const transferRowsPerContextCheck = 64

// transferImage returns img with the current transfer applied to its colour,
// img itself when the transfer is the identity. img is the decoder's colour,
// its transparency carried by its masks: an *image.NRGBA holds straight
// colour beside the opacity of its data, which the masks already took, so its
// colour is read without that alpha. The copy has img's size, which the image
// decoder already held to the image pixel budget.
func (r *renderer) transferImage(img image.Image) (image.Image, error) {
	t := r.currentTransfer()
	if t == nil {
		return img, nil
	}
	if own, ok := img.(*image.NRGBA); ok {
		opaque := *own
		opaque.Pix = slices.Clone(own.Pix)
		for i := 3; i < len(opaque.Pix); i += 4 {
			opaque.Pix[i] = 0xff
		}
		img = &opaque
	}
	b := img.Bounds()
	out := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := range b.Dy() {
		if y%transferRowsPerContextCheck == 0 {
			if err := r.ctx.Err(); err != nil {
				return nil, fmt.Errorf("applying a transfer function: %w", err)
			}
		}
		for x := range b.Dx() {
			cr, cg, cb, _ := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			tr, tg, tb := t.apply(float32(cr)/0xffff, float32(cg)/0xffff, float32(cb)/0xffff)
			i := y*out.Stride + 4*x
			out.Pix[i] = uint8(tr*255 + 0.5)   //nolint:gosec // tr is in [0, 1], so the product fits a byte
			out.Pix[i+1] = uint8(tg*255 + 0.5) //nolint:gosec // tg is in [0, 1], so the product fits a byte
			out.Pix[i+2] = uint8(tb*255 + 0.5) //nolint:gosec // tb is in [0, 1], so the product fits a byte
			out.Pix[i+3] = 0xff
		}
	}
	return out, nil
}
