package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

func TestStructParentPresence(t *testing.T) {
	doc, _ := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
	)
	ctx := context.Background()

	for profile, rule := range map[string]Rule{
		"PDF/UA-1": Rule_14289_1_7_1_t12,
		"PDF/UA-2": Rule_14289_2_8_2_1_t02,
	} {
		var results []Violation
		state := &walkState{results: &results, profile: profile, doc: doc}
		walkElement(ctx, state, ast.PdfDict{"S": ast.PdfName("P")}, 0, 0)
		if !hasRule(results, rule) {
			t.Errorf("%s element without /P: got %v, want includes %v", profile, results, rule)
		}

		results = nil
		state = &walkState{results: &results, profile: profile, doc: doc}
		walkElement(ctx, state, ast.PdfDict{"S": ast.PdfName("P"), "P": ast.PdfName("x")}, 0, 0)
		if hasRule(results, rule) {
			t.Errorf("%s element with /P: got %v, want no %v", profile, results, rule)
		}
	}

	// PDF/A profiles must not emit the UA /P rules.
	var results []Violation
	state := &walkState{results: &results, profile: "PDF/A-2A", doc: doc}
	walkElement(ctx, state, ast.PdfDict{"S": ast.PdfName("P")}, 0, 0)
	if hasRule(results, Rule_14289_1_7_1_t12) || hasRule(results, Rule_14289_2_8_2_1_t02) {
		t.Errorf("PDF/A profile wrongly emitted a UA /P rule: %v", results)
	}
}

func TestStructuralChildTypeRuleIDs(t *testing.T) {
	emit := func(fn func(*walkState)) []Violation {
		var r []Violation
		fn(&walkState{results: &r, profile: "PDF/UA-1"})
		return r
	}
	ctx := context.Background()

	// Child-type rules (a structure element's permitted children).
	childCases := []struct {
		name string
		run  func(*walkState)
		want Rule
	}{
		{"L children", func(s *walkState) { validateListContainment(ctx, s, nil, "P", "L") }, Rule_14289_1_7_2_t19},
		{"LI children", func(s *walkState) { validateListContainment(ctx, s, nil, "P", "LI") }, Rule_14289_1_7_2_t20},
		{"THead children", func(s *walkState) { validateTableChildren(s, "P", "THead") }, Rule_14289_1_7_2_t36},
		{"TBody children", func(s *walkState) { validateTableChildren(s, "P", "TBody") }, Rule_14289_1_7_2_t37},
		{"TFoot children", func(s *walkState) { validateTableChildren(s, "P", "TFoot") }, Rule_14289_1_7_2_t38},
	}
	for _, c := range childCases {
		if got := emit(c.run); !hasRule(got, c.want) {
			t.Errorf("%s: got %v, want %+v", c.name, got, c.want)
		}
	}

	// Containment rules (a structure element's required parent).
	contCases := []struct {
		name     string
		typeName ast.PdfName
		rule     Rule
	}{
		{"TR", "TR", Rule_14289_1_7_2_t04},
		{"THead", "THead", Rule_14289_1_7_2_t05},
		{"TBody", "TBody", Rule_14289_1_7_2_t06},
		{"TFoot", "TFoot", Rule_14289_1_7_2_t07},
		{"TH", "TH", Rule_14289_1_7_2_t08},
		{"TD", "TD", Rule_14289_1_7_2_t09},
	}
	for _, c := range contCases {
		got := emit(func(s *walkState) { validateTableContainment(s, c.typeName, "P") })
		if !hasRule(got, c.rule) {
			t.Errorf("%s containment: got %v, want %+v", c.name, got, c.rule)
		}
	}
}

func TestLIDirectContentItem(t *testing.T) {
	doc, _ := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
	)
	ctx := context.Background()

	// LI whose /K is a bare MCID integer: direct content item.
	var results []Violation
	state := &walkState{results: &results, profile: "PDF/UA-2", doc: doc}
	walkElement(ctx, state, ast.PdfDict{"S": ast.PdfName("LI"), "P": ast.PdfName("l"), "K": ast.PdfInt(5)}, 0, 0)
	if !hasRule(results, Rule_14289_2_8_2_5_25_t02) {
		t.Errorf("LI with direct MCID: got %v, want includes §8.2.5.25 t2", results)
	}

	// LI whose only child is an LBody: conformant.
	results = nil
	state = &walkState{results: &results, profile: "PDF/UA-2", doc: doc}
	lbody := ast.PdfDict{"S": ast.PdfName("LBody"), "P": ast.PdfName("li")}
	walkElement(ctx, state, ast.PdfDict{"S": ast.PdfName("LI"), "P": ast.PdfName("l"), "K": lbody}, 0, 0)
	if hasRule(results, Rule_14289_2_8_2_5_25_t02) {
		t.Errorf("LI wrapping content in LBody: got %v, want no §8.2.5.25 t2", results)
	}
}

func TestFENoteNoteType(t *testing.T) {
	doc, _ := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
	)
	ctx := context.Background()

	mk := func(noteType string) []Violation {
		var results []Violation
		state := &walkState{results: &results, profile: "PDF/UA-2", doc: doc}
		// FENote is a standard type of the PDF 2.0 namespace only, and veraPDF
		// reads NoteType from FENote-owned attributes alone.
		elem := ast.PdfDict{
			"S": ast.PdfName("FENote"), "P": ast.PdfName("x"),
			"NS": ast.PdfDict{"NS": ast.PdfString("http://iso.org/pdf2/ssn")},
		}
		if noteType != "" {
			elem["A"] = ast.PdfDict{"O": ast.PdfName("FENote"), "NoteType": ast.PdfName(noteType)}
		}
		walkElement(ctx, state, elem, 0, 0)
		return results
	}

	if got := mk("Bogus"); !hasRule(got, Rule_14289_2_8_2_5_14_t04) {
		t.Errorf("FENote NoteType=Bogus: got %v, want includes §8.2.5.14 t4", got)
	}
	for _, valid := range []string{"Footnote", "Endnote", "None"} {
		if got := mk(valid); hasRule(got, Rule_14289_2_8_2_5_14_t04) {
			t.Errorf("FENote NoteType=%s: got %v, want no §8.2.5.14 t4", valid, got)
		}
	}
	// Absent NoteType is permissible (no FP).
	if got := mk(""); hasRule(got, Rule_14289_2_8_2_5_14_t04) {
		t.Errorf("FENote without NoteType: got %v, want no §8.2.5.14 t4", got)
	}
}
