package content

import (
	"testing"
)

func TestParseStreamData_SkipsInlineImage(t *testing.T) {
	// Content stream with text, an inline image, then more text.
	// The binary data between ID and EI contains arbitrary bytes.
	stream := []byte("BT /F1 12 Tf (Hello) Tj ET BI /W 10 /H 5 /BPC 8 /CS /RGB ID \x00\x01\x02\xFF\xFE\xFD \nEI BT (World) Tj ET")

	ops, err := ParseStreamData(stream)
	if err != nil {
		t.Fatalf("ParseStreamData failed: %v", err)
	}

	// Expected operators: BT, Tf, Tj, ET, BT, Tj, ET (BI/ID/EI skipped).
	expected := []string{"BT", "Tf", "Tj", "ET", "BT", "Tj", "ET"}
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

func TestParseStreamData_InlineImageAtEnd(t *testing.T) {
	// Inline image at the very end of the stream (EI followed by EOF).
	stream := []byte("BT (Hello) Tj ET BI /W 1 /H 1 /BPC 8 /CS /G ID \xFF EI")

	ops, err := ParseStreamData(stream)
	if err != nil {
		t.Fatalf("ParseStreamData failed: %v", err)
	}

	expected := []string{"BT", "Tj", "ET"}
	if len(ops) != len(expected) {
		t.Fatalf("expected %d ops, got %d", len(expected), len(ops))
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

	ops, err := ParseStreamData(stream)
	if err != nil {
		t.Fatalf("ParseStreamData failed: %v", err)
	}

	expected := []string{"BT", "Tj", "ET", "BT", "Tj", "ET"}
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

	ops, err := ParseStreamData(stream)
	if err != nil {
		t.Fatalf("ParseStreamData failed: %v", err)
	}

	expected := []string{"BT", "Tj", "ET", "BT", "Tj", "ET", "BT", "Tj", "ET"}
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

	ops, err := ParseStreamData(stream)
	if err != nil {
		t.Fatalf("ParseStreamData failed: %v", err)
	}

	expected := []string{"BT", "Tj", "ET"}
	if len(ops) != len(expected) {
		t.Fatalf("expected %d ops, got %d", len(expected), len(ops))
	}
}

func TestParseStreamData_NoInlineImage(t *testing.T) {
	// Normal stream with no inline images — ensure no regressions.
	stream := []byte("BT /F1 12 Tf (Hello World) Tj ET")

	ops, err := ParseStreamData(stream)
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

	ops, err := ParseStreamData(stream)
	if err != nil {
		t.Fatalf("ParseStreamData failed: %v", err)
	}

	expected := []string{"BT", "Tj", "ET"}
	if len(ops) != len(expected) {
		var got []string
		for _, op := range ops {
			got = append(got, op.Operator)
		}
		t.Fatalf("expected %d ops %v, got %d ops %v", len(expected), expected, len(ops), got)
	}
}
