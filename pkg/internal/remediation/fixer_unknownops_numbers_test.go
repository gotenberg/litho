package remediation

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// TestRepairMalformedNumbersKeepsOperatorArity pins that a numeric literal the
// content parser could not read stays part of its operator's arguments.
//
// Take a text matrix written as
//
//	-. -.002 .002 -. 59.64 760.026 Tm
//
// "-." carries no digit, so ISO 32000-1, 7.3.3 makes it not a number and the
// parser reports it as an operator. That splits the operand run, and removing it
// as an operator undefined by ISO 19005-2, 6.2.2 took the operands with it: Tm
// arrived with two of the six operands Table 108 requires, no text matrix was
// set, and the page's text stopped extracting while veraPDF still called the
// output conformant.
//
// # Corpus evidence
//
//   - pdfjs issue15604.pdf: writes its text matrix this way, and all 1027
//     characters of the page stopped extracting while veraPDF still called the
//     output conformant.
func TestRepairMalformedNumbersKeepsOperatorArity(t *testing.T) {
	t.Parallel()

	ops := []content.Operation{
		{Operator: "-."},
		{Operands: []ast.PdfObject{ast.PdfReal(-0.002), ast.PdfReal(0.002)}, Operator: "-."},
		{Operands: []ast.PdfObject{ast.PdfReal(59.64), ast.PdfReal(760.026)}, Operator: "Tm"},
	}

	got, repaired := repairMalformedNumbers(ops)
	if repaired != 2 {
		t.Errorf("repaired = %d, want 2", repaired)
	}
	if len(got) != 1 {
		t.Fatalf("got %d operations, want 1 (both malformed numbers fold into Tm): %+v", len(got), got)
	}
	if got[0].Operator != "Tm" {
		t.Fatalf("operator = %q, want Tm", got[0].Operator)
	}
	want := []float64{0, -0.002, 0.002, 0, 59.64, 760.026}
	if len(got[0].Operands) != len(want) {
		t.Fatalf("Tm got %d operands, want %d: %+v", len(got[0].Operands), len(want), got[0].Operands)
	}
	for i, w := range want {
		operand, ok := got[0].Operands[i].(ast.PdfReal)
		if !ok {
			t.Errorf("operand %d is %T, want ast.PdfReal", i, got[0].Operands[i])
			continue
		}
		if float64(operand) != w {
			t.Errorf("operand %d = %v, want %v", i, float64(operand), w)
		}
	}
}

// TestRepairMalformedNumbersLeavesRealOperatorsAlone pins that the repair only
// claims tokens built from numeric characters. A vendor operator owns its
// operands, so folding those into the next operator would corrupt a stream the
// removal path handles correctly.
func TestRepairMalformedNumbersLeavesRealOperatorsAlone(t *testing.T) {
	t.Parallel()

	ops := []content.Operation{
		{Operands: []ast.PdfObject{ast.PdfReal(1)}, Operator: "SomeVendorOp"},
		{Operands: []ast.PdfObject{ast.PdfReal(2)}, Operator: "Tj"},
	}
	got, repaired := repairMalformedNumbers(ops)
	if repaired != 0 {
		t.Errorf("repaired = %d, want 0", repaired)
	}
	if len(got) != 2 {
		t.Errorf("got %d operations, want both left in place", len(got))
	}
}

func TestMalformedNumberClassification(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		tok  string
		want bool
	}{
		{"-.", true},
		{".", true},
		{"-", true},
		{"+", true},
		{"1.2.3", true},
		{"--5", true},
		{"", false},
		{"Tm", false},
		{"Tj", false},
		{"BX", false},
		{"T*", false},
		{"d0", false},
	} {
		if got := malformedNumber(tc.tok); got != tc.want {
			t.Errorf("malformedNumber(%q) = %v, want %v", tc.tok, got, tc.want)
		}
	}
}

func TestNumericPrefixSalvagesValue(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		tok  string
		want float64
	}{
		{"-.", 0},      // no digit at all: negative zero
		{".", 0},       //
		{"-", 0},       //
		{"1.2.3", 1.2}, // the longest leading run that is a number
		{"--5", 0},     // the second sign ends the run before any digit
		{"12", 12},
	} {
		if got := numericPrefix(tc.tok); got != tc.want {
			t.Errorf("numericPrefix(%q) = %v, want %v", tc.tok, got, tc.want)
		}
	}
}

// malformedNumberPDF builds a one-page document whose text matrix carries tok
// in two of its six operand positions.
//
// # Corpus evidence
//
//   - pdfjs issue15604.pdf: carries "-." in two of its six Tm operand positions.
func malformedNumberPDF(tok string) []byte {
	content := "BT\n/F0 9.96 Tf\n" + tok + " -.002 .002 " + tok + " 59.64 760.026 Tm\n(hello) Tj\nET\n"

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R " +
			"/Resources << /Font << /F0 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}

	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	start := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, start)
	return b.Bytes()
}

// TestFixUnknownOpsRepairsRatherThanStrips pins the wiring: the fixer must run
// the repair before deciding what is an undefined operator, and must say so.
//
// Without it, stripping a malformed number as an operator undefined by
// ISO 19005-2, 6.2.2 takes the operands sitting in front of it, so Tm loses four
// of its six arguments and the page's text stops being placed at all.
//
// The token here carries digits ("0.0.0", two decimal points, which ISO 32000-2,
// 7.3.3 does not define). The digit-less spellings take the same repair; see
// TestFixUnknownOpsRespellsDigitlessNumbers.
func TestFixUnknownOpsRepairsRatherThanStrips(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := malformedNumberPDF("0.0.0")
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber(ctx))

	fixes, err := fixUnknownOps(ctx, alloc, doc, Options{}, nil, "")
	if err != nil {
		t.Fatalf("fixUnknownOps: %v", err)
	}
	// containsFix is an exact match; this assertion is about the substring.
	reported := slices.ContainsFunc(fixes, func(f string) bool {
		return strings.Contains(f, "malformed number")
	})
	if !reported {
		t.Errorf("the content stream was rewritten but no repair was reported: %v", fixes)
	}

	stream, ok := alloc.Pending()[4].(ast.PdfStream)
	if !ok {
		t.Fatalf("content stream not rewritten: %T", alloc.Pending()[4])
	}
	body, err := stream.Bytes(ctx)
	if err != nil {
		t.Fatalf("reading rewritten content: %v", err)
	}
	if !strings.Contains(string(body), "0 -0.002 0.002 0 59.64 760.026 Tm") {
		t.Errorf("Tm did not keep its six operands, so the text matrix is never set:\n%s", body)
	}
}

// TestFixUnknownOpsRespellsDigitlessNumbers pins that a digit-less spelling
// reaches the repair and comes out as 0, the number Adobe Reader and PDF.js
// read in it.
//
// veraPDF reads "-" and "+" as the largest 64-bit integer, and "-." and "." as
// the largest double, signed, which fail the implementation limits of ISO
// 19005-1, 6.1.12 and ISO 19005-2, 6.1.13, and the content parse reads them the
// same way. Written back as that value, the text matrix would move the text
// off the page, so the repair reads the stream with the spelling kept and
// writes 0, as it writes the numeric prefix of "0.0.0".
//
// # Corpus evidence
//
//   - pdfjs issue15604.pdf: writes "-." in two of its Tm operand positions,
//     and veraPDF 1.30 fails 6.1.12 t2 twice on it at PDF/A-1b.
func TestFixUnknownOpsRespellsDigitlessNumbers(t *testing.T) {
	t.Parallel()

	for _, tok := range []string{"-.", "-", "+", "."} {
		t.Run("the literal "+tok, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			data := malformedNumberPDF(tok)
			doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
			if err != nil {
				t.Fatalf("NewDocument: %v", err)
			}
			alloc := writer.NewObjAllocator(doc.HighestObjectNumber(ctx))

			fixes, err := fixUnknownOps(ctx, alloc, doc, Options{}, nil, "")
			if err != nil {
				t.Fatalf("fixUnknownOps: %v", err)
			}
			if !slices.Contains(fixes, "repaired 2 malformed number(s) in content streams") {
				t.Errorf("%q was not repaired: %v", tok, fixes)
			}
			stream, ok := alloc.Pending()[4].(ast.PdfStream)
			if !ok {
				t.Fatalf("content stream not rewritten: %T", alloc.Pending()[4])
			}
			body, err := stream.Bytes(ctx)
			if err != nil {
				t.Fatalf("reading rewritten content: %v", err)
			}
			if !strings.Contains(string(body), "0 -0.002 0.002 0 59.64 760.026 Tm") {
				t.Errorf("%q did not become 0 in the text matrix:\n%s", tok, body)
			}
		})
	}
}
