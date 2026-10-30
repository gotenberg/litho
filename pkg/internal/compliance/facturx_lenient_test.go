package compliance

import (
	"context"
	"log/slog"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

func lenientTestDoc() *document.Document {
	return &document.Document{Logger: slog.New(slog.DiscardHandler)}
}

func TestCheckInvoiceAFRelationship_StrictVsLenient(t *testing.T) {
	ctx := context.Background()
	doc := lenientTestDoc()
	minimum := variantFacturX.atLevel(models.FacturXMinimum)

	// /Alternative is a tolerated non-spec value for MINIMUM, which
	// Factur-X 1.07.2, 6.2.2 restricts to /Data.
	inv := invoiceFileSpec{found: true, objNum: 5, dict: ast.PdfDict{"AFRelationship": ast.PdfName("Alternative")}}

	strict := checkInvoiceAFRelationship(ctx, doc, inv, minimum, false)
	if len(strict) != 1 {
		t.Fatalf("strict /Alternative: got %d violations, want 1", len(strict))
	}
	if strict[0].Rule != Rule_invoice_AFRelationshipNonSpec {
		t.Errorf("strict rule = %+v, want AFRelationshipNonSpec", strict[0].Rule)
	}
	if got := SeverityOf(strict[0]); got != SeverityWarning {
		t.Errorf("strict severity = %q, want warning", got)
	}

	if lenient := checkInvoiceAFRelationship(ctx, doc, inv, minimum, true); len(lenient) != 0 {
		t.Errorf("lenient /Alternative: got %d violations, want 0", len(lenient))
	}

	// The canonical /Data is accepted in both modes.
	inv.dict["AFRelationship"] = ast.PdfName("Data")
	if got := checkInvoiceAFRelationship(ctx, doc, inv, minimum, false); len(got) != 0 {
		t.Errorf("strict /Data: got %d violations, want 0", len(got))
	}

	// A value that is neither canonical nor tolerated is fatal in both modes.
	inv.dict["AFRelationship"] = ast.PdfName("Bogus")
	bad := checkInvoiceAFRelationship(ctx, doc, inv, minimum, true)
	if len(bad) != 1 || SeverityOf(bad[0]) != SeverityFatal {
		t.Errorf("bogus /AFRelationship (lenient): got %v, want one fatal", bad)
	}
}

// TestCheckInvoiceAFRelationship_ByLevel pins the relationships Factur-X
// 1.07.2 admits per level (5.3, 6.2.2 and 7.7).
func TestCheckInvoiceAFRelationship_ByLevel(t *testing.T) {
	ctx := context.Background()
	doc := lenientTestDoc()

	for _, tc := range []struct {
		level models.FacturXLevel
		value string
		want  int
	}{
		{models.FacturXBasicWL, "Data", 0},
		{models.FacturXBasicWL, "Source", 1},
		{models.FacturXBasic, "Data", 0},
		{models.FacturXEN16931, "Source", 0},
		{models.FacturXExtended, "Alternative", 0},
		{models.FacturXXRechnung, "Alternative", 0},
		{models.FacturXXRechnung, "Data", 1},
		{"", "Source", 0},
	} {
		inv := invoiceFileSpec{found: true, objNum: 5, dict: ast.PdfDict{"AFRelationship": ast.PdfName(tc.value)}}
		got := checkInvoiceAFRelationship(ctx, doc, inv, variantFacturX.atLevel(tc.level), false)
		if len(got) != tc.want {
			t.Errorf("level %q, /%s: got %d violations, want %d", tc.level, tc.value, len(got), tc.want)
		}
	}
}

func TestCheckInvoiceStreamSubtype_StrictVsLenient(t *testing.T) {
	ctx := context.Background()
	doc := lenientTestDoc()

	inv := invoiceFileSpec{
		found:      true,
		streamRef:  ast.PdfIndirectRef{ObjectNum: 6},
		streamDict: ast.PdfDict{"Subtype": ast.PdfName("application/xml")},
	}

	strict := checkInvoiceStreamSubtype(ctx, doc, inv, variantFacturX, false)
	if len(strict) != 1 || strict[0].Rule != Rule_invoice_StreamSubtypeNonSpec || SeverityOf(strict[0]) != SeverityWarning {
		t.Errorf("strict application/xml: got %v, want one warning", strict)
	}

	if got := checkInvoiceStreamSubtype(ctx, doc, inv, variantFacturX, true); len(got) != 0 {
		t.Errorf("lenient application/xml: got %d violations, want 0", len(got))
	}

	// The mandated text/xml is accepted in both modes.
	inv.streamDict["Subtype"] = ast.PdfName("text/xml")
	if got := checkInvoiceStreamSubtype(ctx, doc, inv, variantFacturX, false); len(got) != 0 {
		t.Errorf("strict text/xml: got %d violations, want 0", len(got))
	}

	// An unrelated subtype is fatal regardless of mode.
	inv.streamDict["Subtype"] = ast.PdfName("application/pdf")
	bad := checkInvoiceStreamSubtype(ctx, doc, inv, variantFacturX, true)
	if len(bad) != 1 || SeverityOf(bad[0]) != SeverityFatal {
		t.Errorf("application/pdf (lenient): got %v, want one fatal", bad)
	}
}
