package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/models"
)

func TestMatchListContent(t *testing.T) {
	t.Parallel()
	if !matchListContent(compliance.Violation{Rule: compliance.Rule_14289_2_8_2_5_25_t02}) {
		t.Error("must match ISO 14289-2 8.2.5.25 t2")
	}
	// t1 states ListNumbering on the L element, a different defect.
	if matchListContent(compliance.Violation{Rule: compliance.Rule_14289_2_8_2_5_25_t01}) {
		t.Error("must not match 8.2.5.25 t1")
	}
	// The PDF/UA-1 list rule belongs to fixListBody, which moves structure
	// elements. Claiming it here would match violations this fixer declines.
	if matchListContent(compliance.Violation{Rule: compliance.Rule_14289_1_7_2_t20}) {
		t.Error("must not match ISO 14289-1 7.2 t20")
	}
}

// TestMatchListContent_LiveValidator pins the matcher to the validator's real
// emission. A matcher built on a fabricated rule constant stays green while the
// clause table moves underneath it, which is how the list-body fixer was once
// left permanently dead.
func TestMatchListContent_LiveValidator(t *testing.T) {
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /StructParents 0 >>",
		"<< /Type /StructTreeRoot /K 5 0 R /ParentTree << /Nums [0 [6 0 R]] >> >>",
		"<< /Type /StructElem /S /L /P 4 0 R /K [6 0 R] >>",
		"<< /Type /StructElem /S /LI /P 5 0 R /Pg 3 0 R /K [0] >>",
	)
	doc, _ := newTestDoc(t, pdf)

	violations, err := compliance.ValidatePDFUA(context.Background(), doc, models.PDFUA2)
	if err != nil {
		t.Fatalf("ValidatePDFUA: %v", err)
	}
	for _, e := range violations {
		if v, ok := e.(compliance.Violation); ok && matchListContent(v) {
			return
		}
	}
	t.Fatalf("no live violation matched matchListContent; fixer is dead. Emissions: %v", violations)
}

func TestFixListContent_WrapsContentItem(t *testing.T) {
	// LI /K = [0], a bare MCID. It must move into a new LBody and the page's
	// ParentTree slot must follow it.
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /StructParents 0 >>",
		"<< /Type /StructTreeRoot /K 5 0 R /ParentTree << /Nums [0 [6 0 R]] >> >>",
		"<< /Type /StructElem /S /L /P 4 0 R /K [6 0 R] >>",
		"<< /Type /StructElem /S /LI /P 5 0 R /Pg 3 0 R /K [0] >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixListContent(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-2")
	if err != nil {
		t.Fatalf("fixListContent: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 description, got %v", descs)
	}

	liK, ok := pendingDict(t, alloc, 6)["K"].(ast.PdfArray)
	if !ok || len(liK) != 1 {
		t.Fatalf("LI /K: got %v, want [LBody]", pendingDict(t, alloc, 6)["K"])
	}
	lbodyRef, ok := liK[0].(ast.PdfIndirectRef)
	if !ok {
		t.Fatalf("LI /K[0]: got %T, want an LBody indirect ref", liK[0])
	}

	lbody := pendingDict(t, alloc, lbodyRef.ObjectNum)
	if lbody["S"] != ast.PdfName("LBody") {
		t.Errorf("LBody /S: got %v, want LBody", lbody["S"])
	}
	if lbody["P"] != (ast.PdfIndirectRef{ObjectNum: 6}) {
		t.Errorf("LBody /P: got %v, want LI 6 0 R", lbody["P"])
	}
	if lbody["Pg"] != (ast.PdfIndirectRef{ObjectNum: 3}) {
		t.Errorf("LBody /Pg: got %v, want page 3 0 R", lbody["Pg"])
	}
	if _, hasNS := lbody["NS"]; !hasNS {
		t.Error("LBody /NS: want the PDF 2.0 namespace under PDF/UA-2")
	}
	lbodyK, ok := lbody["K"].(ast.PdfArray)
	if !ok || len(lbodyK) != 1 || lbodyK[0] != ast.PdfInt(0) {
		t.Fatalf("LBody /K: got %v, want [0]", lbody["K"])
	}

	// ISO 32000-2 14.7.4.4: the MCID slot must now resolve to the LBody.
	st := pendingDict(t, alloc, 4)
	pt, ok := st["ParentTree"].(ast.PdfDict)
	if !ok {
		t.Fatalf("StructTreeRoot /ParentTree: got %T, want a dictionary", st["ParentTree"])
	}
	nums, ok := pt["Nums"].(ast.PdfArray)
	if !ok || len(nums) != 2 {
		t.Fatalf("/Nums: got %v, want one key/value pair", pt["Nums"])
	}
	slots, ok := nums[1].(ast.PdfArray)
	if !ok || len(slots) != 1 {
		t.Fatalf("/Nums value: got %v, want a one-slot array", nums[1])
	}
	if slots[0] != lbodyRef {
		t.Errorf("ParentTree slot 0: got %v, want the LBody %v", slots[0], lbodyRef)
	}
}

func TestFixListContent_SingleContentItemK(t *testing.T) {
	// ISO 32000-1 14.7.2: /K may be a single content item instead of an
	// array. Most real list items carrying this violation use the bare
	// integer form, which resolveStructKids reports as no kids at all, so a
	// fixer that only reads the array form looks healthy in unit tests and
	// repairs almost nothing in the corpus.
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /StructParents 0 >>",
		"<< /Type /StructTreeRoot /K 5 0 R /ParentTree << /Nums [0 [6 0 R]] >> >>",
		"<< /Type /StructElem /S /L /P 4 0 R /K [6 0 R] >>",
		"<< /Type /StructElem /S /LI /P 5 0 R /Pg 3 0 R /K 0 >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixListContent(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-2")
	if err != nil {
		t.Fatalf("fixListContent: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 description, got %v", descs)
	}

	liK, ok := pendingDict(t, alloc, 6)["K"].(ast.PdfArray)
	if !ok || len(liK) != 1 {
		t.Fatalf("LI /K: got %v, want [LBody]", pendingDict(t, alloc, 6)["K"])
	}
	lbodyRef, ok := liK[0].(ast.PdfIndirectRef)
	if !ok {
		t.Fatalf("LI /K[0]: got %T, want an LBody indirect ref", liK[0])
	}
	lbodyK, ok := pendingDict(t, alloc, lbodyRef.ObjectNum)["K"].(ast.PdfArray)
	if !ok || len(lbodyK) != 1 || lbodyK[0] != ast.PdfInt(0) {
		t.Fatalf("LBody /K: got %v, want [0]", pendingDict(t, alloc, lbodyRef.ObjectNum)["K"])
	}

	st := pendingDict(t, alloc, 4)
	pt, ok := st["ParentTree"].(ast.PdfDict)
	if !ok {
		t.Fatalf("StructTreeRoot /ParentTree: got %T, want a dictionary", st["ParentTree"])
	}
	nums, ok := pt["Nums"].(ast.PdfArray)
	if !ok || len(nums) != 2 {
		t.Fatalf("/Nums: got %v, want one key/value pair", pt["Nums"])
	}
	slots, ok := nums[1].(ast.PdfArray)
	if !ok || len(slots) != 1 || slots[0] != lbodyRef {
		t.Errorf("ParentTree slot 0: got %v, want the LBody %v", nums[1], lbodyRef)
	}
}

func TestFixListContent_IndirectParentTreeArray(t *testing.T) {
	// The page's ParentTree entry is an indirect array. It must be rewritten
	// at its own object number so any sharing of that array survives.
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /StructParents 0 >>",
		"<< /Type /StructTreeRoot /K 5 0 R /ParentTree 7 0 R >>",
		"<< /Type /StructElem /S /L /P 4 0 R /K [6 0 R] >>",
		"<< /Type /StructElem /S /LI /P 5 0 R /Pg 3 0 R /K [0] >>",
		"<< /Nums [0 8 0 R] >>",
		"[6 0 R]",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixListContent(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-2")
	if err != nil {
		t.Fatalf("fixListContent: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 description, got %v", descs)
	}

	liK, ok := pendingDict(t, alloc, 6)["K"].(ast.PdfArray)
	if !ok || len(liK) != 1 {
		t.Fatalf("LI /K: got %v, want [LBody]", pendingDict(t, alloc, 6)["K"])
	}
	lbodyRef, ok := liK[0].(ast.PdfIndirectRef)
	if !ok {
		t.Fatalf("LI /K[0]: got %T, want an LBody indirect ref", liK[0])
	}

	slots, ok := alloc.Pending()[8].(ast.PdfArray)
	if !ok {
		t.Fatalf("object 8: got %T, want the rewritten ParentTree array", alloc.Pending()[8])
	}
	if len(slots) != 1 || slots[0] != lbodyRef {
		t.Errorf("ParentTree slot 0: got %v, want the LBody %v", slots, lbodyRef)
	}
}

func TestFixListContent_PreservesLbl(t *testing.T) {
	// /K = [Lbl, 0]. The Lbl stays in place and the LBody takes the content
	// item's slot, so reading order is unchanged.
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /StructParents 0 >>",
		"<< /Type /StructTreeRoot /K 5 0 R /ParentTree << /Nums [0 [6 0 R]] >> >>",
		"<< /Type /StructElem /S /L /P 4 0 R /K [6 0 R] >>",
		"<< /Type /StructElem /S /LI /P 5 0 R /Pg 3 0 R /K [7 0 R 0] >>",
		"<< /Type /StructElem /S /Lbl /P 6 0 R >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixListContent(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-2")
	if err != nil {
		t.Fatalf("fixListContent: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 description, got %v", descs)
	}
	liK, ok := pendingDict(t, alloc, 6)["K"].(ast.PdfArray)
	if !ok || len(liK) != 2 {
		t.Fatalf("LI /K: got %v, want [Lbl LBody]", pendingDict(t, alloc, 6)["K"])
	}
	if liK[0] != (ast.PdfIndirectRef{ObjectNum: 7}) {
		t.Errorf("LI /K[0]: got %v, want Lbl 7 0 R", liK[0])
	}
	if _, ok := liK[1].(ast.PdfIndirectRef); !ok {
		t.Errorf("LI /K[1]: got %T, want an LBody indirect ref", liK[1])
	}
}

func TestFixListContent_Declines(t *testing.T) {
	stRoot := "<< /Type /StructTreeRoot /K 5 0 R /ParentTree << /Nums [0 [6 0 R]] >> >>"
	list := "<< /Type /StructElem /S /L /P 4 0 R /K [6 0 R] >>"
	tests := []struct {
		name   string
		bodies []string
	}{
		{
			// An LI that already owns an LBody: a second one is not a shape
			// any producer writes, and merging reorders content.
			name: "existing LBody",
			bodies: []string{stRoot, list,
				"<< /Type /StructElem /S /LI /P 5 0 R /Pg 3 0 R /K [0 7 0 R] >>",
				"<< /Type /StructElem /S /LBody /P 6 0 R >>"},
		},
		{
			// An /MCR may name its own /Pg, so the page whose ParentTree slot
			// needs rewriting is not the LI's.
			name: "MCR content item",
			bodies: []string{stRoot, list,
				"<< /Type /StructElem /S /LI /P 5 0 R /Pg 3 0 R /K [<< /Type /MCR /Pg 3 0 R /MCID 0 >>] >>"},
		},
		{
			// Without a /Pg there is no page whose ParentTree array indexes
			// the MCID.
			name: "no /Pg",
			bodies: []string{stRoot, list,
				"<< /Type /StructElem /S /LI /P 5 0 R /K [0] >>"},
		},
		{
			// Table 5 looks through Div, so the LI is flagged while its
			// content sits deeper than the /K this fixer rebuilds.
			name: "pass-through Div child",
			bodies: []string{stRoot, list,
				"<< /Type /StructElem /S /LI /P 5 0 R /Pg 3 0 R /K [0 7 0 R] >>",
				"<< /Type /StructElem /S /Div /P 6 0 R >>"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bodies := append([]string{
				"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /StructParents 0 >>",
			}, tc.bodies...)
			doc, alloc := newTestDoc(t, buildStructPDF(bodies...))

			descs, err := fixListContent(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-2")
			if err != nil {
				t.Fatalf("fixListContent: %v", err)
			}
			if len(descs) != 0 {
				t.Fatalf("expected no repair, got %v", descs)
			}
		})
	}
}
