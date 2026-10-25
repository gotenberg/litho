package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// overhangCLIPDF returns a page showing "A" at 60 pt at x 100, y 400, in an
// embedded TrueType font whose /Widths gives A an advance of 100 thousandths
// while its outline spans 667: the glyph inks to x 140, its advance box ends
// at x 106.
func overhangCLIPDF(t *testing.T) []byte {
	t.Helper()
	program, err := os.ReadFile("../../pkg/pdf/testdata/fonts/example_sans.ttf")
	if err != nil {
		t.Fatalf("reading the font: %v", err)
	}
	body := "BT /F1 60 Tf 100 400 Td (A) Tj ET"
	var b bytes.Buffer
	offsets := make([]int, 8)
	obj := func(n int, s string) {
		offsets[n] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", n, s)
	}
	b.WriteString("%PDF-1.7\n")
	obj(1, "<< /Type /Catalog /Pages 2 0 R >>")
	obj(2, "<< /Type /Pages /Kids [3 0 R] /Count 1 >>")
	obj(3, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>")
	obj(4, "<< /Type /Font /Subtype /TrueType /BaseFont /ExampleSans /FirstChar 65 /LastChar 65 /Widths [100] /Encoding /WinAnsiEncoding /FontDescriptor 6 0 R >>")
	obj(5, "<< /Length "+strconv.Itoa(len(body))+" >>\nstream\n"+body+"\nendstream")
	obj(6, "<< /Type /FontDescriptor /FontName /ExampleSans /Flags 32 /FontBBox [0 0 667 500] /ItalicAngle 0 /Ascent 500 /Descent 0 /CapHeight 500 /StemV 80 /FontFile2 7 0 R >>")
	offsets[7] = b.Len()
	fmt.Fprintf(&b, "7 0 obj\n<< /Length %d >>\nstream\n%s\nendstream\nendobj\n", len(program), program)
	xref := b.Len()
	b.WriteString("xref\n0 8\n0000000000 65535 f \n")
	for n := 1; n <= 7; n++ {
		fmt.Fprintf(&b, "%010d 00000 n \n", offsets[n])
	}
	fmt.Fprintf(&b, "trailer\n<< /Size 8 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xref)
	return b.Bytes()
}

// TestRedact_GlyphOutlinesRemoveAnOverhangingGlyph pins --glyph-outlines: a
// region over a glyph's outline but not its advance box removes nothing by
// default, which the pixel check refuses, and with the flag removes the
// glyph.
func TestRedact_GlyphOutlinesRemoveAnOverhangingGlyph(t *testing.T) {
	path := writeTempPDF(t, overhangCLIPDF(t))
	out := filepath.Join(t.TempDir(), "redacted.pdf")

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(),
		[]string{"redact", "-region", "1:115,380,85,70", "-o", out, path}, &stdout, &stderr); rc == exitOK {
		t.Fatal("a region over a kept glyph's outline passed the pixel check without --glyph-outlines")
	}

	stdout.Reset()
	stderr.Reset()
	rc := run(context.Background(),
		[]string{"redact", "-region", "1:115,380,85,70", "--glyph-outlines", "-f", "json", "-o", out, path}, &stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("exit %d, stderr: %s", rc, stderr.String())
	}
	var report struct {
		RemovedText []string `json:"removedText"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("report: %v (%s)", err, stdout.String())
	}
	if len(report.RemovedText) != 1 || report.RemovedText[0] != "A" {
		t.Errorf("removedText = %q, want [A]", report.RemovedText)
	}
}
