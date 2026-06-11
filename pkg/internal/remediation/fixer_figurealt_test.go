package remediation

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

func TestMatchFigureAlt(t *testing.T) {
	t.Parallel()
	if !matchFigureAlt(compliance.Violation{Rule: compliance.Rule_14289_1_7_3_t01}) {
		t.Error("must match §7.3 t1 (PDF/UA-1 Figure /Alt)")
	}
	if !matchFigureAlt(compliance.Violation{Rule: compliance.Rule_14289_2_8_2_5_28_2_t01}) {
		t.Error("must match §8.2.5.28.2 t1 (PDF/UA-2 Figure /Alt)")
	}
	if matchFigureAlt(compliance.Violation{Rule: compliance.Rule{Clause: "7.7", TestNumber: 1}}) {
		t.Error("must not match §7.7 (Formula)")
	}
	if matchFigureAlt(compliance.Violation{Rule: compliance.Rule{Clause: "7.3", TestNumber: 2}}) {
		t.Error("must not match a different §7.3 test number")
	}
}

// figurePDF builds a single-page tagged PDF whose StructTreeRoot points at one
// Figure (object 5). The caller supplies the Figure's dict body and any extra
// trailing objects (e.g. a Caption child) so each test shapes its own figure.
func figurePDF(structTreeRoot, figure string, extra ...string) []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		structTreeRoot,
		figure,
	}
	objs = append(objs, extra...)
	return buildStructPDF(objs...)
}

func TestFixFigureAlt_NoopByDefault(t *testing.T) {
	// Safe default (PreserveContent true, no FigureAlt): a Figure missing /Alt is
	// left for a human rather than given fabricated text.
	pdf := figurePDF(
		"<< /Type /StructTreeRoot /K 5 0 R >>",
		"<< /Type /StructElem /S /Figure /P 4 0 R >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixFigureAlt(context.Background(), alloc, doc, Options{PreserveContent: true}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixFigureAlt: %v", err)
	}
	if len(descs) != 0 || len(alloc.Pending()) != 0 {
		t.Errorf("safe mode must not fabricate /Alt: descs=%v pending=%d", descs, len(alloc.Pending()))
	}
}

func TestFixFigureAlt_AggressiveDefaultPlaceholder(t *testing.T) {
	// Aggressive mode (PreserveContent false), no caller string: a generic
	// placeholder is inserted and the description flags the residual.
	pdf := figurePDF(
		"<< /Type /StructTreeRoot /K 5 0 R >>",
		"<< /Type /StructElem /S /Figure /P 4 0 R >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixFigureAlt(context.Background(), alloc, doc, Options{PreserveContent: false}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixFigureAlt: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 description, got %v", descs)
	}
	if !strings.Contains(descs[0], "placeholder") || !strings.Contains(descs[0], "human review still owed") {
		t.Errorf("description must flag the residual: %q", descs[0])
	}
	if got := pendingDict(t, alloc, 5)["Alt"]; got != ast.PdfString("Figure") {
		t.Errorf("figure /Alt: got %v, want placeholder \"Figure\"", got)
	}
}

func TestFixFigureAlt_ExplicitOverrideInSafeMode(t *testing.T) {
	// A caller-supplied string is honest content, so it applies even in safe mode.
	pdf := figurePDF(
		"<< /Type /StructTreeRoot /K 5 0 R >>",
		"<< /Type /StructElem /S /Figure /P 4 0 R >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixFigureAlt(context.Background(), alloc, doc, Options{PreserveContent: true, FigureAlt: "A quarterly revenue bar chart"}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixFigureAlt: %v", err)
	}
	if len(descs) != 1 || strings.Contains(descs[0], "placeholder") {
		t.Fatalf("caller-supplied alt is not a placeholder: %v", descs)
	}
	if got := pendingDict(t, alloc, 5)["Alt"]; got != ast.PdfString("A quarterly revenue bar chart") {
		t.Errorf("figure /Alt: got %v, want the supplied string", got)
	}
}

func TestFixFigureAlt_SkipsSatisfiedFigure(t *testing.T) {
	// A Figure that already carries non-empty /Alt, and one that carries
	// /ActualText, both satisfy PDF/UA-1 §7.3 and must be left untouched even in
	// aggressive mode.
	for _, tc := range []struct {
		name, figure string
	}{
		{"has Alt", "<< /Type /StructElem /S /Figure /P 4 0 R /Alt (Existing description) >>"},
		{"has ActualText", "<< /Type /StructElem /S /Figure /P 4 0 R /ActualText (literal text) >>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pdf := figurePDF("<< /Type /StructTreeRoot /K 5 0 R >>", tc.figure)
			doc, alloc := newTestDoc(t, pdf)

			descs, err := fixFigureAlt(context.Background(), alloc, doc, Options{PreserveContent: false}, nil, "PDF/UA-1")
			if err != nil {
				t.Fatalf("fixFigureAlt: %v", err)
			}
			if len(descs) != 0 || len(alloc.Pending()) != 0 {
				t.Errorf("a satisfied figure needs no fix: descs=%v pending=%d", descs, len(alloc.Pending()))
			}
		})
	}
}

func TestFixFigureAlt_RoleMapped(t *testing.T) {
	// A non-standard role mapped to Figure via /RoleMap must be repaired exactly
	// as the validator flags it.
	pdf := figurePDF(
		"<< /Type /StructTreeRoot /K 5 0 R /RoleMap << /Fig /Figure >> >>",
		"<< /Type /StructElem /S /Fig /P 4 0 R >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixFigureAlt(context.Background(), alloc, doc, Options{PreserveContent: false}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixFigureAlt: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 description, got %v", descs)
	}
	if got := pendingDict(t, alloc, 5)["Alt"]; got != ast.PdfString("Figure") {
		t.Errorf("role-mapped figure /Alt: got %v, want placeholder", got)
	}
}

func TestFixFigureAlt_UA2CaptionChildSatisfies(t *testing.T) {
	// Under PDF/UA-2 a Caption child satisfies §8.2.5.28.2, so the Figure needs
	// no /Alt even in aggressive mode.
	pdf := figurePDF(
		"<< /Type /StructTreeRoot /K 5 0 R >>",
		"<< /Type /StructElem /S /Figure /P 4 0 R /K 6 0 R >>",
		"<< /Type /StructElem /S /Caption /P 5 0 R >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixFigureAlt(context.Background(), alloc, doc, Options{PreserveContent: false}, nil, "PDF/UA-2")
	if err != nil {
		t.Fatalf("fixFigureAlt: %v", err)
	}
	if len(descs) != 0 || len(alloc.Pending()) != 0 {
		t.Errorf("a UA-2 figure with a Caption child needs no fix: descs=%v pending=%d", descs, len(alloc.Pending()))
	}
}

func TestFixFigureAlt_MultipleFiguresMixedSatisfaction(t *testing.T) {
	// Two figures under the root: object 5 already has /Alt (satisfied), object 6
	// has none. Aggressive mode must fix only object 6 and leave 5 untouched.
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K [5 0 R 6 0 R] >>",
		"<< /Type /StructElem /S /Figure /P 4 0 R /Alt (Already described) >>",
		"<< /Type /StructElem /S /Figure /P 4 0 R >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixFigureAlt(context.Background(), alloc, doc, Options{PreserveContent: false}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixFigureAlt: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 figure fixed, got %v", descs)
	}
	if _, queued := alloc.Pending()[5]; queued {
		t.Error("a satisfied figure (obj 5) must not be rewritten")
	}
	if got := pendingDict(t, alloc, 6)["Alt"]; got != ast.PdfString("Figure") {
		t.Errorf("unsatisfied figure (obj 6) /Alt: got %v, want placeholder", got)
	}
}

func TestFixFigureAlt_NestedFigureCollected(t *testing.T) {
	// A Figure nested Document > Sect > Figure must be reached by the walk.
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K 5 0 R >>",
		"<< /Type /StructElem /S /Document /P 4 0 R /K 6 0 R >>",
		"<< /Type /StructElem /S /Sect /P 5 0 R /K 7 0 R >>",
		"<< /Type /StructElem /S /Figure /P 6 0 R >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixFigureAlt(context.Background(), alloc, doc, Options{PreserveContent: false}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixFigureAlt: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 figure fixed, got %v", descs)
	}
	if got := pendingDict(t, alloc, 7)["Alt"]; got != ast.PdfString("Figure") {
		t.Errorf("nested figure /Alt: got %v, want placeholder", got)
	}
}

func TestFixFigureAlt_DepthBoundCapsCollection(t *testing.T) {
	// A Figure buried beyond MaxStructTreeWalkDepth must not be collected, so a
	// pathologically deep tree cannot drive the walk into unbounded recursion.
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K 5 0 R >>",
	}
	const firstSect = 5
	chain := compliance.MaxStructTreeWalkDepth + 10
	for i := range chain {
		obj := firstSect + i
		parent := obj - 1
		if i == 0 {
			parent = 4 // the StructTreeRoot
		}
		objs = append(objs, fmt.Sprintf("<< /Type /StructElem /S /Sect /P %d 0 R /K %d 0 R >>", parent, obj+1))
	}
	figObj := firstSect + chain
	objs = append(objs, fmt.Sprintf("<< /Type /StructElem /S /Figure /P %d 0 R >>", figObj-1))
	pdf := buildStructPDF(objs...)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixFigureAlt(context.Background(), alloc, doc, Options{PreserveContent: false}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixFigureAlt: %v", err)
	}
	if len(descs) != 0 || len(alloc.Pending()) != 0 {
		t.Errorf("a figure beyond the depth bound must not be collected: descs=%v pending=%d", descs, len(alloc.Pending()))
	}
}

func TestFixFigureAlt_InlineFigureSkipped(t *testing.T) {
	// An inline Figure (a direct dict in /K, no object number) cannot be rewritten
	// in place, so it is skipped rather than corrupting the parent.
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K 5 0 R >>",
		"<< /Type /StructElem /S /Document /P 4 0 R /K [<< /Type /StructElem /S /Figure >>] >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixFigureAlt(context.Background(), alloc, doc, Options{PreserveContent: false}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixFigureAlt: %v", err)
	}
	if len(descs) != 0 || len(alloc.Pending()) != 0 {
		t.Errorf("an inline figure must be skipped: descs=%v pending=%d", descs, len(alloc.Pending()))
	}
}

func TestFixFigureAlt_UA1EmptyAltNotSatisfied(t *testing.T) {
	// An empty /Alt fails PDF/UA-1 §7.3 (it demands non-empty), so aggressive mode
	// replaces it with the placeholder.
	pdf := figurePDF(
		"<< /Type /StructTreeRoot /K 5 0 R >>",
		"<< /Type /StructElem /S /Figure /P 4 0 R /Alt () >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixFigureAlt(context.Background(), alloc, doc, Options{PreserveContent: false}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixFigureAlt: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 description, got %v", descs)
	}
	if got := pendingDict(t, alloc, 5)["Alt"]; got != ast.PdfString("Figure") {
		t.Errorf("empty /Alt should be replaced: got %v", got)
	}
}
