package pdf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// oneRoundRemediatedWriter opens data, remediates it at PDF/A-2b in a Writer and
// requires the remediation to keep its first round alone, so its repairs stay
// queued in the Writer rather than written into a file it edits.
func oneRoundRemediatedWriter(t *testing.T, data []byte) (*Writer, func()) {
	t.Helper()

	ctx := context.Background()
	rounds := &keptRounds{}
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)), WithLogger(slog.New(rounds)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	w := NewWriter(r)
	cleanup := func() {
		if err := w.Close(); err != nil {
			t.Errorf("Writer.Close: %v", err)
		}
		if err := r.Close(); err != nil {
			t.Errorf("Reader.Close: %v", err)
		}
	}
	report, err := w.RemediatePDFA(ctx, models.PDFA2b)
	if err != nil {
		cleanup()
		t.Fatalf("RemediatePDFA: %v", err)
	}
	if rounds.n.Load() != 0 {
		cleanup()
		t.Fatalf("the remediation kept %d later rounds, so the Writer edits its output and the test proves nothing", rounds.n.Load())
	}
	if len(report.Fixed) == 0 {
		cleanup()
		t.Fatal("the remediation fixed nothing, so the test proves nothing")
	}
	return w, cleanup
}

// writeAndReopen writes w and opens the output.
func writeAndReopen(t *testing.T, w *Writer) *Reader {
	t.Helper()

	ctx := context.Background()
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	r, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("reopening the output: %v", err)
	}
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Errorf("Reader.Close: %v", err)
		}
	})
	return r
}

// TestWriterMetadataEditAfterOneRemediationRoundReachesTheXMP pins that a
// metadata edit queued after a remediation that keeps one round moves the XMP
// packet the remediation wrote, as it does when it edits the remediated file.
//
// A PDF/A remediation of a document with no XMP writes a packet at a new object
// number, queued in the Writer, and the edit synchronised XMP from the packet
// the source document holds at that number: none, so the output carried the
// new title in /Info only and the remediation's packet kept the old one.
func TestWriterMetadataEditAfterOneRemediationRoundReachesTheXMP(t *testing.T) {
	t.Parallel()

	w, cleanup := oneRoundRemediatedWriter(t, signablePDF())
	defer cleanup()
	w.SetMetadata(Metadata{Title: "Edited title", Author: "Edited author"})

	r := writeAndReopen(t, w)
	packet := readXMPPacket(t, r)
	if packet == "" {
		t.Fatal("the output carries no XMP packet")
	}
	for _, want := range []string{"Edited title", "Edited author"} {
		if !strings.Contains(packet, want) {
			t.Errorf("the XMP packet lacks %q, which the /Info dictionary now states", want)
		}
	}
}

// TestWriterEditQueuedBeforeAStageIsAppliedOnce pins that an edit queued before
// a remediation that keeps one round, or before a redaction, reaches the output
// once.
//
// Both stages replay the edits queued before them, so that they see the
// document as staged, and keep what the edits staged in their overrides. The
// edits also stayed queued and were applied again on top of those overrides
// when the Writer was written: a stamp, which appends to the page each time it
// is applied, was drawn twice.
func TestWriterEditQueuedBeforeAStageIsAppliedOnce(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		stage func(ctx context.Context, t *testing.T, w *Writer)
	}{
		{
			name: "a remediation",
			stage: func(ctx context.Context, t *testing.T, w *Writer) {
				if _, err := w.RemediatePDFA(ctx, models.PDFA2b); err != nil {
					t.Fatalf("RemediatePDFA: %v", err)
				}
			},
		},
		{
			name: "a redaction",
			stage: func(ctx context.Context, t *testing.T, w *Writer) {
				report, err := w.Redact(ctx, []RedactRegion{{Page: 1, Rect: [4]float64{10, 690, 100, 720}}})
				if err != nil {
					t.Fatalf("Redact: %v", err)
				}
				if len(report.RemovedText) == 0 {
					t.Fatal("the redaction removed nothing, so the test proves nothing")
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			data := signablePDF()
			rounds := &keptRounds{}
			r, err := Open(ctx, bytes.NewReader(data), int64(len(data)), WithLogger(slog.New(rounds)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer r.Close()
			w := NewWriter(r)
			defer w.Close()
			if err := w.StampText(TextStamp{Text: "STAMPED", X: 50, Y: 50}); err != nil {
				t.Fatalf("StampText: %v", err)
			}
			tc.stage(ctx, t, w)
			if rounds.n.Load() != 0 {
				t.Fatalf("the remediation kept %d later rounds, which write the edits into a file, so the test proves nothing", rounds.n.Load())
			}

			out := writeAndReopen(t, w)
			text, err := out.ExtractText(ctx, 1, WithArtifacts())
			if err != nil {
				t.Fatalf("ExtractText: %v", err)
			}
			if n := strings.Count(text, "STAMPED"); n != 1 {
				t.Errorf("the page shows the stamp %d times, want once: %q", n, text)
			}
		})
	}
}

// textPagePDF builds a one-page document whose page shows text.
func textPagePDF(text string) []byte {
	content := "BT /F1 12 Tf 100 700 Td (" + text + ") Tj ET\n"
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	})
}

// pageTexts returns the text of every page of r, trimmed.
func pageTexts(t *testing.T, r *Reader) []string {
	t.Helper()

	ctx := context.Background()
	n, err := r.PageCount(ctx)
	if err != nil {
		t.Fatalf("PageCount: %v", err)
	}
	texts := make([]string, n)
	for i := range n {
		text, err := r.ExtractText(ctx, i+1)
		if err != nil {
			t.Fatalf("ExtractText page %d: %v", i+1, err)
		}
		texts[i] = strings.TrimSpace(text)
	}
	return texts
}

// TestWriterPageTreeEditsCompose pins that page deletions and insertions queued
// on one Writer build on each other's page tree.
//
// Each rebuilt the page tree from the Reader's pages, whatever an earlier one
// had staged: a second insertion dropped the first, an insertion after a
// deletion put the deleted page back, and a deletion after an insertion dropped
// the inserted pages. The pages they name stay the Reader's, as [Writer]
// documents.
func TestWriterPageTreeEditsCompose(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		edits func(t *testing.T, w *Writer, open func(string) *Reader)
		want  []string
	}{
		{
			name: "two insertions",
			edits: func(t *testing.T, w *Writer, open func(string) *Reader) {
				if err := w.InsertPages(3, open("InsertedA")); err != nil {
					t.Fatalf("InsertPages: %v", err)
				}
				if err := w.InsertPages(3, open("InsertedB")); err != nil {
					t.Fatalf("InsertPages: %v", err)
				}
			},
			want: []string{"Page1", "Page2", "Page3", "InsertedA", "InsertedB"},
		},
		{
			name: "a deletion then an insertion",
			edits: func(t *testing.T, w *Writer, open func(string) *Reader) {
				if err := w.DeletePages(2); err != nil {
					t.Fatalf("DeletePages: %v", err)
				}
				if err := w.InsertPages(3, open("InsertedA")); err != nil {
					t.Fatalf("InsertPages: %v", err)
				}
			},
			want: []string{"Page1", "Page3", "InsertedA"},
		},
		{
			name: "an insertion then a deletion",
			edits: func(t *testing.T, w *Writer, open func(string) *Reader) {
				if err := w.InsertPages(1, open("InsertedA")); err != nil {
					t.Fatalf("InsertPages: %v", err)
				}
				if err := w.DeletePages(3); err != nil {
					t.Fatalf("DeletePages: %v", err)
				}
			},
			want: []string{"Page1", "InsertedA", "Page2"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			data := threePagePDF()
			r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer r.Close()
			open := func(text string) *Reader {
				src := textPagePDF(text)
				sr, err := Open(ctx, bytes.NewReader(src), int64(len(src)))
				if err != nil {
					t.Fatalf("Open: %v", err)
				}
				t.Cleanup(func() {
					if err := sr.Close(); err != nil {
						t.Errorf("Reader.Close: %v", err)
					}
				})
				return sr
			}
			w := NewWriter(r)
			defer w.Close()
			tc.edits(t, w, open)

			if got := pageTexts(t, writeAndReopen(t, w)); !slices.Equal(got, tc.want) {
				t.Errorf("the output pages show %q, want %q", got, tc.want)
			}
		})
	}
}

// TestWriterRedactAfterAnXObjectStampRedacts pins that a redaction queued after
// an image or PDF stamp redacts the page, as it does on the stamped file.
//
// The stamp draws an XObject it queued at a new object number, and the
// redaction looked every XObject the page invokes up in the source document
// alone. The stamp's was not there, so the redaction refused the page as
// invoking content it could not follow, whatever the region covered.
func TestWriterRedactAfterAnXObjectStampRedacts(t *testing.T) {
	t.Parallel()

	pngData := buildPNG(t, false)
	for _, tc := range []struct {
		name  string
		stamp func(ctx context.Context, w *Writer) error
	}{
		{
			name: "an image stamp",
			stamp: func(_ context.Context, w *Writer) error {
				return w.StampImage(ImageStamp{Data: pngData, MimeType: "image/png", X: 300, Y: 300, Width: 20})
			},
		},
		{
			name: "a PDF stamp",
			stamp: func(ctx context.Context, w *Writer) error {
				src := textPagePDF("Stamp")
				r, err := Open(ctx, bytes.NewReader(src), int64(len(src)))
				if err != nil {
					return err
				}
				t.Cleanup(func() {
					if err := r.Close(); err != nil {
						t.Errorf("Reader.Close: %v", err)
					}
				})
				return w.StampPDF(PDFStamp{Source: r, Page: 1, X: 300, Y: 300, Scale: 0.1})
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			data := signablePDF()
			r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer r.Close()
			w := NewWriter(r)
			defer w.Close()
			if err := tc.stamp(ctx, w); err != nil {
				t.Fatalf("stamping: %v", err)
			}
			report, err := w.Redact(ctx, []RedactRegion{{Page: 1, Rect: [4]float64{10, 690, 100, 720}}})
			if err != nil {
				t.Fatalf("Redact: %v", err)
			}
			if !slices.Contains(report.RemovedText, "page") {
				t.Errorf("the redaction removed %q, want the page's text", report.RemovedText)
			}
			if text := strings.Join(pageTexts(t, writeAndReopen(t, w)), " "); strings.Contains(text, "page") {
				t.Errorf("the output page still shows the redacted text: %q", text)
			}
		})
	}
}

// TestWriterRedactOverAPDFStampRefusesAsOnTheStampedFile pins that a region
// covering the text a PDF stamp queued earlier draws is refused, as it is on
// the stamped file: the text lives in the stamp's form XObject, whose stream a
// redaction cannot edit (see [ErrRedactionRefused]).
//
// The text extraction the redaction runs looked the form up in the source
// document alone and did not find it, so it saw no text in the region and
// reported success over a page still showing it.
func TestWriterRedactOverAPDFStampRefusesAsOnTheStampedFile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	stampAndRedact := func(data []byte, stamp bool) error {
		r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		defer r.Close()
		w := NewWriter(r)
		defer w.Close()
		if stamp {
			src := textPagePDF("Stamp")
			sr, err := Open(ctx, bytes.NewReader(src), int64(len(src)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer sr.Close()
			// The stamp page draws its text at (100, 700), which the
			// translation moves to (100, 400).
			if err := w.StampPDF(PDFStamp{Source: sr, Page: 1, Y: -300}); err != nil {
				t.Fatalf("StampPDF: %v", err)
			}
		}
		_, err = w.Redact(ctx, []RedactRegion{{Page: 1, Rect: [4]float64{90, 390, 200, 420}}})
		return err
	}

	// The stamped file, written, then redacted on its own.
	r, err := Open(ctx, bytes.NewReader(signablePDF()), int64(len(signablePDF())))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	w := NewWriter(r)
	defer w.Close()
	src := textPagePDF("Stamp")
	sr, err := Open(ctx, bytes.NewReader(src), int64(len(src)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer sr.Close()
	if err := w.StampPDF(PDFStamp{Source: sr, Page: 1, Y: -300}); err != nil {
		t.Fatalf("StampPDF: %v", err)
	}
	var stamped bytes.Buffer
	if err := w.WriteTo(ctx, &stamped); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if err := stampAndRedact(stamped.Bytes(), false); !errors.Is(err, ErrRedactionRefused) {
		t.Fatalf("redacting the stamped file returned %v, want ErrRedactionRefused, so the test proves nothing", err)
	}

	if err := stampAndRedact(signablePDF(), true); !errors.Is(err, ErrRedactionRefused) {
		t.Errorf("redacting after the stamp in one Writer returned %v, want ErrRedactionRefused as on the stamped file", err)
	}
}

// TestWriterRedactAfterBookmarksScrubsTheirTitles pins that a redaction queued
// after an outline edit empties the titles of the staged outline that quote
// what it removed, as it does on the file carrying that outline.
//
// The outline pass walked the source document's outline, which the edit had
// replaced with one queued at new object numbers. It found no title to empty,
// the verification scan then found the staged title quoting the removed text,
// and the redaction refused a document it could have cleaned.
func TestWriterRedactAfterBookmarksScrubsTheirTitles(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := signablePDF()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	w := NewWriter(r)
	defer w.Close()
	w.SetBookmarks([]models.Bookmark{{Title: "The page", PageNumber: 1}})
	report, err := w.Redact(ctx, []RedactRegion{{Page: 1, Rect: [4]float64{10, 690, 100, 720}}})
	if err != nil {
		t.Fatalf("Redact: %v", err)
	}
	if !slices.Contains(report.RemovedText, "page") {
		t.Fatalf("the redaction removed %q, want the page's text, so the test proves nothing", report.RemovedText)
	}

	out := writeAndReopen(t, w)
	bookmarks, err := out.Bookmarks(ctx)
	if err != nil {
		t.Fatalf("Bookmarks: %v", err)
	}
	if len(bookmarks) != 1 {
		t.Fatalf("the output carries %d bookmarks, want the one set", len(bookmarks))
	}
	if strings.Contains(bookmarks[0].Title, "page") {
		t.Errorf("the bookmark title %q still quotes the redacted text", bookmarks[0].Title)
	}
}

// TestWriterGeneratedBookmarksKeepTheStagedOutline pins that bookmarks generated
// in the append and prepend modes join the outline an earlier edit on the
// Writer set, as they join the outline of the file carrying it.
//
// The modes read the existing outline from the Reader's document, which the
// earlier edit had not touched: it queued its own outline, so the generated
// bookmarks replaced it.
func TestWriterGeneratedBookmarksKeepTheStagedOutline(t *testing.T) {
	t.Parallel()

	for name, mode := range map[string]BookmarkMergeMode{"append": BookmarkAppend, "prepend": BookmarkPrepend} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			data := headingPDF()
			r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer r.Close()
			w := NewWriter(r)
			defer w.Close()
			w.SetBookmarks([]models.Bookmark{{Title: "Set earlier", PageNumber: 2, Children: []models.Bookmark{{Title: "Child", PageNumber: 1}}}})
			w.GenerateBookmarks(BookmarkGenerationOptions{}, mode)

			bookmarks, err := writeAndReopen(t, w).Bookmarks(ctx)
			if err != nil {
				t.Fatalf("Bookmarks: %v", err)
			}
			if len(bookmarks) < 2 {
				t.Fatalf("the output carries %d bookmarks, want the one set and the generated ones", len(bookmarks))
			}
			set := bookmarks[0]
			if mode == BookmarkPrepend {
				set = bookmarks[len(bookmarks)-1]
			}
			want := models.Bookmark{Title: "Set earlier", PageNumber: 2, Children: []models.Bookmark{{Title: "Child", PageNumber: 1}}}
			if set.Title != want.Title || set.PageNumber != want.PageNumber || len(set.Children) != 1 ||
				set.Children[0].Title != "Child" || set.Children[0].PageNumber != 1 {
				t.Errorf("the outline holds %+v where the bookmark set earlier belongs, want %+v", set, want)
			}
		})
	}
}

// namedDestinationsPDF builds a three-page document whose catalog names a
// destination on each page in a /Names /Dests tree. Each page draws its own
// text: Page1, Page2 and Page3.
func namedDestinationsPDF() []byte {
	page := func(n int) string {
		return fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents %d 0 R /Resources << /Font << /F1 9 0 R >> >> >>", 5+n)
	}
	stream := func(n int) string {
		content := fmt.Sprintf("BT /F1 12 Tf 100 700 Td (Page%d) Tj ET\n", n)
		return fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content)
	}
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /Names << /Dests 10 0 R >> >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R 5 0 R] /Count 3 >>",
		page(1), page(2), page(3),
		stream(1), stream(2), stream(3),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		"<< /Names [(p1) [3 0 R /Fit] (p2) [4 0 R /Fit] (p3) [5 0 R /Fit]] >>",
	})
}

// TestWriterDeletedPageStaysOutOfTheStagedReferences pins that a page deleted on
// a Writer stays out of the output when another edit on the same Writer
// references it, as it does when the edits run on the written files in turn.
//
// A deletion drops what still names the deleted page, which is otherwise enough
// to write the page into the output. It dropped only what the source document
// held: an outline an earlier edit set, and a /Names tree an attachment had
// queued afresh, kept their destinations, and so did an outline set after the
// deletion, and each carried the deleted page and its content into the file.
func TestWriterDeletedPageStaysOutOfTheStagedReferences(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		edits func(t *testing.T, w *Writer)
	}{
		{
			name: "an outline set before the deletion",
			edits: func(t *testing.T, w *Writer) {
				w.SetBookmarks([]models.Bookmark{{Title: "Third", PageNumber: 3}})
				if err := w.DeletePages(3); err != nil {
					t.Fatalf("DeletePages: %v", err)
				}
			},
		},
		{
			name: "an outline set after the deletion",
			edits: func(t *testing.T, w *Writer) {
				if err := w.DeletePages(3); err != nil {
					t.Fatalf("DeletePages: %v", err)
				}
				w.SetBookmarks([]models.Bookmark{{Title: "Third", PageNumber: 3}})
			},
		},
		{
			name: "named destinations an attachment requeued",
			edits: func(t *testing.T, w *Writer) {
				w.AddAttachment("note.txt", []byte("note"), "text/plain")
				if err := w.DeletePages(3); err != nil {
					t.Fatalf("DeletePages: %v", err)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			data := namedDestinationsPDF()
			r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer r.Close()
			w := NewWriter(r)
			defer w.Close()
			tc.edits(t, w)

			var out bytes.Buffer
			if err := w.WriteTo(ctx, &out); err != nil {
				t.Fatalf("WriteTo: %v", err)
			}
			if bytes.Contains(out.Bytes(), []byte("(Page3)")) {
				t.Error("the output carries the deleted page's content")
			}
			r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
			if err != nil {
				t.Fatalf("reopening the output: %v", err)
			}
			defer r2.Close()
			if got := pageTexts(t, r2); !slices.Equal(got, []string{"Page1", "Page2"}) {
				t.Errorf("the output pages show %q, want Page1 and Page2", got)
			}
		})
	}
}

// TestWriterGeneratedBookmarksSkipADeletedPage pins that bookmarks generated
// after a deletion on the same Writer name no heading of the deleted page, as
// generating them on the file the deletion wrote names none.
//
// Headings are detected in the Reader's pages, the deleted one among them, and
// each became a bookmark: its destination named the deleted page, which is
// enough to write the page into the output, and its title quoted the page.
func TestWriterGeneratedBookmarksSkipADeletedPage(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := headingPDF()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	generated, err := r.GenerateBookmarks(ctx, BookmarkGenerationOptions{})
	if err != nil {
		t.Fatalf("GenerateBookmarks: %v", err)
	}
	if !slices.ContainsFunc(generated, func(b models.Bookmark) bool { return b.Title == "Details" && b.PageNumber == 2 }) {
		t.Fatalf("the generator found %+v, not the heading of page 2, so the test proves nothing", generated)
	}

	w := NewWriter(r)
	defer w.Close()
	if err := w.DeletePages(2); err != nil {
		t.Fatalf("DeletePages: %v", err)
	}
	w.GenerateBookmarks(BookmarkGenerationOptions{}, BookmarkReplace)

	bookmarks, err := writeAndReopen(t, w).Bookmarks(ctx)
	if err != nil {
		t.Fatalf("Bookmarks: %v", err)
	}
	var titles []string
	var walk func([]models.Bookmark)
	walk = func(bms []models.Bookmark) {
		for _, b := range bms {
			titles = append(titles, b.Title)
			walk(b.Children)
		}
	}
	walk(bookmarks)
	if slices.Contains(titles, "Details") {
		t.Errorf("the outline %q names the heading of the deleted page", titles)
	}
	if !slices.Contains(titles, "Introduction") {
		t.Errorf("the outline %q lost the heading of the kept page", titles)
	}
}
