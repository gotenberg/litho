package function

import (
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestSampledFunctionTwoInputs pins the multilinear path, which every other
// test leaves unexercised.
//
// One input and many inputs agree on everything except the SAMPLE ORDER, so a
// 1-D test cannot tell a correct index from a transposed one. Per ISO 32000-1,
// 7.10.2 the FIRST input varies fastest in the sample table: sample (i0, i1)
// lives at i0 + i1*Size[0]. The sizes here are deliberately asymmetric, [2 3],
// because a square table would give the same answer either way.
//
// The expectations come from evaluating the algorithm by hand, not from this
// implementation. A transposed index yields 0.705882 where this asserts
// 0.235294, so the test fails loudly rather than drifting.
func TestSampledFunctionTwoInputs(t *testing.T) {
	t.Parallel()

	// i0 varies fastest: (0,0)=0 (1,0)=60 (0,1)=120 (1,1)=180 (0,2)=240 (1,2)=255
	samples := []byte{0, 60, 120, 180, 240, 255}
	ctx, doc := funcPDF(t, []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [] /Count 0 >>",
		fmt.Sprintf("<< /FunctionType 0 /Domain [0 1 0 1] /Range [0 1]"+
			" /Size [2 3] /BitsPerSample 8 /Length %d >>\nstream\n%s\nendstream",
			len(samples), samples),
	})

	f, err := New(ctx, doc, ast.PdfIndirectRef{ObjectNum: 3})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	for _, tc := range []struct {
		x0, x1 float64
		want   float64
		note   string
	}{
		{0, 0, 0.000000, "corner (0,0)"},
		{1, 0, 0.235294, "corner (1,0): 60/255, the FIRST-fastest neighbour"},
		{0, 1, 0.941176, "corner (0,1): 240/255"},
		{1, 1, 1.000000, "corner (1,1): 255/255"},
		{0.5, 0.5, 0.588235, "centre, all four corners weighted"},
		{0, 0.5, 0.470588, "edge along input 1"},
		{1, 0.5, 0.705882, "the value a TRANSPOSED index would return for (1,0)"},
	} {
		out, err := f.Eval([]float64{tc.x0, tc.x1})
		if err != nil {
			t.Fatalf("Eval(%v,%v): %v", tc.x0, tc.x1, err)
		}
		near(t, out[0], tc.want, tc.note)
	}
}

// TestSampledFunctionThreeInputs pins that the stride keeps compounding beyond
// two dimensions, which is the shape a DeviceN tint transform uses: the corpus
// carries 7-input tables with /Size [3 3 3 3 3 3 3].
//
// Each input is given a distinct scale so a swapped or collapsed stride shows up
// as a wrong value rather than a coincidence.
func TestSampledFunctionThreeInputs(t *testing.T) {
	t.Parallel()

	// Size [2 2 2], one output. Index = i0 + 2*i1 + 4*i2, value = index*36 to
	// keep every cell distinct and inside a byte.
	samples := make([]byte, 8)
	for i := range samples {
		samples[i] = byte(i * 36)
	}
	ctx, doc := funcPDF(t, []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [] /Count 0 >>",
		fmt.Sprintf("<< /FunctionType 0 /Domain [0 1 0 1 0 1] /Range [0 1]"+
			" /Size [2 2 2] /BitsPerSample 8 /Length %d >>\nstream\n%s\nendstream",
			len(samples), samples),
	})

	f, err := New(ctx, doc, ast.PdfIndirectRef{ObjectNum: 3})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Each unit corner must read its own cell: index i0 + 2*i1 + 4*i2.
	for _, tc := range []struct {
		in   []float64
		cell int
	}{
		{[]float64{0, 0, 0}, 0},
		{[]float64{1, 0, 0}, 1},
		{[]float64{0, 1, 0}, 2},
		{[]float64{1, 1, 0}, 3},
		{[]float64{0, 0, 1}, 4},
		{[]float64{1, 0, 1}, 5},
		{[]float64{0, 1, 1}, 6},
		{[]float64{1, 1, 1}, 7},
	} {
		out, err := f.Eval(tc.in)
		if err != nil {
			t.Fatalf("Eval(%v): %v", tc.in, err)
		}
		want := float64(tc.cell*36) / 255
		near(t, out[0], want, fmt.Sprintf("corner %v should read cell %d", tc.in, tc.cell))
	}

	// The centre of the cube is the mean of all eight cells: (0+36+...+252)/8
	// = 126, over 255.
	out, err := f.Eval([]float64{0.5, 0.5, 0.5})
	if err != nil {
		t.Fatalf("Eval(centre): %v", err)
	}
	near(t, out[0], 126.0/255.0, "cube centre is the mean of all eight corners")
}

// TestSampledFunctionMultiInputRejectsWrongArity pins that a multi-input
// function checks its input count, since /Domain is what declares it.
func TestSampledFunctionMultiInputRejectsWrongArity(t *testing.T) {
	t.Parallel()

	samples := []byte{0, 60, 120, 180, 240, 255}
	ctx, doc := funcPDF(t, []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [] /Count 0 >>",
		fmt.Sprintf("<< /FunctionType 0 /Domain [0 1 0 1] /Range [0 1]"+
			" /Size [2 3] /BitsPerSample 8 /Length %d >>\nstream\n%s\nendstream",
			len(samples), samples),
	})
	f, err := New(ctx, doc, ast.PdfIndirectRef{ObjectNum: 3})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := f.Eval([]float64{0.5}); err == nil {
		t.Error("Eval accepted one input for a two-input function")
	}
}

// TestSampledFunctionSizeMustMatchDomain pins that /Size is checked against the
// input count rather than trusted, since both come from the file.
func TestSampledFunctionSizeMustMatchDomain(t *testing.T) {
	t.Parallel()

	samples := []byte{0, 255}
	ctx, doc := funcPDF(t, []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [] /Count 0 >>",
		fmt.Sprintf("<< /FunctionType 0 /Domain [0 1 0 1] /Range [0 1]"+
			" /Size [2] /BitsPerSample 8 /Length %d >>\nstream\n%s\nendstream",
			len(samples), samples),
	})
	if _, err := New(ctx, doc, ast.PdfIndirectRef{ObjectNum: 3}); err == nil {
		t.Error("New accepted /Size [2] for a two-input /Domain")
	}
}
