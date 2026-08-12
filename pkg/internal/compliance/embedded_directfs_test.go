package compliance

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// TestDirectFileSpecInAnnotIsChecked pins that a file specification written as a
// direct dictionary is subject to ISO 19005-3 6.8.
//
// ISO 32000-1, 7.11.3 does not require a file specification to be indirect, and
// [collectFileSpecs] finds them by sweeping the cross-reference map, which holds
// only indirect objects. A FileAttachment annotation whose /FS is inline was
// therefore invisible to every clause 6.8 check: pdfjs issue14203.pdf attaches
// example.tex that way, and litho passed it while veraPDF failed t2, t3 and t4.
func TestDirectFileSpecInAnnotIsChecked(t *testing.T) {
	t.Parallel()

	data := cyclePDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /FileAttachment /Rect [0 0 20 20] "+
			"/FS << /Type /Filespec /F (a.tex) /EF << /F 5 0 R >> >> >>",
		"<< /Length 5 /Subtype /text#2Fplain >>\nstream\nlitho\nendstream",
	)

	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	viols, err := ValidatePDFA(context.Background(), doc, models.PDFA3b)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}

	counts := map[int]int{}
	for _, e := range viols {
		if v, ok := e.(Violation); ok && v.Rule.Clause == "6.8" {
			counts[v.Rule.TestNumber]++
		}
	}

	// t2: the filespec carries /F but no /UF. t3: no /AFRelationship. t4: no /AF
	// array anywhere references it. veraPDF 1.30.2 reports one failed check each.
	for _, test := range []int{2, 3, 4} {
		if counts[test] != 1 {
			t.Errorf("clause 6.8 t%d violations = %d, want 1", test, counts[test])
		}
	}
}

// TestDirectFileSpecReportedOncePerAnnot checks an annotation reachable from two
// pages states its filespec once. /Annots may list the same annotation object on
// several pages, and a per-page walk would report the same defect twice.
func TestDirectFileSpecReportedOncePerAnnot(t *testing.T) {
	t.Parallel()

	data := cyclePDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 6 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /FileAttachment /Rect [0 0 20 20] "+
			"/FS << /Type /Filespec /F (a.tex) /EF << /F 5 0 R >> >> >>",
		"<< /Length 5 /Subtype /text#2Fplain >>\nstream\nlitho\nendstream",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
	)

	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	viols, err := ValidatePDFA(context.Background(), doc, models.PDFA3b)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}

	got := 0
	for _, e := range viols {
		if v, ok := e.(Violation); ok && v.Rule.Clause == "6.8" && v.Rule.TestNumber == 3 {
			got++
		}
	}
	if got != 1 {
		t.Errorf("clause 6.8 t3 violations = %d, want 1 (the annotation is one object)", got)
	}
}
