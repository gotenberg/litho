package pdf

import (
	"bytes"
	"context"
	"errors"
	"strings"
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
	report, err := w.Redact(ctx, []RedactRegion{pixelCheckRegion})
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
	_, err := w.Redact(ctx, []RedactRegion{pixelCheckRegion})
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
	if _, err := w.Redact(ctx, regions); err != nil {
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

// type3ShadingPDF shows one Type 3 glyph whose description paints a shading
// over a 300-point square while its font states a box of a hundredth of
// that.
func type3ShadingPDF() []byte {
	shading := "<< /ShadingType 2 /ColorSpace /DeviceGray /Coords [0 0 1 0] " +
		"/Function << /FunctionType 2 /Domain [0 1] /C0 [0] /C1 [0] /N 1 >> /Extend [true true] >>"
	return type3GlyphPDF("1000 0 d0 q 0 0 3000 3000 re W n /Sh sh Q", "/Shading << /Sh "+shading+" >>")
}

// type3GlyphPDF shows, at 100 400 and Tf 100, the one glyph of a Type 3 font
// whose /FontMatrix scales glyph space by a thousandth and whose /FontBBox
// spans ten units, its description glyph run against the font resources
// resources.
func type3GlyphPDF(glyph, resources string) []byte {
	return type3GlyphShownPDF(glyph, resources, "")
}

// type3GlyphShownPDF is [type3GlyphPDF] with the operators before set before
// the glyph is shown.
func type3GlyphShownPDF(glyph, resources, before string) []byte {
	return []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /T3 4 0 R >> >> /Contents 6 0 R >>\nendobj\n" +
		"4 0 obj\n<< /Type /Font /Subtype /Type3 /FontBBox [0 0 10 10] /FontMatrix [0.001 0 0 0.001 0 0] /CharProcs << /a 5 0 R >> " +
		"/Encoding << /Type /Encoding /Differences [97 /a] >> /FirstChar 97 /LastChar 97 /Widths [1000] " +
		"/Resources << " + resources + " >> >>\nendobj\n" +
		plantStream(5, "", glyph) +
		plantStream(6, "", before+"BT /T3 100 Tf 100 400 Td (a) Tj ET") +
		"trailer\n<< /Size 7 /Root 1 0 R >>\n%%EOF\n")
}

// type3ShadingRegion lies over the shading, clear of the glyph's stated box.
var type3ShadingRegion = RedactRegion{Page: 1, Rect: [4]float64{250, 550, 350, 650}}

// TestRedactRemovesAType3GlyphByWhatItPaints redacts a region over the far
// side of the shading a Type 3 glyph paints, clear of the box its font
// states: the glyph is bounded by its painted extent, so the region selects
// it, and the pixel check finds nothing left.
//
// A glyph declaring a d1 box it paints far past is bounded the same way:
// ISO 32000-1, 9.6.5 does not clip it, and litho's renderer draws it whole.
func TestRedactRemovesAType3GlyphByWhatItPaints(t *testing.T) {
	ctx := context.Background()
	for name, src := range map[string][]byte{
		"shading over its own clip": type3ShadingPDF(),
		"fill past its d1 box":      type3GlyphPDF("1000 0 0 0 10 10 d1 0 0 3000 3000 re f", ""),
	} {
		t.Run(name, func(t *testing.T) {
			w := NewWriter(openPixelCheck(t, src))
			defer w.Close()
			report, err := w.Redact(ctx, []RedactRegion{type3ShadingRegion})
			if err != nil {
				t.Fatalf("Redact: %v", err)
			}
			boxes := report.RemovedGlyphBoxes[1]
			if len(boxes) != 1 {
				t.Fatalf("removed glyph boxes %v, want the one glyph", boxes)
			}
			// The glyph paints [100 400 400 700].
			if b := boxes[0]; b[0] > 100.5 || b[1] > 400.5 || b[2] < 399.5 || b[3] < 699.5 {
				t.Fatalf("removed glyph box %v does not hold what the glyph paints", b)
			}
		})
	}
}

// resourcelessType3PDF shows "BBB" at 20 200 and Tf 40 in a Type 3 font
// stating no /Resources, whose glyph B paints the form /Bar the page names: a
// bar 32 by 28, 4 points right of each glyph's origin.
func resourcelessType3PDF() []byte {
	return []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 400 400] " +
		"/Resources << /Font << /T3 4 0 R >> /XObject << /Bar 7 0 R >> >> /Contents 6 0 R >>\nendobj\n" +
		"4 0 obj\n<< /Type /Font /Subtype /Type3 /FontBBox [0 0 1000 1000] /FontMatrix [0.001 0 0 0.001 0 0] /CharProcs << /B 5 0 R >> " +
		"/Encoding << /Type /Encoding /Differences [66 /B] >> /FirstChar 66 /LastChar 66 /Widths [1000] >>\nendobj\n" +
		plantStream(5, "", "1000 0 d0 /Bar Do") +
		plantStream(6, "", "BT /T3 40 Tf 20 200 Td (BBB) Tj ET") +
		plantStream(7, "/Type /XObject /Subtype /Form /BBox [100 0 900 700]", "100 0 800 700 re f") +
		"trailer\n<< /Size 8 /Root 1 0 R >>\n%%EOF\n")
}

// TestRedactBoundsAResourcelessType3GlyphWhereShown redacts a region over the
// third glyph of a Type 3 font stating no /Resources. Its glyphs paint a form
// the page names, which ISO 32000-1, 9.6.5, Table 112 lets a description
// without resources take from the stream showing it: each glyph is bounded by
// the bar it paints there, not by the page, so the region removes the one
// glyph under it and keeps the two beside it, and the pixel check passes.
func TestRedactBoundsAResourcelessType3GlyphWhereShown(t *testing.T) {
	ctx := context.Background()
	w := NewWriter(openPixelCheck(t, resourcelessType3PDF()))
	defer w.Close()
	report, err := w.Redact(ctx, []RedactRegion{{Page: 1, Rect: [4]float64{110, 190, 130, 240}}})
	if err != nil {
		t.Fatalf("Redact: %v", err)
	}
	boxes := report.RemovedGlyphBoxes[1]
	if len(boxes) != 1 {
		t.Fatalf("removed glyph boxes %v, want the third glyph alone", boxes)
	}
	// The third glyph's origin is 100 200, its bar [104 200 136 228].
	if b := boxes[0]; b[0] < 99.5 || b[2] > 136.5 || b[1] < 199.5 || b[3] > 228.5 || b[2] < 135.5 || b[3] < 227.5 {
		t.Fatalf("removed glyph box %v, want the bar the glyph paints", b)
	}
}

// strokedTextPDF strokes a glyph with a pen 40 points wide and joins
// joins, which reaches past the outline the glyph's box holds.
func strokedTextPDF(joins string) []byte {
	return plantedPDF("BT /F1 24 Tf 1 Tr 40 w "+joins+" 100 400 Td (W) Tj ET", plant{})
}

// strokedTextRegion lies over the stroke above the outline's box.
var strokedTextRegion = RedactRegion{Page: 1, Rect: [4]float64{100, 432, 130, 442}}

// TestRedactRemovesStrokedTextByItsStroke redacts, with the pixel check off,
// a region over the stroke of a glyph and clear of its outline: the glyph is
// bounded by what its pen paints (ISO 32000-1, 9.3.6, Table 106), so the
// region selects it and no ink is left, under a round join and a miter join
// (8.4.3.5) alike.
func TestRedactRemovesStrokedTextByItsStroke(t *testing.T) {
	ctx := context.Background()
	for name, joins := range map[string]string{
		"miter join": "",
		"round join": "1 j",
	} {
		t.Run(name, func(t *testing.T) {
			src := strokedTextPDF(joins)
			w := NewWriter(openPixelCheck(t, src))
			defer w.Close()
			report, err := w.Redact(ctx, []RedactRegion{strokedTextRegion}, WithoutRedactionPixelCheck())
			if err != nil {
				t.Fatalf("Redact: %v", err)
			}
			if len(report.RemovedText) != 1 || report.RemovedText[0] != "W" {
				t.Fatalf("removed %q, want the stroked glyph", report.RemovedText)
			}
			var out bytes.Buffer
			if err := w.WriteTo(ctx, &out); err != nil {
				t.Fatalf("WriteTo: %v", err)
			}
			before := openPixelCheck(t, src)
			after := openPixelCheck(t, out.Bytes())
			if err := checkRedactionPixels(ctx, before, after, []RedactRegion{strokedTextRegion}, []int{1},
				redactedGlyphs{boxes: report.RemovedGlyphBoxes}); err != nil {
				t.Fatalf("the stroke is left in the region: %v", err)
			}
		})
	}
}

// TestRedactBoundsAStrokeByHalfItsPen checks a stroked glyph is bounded by
// half its pen past its box under either join: a region past that keeps
// the glyph, and the pixel check finds nothing of it there.
func TestRedactBoundsAStrokeByHalfItsPen(t *testing.T) {
	ctx := context.Background()
	// The outline's box tops out at 400 + 24, an em above the baseline; half
	// the pen reaches 20 above that.
	region := RedactRegion{Page: 1, Rect: [4]float64{100, 450, 130, 460}}
	for _, joins := range []string{"", "1 j"} {
		w := NewWriter(openPixelCheck(t, strokedTextPDF(joins)))
		report, err := w.Redact(ctx, []RedactRegion{region})
		w.Close()
		if err != nil {
			t.Fatalf("%q: Redact: %v", joins, err)
		}
		if len(report.RemovedText) != 0 {
			t.Fatalf("%q: removed %q, want the glyph kept", joins, report.RemovedText)
		}
	}
}

// inheritedPenPDF shows a Type 3 glyph whose description strokes a line
// with the pen it inherits from where it is shown (ISO 32000-1, 9.6.5),
// before set there: the line runs from 100 450 to 200 450.
func inheritedPenPDF(before string) []byte {
	return type3GlyphShownPDF("1000 0 d0 0 500 m 1000 500 l S", "", before)
}

// inheritedPenRegion lies over the pen of a line width of 4000 units in
// glyph space, 400 points under the font matrix and size, above the line it
// strokes.
var inheritedPenRegion = RedactRegion{Page: 1, Rect: [4]float64{120, 520, 180, 560}}

// TestRedactBoundsAType3StrokeByTheShowingPen redacts, with the pixel check
// off, a region over the pen a Type 3 glyph's stroke inherits from where the
// glyph is shown, clear of its line: the glyph's box, read from its
// description once per font, grows there by that pen, so the region selects
// it and no ink is left, whether w or a gs (8.4.5, Table 58, /LW) sets the
// width there.
func TestRedactBoundsAType3StrokeByTheShowingPen(t *testing.T) {
	ctx := context.Background()
	for name, src := range map[string][]byte{
		"inherited width": inheritedPenPDF("4000 w "),
		"width set by gs": []byte(strings.Replace(string(type3GlyphShownPDF("1000 0 d0 0 500 m 1000 500 l S", "", "/GS1 gs ")),
			"/Resources << /Font << /T3 4 0 R >> >>", "/Resources << /Font << /T3 4 0 R >> /ExtGState << /GS1 << /LW 4000 >> >> >>", 1)),
	} {
		t.Run(name, func(t *testing.T) {
			w := NewWriter(openPixelCheck(t, src))
			defer w.Close()
			report, err := w.Redact(ctx, []RedactRegion{inheritedPenRegion}, WithoutRedactionPixelCheck())
			if err != nil {
				t.Fatalf("Redact: %v", err)
			}
			if len(report.RemovedText) != 1 {
				t.Fatalf("removed %q, want the stroked glyph", report.RemovedText)
			}
			var out bytes.Buffer
			if err := w.WriteTo(ctx, &out); err != nil {
				t.Fatalf("WriteTo: %v", err)
			}
			if err := checkRedactionPixels(ctx, openPixelCheck(t, src), openPixelCheck(t, out.Bytes()),
				[]RedactRegion{inheritedPenRegion}, []int{1}, redactedGlyphs{boxes: report.RemovedGlyphBoxes}); err != nil {
				t.Fatalf("the pen is left in the region: %v", err)
			}
		})
	}
}

// TestRedactKeepsAType3StrokeUnderAThinShowingPen redacts the same region
// over a glyph shown under the initial pen, and under a wide pen the
// description replaces with its own: the line paints nowhere near it, so the
// glyph stays.
func TestRedactKeepsAType3StrokeUnderAThinShowingPen(t *testing.T) {
	ctx := context.Background()
	for name, src := range map[string][]byte{
		"initial pen":                  inheritedPenPDF(""),
		"description sets its own pen": type3GlyphShownPDF("1000 0 d0 10 w 0 500 m 1000 500 l S", "", "4000 w "),
	} {
		t.Run(name, func(t *testing.T) {
			w := NewWriter(openPixelCheck(t, src))
			defer w.Close()
			report, err := w.Redact(ctx, []RedactRegion{inheritedPenRegion})
			if err != nil {
				t.Fatalf("Redact: %v", err)
			}
			if len(report.RemovedText) != 0 {
				t.Fatalf("removed %q, want the glyph kept", report.RemovedText)
			}
		})
	}
}

// nestedPenPDF shows a Type 3 glyph whose description, after outer, shows
// a glyph of a second Type 3 font, one glyph-space unit to one: the nested
// description strokes a line with the pen it inherits from where it is
// shown (ISO 32000-1, 9.6.5), the outer description's state, itself the
// page's for every parameter outer leaves unset. before sets the page's
// state, and the line runs from 100 450 to 200 450 as [inheritedPenPDF]'s.
func nestedPenPDF(outer, before string) []byte {
	return []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /T3 4 0 R >> >> /Contents 6 0 R >>\nendobj\n" +
		"4 0 obj\n<< /Type /Font /Subtype /Type3 /FontBBox [0 0 10 10] /FontMatrix [0.001 0 0 0.001 0 0] /CharProcs << /a 5 0 R >> " +
		"/Encoding << /Type /Encoding /Differences [97 /a] >> /FirstChar 97 /LastChar 97 /Widths [1000] " +
		"/Resources << /Font << /In 7 0 R >> >> >>\nendobj\n" +
		plantStream(5, "", "1000 0 d0 "+outer+"BT /In 1000 Tf (b) Tj ET") +
		plantStream(6, "", before+"BT /T3 100 Tf 100 400 Td (a) Tj ET") +
		"7 0 obj\n<< /Type /Font /Subtype /Type3 /FontBBox [0 0 10 10] /FontMatrix [0.001 0 0 0.001 0 0] /CharProcs << /b 8 0 R >> " +
		"/Encoding << /Type /Encoding /Differences [98 /b] >> /FirstChar 98 /LastChar 98 /Widths [1000] /Resources << >> >>\nendobj\n" +
		plantStream(8, "", "1000 0 d0 0 500 m 1000 500 l S") +
		"trailer\n<< /Size 9 /Root 1 0 R >>\n%%EOF\n")
}

// TestRedactBoundsANestedType3StrokeByTheShowingPen redacts, with the pixel
// check off, the region over the pen a nested Type 3 glyph's stroke
// inherits through the outer glyph's description from the page: the outer
// glyph's box grows there by the page's pen, so the region selects it and
// no ink is left, whether the outer description leaves the width unset or
// sets only another parameter.
func TestRedactBoundsANestedType3StrokeByTheShowingPen(t *testing.T) {
	ctx := context.Background()
	for name, src := range map[string][]byte{
		"outer sets nothing":      nestedPenPDF("", "4000 w "),
		"outer sets the cap":      nestedPenPDF("0 J ", "4000 w "),
		"outer clips past it":     nestedPenPDF("-5000 -5000 10000 10000 re W n ", "4000 w "),
		"outer cm scales the pen": nestedPenPDF("2 0 0 2 0 -500 cm ", "2000 w "),
	} {
		t.Run(name, func(t *testing.T) {
			w := NewWriter(openPixelCheck(t, src))
			defer w.Close()
			report, err := w.Redact(ctx, []RedactRegion{inheritedPenRegion}, WithoutRedactionPixelCheck())
			if err != nil {
				t.Fatalf("Redact: %v", err)
			}
			if len(report.RemovedText) != 1 {
				t.Fatalf("removed %q, want the glyph showing the nested stroke", report.RemovedText)
			}
			var out bytes.Buffer
			if err := w.WriteTo(ctx, &out); err != nil {
				t.Fatalf("WriteTo: %v", err)
			}
			if err := checkRedactionPixels(ctx, openPixelCheck(t, src), openPixelCheck(t, out.Bytes()),
				[]RedactRegion{inheritedPenRegion}, []int{1}, redactedGlyphs{boxes: report.RemovedGlyphBoxes}); err != nil {
				t.Fatalf("the nested pen is left in the region: %v", err)
			}
		})
	}
}

// TestRedactKeepsANestedType3StrokeUnderTheOuterPen redacts the same region
// over a nested glyph whose outer description sets a thin width of its own
// before showing it, and over one shown under the initial pen: the nested
// stroke inherits the outer description's width, not the page's (ISO
// 32000-1, 9.6.5), and paints nowhere near the region, so the glyph stays.
func TestRedactKeepsANestedType3StrokeUnderTheOuterPen(t *testing.T) {
	ctx := context.Background()
	for name, src := range map[string][]byte{
		"outer sets its own width": nestedPenPDF("10 w ", "4000 w "),
		"outer width set by gs":    []byte(strings.Replace(string(nestedPenPDF("/GS1 gs ", "4000 w ")), "/Resources << /Font << /In 7 0 R >> >>", "/Resources << /Font << /In 7 0 R >> /ExtGState << /GS1 << /LW 10 >> >> >>", 1)),
		"initial pen":              nestedPenPDF("", ""),
	} {
		t.Run(name, func(t *testing.T) {
			w := NewWriter(openPixelCheck(t, src))
			defer w.Close()
			report, err := w.Redact(ctx, []RedactRegion{inheritedPenRegion})
			if err != nil {
				t.Fatalf("Redact: %v", err)
			}
			if len(report.RemovedText) != 0 {
				t.Fatalf("removed %q, want the glyph kept", report.RemovedText)
			}
		})
	}
}

// placeholderLeakRegion lies under the placeholder box the renderer draws
// for [placeholderBody]'s glyph, a quarter em below its baseline, and clear
// of the glyph's box, a fifth of an em below it: the redaction selects no
// glyph and leaves the frame's bottom edge inside the region.
var placeholderLeakRegion = RedactRegion{Page: 1, Rect: [4]float64{120, 348, 180, 356}}

func TestRedactRefusesALeakTheTokenScanCannotSeeByDefault(t *testing.T) {
	ctx := context.Background()
	for name, opts := range map[string][]RedactOption{
		"default": nil,
		// Deprecated, and still accepted: it asks for the default.
		"WithRedactionPixelCheck": {WithRedactionPixelCheck()},
	} {
		t.Run(name, func(t *testing.T) {
			src := plantedPDF(placeholderBody, plant{})
			w := NewWriter(openPixelCheck(t, src))
			defer w.Close()
			_, err := w.Redact(ctx, []RedactRegion{placeholderLeakRegion}, opts...)
			var pe *RedactionPixelError
			if !errors.As(err, &pe) || pe.Inside == 0 || !errors.Is(err, ErrRedactionRefused) {
				t.Fatalf("got %v, want the placeholder frame found inside the region", err)
			}
			var out bytes.Buffer
			if err := w.WriteTo(ctx, &out); err != nil {
				t.Fatalf("WriteTo: %v", err)
			}
			if !bytes.Equal(out.Bytes(), src) {
				t.Fatal("the refused redaction changed what the Writer writes")
			}
		})
	}
}

func TestRedactWithoutPixelCheckSkipsIt(t *testing.T) {
	ctx := context.Background()
	w := NewWriter(openPixelCheck(t, plantedPDF(placeholderBody, plant{})))
	defer w.Close()
	if _, err := w.Redact(ctx, []RedactRegion{placeholderLeakRegion}, WithoutRedactionPixelCheck()); err != nil {
		t.Fatalf("Redact without the pixel check: %v", err)
	}

	// A page the check cannot render refuses under the default, and the
	// opt-out never renders it.
	r := openPixelCheck(t, plantedPDF(pixelCheckBody, plant{}), WithLimits(Limits{MaxRenderPixels: 1000}))
	w2 := NewWriter(r)
	defer w2.Close()
	if _, err := w2.Redact(ctx, []RedactRegion{pixelCheckRegion}, WithoutRedactionPixelCheck()); err != nil {
		t.Fatalf("Redact without the pixel check rendered the page: %v", err)
	}
}

// TestRedactKeepsTheSnapOfASplitRectangle cuts the top of a filled rectangle
// whose top edge falls a third of the way into a pixel row, so it splits on
// both axes: painted as one path the pieces lose the snapping renderers give
// one rectangle and that row turns grey outside the region (pdfcpu
// FOSDEM14_HPC_devroom_14_GoCUDA). The pixel check holds the redaction to
// drawing the rectangle outside the region as before.
func TestRedactKeepsTheSnapOfASplitRectangle(t *testing.T) {
	ctx := context.Background()
	body := "0 g 81.871 204.344 454.406 -137.238 re f"
	r := openPixelCheck(t, plantedPDF(body, plant{}))
	w := NewWriter(r)
	defer w.Close()
	if _, err := w.Redact(ctx, []RedactRegion{{Page: 1, Rect: [4]float64{153, 198, 459, 594}}}); err != nil {
		t.Fatalf("Redact: %v", err)
	}
}

// TestRedactKeepsACutStrokeUnadjusted cuts a 1.5-point rule whose path turns
// down a slanted segment inside the region: what is left outside is one
// horizontal line, which stroke adjustment would snap from three anti-aliased
// rows to one solid row (govdocs thread_000_000753). The pixel check holds
// the redaction to drawing the rule outside the region as before.
func TestRedactKeepsACutStrokeUnadjusted(t *testing.T) {
	ctx := context.Background()
	body := "0 G 0 J 1 j 1.5 w 473.4 302.58 m 444.6 302.58 l 426.18 284.52 l S"
	r := openPixelCheck(t, plantedPDF(body, plant{}))
	w := NewWriter(r)
	defer w.Close()
	if _, err := w.Redact(ctx, []RedactRegion{{Page: 1, Rect: [4]float64{153, 198, 459, 594}}}); err != nil {
		t.Fatalf("Redact: %v", err)
	}
}
