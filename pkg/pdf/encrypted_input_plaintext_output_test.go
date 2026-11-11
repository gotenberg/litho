package pdf

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// encryptedPassword is the user password of the encrypted fixtures below.
const encryptedPassword = "s3cret"

// TestWriter_AnEditOnAnEncryptedSourceWritesAPlaintextOutput pins the
// default for every edit of a Writer over a source opened with its password:
// with no [Writer.Encrypt] and no [WithEncryption], the output opens without
// a password. The Reader decrypts every object it resolves, and the Writer
// encrypts only when asked.
//
// GenerateBookmarks runs with SkipHeaderFooter, PreferStructureTree and
// DetectNumbering set, the choices the removed command made for its callers;
// the zero value of [BookmarkGenerationOptions] leaves them false.
//
// Redact runs on a single page whose text the region covers whole: on a
// document repeating that page, the text left on the other copy makes the
// redaction refuse with [ErrRedactionIncomplete], encrypted or not.
//
// FlattenTransparency runs on a page it flattens. On an opaque document it
// queues no edit, and a Writer with no edit copies its source, encryption
// included; the removed command passed [WithFullRewrite] to rewrite it.
func TestWriter_AnEditOnAnEncryptedSourceWritesAPlaintextOutput(t *testing.T) {
	t.Parallel()

	doc := encryptBytes(t, threePagePDF(), encryptedPassword)
	if opensWithoutAPassword(t, doc) {
		t.Fatal("precondition: the encrypted fixture opens with no password")
	}
	onePage := encryptBytes(t, untaggedHeadingPDF(), encryptedPassword)
	form := encryptBytes(t, formPDF(), encryptedPassword)
	translucent := encryptBytes(t, translucentCyanPDF(), encryptedPassword)
	png := buildPNG(t, false)

	for _, tc := range []struct {
		name string
		src  []byte
		edit func(ctx context.Context, t *testing.T, w *Writer) error
	}{
		{"AddAttachment", doc, func(_ context.Context, _ *testing.T, w *Writer) error {
			w.AddAttachment("table.csv", []byte("a,b\n1,2\n"), "text/csv")
			return nil
		}},
		{"RemoveAttachment", doc, func(_ context.Context, _ *testing.T, w *Writer) error {
			w.RemoveAttachment("absent.csv")
			return nil
		}},
		{"AutoTag", doc, func(_ context.Context, _ *testing.T, w *Writer) error {
			w.AutoTag()
			return nil
		}},
		{"FlattenForm", form, func(_ context.Context, _ *testing.T, w *Writer) error {
			w.FlattenForm()
			return nil
		}},
		{"GenerateBookmarks", doc, func(_ context.Context, _ *testing.T, w *Writer) error {
			w.GenerateBookmarks(BookmarkGenerationOptions{
				SkipHeaderFooter:    true,
				PreferStructureTree: true,
				DetectNumbering:     true,
			}, BookmarkReplace)
			return nil
		}},
		{"SetFormField", form, func(_ context.Context, _ *testing.T, w *Writer) error {
			w.SetFormField("name", "x")
			return nil
		}},
		{"SetMetadata", doc, func(_ context.Context, _ *testing.T, w *Writer) error {
			w.SetMetadata(Metadata{Title: "T"})
			return nil
		}},
		{"StampText", doc, func(_ context.Context, _ *testing.T, w *Writer) error {
			return w.StampText(TextStamp{Text: "DRAFT"})
		}},
		{"StampImage", doc, func(_ context.Context, _ *testing.T, w *Writer) error {
			return w.StampImage(ImageStamp{Data: png, MimeType: "image/png"})
		}},
		{"StampPDF", doc, func(_ context.Context, t *testing.T, w *Writer) error {
			return w.StampPDF(PDFStamp{Source: openColorPDF(t, untaggedHeadingPDF())})
		}},
		{"RemediatePDFA", doc, func(ctx context.Context, _ *testing.T, w *Writer) error {
			_, err := w.RemediatePDFA(ctx, models.PDFA2b)
			return err
		}},
		{"Redact", onePage, func(ctx context.Context, _ *testing.T, w *Writer) error {
			_, err := w.Redact(ctx, []RedactRegion{{Page: 1, Rect: [4]float64{0, 650, 600, 770}}})
			return err
		}},
		{"FlattenTransparency", translucent, func(ctx context.Context, _ *testing.T, w *Writer) error {
			_, err := w.FlattenTransparency(ctx, WithFlattenDPI(72))
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w := NewWriter(openColorPDF(t, tc.src, WithPassword(encryptedPassword)))
			t.Cleanup(func() { _ = w.Close() })
			if err := tc.edit(t.Context(), t, w); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if !opensWithoutAPassword(t, writeAll(t, w)) {
				t.Errorf("the output of %s needs a password, though no encryption was asked", tc.name)
			}
		})
	}
}

// TestPipeline_APageOperationOnAnEncryptedSourceWritesAPlaintextOutput is the
// pipeline half of [TestWriter_AnEditOnAnEncryptedSourceWritesAPlaintextOutput]:
// a page operation on an encrypted document, or one that copies pages from an
// encrypted source into a plain one, writes an output that opens without a
// password when no stage asks for encryption.
func TestPipeline_APageOperationOnAnEncryptedSourceWritesAPlaintextOutput(t *testing.T) {
	t.Parallel()

	enc := encryptBytes(t, threePagePDF(), encryptedPassword)
	for _, tc := range []struct {
		name  string
		build func(t *testing.T, encrypted, plain *Reader) *Pipeline
	}{
		{"delete", func(_ *testing.T, encrypted, _ *Reader) *Pipeline {
			return NewPipeline().Open(encrypted).Apply(func(_ context.Context, w *Writer) error { return w.DeletePages(1) })
		}},
		{"rotate", func(_ *testing.T, encrypted, _ *Reader) *Pipeline {
			return NewPipeline().Open(encrypted).Apply(func(_ context.Context, w *Writer) error { return w.RotatePages(90) })
		}},
		{"split", func(_ *testing.T, encrypted, _ *Reader) *Pipeline {
			return NewPipeline().Open(encrypted).Split([]int{1})
		}},
		{"merge, only the source is encrypted", func(_ *testing.T, encrypted, plain *Reader) *Pipeline {
			return NewPipeline().Open(plain).Merge([]*Reader{encrypted})
		}},
		{"insert, only the source is encrypted", func(_ *testing.T, encrypted, plain *Reader) *Pipeline {
			return NewPipeline().Open(plain).Apply(func(_ context.Context, w *Writer) error { return w.InsertPages(0, encrypted) })
		}},
		{"stamp, only the stamp is encrypted", func(_ *testing.T, encrypted, plain *Reader) *Pipeline {
			return NewPipeline().Open(plain).Apply(func(_ context.Context, w *Writer) error {
				return w.StampPDF(PDFStamp{Source: encrypted})
			})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			p := tc.build(t, openColorPDF(t, enc, WithPassword(encryptedPassword)), openColorPDF(t, untaggedHeadingPDF()))
			if !opensWithoutAPassword(t, runPipeline(t, p)) {
				t.Errorf("the output of %s needs a password, though no encryption was asked", tc.name)
			}
		})
	}
}

// TestPipeline_ACompactingRewriteOfAnEncryptedSourceWritesAPlaintextOutput
// pins the same default on a pipeline with no stage, written in full with
// every compaction pass: compaction encrypts nothing on its own.
func TestPipeline_ACompactingRewriteOfAnEncryptedSourceWritesAPlaintextOutput(t *testing.T) {
	t.Parallel()

	r := openColorPDF(t, encryptBytes(t, threePagePDF(), encryptedPassword), WithPassword(encryptedPassword))
	out := runPipeline(t, NewPipeline().Open(r),
		WithFullRewrite(), WithObjectStreams(), WithDedup(), WithImageRecompression(), WithFontSubsetting())
	if !opensWithoutAPassword(t, out) {
		t.Error("the compacted rewrite needs a password, though no encryption was asked")
	}
}
