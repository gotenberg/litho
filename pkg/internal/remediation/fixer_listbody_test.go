package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

func TestMatchListBody(t *testing.T) {
	t.Parallel()
	// §7.2 t10 is emitted for both the LI ("LI contains ...") and the table cell
	// ("TR contains ...") with the same rule, so the matcher disambiguates on the
	// violation context and claims only the list side.
	li := compliance.Violation{Rule: compliance.Rule_14289_1_7_2_t10, Context: "LI contains P instead of Lbl/LBody"}
	if !matchListBody(li) {
		t.Error("must match the LI-side §7.2 t10 violation")
	}
	tr := compliance.Violation{Rule: compliance.Rule_14289_1_7_2_t10, Context: "TR contains Span instead of TH/TD"}
	if matchListBody(tr) {
		t.Error("must not match the TR-side §7.2 t10 violation")
	}
	other := compliance.Violation{Rule: compliance.Rule_14289_1_7_2_t13, Context: "LI contains P instead of Lbl/LBody"}
	if matchListBody(other) {
		t.Error("must not match a different §7.2 test number")
	}
}

func TestFixListBody_WrapsOrphan(t *testing.T) {
	// LI /K = [Lbl, P] with the P directly under the LI. The P must move into a
	// new LBody, with /P pointers and the LI /K rebuilt.
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K 5 0 R >>",
		"<< /Type /StructElem /S /L /P 4 0 R /K [6 0 R] >>",
		"<< /Type /StructElem /S /LI /P 5 0 R /K [7 0 R 8 0 R] >>",
		"<< /Type /StructElem /S /Lbl /P 6 0 R >>",
		"<< /Type /StructElem /S /P /P 6 0 R >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixListBody(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixListBody: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 description, got %v", descs)
	}

	// LI /K is now [Lbl, LBody].
	liK, ok := pendingDict(t, alloc, 6)["K"].(ast.PdfArray)
	if !ok || len(liK) != 2 {
		t.Fatalf("LI /K: got %v, want [Lbl LBody]", pendingDict(t, alloc, 6)["K"])
	}
	if liK[0] != (ast.PdfIndirectRef{ObjectNum: 7}) {
		t.Errorf("LI /K[0]: got %v, want Lbl 7 0 R", liK[0])
	}
	lbodyRef, ok := liK[1].(ast.PdfIndirectRef)
	if !ok {
		t.Fatalf("LI /K[1]: got %T, want an LBody indirect ref", liK[1])
	}

	lbody := pendingDict(t, alloc, lbodyRef.ObjectNum)
	if lbody["S"] != ast.PdfName("LBody") {
		t.Errorf("LBody /S: got %v, want LBody", lbody["S"])
	}
	if lbody["P"] != (ast.PdfIndirectRef{ObjectNum: 6}) {
		t.Errorf("LBody /P: got %v, want LI 6 0 R", lbody["P"])
	}
	if lbodyK, ok := lbody["K"].(ast.PdfArray); !ok || len(lbodyK) != 1 || lbodyK[0] != (ast.PdfIndirectRef{ObjectNum: 8}) {
		t.Fatalf("LBody /K: got %v, want [P 8 0 R]", lbody["K"])
	}
	if pendingDict(t, alloc, 8)["P"] != lbodyRef {
		t.Errorf("P /P: got %v, want LBody %v", pendingDict(t, alloc, 8)["P"], lbodyRef)
	}
}

func TestFixListBody_BareOrphan(t *testing.T) {
	// An LI with only orphan content and no Lbl: /K = [P] -> [LBody[P]].
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K 5 0 R >>",
		"<< /Type /StructElem /S /L /P 4 0 R /K [6 0 R] >>",
		"<< /Type /StructElem /S /LI /P 5 0 R /K [7 0 R] >>",
		"<< /Type /StructElem /S /P /P 6 0 R >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixListBody(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixListBody: %v", err)
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
	if lbodyK, ok := pendingDict(t, alloc, lbodyRef.ObjectNum)["K"].(ast.PdfArray); !ok || len(lbodyK) != 1 || lbodyK[0] != (ast.PdfIndirectRef{ObjectNum: 7}) {
		t.Fatalf("LBody /K: got %v, want [P 7 0 R]", pendingDict(t, alloc, lbodyRef.ObjectNum)["K"])
	}
}

func TestFixListBody_NonContiguousOrphans(t *testing.T) {
	// Orphans straddle the Lbl: /K = [P, Lbl, P]. Both Ps must move into the LBody
	// even though they are not adjacent, the LBody takes the first orphan's slot,
	// and the Lbl is preserved.
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K 5 0 R >>",
		"<< /Type /StructElem /S /L /P 4 0 R /K [6 0 R] >>",
		"<< /Type /StructElem /S /LI /P 5 0 R /K [7 0 R 8 0 R 9 0 R] >>",
		"<< /Type /StructElem /S /P /P 6 0 R >>",
		"<< /Type /StructElem /S /Lbl /P 6 0 R >>",
		"<< /Type /StructElem /S /P /P 6 0 R >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixListBody(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixListBody: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 description, got %v", descs)
	}

	// LI /K is [LBody, Lbl]: both Ps gone, Lbl kept after the LBody.
	liK, ok := pendingDict(t, alloc, 6)["K"].(ast.PdfArray)
	if !ok || len(liK) != 2 {
		t.Fatalf("LI /K: got %v, want [LBody Lbl]", pendingDict(t, alloc, 6)["K"])
	}
	if liK[1] != (ast.PdfIndirectRef{ObjectNum: 8}) {
		t.Errorf("LI /K[1]: got %v, want Lbl 8 0 R", liK[1])
	}
	lbodyRef, ok := liK[0].(ast.PdfIndirectRef)
	if !ok {
		t.Fatalf("LI /K[0]: got %T, want an LBody indirect ref", liK[0])
	}
	lbodyK, ok := pendingDict(t, alloc, lbodyRef.ObjectNum)["K"].(ast.PdfArray)
	if !ok || len(lbodyK) != 2 {
		t.Fatalf("LBody /K: got %v, want [P@7 P@9]", pendingDict(t, alloc, lbodyRef.ObjectNum)["K"])
	}
	if lbodyK[0] != (ast.PdfIndirectRef{ObjectNum: 7}) || lbodyK[1] != (ast.PdfIndirectRef{ObjectNum: 9}) {
		t.Errorf("LBody /K: got %v, want [7 0 R 9 0 R]", lbodyK)
	}
	for _, n := range []int{7, 9} {
		if pendingDict(t, alloc, n)["P"] != lbodyRef {
			t.Errorf("P %d /P: got %v, want LBody %v", n, pendingDict(t, alloc, n)["P"], lbodyRef)
		}
	}
}

func TestFixListBody_SingleDictKid(t *testing.T) {
	// /K may be a single element rather than an array (ISO 32000-1 §14.7.2). An LI
	// with /K = a lone orphan reference is flagged by the validator and must be
	// repaired just like the array form.
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K 5 0 R >>",
		"<< /Type /StructElem /S /L /P 4 0 R /K [6 0 R] >>",
		"<< /Type /StructElem /S /LI /P 5 0 R /K 7 0 R >>",
		"<< /Type /StructElem /S /P /P 6 0 R >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixListBody(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixListBody: %v", err)
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
	if lbodyK, ok := pendingDict(t, alloc, lbodyRef.ObjectNum)["K"].(ast.PdfArray); !ok || len(lbodyK) != 1 || lbodyK[0] != (ast.PdfIndirectRef{ObjectNum: 7}) {
		t.Fatalf("LBody /K: got %v, want [P 7 0 R]", pendingDict(t, alloc, lbodyRef.ObjectNum)["K"])
	}
	if pendingDict(t, alloc, 7)["P"] != lbodyRef {
		t.Errorf("P /P: got %v, want LBody %v", pendingDict(t, alloc, 7)["P"], lbodyRef)
	}
}

func TestFixListBody_NoopWhenCorrect(t *testing.T) {
	// LI /K = [Lbl, LBody] already conforms.
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K 5 0 R >>",
		"<< /Type /StructElem /S /L /P 4 0 R /K [6 0 R] >>",
		"<< /Type /StructElem /S /LI /P 5 0 R /K [7 0 R 8 0 R] >>",
		"<< /Type /StructElem /S /Lbl /P 6 0 R >>",
		"<< /Type /StructElem /S /LBody /P 6 0 R >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixListBody(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixListBody: %v", err)
	}
	if len(descs) != 0 || len(alloc.Pending()) != 0 {
		t.Errorf("a conformant LI needs no fix: descs=%v pending=%d", descs, len(alloc.Pending()))
	}
}

func TestFixListBody_SkipsIfLBodyPresent(t *testing.T) {
	// An LI that already owns an LBody but also has an orphan P is left untouched:
	// the fixer does not graft a second LBody or merge into the existing one.
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K 5 0 R >>",
		"<< /Type /StructElem /S /L /P 4 0 R /K [6 0 R] >>",
		"<< /Type /StructElem /S /LI /P 5 0 R /K [7 0 R 8 0 R 9 0 R] >>",
		"<< /Type /StructElem /S /Lbl /P 6 0 R >>",
		"<< /Type /StructElem /S /LBody /P 6 0 R >>",
		"<< /Type /StructElem /S /P /P 6 0 R >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixListBody(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixListBody: %v", err)
	}
	if len(descs) != 0 || len(alloc.Pending()) != 0 {
		t.Errorf("an LI with an existing LBody must be left untouched: descs=%v pending=%d", descs, len(alloc.Pending()))
	}
}

func TestFixListBody_SkipsInlineOrphan(t *testing.T) {
	// An LI whose orphan is an inline dict (not an indirect ref) must be left
	// untouched: the rebuild only re-parents indirect children.
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K 5 0 R >>",
		"<< /Type /StructElem /S /L /P 4 0 R /K [6 0 R] >>",
		"<< /Type /StructElem /S /LI /P 5 0 R /K [7 0 R << /Type /StructElem /S /P >>] >>",
		"<< /Type /StructElem /S /Lbl /P 6 0 R >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixListBody(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixListBody: %v", err)
	}
	if len(descs) != 0 || len(alloc.Pending()) != 0 {
		t.Errorf("an LI with an inline orphan must be left untouched: descs=%v pending=%d", descs, len(alloc.Pending()))
	}
}

func TestFixListBody_LeavesRawContent(t *testing.T) {
	// A raw marked-content ID (a bare integer in /K) carries no /S and is not
	// flagged by the validator, so an LI of [Lbl, MCID] needs no fix.
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K 5 0 R >>",
		"<< /Type /StructElem /S /L /P 4 0 R /K [6 0 R] >>",
		"<< /Type /StructElem /S /LI /P 5 0 R /K [7 0 R 0] >>",
		"<< /Type /StructElem /S /Lbl /P 6 0 R >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixListBody(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixListBody: %v", err)
	}
	if len(descs) != 0 || len(alloc.Pending()) != 0 {
		t.Errorf("raw marked content under an LI must be left untouched: descs=%v pending=%d", descs, len(alloc.Pending()))
	}
}

func TestFixListBody_Idempotent(t *testing.T) {
	// A second pass over the already-wrapped LI must do nothing: the LI now owns
	// an LBody, so the existing-LBody guard makes the fixer no-op.
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K 5 0 R >>",
		"<< /Type /StructElem /S /L /P 4 0 R /K [6 0 R] >>",
		"<< /Type /StructElem /S /LI /P 5 0 R /K [7 0 R 8 0 R] >>",
		"<< /Type /StructElem /S /Lbl /P 6 0 R >>",
		"<< /Type /StructElem /S /P /P 6 0 R >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	if _, err := fixListBody(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1"); err != nil {
		t.Fatalf("fixListBody (first pass): %v", err)
	}
	pendingAfterFirst := len(alloc.Pending())

	descs, err := fixListBody(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixListBody (second pass): %v", err)
	}
	if len(descs) != 0 {
		t.Errorf("second pass must be a no-op, got %v", descs)
	}
	if len(alloc.Pending()) != pendingAfterFirst {
		t.Errorf("second pass changed pending objects: was %d, now %d", pendingAfterFirst, len(alloc.Pending()))
	}
}

func TestFixListBody_CycleSafe(t *testing.T) {
	// A /K back-reference from a leaf up to an ancestor must not make the walk
	// loop; the visited set breaks the cycle and the genuine orphan still wraps.
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K 5 0 R >>",
		"<< /Type /StructElem /S /L /P 4 0 R /K [6 0 R] >>",
		"<< /Type /StructElem /S /LI /P 5 0 R /K [7 0 R] >>",
		"<< /Type /StructElem /S /P /P 6 0 R /K [5 0 R] >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixListBody(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixListBody: %v", err)
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
	if lbodyK, ok := pendingDict(t, alloc, lbodyRef.ObjectNum)["K"].(ast.PdfArray); !ok || len(lbodyK) != 1 || lbodyK[0] != (ast.PdfIndirectRef{ObjectNum: 7}) {
		t.Fatalf("LBody /K: got %v, want [P 7 0 R]", pendingDict(t, alloc, lbodyRef.ObjectNum)["K"])
	}
}
