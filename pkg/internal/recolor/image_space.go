package recolor

import (
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics"
)

// The colour space families an image's /ColorSpace may name (ISO 32000-1,
// 8.6.3, Table 62). An image may be in any but Pattern (8.9.5, Table 89).
const (
	familyGray       ast.PdfName = "DeviceGray"
	familyRGB        ast.PdfName = "DeviceRGB"
	familyCMYK       ast.PdfName = "DeviceCMYK"
	familyCalGray    ast.PdfName = "CalGray"
	familyCalRGB     ast.PdfName = "CalRGB"
	familyLab        ast.PdfName = "Lab"
	familyICCBased   ast.PdfName = "ICCBased"
	familyIndexed    ast.PdfName = "Indexed"
	familySeparation ast.PdfName = "Separation"
	familyDeviceN    ast.PdfName = "DeviceN"
	familyPattern    ast.PdfName = "Pattern"
)

// maxImageComponents bounds the components of an image's colour space. ISO
// 32000-1, Annex C, Table C.1 limits a DeviceN space to 32 colorants, the
// most components a colour space has, so no conforming image exceeds it; the
// count is read from the file and sizes buffers.
const maxImageComponents = 32

// imageSpace is the colour space of an image's samples.
type imageSpace struct {
	// obj is the /ColorSpace value as the dictionary holds it, which a
	// [Converter] is asked for.
	obj    ast.PdfObject
	family ast.PdfName
	// arr is the resolved array of a space written as one, nil for a name.
	arr   ast.PdfArray
	comps int
	// ranges holds a [lo hi] pair per component, the default /Decode array
	// of the space (ISO 32000-1, 8.9.5.2, Table 90). An Indexed space's
	// depends on the image's depth, see [imageSpace.decodeDefault].
	ranges []float64
}

// describeImageSpace reads the /ColorSpace value obj of an image.
func describeImageSpace(ctx context.Context, doc *document.Document, obj ast.PdfObject) (imageSpace, error) {
	cs, err := graphics.DescribeColorSpace(ctx, doc, obj)
	if err != nil {
		return imageSpace{}, err
	}
	sp := imageSpace{obj: obj, family: ast.PdfName(cs.Family), comps: cs.Components}
	sp.arr, _ = cs.Object.(ast.PdfArray)
	if sp.arr == nil {
		// A family named directly leaves Object nil; an array resolves to it.
		if resolved, err := document.ResolveArray(ctx, doc, obj); err == nil {
			sp.arr = resolved
		}
	}
	if sp.family == familyPattern {
		// ISO 32000-1, 8.9.5, Table 89: an image's colour space may be any
		// kind but Pattern.
		return imageSpace{}, fmt.Errorf("an image cannot be in a Pattern colour space")
	}
	if sp.comps < 1 || sp.comps > maxImageComponents {
		return imageSpace{}, fmt.Errorf("colour space of %d components", sp.comps)
	}
	sp.ranges = unitRanges(sp.comps)
	switch sp.family {
	case familyLab:
		// L* over [0, 100], a* and b* over /Range, [-100 100 -100 100] by
		// default (ISO 32000-1, 8.6.5.4, Table 65).
		sp.ranges = []float64{0, 100, -100, 100, -100, 100}
		if len(sp.arr) > 1 {
			if params, err := document.ResolveDict(ctx, doc, sp.arr[1]); err == nil {
				if rng, ok := orderedRanges(ctx, doc, params["Range"], 2); ok {
					copy(sp.ranges[2:], rng)
				}
			}
		}
	case familyICCBased:
		// /Range, [0 1] per component by default (8.6.5.5, Table 66).
		if len(sp.arr) > 1 {
			if stream, err := document.ResolveStream(ctx, doc, sp.arr[1]); err == nil && stream.Dict != nil {
				if rng, ok := orderedRanges(ctx, doc, stream.Dict["Range"], sp.comps); ok {
					sp.ranges = rng
				}
			}
		}
	}
	return sp, nil
}

// decodeDefault returns the default /Decode array of an image of depth bpc
// in the space: an Indexed image's maps its samples onto [0, 2^bpc - 1], the
// indices themselves (ISO 32000-1, 8.9.5.2, Table 90).
func (sp imageSpace) decodeDefault(bpc int) []float64 {
	if sp.family == familyIndexed {
		return []float64{0, float64(int(1)<<bpc - 1)}
	}
	return sp.ranges
}

// unitRanges returns n [0 1] pairs.
func unitRanges(n int) []float64 {
	r := make([]float64, 2*n)
	for i := range n {
		r[2*i+1] = 1
	}
	return r
}

// orderedRanges reads n [lo hi] pairs with lo < hi from obj.
func orderedRanges(ctx context.Context, doc *document.Document, obj ast.PdfObject, n int) ([]float64, bool) {
	if obj == nil {
		return nil, false
	}
	arr, err := document.ResolveArray(ctx, doc, obj)
	if err != nil || len(arr) < 2*n {
		return nil, false
	}
	out := make([]float64, 2*n)
	for i := range out {
		v, err := document.ResolveObject(ctx, doc, arr[i])
		if err != nil {
			return nil, false
		}
		f, ok := document.Number(v)
		if !ok {
			return nil, false
		}
		out[i] = f
	}
	for i := range n {
		if !(out[2*i] < out[2*i+1]) {
			return nil, false
		}
	}
	return out, true
}
