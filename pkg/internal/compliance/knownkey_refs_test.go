package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestPDFAKnownKeyReferences pins the PDF/A action and embedded-file presence
// tests on entries veraPDF reads with knownKey: its parser drops an entry written
// as null (ISO 32000-1, 7.3.7) but keeps a reference, so an /AA, /A, /T or /EF
// naming a null object is present. It also pins a catalog /AA whose actions
// the action-type rule still judges once the /AA itself is forbidden. The /C of
// ISO 19005-1, 6.5.3 follows the reference instead (PDAnnotation.getCOSC), so
// one naming a null object is absent. Every case was measured against veraPDF.
func TestPDFAKnownKeyReferences(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const (
		w  = "/Type /Annot /Subtype /Widget /Rect [10 10 100 30] /F 4 /AP << /N 6 0 R >>"
		js = "<< /S /JavaScript /JS (1) >>"
	)

	tests := []struct {
		name    string
		profile models.PDFAProfile
		cat     string // extra catalog entries
		page    string // extra page entries
		annots  string // the page /Annots entries
		fields  string // the /Fields entries
		o4, o5  string
		rule    Rule
		want    int
	}{
		{"a catalog /AA written as null", models.PDFA2b, " /AA null", "", "", "", "<< >>", "<< >>", Rule_19005_2_6_5_2_t01, 0},
		{"a catalog /AA written as null, PDF/A-1", models.PDFA1b, " /AA null", "", "", "", "<< >>", "<< >>", Rule_19005_1_6_6_2_t03, 0},
		{"a catalog /AA naming a null object", models.PDFA2b, " /AA 7 0 R", "", "", "", "<< >>", "<< >>", Rule_19005_2_6_5_2_t01, 1},
		{"a catalog /AA holding a JavaScript action", models.PDFA2b, " /AA << /WC " + js + " >>", "", "", "", "<< >>", "<< >>", Rule_19005_2_6_5_1_t01, 1},
		{"a catalog /AA holding a JavaScript action, PDF/A-1", models.PDFA1b, " /AA << /WC " + js + " >>", "", "", "", "<< >>", "<< >>", Rule_19005_1_6_6_1_t01, 1},
		{"a page /AA written as null", models.PDFA2b, "", " /AA null", "", "", "<< >>", "<< >>", Rule_19005_2_6_5_2_t02, 0},
		{"a page /AA naming a null object", models.PDFA2b, "", " /AA 7 0 R", "", "", "<< >>", "<< >>", Rule_19005_2_6_5_2_t02, 1},
		{"a widget /AA naming a null object, widget rule", models.PDFA2b, "", "", "4 0 R", "4 0 R", "<< " + w + " /FT /Tx /T (f) /AA 7 0 R >>", "<< >>", Rule_19005_2_6_4_1_t01, 1},
		{"a widget /AA naming a null object, field rule", models.PDFA2b, "", "", "4 0 R", "4 0 R", "<< " + w + " /FT /Tx /T (f) /AA 7 0 R >>", "<< >>", Rule_19005_2_6_4_1_t02, 1},
		{"a widget /A naming a null object", models.PDFA2b, "", "", "4 0 R", "4 0 R", "<< " + w + " /FT /Tx /T (f) /A 7 0 R >>", "<< >>", Rule_19005_2_6_4_1_t01, 1},
		{"a kid whose /T names a null object, field rule", models.PDFA2b, "", "", "5 0 R", "4 0 R", "<< /FT /Tx /T (p) /Kids [5 0 R] >>", "<< " + w + " /Parent 4 0 R /T 7 0 R /AA << /K " + js + " >> >>", Rule_19005_2_6_4_1_t02, 1},
		{"a kid whose /T names a null object, action rule", models.PDFA2b, "", "", "5 0 R", "4 0 R", "<< /FT /Tx /T (p) /Kids [5 0 R] >>", "<< " + w + " /Parent 4 0 R /T 7 0 R /AA << /K " + js + " >> >>", Rule_19005_2_6_5_1_t01, 1},
		{"a /C naming a null object", models.PDFA1b, "", "", "4 0 R", "", "<< /Type /Annot /Subtype /Square /Rect [10 10 60 60] /F 4 /C 7 0 R >>", "<< >>", Rule_19005_1_6_5_3_t03, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := objsDocPDF(t,
				"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields ["+tt.fields+"] >>"+tt.cat+" >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Annots ["+tt.annots+"]"+tt.page+" >>",
				tt.o4,
				tt.o5,
				"<< /Type /XObject /Subtype /Form /BBox [0 0 90 20] /Length 0 >>\nstream\n\nendstream",
				"null",
			)
			viols, err := ValidatePDFA(ctx, doc, tt.profile)
			if err != nil {
				t.Fatalf("ValidatePDFA: %v", err)
			}
			if got := countRuleErrs(t, viols, tt.rule); got != tt.want {
				t.Errorf("%s t%d fired %d times, want %d", tt.rule.Clause, tt.rule.TestNumber, got, tt.want)
			}
		})
	}
}

// TestEmbeddedFileKnownKeyReferences pins the file specification presence tests
// on /EF and /Desc, both knownKey reads in veraPDF (GFCosFileSpecification
// containsEF and containsDesc): an entry naming a null object is present. Every
// case was measured against veraPDF.
func TestEmbeddedFileKnownKeyReferences(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		name     string
		filespec string
		want     int
	}{
		{"an /EF naming a null object, PDF/A-2 6.8 t2", "/F (a.txt) /AFRelationship /Data /Desc (d) /EF 6 0 R", 1},
		{"an /EF naming a null object, PDF/UA-1 7.11 t1", "/F (a.txt) /AFRelationship /Data /Desc (d) /EF 6 0 R", 1},
		{"a /Desc naming a null object, PDF/UA-2 8.14.1 t1", "/F (a.txt) /UF (a.txt) /AFRelationship /Data /EF << /F 5 0 R >> /Desc 6 0 R", 0},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := objsDocPDF(t,
				"<< /Type /Catalog /Pages 2 0 R /Names << /EmbeddedFiles << /Names [(a.txt) 4 0 R] >> >> /MarkInfo << /Marked true >> /Lang (en) >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>",
				"<< /Type /Filespec "+tt.filespec+" >>",
				"<< /Type /EmbeddedFile /Length 5 >>\nstream\nhello\nendstream",
				"null",
			)
			var (
				viols []error
				err   error
				rule  Rule
			)
			switch i {
			case 0:
				viols, err = ValidatePDFA(ctx, doc, models.PDFA2b)
				rule = Rule_19005_2_6_8_t02
			case 1:
				viols, err = ValidatePDFUA(ctx, doc, models.PDFUA1)
				rule = Rule_14289_1_7_11_t01
			default:
				viols, err = ValidatePDFUA(ctx, doc, models.PDFUA2)
				rule = Rule_14289_2_8_14_1_t01
			}
			if err != nil {
				t.Fatalf("validate: %v", err)
			}
			if got := countRuleErrs(t, viols, rule); got != tt.want {
				t.Errorf("%s t%d fired %d times, want %d", rule.Clause, rule.TestNumber, got, tt.want)
			}
		})
	}
}

// TestUAKnownKeyReferences pins the PDF/UA presence tests veraPDF reads with
// knownKey on entries that name a null object: /RC (ISO 14289-2, 8.9.2.3 t2),
// /RV (8.10.3.3 t1), a widget /AA (8.10.2.3 t2), an optional content /AS
// (ISO 14289-1, 7.10 t2), and a widget /T, which makes a field widget (8.10.1
// t1). A /Parent naming a null object follows the reference instead and makes
// none. Every case was measured against veraPDF.
func TestUAKnownKeyReferences(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	form := func(t *testing.T, encl, widget string) []error {
		t.Helper()
		doc, _ := objsDocPDF(t,
			"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R /AcroForm << /Fields [7 0 R] >> /MarkInfo << /Marked true >> /Lang (en) >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Annots [7 0 R] >>",
			"<< /Type /StructTreeRoot /K 5 0 R /ParentTree << /Nums [0 6 0 R] >> /Namespaces [8 0 R] >>",
			"<< /S /Document /NS 8 0 R /P 4 0 R /K [6 0 R] >>",
			"<< /S /"+encl+" /NS 8 0 R /P 5 0 R /Pg 3 0 R /K [<< /Type /OBJR /Obj 7 0 R /Pg 3 0 R >>] >>",
			"<< /Type /Annot /Subtype /Widget /Rect [10 10 100 30] /F 4 /StructParent 0"+widget+" >>",
			"<< /Type /Namespace /NS (http://iso.org/pdf2/ssn) >>",
			"null",
		)
		viols, err := ValidatePDFUA(ctx, doc, models.PDFUA2)
		if err != nil {
			t.Fatalf("ValidatePDFUA: %v", err)
		}
		return viols
	}
	t.Run("an /RC naming a null object", func(t *testing.T) {
		t.Parallel()
		viols, err := ValidatePDFUA(ctx, annotPresenceDoc(t, "/Subtype /Square /RC 8 0 R /Contents (x)", ""), models.PDFUA2)
		if err != nil {
			t.Fatalf("ValidatePDFUA: %v", err)
		}
		if got := countRuleErrs(t, viols, Rule_14289_2_8_9_2_3_t02); got != 1 {
			t.Errorf("8.9.2.3 t2 fired %d times, want 1", got)
		}
	})
	t.Run("an /RV naming a null object", func(t *testing.T) {
		t.Parallel()
		viols := form(t, "Form", " /FT /Tx /T (f) /TU (field) /Contents (field) /RV 9 0 R /V (x)")
		if got := countRuleErrs(t, viols, Rule_14289_2_8_10_3_3_t01); got != 1 {
			t.Errorf("8.10.3.3 t1 fired %d times, want 1", got)
		}
	})
	t.Run("a widget /AA naming a null object", func(t *testing.T) {
		t.Parallel()
		viols := form(t, "Form", " /FT /Tx /T (f) /TU (field) /AA 9 0 R")
		if got := countRuleErrs(t, viols, Rule_14289_2_8_10_2_3_t02); got != 1 {
			t.Errorf("8.10.2.3 t2 fired %d times, want 1", got)
		}
	})
	t.Run("a widget /T naming a null object", func(t *testing.T) {
		t.Parallel()
		viols := form(t, "P", " /T 9 0 R")
		if got := countRuleErrs(t, viols, Rule_14289_2_8_10_1_t01); got != 1 {
			t.Errorf("8.10.1 t1 fired %d times, want 1", got)
		}
	})
	t.Run("a widget /Parent naming a null object", func(t *testing.T) {
		t.Parallel()
		viols := form(t, "P", " /Parent 9 0 R")
		if got := countRuleErrs(t, viols, Rule_14289_2_8_10_1_t01); got != 0 {
			t.Errorf("8.10.1 t1 fired %d times, want 0", got)
		}
	})
	t.Run("an optional content /AS naming a null object", func(t *testing.T) {
		t.Parallel()
		doc, _ := objsDocPDF(t,
			"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R /MarkInfo << /Marked true >> /Lang (en)"+
				" /OCProperties << /OCGs [6 0 R] /D << /Name (Default) /AS 7 0 R >> >> >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>",
			"<< /Type /StructTreeRoot /K 5 0 R >>",
			"<< /S /Document /P 4 0 R >>",
			"<< /Type /OCG /Name (Layer) >>",
			"null",
		)
		viols, err := ValidatePDFUA(ctx, doc, models.PDFUA1)
		if err != nil {
			t.Fatalf("ValidatePDFUA: %v", err)
		}
		if got := countRuleErrs(t, viols, Rule_14289_1_7_10_t02); got != 1 {
			t.Errorf("7.10 t2 fired %d times, want 1", got)
		}
	})
}

// TestStructureDestinationKnownKeyReferences pins ISO 14289-2, 8.8 on entries
// veraPDF reads with knownKey, each naming a null object: a GoTo /SD and a
// destination dictionary's /SD make structure destinations, as does an array
// whose first element has such an /S, while a Link or outline /Dest is a
// destination to judge, and a null one is no structure destination. Every case
// was measured against veraPDF.
func TestStructureDestinationKnownKeyReferences(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		name   string
		link   string
		cat    string
		t1, t2 int
	}{
		{"a GoTo /SD naming a null object", " /A << /S /GoTo /D [3 0 R /Fit] /SD 11 0 R >>", "", 0, 0},
		{"a destination dictionary /SD naming a null object", " /Dest (d1)", " /Names << /Dests << /Names [(d1) << /D [3 0 R /Fit] /SD 11 0 R >>] >> >>", 0, 0},
		{"an array whose first element's /S names a null object", " /Dest [12 0 R /Fit]", "", 0, 0},
		{"a Link /Dest naming a null object", " /Dest 11 0 R", "", 1, 0},
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
				"null",
				"<< /Type /StructElem /S 11 0 R /P 5 0 R >>",
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

	t.Run("an outline /Dest naming a null object", func(t *testing.T) {
		t.Parallel()
		doc, _ := objsDocPDF(t,
			"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R /MarkInfo << /Marked true >> /Lang (en) /Outlines 7 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>",
			"<< /Type /StructTreeRoot /K 5 0 R /Namespaces [6 0 R] >>",
			"<< /Type /StructElem /S /Document /NS 6 0 R /P 4 0 R >>",
			"<< /Type /Namespace /NS (http://iso.org/pdf2/ssn) >>",
			"<< /Type /Outlines /First 8 0 R /Last 8 0 R /Count 1 >>",
			"<< /Title (x) /Parent 7 0 R /Dest 9 0 R >>",
			"null",
		)
		viols, err := ValidatePDFUA(ctx, doc, models.PDFUA2)
		if err != nil {
			t.Fatalf("ValidatePDFUA: %v", err)
		}
		if got := countRuleErrs(t, viols, Rule_14289_2_8_8_t01); got != 1 {
			t.Errorf("8.8 t1 fired %d times, want 1", got)
		}
	})
}
