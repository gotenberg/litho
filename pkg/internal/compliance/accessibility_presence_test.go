package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// annotPresenceDoc builds a PDF/UA-2 document whose one annotation (object 7,
// with the given entries) sits in an Annot element (object 6) that its
// /StructParent names. Object 8 is a null object.
func annotPresenceDoc(t *testing.T, body, elemExtra string) *document.Document {
	t.Helper()
	doc, _ := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R /MarkInfo << /Marked true >> /Lang (en) >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Annots [7 0 R] >>",
		"<< /Type /StructTreeRoot /K 5 0 R /ParentTree << /Nums [0 6 0 R] >> /Namespaces [9 0 R] >>",
		"<< /S /Document /NS 9 0 R /P 4 0 R /K [6 0 R] >>",
		"<< /S /Annot /NS 9 0 R /P 5 0 R /Pg 3 0 R"+elemExtra+" /K [<< /Type /OBJR /Obj 7 0 R /Pg 3 0 R >>] >>",
		"<< /Type /Annot /Rect [10 10 60 60] /F 4 /StructParent 0 "+body+" >>",
		"null",
		"<< /Type /Namespace /NS (http://iso.org/pdf2/ssn) >>",
	)
	return doc
}

// TestAnnotEntryPresence pins how the PDF/UA-2 per-subtype description rules
// and ISO 14289-2, 8.9.4.2 t1 read an annotation's /Contents and /Name, as
// veraPDF reads them: /Contents counts only as a string (getStringKey), empty
// or not, and /Name only as a name (getNameKeyStringValue); a null value,
// written directly or through a reference to a null object, is absent
// (ISO 32000-1, 7.3.7). Every case was measured against veraPDF.
func TestAnnotEntryPresence(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const ink = "/Subtype /Ink /InkList [[10 10 60 60]]"

	tests := []struct {
		name      string
		body      string
		elemExtra string
		rule      Rule
		want      int
	}{
		{"an Ink /Contents written as null", ink + " /Contents null", "", Rule_14289_2_8_9_2_4_8_t01, 1},
		{"an Ink /Contents referring to a null object", ink + " /Contents 8 0 R", "", Rule_14289_2_8_9_2_4_8_t01, 1},
		{"an Ink /Contents of text", ink + " /Contents (a stroke)", "", Rule_14289_2_8_9_2_4_8_t01, 0},
		{"an Ink without /Contents", ink, "", Rule_14289_2_8_9_2_4_8_t01, 1},
		{"an empty Ink /Contents", ink + " /Contents ()", "", Rule_14289_2_8_9_2_4_8_t01, 0},
		{"an Ink /Contents that is a name", ink + " /Contents /x", "", Rule_14289_2_8_9_2_4_8_t01, 1},
		{"a Stamp /Name written as null", "/Subtype /Stamp /Name null", "", Rule_14289_2_8_9_2_4_7_t01, 1},
		{"a Stamp /Name that is a name", "/Subtype /Stamp /Name /Approved", "", Rule_14289_2_8_9_2_4_7_t01, 0},
		{"a Stamp /Name that is a string", "/Subtype /Stamp /Name (Approved)", "", Rule_14289_2_8_9_2_4_7_t01, 1},
		{"a null /Contents beside an enclosing /Alt", "/Subtype /Square /Contents null", " /Alt (y)", Rule_14289_2_8_9_4_2_t01, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			viols, err := ValidatePDFUA(ctx, annotPresenceDoc(t, tt.body, tt.elemExtra), models.PDFUA2)
			if err != nil {
				t.Fatalf("ValidatePDFUA: %v", err)
			}
			if got := countRuleErrs(t, viols, tt.rule); got != tt.want {
				t.Errorf("%s t%d fired %d times, want %d", tt.rule.Clause, tt.rule.TestNumber, got, tt.want)
			}
		})
	}
}

// TestFigureAltPresence pins how the Figure requirements read /ActualText and
// /Alt, as veraPDF reads them: /ActualText counts as a string or a name, empty
// or not, and /Alt only as a string; a null value, direct or referenced, and a
// numeric /ActualText are absent. Every case was measured against veraPDF.
func TestFigureAltPresence(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		name  string
		ua2   bool
		extra string // entries on the Figure (object 6); object 7 is null
		rule  Rule
		want  int
	}{
		{"UA-1: an /ActualText written as null", false, " /ActualText null", Rule_14289_1_7_3_t01, 1},
		{"UA-1: an /ActualText referring to a null object", false, " /ActualText 7 0 R", Rule_14289_1_7_3_t01, 1},
		{"UA-1: an empty /ActualText", false, " /ActualText ()", Rule_14289_1_7_3_t01, 0},
		{"UA-1: an /ActualText that is a name", false, " /ActualText /x", Rule_14289_1_7_3_t01, 0},
		{"UA-1: an /ActualText that is a number", false, " /ActualText 5", Rule_14289_1_7_3_t01, 1},
		{"UA-2: an /Alt written as null", true, " /Alt null", Rule_14289_2_8_2_5_28_2_t01, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ns, namespaces := "", ""
			profile := models.PDFUA1
			if tt.ua2 {
				ns, namespaces, profile = " /NS 8 0 R", " /Namespaces [8 0 R]", models.PDFUA2
			}
			doc, _ := objsDocPDF(t,
				"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R /MarkInfo << /Marked true >> /Lang (en) >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>",
				"<< /Type /StructTreeRoot /K 5 0 R"+namespaces+" >>",
				"<< /S /Document"+ns+" /P 4 0 R /K [6 0 R] >>",
				"<< /S /Figure"+ns+" /P 5 0 R"+tt.extra+" >>",
				"null",
				"<< /Type /Namespace /NS (http://iso.org/pdf2/ssn) >>",
			)
			viols, err := ValidatePDFUA(ctx, doc, profile)
			if err != nil {
				t.Fatalf("ValidatePDFUA: %v", err)
			}
			if got := countRuleErrs(t, viols, tt.rule); got != tt.want {
				t.Errorf("%s t%d fired %d times, want %d", tt.rule.Clause, tt.rule.TestNumber, got, tt.want)
			}
		})
	}
}
