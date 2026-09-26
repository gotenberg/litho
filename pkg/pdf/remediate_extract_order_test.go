package pdf

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/models"
)

// TestRemediationIgnoresEarlierExtraction pins that remediating a fresh
// [Reader] and one that extracted text first writes the same bytes, on a file
// whose table leaves out objects the extraction reaches ([unlistedWidthsPDF]).
//
// The extraction runs the raw-byte augmentation, which adds those objects to
// the index. What remediation reads from the index must not depend on whether
// something ran it before (CONTRIBUTING "Deterministic output").
func TestRemediationIgnoresEarlierExtraction(t *testing.T) {
	t.Parallel()

	data := unlistedWidthsPDF()
	ctx := context.Background()
	for _, profile := range []models.PDFAProfile{models.PDFA1b, models.PDFA2b} {
		for _, aggressive := range []bool{false, true} {
			remediate := func(t *testing.T, extractFirst bool) []byte {
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
					// Control: the extraction must have added the unlisted
					// objects, or both runs start from the same index.
					if _, ok := r.doc.XRefEntry(7); !ok {
						t.Fatal("text extraction did not add object 7 to the index")
					}
				} else if r.doc.XRefAugmented() {
					t.Fatal("opening the file augmented the index")
				}
				opts := []RemediateOption{WithDocumentID([]byte("0123456789abcdef"))}
				if aggressive {
					opts = append(opts, WithAggressiveFixes())
				}
				w := NewWriter(r)
				if _, err := w.RemediatePDFA(ctx, profile, opts...); err != nil {
					t.Fatalf("RemediatePDFA: %v", err)
				}
				var out bytes.Buffer
				if err := w.WriteTo(ctx, &out); err != nil {
					t.Fatalf("WriteTo: %v", err)
				}
				return out.Bytes()
			}
			t.Run(string(profile)+map[bool]string{false: "", true: "/aggressive"}[aggressive], func(t *testing.T) {
				fresh, extracted := remediate(t, false), remediate(t, true)
				if !bytes.Equal(fresh, extracted) {
					t.Errorf("the output after ExtractText (%d bytes) differs from a fresh reader's (%d bytes)",
						len(extracted), len(fresh))
				}
				// The font's /Widths is object 6, which the table leaves out.
				// An object the remediation added at that number replaced it.
				out, err := Open(ctx, bytes.NewReader(fresh), int64(len(fresh)))
				if err != nil {
					t.Fatalf("reopening the output: %v", err)
				}
				defer out.Close()
				if widths, err := out.doc.Resolve(ctx, ast.PdfIndirectRef{ObjectNum: 6}); err != nil {
					t.Errorf("resolving the font's /Widths in the output: %v", err)
				} else if _, ok := widths.(ast.PdfArray); !ok {
					t.Errorf("the font's /Widths is a %T in the output, want the file's array", widths)
				}
			})
		}
	}
}
