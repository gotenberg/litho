package content

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

func TestParseStreamData_EmitsInlineImageOp(t *testing.T) {
	// Content stream with text, an inline image, then more text.
	// The binary data between ID and EI contains arbitrary bytes.
	stream := []byte("BT /F1 12 Tf (Hello) Tj ET BI /W 10 /H 5 /BPC 8 /CS /RGB ID \x00\x01\x02\xFF\xFE\xFD \nEI BT (World) Tj ET")

	ops, err := ParseStreamData(context.Background(), stream)
	if err != nil {
		t.Fatalf("ParseStreamData failed: %v", err)
	}

	// Expected operators: BT, Tf, Tj, ET, BI, BT, Tj, ET.
	expected := []string{"BT", "Tf", "Tj", "ET", "BI", "BT", "Tj", "ET"}
	if len(ops) != len(expected) {
		var got []string
		for _, op := range ops {
			got = append(got, op.Operator)
		}
		t.Fatalf("expected %d ops %v, got %d ops %v", len(expected), expected, len(ops), got)
	}
	for i, want := range expected {
		if ops[i].Operator != want {
			t.Errorf("op[%d]: got %q, want %q", i, ops[i].Operator, want)
		}
	}

	// Verify the BI operation contains the inline image dict.
	biOp := ops[4]
	if len(biOp.Operands) != 1 {
		t.Fatalf("BI op: expected 1 operand, got %d", len(biOp.Operands))
	}
	dict, ok := biOp.Operands[0].(ast.PdfDict)
	if !ok {
		t.Fatalf("BI op: expected PdfDict operand, got %T", biOp.Operands[0])
	}
	// Check /W = 10.
	if w, ok := dict["W"]; !ok {
		t.Error("BI dict missing /W key")
	} else if wInt, ok := w.(ast.PdfInt); !ok || int(wInt) != 10 {
		t.Errorf("BI dict /W: got %v (%T), want 10", w, w)
	}
	// Check /CS = /RGB.
	if cs, ok := dict["CS"]; !ok {
		t.Error("BI dict missing /CS key")
	} else if csName, ok := cs.(ast.PdfName); !ok || string(csName) != "RGB" {
		t.Errorf("BI dict /CS: got %v (%T), want RGB", cs, cs)
	}
}

func TestParseStreamData_InlineImageAtEnd(t *testing.T) {
	// Inline image at the very end of the stream (EI followed by EOF).
	stream := []byte("BT (Hello) Tj ET BI /W 1 /H 1 /BPC 8 /CS /G ID \xFF EI")

	ops, err := ParseStreamData(context.Background(), stream)
	if err != nil {
		t.Fatalf("ParseStreamData failed: %v", err)
	}

	expected := []string{"BT", "Tj", "ET", "BI"}
	if len(ops) != len(expected) {
		var got []string
		for _, op := range ops {
			got = append(got, op.Operator)
		}
		t.Fatalf("expected %d ops %v, got %d ops %v", len(expected), expected, len(ops), got)
	}
	for i, want := range expected {
		if ops[i].Operator != want {
			t.Errorf("op[%d]: got %q, want %q", i, ops[i].Operator, want)
		}
	}
}

func TestParseStreamData_InlineImageWithFalseEI(t *testing.T) {
	// Binary data contains the bytes 'E' and 'I' adjacent but not as the
	// actual terminator (no preceding whitespace for the first occurrence).
	// The real EI has proper whitespace context.
	stream := []byte("BT (Before) Tj ET BI /W 2 /H 1 /BPC 8 /CS /G ID \xAAEI\xBB \nEI\nBT (After) Tj ET")

	ops, err := ParseStreamData(context.Background(), stream)
	if err != nil {
		t.Fatalf("ParseStreamData failed: %v", err)
	}

	expected := []string{"BT", "Tj", "ET", "BI", "BT", "Tj", "ET"}
	if len(ops) != len(expected) {
		var got []string
		for _, op := range ops {
			got = append(got, op.Operator)
		}
		t.Fatalf("expected %d ops %v, got %d ops %v", len(expected), expected, len(ops), got)
	}
}

func TestParseStreamData_MultipleInlineImages(t *testing.T) {
	// Two inline images separated by text operators.
	stream := []byte("BT (A) Tj ET BI /W 1 /H 1 /BPC 8 /CS /G ID \x00 \nEI BT (B) Tj ET BI /W 1 /H 1 /BPC 8 /CS /G ID \xFF \nEI BT (C) Tj ET")

	ops, err := ParseStreamData(context.Background(), stream)
	if err != nil {
		t.Fatalf("ParseStreamData failed: %v", err)
	}

	expected := []string{"BT", "Tj", "ET", "BI", "BT", "Tj", "ET", "BI", "BT", "Tj", "ET"}
	if len(ops) != len(expected) {
		var got []string
		for _, op := range ops {
			got = append(got, op.Operator)
		}
		t.Fatalf("expected %d ops %v, got %d ops %v", len(expected), expected, len(ops), got)
	}
}

func TestParseStreamData_InlineImageEmptyData(t *testing.T) {
	// Edge case: zero-length image data (ID immediately followed by
	// whitespace + EI).
	stream := []byte("BI /W 0 /H 0 /BPC 1 /CS /G ID \nEI\nBT (Done) Tj ET")

	ops, err := ParseStreamData(context.Background(), stream)
	if err != nil {
		t.Fatalf("ParseStreamData failed: %v", err)
	}

	expected := []string{"BI", "BT", "Tj", "ET"}
	if len(ops) != len(expected) {
		var got []string
		for _, op := range ops {
			got = append(got, op.Operator)
		}
		t.Fatalf("expected %d ops %v, got %d ops %v", len(expected), expected, len(ops), got)
	}
}

func TestParseStreamData_NoInlineImage(t *testing.T) {
	// Normal stream with no inline images. Ensures no regressions.
	stream := []byte("BT /F1 12 Tf (Hello World) Tj ET")

	ops, err := ParseStreamData(context.Background(), stream)
	if err != nil {
		t.Fatalf("ParseStreamData failed: %v", err)
	}

	expected := []string{"BT", "Tf", "Tj", "ET"}
	if len(ops) != len(expected) {
		t.Fatalf("expected %d ops, got %d", len(expected), len(ops))
	}
}

func TestParseStreamData_InlineImageWithArrayValue(t *testing.T) {
	// Inline image dictionary containing an array value (e.g., /D [1 0]).
	stream := []byte("BI /W 4 /H 4 /BPC 1 /CS /G /D [1 0] ID \x00\xFF\x00\xFF \nEI\nBT (OK) Tj ET")

	ops, err := ParseStreamData(context.Background(), stream)
	if err != nil {
		t.Fatalf("ParseStreamData failed: %v", err)
	}

	expected := []string{"BI", "BT", "Tj", "ET"}
	if len(ops) != len(expected) {
		var got []string
		for _, op := range ops {
			got = append(got, op.Operator)
		}
		t.Fatalf("expected %d ops %v, got %d ops %v", len(expected), expected, len(ops), got)
	}
	// Verify array value was parsed.
	biOp := ops[0]
	dict := biOp.Operands[0].(ast.PdfDict)
	if _, ok := dict["D"]; !ok {
		t.Error("BI dict missing /D key")
	}
}
