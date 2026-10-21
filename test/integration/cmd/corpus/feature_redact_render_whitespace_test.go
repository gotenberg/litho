package main

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/pdf"
)

// whitespaceGlyphPDF returns a 200 by 200 page drawing body, with /T3 a Type 3
// font whose code 32, named /space and so read as a space, paints a filled
// square one unit of font size wide and high.
func whitespaceGlyphPDF(body string) []byte {
	stream := func(dict, data string) string {
		return fmt.Sprintf("<< %s /Length %d >>\nstream\n%s\nendstream", dict, len(data), data)
	}
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 4 0 R /Resources << /Font << /T3 5 0 R >> >> >>",
		stream("", body),
		"<< /Type /Font /Subtype /Type3 /FontBBox [0 0 100 100] /FontMatrix [0.01 0 0 0.01 0 0]" +
			" /CharProcs << /space 6 0 R >> /Encoding << /Type /Encoding /Differences [32 /space] >>" +
			" /FirstChar 32 /LastChar 32 /Widths [100] /Resources << >> >>",
		stream("", "100 0 0 0 100 100 d1 0 0 100 100 re f"),
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
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

// TestRedactRenderCatchesAWhitespaceGlyphLeftInside plants a redaction that
// removes nothing over a glyph read as a space that paints a square inside the
// p1-quarter region, and requires the inside check to fail: excluding the
// glyphs a redaction removes from the outside check must not hide one it
// kept inside.
func TestRedactRenderCatchesAWhitespaceGlyphLeftInside(t *testing.T) {
	t.Parallel()
	data := whitespaceGlyphPDF("BT /T3 24 Tf 88 88 Td ( ) Tj ET")

	broken := newTestRedactRender()
	broken.redact = func(context.Context, *pdf.Writer, pdf.RedactRegion) (*pdf.RedactReport, error) { return nil, nil }
	v, sc := runRedactRender(t, broken, data, "p1-quarter")
	if sc.Litho == nil || sc.Litho.Inside == 0 || v.Metrics[metricInsideLeaked] == 0 {
		t.Errorf("a redaction that left the glyph passed the inside check: %+v", sc.Litho)
	}

	_, sc = runRedactRender(t, newTestRedactRender(), data, "p1-quarter")
	if sc.Litho == nil || !sc.Litho.passed() {
		t.Errorf("the real redaction failed a check: %+v (%s)", sc.Litho, sc.Cause)
	}
}

// TestRedactRenderExcludesRemovedWhitespaceGlyphs covers litho corpus
// cmapverabase_2b: a glyph read as a space whose square straddles the top edge
// of the p1-quarter region is removed whole, so its ink above the region goes
// too. Extraction drops the glyph's run, so only the redaction's report says
// it went, and the outside check must not count it.
func TestRedactRenderExcludesRemovedWhitespaceGlyphs(t *testing.T) {
	t.Parallel()
	data := whitespaceGlyphPDF("BT /T3 24 Tf 88 140 Td ( ) Tj ET")

	_, sc := runRedactRender(t, newTestRedactRender(), data, "p1-quarter")
	if sc.Litho == nil || !sc.Litho.passed() {
		t.Errorf("the real redaction failed a check: %+v (%s)", sc.Litho, sc.Cause)
	}

	// The report is what excludes the glyph: without it, its ink above the
	// region reads as changed outside.
	blind := newTestRedactRender()
	blind.redact = func(ctx context.Context, w *pdf.Writer, region pdf.RedactRegion) (*pdf.RedactReport, error) {
		_, err := w.Redact(ctx, []pdf.RedactRegion{region})
		return nil, err
	}
	_, sc = runRedactRender(t, blind, data, "p1-quarter")
	if sc.Litho == nil || sc.Litho.Outside == 0 {
		t.Errorf("the glyph's ink above the region was not read as changed without the report: %+v", sc.Litho)
	}
}
