package compliance

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// objsDocPDF assembles a PDF from explicit indirect objects (objs[0] becomes
// object 1, the catalog) with a correct xref table, parses it, and returns the
// document and its catalog dict.
func objsDocPDF(t *testing.T, objs ...string) (*document.Document, ast.PdfDict) {
	t.Helper()

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)

	data := buf.Bytes()
	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	cat, err := document.ResolveCatalog(context.Background(), doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	return doc, cat.Dict
}

func hasRule(vs []Violation, r Rule) bool {
	for _, v := range vs {
		if v.Rule == r {
			return true
		}
	}
	return false
}

func TestPresStepsForbidden(t *testing.T) {
	ctx := context.Background()
	doc, cat := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /PresSteps << /Type /NavNode >> >>",
	)
	for part, want := range map[int]Rule{
		2: Rule_19005_2_6_10_t02,
		3: Rule_19005_3_6_10_t02,
		4: Rule_19005_4_6_11_t02,
	} {
		got := checkPresSteps(ctx, doc, cat, part)
		if len(got) != 1 || got[0].Rule != want {
			t.Errorf("part %d page /PresSteps: got %v, want one %v", part, got, want)
		}
	}

	docOK, catOK := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
	)
	if got := checkPresSteps(ctx, docOK, catOK, 2); len(got) != 0 {
		t.Errorf("clean page: got %v, want none", got)
	}
}

func TestFormFieldAAForbidden(t *testing.T) {
	ctx := context.Background()
	// A form field carrying an (empty) /AA entry. ISO 19005-1 §6.6.2 and
	// ISO 19005-2/3 §6.4.1 forbid the field additional-actions dictionary
	// itself, so its mere presence is the violation independent of whether it
	// holds any actions.
	doc, cat := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /FT /Tx /T (f1) /AA << >> >>",
	)
	for part, want := range map[int]Rule{
		1: Rule_19005_1_6_6_2_t02,
		2: Rule_19005_2_6_4_1_t02,
		3: Rule_19005_3_6_4_1_t02,
	} {
		got := checkFormFieldActions(ctx, doc, cat, "", part)
		if !hasRule(got, want) {
			t.Errorf("part %d field /AA: got %v, want includes %v", part, got, want)
		}
	}

	// PDF/A-4 has no blanket field-/AA prohibition (§6.6.3 permits a key set).
	got4 := checkFormFieldActions(ctx, doc, cat, "", 4)
	for _, r := range []Rule{Rule_19005_1_6_6_2_t02, Rule_19005_2_6_4_1_t02, Rule_19005_3_6_4_1_t02} {
		if hasRule(got4, r) {
			t.Errorf("part 4 wrongly emitted field-/AA rule %v: %v", r, got4)
		}
	}

	// A field without /AA is clean.
	docOK, catOK := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /FT /Tx /T (f1) >>",
	)
	if got := checkFormFieldActions(ctx, docOK, catOK, "", 2); hasRule(got, Rule_19005_2_6_4_1_t02) {
		t.Errorf("field without /AA wrongly flagged: %v", got)
	}
}

func TestDestOutputProfileRefForbidden(t *testing.T) {
	ctx := context.Background()
	mk := func(subtype string, withRef bool) (*document.Document, ast.PdfDict) {
		ref := ""
		if withRef {
			ref = " /DestOutputProfileRef << /CheckSum (x) >>"
		}
		return objsDocPDF(t,
			"<< /Type /Catalog /Pages 2 0 R /OutputIntents [4 0 R] >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
			fmt.Sprintf("<< /Type /OutputIntent /S /%s%s >>", subtype, ref),
		)
	}

	// PDF/X intent carrying DestOutputProfileRef: flagged for parts 2 and 3.
	for part, want := range map[int]Rule{2: Rule_19005_2_6_2_3_t03, 3: Rule_19005_3_6_2_3_t03} {
		doc, cat := mk("GTS_PDFX", true)
		var results []Violation
		checkOutputIntentICC(ctx, doc, cat, part, &results)
		if !hasRule(results, want) {
			t.Errorf("part %d GTS_PDFX /DestOutputProfileRef: got %v, want includes %v", part, results, want)
		}
	}

	// Non-PDF/X intent with DestOutputProfileRef: not flagged (subtype guard).
	{
		doc, cat := mk("GTS_PDFA1", true)
		var results []Violation
		checkOutputIntentICC(ctx, doc, cat, 2, &results)
		if hasRule(results, Rule_19005_2_6_2_3_t03) {
			t.Errorf("non-PDF/X intent wrongly flagged: %v", results)
		}
	}

	// PDF/X intent without DestOutputProfileRef: not flagged.
	{
		doc, cat := mk("GTS_PDFX", false)
		var results []Violation
		checkOutputIntentICC(ctx, doc, cat, 2, &results)
		if hasRule(results, Rule_19005_2_6_2_3_t03) {
			t.Errorf("PDF/X intent without ref wrongly flagged: %v", results)
		}
	}
}
