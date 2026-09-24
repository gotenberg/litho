package compliance

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// TestFormXObjectInheritsResources pins that a Form XObject omitting
// /Resources still reaches the XObjects its content stream names.
//
// ISO 32000-1, 8.10.1 makes /Resources optional: a form that omits it draws on
// the resource dictionary of the content stream that invoked it. litho returned
// early when the entry was absent, so every nested XObject, shading and pattern
// of such a form went unscanned and its device colour went unreported.
//
// # Corpus evidence
//
//   - pdfjs issue1055r.pdf: draws a form with no /Resources whose stream
//     invokes a second form by a name only the page declares, and that one
//     fills with an "rg" operator; veraPDF reports ISO 19005-2 §6.2.4.3 t2 at
//     .../xObject[0]/contentStream[0]/operators[3]/fillCS[0].
func TestFormXObjectInheritsResources(t *testing.T) {
	t.Parallel()

	// pdf builds a page with no /OutputIntents that draws /Outer. Outer carries
	// its own /Resources only when ownRes is set; either way it invokes /Inner,
	// which fills with DeviceRGB.
	pdf := func(ownRes bool) []byte {
		const inner = "q 1 0 0 rg 0 0 20 20 re f Q"
		const outer = "q /Inner Do Q"
		outerRes := ""
		if ownRes {
			outerRes = "/Resources << /XObject << /Inner 5 0 R >> >> "
		}
		return cyclePDF(
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Contents 6 0 R "+
				"/Resources << /XObject << /Outer 4 0 R /Inner 5 0 R >> >> >>",
			"<< /Type /XObject /Subtype /Form /BBox [0 0 20 20] "+outerRes+
				"/Length "+itoa(len(outer))+" >>\nstream\n"+outer+"\nendstream",
			"<< /Type /XObject /Subtype /Form /BBox [0 0 20 20] "+
				"/Length "+itoa(len(inner))+" >>\nstream\n"+inner+"\nendstream",
			"<< /Length 15 >>\nstream\nq /Outer Do Q\nendstream",
		)
	}

	fires := func(t *testing.T, data []byte) bool {
		t.Helper()
		doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
		if err != nil {
			t.Fatalf("NewDocument: %v", err)
		}
		viols, err := ValidatePDFA(context.Background(), doc, models.PDFA2b)
		if err != nil {
			t.Fatalf("ValidatePDFA: %v", err)
		}
		for _, e := range viols {
			v, ok := e.(Violation)
			if ok && v.Rule.Clause == "6.2.4.3" && v.Rule.TestNumber == 2 {
				return true
			}
		}
		return false
	}

	t.Run("a form with no Resources reaches the page's", func(t *testing.T) {
		t.Parallel()
		if !fires(t, pdf(false)) {
			t.Error("the nested form fills with rg and no OutputIntent covers it")
		}
	})

	// The path that already worked stays working: a form stating its own
	// /Resources resolves the nested name there.
	t.Run("a form with its own Resources still reaches them", func(t *testing.T) {
		t.Parallel()
		if !fires(t, pdf(true)) {
			t.Error("the nested form fills with rg and no OutputIntent covers it")
		}
	})
}
