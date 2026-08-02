package compliance

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// roleMapPDF builds a minimal tagged one-page PDF whose structure tree holds a
// single element of type elemType, under a /RoleMap of roleMap.
func roleMapPDF(roleMap, elemType string) []byte {
	return cyclePDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 5 0 R /Lang (en-US) "+
			"/MarkInfo << /Marked true >> /ViewerPreferences << /DisplayDocTitle true >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /StructParents 0 /Tabs /S >>",
		"<< /Length 0 >>\nstream\n\nendstream",
		fmt.Sprintf("<< /Type /StructTreeRoot /K [6 0 R] /ParentTree 8 0 R /RoleMap %s >>", roleMap),
		"<< /Type /StructElem /S /Document /P 5 0 R /K [7 0 R] >>",
		fmt.Sprintf("<< /Type /StructElem /S %s /P 6 0 R /Pg 3 0 R /K 0 >>", elemType),
		"<< /Nums [0 [7 0 R]] >>",
	)
}

// TestRoleMapRulesAreElementScoped covers which /RoleMap entries answer for a
// circular or dangling chain. Every /RoleMap rule carries a structure element
// as its object, not the map: ISO 14289-1 7.1 t5 and t6 name veraPDF's
// SENonStandard and PDStructElem, and ISO 14289-2 8.2.4 and ISO 19005-2 6.7.3.4
// spell the same pair. An entry no element uses has no element to answer for
// it, so litho reporting the map alone condemned documents veraPDF passes.
func TestRoleMapRulesAreElementScoped(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		roleMap  string
		elem     string
		clause   string
		test     int
		wantFire bool
	}{
		{
			name:     "a circular entry no element uses is inert",
			roleMap:  "<< /Foo /Bar /Bar /Foo >>",
			elem:     "/P",
			clause:   "7.1",
			test:     6,
			wantFire: false,
		},
		{
			name:     "the same cycle answers once an element uses it",
			roleMap:  "<< /Foo /Bar /Bar /Foo >>",
			elem:     "/Foo",
			clause:   "7.1",
			test:     6,
			wantFire: true,
		},
		{
			name:     "a chain reaching no standard type is inert while unused",
			roleMap:  "<< /Foo /Baz >>",
			elem:     "/P",
			clause:   "7.1",
			test:     5,
			wantFire: false,
		},
		{
			name:     "the same chain answers once an element uses it",
			roleMap:  "<< /Foo /Baz >>",
			elem:     "/Foo",
			clause:   "7.1",
			test:     5,
			wantFire: true,
		},
		{
			name:     "a standard type remapped but unused is inert",
			roleMap:  "<< /Caption /P >>",
			elem:     "/P",
			clause:   "7.1",
			test:     7,
			wantFire: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := firesUA1(t, roleMapPDF(tc.roleMap, tc.elem), tc.clause, tc.test)
			if got != tc.wantFire {
				t.Errorf("%s t%d fired = %v, want %v", tc.clause, tc.test, got, tc.wantFire)
			}
		})
	}
}

// TestRoleMapCycleReportsOncePerElement covers the count. veraPDF builds one
// PDStructElem per element and reports the cycle against that element, so a
// cycle spanning three entries used by one element is one violation, not three.
func TestRoleMapCycleReportsOncePerElement(t *testing.T) {
	t.Parallel()

	data := roleMapPDF("<< /Foo /Bar /Bar /Qux /Qux /Foo >>", "/Foo")
	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	viols, err := ValidatePDFUA(context.Background(), doc, models.PDFUA1)
	if err != nil {
		t.Fatalf("ValidatePDFUA: %v", err)
	}

	var n int
	for _, e := range viols {
		if v, ok := e.(Violation); ok && v.Rule.Clause == "7.1" && v.Rule.TestNumber == 6 {
			n++
		}
	}
	if n != 1 {
		t.Errorf("circular mapping violations = %d, want 1 (%v)", n, viols)
	}
}
