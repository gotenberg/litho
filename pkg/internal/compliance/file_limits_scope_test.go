package compliance

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestCheckNumericLimits_TrailerDictionary pins that the trailer's own values
// are held to the implementation limits.
//
// ISO 19005-2 §6.1.13 t1 forbids any integer outside the 32-bit range that "a
// conforming file" contains, wherever it is written, and the trailer dictionary
// is not an indirect object, so the object walk never reached it. veraPDF
// reports such a value against root/trailer[0].
//
// # Corpus evidence
//
//   - pdfjs issue5599.pdf: writes /ID 77777777777777777 in its trailer, and
//     veraPDF reports it against root/trailer[0] while litho saw nothing.
func TestCheckNumericLimits_TrailerDictionary(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// fires reports whether the integer-range rule fails for a file whose
	// trailer carries the given extra entry.
	fires := func(t *testing.T, trailerExtra string) bool {
		t.Helper()
		data := trailerPDF(trailerExtra)
		doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
		if err != nil {
			t.Fatalf("NewDocument: %v", err)
		}
		return hasRule(checkNumericLimits(ctx, doc, "PDF/A-2b", nil), Rule_19005_2_6_1_13_t01)
	}

	cases := []struct {
		name  string
		extra string
		want  bool
	}{
		{"an out-of-range integer in the trailer fires", "/ID 77777777777777777", true},
		{"an in-range integer does not", "/ID 7777", false},
		{"no extra entry does not", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := fires(t, tc.extra); got != tc.want {
				t.Errorf("integer-range violation = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestCheckNumericLimits_SynthesizedTrailerIgnored pins that a trailer litho
// made up is not reported on. A file with no recoverable trailer gets one from
// [document.Document] built out of whatever the raw scan found, so its entries
// are litho's numbers rather than the file's. Such a trailer can state a value
// out of the ISO 19005-2 §6.1.13 t1 range that appears nowhere in the file.
//
// # Corpus evidence
//
//   - pdfjs bug1980958.pdf: is 219 damaged bytes with no trailer keyword, no
//     startxref and no /Size, and the synthesized /Size 2147483648 (a bogus
//     object number the scan picked up) is out of the ISO 19005-2 §6.1.13 t1
//     range while appearing nowhere in the file.
func TestCheckNumericLimits_SynthesizedTrailerIgnored(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// A file whose xref and trailer are both unrecoverable.
	data := []byte("%PDF-1.7\n1 0 obj\n<< /Type /Catalog >>\nendobj\n2147483647 0 obj\n<< >>\nendobj\n")
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	if !doc.TrailerSynthesized {
		t.Skip("this fixture no longer exercises the synthesized-trailer path")
	}
	if hasRule(checkNumericLimits(ctx, doc, "PDF/A-2b", nil), Rule_19005_2_6_1_13_t01) {
		t.Error("a synthesized trailer must not be reported against the integer-range rule")
	}
}

// trailerPDF builds a minimal one-page PDF carrying extra in its trailer.
func trailerPDF(extra string) []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
	}
	out := "%PDF-1.7\n"
	offs := make([]int, len(objs))
	for i, o := range objs {
		offs[i] = len(out)
		out += fmt.Sprintf("%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := len(out)
	out += fmt.Sprintf("xref\n0 %d\n0000000000 65535 f \r\n", len(objs)+1)
	for _, o := range offs {
		out += fmt.Sprintf("%010d 00000 n \r\n", o)
	}
	out += fmt.Sprintf("trailer\n<< /Size %d /Root 1 0 R %s>>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, extra, xref)
	return []byte(out)
}
