package pdf

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// headingPagesPDF builds one page per title, each drawing its title at 24
// points over two lines of 10 point body text, so heading detection and
// auto-tagging have structure to find on every page.
func headingPagesPDF(titles ...string) []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"", // the page tree, once the page numbers are known
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	var kids []string
	for _, title := range titles {
		content := fmt.Sprintf("BT /F1 24 Tf 72 700 Td (%s) Tj ET\n"+
			"BT /F1 10 Tf 72 670 Td (Body text of the %s page, long enough to set the body size.) Tj ET\n"+
			"BT /F1 10 Tf 72 656 Td (More body text on the %s page, also at the body size.) Tj ET\n",
			title, title, title)
		pageNum := len(objs) + 1
		objs = append(objs,
			fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents %d 0 R"+
				" /Resources << /Font << /F1 3 0 R >> >> >>", pageNum+1),
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content))
		kids = append(kids, fmt.Sprintf("%d 0 R", pageNum))
	}
	objs[1] = fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), len(kids))
	return buildTestPDF(objs, "")
}

// TestWriterAutoTagSkipsADeletedPage pins that AutoTag queued after
// DeletePages tags the pages the Writer holds and leaves the deleted one
// alone.
//
// AutoTag read the Reader's page tree. It marked up the deleted page and gave
// its structure elements a /Pg naming it (ISO 32000-1, 14.7.4.3), a live
// reference the rewrite follows, so the output carried the deleted page's
// content while reporting one page fewer.
func TestWriterAutoTagSkipsADeletedPage(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := headingPagesPDF("Kept", "Removed")
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	w := NewWriter(r)
	defer w.Close()
	if err := w.DeletePages(2); err != nil {
		t.Fatalf("DeletePages: %v", err)
	}
	w.AutoTag()
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	if bytes.Contains(out.Bytes(), []byte("Removed page")) {
		t.Error("the output holds the deleted page's text")
	}
	got, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("reopening the output: %v", err)
	}
	defer got.Close()
	wantPages(t, got, strings.TrimSpace(mustText(t, got, 1)))
	if !bytes.Contains(out.Bytes(), []byte("/StructTreeRoot")) {
		t.Fatal("the output has no structure tree, so the test proves nothing")
	}
	// ISO 14289-1, 7.1 test 3 is content neither tagged nor an artifact.
	report, err := got.ValidatePDFUAReport(ctx, models.PDFUA1)
	if err != nil {
		t.Fatalf("ValidatePDFUAReport: %v", err)
	}
	for _, v := range report {
		if v.Clause == "7.1" && v.TestNumber == 3 {
			t.Errorf("the kept page is not tagged: %s", v.Error())
		}
	}
}

// mustText returns the text of page pageNum of r.
func mustText(t *testing.T, r *Reader, pageNum int) string {
	t.Helper()

	text, err := r.ExtractText(context.Background(), pageNum, WithArtifacts())
	if err != nil {
		t.Fatalf("ExtractText(%d): %v", pageNum, err)
	}
	return text
}

// stampPNG returns a 2x2 opaque PNG.
func stampPNG(t *testing.T) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	for x := range 2 {
		for y := range 2 {
			img.Set(x, y, color.RGBA{R: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encoding the stamp: %v", err)
	}
	return buf.Bytes()
}

// TestWriterEditsWithNoPageListReachInsertedPages pins that a stamp or a
// rotation given no page list covers the pages the Writer holds when it is
// queued, pages inserted before it included, and that one queued before an
// insertion leaves the inserted pages alone.
//
// All four read "every page" as the Reader's pages, so a page inserted on the
// same Writer came out unstamped and unrotated.
func TestWriterEditsWithNoPageListReachInsertedPages(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := headingPagesPDF("One", "Two")
	insert := headingPagesPDF("Inserted")
	stampDoc := headingPagesPDF("Overlay")

	for _, tc := range []struct {
		name string
		edit func(t *testing.T, w *Writer)
		// marked reports whether page n of the output carries the edit.
		marked func(t *testing.T, got *Reader, n int) bool
	}{
		{
			name: "text stamp",
			edit: func(t *testing.T, w *Writer) {
				if err := w.StampText(TextStamp{Text: "WATERMARK", X: 300, Y: 100}); err != nil {
					t.Fatalf("StampText: %v", err)
				}
			},
			marked: func(t *testing.T, got *Reader, n int) bool {
				return strings.Contains(mustText(t, got, n), "WATERMARK")
			},
		},
		{
			name: "image stamp",
			edit: func(t *testing.T, w *Writer) {
				if err := w.StampImage(ImageStamp{Data: stampPNG(t), MimeType: "image/png", X: 10, Y: 10, Width: 20}); err != nil {
					t.Fatalf("StampImage: %v", err)
				}
			},
			marked: func(t *testing.T, got *Reader, n int) bool {
				images, err := got.ExtractImages(ctx, n)
				if err != nil {
					t.Fatalf("ExtractImages(%d): %v", n, err)
				}
				return len(images) > 0
			},
		},
		{
			name: "PDF stamp",
			edit: func(t *testing.T, w *Writer) {
				src, err := Open(ctx, bytes.NewReader(stampDoc), int64(len(stampDoc)))
				if err != nil {
					t.Fatalf("Open: %v", err)
				}
				t.Cleanup(func() { _ = src.Close() })
				if err := w.StampPDF(PDFStamp{Source: src, Scale: 0.2}); err != nil {
					t.Fatalf("StampPDF: %v", err)
				}
			},
			marked: func(t *testing.T, got *Reader, n int) bool {
				return strings.Contains(mustText(t, got, n), "Overlay")
			},
		},
		{
			name: "rotation",
			edit: func(t *testing.T, w *Writer) {
				if err := w.RotatePages(90); err != nil {
					t.Fatalf("RotatePages: %v", err)
				}
			},
			marked: func(t *testing.T, got *Reader, n int) bool {
				page, err := got.Page(ctx, n)
				if err != nil {
					t.Fatalf("Page(%d): %v", n, err)
				}
				return page.Rotate == 90
			},
		},
	} {
		for _, editFirst := range []bool{false, true} {
			name := tc.name + ", queued after the insertion"
			if editFirst {
				name = tc.name + ", queued before the insertion"
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
				if err != nil {
					t.Fatalf("Open: %v", err)
				}
				defer r.Close()
				src, err := Open(ctx, bytes.NewReader(insert), int64(len(insert)))
				if err != nil {
					t.Fatalf("Open: %v", err)
				}
				defer src.Close()

				w := NewWriter(r)
				defer w.Close()
				if editFirst {
					tc.edit(t, w)
				}
				if err := w.InsertPages(1, src); err != nil {
					t.Fatalf("InsertPages: %v", err)
				}
				if !editFirst {
					tc.edit(t, w)
				}
				var out bytes.Buffer
				if err := w.WriteTo(ctx, &out); err != nil {
					t.Fatalf("WriteTo: %v", err)
				}
				got, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
				if err != nil {
					t.Fatalf("reopening the output: %v", err)
				}
				defer got.Close()

				if !strings.Contains(mustText(t, got, 2), "Inserted") {
					t.Fatalf("page 2 reads %q, want the inserted page", mustText(t, got, 2))
				}
				for n, want := range map[int]bool{1: true, 2: !editFirst, 3: true} {
					if got := tc.marked(t, got, n); got != want {
						t.Errorf("page %d carries the edit: %v, want %v", n, got, want)
					}
				}
			})
		}
	}
}

// TestWriterStampsEachPageWhereAViewerShowsIt pins that a text stamp lands at
// the X and Y it names from the bottom-left corner of the region a viewer
// shows (see [TextStamp]), on a page of the Reader and on a page an insertion
// staged alike, however the page states its boxes. The region is what pdf.js
// calls the page view: the crop box reduced to the media box (ISO 32000-1,
// 14.11.2.1), the media box where the crop box is no box a viewer uses, and US
// Letter where the media box is none either (see [document.UsableBox]).
//
// An inserted page read its boxes with a four-entry check alone, so a crop box
// missing its media box moved the stamp off the page, one of no width moved it
// by its corner, and one reaching past its media box left it unmoved where the
// page view starts elsewhere. A media box no viewer uses moved the stamp by its
// corner, where a viewer draws US Letter from (0, 0).
//
// # Corpus evidence
//
//   - pdfjs boundingBox_invalid.pdf: page 3 states /CropBox [600 800 1000 1000]
//     on /MediaBox [0 0 600 800]. Inserted, then stamped, its stamp was moved
//     by (600, 800), off the page.
//   - govdocs thread_300_300123.pdf, pdfjs bug1755507.pdf: state a crop box
//     reaching past the media box. Inserted, then stamped, their stamps were
//     not moved to the corner of the page view, (36, 36) and (39, 39).
//   - litho viewerboxes_2b.pdf: page 1 states /MediaBox [10 10 300], and a
//     stamp on it was moved by (10, 10).
func TestWriterStampsEachPageWhereAViewerShowsIt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, shape := range []struct {
		name, boxes string
		corner      [2]float64
	}{
		{"a crop box missing the media box", "/MediaBox [0 0 600 800] /CropBox [600 800 1000 1000]", [2]float64{0, 0}},
		{"a crop box of no width", "/MediaBox [0 0 600 800] /CropBox [50 50 50 400]", [2]float64{0, 0}},
		{"a crop box reaching past the media box", "/MediaBox [36 36 648 828] /CropBox [0 0 684 864]", [2]float64{36, 36}},
		{"a media box of three entries", "/MediaBox [10 10 300]", [2]float64{0, 0}},
		{"a media box of no width", "/MediaBox [5 5 5 800]", [2]float64{0, 0}},
		// Object 4 is the number 36, which the copy renumbers.
		{"a crop box of references", "/MediaBox [0 0 600 800] /CropBox [4 0 R 4 0 R 500 700]", [2]float64{36, 36}},
		{"control", "/MediaBox [20 20 620 820] /CropBox [30 30 600 800]", [2]float64{30, 30}},
	} {
		src := buildPDF([]string{
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R " + shape.boxes + " >>",
			"36",
		})
		for _, inserted := range []bool{false, true} {
			name := shape.name + ", on the Reader's page"
			if inserted {
				name = shape.name + ", on an inserted page"
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				target, pageNum := src, 1
				if inserted {
					target, pageNum = labelledPDF(1, ""), 2
				}
				out := writeLabelled(t, target, func(w *Writer) error {
					if inserted {
						if err := w.InsertPages(1, openBytes(t, src)); err != nil {
							return err
						}
					}
					return w.StampText(TextStamp{Text: "MARK", X: 100, Y: 100})
				})
				r := openBytes(t, out)
				defer r.Close()
				elements, err := r.ExtractTextElements(ctx, pageNum)
				if err != nil {
					t.Fatalf("ExtractTextElements: %v", err)
				}
				want := [2]float64{shape.corner[0] + 100, shape.corner[1] + 100}
				found := false
				for _, e := range elements {
					if !strings.Contains(e.Text, "MARK") {
						continue
					}
					found = true
					if got := [2]float64{e.X, e.Y}; !almostEqual(got[0], want[0], 0.01) || !almostEqual(got[1], want[1], 0.01) {
						t.Errorf("the stamp lands at %v, want %v", got, want)
					}
				}
				if !found {
					t.Errorf("page %d carries no stamp: %v", pageNum, elements)
				}
			})
		}
	}
}

// TestWriterGenerateBookmarksReadsInsertedPages pins that the headings
// GenerateBookmarks finds are those of the pages the Writer holds: an inserted
// page contributes its heading at its place in the document, and a deleted
// page contributes none.
//
// The headings came from the Reader's pages alone, so an inserted page got no
// bookmark and every page after it was named by its number in the Reader.
func TestWriterGenerateBookmarksReadsInsertedPages(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := headingPagesPDF("Alpha", "Beta", "Gamma")
	insert := headingPagesPDF("Inserted")

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	src, err := Open(ctx, bytes.NewReader(insert), int64(len(insert)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer src.Close()

	w := NewWriter(r)
	defer w.Close()
	if err := w.InsertPages(1, src); err != nil {
		t.Fatalf("InsertPages: %v", err)
	}
	if err := w.DeletePages(2); err != nil {
		t.Fatalf("DeletePages: %v", err)
	}
	w.GenerateBookmarks(BookmarkGenerationOptions{}, BookmarkReplace)
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	got, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("reopening the output: %v", err)
	}
	defer got.Close()

	outline, err := got.Bookmarks(ctx)
	if err != nil {
		t.Fatalf("Bookmarks: %v", err)
	}
	var titles []string
	for _, b := range outline {
		titles = append(titles, fmt.Sprintf("%s@%d", b.Title, b.PageNumber))
	}
	if want := []string{"Alpha@1", "Inserted@2", "Gamma@3"}; !slices.Equal(titles, want) {
		t.Errorf("the outline is %q, want %q", titles, want)
	}
}
