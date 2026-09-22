package pdf

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// The version a document declares can land in either of two places, and which
// one depends on the write path: a full rewrite re-emits the header, while an
// incremental update cannot touch it and records the version in the catalog
// /Version instead (ISO 32000-1, 7.5.2).
//
// Asserting on the leading bytes therefore tests the path, not the contract.
// [Reader.Version] resolves both, so these reopen the output and ask what the
// document says it is, which is also what a caller sees.

// reportedVersion writes the queued work and returns the version the output
// declares once reopened.
func reportedVersion(t *testing.T, out []byte) string {
	t.Helper()
	r, err := Open(context.Background(), bytes.NewReader(out), int64(len(out)))
	if err != nil {
		t.Fatalf("reopening the output: %v", err)
	}
	defer r.Close()
	return r.Version()
}

// There is deliberately no standalone test here for [Reader.Version] on a
// document with no catalog /Version: TestVersion in reader_test.go already
// pins that, and a second copy under a name promising more would assert less
// than it claims. The override case, where the catalog disagrees with the
// header, is exercised below, since that is the reading the accessor's doc
// comment once got wrong.

// TestAddAssociatedFile_RaisesTheReportedVersion covers the public entry point
// for associated files. /AF and /AFRelationship are ISO 32000-2 keys, and this
// path queues a mutation rather than forcing a rewrite, so the raise has to
// reach the catalog for the document to declare what it carries.
func TestAddAssociatedFile_RaisesTheReportedVersion(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := threePagePDF()

	if !bytes.HasPrefix(data, []byte("%PDF-1.4")) {
		t.Fatalf("fixture declares %q, want %%PDF-1.4; a raise to 1.7 would not be observable",
			pdfHeader(data))
	}

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	w := NewWriter(r)
	w.AddAssociatedFile(AssociatedFile{
		Name:         "invoice.xml",
		Data:         []byte("<x/>"),
		MimeType:     "application/xml",
		Relationship: "Data",
	})

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	// Precondition: the associated-file keys are actually present, or the
	// version assertion would hold on an output that never took this path.
	if !bytes.Contains(out.Bytes(), []byte("/AFRelationship")) {
		t.Fatal("output carries no /AFRelationship, so this proves nothing")
	}

	// The header MUST still read 1.4: this path queues a mutation rather than
	// forcing a rewrite, and an incremental update cannot re-emit byte zero.
	// Asserting both halves is what proves the catalog /Version is carrying
	// the declaration, which is the contract [Reader.Version] documents.
	if !bytes.HasPrefix(out.Bytes(), []byte("%PDF-1.4")) {
		t.Errorf("header is %q, want the source's %%PDF-1.4 unchanged: an incremental "+
			"update cannot rewrite it, so a moved header means a different path ran",
			pdfHeader(out.Bytes()))
	}
	if got := reportedVersion(t, out.Bytes()); got != "1.7" {
		t.Errorf("document reports version %q, want 1.7: it carries ISO 32000-2 associated-file keys", got)
	}
}

// TestAddAttachment_LeavesTheReportedVersion is the control. A plain
// attachment lives in the /Names /EmbeddedFiles tree (ISO 32000-1, 7.11.4), a
// PDF 1.4 construct that declares nothing new.
func TestAddAttachment_LeavesTheReportedVersion(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := threePagePDF()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	w := NewWriter(r)
	w.AddAttachment("note.txt", []byte("hello"), "text/plain")

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	if bytes.Contains(out.Bytes(), []byte("/AFRelationship")) {
		t.Fatal("the plain attachment wrote associated-file keys; it no longer controls for anything")
	}
	if got := reportedVersion(t, out.Bytes()); got != "1.4" {
		t.Errorf("a plain attachment moved the reported version to %q, want 1.4", got)
	}
}

// TestRemediate_RaisesTheReportedVersionForISO14289_2 covers the two profiles
// built on ISO 14289-2, which is itself built on PDF 2.0. Whether the raise
// lands in the header or the catalog depends on whether remediation forces a
// rewrite, and [Reader.Version] resolves either.
func TestRemediate_RaisesTheReportedVersionForISO14289_2(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		run  func(context.Context, *Writer) (*RemediationReport, error)
	}{
		{"PDF/UA-2", func(ctx context.Context, w *Writer) (*RemediationReport, error) {
			return w.RemediatePDFUA(ctx, models.PDFUA2)
		}},
		{"WTPDF-1.0-Reuse", func(ctx context.Context, w *Writer) (*RemediationReport, error) {
			return w.RemediateWTPDF(ctx, models.WTPDF1Reuse)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			data := threePagePDF()

			// Without this a fixture that already declared 2.0 would satisfy
			// the assertion below without the raise ever running.
			if !bytes.HasPrefix(data, []byte("%PDF-1.4")) {
				t.Fatalf("fixture declares %q, want %%PDF-1.4; a raise to 2.0 would not be observable",
					pdfHeader(data))
			}

			r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer r.Close()

			w := NewWriter(r)
			report, err := tc.run(ctx, w)
			if err != nil {
				t.Fatalf("remediate: %v", err)
			}
			// Remediation returns an empty report for a document with no
			// violations, and then nothing at all was exercised.
			if len(report.Fixed) == 0 && len(report.Residual) == 0 {
				t.Fatal("the fixture produced no violations, so remediation did nothing")
			}

			var out bytes.Buffer
			if err := w.WriteTo(ctx, &out); err != nil {
				t.Fatalf("WriteTo: %v", err)
			}
			if got := reportedVersion(t, out.Bytes()); got != "2.0" {
				t.Errorf("document reports version %q, want 2.0: ISO 14289-2 builds on PDF 2.0", got)
			}
		})
	}
}
