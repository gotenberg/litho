package pdf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"slices"
	"sync"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/models"
)

// sharedReaderOp is one use of a [Reader] that writes a document: a Writer
// built on it, or a standalone operation reading it as a source.
type sharedReaderOp struct {
	name string
	run  func(ctx context.Context, r *Reader) ([]byte, error)
}

// writeOnReader builds a Writer on r, lets edit queue what it asks for, and
// writes the result with opts.
func writeOnReader(ctx context.Context, r *Reader, edit func(*Writer) error, opts ...WriteOption) (_ []byte, err error) {
	w := NewWriter(r)
	defer func() {
		if closeErr := w.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("closing the writer: %w", closeErr))
		}
	}()
	if err := edit(w); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := w.WriteTo(ctx, &buf, opts...); err != nil {
		return nil, fmt.Errorf("WriteTo: %w", err)
	}
	return buf.Bytes(), nil
}

// sharedReaderOps lists every kind of write a Reader can be the source of: the
// remediations, which repair the trailer and the metadata; the edits queued on
// a Writer, incremental and full rewrite; encryption and signing; the write
// options that reach the document, such as the logger; redaction; and the
// standalone operations and the page imports that read a Reader as a source.
func sharedReaderOps(t *testing.T) []sharedReaderOp {
	t.Helper()

	id := []byte("0123456789abcdef")
	signer := newCMSTestSigner(t)
	logger := slog.New(slog.DiscardHandler)
	remediate := func(profile models.PDFAProfile, opts ...RemediateOption) func(context.Context, *Reader) ([]byte, error) {
		return func(ctx context.Context, r *Reader) ([]byte, error) {
			return writeOnReader(ctx, r, func(w *Writer) error {
				_, err := w.RemediatePDFA(ctx, profile, append(opts, WithDocumentID(id))...)
				return err
			})
		}
	}

	return []sharedReaderOp{
		{"RemediatePDFA-2b", remediate(models.PDFA2b)},
		{"RemediatePDFA-4", remediate(models.PDFA4)},
		{"RemediatePDFA-2b-aggressive", remediate(models.PDFA2b, WithAggressiveFixes())},
		{"RemediatePDFUA-1", func(ctx context.Context, r *Reader) ([]byte, error) {
			return writeOnReader(ctx, r, func(w *Writer) error {
				_, err := w.RemediatePDFUA(ctx, models.PDFUA1, WithDocumentID(id))
				return err
			})
		}},
		{"EditThenRemediate", func(ctx context.Context, r *Reader) ([]byte, error) {
			return writeOnReader(ctx, r, func(w *Writer) error {
				w.SetMetadata(Metadata{Title: "edited"})
				_, err := w.RemediatePDFA(ctx, models.PDFA2b, WithDocumentID(id))
				return err
			})
		}},
		{"SetMetadata", func(ctx context.Context, r *Reader) ([]byte, error) {
			return writeOnReader(ctx, r, func(w *Writer) error {
				w.SetMetadata(Metadata{Title: "title", Author: "author"})
				return nil
			})
		}},
		{"IncrementalEdits", func(ctx context.Context, r *Reader) ([]byte, error) {
			return writeOnReader(ctx, r, func(w *Writer) error {
				if err := w.RotatePages(90, 1); err != nil {
					return err
				}
				if err := w.StampText(TextStamp{Text: "stamp", X: 50, Y: 50}); err != nil {
					return err
				}
				w.SetFormField("blank", "filled in")
				w.AddAttachment("note.txt", []byte("attached"), "text/plain")
				w.SetBookmarks([]models.Bookmark{{Title: "first", PageNumber: 1}})
				w.GenerateBookmarks(BookmarkGenerationOptions{}, BookmarkAppend)
				return nil
			})
		}},
		{"FullRewriteEdits", func(ctx context.Context, r *Reader) ([]byte, error) {
			return writeOnReader(ctx, r, func(w *Writer) error {
				w.FlattenForm()
				w.ClearMetadata()
				w.AutoTag()
				return w.DeletePages(2)
			}, WithObjectStreams(), WithDedup())
		}},
		{"InsertAndStampFromItself", func(ctx context.Context, r *Reader) ([]byte, error) {
			return writeOnReader(ctx, r, func(w *Writer) error {
				if err := w.InsertPages(0, r, 2); err != nil {
					return err
				}
				return w.StampPDF(PDFStamp{Source: r, Page: 1, Scale: 0.25})
			})
		}},
		{"Encrypt", func(ctx context.Context, r *Reader) ([]byte, error) {
			return writeOnReader(ctx, r, func(w *Writer) error {
				w.Encrypt(EncryptConfig{UserPassword: "u", OwnerPassword: "o", Algorithm: EncryptAES256, Permissions: -1})
				return nil
			})
		}},
		{"WithEncryption", func(ctx context.Context, r *Reader) ([]byte, error) {
			return writeOnReader(ctx, r, func(w *Writer) error {
				w.SetMetadata(Metadata{Title: "encrypted"})
				return nil
			}, WithEncryption(EncryptConfig{OwnerPassword: "o", Algorithm: EncryptAES128, Permissions: -1}))
		}},
		{"Sign", func(ctx context.Context, r *Reader) ([]byte, error) {
			return writeOnReader(ctx, r, func(w *Writer) error {
				w.SetMetadata(Metadata{Subject: "signed"})
				w.Sign(signer)
				return nil
			})
		}},
		{"SignFullRewrite", func(ctx context.Context, r *Reader) ([]byte, error) {
			return writeOnReader(ctx, r, func(w *Writer) error {
				w.Sign(signer)
				return nil
			}, WithFullRewrite(), WithWriteLogger(logger))
		}},
		{"WriteLogger", func(ctx context.Context, r *Reader) ([]byte, error) {
			return writeOnReader(ctx, r, func(w *Writer) error {
				w.SetMetadata(Metadata{Keywords: "logged"})
				return nil
			}, WithWriteLogger(logger))
		}},
		{"Redact", func(ctx context.Context, r *Reader) ([]byte, error) {
			return writeOnReader(ctx, r, func(w *Writer) error {
				w.SetMetadata(Metadata{Title: "before redaction"})
				_, err := w.Redact(ctx, []RedactRegion{{Page: 1, Rect: [4]float64{10, 690, 200, 720}}, {Page: 2, Rect: [4]float64{10, 690, 200, 720}}})
				return err
			})
		}},
		{"Split", func(ctx context.Context, r *Reader) ([]byte, error) {
			var buf bytes.Buffer
			err := Split(ctx, r, []int{2, 1}, &buf, WithWriteLogger(logger))
			return buf.Bytes(), err
		}},
		{"Merge", func(ctx context.Context, r *Reader) ([]byte, error) {
			var buf bytes.Buffer
			err := Merge(ctx, []*Reader{r, r}, &buf, WithWriteLogger(logger))
			return buf.Bytes(), err
		}},
	}
}

// readerForkFields records what [Reader.fork] does with each field of
// [Reader]: "forked" for the document, which the fork copies (see
// [document.Document.Fork]); "shared" for configuration the fork reads as the
// Reader does; "copied" for a value the fork holds its own copy of; "fresh"
// for state the fork starts empty. A field this table does not name fails
// TestReaderForkDecidesEveryField, so a Writer cannot lose a reading option
// the caller opened the Reader with.
var readerForkFields = map[string]string{
	"doc":             "forked",
	"ocr":             "shared",
	"closer":          "fresh",
	"pagesOnce":       "fresh",
	"cachedPages":     "fresh",
	"pagesErr":        "fresh",
	"ciiSchema":       "shared",
	"schSchema":       "shared",
	"schPhase":        "copied",
	"lenient":         "copied",
	"encryptedOrigin": "copied",
}

// TestReaderForkDecidesEveryField pins that [readerForkFields] names every
// field of [Reader] and nothing else.
func TestReaderForkDecidesEveryField(t *testing.T) {
	t.Parallel()

	typ := reflect.TypeFor[Reader]()
	seen := make(map[string]bool, typ.NumField())
	for f := range typ.Fields() {
		seen[f.Name] = true
		if _, ok := readerForkFields[f.Name]; !ok {
			t.Errorf("Reader.%s: decide what Reader.fork does with it and record it in readerForkFields", f.Name)
		}
	}
	for name := range readerForkFields {
		if !seen[name] {
			t.Errorf("readerForkFields names %s, which Reader no longer has", name)
		}
	}
}

// readerView is what a Reader reports about its document, for comparing the
// Reader before and after writes built on it.
type readerView struct {
	trailer   string
	metadata  string
	bookmarks int
	fields    int
	pageCount int
	text      []string
}

// viewOf reads r's view of its document. The trailer is read from the
// document itself, since no public reader returns all of it.
func viewOf(ctx context.Context, t *testing.T, r *Reader) readerView {
	t.Helper()

	var v readerView
	var buf bytes.Buffer
	if _, err := writer.Serialize(ctx, &buf, r.doc.Index.Trailer); err != nil {
		t.Fatalf("serializing the trailer: %v", err)
	}
	v.trailer = buf.String()
	meta, err := r.Metadata(ctx)
	if err != nil {
		t.Fatalf("Metadata: %v", err)
	}
	v.metadata = fmt.Sprintf("%+v", meta)
	marks, err := r.Bookmarks(ctx)
	if err != nil {
		t.Fatalf("Bookmarks: %v", err)
	}
	v.bookmarks = len(marks)
	fields, err := r.FormFields(ctx)
	if err != nil {
		t.Fatalf("FormFields: %v", err)
	}
	v.fields = len(fields)
	if v.pageCount, err = r.PageCount(ctx); err != nil {
		t.Fatalf("PageCount: %v", err)
	}
	for p := 1; p <= v.pageCount; p++ {
		text, err := r.ExtractText(ctx, p)
		if err != nil {
			t.Fatalf("ExtractText(%d): %v", p, err)
		}
		v.text = append(v.text, text)
	}
	return v
}

func (v readerView) equal(o readerView) bool {
	return v.trailer == o.trailer && v.metadata == o.metadata && v.bookmarks == o.bookmarks &&
		v.fields == o.fields && v.pageCount == o.pageCount && slices.Equal(v.text, o.text)
}

// resolvedObjects resolves every object r's document lists and serializes it,
// by object number. Resolving caches each object, so a second call reads back
// the objects the first one cached, and tells an object edited in place from
// one left alone.
func resolvedObjects(ctx context.Context, t *testing.T, r *Reader) map[int]string {
	t.Helper()

	var nums []int
	for n := range r.doc.XRefEntries() {
		nums = append(nums, n)
	}
	out := make(map[int]string, len(nums))
	for _, n := range nums {
		obj, err := r.doc.Resolve(ctx, ast.PdfIndirectRef{ObjectNum: n})
		if err != nil {
			continue
		}
		var buf bytes.Buffer
		if _, err := writer.Serialize(ctx, &buf, obj); err != nil {
			t.Fatalf("serializing object %d: %v", n, err)
		}
		out[n] = buf.String()
	}
	return out
}

// TestWriterLeavesItsReaderUntouched pins that a write built on a Reader does
// not change what the Reader reports, one write at a time: its trailer, its
// metadata, its outline, its form, its pages and text, and every object its
// cache already held. A Reader is safe for concurrent use (see [Reader]), so a
// second caller may be reading it while the write runs, and whatever a Writer
// or a standalone operation stages belongs to its own output.
func TestWriterLeavesItsReaderUntouched(t *testing.T) {
	t.Parallel()

	data := richReaderPDF(t)
	for _, op := range sharedReaderOps(t) {
		t.Run(op.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			t.Cleanup(func() {
				if closeErr := r.Close(); closeErr != nil {
					t.Logf("closing reader: %v", closeErr)
				}
			})
			before := viewOf(ctx, t, r)
			cached := resolvedObjects(ctx, t, r)

			out, err := op.run(ctx, r)
			if err != nil {
				t.Fatalf("%s: %v", op.name, err)
			}
			if len(out) == 0 {
				t.Fatalf("%s wrote nothing", op.name)
			}

			if after := viewOf(ctx, t, r); !after.equal(before) {
				t.Errorf("%s changed the Reader:\nbefore %+v\nafter  %+v", op.name, before, after)
			}
			now := resolvedObjects(ctx, t, r)
			for n, was := range cached {
				if got, ok := now[n]; ok && got != was {
					t.Errorf("%s edited cached object %d in place:\nbefore %s\nafter  %s", op.name, n, was, got)
				}
			}
		})
	}
}

// TestWritersShareOneReader runs every kind of write against one Reader at
// once, with readers of the same Reader alongside, and pins that each write
// succeeds and the Reader reports afterwards what it reported before. Run it
// under -race: two remediations of one Reader used to write /ID into the one
// trailer both read.
func TestWritersShareOneReader(t *testing.T) {
	t.Parallel()

	data := richReaderPDF(t)
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := r.Close(); closeErr != nil {
			t.Logf("closing reader: %v", closeErr)
		}
	})
	before := viewOf(ctx, t, r)
	wantViolations, err := r.ValidatePDFA(ctx, models.PDFA2b)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}

	const rounds = 2
	ops := sharedReaderOps(t)
	var wg sync.WaitGroup
	for range rounds {
		for _, op := range ops {
			wg.Go(func() {
				out, err := op.run(ctx, r)
				if err != nil {
					t.Errorf("%s: %v", op.name, err)
					return
				}
				check, err := Open(ctx, bytes.NewReader(out), int64(len(out)), WithPassword("o"))
				if err != nil {
					t.Errorf("%s: reopening the output: %v", op.name, err)
					return
				}
				if _, err := check.PageCount(ctx); err != nil {
					t.Errorf("%s: counting the output's pages: %v", op.name, err)
				}
				if err := check.Close(); err != nil {
					t.Errorf("%s: closing the output: %v", op.name, err)
				}
			})
		}
		wg.Go(func() {
			got, err := r.ValidatePDFA(ctx, models.PDFA2b)
			if err != nil {
				t.Errorf("ValidatePDFA: %v", err)
				return
			}
			if len(got) != len(wantViolations) {
				t.Errorf("ValidatePDFA beside the writes: %d violations, want %d", len(got), len(wantViolations))
			}
		})
		wg.Go(func() {
			if _, err := r.Metadata(ctx); err != nil {
				t.Errorf("Metadata: %v", err)
			}
			for p := 1; p <= before.pageCount; p++ {
				if _, err := r.ExtractText(ctx, p); err != nil {
					t.Errorf("ExtractText(%d): %v", p, err)
				}
			}
		})
	}
	wg.Wait()

	if after := viewOf(ctx, t, r); !after.equal(before) {
		t.Errorf("the writes changed the Reader:\nbefore %+v\nafter  %+v", before, after)
	}
}
