package compliance

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// capNode is one structure element of a [TestUA1CaptionKidPositions] tree.
type capNode struct {
	s    string
	kids []capNode
}

// buildCapDoc writes tree under a Document element, object 6 being its root.
func buildCapDoc(t *testing.T, tree capNode, roleMap string) *document.Document {
	t.Helper()
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R /MarkInfo << /Marked true >> /Lang (en) >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>",
		"<< /Type /StructTreeRoot /K [5 0 R] /RoleMap " + roleMap + " >>",
		"<< /S /Document /P 4 0 R /K [6 0 R] >>",
	}
	var add func(n capNode, parent int) int
	add = func(n capNode, parent int) int {
		num := len(objs) + 1
		objs = append(objs, "")
		kids := make([]string, 0, len(n.kids))
		for _, k := range n.kids {
			kids = append(kids, fmt.Sprintf("%d 0 R", add(k, num)))
		}
		objs[num-1] = fmt.Sprintf("<< /S /%s /P %d 0 R /K [%s] >>", n.s, parent, strings.Join(kids, " "))
		return num
	}
	add(tree, 5)
	doc, _ := objsDocPDF(t, objs...)
	return doc
}

// TestUA1CaptionKidPositions pins ISO 14289-1, 7.2 t16, t28 and t40 as veraPDF
// tests them: on the Table, TOC or L itself (SETable, SETOC, SEL, built from the
// standard type) over its kidsStandardTypes, so the role map decides the
// parent's and each kid's type, a Div wraps nothing away, and a parent fails
// once however many Captions are misplaced. Every case was measured against
// veraPDF.
func TestUA1CaptionKidPositions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tr := capNode{"TR", []capNode{{"TD", nil}}}
	li := capNode{"LI", []capNode{{"LBody", nil}}}
	caption := capNode{"Caption", nil}

	tests := []struct {
		name          string
		tree          capNode
		roleMap       string
		t16, t28, t40 int
	}{
		{"a Caption between two rows", capNode{"Table", []capNode{tr, caption, tr}}, "<< >>", 1, 0, 0},
		{"a name mapped to Caption between two rows", capNode{"Table", []capNode{tr, {"MyCap", nil}, tr}},
			"<< /MyCap /Caption >>", 1, 0, 0},
		{"a name mapped to Table with a middle Caption", capNode{"MyTable", []capNode{tr, caption, tr}},
			"<< /MyTable /Table >>", 1, 0, 0},
		{"two middle Captions", capNode{"Table", []capNode{tr, caption, caption, tr}}, "<< >>", 1, 0, 0},
		{"a middle Caption inside a Div", capNode{"Table", []capNode{tr, {"Div", []capNode{caption}}, tr}},
			"<< >>", 1, 0, 0},
		{"a last Caption", capNode{"Table", []capNode{tr, tr, caption}}, "<< >>", 0, 0, 0},
		{"a first Caption", capNode{"Table", []capNode{caption, tr}}, "<< >>", 0, 0, 0},
		{"a middle Caption mapped to P", capNode{"Table", []capNode{tr, caption, tr}}, "<< /Caption /P >>", 0, 0, 0},
		{"an L with a second Caption", capNode{"L", []capNode{li, caption}}, "<< >>", 0, 0, 1},
		{"an L with two late Captions", capNode{"L", []capNode{li, caption, caption}}, "<< >>", 0, 0, 1},
		{"a name mapped to L with a second Caption", capNode{"MyL", []capNode{li, caption}}, "<< /MyL /L >>", 0, 0, 1},
		{"a TOC with a second Caption", capNode{"TOC", []capNode{{"TOCI", nil}, caption}}, "<< >>", 0, 1, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			viols, err := ValidatePDFUA(ctx, buildCapDoc(t, tt.tree, tt.roleMap), models.PDFUA1)
			if err != nil {
				t.Fatalf("ValidatePDFUA: %v", err)
			}
			got := map[int]int{}
			for _, e := range viols {
				if v, ok := e.(Violation); ok && v.Rule.Clause == "7.2" {
					got[v.Rule.TestNumber]++
				}
			}
			if got[16] != tt.t16 || got[28] != tt.t28 || got[40] != tt.t40 {
				t.Errorf("7.2 t16/t28/t40 fired %d/%d/%d times, want %d/%d/%d",
					got[16], got[28], got[40], tt.t16, tt.t28, tt.t40)
			}
		})
	}
}
