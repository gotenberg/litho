package compliance

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// TestTransparencyParts2Plus_ExtGStateScopedToDrawn pins that a graphics state
// the content never selects contributes no transparency.
//
// ISO 19005-2, 6.2.10 t2 and ISO 19005-4, 6.2.9 t2 ask whether the PAGE
// contains transparency, and veraPDF answers it from the content stream: it
// builds a PDExtGState from the gs operator that selects one, so a state the
// page merely declares is modelled nowhere. [checkExtGStateTransparency1] and
// [resourcesUseTransparency] both scoped themselves that way, but the
// ExtGState loop inside [checkTransparencyParts2Plus] did not, and it runs
// first and short-circuits the others. The alpha arms matter as much as the
// blend-mode ones: /CA on a state nothing selects trips the same loop.
//
// The selected arms are what keep this honest. A gate that simply silenced the
// loop would satisfy the unselected cases and quietly lose every real report.
//
// # Corpus evidence
//
//   - pdfjs issue6741.pdf: the witness. It declares /G1 (/BM /Darken) and /G2
//     (/BM /Lighten), sets only /G0 (/BM /Normal), paints one CCITTFax image
//     whose /SMask is None, and carries no page /Group and no OutputIntent.
//     veraPDF passes it; litho reported §6.2.10 t2. The opaque-state case is
//     its control.
//   - pdfjs issue13915, issue14864: tripped the same loop through /CA on
//     states nothing selects, and litho reported 54 and 8 where veraPDF
//     reports 8 and 6.
func TestTransparencyParts2Plus_ExtGStateScopedToDrawn(t *testing.T) {
	t.Parallel()

	// pdf builds a one-page document whose /Resources declare a single
	// graphics state, painting a rectangle after selecting it or not. The page
	// has no /Group and the document no /OutputIntents, so the blending colour
	// space rule turns on the transparency question alone.
	pdf := func(gsBody, content string) []byte {
		return cyclePDF(
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] "+
				"/Resources << /ExtGState << /G1 5 0 R >> >> /Contents 4 0 R >>",
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
			gsBody,
		)
	}

	fires := func(t *testing.T, data []byte, profile models.PDFAProfile, clause string) bool {
		t.Helper()
		doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
		if err != nil {
			t.Fatalf("NewDocument: %v", err)
		}
		viols, err := ValidatePDFA(context.Background(), doc, profile)
		if err != nil {
			t.Fatalf("ValidatePDFA: %v", err)
		}
		for _, e := range viols {
			if v, ok := e.(Violation); ok && v.Rule.Clause == clause && v.Rule.TestNumber == 2 {
				return true
			}
		}
		return false
	}

	const unselected = "0 0 10 10 re f"
	const selected = "/G1 gs 0 0 10 10 re f"

	states := []struct {
		name   string
		gsBody string
	}{
		{"a transparent blend mode", "<< /Type /ExtGState /BM /Darken >>"},
		{"a fill alpha below 1", "<< /Type /ExtGState /ca 0.5 >>"},
		{"a stroke alpha below 1", "<< /Type /ExtGState /CA 0.5 >>"},
	}

	profiles := []struct {
		name    string
		profile models.PDFAProfile
		clause  string
	}{
		{"PDF/A-2b", models.PDFA2b, "6.2.10"},
		{"PDF/A-4", models.PDFA4, "6.2.9"},
	}

	for _, pr := range profiles {
		for _, st := range states {
			t.Run(pr.name+": "+st.name+" the content never selects", func(t *testing.T) {
				t.Parallel()
				if fires(t, pdf(st.gsBody, unselected), pr.profile, pr.clause) {
					t.Error("a graphics state no gs operator selects contributes no transparency")
				}
			})
			t.Run(pr.name+": "+st.name+" the content selects", func(t *testing.T) {
				t.Parallel()
				if !fires(t, pdf(st.gsBody, selected), pr.profile, pr.clause) {
					t.Error("a selected transparent graphics state with no blending colour space must fire")
				}
			})
		}
	}

	t.Run("an opaque state the content selects stays silent", func(t *testing.T) {
		t.Parallel()
		// The control: /BM /Normal is not transparency, so selecting it must
		// not fire either.
		if fires(t, pdf("<< /Type /ExtGState /BM /Normal >>", selected), models.PDFA2b, "6.2.10") {
			t.Error("/BM /Normal is not transparency")
		}
	})
}
