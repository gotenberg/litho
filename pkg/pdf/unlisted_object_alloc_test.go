package pdf

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// TestEditsKeepObjectsTheTableLeavesOut pins that an object an edit adds never
// takes the number of one the file defines but its cross-reference table leaves
// out, and that a fresh [Reader] and one that extracted text first write the
// same bytes.
//
// The file is [unlistedWidthsPDF]: its font's /Widths is object 6, which the
// table and its /Size leave out. Text extraction misses the index on it and
// runs the raw-byte augmentation, which adds it. An allocator seeded from the
// index as it stood started at 6 on a fresh Reader and at 8 after the
// extraction, so the new object replaced the widths on the first and not on
// the second (CONTRIBUTING "Deterministic output").
func TestEditsKeepObjectsTheTableLeavesOut(t *testing.T) {
	t.Parallel()

	stamp := TextStamp{Text: "Draft", X: 72, Y: 72}
	cases := []struct {
		name string
		edit func(ctx context.Context, w *Writer) error
		opts []WriteOption
	}{
		{
			name: "stamp",
			edit: func(_ context.Context, w *Writer) error { return w.StampText(stamp) },
		},
		{
			name: "stamp/full-rewrite",
			edit: func(_ context.Context, w *Writer) error { return w.StampText(stamp) },
			opts: []WriteOption{WithFullRewrite()},
		},
		{
			name: "attach",
			edit: func(_ context.Context, w *Writer) error {
				w.AddAttachment("note.txt", []byte("a note"), "text/plain")
				return nil
			},
		},
		{
			name: "metadata",
			edit: func(_ context.Context, w *Writer) error {
				w.SetMetadata(Metadata{Title: "Edited"})
				return nil
			},
		},
		{
			name: "sign",
			edit: func(_ context.Context, w *Writer) error {
				w.Sign(&mockSigner{})
				return nil
			},
		},
		{
			name: "redact",
			edit: func(ctx context.Context, w *Writer) error {
				_, err := w.Redact(ctx, []RedactRegion{{Page: 1, Rect: [4]float64{60, 700, 200, 740}}})
				return err
			},
		},
		{
			// The stamp is staged when the remediation starts, and its objects
			// are allocated before the remediation reserves any number.
			name: "stamp-then-remediate",
			edit: func(ctx context.Context, w *Writer) error {
				if err := w.StampText(stamp); err != nil {
					return err
				}
				_, err := w.RemediatePDFA(ctx, models.PDFA2b, WithDocumentID([]byte("0123456789abcdef")))
				return err
			},
		},
	}

	data := unlistedWidthsPDF()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			write := func(t *testing.T, extractFirst bool) []byte {
				t.Helper()
				r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
				if err != nil {
					t.Fatalf("Open: %v", err)
				}
				defer r.Close()
				if extractFirst {
					if _, err := r.ExtractText(ctx, 1); err != nil {
						t.Fatalf("ExtractText: %v", err)
					}
					// Control: the extraction must have added object 6, or
					// both runs start from the same index.
					if _, ok := r.doc.XRefEntry(6); !ok {
						t.Fatal("text extraction did not add object 6 to the index")
					}
				} else if r.doc.XRefAugmented() {
					t.Fatal("opening the file augmented the index")
				}
				w := NewWriter(r)
				defer w.Close()
				if err := tc.edit(ctx, w); err != nil {
					t.Fatalf("edit: %v", err)
				}
				var out bytes.Buffer
				if err := w.WriteTo(ctx, &out, tc.opts...); err != nil {
					t.Fatalf("WriteTo: %v", err)
				}
				return out.Bytes()
			}

			fresh, extracted := write(t, false), write(t, true)
			if !bytes.Equal(fresh, extracted) {
				t.Errorf("the output after ExtractText (%d bytes) differs from a fresh reader's (%d bytes)",
					len(extracted), len(fresh))
			}
			for label, out := range map[string][]byte{"fresh": fresh, "extract-first": extracted} {
				if err := fontWidthsArray(ctx, out); err != nil {
					t.Errorf("%s output: %v", label, err)
				}
			}
		})
	}
}

// fontWidthsArray reports why the page font's /Widths in out is not the
// 40-entry array [unlistedWidthsPDF] gives it. It follows the page tree rather
// than object 6, so it answers for a full rewrite, which renumbers.
func fontWidthsArray(ctx context.Context, out []byte) error {
	r, err := Open(ctx, bytes.NewReader(out), int64(len(out)))
	if err != nil {
		return err
	}
	defer r.Close()
	page, err := r.resolvePage(ctx, 1)
	if err != nil {
		return err
	}
	fonts, err := document.ResolveDict(ctx, r.doc, page.Resources["Font"])
	if err != nil {
		return err
	}
	font, err := document.ResolveDict(ctx, r.doc, fonts["F1"])
	if err != nil {
		return err
	}
	widths, err := document.ResolveArray(ctx, r.doc, font["Widths"])
	if err != nil {
		return err
	}
	if len(widths) != 40 {
		return fmt.Errorf("the font's /Widths holds %d entries, want the file's 40", len(widths))
	}
	if w, ok := widths[0].(ast.PdfInt); !ok || w != 500 {
		return fmt.Errorf("the font's /Widths starts with %v, want the file's 500", widths[0])
	}
	return nil
}
