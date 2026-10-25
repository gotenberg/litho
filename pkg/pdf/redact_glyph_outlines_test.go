package pdf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"testing"
)

// glyphOverhangPDF returns a page showing "A" at 60 pt at x 100, y 400, and
// "B" at 60 pt at x 100, y 600, in a TrueType font it embeds whose /Widths
// gives A an advance of 100 thousandths: A's outline spans 667 thousandths
// wide (pkg/pdf/testdata/fonts/example_sans.ttf), so it inks from x 100 to
// 140 while its advance box ends at x 106 (ISO 32000-1, 9.2.4: the outline
// paints where glyph space puts it, the advance only moves the text
// position). B advances its full 667.
func glyphOverhangPDF(t *testing.T) []byte {
	t.Helper()
	program, err := os.ReadFile("testdata/fonts/example_sans.ttf")
	if err != nil {
		t.Fatalf("reading the font: %v", err)
	}
	body := "BT /F1 60 Tf 100 400 Td (A) Tj ET BT /F1 60 Tf 100 600 Td (B) Tj ET"
	var b bytes.Buffer
	offsets := make([]int, 9)
	obj := func(n int, s string) {
		offsets[n] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", n, s)
	}
	b.WriteString("%PDF-1.7\n")
	obj(1, "<< /Type /Catalog /Pages 2 0 R >>")
	obj(2, "<< /Type /Pages /Kids [3 0 R] /Count 1 >>")
	obj(3, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>")
	obj(4, "<< /Type /Font /Subtype /TrueType /BaseFont /ExampleSans /FirstChar 65 /LastChar 66 /Widths [100 667] /Encoding /WinAnsiEncoding /FontDescriptor 6 0 R >>")
	obj(5, "<< /Length "+strconv.Itoa(len(body))+" >>\nstream\n"+body+"\nendstream")
	obj(6, "<< /Type /FontDescriptor /FontName /ExampleSans /Flags 32 /FontBBox [0 0 667 500] /ItalicAngle 0 /Ascent 500 /Descent 0 /CapHeight 500 /StemV 80 /FontFile2 7 0 R >>")
	obj(7, "<< /Length "+strconv.Itoa(len(program))+" >>\nstream\n"+string(program)+"\nendstream")
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 8\n0000000000 65535 f \n")
	for n := 1; n <= 7; n++ {
		fmt.Fprintf(&b, "%010d 00000 n \n", offsets[n])
	}
	fmt.Fprintf(&b, "trailer\n<< /Size 8 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xref)
	return b.Bytes()
}

// glyphOverhangRegion covers x 115 to 200 around "A": its outline, not its
// advance box.
var glyphOverhangRegion = []RedactRegion{{Page: 1, Rect: [4]float64{115, 380, 200, 450}}}

// redactOutlined runs a redaction with opts and returns the written bytes, or the
// redaction's error.
func redactOutlined(t *testing.T, src []byte, regions []RedactRegion, opts ...RedactOption) ([]byte, *RedactReport, error) {
	t.Helper()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(src), int64(len(src)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	w := NewWriter(r)
	defer w.Close()
	report, err := w.Redact(ctx, regions, opts...)
	if err != nil {
		return nil, report, err
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	return out.Bytes(), report, nil
}

// TestWriter_Redact_GlyphOutlinesRemoveAGlyphWhoseOutlineReachesIn pins
// outline selection, the default: a region over the outline of a glyph whose
// advance box lies outside it removes the glyph, its box in the report
// holding its outline, and the pixel check passes. Under
// [WithoutRedactionGlyphOutlines] the region selects nothing, the glyph keeps
// painting inside it and the pixel check refuses; a later
// [WithRedactionGlyphOutlines] undoes it.
func TestWriter_Redact_GlyphOutlinesRemoveAGlyphWhoseOutlineReachesIn(t *testing.T) {
	t.Parallel()
	src := glyphOverhangPDF(t)
	if got := pageLetters(t, src); got != "AB" {
		t.Fatalf("source letters = %q, want AB", got)
	}

	for name, opts := range map[string][]RedactOption{
		"default":          nil,
		"opted out and in": {WithoutRedactionGlyphOutlines(), WithRedactionGlyphOutlines()},
	} {
		out, report, err := redactOutlined(t, src, glyphOverhangRegion, opts...)
		if err != nil {
			t.Fatalf("%s: outline selection: %v", name, err)
		}
		if got := pageLetters(t, out); got != "B" {
			t.Errorf("%s: outline selection letters = %q, want B", name, got)
		}
		boxes := report.RemovedGlyphBoxes[1]
		if len(boxes) != 1 || boxes[0][2] < 139 {
			t.Errorf("%s: removed glyph boxes = %v, want one reaching A's outline at x 140", name, boxes)
		}
	}

	if _, _, err := redactOutlined(t, src, glyphOverhangRegion, WithoutRedactionGlyphOutlines()); !errors.Is(err, ErrRedactionPixelCheck) {
		t.Errorf("advance-box selection: err = %v, want ErrRedactionPixelCheck (the kept A paints inside the region)", err)
	}
	out, _, err := redactOutlined(t, src, glyphOverhangRegion, WithoutRedactionGlyphOutlines(), WithoutRedactionPixelCheck())
	if err != nil {
		t.Fatalf("advance-box selection without the pixel check: %v", err)
	}
	if got := pageLetters(t, out); got != "AB" {
		t.Errorf("advance-box selection letters = %q, want AB", got)
	}
}

// TestWriter_Redact_GlyphOutlinesKeepWhatNoOutlineReaches pins that outline
// selection removes nothing the regions' outlines do not reach: a region right
// of A's outline keeps both glyphs.
func TestWriter_Redact_GlyphOutlinesKeepWhatNoOutlineReaches(t *testing.T) {
	t.Parallel()
	src := glyphOverhangPDF(t)
	region := []RedactRegion{{Page: 1, Rect: [4]float64{150, 380, 200, 450}}}
	out, report, err := redactOutlined(t, src, region)
	if err != nil {
		t.Fatalf("Redact: %v", err)
	}
	if out != nil {
		if got := pageLetters(t, out); got != "AB" {
			t.Errorf("letters = %q, want AB", got)
		}
	}
	if len(report.RemovedText) != 0 {
		t.Errorf("removed %q, want nothing", report.RemovedText)
	}
}
