package pdf

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
)

// TestExtractText_GlyphPastTheCropBoxRightEdgeStaysOut pins that a glyph
// whose origin lies past the right edge of the crop box reads as no text,
// even when it continues a word inside the box: upright text advances
// rightward from its origin, so the whole glyph paints outside the visible
// region (ISO 32000-1, 14.11.2). "B" starts 0.404 units past the edge.
func TestExtractText_GlyphPastTheCropBoxRightEdgeStaysOut(t *testing.T) {
	t.Parallel()

	const content = "BT /F1 12 Tf 112.4 700 Td (AB) Tj ET\n"
	data := buildTestPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /CropBox [0 0 120 792] /Resources 4 0 R /Contents 5 0 R >>",
		"<< /Font << /F1 6 0 R >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}, "")

	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	got, err := r.ExtractText(ctx, 1)
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	if got = strings.TrimSpace(got); got != "A" {
		t.Fatalf("ExtractText = %q, want %q", got, "A")
	}
}

// offPageReader opens a one-page document of Helvetica text drawn by content
// on a Letter media box, cropped to crop when it is not empty.
func offPageReader(t *testing.T, content, crop string) *Reader {
	t.Helper()
	box := ""
	if crop != "" {
		box = " /CropBox " + crop
	}
	data := buildTestPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792]" + box + " /Resources 4 0 R /Contents 5 0 R >>",
		"<< /Font << /F1 6 0 R >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}, "")
	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

// TestExtractText_GlyphCutByTheCropBoxShows pins the crop box test glyph by
// glyph: a glyph any part of which lies inside the crop box shows, cut at the
// edge, and reads; one wholly outside it does not (ISO 32000-1, 14.11.2).
// "abcdefghij" at 10 points advances 5.56, 5.56, 5, 5.56, 5.56, 2.78, 5.56,
// 5.56, 2.22 and 2.22 units.
func TestExtractText_GlyphCutByTheCropBoxShows(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, content, crop, want string
	}{
		// "e" starts at 611.68 and ends past the edge; "f" starts past it.
		{"right edge", "BT /F1 10 Tf 590 700 Td (abcdefghij) Tj ET", "", "abcde"},
		// "c" starts at -0.88 and ends inside; "b" ends at -0.88.
		{"left edge", "BT /F1 10 Tf -12 700 Td (abcdefghij) Tj ET", "", "cdefghij"},
		// The same text scaled by the text matrix and by the CTM.
		{"left edge, text matrix", "BT /F1 1 Tf 10 0 0 10 -12 700 Tm (abcdefghij) Tj ET", "", "cdefghij"},
		{"left edge, CTM", "q 2 0 0 2 0 0 cm BT /F1 5 Tf -6 350 Td (abcdefghij) Tj ET Q", "", "cdefghij"},
		// A crop box inside the media box cuts "k" to "m" at x = 50.
		{"crop box left edge", "BT /F1 10 Tf 38 600 Td (klmnopqrst) Tj ET", "[50 50 562 742]", "mnopqrst"},
		// Turned a quarter, the text runs up through the top edge: "e"
		// starts at 791.68.
		{"turned text, top edge", "BT /F1 10 Tf 0 1 -1 0 300 770 Tm (abcdefghij) Tj ET", "", "abcde"},
		// Across the baseline the glyph is read at its baseline: capitals
		// on a baseline 3 units under the bottom edge are off the page,
		// those 1 unit over it on.
		{"bottom edge", "BT /F1 10 Tf 100 -3 Td (CUT) Tj ET BT /F1 10 Tf 200 1 Td (KEPT) Tj ET", "", "KEPT"},
		{"top edge", "BT /F1 10 Tf 100 793 Td (gyp) Tj ET BT /F1 10 Tf 200 788 Td (TOP) Tj ET", "", "TOP"},
		// Turned a quarter, the text runs up from under the bottom edge:
		// "m" starts at -4.78 and ends at 3.55, inside.
		{"turned text, bottom edge", "BT /F1 10 Tf 0 1 -1 0 200 -12 Tm (klmnopqrst) Tj ET", "", "mnopqrst"},
		{"wholly off", "BT /F1 10 Tf 700 400 Td (east) Tj ET BT /F1 10 Tf -100 400 Td (west) Tj ET " +
			"BT /F1 10 Tf 300 900 Td (north) Tj ET BT /F1 10 Tf 300 -50 Td (south) Tj ET BT /F1 10 Tf 300 400 Td (seen) Tj ET", "", "seen"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := offPageReader(t, tc.content, tc.crop)
			ctx := context.Background()
			got, err := r.ExtractText(ctx, 1)
			if err != nil {
				t.Fatalf("ExtractText: %v", err)
			}
			if got = strings.TrimSpace(got); got != tc.want {
				t.Errorf("ExtractText = %q, want %q", got, tc.want)
			}
			for _, opt := range []ExtractTextOption{WithClippedText(), WithHiddenText()} {
				all, err := r.ExtractText(ctx, 1, opt)
				if err != nil {
					t.Fatalf("ExtractText: %v", err)
				}
				// Every case draws text wholly off the page, which the
				// option brings back.
				if len(strings.TrimSpace(all)) <= len(got) {
					t.Errorf("ExtractText with the off-page text = %q, want more than %q", all, got)
				}
			}
		})
	}
}

// TestExtractTextElements_RunSplitAtTheCropBoxEdge pins that a run of glyphs
// cut by the edge of the crop box is reported as two elements, the part off
// the page with IsClipped set, so each element's flag holds for every glyph
// of it.
func TestExtractTextElements_RunSplitAtTheCropBoxEdge(t *testing.T) {
	t.Parallel()
	r := offPageReader(t, "BT /F1 10 Tf -12 700 Td (abcdefghij) Tj ET", "")
	els, err := r.ExtractTextElements(context.Background(), 1)
	if err != nil {
		t.Fatalf("ExtractTextElements: %v", err)
	}
	var got []string
	for _, el := range els {
		got = append(got, fmt.Sprintf("%s:%v", el.Text, el.IsClipped))
	}
	if want := []string{"ab:true", "cdefghij:false"}; strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("elements = %v, want %v", got, want)
	}
}
