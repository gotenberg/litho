package pdf

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

// pixelCheckBody draws a word to redact at the top of the page, a word to
// keep at the bottom, and a blue square between them.
const pixelCheckBody = "BT /F1 24 Tf 100 700 Td (Secret) Tj ET " +
	"q 0 0 1 rg 300 300 100 100 re f Q " +
	"BT /F1 24 Tf 100 100 Td (Keep) Tj ET"

// pixelCheckRegion covers the word to redact.
var pixelCheckRegion = RedactRegion{Page: 1, Rect: [4]float64{80, 680, 300, 740}}

func openPixelCheck(t *testing.T, data []byte, opts ...OpenOption) *Reader {
	t.Helper()
	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)), opts...)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func TestRedactPixelCheckPassesAnExactRedaction(t *testing.T) {
	ctx := context.Background()
	r := openPixelCheck(t, plantedPDF(pixelCheckBody, plant{}))
	w := NewWriter(r)
	defer w.Close()
	report, err := w.Redact(ctx, []RedactRegion{pixelCheckRegion}, WithRedactionPixelCheck())
	if err != nil {
		t.Fatalf("Redact: %v", err)
	}
	if len(report.RemovedText) == 0 {
		t.Fatal("nothing removed")
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if bytes.Contains(out.Bytes(), []byte("Secret")) {
		t.Fatal("output still holds the redacted word")
	}
}

func TestRedactPixelCheckFindsContentLeftInsideARegion(t *testing.T) {
	ctx := context.Background()
	before := openPixelCheck(t, plantedPDF(pixelCheckBody, plant{}))
	// The "redacted" document still draws the word: what a redaction that
	// missed it would write.
	after := openPixelCheck(t, plantedPDF(pixelCheckBody, plant{}))
	err := checkRedactionPixels(ctx, before, after, []RedactRegion{pixelCheckRegion}, []int{1}, nil)
	var pe *RedactionPixelError
	if !errors.As(err, &pe) {
		t.Fatalf("got %v, want a RedactionPixelError", err)
	}
	if pe.Inside == 0 || pe.Page != 1 || pe.Region != pixelCheckRegion.Rect {
		t.Fatalf("got %+v, want inside pixels on page 1 in the region", pe)
	}
	if !errors.Is(err, ErrRedactionPixelCheck) || !errors.Is(err, ErrRedactionRefused) {
		t.Fatalf("%v matches neither sentinel", err)
	}
}

func TestRedactPixelCheckFindsAChangeOutsideTheRegions(t *testing.T) {
	ctx := context.Background()
	before := openPixelCheck(t, plantedPDF(pixelCheckBody, plant{}))
	// The word is gone, and so is the square no region covers.
	after := openPixelCheck(t, plantedPDF("BT /F1 24 Tf 100 100 Td (Keep) Tj ET", plant{}))
	err := checkRedactionPixels(ctx, before, after, []RedactRegion{pixelCheckRegion}, []int{1}, nil)
	var pe *RedactionPixelError
	if !errors.As(err, &pe) {
		t.Fatalf("got %v, want a RedactionPixelError", err)
	}
	if pe.Outside == 0 || pe.Inside != 0 {
		t.Fatalf("got %+v, want outside pixels only", pe)
	}
	// The square spans [300 300 400 400]; Where bounds it, within a pixel.
	if pe.Where[0] < 298 || pe.Where[1] < 298 || pe.Where[2] > 402 || pe.Where[3] > 402 {
		t.Fatalf("Where %v does not bound the square", pe.Where)
	}
}

func TestRedactPixelCheckIgnoresRemovedGlyphsReachingPastTheRegion(t *testing.T) {
	ctx := context.Background()
	before := openPixelCheck(t, plantedPDF(pixelCheckBody, plant{}))
	after := openPixelCheck(t, plantedPDF("q 0 0 1 rg 300 300 100 100 re f Q BT /F1 24 Tf 100 100 Td (Keep) Tj ET", plant{}))
	// A region meeting only the top of the word: the redaction removes the
	// glyphs whole, and their boxes say where the page changed for them.
	region := RedactRegion{Page: 1, Rect: [4]float64{80, 715, 300, 740}}
	if err := checkRedactionPixels(ctx, before, after, []RedactRegion{region}, []int{1}, nil); err == nil {
		t.Fatal("the glyphs below the region changed, and no glyph box excused them")
	}
	removed := map[int][][4]float64{1: {{95, 690, 190, 730}}}
	if err := checkRedactionPixels(ctx, before, after, []RedactRegion{region}, []int{1}, removed); err != nil {
		t.Fatalf("removed glyph boxes do not excuse their own pixels: %v", err)
	}
}

func TestRedactPixelCheckRefusesAPageItCannotRenderAndLeavesTheWriter(t *testing.T) {
	ctx := context.Background()
	src := plantedPDF(pixelCheckBody, plant{})
	r := openPixelCheck(t, src, WithLimits(Limits{MaxRenderPixels: 1000}))
	w := NewWriter(r)
	defer w.Close()
	_, err := w.Redact(ctx, []RedactRegion{pixelCheckRegion}, WithRedactionPixelCheck())
	var pe *RedactionPixelError
	if !errors.As(err, &pe) || pe.Err == nil || !errors.Is(err, ErrRedactionPixelCheck) {
		t.Fatalf("got %v, want a pixel check that could not render", err)
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if !bytes.Equal(out.Bytes(), src) {
		t.Fatal("the refused redaction changed what the Writer writes")
	}
}
