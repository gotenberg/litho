package compliance

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// rectPDF builds a page carrying one Square annotation with no appearance
// dictionary, whose /Rect is written as the caller specifies.
func rectPDF(rect string) []byte {
	return cyclePDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /Square /Rect "+rect+" /F 4 >>",
		"0", "0", "100", "100",
	)
}

// missingAPViolations counts the reports that an annotation carries no
// appearance dictionary.
func missingAPViolations(t *testing.T, data []byte) int {
	t.Helper()

	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	viols, err := ValidatePDFA(ctx, doc, models.PDFA2b)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}

	count := 0
	for _, e := range viols {
		if strings.Contains(e.Error(), "missing appearance dictionary") {
			count++
		}
	}
	return count
}

// TestZeroRectExemptionIgnoresIndirectCoordinates pins that an annotation whose
// /Rect coordinates are indirect references is still held to the appearance
// requirement.
//
// ISO 19005-2, 6.3.3 requires an appearance dictionary, and litho exempts an
// annotation whose rectangle encloses no area, since nothing it draws could be
// seen. ISO 32000-1, 7.3.9 allows an indirect reference wherever a direct object
// may appear, and reading those references as numbers gives zero for every
// corner: a real 100 by 100 rectangle looked empty, so the exemption applied and
// the missing /AP went unreported.
func TestZeroRectExemptionIgnoresIndirectCoordinates(t *testing.T) {
	t.Parallel()

	direct := missingAPViolations(t, rectPDF("[0 0 100 100]"))
	if direct != 1 {
		t.Fatalf("the direct rectangle reported %d missing-/AP violations, want 1", direct)
	}

	indirect := missingAPViolations(t, rectPDF("[5 0 R 6 0 R 7 0 R 8 0 R]"))
	if indirect != direct {
		t.Errorf("the same annotation written with indirect coordinates reported %d "+
			"missing-/AP violations, want %d: the zero-area exemption swallowed it",
			indirect, direct)
	}
}

// TestZeroRectExemptionStillApplies is the control: an annotation that really
// does enclose no area keeps the exemption.
func TestZeroRectExemptionStillApplies(t *testing.T) {
	t.Parallel()

	if got := missingAPViolations(t, rectPDF("[10 10 10 10]")); got != 0 {
		t.Errorf("a genuinely empty rectangle reported %d missing-/AP violations, want 0", got)
	}
}
