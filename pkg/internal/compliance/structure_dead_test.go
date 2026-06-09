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
		walkElement(ctx, state, ast.PdfDict{"S": ast.PdfName("P")}, "", 0, 0)
		if !hasRule(results, rule) {
			t.Errorf("%s element without /P: got %v, want includes %v", profile, results, rule)
		}

		results = nil
		state = &walkState{results: &results, profile: profile, doc: doc}
		walkElement(ctx, state, ast.PdfDict{"S": ast.PdfName("P"), "P": ast.PdfName("x")}, "", 0, 0)
		if hasRule(results, rule) {
			t.Errorf("%s element with /P: got %v, want no %v", profile, results, rule)
		}
	}

	// PDF/A profiles must not emit the UA /P rules.
	var results []Violation
	state := &walkState{results: &results, profile: "PDF/A-2A", doc: doc}
	walkElement(ctx, state, ast.PdfDict{"S": ast.PdfName("P")}, "", 0, 0)
	if hasRule(results, Rule_14289_1_7_1_t12) || hasRule(results, Rule_14289_2_8_2_1_t02) {
		t.Errorf("PDF/A profile wrongly emitted a UA /P rule: %v", results)
	}
}

func TestCaptionPositionProfileSemantics(t *testing.T) {
	doc, _ := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
	)
	caption := ast.PdfDict{"S": ast.PdfName("Caption")}
	run := func(profile string, parent ast.PdfName, idx, total int) []Violation {
		var r []Violation
		validateCaptionPosition(&walkState{results: &r, profile: profile, doc: doc}, caption, parent, idx, total)
		return r
	}

	// PDF/UA-2 allows a Caption as first OR last child (§8.2.5.27). A Caption
	// as the last child of an L must NOT be rejected (the pre-existing FP).
	if r := run("PDF/UA-2", "L", 1, 2); len(r) != 0 {
		t.Errorf("UA-2 Caption last in L wrongly flagged: %v", r)
	}
	// A middle Caption under PDF/UA-2 fails §8.2.5.27 t1, for any parent.
	if r := run("PDF/UA-2", "L", 1, 3); !hasRule(r, Rule_14289_2_8_2_5_27_t01) {
		t.Errorf("UA-2 middle Caption in L: got %v, want §8.2.5.27 t1", r)
	}
	if r := run("PDF/UA-2", "Table", 1, 3); !hasRule(r, Rule_14289_2_8_2_5_27_t01) {
		t.Errorf("UA-2 middle Caption in Table: got %v, want §8.2.5.27 t1", r)
	}
	if r := run("PDF/UA-2", "Table", 0, 3); len(r) != 0 {
		t.Errorf("UA-2 first Caption wrongly flagged: %v", r)
	}

	// PDF/UA-1 keeps first-only semantics for L (§7.2 t40).
	if r := run("PDF/UA-1", "L", 1, 2); !hasRule(r, Rule_14289_1_7_2_t40) {
		t.Errorf("UA-1 Caption last in L: got %v, want §7.2 t40 (first-only)", r)
	}
	if r := run("PDF/UA-1", "L", 0, 2); len(r) != 0 {
		t.Errorf("UA-1 Caption first in L wrongly flagged: %v", r)
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
	walkElement(ctx, state, ast.PdfDict{"S": ast.PdfName("LI"), "P": ast.PdfName("l"), "K": ast.PdfInt(5)}, "L", 0, 0)
	if !hasRule(results, Rule_14289_2_8_2_5_25_t02) {
		t.Errorf("LI with direct MCID: got %v, want includes §8.2.5.25 t2", results)
	}

	// LI whose only child is an LBody: conformant.
	results = nil
	state = &walkState{results: &results, profile: "PDF/UA-2", doc: doc}
	lbody := ast.PdfDict{"S": ast.PdfName("LBody"), "P": ast.PdfName("li")}
	walkElement(ctx, state, ast.PdfDict{"S": ast.PdfName("LI"), "P": ast.PdfName("l"), "K": lbody}, "L", 0, 0)
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
		elem := ast.PdfDict{"S": ast.PdfName("FENote"), "P": ast.PdfName("x")}
		if noteType != "" {
			elem["A"] = ast.PdfDict{"NoteType": ast.PdfName(noteType)}
		}
		walkElement(ctx, state, elem, "", 0, 0)
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
