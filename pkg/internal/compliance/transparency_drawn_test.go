package compliance

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// TestTransparency_ScopedToDrawnResources pins that a page "contains
// transparency" only when its content stream paints something transparent.
//
// ISO 19005-2, 6.2.10 t2 and ISO 19005-4, 6.2.9 t2 ask whether the page
// contains transparency, which veraPDF answers from the content stream: it
// raises PDPage.containsTransparency while walking operators, so a soft masked
// image the page never paints leaves it false. Two files identical but for a
// single Do settle it, and litho reported both.
//
// # Corpus evidence
//
//   - pdfjs issue12963.pdf: declares eleven soft masked images and draws only
//     FXX15, which carries no /SMask, and veraPDF reports nothing at 2b or
//     at 4.
//   - pdfjs issue14847.pdf: declares two soft masked images and draws nothing
//     at all, and veraPDF reports nothing at 2b or at 4.
func TestTransparency_ScopedToDrawnResources(t *testing.T) {
	t.Parallel()

	// pdf builds a page whose resources declare a soft-masked image, painting
	// it only when draw is set. The document carries no output intent and the
	// page no /Group, so the blending colour space rule turns on the
	// transparency question alone.
	pdf := func(content string) []byte {
		return cyclePDF(
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] "+
				"/Resources << /XObject << /Im0 5 0 R >> >> /Contents 4 0 R >>",
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
			"<< /Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray "+
				"/BitsPerComponent 8 /SMask 6 0 R /Length 1 >>\nstream\n\xff\nendstream",
			"<< /Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray "+
				"/BitsPerComponent 8 /Length 1 >>\nstream\n\xff\nendstream",
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

	cases := []struct {
		name    string
		profile models.PDFAProfile
		clause  string
	}{
		{"PDF/A-2b", models.PDFA2b, "6.2.10"},
		{"PDF/A-4", models.PDFA4, "6.2.9"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			t.Run("a painted soft-masked image is transparency", func(t *testing.T) {
				t.Parallel()
				if !fires(t, pdf("q 200 0 0 100 10 10 cm /Im0 Do Q"), tc.profile, tc.clause) {
					t.Error("the page paints a soft-masked image and defines no blending colour space")
				}
			})

			t.Run("a declared but unpainted one is not", func(t *testing.T) {
				t.Parallel()
				if fires(t, pdf("q 1 0 0 1 0 0 cm Q"), tc.profile, tc.clause) {
					t.Error("nothing paints /Im0, so the page contains no transparency")
				}
			})

			t.Run("an empty content stream paints nothing", func(t *testing.T) {
				t.Parallel()
				if fires(t, pdf(""), tc.profile, tc.clause) {
					t.Error("a page with no operators cannot contain transparency")
				}
			})
		})
	}
}
