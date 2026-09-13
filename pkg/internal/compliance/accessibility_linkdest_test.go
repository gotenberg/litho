package compliance

import (
	"context"
	"fmt"
	"testing"
)

// TestUA2LinkTargetConflict pins ISO 14289-2, 8.2.5.20 t2 as the PDF/UA-2
// validation profile tests it: on each Link annotation, against the element its
// /StructParent names, whatever that element's type. An annotation is reported
// when an earlier Link annotation under the same element targeted a different
// structure destination, so the count follows the annotations, not the
// elements. Every case was measured against veraPDF.
func TestUA2LinkTargetConflict(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Objects 7, 8 and 9 are the structure elements the links target; object 6
	// encloses the links, which are objects 10, 11 and 12.
	link := func(structParent, target int) string {
		return fmt.Sprintf("<< /Type /Annot /Subtype /Link /Rect [0 0 10 10] /StructParent %d"+
			" /A << /S /GoTo /SD [%d 0 R /Fit] /D [3 0 R /Fit] >> >>", structParent, target)
	}
	tests := []struct {
		name    string
		parentS string
		roleMap string
		targets []int
		want    int
	}{
		{"a P holding links onto two elements", "P", "<< >>", []int{7, 8}, 1},
		{"a name mapped to Link holding links onto two elements", "MyLink", "<< /MyLink /Link >>", []int{7, 8}, 1},
		{"a Link holding links onto two elements", "Link", "<< >>", []int{7, 8}, 1},
		{"a Link holding links onto three elements", "Link", "<< >>", []int{7, 8, 9}, 2},
		{"a Link holding links onto A, B, then A", "Link", "<< >>", []int{7, 8, 7}, 2},
		{"a Link holding links onto A, A, then B", "Link", "<< >>", []int{7, 7, 8}, 1},
		{"a Link holding two links onto one element", "Link", "<< >>", []int{7, 7}, 0},
		// t2 has no artifact exemption, unlike the nesting rules.
		{"an Artifact holding links onto two elements", "Artifact", "<< >>", []int{7, 8}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var annotRefs, objrs, nums string
			links := make([]string, 3)
			for i := range links {
				target := 7
				if i < len(tt.targets) {
					target = tt.targets[i]
					annotRefs += fmt.Sprintf(" %d 0 R", 10+i)
					objrs += fmt.Sprintf(" << /Type /OBJR /Obj %d 0 R >>", 10+i)
					nums += fmt.Sprintf(" %d 6 0 R", i)
				}
				links[i] = link(i, target)
			}
			doc, cat := objsDocPDF(t,
				"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots ["+annotRefs+" ] >>",
				"<< /Type /StructTreeRoot /K 5 0 R /ParentTree << /Nums ["+nums+" ] >> /RoleMap "+tt.roleMap+" >>",
				"<< /Type /StructElem /S /Document /P 4 0 R /K [6 0 R 7 0 R 8 0 R 9 0 R] >>",
				"<< /Type /StructElem /S /"+tt.parentS+" /P 5 0 R /K ["+objrs+" ] >>",
				"<< /Type /StructElem /S /P /P 5 0 R >>",
				"<< /Type /StructElem /S /P /P 5 0 R >>",
				"<< /Type /StructElem /S /P /P 5 0 R >>",
				links[0], links[1], links[2],
			)
			got := 0
			for _, v := range checkPDFUA2AnnotStructNesting(ctx, doc, cat, "PDF/UA-2") {
				if v.Rule == Rule_14289_2_8_2_5_20_t02 {
					got++
				}
			}
			if got != tt.want {
				t.Errorf("8.2.5.20 t2 fired %d times, want %d", got, tt.want)
			}
		})
	}
}
