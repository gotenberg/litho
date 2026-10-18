package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestStructureDestinationForms pins ISO 14289-2, 8.8 t1 and t2 as veraPDF
// decides them. A destination (t1) is a structure destination when it, or the
// value a string names in the /Names /Dests tree or a name names in the catalog
// /Dests, is a dictionary with a non-null /SD or an array whose first element
// carries a non-null /S. A GoTo action (t2) passes on a non-null /SD, or on a /D
// that is a name or string naming a structure destination; an explicit /D array
// and a missing /D both fail. A /Dest written as null is absent (ISO 32000-1,
// 7.3.7). Every case was measured against veraPDF.
func TestStructureDestinationForms(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const (
		sdDict   = "<< /D [3 0 R /Fit] /SD [5 0 R] >>"
		nameTree = " /Names << /Dests << /Names [(d1) "
	)

	tests := []struct {
		name   string
		link   string // entries on the Link annotation
		cat    string // entries on the catalog
		t1, t2 int
	}{
		{"a GoTo with a /SD", " /A << /S /GoTo /D [3 0 R /Fit] /SD [5 0 R] >>", "", 0, 0},
		{"a GoTo whose /SD is null", " /A << /S /GoTo /D [3 0 R /Fit] /SD null >>", "", 0, 1},
		{"a GoTo whose /SD is a name", " /A << /S /GoTo /D [3 0 R /Fit] /SD /x >>", "", 0, 0},
		{"a string /Dest naming a dictionary with /SD", " /Dest (d1)", nameTree + sdDict + "] >> >>", 0, 0},
		{"a string /Dest naming a dictionary whose /SD is null", " /Dest (d1)", nameTree + "<< /D [3 0 R /Fit] /SD null >>] >> >>", 1, 0},
		{"a string /Dest naming an array to a structure element", " /Dest (d1)", nameTree + "[5 0 R /Fit]] >> >>", 0, 0},
		{"a name /Dest naming a catalog /Dests dictionary with /SD", " /Dest /d1", " /Dests << /d1 " + sdDict + " >>", 0, 0},
		{"a string /Dest found only in the catalog /Dests", " /Dest (d1)", " /Dests << /d1 " + sdDict + " >>", 1, 0},
		{"an array whose first element has /Type /StructElem and no /S", " /Dest [9 0 R /Fit]", "", 1, 0},
		{"an array whose first element has a null /S", " /Dest [10 0 R /Fit]", "", 1, 0},
		{"a /Dest written as null", " /Dest null", "", 0, 0},
		{"a GoTo /D string naming a structure destination", " /A << /S /GoTo /D (d1) >>", nameTree + sdDict + "] >> >>", 0, 0},
		{"a GoTo /D name naming a structure destination", " /A << /S /GoTo /D /d1 >>", " /Dests << /d1 " + sdDict + " >>", 0, 0},
		{"a GoTo /D array to a structure element", " /A << /S /GoTo /D [5 0 R /Fit] >>", "", 0, 1},
		{"a GoTo with neither /D nor /SD", " /A << /S /GoTo >>", "", 0, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := objsDocPDF(t,
				"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R /MarkInfo << /Marked true >> /Lang (en)"+tt.cat+" >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Annots [7 0 R] /Tabs /S >>",
				"<< /Type /StructTreeRoot /K 5 0 R /ParentTree << /Nums [0 6 0 R] >> /Namespaces [8 0 R] >>",
				"<< /Type /StructElem /S /Document /NS 8 0 R /P 4 0 R /K [6 0 R] >>",
				"<< /Type /StructElem /S /Link /NS 8 0 R /P 5 0 R /Pg 3 0 R /K [<< /Type /OBJR /Obj 7 0 R /Pg 3 0 R >>] >>",
				"<< /Type /Annot /Subtype /Link /Rect [10 10 100 30] /F 4 /StructParent 0 /Contents (link)"+tt.link+" >>",
				"<< /Type /Namespace /NS (http://iso.org/pdf2/ssn) >>",
				"<< /Type /StructElem /P 5 0 R >>",
				"<< /Type /StructElem /S null /P 5 0 R >>",
			)
			viols, err := ValidatePDFUA(ctx, doc, models.PDFUA2)
			if err != nil {
				t.Fatalf("ValidatePDFUA: %v", err)
			}
			if got := countRuleErrs(t, viols, Rule_14289_2_8_8_t01); got != tt.t1 {
				t.Errorf("8.8 t1 fired %d times, want %d", got, tt.t1)
			}
			if got := countRuleErrs(t, viols, Rule_14289_2_8_8_t02); got != tt.t2 {
				t.Errorf("8.8 t2 fired %d times, want %d", got, tt.t2)
			}
		})
	}
}

// TestOutlineStructureDestination pins ISO 14289-2, 8.8 on outline items: a
// /Dest written as null is absent (ISO 32000-1, 7.3.7), a page destination is no
// structure destination, and a GoTo action with no /D fails t2. Every case was
// measured against veraPDF.
func TestOutlineStructureDestination(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		name   string
		item   string // entries on the outline item
		t1, t2 int
	}{
		{"a /Dest written as null", " /Dest null", 0, 0},
		{"a page destination", " /Dest [3 0 R /Fit]", 1, 0},
		{"a GoTo with no /D", " /A << /S /GoTo >>", 0, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := objsDocPDF(t,
				"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R /MarkInfo << /Marked true >> /Lang (en) /Outlines 7 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>",
				"<< /Type /StructTreeRoot /K 5 0 R /Namespaces [6 0 R] >>",
				"<< /Type /StructElem /S /Document /NS 6 0 R /P 4 0 R >>",
				"<< /Type /Namespace /NS (http://iso.org/pdf2/ssn) >>",
				"<< /Type /Outlines /First 8 0 R /Last 8 0 R /Count 1 >>",
				"<< /Title (x) /Parent 7 0 R"+tt.item+" >>",
			)
			viols, err := ValidatePDFUA(ctx, doc, models.PDFUA2)
			if err != nil {
				t.Fatalf("ValidatePDFUA: %v", err)
			}
			if got := countRuleErrs(t, viols, Rule_14289_2_8_8_t01); got != tt.t1 {
				t.Errorf("8.8 t1 fired %d times, want %d", got, tt.t1)
			}
			if got := countRuleErrs(t, viols, Rule_14289_2_8_8_t02); got != tt.t2 {
				t.Errorf("8.8 t2 fired %d times, want %d", got, tt.t2)
			}
		})
	}
}

// TestCatalogAAStructureDestination pins ISO 14289-2, 8.8 t2 on the GoTo
// actions of the catalog /AA. Its document-level triggers WC, WS, DS, WP and DP
// (ISO 32000-1, Table 197) carry actions as a page's O and C do, and veraPDF
// builds them (PDCatalogAdditionalActions) and judges each GoTo among them; a
// key outside that list names no catalog action. Every case was measured with
// veraPDF 1.30.2 on the same shape.
func TestCatalogAAStructureDestination(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		name string
		aa   string // the catalog /AA
		t2   int
	}{
		{"a GoTo under WC", "<< /WC << /S /GoTo /D [3 0 R /Fit] >> >>", 1},
		{"GoTos under every document trigger", "<< /WC << /S /GoTo >> /WS << /S /GoTo >> /DS << /S /GoTo >> /WP << /S /GoTo >> /DP << /S /GoTo >> >>", 5},
		{"a GoTo under WS with a structure destination", "<< /WS << /S /GoTo /D [3 0 R /Fit] /SD [5 0 R] >> >>", 0},
		{"a GoTo under E, no catalog trigger", "<< /E << /S /GoTo /D [3 0 R /Fit] >> >>", 0},
		{"a GoTo run through /Next after a WC action", "<< /WC << /S /JavaScript /JS (1) /Next << /S /GoTo >> >> >>", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := objsDocPDF(t,
				"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R /MarkInfo << /Marked true >> /Lang (en) /AA "+tt.aa+" >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>",
				"<< /Type /StructTreeRoot /K 5 0 R /Namespaces [6 0 R] >>",
				"<< /Type /StructElem /S /Document /NS 6 0 R /P 4 0 R >>",
				"<< /Type /Namespace /NS (http://iso.org/pdf2/ssn) >>",
			)
			viols, err := ValidatePDFUA(ctx, doc, models.PDFUA2)
			if err != nil {
				t.Fatalf("ValidatePDFUA: %v", err)
			}
			if got := countRuleErrs(t, viols, Rule_14289_2_8_8_t02); got != tt.t2 {
				t.Errorf("8.8 t2 fired %d times, want %d", got, tt.t2)
			}
		})
	}
}
