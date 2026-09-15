package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestFormRoleAttribute pins ISO 14289-1, 7.18.4 t2's Role attribute as
// veraPDF reads it (AttributeHelper.getRole: a PrintField-owned /Role name,
// from /A and then from the classes /C names in the /ClassMap, not inherited):
// a null or string /Role is absent, and a /Role supplied through a class
// counts. The Form has two widget kids, so only the Role can excuse it. Every
// case was measured against veraPDF.
func TestFormRoleAttribute(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		name      string
		elemExtra string
		rootExtra string
		want      int
	}{
		{"a PrintField /Role name", " /A << /O /PrintField /Role /tv >>", "", 0},
		{"a PrintField /Role written as null", " /A << /O /PrintField /Role null >>", "", 1},
		{"a PrintField /Role that is a string", " /A << /O /PrintField /Role (tv) >>", "", 1},
		{"a PrintField /Role through a class", " /C /pf", " /ClassMap << /pf << /O /PrintField /Role /tv >> >>", 0},
		{"no /Role", "", "", 1},
		{"a /Role under another owner", " /A << /O /Layout /Role /tv >>", "", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := objsDocPDF(t,
				"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R /AcroForm << /Fields [7 0 R 8 0 R] >>"+
					" /MarkInfo << /Marked true >> /Lang (en) >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Annots [7 0 R 8 0 R] >>",
				"<< /Type /StructTreeRoot /K 5 0 R /ParentTree << /Nums [0 6 0 R 1 6 0 R] >>"+tt.rootExtra+" >>",
				"<< /S /Document /P 4 0 R /K [6 0 R] >>",
				"<< /S /Form /P 5 0 R /Pg 3 0 R /K [<< /Type /OBJR /Obj 7 0 R /Pg 3 0 R >>"+
					" << /Type /OBJR /Obj 8 0 R /Pg 3 0 R >>]"+tt.elemExtra+" >>",
				"<< /Type /Annot /Subtype /Widget /FT /Tx /T (f0) /TU (field 0) /Rect [10 10 100 30] /F 4 /StructParent 0 >>",
				"<< /Type /Annot /Subtype /Widget /FT /Tx /T (f1) /TU (field 1) /Rect [10 40 100 60] /F 4 /StructParent 1 >>",
			)
			viols, err := ValidatePDFUA(ctx, doc, models.PDFUA1)
			if err != nil {
				t.Fatalf("ValidatePDFUA: %v", err)
			}
			if got := countRuleErrs(t, viols, Rule_14289_1_7_18_4_t02); got != tt.want {
				t.Errorf("7.18.4 t2 fired %d times, want %d", got, tt.want)
			}
		})
	}
}

// TestWidgetAAAndContents pins ISO 14289-2, 8.10.2.3 t1 and t2 as veraPDF reads
// a field widget: containsAA is a knownKey on /AA, so a null /AA is absent, and
// /Contents counts only as a string, empty or not, so a null or name /Contents
// is absent (ISO 32000-1, 7.3.7). Every case was measured against veraPDF.
func TestWidgetAAAndContents(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const aa = " /AA << /K << /S /JavaScript /JS (1) >> >>"

	tests := []struct {
		name   string
		extra  string // entries on the widget
		t1, t2 int
	}{
		{"an /AA written as null and no /Contents", " /AA null", 1, 0},
		{"an /AA with a /Contents written as null", aa + " /Contents null", 1, 1},
		{"an /AA with a /Contents that is a name", aa + " /Contents /x", 1, 1},
		{"an /AA with a /Contents of text", aa + " /Contents (field)", 0, 0},
		{"an /AA and no /Contents", aa, 1, 1},
		{"an /AA with an empty /Contents", aa + " /Contents ()", 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := objsDocPDF(t,
				"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R /AcroForm << /Fields [7 0 R] >>"+
					" /MarkInfo << /Marked true >> /Lang (en) >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Annots [7 0 R] >>",
				"<< /Type /StructTreeRoot /K 5 0 R /ParentTree << /Nums [0 6 0 R] >> /Namespaces [8 0 R] >>",
				"<< /S /Document /NS 8 0 R /P 4 0 R /K [6 0 R] >>",
				"<< /S /Form /NS 8 0 R /P 5 0 R /Pg 3 0 R /K [<< /Type /OBJR /Obj 7 0 R /Pg 3 0 R >>] >>",
				"<< /Type /Annot /Subtype /Widget /FT /Tx /T (f) /TU (field) /Rect [10 10 100 30] /F 4 /StructParent 0"+tt.extra+" >>",
				"<< /Type /Namespace /NS (http://iso.org/pdf2/ssn) >>",
			)
			viols, err := ValidatePDFUA(ctx, doc, models.PDFUA2)
			if err != nil {
				t.Fatalf("ValidatePDFUA: %v", err)
			}
			if got := countRuleErrs(t, viols, Rule_14289_2_8_10_2_3_t01); got != tt.t1 {
				t.Errorf("8.10.2.3 t1 fired %d times, want %d", got, tt.t1)
			}
			if got := countRuleErrs(t, viols, Rule_14289_2_8_10_2_3_t02); got != tt.t2 {
				t.Errorf("8.10.2.3 t2 fired %d times, want %d", got, tt.t2)
			}
		})
	}
}

// TestOCConfigNameAndAS pins ISO 14289-1, 7.10 t1 and t2 on the default
// optional content configuration as veraPDF reads it: /Name shall be a
// non-empty string, and an /AS written as null is absent (ISO 32000-1, 7.3.7).
// Every case was measured against veraPDF.
func TestOCConfigNameAndAS(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		name   string
		dcfg   string
		t1, t2 int
	}{
		{"an /AS written as null", "/Name (Default) /AS null", 0, 0},
		{"a /Name written as null", "/Name null", 1, 0},
		{"a /Name that is a name", "/Name /Default", 1, 0},
		{"a /Name of text", "/Name (Default)", 0, 0},
		{"an /AS array", "/Name (Default) /AS []", 0, 1},
		{"an empty /Name", "/Name ()", 1, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := objsDocPDF(t,
				"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R /MarkInfo << /Marked true >> /Lang (en)"+
					" /OCProperties << /OCGs [6 0 R] /D << "+tt.dcfg+" >> >> >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>",
				"<< /Type /StructTreeRoot /K 5 0 R >>",
				"<< /S /Document /P 4 0 R >>",
				"<< /Type /OCG /Name (Layer) >>",
			)
			viols, err := ValidatePDFUA(ctx, doc, models.PDFUA1)
			if err != nil {
				t.Fatalf("ValidatePDFUA: %v", err)
			}
			if got := countRuleErrs(t, viols, Rule_14289_1_7_10_t01); got != tt.t1 {
				t.Errorf("7.10 t1 fired %d times, want %d", got, tt.t1)
			}
			if got := countRuleErrs(t, viols, Rule_14289_1_7_10_t02); got != tt.t2 {
				t.Errorf("7.10 t2 fired %d times, want %d", got, tt.t2)
			}
		})
	}
}
