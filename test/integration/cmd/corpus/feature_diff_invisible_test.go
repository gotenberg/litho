package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// invisibleTextPDF returns a one-page PDF that shows "Seen" and holds
// "Hidden" in text rendering mode 3 (ISO 32000-1, 9.3.6), which paints
// nothing.
func invisibleTextPDF() []byte {
	content := "BT /F1 24 Tf 20 60 Td (Seen) Tj ET BT /F1 24 Tf 3 Tr 20 20 Td (Hidden) Tj 0 Tr ET"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	offsets := make([]int, 0, len(objects))
	for i, obj := range objects {
		offsets = append(offsets, buf.Len())
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, obj)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, off := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return buf.Bytes()
}

// TestReadDiffPages_KeepsInvisibleText checks that the diff cell reads the
// text a page holds invisible, as PDF.js reads the reference's: a
// converter that keeps text drawn otherwise as invisible text is scored on
// it.
func TestReadDiffPages_KeepsInvisibleText(t *testing.T) {
	doc, err := readDiffPages(t.Context(), invisibleTextPDF(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.pages) != 1 {
		t.Fatalf("%d pages, want 1", len(doc.pages))
	}
	if text := doc.pages[0].text; !strings.Contains(text, "Seen") || !strings.Contains(text, "Hidden") {
		t.Errorf("text %q, want the visible and the invisible words", text)
	}
}
