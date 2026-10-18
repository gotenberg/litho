package pdf_test

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
	"github.com/gotenberg/litho/pkg/pdf"
)

// TestValidatePDFA1aReportsEveryRubyAndWarichuElement pins that PDF/A-1a
// judges the Ruby and Warichu types non-standard. ISO 19005-1, 6.8.3.4 names
// the standard types of PDF Reference 1.4, 9.7.4, and PDF 1.5 added the seven.
// structkinds_ua2 draws 21 elements unmapped at PDF/A-1a, as veraPDF 1.30.2
// reports them: the two math elements, four Ruby, three Warichu and their
// RB, RT, RP, WT and WP children.
func TestValidatePDFA1aReportsEveryRubyAndWarichuElement(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data, err := os.ReadFile("../../test/integration/corpus/litho/pdfs/structkinds_ua2.pdf")
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}
	r, err := pdf.Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	count := func(profile models.PDFAProfile, clause string) int {
		vs, err := r.ValidatePDFAReport(ctx, profile)
		if err != nil {
			t.Fatalf("ValidatePDFAReport(%s): %v", profile, err)
		}
		n := 0
		for _, v := range vs {
			if v.Clause == clause && v.TestNumber == 1 {
				n++
			}
		}
		return n
	}
	if got := count(models.PDFA1a, "6.8.3.4"); got != 21 {
		t.Errorf("PDF/A-1a: 6.8.3.4 test 1 fires %d times, want 21", got)
	}
	// From part 2 the seven are standard (ISO 32000-1, 14.8.4.3.4), so only
	// the math elements stay unmapped.
	if got := count(models.PDFA2a, "6.7.3.4"); got != 2 {
		t.Errorf("PDF/A-2a: 6.7.3.4 test 1 fires %d times, want 2", got)
	}
}
