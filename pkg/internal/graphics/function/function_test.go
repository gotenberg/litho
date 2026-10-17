package function

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/postscript"
)

// funcPDF builds a one-page document whose objects are objs, and returns a
// document the function package can resolve references through.
//
// A function reaches its sample stream by indirect reference in every real
// file, so the tests exercise resolution rather than hand-built objects.
func funcPDF(t *testing.T, objs []string) (context.Context, *document.Document) {
	t.Helper()

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)

	data := buf.Bytes()
	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	return ctx, doc
}

func near(t *testing.T, got, want float64, what string) {
	t.Helper()
	if math.Abs(got-want) > 1e-6 {
		t.Errorf("%s = %.6f, want %.6f", what, got, want)
	}
}

// TestSampledFunction pins type 0 evaluation against values computed from the
// algorithm of ISO 32000-1, 7.10.2 independently of this implementation.
//
// The table is a three-entry white-to-black ramp, the shape of a real /Black
// tint transform. x=0.5 lands on 0.501961 rather than 0.5 because the middle
// sample is 128/255: asserting a round 0.5 would be asserting an intuition
// over the arithmetic.
func TestSampledFunction(t *testing.T) {
	t.Parallel()

	samples := []byte{255, 255, 255, 128, 128, 128, 0, 0, 0}
	ctx, doc := funcPDF(t, []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [] /Count 0 >>",
		fmt.Sprintf("<< /FunctionType 0 /Domain [0 1] /Range [0 1 0 1 0 1]"+
			" /Size [3] /BitsPerSample 8 /Encode [0 2] /Decode [0 1 0 1 0 1]"+
			" /Length %d >>\nstream\n%s\nendstream", len(samples), samples),
	})

	f, err := New(ctx, doc, ast.PdfIndirectRef{ObjectNum: 3})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if f.Outputs() != 3 {
		t.Fatalf("Outputs() = %d, want 3", f.Outputs())
	}

	for _, tc := range []struct {
		x    float64
		want float64
	}{
		{0.00, 1.000000},
		{0.25, 0.750980},
		{0.50, 0.501961},
		{0.75, 0.250980},
		{1.00, 0.000000},
	} {
		out, err := f.Eval([]float64{tc.x})
		if err != nil {
			t.Fatalf("Eval(%v): %v", tc.x, err)
		}
		if len(out) != 3 {
			t.Fatalf("Eval(%v) returned %d values, want 3", tc.x, len(out))
		}
		for j, got := range out {
			near(t, got, tc.want, fmt.Sprintf("sampled(%.2f)[%d]", tc.x, j))
		}
	}
}

// TestSampledFunctionClampsOutsideDomain pins that an input beyond /Domain is
// clamped rather than extrapolated, per ISO 32000-1, 7.10.
func TestSampledFunctionClampsOutsideDomain(t *testing.T) {
	t.Parallel()

	samples := []byte{255, 0}
	ctx, doc := funcPDF(t, []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [] /Count 0 >>",
		fmt.Sprintf("<< /FunctionType 0 /Domain [0 1] /Range [0 1]"+
			" /Size [2] /BitsPerSample 8 /Length %d >>\nstream\n%s\nendstream",
			len(samples), samples),
	})
	f, err := New(ctx, doc, ast.PdfIndirectRef{ObjectNum: 3})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	below, err := f.Eval([]float64{-5})
	if err != nil {
		t.Fatalf("Eval(-5): %v", err)
	}
	above, err := f.Eval([]float64{5})
	if err != nil {
		t.Fatalf("Eval(5): %v", err)
	}
	near(t, below[0], 1.0, "clamped below domain")
	near(t, above[0], 0.0, "clamped above domain")
}

// TestExponentialFunction pins type 2 against the coefficients of a real
// tint transform, computed independently.
//
// # Corpus evidence
//
//   - govdocs thread_200_200192.pdf: carries the tint transform whose
//     coefficients this test uses.
func TestExponentialFunction(t *testing.T) {
	t.Parallel()

	ctx, doc := funcPDF(t, []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [] /Count 0 >>",
		"<< /FunctionType 2 /Domain [0 1] /Range [0 1 0 1 0 1 0 1]" +
			" /C0 [0 0 0 0] /C1 [0.559998 0.109985 0.0 0.429993] /N 1 >>",
	})
	f, err := New(ctx, doc, ast.PdfIndirectRef{ObjectNum: 3})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	for _, tc := range []struct {
		x    float64
		want []float64
	}{
		{0.0, []float64{0, 0, 0, 0}},
		{0.5, []float64{0.279999, 0.054992, 0.0, 0.214997}},
		{1.0, []float64{0.559998, 0.109985, 0.0, 0.429993}},
	} {
		out, err := f.Eval([]float64{tc.x})
		if err != nil {
			t.Fatalf("Eval(%v): %v", tc.x, err)
		}
		for j := range tc.want {
			near(t, out[j], tc.want[j], fmt.Sprintf("exponential(%.1f)[%d]", tc.x, j))
		}
	}
}

// TestExponentialDefaults pins that /C0 and /C1 default to [0] and [1] per
// ISO 32000-1, 7.10.3, so a function omitting them is the identity ramp.
func TestExponentialDefaults(t *testing.T) {
	t.Parallel()

	ctx, doc := funcPDF(t, []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [] /Count 0 >>",
		"<< /FunctionType 2 /Domain [0 1] /N 1 >>",
	})
	f, err := New(ctx, doc, ast.PdfIndirectRef{ObjectNum: 3})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if f.Outputs() != 1 {
		t.Fatalf("Outputs() = %d, want 1 from the /C0 default", f.Outputs())
	}
	out, err := f.Eval([]float64{0.25})
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	near(t, out[0], 0.25, "identity ramp")
}

// TestPostScriptFunction pins that a type 4 program runs through the existing
// evaluator and that /Range selects the trailing outputs.
func TestPostScriptFunction(t *testing.T) {
	t.Parallel()

	// One input, three outputs: a grey ramp inverted on the red channel.
	program := "{ dup 1 exch sub exch dup }"
	ctx, doc := funcPDF(t, []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [] /Count 0 >>",
		fmt.Sprintf("<< /FunctionType 4 /Domain [0 1] /Range [0 1 0 1 0 1]"+
			" /Length %d >>\nstream\n%s\nendstream", len(program), program),
	})
	f, err := New(ctx, doc, ast.PdfIndirectRef{ObjectNum: 3})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	out, err := f.Eval([]float64{0.25})
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if len(out) != 3 {
		t.Fatalf("Eval returned %d values, want 3", len(out))
	}
	near(t, out[0], 0.75, "postscript[0]")
	near(t, out[1], 0.25, "postscript[1]")
	near(t, out[2], 0.25, "postscript[2]")
}

// TestPostScriptFunction_CompilesOnce pins that New compiles the program, so
// a program the parser refuses fails at New and never reaches Eval.
//
// Eval used to re-tokenize the raw program on every call, and the tokenizer
// recursed once per '{' with no bound. A Separation image evaluates its tint
// transform for every sample, so one crafted program either overflowed the
// goroutine stack (a fatal error) or cost a full parse per sample.
func TestPostScriptFunction_CompilesOnce(t *testing.T) {
	t.Parallel()

	program := "{ " + strings.Repeat("{", 1000) + strings.Repeat("}", 1000) + " }"
	ctx, doc := funcPDF(t, []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [] /Count 0 >>",
		fmt.Sprintf("<< /FunctionType 4 /Domain [0 1] /Range [0 1]"+
			" /Length %d >>\nstream\n%s\nendstream", len(program), program),
	})
	_, err := New(ctx, doc, ast.PdfIndirectRef{ObjectNum: 3})
	if !errors.Is(err, postscript.ErrBlockTooDeep) {
		t.Fatalf("New = %v, want postscript.ErrBlockTooDeep", err)
	}
}

// TestStitchingFunctionIsRefused pins that type 3 is refused rather than
// silently mis-evaluated.
//
// Nothing real exercises type 3, so shipping an untested implementation would
// be speculation. The refusal names the type.
//
// # Corpus evidence
//
//   - All corpora: no fixture uses a type 3 function. Across the 20 files
//     holding Separation or DeviceN images, the functions measured are type 0
//     x47, type 2 x6 and type 4 x3.
func TestStitchingFunctionIsRefused(t *testing.T) {
	t.Parallel()

	ctx, doc := funcPDF(t, []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [] /Count 0 >>",
		"<< /FunctionType 3 /Domain [0 1] /Range [0 1] /Bounds [] /Encode [0 1] /Functions [] >>",
	})
	_, err := New(ctx, doc, ast.PdfIndirectRef{ObjectNum: 3})
	if !errors.Is(err, ErrUnsupportedType) {
		t.Fatalf("New(type 3) = %v, want ErrUnsupportedType", err)
	}
}

// TestMalformedFunctionsAreRefused pins the required entries. Every value here
// comes from the file, so a missing or inconsistent one must be reported rather
// than defaulted into a plausible-looking answer.
func TestMalformedFunctionsAreRefused(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		obj  string
	}{
		{"no /FunctionType", "<< /Domain [0 1] /Range [0 1] >>"},
		{"no /Domain", "<< /FunctionType 2 /N 1 >>"},
		{"odd /Domain", "<< /FunctionType 2 /Domain [0 1 0] /N 1 >>"},
		{"type 0 as a dict", "<< /FunctionType 0 /Domain [0 1] /Range [0 1] /Size [2] /BitsPerSample 8 >>"},
		{"type 2 without /N", "<< /FunctionType 2 /Domain [0 1] /C0 [0] /C1 [1] >>"},
		{"type 2 mismatched /C0 and /C1", "<< /FunctionType 2 /Domain [0 1] /C0 [0 0] /C1 [1] /N 1 >>"},
		{"/Range holding a name", "<< /FunctionType 2 /Domain [0 1] /Range [0 /One] /N 1 >>"},
		{"/Range not an array", "<< /FunctionType 2 /Domain [0 1] /Range 1 /N 1 >>"},
	} {
		ctx, doc := funcPDF(t, []string{
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [] /Count 0 >>",
			tc.obj,
		})
		if _, err := New(ctx, doc, ast.PdfIndirectRef{ObjectNum: 3}); err == nil {
			t.Errorf("%s: New accepted a malformed function", tc.name)
		}
	}
}

// TestEvalRejectsWrongInputCount pins that the input count is checked against
// /Domain rather than assumed.
func TestEvalRejectsWrongInputCount(t *testing.T) {
	t.Parallel()

	ctx, doc := funcPDF(t, []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [] /Count 0 >>",
		"<< /FunctionType 2 /Domain [0 1] /C0 [0] /C1 [1] /N 1 >>",
	})
	f, err := New(ctx, doc, ast.PdfIndirectRef{ObjectNum: 3})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := f.Eval([]float64{0.1, 0.2}); !errors.Is(err, ErrInputCount) {
		t.Errorf("Eval with two inputs = %v, want ErrInputCount", err)
	}
}

// TestBitsAt pins the sample reader against hand-computed packings. Samples are
// packed most significant bit first with no padding (ISO 32000-1, 7.10.2), so a
// sub-byte depth crosses byte boundaries.
func TestBitsAt(t *testing.T) {
	t.Parallel()

	// 0b1011_0010 0b0100_1111
	data := []byte{0xB2, 0x4F}
	for _, tc := range []struct {
		bps, i int
		want   uint64
	}{
		{8, 0, 0xB2}, {8, 1, 0x4F},
		{4, 0, 0xB}, {4, 1, 0x2}, {4, 2, 0x4}, {4, 3, 0xF},
		{1, 0, 1}, {1, 1, 0}, {1, 2, 1}, {1, 3, 1},
		{16, 0, 0xB24F},
		{2, 0, 0b10}, {2, 1, 0b11}, {2, 2, 0b00}, {2, 3, 0b10},
	} {
		if got := bitsAt(data, tc.i, tc.bps); got != tc.want {
			t.Errorf("bitsAt(%d bits, index %d) = %#x, want %#x", tc.bps, tc.i, got, tc.want)
		}
	}
}
