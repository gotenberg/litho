package compliance

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// unlistedObjectPDF assembles a document whose body carries object 4, a
// stream with the forbidden /F key that the cross-reference table leaves out,
// and the objects unlisted, which the table leaves out too. catalogExtra and
// pageExtra are appended to the catalog and page dictionaries. With /Metadata
// 9 0 R, an object the file does not hold, resolving the dangling reference
// runs the raw-byte xref augmentation, and the augmentation adds object 4 to
// the index.
func unlistedObjectPDF(catalogExtra, pageExtra, unlisted string) []byte {
	listed := []string{
		"<< /Type /Catalog /Pages 2 0 R" + catalogExtra + " >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792]" + pageExtra + " >>",
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n%\xe2\xe3\xcf\xd3\n")
	offsets := make([]int, len(listed)+1)
	for i, body := range listed {
		offsets[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}
	buf.WriteString("4 0 obj\n<< /Length 0 /F (external.dat) >>\nstream\n\nendstream\nendobj\n")
	buf.WriteString(unlisted)
	start := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \r\n", len(listed)+1)
	for i := 1; i <= len(listed); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \r\n", offsets[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R /ID [<00112233445566778899AABBCCDDEEFF> <00112233445566778899AABBCCDDEEFF>] >>\nstartxref\n%d\n%%%%EOF\n",
		len(listed)+1, start)
	return buf.Bytes()
}

// TestXRefAugmentationLeavesTheSweepsAlone pins that a validation run judges
// the same objects whether the validation itself or an earlier caller runs the
// raw-byte augmentation, and that no check judges an object the file's table
// leaves out.
//
// The augmentation that repairs a damaged index runs lazily, the first time a
// lookup misses. With the dangling /Metadata, the XMP group's lookup runs it
// while the other groups are fanned out, and a group that swept the index
// before that moment missed object 4 while a group that swept it after saw it:
// the same bytes reported object 4's /F key (ISO 19005-2, 6.1.7.1) on 9 runs of
// 200 and not on the others (CONTRIBUTING "Deterministic output"). Running the
// groups again once the index changed made that every run, but without the
// dangling reference the validation misses no lookup, and object 4 was
// reported only when an earlier caller had run the augmentation. The sweeps now
// judge the objects the index held when the document was opened, as veraPDF,
// which reads the file's own table, does.
//
// The page's /Resources in the last case is object 5, which the table leaves
// out and which holds a hex string with an odd digit count (ISO 19005-2, 6.1.6
// t1). A validation run resolves it while it builds its context, in
// compliance mode, and records the finding. A caller that resolved it first
// cached it with nothing recorded, and the run then reported nothing.
func TestXRefAugmentationLeavesTheSweepsAlone(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name                           string
		catalogExtra, pageExtra, extra string
		before                         func(context.Context, *document.Document) error
		forbidden                      string
	}{
		{
			name:         "validation misses a lookup",
			catalogExtra: " /Metadata 9 0 R",
			before: func(ctx context.Context, doc *document.Document) error {
				doc.AugmentXRef(ctx)
				return nil
			},
			forbidden: "object 4",
		},
		{
			name: "validation misses none",
			before: func(ctx context.Context, doc *document.Document) error {
				doc.AugmentXRef(ctx)
				return nil
			},
			forbidden: "object 4",
		},
		{
			name:      "an earlier caller resolved an unlisted object",
			pageExtra: " /Resources 5 0 R",
			extra:     "5 0 obj\n<< /ProcSet [/PDF] /Probe <ABC> >>\nendobj\n",
			before: func(ctx context.Context, doc *document.Document) error {
				_, err := document.ResolveObject(ctx, doc, ast.PdfIndirectRef{ObjectNum: 5})
				return err
			},
			forbidden: "hex string has odd number of digits",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			data := unlistedObjectPDF(tc.catalogExtra, tc.pageExtra, tc.extra)
			validate := func(t *testing.T, first bool) []string {
				t.Helper()
				doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
				if err != nil {
					t.Fatalf("NewDocument: %v", err)
				}
				if first {
					if err := tc.before(ctx, doc); err != nil {
						t.Fatalf("earlier call: %v", err)
					}
					// Control: the earlier call must add object 4, or the
					// runs below compare nothing.
					if _, ok := doc.XRefEntry(4); !ok {
						t.Fatal("the earlier call did not add object 4 to the index")
					}
				}
				errs, err := ValidatePDFA(ctx, doc, models.PDFA2b)
				if err != nil {
					t.Fatalf("ValidatePDFA: %v", err)
				}
				out := make([]string, len(errs))
				for i, e := range errs {
					out[i] = e.Error()
				}
				return out
			}

			want := validate(t, true)
			if i := slices.IndexFunc(want, func(s string) bool { return strings.Contains(s, tc.forbidden) }); i >= 0 {
				t.Fatalf("a run after the earlier call judges an object the table leaves out: %q", want[i])
			}
			for run := range 32 {
				if got := validate(t, false); !slices.Equal(got, want) {
					t.Fatalf("run %d: a fresh document reports\n%q\nand one an earlier call ran on reports\n%q", run, got, want)
				}
			}
		})
	}
}
