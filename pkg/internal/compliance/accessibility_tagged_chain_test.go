package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestCheckContentMarking_OwnerReachesRoot pins that an /MCID tags its content
// only when the structure element the ParentTree names for it reaches the
// structure tree root through /P.
//
// veraPDF (GFSEGroupedContent.isTaggedContent) climbs the owner's /P chain and
// calls the content tagged only on reaching a /Type /StructTreeRoot node; a
// missing or non-dictionary /P, or a loop, stops the climb. ISO 32000-1,
// 14.7.2, Table 355 makes /P required. The chain need not follow the /K tree:
// an element outside it that climbs to the root still tags. The same climb
// decides ISO 14289-1, 7.1 t3 and ISO 14289-2, 8.2.2 t1. Every case was
// measured against veraPDF.
func TestCheckContentMarking_OwnerReachesRoot(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	const reaching = "<< /S /Sect /K [8 0 R] /P 6 0 R >>"
	tests := []struct {
		name    string
		profile string
		section string // object 7
		owner   string // object 8, which the ParentTree names for MCID 0
		want    bool   // the untagged-content rule fires
	}{
		{
			"an owner whose /P chain reaches the root", "PDF/UA-1",
			reaching, "<< /S /P /Pg 3 0 R /K [0] /P 7 0 R >>", false,
		},
		{
			"an owner with no /P", "PDF/UA-1",
			reaching, "<< /S /P /Pg 3 0 R /K [0] >>", true,
		},
		{
			"a parent with no /P", "PDF/UA-1",
			"<< /S /Sect /K [8 0 R] >>", "<< /S /P /Pg 3 0 R /K [0] /P 7 0 R >>", true,
		},
		{
			"a /P loop", "PDF/UA-1",
			"<< /S /Sect /K [8 0 R] /P 8 0 R >>", "<< /S /P /Pg 3 0 R /K [0] /P 7 0 R >>", true,
		},
		{
			"a /P that is no dictionary", "PDF/UA-1",
			reaching, "<< /S /P /Pg 3 0 R /K [0] /P 5 >>", true,
		},
		{
			"a /P straight to the root", "PDF/UA-1",
			reaching, "<< /S /P /Pg 3 0 R /K [0] /P 5 0 R >>", false,
		},
		{
			"a /P through an element outside the /K tree", "PDF/UA-1",
			reaching, "<< /S /P /Pg 3 0 R /K [0] /P 11 0 R >>", false,
		},
		{
			"PDF/UA-2: an owner with no /P", "PDF/UA-2",
			reaching, "<< /S /P /Pg 3 0 R /K [0] >>", true,
		},
		{
			"PDF/UA-2: an owner whose /P chain reaches the root", "PDF/UA-2",
			reaching, "<< /S /P /Pg 3 0 R /K [0] /P 7 0 R >>", false,
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
				"<< /Type /StructTreeRoot /K [6 0 R] /ParentTree 10 0 R >>",
				"<< /S /Document /K [7 0 R] /P 5 0 R >>",
				tt.section,
				tt.owner,
				"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
				"<< /Nums [0 [8 0 R]] >>",
				"<< /S /Div /P 6 0 R >>",
			)
			catalog, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 1})
			if err != nil {
				t.Fatalf("resolve catalog: %v", err)
			}
			rule := Rule_14289_1_7_1_t03
			if tt.profile == "PDF/UA-2" {
				rule = Rule_14289_2_8_2_2_t01
			}
			got := checkContentMarking(ctx, doc, catalog, tt.profile)
			if fired := hasRule(got, rule); fired != tt.want {
				t.Errorf("%s fired = %v, want %v (%v)", rule.Clause, fired, tt.want, got)
			}
		})
	}
}
