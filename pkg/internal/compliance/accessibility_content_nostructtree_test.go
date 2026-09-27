package compliance

import (
	"context"
	"fmt"
	"testing"
)

// TestCheckContentMarking_MCIDWithoutStructTree pins that marked content with
// an /MCID is not tagged content when the document has no structure tree (ISO
// 14289-1, 7.1 t3; ISO 14289-2, 8.2.2 t1).
//
// veraPDF decides tagging from the structure element owning the content
// (GFSEGroupedContent.isTaggedContent walks its parent chain and is true only
// on reaching the StructTreeRoot), so with no structure tree there is no owner
// and the content is untagged. litho used to count any /MCID as tagged. The
// control keeps the same content tagged once a structure tree exists whose
// ParentTree maps the MCID to an element reaching the root.
//
// # Corpus evidence
//
//   - pdfjs bug1019475_1.pdf: wraps its text in /H1 <</MCID 0>> BDC sequences
//     under a catalog with no StructTreeRoot; veraPDF reports it and litho,
//     counting any /MCID as tagged, did not.
func TestCheckContentMarking_MCIDWithoutStructTree(t *testing.T) {
	t.Parallel()

	const content = "/P <</MCID 0>> BDC\nBT 10 50 Td (x) Tj ET\nEMC\n"
	tests := []struct {
		name       string
		structTree bool
		fires      bool
	}{
		{"no structure tree", false, true},
		{"structure tree present", true, false},
	}
	for _, tt := range tests {
		for _, profile := range []string{"PDF/UA-1", "PDF/UA-2"} {
			t.Run(tt.name+" "+profile, func(t *testing.T) {
				t.Parallel()
				catalog := "<< /Type /Catalog /Pages 2 0 R >>"
				objs := []string{
					catalog,
					"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
					"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R >>",
					fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
				}
				if tt.structTree {
					objs[0] = "<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 5 0 R >>"
					objs[2] = "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /StructParents 0 >>"
					objs = append(objs,
						"<< /Type /StructTreeRoot /K [6 0 R] /ParentTree 7 0 R >>",
						"<< /Type /StructElem /S /P /P 5 0 R /Pg 3 0 R /K [0] >>",
						"<< /Nums [0 [6 0 R]] >>")
				}
				doc, cat := tableDoc(t, 1, objs...)
				rule := Rule_14289_1_7_1_t03
				if profile == "PDF/UA-2" {
					rule = Rule_14289_2_8_2_2_t01
				}
				got := checkContentMarking(context.Background(), doc, cat, profile)
				if fired := hasRule(got, rule); fired != tt.fires {
					t.Errorf("fired = %v, want %v (%v)", fired, tt.fires, got)
				}
			})
		}
	}
}
