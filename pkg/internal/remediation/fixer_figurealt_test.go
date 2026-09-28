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

func TestFixFigureAlt_UA2CaptionChildDoesNotSatisfy(t *testing.T) {
	// ISO 14289-2 §8.2.5.28.2 lists /Alt and /ActualText alone, so a Caption
	// child does not stand in for them: veraPDF reports such a Figure, and the
	// fixer repairs exactly what the validator flags.
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
	if len(descs) != 1 {
		t.Fatalf("a UA-2 figure whose only alternative is a Caption child needs a fix: descs=%v", descs)
	}
}

func TestFixFigureAlt_RemappedFigureIsNoFigure(t *testing.T) {
	// The role map applies first: a standard Figure the /RoleMap remaps to Span
	// stands for Span, which veraPDF does not judge as a Figure, so the fixer
	// leaves it alone.
	pdf := figurePDF(
		"<< /Type /StructTreeRoot /K 5 0 R /RoleMap << /Figure /Span >> >>",
		"<< /Type /StructElem /S /Figure /P 4 0 R >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixFigureAlt(context.Background(), alloc, doc, Options{PreserveContent: false}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixFigureAlt: %v", err)
	}
	if len(descs) != 0 || len(alloc.Pending()) != 0 {
		t.Errorf("a Figure remapped to Span needs no fix: descs=%v pending=%d", descs, len(alloc.Pending()))
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

// TestFixFigureAlt_WritesFiguresWrittenDirectly pins that a Figure written
// directly in a /K, with no object number of its own, takes the alternate text
// in the object that holds it: the element or the /K array written as an
// object of its own, however deep inline elements nest it. A /K array two
// elements share is rewritten once.
func TestFixFigureAlt_WritesFiguresWrittenDirectly(t *testing.T) {
	t.Parallel()
	figure := "<< /Type /StructElem /S /Figure >>"
	for _, tc := range []struct {
		name  string
		objs  []string
		owner int
		path  []int
	}{
		{name: "in an element's /K array", objs: []string{
			"<< /Type /StructElem /S /Document /P 4 0 R /K [" + figure + "] >>",
		}, owner: 5, path: []int{0}},
		{name: "as an element's /K", objs: []string{
			"<< /Type /StructElem /S /Document /P 4 0 R /K " + figure + " >>",
		}, owner: 5},
		{name: "under an inline element", objs: []string{
			"<< /Type /StructElem /S /Document /P 4 0 R /K [<< /Type /StructElem /S /Sect /K [(x) " + figure + "] >>] >>",
		}, owner: 5, path: []int{0, 1}},
		{name: "in a /K array two elements share", objs: []string{
			"<< /Type /StructElem /S /Document /P 4 0 R /K [6 0 R 7 0 R] >>",
			"<< /Type /StructElem /S /Sect /P 5 0 R /K 8 0 R >>",
			"<< /Type /StructElem /S /Sect /P 5 0 R /K 8 0 R >>",
			"[<< /Type /StructElem /S /Span >> " + figure + "]",
		}, owner: 8, path: []int{1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			objs := append([]string{
				"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
				"<< /Type /StructTreeRoot /K 5 0 R >>",
			}, tc.objs...)
			doc, alloc := newTestDoc(t, buildStructPDF(objs...))

			descs, err := fixFigureAlt(context.Background(), alloc, doc, Options{FigureAlt: "A chart"}, nil, "PDF/UA-1")
			if err != nil {
				t.Fatalf("fixFigureAlt: %v", err)
			}
			if len(descs) != 1 || !strings.Contains(descs[0], "on 1 figure(s)") {
				t.Fatalf("descs %v, want one Figure given alternate text", descs)
			}
			if len(alloc.Pending()) != 1 {
				t.Errorf("pending %d objects, want the owner %d alone", len(alloc.Pending()), tc.owner)
			}
			var node ast.PdfObject = alloc.Pending()[tc.owner]
			if d, isDict := node.(ast.PdfDict); isDict {
				node = d["K"]
			}
			for _, i := range tc.path {
				if d, isDict := node.(ast.PdfDict); isDict {
					node = d["K"]
				}
				arr, isArr := node.(ast.PdfArray)
				if !isArr || i >= len(arr) {
					t.Fatalf("owner %d holds %v, no kid %d", tc.owner, node, i)
				}
				node = arr[i]
			}
			fig, _ := node.(ast.PdfDict)
			if got := fig["Alt"]; got != ast.PdfString("A chart") {
				t.Errorf("Figure %v: /Alt %v, want the caller's text", fig, got)
			}
		})
	}
}

// TestFixFigureAlt_WritesFigureAnInlineElementLists pins that a Figure with an
// object number of its own that only an element written inline lists takes
// the alternate text at its own number.
func TestFixFigureAlt_WritesFigureAnInlineElementLists(t *testing.T) {
	t.Parallel()
	doc, alloc := newTestDoc(t, buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K 5 0 R >>",
		"<< /Type /StructElem /S /Document /P 4 0 R /K [<< /Type /StructElem /S /Sect /K [6 0 R] >>] >>",
		"<< /Type /StructElem /S /Figure >>",
	))
	descs, err := fixFigureAlt(context.Background(), alloc, doc, Options{FigureAlt: "A chart"}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixFigureAlt: %v", err)
	}
	if len(descs) != 1 || !strings.Contains(descs[0], "on 1 figure(s)") {
		t.Fatalf("descs %v, want one Figure given alternate text", descs)
	}
	if got := pendingDict(t, alloc, 6)["Alt"]; got != ast.PdfString("A chart") {
		t.Errorf("Figure 6 /Alt = %v, want the caller's text", got)
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
