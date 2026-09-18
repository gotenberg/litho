package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestTOCIRef_StandardTypeAndDescendants pins ISO 14289-2, 8.2.5.8 t1 as
// veraPDF judges it: the requirement binds the structure type an element stands
// for through the role map (its SETOCI), and a /Ref on a child structure element
// identifies the target as well as one on the TOCI itself (its containsRef
// descends through every structure-element kid). Every case was measured against
// veraPDF.
func TestTOCIRef_StandardTypeAndDescendants(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		name    string
		toci    string // object 12, the TOC's kid
		child   string // object 8, which the ParentTree names for MCID 0
		roleMap string
		want    bool
	}{
		{
			"a name mapped to TOCI, without /Ref", "<< /S /MyTOCI /K [8 0 R] /P 7 0 R >>",
			"<< /S /P /Pg 3 0 R /K [0] /P 12 0 R >>", "<< /MyTOCI /TOCI >>", true,
		},
		{
			"a TOCI without /Ref", "<< /S /TOCI /K [8 0 R] /P 7 0 R >>",
			"<< /S /P /Pg 3 0 R /K [0] /P 12 0 R >>", "<< >>", true,
		},
		{
			"a TOCI whose child carries /Ref", "<< /S /TOCI /K [8 0 R] /P 7 0 R >>",
			"<< /S /P /Pg 3 0 R /K [0] /P 12 0 R /Ref [6 0 R] >>", "<< >>", false,
		},
		{
			"a TOCI carrying /Ref", "<< /S /TOCI /K [8 0 R] /P 7 0 R /Ref [6 0 R] >>",
			"<< /S /P /Pg 3 0 R /K [0] /P 12 0 R >>", "<< >>", false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := objsDocPDF(t,
				"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 5 0 R /MarkInfo << /Marked true >> /Lang (en) >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 4 0 R /StructParents 0"+
					" /Resources << /Font << /F1 9 0 R >> >> >>",
				streamObj("/P <</MCID 0>> BDC BT /F1 12 Tf 10 50 Td (Hi) Tj ET EMC"),
				"<< /Type /StructTreeRoot /K [6 0 R] /ParentTree 10 0 R /RoleMap "+tt.roleMap+" >>",
				"<< /S /Document /K [7 0 R] /P 5 0 R >>",
				"<< /S /TOC /K [12 0 R] /P 6 0 R >>",
				tt.child,
				"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
				"<< /Nums [0 [8 0 R]] >>",
				"<< >>",
				tt.toci,
			)
			viols, err := ValidatePDFUA(ctx, doc, models.PDFUA2)
			if err != nil {
				t.Fatalf("ValidatePDFUA: %v", err)
			}
			fired := false
			for _, e := range viols {
				if v, ok := e.(Violation); ok && v.Rule.Clause == "8.2.5.8" && v.Rule.TestNumber == 1 {
					fired = true
				}
			}
			if fired != tt.want {
				t.Errorf("8.2.5.8 t1 fired = %v, want %v", fired, tt.want)
			}
		})
	}
}
