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
	err := checkRedactionPixels(ctx, before, after, []RedactRegion{pixelCheckRegion}, []int{1}, redactedGlyphs{})
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
	err := checkRedactionPixels(ctx, before, after, []RedactRegion{pixelCheckRegion}, []int{1}, redactedGlyphs{})
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
	if err := checkRedactionPixels(ctx, before, after, []RedactRegion{region}, []int{1}, redactedGlyphs{}); err == nil {
		t.Fatal("the glyphs below the region changed, and no glyph box excused them")
	}
	removed := map[int][][4]float64{1: {{95, 690, 190, 730}}}
	if err := checkRedactionPixels(ctx, before, after, []RedactRegion{region}, []int{1}, redactedGlyphs{boxes: removed}); err != nil {
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

// placeholderBody shows "S" in Helvetica, which the document does not embed,
// at 200 points: the renderer draws it as a placeholder box a quarter em
// below the baseline, past the fifth of an em the glyph's box reaches.
const placeholderBody = "BT /F1 200 Tf 100 400 Td (S) Tj ET"

// placeholderRegion meets the top of the glyph only.
var placeholderRegion = RedactRegion{Page: 1, Rect: [4]float64{80, 560, 150, 640}}

// placeholderRemoved is what the redaction reports of the glyph: its box,
// from 0.2 em below the baseline to 1 em above it, and its origin.
var placeholderRemoved = redactedGlyphs{
	boxes:   map[int][][4]float64{1: {{100, 360, 233.4, 600}}},
	origins: map[int][][2]float64{1: {{100, 400}}},
}

func TestRedactPixelCheckClearsWhatAPlaceholderDrewForARemovedGlyph(t *testing.T) {
	ctx := context.Background()
	before := openPixelCheck(t, plantedPDF(placeholderBody, plant{}))
	after := openPixelCheck(t, plantedPDF("", plant{}))
	regions := []RedactRegion{placeholderRegion}
	boxesOnly := redactedGlyphs{boxes: placeholderRemoved.boxes}
	var pe *RedactionPixelError
	if err := checkRedactionPixels(ctx, before, after, regions, []int{1}, boxesOnly); !errors.As(err, &pe) || pe.Outside == 0 {
		t.Fatalf("got %v, want the placeholder's frame below the glyph box to count as changed without its origin", err)
	}
	if err := checkRedactionPixels(ctx, before, after, regions, []int{1}, placeholderRemoved); err != nil {
		t.Fatalf("what the page drew for the removed glyph counts as a change: %v", err)
	}

	r := openPixelCheck(t, plantedPDF(placeholderBody, plant{}))
	w := NewWriter(r)
	defer w.Close()
	if _, err := w.Redact(ctx, regions, WithRedactionPixelCheck()); err != nil {
		t.Fatalf("Redact: %v", err)
	}
}

func TestRedactPixelCheckFindsAKeptGlyphOverlappingARemovedPlaceholder(t *testing.T) {
	ctx := context.Background()
	// A negative character spacing draws the K over the S's box; the
	// "redaction" removed both, though only the S met the region.
	before := openPixelCheck(t, plantedPDF("BT /F1 200 Tf -60 Tc 100 400 Td (SK) Tj ET", plant{}))
	after := openPixelCheck(t, plantedPDF("", plant{}))
	err := checkRedactionPixels(ctx, before, after, []RedactRegion{placeholderRegion}, []int{1}, placeholderRemoved)
	var pe *RedactionPixelError
	if !errors.As(err, &pe) || pe.Outside == 0 {
		t.Fatalf("got %v, want the lost K found outside the region", err)
	}
}

func TestRedactPixelCheckKeepsAGlyphDrawnAtARemovedOriginInTheCheck(t *testing.T) {
	ctx := context.Background()
	// The I is overstruck at the S's origin and kept: what is drawn there
	// cannot be told apart, so none of it is cleared, and the "redaction"
	// that lost the I is found.
	before := openPixelCheck(t, plantedPDF(placeholderBody+" BT /F1 200 Tf 100 400 Td (I) Tj ET", plant{}))
	after := openPixelCheck(t, plantedPDF("", plant{}))
	err := checkRedactionPixels(ctx, before, after, []RedactRegion{placeholderRegion}, []int{1}, placeholderRemoved)
	var pe *RedactionPixelError
	if !errors.As(err, &pe) || pe.Outside == 0 {
		t.Fatalf("got %v, want the lost I found outside the region", err)
	}
}

// transferPDF draws a 2×2 grey image under a transfer function mapping every
// level into [0.9, 1], as a producer lightens a picture behind text.
func transferPDF(samples string) []byte {
	body := "q /GS1 gs 200 0 0 200 100 400 cm /Im1 Do Q"
	return []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " +
		"/Resources << /ExtGState << /GS1 4 0 R >> /XObject << /Im1 6 0 R >> >> /Contents 7 0 R >>\nendobj\n" +
		"4 0 obj\n<< /Type /ExtGState /TR 5 0 R >>\nendobj\n" +
		"5 0 obj\n<< /FunctionType 2 /Domain [0 1] /C0 [0.9] /C1 [1] /N 1 >>\nendobj\n" +
		plantStream(6, "/Type /XObject /Subtype /Image /Width 2 /Height 2 /ColorSpace /DeviceGray /BitsPerComponent 8", samples) +
		plantStream(7, "", body) +
		"trailer\n<< /Size 8 /Root 1 0 R >>\n%%EOF\n")
}

// transferRegion covers the image.
var transferRegion = RedactRegion{Page: 1, Rect: [4]float64{80, 380, 320, 620}}

func TestRedactPixelCheckReadsABlankedImageBeforeItsTransfer(t *testing.T) {
	ctx := context.Background()
	before := openPixelCheck(t, transferPDF("\x00\xff\x80\x40"))
	// Blanked to black, which the transfer paints near white.
	after := openPixelCheck(t, transferPDF("\x00\x00\x00\x00"))
	if err := checkRedactionPixels(ctx, before, after, []RedactRegion{transferRegion}, []int{1}, redactedGlyphs{}); err != nil {
		t.Fatalf("a blanked image under a transfer function counts as a leak: %v", err)
	}
}

func TestRedactPixelCheckFindsAnImageLeftUnderATransfer(t *testing.T) {
	ctx := context.Background()
	before := openPixelCheck(t, transferPDF("\x00\xff\x80\x40"))
	// The "redaction" left the picture, which the transfer turns pale.
	after := openPixelCheck(t, transferPDF("\x00\xff\x80\x40"))
	err := checkRedactionPixels(ctx, before, after, []RedactRegion{transferRegion}, []int{1}, redactedGlyphs{})
	var pe *RedactionPixelError
	if !errors.As(err, &pe) || pe.Inside == 0 {
		t.Fatalf("got %v, want the picture found inside the region", err)
	}
}
