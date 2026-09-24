package compliance

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestTrailerID_SynthesizedTrailerIgnored pins that a trailer litho made up is
// not reported against the file-identifier rule.
//
// A file whose trailer cannot be recovered gets one built from whatever the raw
// scan found, so its entries are litho's rather than the file's and it answers
// nothing about the identifier the file carries.
// [TestCheckNumericLimits_SynthesizedTrailerIgnored] declines to judge the same
// trailer for ISO 19005-2 §6.1.13, and this is the same reasoning for §6.1.3.
//
// The well-formed case below is the guard that matters: a file whose trailer is
// genuinely missing /ID must still fire, so the rule is narrowed rather than
// silenced.
//
// # Corpus evidence
//
//   - pdfjs, 1201 files: 4 synthesize a trailer, and all four were reported
//     here.
//   - verapdf, 2601 files: 0 synthesize a trailer.
//   - pdfjs issue11330.pdf, issue14269.pdf, issue15833.pdf: each carries an
//     /ID inside a /Type /XRef stream dictionary the rebuild could not
//     recover, and veraPDF passes issue11330.pdf with a real verdict.
func TestTrailerID_SynthesizedTrailerIgnored(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("a synthesized trailer is not judged", func(t *testing.T) {
		t.Parallel()
		// The same unrecoverable-trailer fixture the §6.1.13 guard uses.
		data := []byte("%PDF-1.7\n1 0 obj\n<< /Type /Catalog >>\nendobj\n2147483647 0 obj\n<< >>\nendobj\n")
		doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
		if err != nil {
			t.Fatalf("NewDocument: %v", err)
		}
		if !doc.TrailerSynthesized {
			t.Skip("this fixture no longer exercises the synthesized-trailer path")
		}
		if got := checkTrailerID(ctx, doc, "PDF/A-2b"); hasRule(got, trailerIDRule("PDF/A-2b")) {
			t.Errorf("a synthesized trailer must not be reported against the identifier rule, got %v", got)
		}
	})

	t.Run("a recovered trailer with no /ID still fires", func(t *testing.T) {
		t.Parallel()
		data := trailerPDF("")
		doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
		if err != nil {
			t.Fatalf("NewDocument: %v", err)
		}
		if doc.TrailerSynthesized {
			t.Fatal("the well-formed fixture must not take the synthesized path")
		}
		if got := checkTrailerID(ctx, doc, "PDF/A-2b"); !hasRule(got, trailerIDRule("PDF/A-2b")) {
			t.Errorf("a real trailer carrying no /ID must fire the identifier rule, got %v", got)
		}
	})
}
