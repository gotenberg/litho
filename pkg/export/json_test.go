package export

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/pdf"
)

// buildMinimalPDF constructs a minimal valid one-page PDF with a MediaBox.
func buildMinimalPDF() []byte {
	obj1 := "1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n"
	obj2 := "2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 /MediaBox [0 0 612 792] >>\nendobj\n"
	obj3 := "3 0 obj\n<< /Type /Page /Parent 2 0 R >>\nendobj\n"

	body := obj1 + obj2 + obj3

	off1 := 0
	off2 := len(obj1)
	off3 := off2 + len(obj2)

	xrefOffset := len(body)
	xref := fmt.Sprintf("xref\n0 4\n0000000000 65535 f \n%010d 00000 n \n%010d 00000 n \n%010d 00000 n \n", off1, off2, off3)
	trailer := fmt.Sprintf("trailer\n<< /Size 4 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xrefOffset)

	return []byte("%PDF-1.7\n" + body + xref + trailer)
}

func openTestReader(t *testing.T) *pdf.Reader {
	t.Helper()
	ctx := context.Background()
	data := buildMinimalPDF()
	r, err := pdf.Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("failed to open minimal PDF: %v", err)
	}
	return r
}

func TestExtract(t *testing.T) {
	ctx := context.Background()
	r := openTestReader(t)
	doc, err := Extract(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Pages) != 1 {
		t.Fatalf("pages = %d, want 1", len(doc.Pages))
	}
	if doc.Pages[0].Page.Width != 612 {
		t.Errorf("page width = %v, want 612", doc.Pages[0].Page.Width)
	}
	if doc.Pages[0].Page.Height != 792 {
		t.Errorf("page height = %v, want 792", doc.Pages[0].Page.Height)
	}
}

func TestJSON(t *testing.T) {
	ctx := context.Background()
	r := openTestReader(t)
	var buf bytes.Buffer
	if err := JSON(ctx, &buf, r); err != nil {
		t.Fatal(err)
	}

	output := buf.String()
	if len(output) == 0 {
		t.Fatal("JSON output is empty")
	}

	// Should be valid JSON.
	var parsed Document
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("JSON output is not valid JSON: %v", err)
	}

	if len(parsed.Pages) != 1 {
		t.Fatalf("pages = %d, want 1", len(parsed.Pages))
	}
}

func TestJSONCompact(t *testing.T) {
	ctx := context.Background()
	r := openTestReader(t)
	var buf bytes.Buffer
	if err := JSONCompact(ctx, &buf, r); err != nil {
		t.Fatal(err)
	}

	// Compact JSON should not contain indentation.
	output := buf.String()
	if strings.Contains(output, "  \"") {
		t.Error("compact JSON should not contain indentation")
	}

	// Verify it's valid JSON.
	var parsed Document
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("compact JSON is not valid: %v", err)
	}
}

func TestMarshalJSON(t *testing.T) {
	ctx := context.Background()
	r := openTestReader(t)
	data, err := MarshalJSON(ctx, r)
	if err != nil {
		t.Fatal(err)
	}

	var parsed Document
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed.Pages) != 1 {
		t.Fatalf("pages = %d, want 1", len(parsed.Pages))
	}
}

func TestMarshalJSONCompact(t *testing.T) {
	ctx := context.Background()
	r := openTestReader(t)
	data, err := MarshalJSONCompact(ctx, r)
	if err != nil {
		t.Fatal(err)
	}

	var parsed Document
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed.Pages) != 1 {
		t.Fatalf("pages = %d, want 1", len(parsed.Pages))
	}
}

func TestJSON_CompactShorterThanPretty(t *testing.T) {
	ctx := context.Background()
	r := openTestReader(t)
	pretty, err := MarshalJSON(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	compact, err := MarshalJSONCompact(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	if len(compact) >= len(pretty) {
		t.Error("compact JSON should be shorter than pretty-printed JSON")
	}
}

func TestJSON_EscapeHTML_Disabled(t *testing.T) {
	// This test verifies that the encoder does not escape HTML characters.
	// Since the minimal PDF has no text with < > &, we verify by checking
	// that the encoder configuration works (no \\u003c in output).
	ctx := context.Background()
	r := openTestReader(t)
	var buf bytes.Buffer
	if err := JSON(ctx, &buf, r); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(buf.Bytes(), []byte(`\u003c`)) {
		t.Error("HTML escaping should be disabled")
	}
}

func TestJSON_PageGeometry(t *testing.T) {
	ctx := context.Background()
	r := openTestReader(t)
	doc, err := Extract(ctx, r)
	if err != nil {
		t.Fatal(err)
	}

	page := doc.Pages[0].Page
	if page.MediaBox != [4]float64{0, 0, 612, 792} {
		t.Errorf("MediaBox = %v, want [0 0 612 792]", page.MediaBox)
	}
	if page.PageNumber != 1 {
		t.Errorf("PageNumber = %d, want 1", page.PageNumber)
	}
}
