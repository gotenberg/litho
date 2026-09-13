package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// annotAltDoc builds a document whose one Square annotation (object 7) sits in
// an Annot structure element (object 6). contents and alt are PDF string tokens,
// empty for absent; structParent links the annotation through the structure
// parent tree and objr through an OBJR kid of the element.
func annotAltDoc(t *testing.T, ua2 bool, contents, alt string, structParent, objr bool) *document.Document {
	t.Helper()
	ns, namespaces := "", ""
	if ua2 {
		ns, namespaces = " /NS 8 0 R", " /Namespaces [8 0 R]"
	}
	nums, sp := "[]", ""
	if structParent {
		nums, sp = "[0 6 0 R]", " /StructParent 0"
	}
	kids := "[]"
	if objr {
		kids = "[<< /Type /OBJR /Obj 7 0 R /Pg 3 0 R >>]"
	}
	altEntry, contentsEntry := "", ""
	if alt != "" {
		altEntry = " /Alt " + alt
	}
	if contents != "" {
		contentsEntry = " /Contents " + contents
	}
	doc, _ := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R /MarkInfo << /Marked true >> /Lang (en) >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Annots [7 0 R] >>",
		"<< /Type /StructTreeRoot /K 5 0 R /ParentTree << /Nums "+nums+" >>"+namespaces+" >>",
		"<< /S /Document"+ns+" /P 4 0 R /K [6 0 R] >>",
		"<< /S /Annot"+ns+" /P 5 0 R /Pg 3 0 R /K "+kids+altEntry+" >>",
		"<< /Type /Annot /Subtype /Square /Rect [10 10 60 60] /F 4"+sp+contentsEntry+" >>",
		"<< /Type /Namespace /NS (http://iso.org/pdf2/ssn) >>",
	)
	return doc
}

// countRuleErrs reports how many violations of rule a validation returned.
func countRuleErrs(t *testing.T, viols []error, rule Rule) int {
	t.Helper()
	n := 0
	for _, e := range viols {
		if v, ok := e.(Violation); ok && v.Rule == rule {
			n++
		}
	}
	return n
}

// TestAnnotAltFromStructParent pins how veraPDF reads an annotation's
// alternate text for ISO 14289-1, 7.18.1 t2 and ISO 14289-2, 8.9.4.2 t1. The
// /Alt is the one on the element the annotation's /StructParent names in the
// structure parent tree (GFPDAnnot.getAlt), whether or not an OBJR of that
// element references the annotation. /Contents and /Alt are compared as decoded
// text strings, untrimmed: a value of spaces is not empty, and one holding only
// a byte order mark is. Every case was measured against veraPDF.
func TestAnnotAltFromStructParent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		name          string
		ua2           bool
		contents, alt string
		structParent  bool
		objr          bool
		rule          Rule
		want          int
	}{
		{"UA-1: no /Contents and no /Alt", false, "", "", true, true, Rule_14289_1_7_18_1_t02, 1},
		{"UA-1: an /Alt reached through /StructParent alone", false, "", "(a square)", true, false, Rule_14289_1_7_18_1_t02, 0},
		{"UA-1: an /Alt reached through an OBJR alone", false, "", "(a square)", false, true, Rule_14289_1_7_18_1_t02, 1},
		{"UA-1: an /Alt reached both ways", false, "", "(a square)", true, true, Rule_14289_1_7_18_1_t02, 0},
		{"UA-1: a /Contents of spaces", false, "(   )", "", true, true, Rule_14289_1_7_18_1_t02, 0},
		{"UA-1: an /Alt of spaces", false, "", "(   )", true, true, Rule_14289_1_7_18_1_t02, 0},
		{"UA-1: a /Contents of text", false, "(a square)", "", true, true, Rule_14289_1_7_18_1_t02, 0},
		{"UA-1: a /Contents holding only a byte order mark", false, "<FEFF>", "", true, true, Rule_14289_1_7_18_1_t02, 1},

		{"UA-2: a differing /Alt reached through /StructParent alone", true, "(x)", "(y)", true, false, Rule_14289_2_8_9_4_2_t01, 1},
		{"UA-2: a differing /Alt reached through an OBJR alone", true, "(x)", "(y)", false, true, Rule_14289_2_8_9_4_2_t01, 0},
		{"UA-2: an equal /Alt reached both ways", true, "(x)", "(x)", true, true, Rule_14289_2_8_9_4_2_t01, 0},
		{"UA-2: a differing /Alt reached both ways", true, "(x)", "(y)", true, true, Rule_14289_2_8_9_4_2_t01, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			profile := models.PDFUA1
			if tt.ua2 {
				profile = models.PDFUA2
			}
			viols, err := ValidatePDFUA(ctx, annotAltDoc(t, tt.ua2, tt.contents, tt.alt, tt.structParent, tt.objr), profile)
			if err != nil {
				t.Fatalf("ValidatePDFUA: %v", err)
			}
			if got := countRuleErrs(t, viols, tt.rule); got != tt.want {
				t.Errorf("%s t%d fired %d times, want %d", tt.rule.Clause, tt.rule.TestNumber, got, tt.want)
			}
		})
	}
}

// TestFigureAltDecodedUntrimmed pins ISO 14289-1, 7.3 t1 on the decoded /Alt,
// untrimmed, as veraPDF compares it with the empty string: an /Alt of spaces
// passes and one holding only a byte order mark fails. Measured against veraPDF.
func TestFigureAltDecodedUntrimmed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tests := []struct {
		name string
		alt  string
		want int
	}{
		{"an /Alt of spaces", "(   )", 0},
		{"an /Alt holding only a byte order mark", "<FEFF>", 1},
		{"an /Alt of text", "(a chart)", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc := buildCapDoc(t, capNode{"Figure /Alt " + tt.alt, nil}, "<< >>")
			viols, err := ValidatePDFUA(ctx, doc, models.PDFUA1)
			if err != nil {
				t.Fatalf("ValidatePDFUA: %v", err)
			}
			if got := countRuleErrs(t, viols, Rule_14289_1_7_3_t01); got != tt.want {
				t.Errorf("7.3 t1 fired %d times, want %d", got, tt.want)
			}
		})
	}
}
