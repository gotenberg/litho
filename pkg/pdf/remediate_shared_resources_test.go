package pdf

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// sharedFormResourcesPDF builds a page painting in DeviceRGB that draws two
// identical forms, /X1 and /X2, whose /Resources is one indirect dictionary.
// A PDF/A-1b remediation adds an sRGB output intent and injects /DefaultCMYK
// into every resource dictionary the page reaches (ISO 19005-1, 6.2.3.3),
// the shared one included.
func sharedFormResourcesPDF() []byte {
	const content = "1 0 0 rg 10 10 50 50 re f\nq /X1 Do Q q /X2 Do Q\n"
	const form = "0 0 1 rg 0 0 10 10 re f\n"
	formObj := fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 100 100] /Resources 7 0 R /Length %d >>\nstream\n%s\nendstream",
		len(form), form)
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R " +
			"/Resources << /XObject << /X1 5 0 R /X2 6 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		formObj,
		formObj,
		"<< /ProcSet [/PDF] >>",
	})
}

// TestRemediation_SharedFormResourcesWritesTheSameBytes pins that remediating
// the same input with the /ID seam pinned writes the same bytes every run, on
// a page whose two forms share one indirect /Resources ([sharedFormResourcesPDF])
// (CONTRIBUTING "Deterministic output").
//
// The walk that injects the default colour space patches the shared dictionary
// once, at its own object number, and rewrites the form it reached it through.
// It walked the page's /XObject dictionary in Go's map order, so which of the
// two forms it rewrote changed from run to run. Each run draws a fresh map
// order, so 24 runs agreeing by chance on the walk that depended on it happen
// less than once in a thousand.
func TestRemediation_SharedFormResourcesWritesTheSameBytes(t *testing.T) {
	t.Parallel()

	data := sharedFormResourcesPDF()
	ctx := context.Background()
	remediate := func() []byte {
		r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		defer r.Close()
		w := NewWriter(r)
		defer w.Close()
		rep, err := w.RemediatePDFA(ctx, models.PDFA1b, WithDocumentID([]byte("0123456789abcdef")))
		if err != nil {
			t.Fatalf("RemediatePDFA: %v", err)
		}
		// Control: the default colour space must have been injected, or the
		// walk this test pins never ran.
		injected := false
		for _, fix := range rep.Fixed {
			if bytes.Contains([]byte(fix), []byte("DefaultCMYK")) {
				injected = true
			}
		}
		if !injected {
			t.Fatalf("remediation injected no /DefaultCMYK, fixes %q", rep.Fixed)
		}
		var out bytes.Buffer
		if err := w.WriteTo(ctx, &out); err != nil {
			t.Fatalf("WriteTo: %v", err)
		}
		return out.Bytes()
	}

	first := remediate()
	const runs = 24
	for run := 1; run < runs; run++ {
		if got := remediate(); !bytes.Equal(got, first) {
			t.Fatalf("run %d wrote bytes that differ from run 0's: the output depends on map order", run)
		}
	}
}
