package pdf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// threeTextPagesPDF builds three pages drawing "first", "second" and "third"
// with a text field on the first, an /Info title and an exact
// cross-reference table.
func threeTextPagesPDF() []byte {
	page := func(word string) string {
		return fmt.Sprintf("BT /F1 12 Tf 100 700 Td (%s) Tj ET\n", word)
	}
	stream := func(content string) string {
		return fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content)
	}
	return buildTestPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [10 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R 5 0 R] /Count 3 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 6 0 R /Resources 9 0 R /Annots [10 0 R] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 7 0 R /Resources 9 0 R >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 8 0 R /Resources 9 0 R >>",
		stream(page("first")),
		stream(page("second")),
		stream(page("third")),
		"<< /Font << /F1 11 0 R >> >>",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /V (John) /Rect [100 600 300 620] /P 3 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		"<< /Title (Three pages) /Author (Jane) >>",
	}, "/Info 12 0 R")
}

// TestWriterSignsAnEditThatRewrites pins that an edit which forces a full
// rewrite is signed rather than refused: [Writer.WriteTo] writes the rewrite,
// then signs it in an incremental update on top, as it signs a remediation
// whose repairs need one.
//
// The signature covers every byte of the file but its own value (ISO 32000-1,
// 12.8.1), and a full rewrite renumbers every object, so the two cannot share a
// pass. [Writer.WriteTo] returned [ErrSignFullRewriteIncompatible] for all five
// edits, and a caller had to write, reopen and sign by hand.
func TestWriterSignsAnEditThatRewrites(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := threeTextPagesPDF()

	for _, tc := range []struct {
		name  string
		edit  func(t *testing.T, w *Writer)
		check func(t *testing.T, got *Reader)
	}{
		{
			name: "delete pages",
			edit: func(t *testing.T, w *Writer) {
				if err := w.DeletePages(2); err != nil {
					t.Fatalf("DeletePages: %v", err)
				}
			},
			check: func(t *testing.T, got *Reader) {
				wantPages(t, got, "first", "third")
			},
		},
		{
			name: "insert pages",
			edit: func(t *testing.T, w *Writer) {
				src, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
				if err != nil {
					t.Fatalf("Open: %v", err)
				}
				t.Cleanup(func() { _ = src.Close() })
				if err := w.InsertPages(1, src, 3); err != nil {
					t.Fatalf("InsertPages: %v", err)
				}
			},
			check: func(t *testing.T, got *Reader) {
				wantPages(t, got, "first", "third", "second", "third")
			},
		},
		{
			name: "flatten form",
			edit: func(_ *testing.T, w *Writer) { w.FlattenForm() },
			check: func(t *testing.T, got *Reader) {
				fields, err := got.FormFields(ctx)
				if err != nil {
					t.Fatalf("FormFields: %v", err)
				}
				for _, f := range fields {
					if f.Name == "name" {
						t.Errorf("the flattened field %q is still a field: %+v", f.Name, fields)
					}
				}
			},
		},
		{
			name: "clear metadata",
			edit: func(_ *testing.T, w *Writer) { w.ClearMetadata() },
			check: func(t *testing.T, got *Reader) {
				meta, err := got.Metadata(ctx)
				if errors.Is(err, ErrNoMetadata) {
					return
				}
				if err != nil {
					t.Fatalf("Metadata: %v", err)
				}
				if meta["Title"] != "" || meta["Author"] != "" {
					t.Errorf("the cleared metadata still reads %v", meta)
				}
			},
		},
		{
			name: "redact",
			edit: func(t *testing.T, w *Writer) {
				report, err := w.Redact(ctx, []RedactRegion{{Page: 2, Rect: [4]float64{90, 690, 300, 730}}})
				if err != nil {
					t.Fatalf("Redact: %v", err)
				}
				if len(report.RemovedText) == 0 {
					t.Fatal("the redaction removed nothing, so the test proves nothing")
				}
			},
			check: func(t *testing.T, got *Reader) {
				wantPages(t, got, "first", "", "third")
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			write := func(signer Signer) ([]byte, error) {
				r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
				if err != nil {
					t.Fatalf("Open: %v", err)
				}
				defer r.Close()
				w := NewWriter(r)
				defer w.Close()
				if signer != nil {
					w.Sign(signer)
				}
				tc.edit(t, w)
				var out bytes.Buffer
				err = w.WriteTo(ctx, &out)
				return out.Bytes(), err
			}

			plain, err := write(nil)
			if err != nil {
				t.Fatalf("unsigned WriteTo: %v", err)
			}
			signer := newCMSTestSigner(t)
			signed, err := write(signer)
			if err != nil {
				t.Fatalf("signed WriteTo: %v", err)
			}

			checkSignedOutput(t, signed, signer)
			if n := signer.calls.Load(); n != 1 {
				t.Errorf("the signer ran %d times, want once", n)
			}
			if !bytes.HasPrefix(signed, plain) {
				t.Error("the signature is not an incremental update on top of the unsigned output")
			}
			got, err := Open(ctx, bytes.NewReader(signed), int64(len(signed)))
			if err != nil {
				t.Fatalf("reopening the signed output: %v", err)
			}
			defer got.Close()
			tc.check(t, got)
		})
	}
}

// wantPages checks that got holds one page per entry of words, each drawing
// that word, or no text for an empty entry.
func wantPages(t *testing.T, got *Reader, words ...string) {
	t.Helper()

	ctx := context.Background()
	n, err := got.PageCount(ctx)
	if err != nil {
		t.Fatalf("PageCount: %v", err)
	}
	if n != len(words) {
		t.Fatalf("the output holds %d pages, want %d", n, len(words))
	}
	for i, word := range words {
		text, err := got.ExtractText(ctx, i+1)
		if err != nil {
			t.Fatalf("ExtractText(%d): %v", i+1, err)
		}
		if strings.TrimSpace(text) != word {
			t.Errorf("page %d reads %q, want %q", i+1, text, word)
		}
	}
}

// TestWriterSignsARequestedFullRewrite pins that [WithFullRewrite] is signed
// rather than refused: [Writer.WriteTo] writes the rewrite the option asks for,
// then signs it in an incremental update on top, as it signs an edit that
// forces one. The signature covers every byte of the file but its own value
// (ISO 32000-1, 12.8.1), so it is written after the rewrite, which renumbers
// every object.
//
// WriteTo returned [ErrSignFullRewriteIncompatible] for the option, and a
// caller had to write, reopen and sign by hand.
func TestWriterSignsARequestedFullRewrite(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	// The /ID a remediation mints is random unless pinned, and the test
	// compares the signed output with the unsigned one byte for byte.
	pinnedID := WithDocumentID([]byte("0123456789abcdef"))
	three := []string{"first", "second", "third"}

	for _, tc := range []struct {
		name  string
		data  func() []byte
		opts  []WriteOption
		edit  func(t *testing.T, w *Writer)
		pages []string
	}{
		{
			name:  "nothing queued",
			data:  threeTextPagesPDF,
			opts:  []WriteOption{WithFullRewrite()},
			edit:  func(*testing.T, *Writer) {},
			pages: three,
		},
		{
			name: "an incremental edit",
			data: threeTextPagesPDF,
			opts: []WriteOption{WithFullRewrite()},
			edit: func(t *testing.T, w *Writer) {
				if err := w.RotatePages(90, 2); err != nil {
					t.Fatalf("RotatePages: %v", err)
				}
			},
			pages: three,
		},
		{
			name:  "packed into object streams",
			data:  threeTextPagesPDF,
			opts:  []WriteOption{WithFullRewrite(), WithObjectStreams(), WithXRefStream()},
			edit:  func(*testing.T, *Writer) {},
			pages: three,
		},
		{
			name: "a remediation",
			data: signablePDF,
			opts: []WriteOption{WithFullRewrite()},
			edit: func(t *testing.T, w *Writer) {
				if _, err := w.RemediatePDFA(ctx, models.PDFA2b, pinnedID); err != nil {
					t.Fatalf("RemediatePDFA: %v", err)
				}
			},
			pages: []string{"page"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			src := tc.data()
			write := func(signer Signer) ([]byte, error) {
				r, err := Open(ctx, bytes.NewReader(src), int64(len(src)))
				if err != nil {
					t.Fatalf("Open: %v", err)
				}
				defer r.Close()
				w := NewWriter(r)
				defer w.Close()
				if signer != nil {
					w.Sign(signer)
				}
				tc.edit(t, w)
				var out bytes.Buffer
				err = w.WriteTo(ctx, &out, tc.opts...)
				return out.Bytes(), err
			}

			plain, err := write(nil)
			if err != nil {
				t.Fatalf("unsigned WriteTo: %v", err)
			}
			if bytes.HasPrefix(plain, src) {
				t.Fatal("the unsigned output is an incremental update of the source, so the test proves nothing")
			}
			signer := newCMSTestSigner(t)
			signed, err := write(signer)
			if err != nil {
				t.Fatalf("signed WriteTo: %v", err)
			}

			checkSignedOutput(t, signed, signer)
			if n := signer.calls.Load(); n != 1 {
				t.Errorf("the signer ran %d times, want once", n)
			}
			if !bytes.HasPrefix(signed, plain) {
				t.Error("the signature is not an incremental update on top of the unsigned rewrite")
			}
			got, err := Open(ctx, bytes.NewReader(signed), int64(len(signed)))
			if err != nil {
				t.Fatalf("reopening the signed output: %v", err)
			}
			defer got.Close()
			wantPages(t, got, tc.pages...)
		})
	}
}

// TestPipelineSignsARequestedFullRewrite pins the same through a [Pipeline],
// whose final write takes the option.
func TestPipelineSignsARequestedFullRewrite(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := threeTextPagesPDF()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	signer := newCMSTestSigner(t)
	var out bytes.Buffer
	_, err = NewPipeline().Open(r).Apply(func(_ context.Context, w *Writer) error {
		w.Sign(signer)
		return nil
	}).WriteTo(ctx, &out, WithFullRewrite())
	if err != nil {
		t.Fatalf("Pipeline.WriteTo: %v", err)
	}
	checkSignedOutput(t, out.Bytes(), signer)
	if bytes.HasPrefix(out.Bytes(), data) {
		t.Error("the signed output is an incremental update of the source, not of its rewrite")
	}
}
