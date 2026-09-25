package remediation

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// roundTripOperand serialises a one-operand Tj through [serializeOps] and parses
// the result back, returning the bytes a reader recovers.
func roundTripOperand(t *testing.T, in []byte) []byte {
	t.Helper()

	ops := []content.Operation{{
		Operator: "Tj",
		Operands: []ast.PdfObject{ast.PdfString(in)},
	}}
	data, err := serializeOps(ops)
	if err != nil {
		t.Fatalf("serializeOps: %v", err)
	}

	parsed, err := content.ParseStreamData(context.Background(), data)
	if err != nil {
		t.Fatalf("parsing %q: %v", string(data), err)
	}
	if len(parsed) != 1 || len(parsed[0].Operands) != 1 {
		t.Fatalf("parsing %q: got %d operation(s)", string(data), len(parsed))
	}
	s, ok := parsed[0].Operands[0].(ast.PdfString)
	if !ok {
		t.Fatalf("parsing %q: operand is %T, want a string", string(data), parsed[0].Operands[0])
	}
	return []byte(s)
}

// TestSerializeOpsPreservesCarriageReturn pins the byte a literal string cannot
// carry raw.
//
// ISO 32000-2, 7.3.4.2 has a reader normalise an end-of-line marker inside a
// literal string to a single LF: a lone CR becomes LF, and a CR LF pair becomes
// one LF, losing a byte. A content stream rewritten with the carriage return
// written raw therefore comes back saying something else: a character code
// 0x0D is read back as 0x0A and selects a different glyph.
//
// The test asserts the PARSE rather than the bytes on the wire: the bytes
// matched before this repair and the value still did not survive them.
//
// # Corpus evidence
//
//   - pdfjs issue18645.pdf: its font draws every code from 0x01 up, and the
//     code 0x0D selecting "C" was read back as 0x0A and drew "9" instead,
//     three times over.
func TestSerializeOpsPreservesCarriageReturn(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		in   []byte
	}{
		{"lone carriage return", []byte{0x41, 0x0D, 0x42}},
		{"carriage return line feed", []byte{0x41, 0x0D, 0x0A, 0x42}},
		{"carriage return at the end", []byte{0x41, 0x0D}},
		{"consecutive carriage returns", []byte{0x0D, 0x0D, 0x41}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := roundTripOperand(t, tc.in); !bytes.Equal(got, tc.in) {
				t.Errorf("round trip changed the operand: wrote % x, read back % x", tc.in, got)
			}
		})
	}
}

// TestSerializeOpsLeavesRoundTrippingBytesAlone pins the other half: the bytes
// that already survive are not escaped on the way out. A literal string carries
// LF, tab and every high byte as written (ISO 32000-2, 7.3.4.2 normalises only
// the end-of-line markers), and the parens and the reverse solidus were already
// escaped before this repair. Escaping more than the evidence calls for would
// churn every rewritten content stream for nothing.
func TestSerializeOpsLeavesRoundTrippingBytesAlone(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		in   []byte
	}{
		{"line feed", []byte{0x41, 0x0A, 0x42}},
		{"tab", []byte{0x41, 0x09, 0x42}},
		{"parentheses", []byte{0x41, 0x28, 0x29, 0x42}},
		{"reverse solidus", []byte{0x41, 0x5C, 0x42}},
		{"high byte", []byte{0x41, 0xFE, 0x42}},
		{"null", []byte{0x41, 0x00, 0x42}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := roundTripOperand(t, tc.in); !bytes.Equal(got, tc.in) {
				t.Errorf("round trip changed the operand: wrote % x, read back % x", tc.in, got)
			}
		})
	}
}
