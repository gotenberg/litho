package function

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// stitchHead is the catalog and page tree every stitching fixture starts with,
// so the function under test is object 3 and its parts follow.
var stitchHead = []string{
	"<< /Type /Catalog /Pages 2 0 R >>",
	"<< /Type /Pages /Kids [] /Count 0 >>",
}

// redGreen and greenBlue are type 2 segments of a three-stop gradient.
const (
	redGreen  = "<< /FunctionType 2 /Domain [0 1] /C0 [1 0 0] /C1 [0 1 0] /N 1 >>"
	greenBlue = "<< /FunctionType 2 /Domain [0 1] /C0 [0 1 0] /C1 [0 0 1] /N 1 >>"
)

func evalAll(t *testing.T, f *Function, cases []struct {
	x    float64
	want []float64
},
) {
	t.Helper()
	for _, tc := range cases {
		out, err := f.Eval([]float64{tc.x})
		if err != nil {
			t.Fatalf("Eval(%v): %v", tc.x, err)
		}
		if len(out) != len(tc.want) {
			t.Fatalf("Eval(%v) returned %d values, want %d", tc.x, len(out), len(tc.want))
		}
		for j := range out {
			near(t, out[j], tc.want[j], fmt.Sprintf("f(%v)[%d]", tc.x, j))
		}
	}
}

// TestStitchingFunction pins type 3 evaluation per ISO 32000-1, 7.10.4.
//
// The second segment's /Encode is reversed, [1 0], so a value on the bound
// tells the two readings of the boundary apart: x = 0.5 opens the second
// subdomain and lands on that segment's far end, blue, where the first
// segment would give green. x = Domain1 belongs to the last, closed,
// subdomain, and a value outside /Domain is clamped first.
func TestStitchingFunction(t *testing.T) {
	t.Parallel()

	ctx, doc := funcPDF(t, append(append([]string(nil), stitchHead...),
		"<< /FunctionType 3 /Domain [0 1] /Functions [4 0 R 5 0 R] /Bounds [0.5] /Encode [0 1 1 0] >>",
		redGreen, greenBlue))
	f, err := New(ctx, doc, ast.PdfIndirectRef{ObjectNum: 3})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if f.Inputs() != 1 || f.Outputs() != 3 {
		t.Fatalf("Inputs, Outputs = %d, %d, want 1, 3", f.Inputs(), f.Outputs())
	}
	evalAll(t, f, []struct {
		x    float64
		want []float64
	}{
		{-1, []float64{1, 0, 0}},
		{0, []float64{1, 0, 0}},
		{0.25, []float64{0.5, 0.5, 0}},
		{0.4999, []float64{0.0002, 0.9998, 0}},
		{0.5, []float64{0, 0, 1}},
		{0.75, []float64{0, 0.5, 0.5}},
		{1, []float64{0, 1, 0}},
		{7, []float64{0, 1, 0}},
	})
}

// TestStitchingFunctionMapsItsOwnDomain pins that each subdomain is mapped
// from its bounds, not from [0 1]: over /Domain [10 20] the midpoint of the
// first subdomain is 12.5.
func TestStitchingFunctionMapsItsOwnDomain(t *testing.T) {
	t.Parallel()

	ctx, doc := funcPDF(t, append(append([]string(nil), stitchHead...),
		"<< /FunctionType 3 /Domain [10 20] /Functions [4 0 R 5 0 R] /Bounds [15] /Encode [0 1 0 1] >>",
		redGreen, greenBlue))
	f, err := New(ctx, doc, ast.PdfIndirectRef{ObjectNum: 3})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	evalAll(t, f, []struct {
		x    float64
		want []float64
	}{
		{10, []float64{1, 0, 0}},
		{12.5, []float64{0.5, 0.5, 0}},
		{15, []float64{0, 1, 0}},
		{17.5, []float64{0, 0.5, 0.5}},
		{20, []float64{0, 0, 1}},
	})
}

// TestStitchingFunctionRangeClips pins that an optional /Range clips the
// result, per ISO 32000-1, 7.10.1.
func TestStitchingFunctionRangeClips(t *testing.T) {
	t.Parallel()

	ctx, doc := funcPDF(t, append(append([]string(nil), stitchHead...),
		"<< /FunctionType 3 /Domain [0 1] /Range [0 0.5 0 1 0 1] /Functions [4 0 R] /Bounds [] /Encode [0 1] >>",
		redGreen))
	f, err := New(ctx, doc, ast.PdfIndirectRef{ObjectNum: 3})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	evalAll(t, f, []struct {
		x    float64
		want []float64
	}{
		{0, []float64{0.5, 0, 0}},
		{1, []float64{0, 1, 0}},
	})
}

// TestStitchingFunctionShapes pins the shapes a real producer writes that the
// strict reading of 7.10.4 would not all accept: a nested stitch, one
// subfunction shared by two slots, an empty subdomain from equal bounds, and
// Domain0 = Bounds0. Each stays a well-defined function.
func TestStitchingFunctionShapes(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		parts []string
		cases []struct {
			x    float64
			want []float64
		}
	}{
		{
			"a stitch nested in a stitch",
			[]string{
				"<< /FunctionType 3 /Domain [0 1] /Functions [4 0 R 5 0 R] /Bounds [0.5] /Encode [0 1 0 1] >>",
				"<< /FunctionType 3 /Domain [0 1] /Functions [5 0 R 6 0 R] /Bounds [0.5] /Encode [0 1 0 1] >>",
				redGreen, greenBlue,
			},
			[]struct {
				x    float64
				want []float64
			}{{0.125, []float64{0.5, 0.5, 0}}, {0.375, []float64{0, 0.5, 0.5}}, {0.75, []float64{0.5, 0.5, 0}}},
		},
		{
			"one subfunction in two slots",
			[]string{
				"<< /FunctionType 3 /Domain [0 1] /Functions [4 0 R 4 0 R] /Bounds [0.5] /Encode [0 1 0 1] >>",
				redGreen,
			},
			[]struct {
				x    float64
				want []float64
			}{{0.25, []float64{0.5, 0.5, 0}}, {0.75, []float64{0.5, 0.5, 0}}},
		},
		{
			"equal bounds leave an empty middle subdomain",
			[]string{
				"<< /FunctionType 3 /Domain [0 1] /Functions [4 0 R 4 0 R 5 0 R] /Bounds [0.5 0.5] /Encode [0 1 0 1 0 1] >>",
				redGreen, greenBlue,
			},
			[]struct {
				x    float64
				want []float64
			}{{0.25, []float64{0.5, 0.5, 0}}, {0.5, []float64{0, 1, 0}}, {0.75, []float64{0, 0.5, 0.5}}},
		},
		{
			"Domain0 = Bounds0",
			[]string{
				"<< /FunctionType 3 /Domain [0 1] /Functions [4 0 R 5 0 R] /Bounds [0] /Encode [0 1 0 1] >>",
				redGreen, greenBlue,
			},
			[]struct {
				x    float64
				want []float64
			}{{0, []float64{0, 1, 0}}, {0.5, []float64{0, 0.5, 0.5}}},
		},
	} {
		ctx, doc := funcPDF(t, append(append([]string(nil), stitchHead...), tc.parts...))
		f, err := New(ctx, doc, ast.PdfIndirectRef{ObjectNum: 3})
		if err != nil {
			t.Fatalf("%s: New: %v", tc.name, err)
		}
		evalAll(t, f, tc.cases)
	}
}

// TestMalformedStitchingFunctionsAreRefused pins every refusal of a type 3
// function. Each entry it reads comes from the file, so a missing, short or
// inconsistent one is reported rather than defaulted, and a self-reference or
// an exponential fan-out ends at a named bound rather than in a stack
// overflow or a blow-up.
func TestMalformedStitchingFunctionsAreRefused(t *testing.T) {
	t.Parallel()

	// fanOut builds a stitch of 40 slots naming object next, the shape that
	// grows as 40^depth while its bytes stay a few hundred.
	fanOut := func(next int) string {
		refs := strings.Repeat(fmt.Sprintf("%d 0 R ", next), 40)
		bounds := make([]string, 39)
		for i := range bounds {
			bounds[i] = fmt.Sprintf("%.3f", float64(i+1)/40)
		}
		return fmt.Sprintf("<< /FunctionType 3 /Domain [0 1] /Functions [%s] /Bounds [%s] /Encode [%s] >>",
			refs, strings.Join(bounds, " "), strings.Repeat("0 1 ", 40))
	}

	// chain builds n stitches, each naming the next, ending at a type 2.
	chain := func(n int) []string {
		var objs []string
		for i := range n {
			objs = append(objs, fmt.Sprintf(
				"<< /FunctionType 3 /Domain [0 1] /Functions [%d 0 R] /Bounds [] /Encode [0 1] >>", 4+i))
		}
		return append(objs, redGreen)
	}

	for _, tc := range []struct {
		name  string
		parts []string
		want  error
	}{
		{"two inputs", []string{"<< /FunctionType 3 /Domain [0 1 0 1] /Functions [4 0 R] /Bounds [] /Encode [0 1] >>", redGreen}, ErrMalformed},
		{"no /Functions", []string{"<< /FunctionType 3 /Domain [0 1] /Bounds [] /Encode [0 1] >>"}, ErrMalformed},
		{"empty /Functions", []string{"<< /FunctionType 3 /Domain [0 1] /Functions [] /Bounds [] /Encode [] >>"}, ErrMalformed},
		{"no /Bounds", []string{"<< /FunctionType 3 /Domain [0 1] /Functions [4 0 R] /Encode [0 1] >>", redGreen}, ErrMalformed},
		{"short /Bounds", []string{"<< /FunctionType 3 /Domain [0 1] /Functions [4 0 R 4 0 R] /Bounds [] /Encode [0 1 0 1] >>", redGreen}, ErrMalformed},
		{"decreasing /Bounds", []string{"<< /FunctionType 3 /Domain [0 1] /Functions [4 0 R 4 0 R 4 0 R] /Bounds [0.6 0.3] /Encode [0 1 0 1 0 1] >>", redGreen}, ErrMalformed},
		{"a bound past /Domain", []string{"<< /FunctionType 3 /Domain [0 1] /Functions [4 0 R 4 0 R] /Bounds [2] /Encode [0 1 0 1] >>", redGreen}, ErrMalformed},
		{"short /Encode", []string{"<< /FunctionType 3 /Domain [0 1] /Functions [4 0 R 4 0 R] /Bounds [0.5] /Encode [0 1] >>", redGreen}, ErrMalformed},
		{"a non-number in /Encode", []string{"<< /FunctionType 3 /Domain [0 1] /Functions [4 0 R] /Bounds [] /Encode [0 /One] >>", redGreen}, ErrMalformed},
		{"a two-input part", []string{
			"<< /FunctionType 3 /Domain [0 1] /Functions [4 0 R] /Bounds [] /Encode [0 1] >>",
			"<< /FunctionType 2 /Domain [0 1 0 1] /C0 [0] /C1 [1] /N 1 >>",
		}, ErrMalformed},
		{"parts of different widths", []string{
			"<< /FunctionType 3 /Domain [0 1] /Functions [4 0 R 5 0 R] /Bounds [0.5] /Encode [0 1 0 1] >>",
			redGreen, "<< /FunctionType 2 /Domain [0 1] /C0 [0] /C1 [1] /N 1 >>",
		}, ErrMalformed},
		{"/Range narrower than the parts", []string{"<< /FunctionType 3 /Domain [0 1] /Range [0 1] /Functions [4 0 R] /Bounds [] /Encode [0 1] >>", redGreen}, ErrMalformed},
		{"a part that does not resolve", []string{"<< /FunctionType 3 /Domain [0 1] /Functions [99 0 R] /Bounds [] /Encode [0 1] >>"}, document.ErrObjectNotFound},
		{"a part of an undefined type", []string{"<< /FunctionType 3 /Domain [0 1] /Functions [4 0 R] /Bounds [] /Encode [0 1] >>", "<< /FunctionType 7 /Domain [0 1] /Range [0 1] >>"}, ErrUnsupportedType},
		{"itself as a part", []string{"<< /FunctionType 3 /Domain [0 1] /Functions [3 0 R] /Bounds [] /Encode [0 1] >>"}, ErrStitchCycle},
		{"a two-object cycle", []string{
			"<< /FunctionType 3 /Domain [0 1] /Functions [4 0 R] /Bounds [] /Encode [0 1] >>",
			"<< /FunctionType 3 /Domain [0 1] /Functions [3 0 R] /Bounds [] /Encode [0 1] >>",
		}, ErrStitchCycle},
		{"nested past maxStitchDepth", chain(maxStitchDepth + 1), ErrStitchTooLarge},
		{"a fan-out past maxStitchFunctions", []string{fanOut(4), fanOut(5), redGreen}, ErrStitchTooLarge},
	} {
		ctx, doc := funcPDF(t, append(append([]string(nil), stitchHead...), tc.parts...))
		_, err := New(ctx, doc, ast.PdfIndirectRef{ObjectNum: 3})
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: New = %v, want %v", tc.name, err, tc.want)
		}
	}
}

// TestStitchingFunctionAtTheBounds pins that the bounds are inclusive: a
// chain exactly maxStitchDepth deep and a fan-out of exactly
// maxStitchFunctions compiled functions both compile.
func TestStitchingFunctionAtTheBounds(t *testing.T) {
	t.Parallel()

	var objs []string
	for i := range maxStitchDepth {
		objs = append(objs, fmt.Sprintf(
			"<< /FunctionType 3 /Domain [0 1] /Functions [%d 0 R] /Bounds [] /Encode [0 1] >>", 4+i))
	}
	objs = append(objs, redGreen)
	ctx, doc := funcPDF(t, append(append([]string(nil), stitchHead...), objs...))
	f, err := New(ctx, doc, ast.PdfIndirectRef{ObjectNum: 3})
	if err != nil {
		t.Fatalf("a %d-deep chain: New: %v", maxStitchDepth, err)
	}
	evalAll(t, f, []struct {
		x    float64
		want []float64
	}{{0.5, []float64{0.5, 0.5, 0}}})

	// One stitch of maxStitchFunctions-1 slots plus itself is exactly the
	// budget.
	k := maxStitchFunctions - 1
	bounds := make([]string, k-1)
	for i := range bounds {
		bounds[i] = fmt.Sprintf("%.6f", float64(i+1)/float64(k))
	}
	wide := fmt.Sprintf("<< /FunctionType 3 /Domain [0 1] /Functions [%s] /Bounds [%s] /Encode [%s] >>",
		strings.Repeat("4 0 R ", k), strings.Join(bounds, " "), strings.Repeat("0 1 ", k))
	ctx, doc = funcPDF(t, append(append([]string(nil), stitchHead...), wide, redGreen))
	if _, err := New(ctx, doc, ast.PdfIndirectRef{ObjectNum: 3}); err != nil {
		t.Fatalf("a %d-slot stitch: New: %v", k, err)
	}
}
