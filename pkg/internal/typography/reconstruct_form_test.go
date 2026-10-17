package typography

import (
	"bytes"
	"context"
	"strconv"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/models"
)

// streamFonts returns one run font per element, all in the font F1 at
// size, placed in the streams and segments given.
func streamFonts(size float64, streams, segments []int) []runFont {
	f := &Font{}
	fonts := make([]runFont, len(streams))
	for i := range streams {
		fonts[i] = runFont{name: "F1", font: f, size: size, stream: streams[i], segment: segments[i]}
	}
	return fonts
}

// TestReconstructText_FormBoundaryLines pins where a line breaks at the
// edge of a form XObject. PDF.js reads a form with a text builder of its
// own: the form's first glyph begins a text item and is compared with
// nothing, and the glyph after the form is compared with the glyph before
// it. The reference then breaks a line between two items more than 2 units
// apart across the baseline. Each case is the same glyphs read in one
// stream, where the run rules decide.
func TestReconstructText_FormBoundaryLines(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		in        []models.TextElement
		size      float64
		streams   []int
		segments  []int
		want, one string
	}{
		{
			// The form's glyph sits 2.2 units up, inside a quarter of the font
			// size, so in one stream it continues the run; as a form's first
			// glyph it begins an item past the tolerance.
			name: "form glyph past the tolerance",
			in: []models.TextElement{
				{Text: "a", X: 100, Y: 500, Width: 6, FontSize: 10},
				{Text: "b", X: 106, Y: 502.2, Width: 6, FontSize: 10},
			},
			size:     10,
			streams:  []int{1, 3},
			segments: []int{2, 4},
			want:     "a\nb",
			one:      "ab",
		},
		{
			// A 1-unit font: the form's glyph 1.5 units up passes the font
			// size, which ends the line within a stream, but it has nothing
			// to compare with; the glyph after the form is compared with "a",
			// on its baseline, and not with the form's "b".
			name: "tiny font across a form",
			in: []models.TextElement{
				{Text: "a", X: 100, Y: 500, Width: 0.5, FontSize: 1},
				{Text: "b", X: 100.5, Y: 501.5, Width: 0.5, FontSize: 1},
				{Text: "c", X: 100.5, Y: 500, Width: 0.5, FontSize: 1},
			},
			size:     1,
			streams:  []int{1, 3, 1},
			segments: []int{2, 4, 5},
			want:     "abc",
			one:      "a\nb\nc",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			if got := reconstructText(ctx, tc.in, streamFonts(tc.size, tc.streams, tc.segments), nil); got != tc.want {
				t.Errorf("across streams: got %q, want %q", got, tc.want)
			}
			one := make([]int, len(tc.in))
			if got := reconstructText(ctx, tc.in, streamFonts(tc.size, one, one), nil); got != tc.one {
				t.Errorf("one stream: got %q, want %q", got, tc.one)
			}
		})
	}
}

// TestReconstructText_FormBoundarySpaces pins the word spaces PDF.js writes
// at the edge of a form XObject. Before the form's first glyph it writes
// none, not even a space glyph shown before the form. After the form it
// measures the gap from the glyph before the form, writes a space past the
// tracking threshold, and drops a space glyph the next glyph sets back
// over, as it does after a Do that paints an image.
//
// # Corpus evidence
//
//   - pdfjs issue14301.pdf: text a form XObject draws after a label read
//     "Prénom: 1/20" where PDF.js reads "Prénom:1/20".
//   - govdocs thread_400_400566.pdf page 58: "G2G3/S2/D", an image Do
//     between "3 " and a "/" set back over the space.
func TestReconstructText_FormBoundarySpaces(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		in       []models.TextElement
		streams  []int
		segments []int
		want     string
	}{
		{
			name: "no space before a form",
			in: []models.TextElement{
				{Text: "a", X: 100, Y: 500, Width: 6, FontSize: 10},
				{Text: " ", X: 106, Y: 500, Width: 3, FontSize: 10},
				{Text: "b", X: 130, Y: 500, Width: 6, FontSize: 10},
			},
			streams:  []int{1, 1, 3},
			segments: []int{2, 2, 4},
			want:     "ab",
		},
		{
			name: "space after a form measured from the glyph before it",
			in: []models.TextElement{
				{Text: "a", X: 100, Y: 500, Width: 6, FontSize: 10},
				{Text: "b", X: 300, Y: 500, Width: 6, FontSize: 10},
				{Text: "c", X: 108, Y: 500, Width: 6, FontSize: 10},
			},
			streams:  []int{1, 3, 1},
			segments: []int{2, 4, 5},
			want:     "ab c",
		},
		{
			name: "space glyph set back over after an image",
			in: []models.TextElement{
				{Text: "3", X: 152.029, Y: 498.959, Width: 5.125, FontSize: 10.25},
				{Text: " ", X: 157.029, Y: 498.959, Width: 2.562, FontSize: 10.25},
				{Text: "/", X: 156.959, Y: 498.959, Width: 2.85, FontSize: 10.25},
			},
			streams:  []int{1, 1, 1},
			segments: []int{2, 2, 3},
			want:     "3/",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := reconstructText(context.Background(), tc.in, streamFonts(1, tc.streams, tc.segments), nil); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// formEdgePDF shows "Name:" on the page, paints an image and then a form
// that shows "John" to its right, and shows "Doe" after the form.
func formEdgePDF() []byte {
	form := "BT /F1 10 Tf 150 700 Td (John) Tj ET"
	img := "\xff"
	body := "BT /F1 10 Tf 72 700 Td (Name:) Tj ET /Im1 Do /Fm1 Do BT /F1 10 Tf 72 680 Td (Doe) Tj ET"
	return []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n" +
		"5 0 obj\n<< /Font << /F1 4 0 R >> /XObject << /Fm1 7 0 R /Im1 8 0 R >> >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 5 0 R /Contents 6 0 R >>\nendobj\n" +
		"6 0 obj\n<< /Length " + strconv.Itoa(len(body)) + " >>\nstream\n" + body + "\nendstream\nendobj\n" +
		"7 0 obj\n<< /Type /XObject /Subtype /Form /BBox [0 0 612 792] " +
		"/Resources << /Font << /F1 4 0 R >> >> /Length " + strconv.Itoa(len(form)) +
		" >>\nstream\n" + form + "\nendstream\nendobj\n" +
		"8 0 obj\n<< /Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8 /Length 1 >>\nstream\n" +
		img + "\nendstream\nendobj\n" +
		"trailer\n<< /Size 9 /Root 1 0 R >>\n%%EOF\n")
}

func formEdgePage(t *testing.T) (*document.Document, *document.Page, []content.Operation) {
	t.Helper()
	ctx := context.Background()
	data := formEdgePDF()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil || len(pages) == 0 {
		t.Fatalf("ResolvePages: %v (%d pages)", err, len(pages))
	}
	ops, err := content.ParsePageContent(ctx, doc, pages[0].Dict)
	if err != nil {
		t.Fatalf("ParsePageContent: %v", err)
	}
	return doc, pages[0], ops
}

// TestExtractElements_RunFontsNameStreams pins that extraction names the
// stream and segment of each element: a form's glyphs sit in a stream of
// their own, the glyphs after the form in the page's stream again, and
// every Do, an image's included, starts a segment.
func TestExtractElements_RunFontsNameStreams(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc, page, ops := formEdgePage(t)

	machine := graphics.NewMachine([4]float64{0, 0, 612, 792})
	ex, err := extractElements(ctx, doc, machine, page, ops, extractionRecord{runFonts: true})
	if err != nil {
		t.Fatalf("extractElements: %v", err)
	}
	if len(ex.runFonts) != len(ex.elements) {
		t.Fatalf("got %d run fonts for %d elements, want one each", len(ex.runFonts), len(ex.elements))
	}
	first := map[byte]runFont{}
	for i, el := range ex.elements {
		if _, ok := first[el.Text[0]]; !ok {
			first[el.Text[0]] = ex.runFonts[i]
		}
	}
	name, john, doe := first['N'], first['J'], first['D']
	if john.stream == name.stream {
		t.Errorf("form glyph in the page's stream %d", name.stream)
	}
	if doe.stream != name.stream {
		t.Errorf("glyph after the form in stream %d, want the page's %d", doe.stream, name.stream)
	}
	if name.segment == john.segment || john.segment == doe.segment || name.segment == doe.segment {
		t.Errorf("segments %d, %d, %d, want three", name.segment, john.segment, doe.segment)
	}
}

// TestExtractText_FormFieldValueJoinsLabel pins the two halves end to end:
// the form's "John" to the right of "Name:" takes no space, as PDF.js reads
// it, and "Doe" below starts its own line.
func TestExtractText_FormFieldValueJoinsLabel(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc, page, ops := formEdgePage(t)

	machine := graphics.NewMachine([4]float64{0, 0, 612, 792})
	got, err := ExtractText(ctx, doc, machine, page, ops, ExtractionConfig{})
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	if want := "Name:John\nDoe"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
