package pdf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// malformedRenderDPI is the resolution the RenderPage entry point draws at: a
// letter page 9 pixels per inch draws 76 by 99 pixels.
const malformedRenderDPI = 9

// malformedEntryPoints are the public operations a caller runs on a document,
// each returning the error it fails with. The document is the Reader r reads.
var malformedEntryPoints = []struct {
	name string
	run  func(ctx context.Context, r *Reader) error
}{
	{"PageCount", func(ctx context.Context, r *Reader) error { _, err := r.PageCount(ctx); return err }},
	{"Metadata", func(ctx context.Context, r *Reader) error { _, err := r.Metadata(ctx); return err }},
	{"Bookmarks", func(ctx context.Context, r *Reader) error { _, err := r.Bookmarks(ctx); return err }},
	{"EmbeddedFiles", func(ctx context.Context, r *Reader) error { _, err := r.EmbeddedFiles(ctx); return err }},
	{"FormFields", func(ctx context.Context, r *Reader) error { _, err := r.FormFields(ctx); return err }},
	{"HasFormFields", func(ctx context.Context, r *Reader) error { _, err := r.HasFormFields(ctx); return err }},
	{"IsTagged", func(ctx context.Context, r *Reader) error { _, err := r.IsTagged(ctx); return err }},
	{"ExtractSignatures", func(ctx context.Context, r *Reader) error { _, err := r.ExtractSignatures(ctx); return err }},
	{"ValidatePDFAReport", func(ctx context.Context, r *Reader) error {
		_, err := r.ValidatePDFAReport(ctx, models.PDFA2b)
		return err
	}},
	{"ValidatePDFUAReport", func(ctx context.Context, r *Reader) error {
		_, err := r.ValidatePDFUAReport(ctx, models.PDFUA1)
		return err
	}},
	{"ValidateFacturXReport", func(ctx context.Context, r *Reader) error { _, err := r.ValidateFacturXReport(ctx); return err }},
	{"DeclaredPDFAPart", func(ctx context.Context, r *Reader) error { _, err := r.DeclaredPDFAPart(ctx); return err }},
	{"ExtractText", func(ctx context.Context, r *Reader) error { _, err := r.ExtractText(ctx, 1); return err }},
	// The resolution decides how many pixels the page fills, not which
	// objects the drawing reads, so a small raster reaches every error the
	// default one does.
	{"RenderPage", func(ctx context.Context, r *Reader) error {
		_, err := r.RenderPage(ctx, 1, WithRenderDPI(malformedRenderDPI))
		return err
	}},
	{"Writer.RotatePages", func(ctx context.Context, r *Reader) error {
		w := NewWriter(r)
		defer w.Close()
		if err := w.RotatePages(90, 1); err != nil {
			return err
		}
		return w.WriteTo(ctx, io.Discard)
	}},
	{"Writer.RemoveEncryption", func(ctx context.Context, r *Reader) error {
		w := NewWriter(r)
		defer w.Close()
		w.RemoveEncryption()
		return w.WriteTo(ctx, io.Discard)
	}},
	{"Writer.WriteTo", func(ctx context.Context, r *Reader) error {
		w := NewWriter(r)
		defer w.Close()
		return w.WriteTo(ctx, io.Discard)
	}},
	{"Split", func(ctx context.Context, r *Reader) error { return Split(ctx, r, []int{1}, io.Discard) }},
	{"Merge", func(ctx context.Context, r *Reader) error { return Merge(ctx, []*Reader{r, r}, io.Discard) }},
	{"Merge of one", func(ctx context.Context, r *Reader) error { return Merge(ctx, []*Reader{r}, io.Discard) }},
	{"Pipeline", func(ctx context.Context, r *Reader) error {
		_, err := NewPipeline().Open(r).Apply(func(_ context.Context, w *Writer) error {
			w.SetMetadata(Metadata{Title: "t"})
			return nil
		}).WriteTo(ctx, io.Discard)
		return err
	}},
}

// TestMalformedDocumentIsInvalidInput pins that a document whose structure
// litho cannot read, a missing catalog or page tree, fails every public
// operation with [ErrMalformedPDF], an error [IsInvalidInput] classifies,
// whose message says the document is not a valid PDF. Opening refuses a
// document with no catalog, so reading its metadata fails rather than answers
// empty. A document with a catalog but no page tree opens, and every operation
// that needs the pages fails.
func TestMalformedDocumentIsInvalidInput(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		data     []byte
		openFail bool
	}{
		// litho recovers a catalog or a page tree root the trailer or the
		// catalog fails to name by scanning the file for one, so these
		// documents hold none to recover.
		{"no catalog", malformedPDF("<< /Size 5 /Info 4 0 R >>", "<< /Kind /NotACatalog >>", "<< /Kids [3 0 R] /Count 1 >>"), true},
		{"root not a dictionary", malformedPDF("<< /Size 5 /Root 2 0 R >>", "<< /Kind /NotACatalog >>", "42"), true},
		{"catalog without /Pages", malformedPDF("<< /Size 5 /Root 1 0 R >>", "<< /Type /Catalog >>", "<< /Kids [3 0 R] /Count 1 >>"), false},
		{"catalog /Pages not a dictionary", malformedPDF("<< /Size 5 /Root 1 0 R >>", "<< /Type /Catalog /Pages 2 0 R >>", "42"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			r, err := Open(ctx, bytes.NewReader(tc.data), int64(len(tc.data)))
			if tc.openFail {
				assertMalformed(t, "Open", err)
				return
			}
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer r.Close()
			for _, op := range []string{"PageCount", "Writer.WriteTo", "Writer.RemoveEncryption", "Split", "Merge", "Merge of one", "Pipeline"} {
				assertMalformed(t, op, runEntryPoint(ctx, t, r, op))
			}
		})
	}
}

// TestTruncatedDocumentIsInvalidInput pins that every public operation on a
// document cut short, at any byte, either succeeds on what litho recovers or
// fails with an error [IsInvalidInput] classifies: never an unclassified error
// a caller can only report as its own failure. Cutting the trailer and the
// catalog used to answer "trailer missing /Root" unclassified, and metadata
// an empty map.
//
// Every cut runs every entry point: a truncation that changes what litho
// recovers can sit at any byte, so sampling the cuts would leave the next
// such byte unpinned. The cost of the walk lies in RenderPage filling a
// default-resolution raster on each cut, so it draws at [malformedRenderDPI]
// instead.
func TestTruncatedDocumentIsInvalidInput(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	full := threePagePDF()
	for n := len("%PDF-1.4\n"); n < len(full); n++ {
		data := full[:n]
		r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
		if err != nil {
			if !IsInvalidInput(err) {
				t.Errorf("Open of the first %d bytes: %v, not classified as invalid input", n, err)
			}
			continue
		}
		for _, ep := range malformedEntryPoints {
			// A document without an Info dictionary or XMP has no metadata
			// to read, which is no failure of the document.
			if err := ep.run(ctx, r); err != nil && !IsInvalidInput(err) && !errors.Is(err, ErrNoMetadata) {
				t.Errorf("%s on the first %d bytes: %v, not classified as invalid input", ep.name, n, err)
			}
		}
		r.Close()
	}
}

// malformedPDF builds a PDF whose trailer is trailer, object 1 is obj1, object
// 2 is obj2, object 3 is a page whose parent is object 2, and object 4 an
// Info dictionary, with a sound cross-reference table.
func malformedPDF(trailer, obj1, obj2 string) []byte {
	objs := []string{obj1, obj2, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>", "<< /Title (Malformed) >>"}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	offs := make([]int, len(objs))
	for i, o := range objs {
		offs[i] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for _, off := range offs {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", off)
	}
	fmt.Fprintf(&buf, "trailer\n%s\nstartxref\n%d\n%%%%EOF\n", trailer, xref)
	return buf.Bytes()
}

// runEntryPoint runs the entry point named name on r.
func runEntryPoint(ctx context.Context, t *testing.T, r *Reader, name string) error {
	t.Helper()
	for _, ep := range malformedEntryPoints {
		if ep.name == name {
			return ep.run(ctx, r)
		}
	}
	t.Fatalf("no entry point %q", name)
	return nil
}

// assertMalformed fails t unless err is the [ErrMalformedPDF] an operation op
// returns for a malformed document.
func assertMalformed(t *testing.T, op string, err error) {
	t.Helper()
	if !errors.Is(err, ErrMalformedPDF) {
		t.Errorf("%s: %v, want ErrMalformedPDF", op, err)
		return
	}
	if !IsInvalidInput(err) {
		t.Errorf("%s: IsInvalidInput(%v) = false", op, err)
	}
	if !strings.Contains(err.Error(), "not a valid PDF document: ") {
		t.Errorf("%s: %q does not say the document is not a valid PDF", op, err)
	}
}
