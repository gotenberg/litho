package remediation

import (
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

func TestMatchAnnotOpacity(t *testing.T) {
	t.Parallel()
	// §6.5.3 t01 is the /CA rule; t02 is the /F-flag rule. The opacity matcher
	// must claim t01 only, and the flag matcher must not also claim t01, so the
	// two fixers never double-edit the same annotation.
	if !matchAnnotOpacity(compliance.Violation{Rule: compliance.Rule_19005_1_6_5_3_t01}) {
		t.Error("must match §6.5.3 t01 (annotation /CA)")
	}
	if matchAnnotOpacity(compliance.Violation{Rule: compliance.Rule_19005_1_6_5_3_t02}) {
		t.Error("must not match §6.5.3 t02 (the /F-flag rule)")
	}
	if matchAnnotFlags(compliance.Violation{Rule: compliance.Rule_19005_1_6_5_3_t01}) {
		t.Error("the flag fixer must not also claim the /CA rule")
	}
}

// annotPDF builds a single-page PDF whose page carries the given annotation
// objects in /Annots. Each annotation body is an object starting at number 4
// (1=Catalog, 2=Pages, 3=Page).
func annotPDF(annotRefs string, annots ...string) []byte {
	bodies := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots " + annotRefs + " >>",
	}
	bodies = append(bodies, annots...)
	return buildStructPDF(bodies...)
}

func TestFixAnnotOpacity_RemovesNonConformantCA(t *testing.T) {
	// Object 4 has /CA 0.5 (non-conformant) and must lose the key; object 5 has
	// /CA 1.0 (conformant) and must be left untouched.
	pdf := annotPDF(
		"[4 0 R 5 0 R]",
		"<< /Type /Annot /Subtype /Text /Rect [0 0 10 10] /CA 0.5 >>",
		"<< /Type /Annot /Subtype /Text /Rect [0 0 10 10] /CA 1.0 >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixAnnotOpacity(context.Background(), alloc, doc, Options{}, nil, "PDF/A-1")
	if err != nil {
		t.Fatalf("fixAnnotOpacity: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 description, got %v", descs)
	}

	if _, hasCA := pendingDict(t, alloc, 4)["CA"]; hasCA {
		t.Error("non-conformant /CA must be removed from annotation 4")
	}
	if _, queued := alloc.Pending()[5]; queued {
		t.Error("a conformant /CA 1.0 annotation (obj 5) must not be rewritten")
	}
}

func TestFixAnnotOpacity_IndirectCA(t *testing.T) {
	// /CA given as an indirect reference must still be resolved and removed.
	pdf := annotPDF(
		"[4 0 R]",
		"<< /Type /Annot /Subtype /Text /Rect [0 0 10 10] /CA 5 0 R >>",
		"0.25",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixAnnotOpacity(context.Background(), alloc, doc, Options{}, nil, "PDF/A-1")
	if err != nil {
		t.Fatalf("fixAnnotOpacity: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 description, got %v", descs)
	}
	if _, hasCA := pendingDict(t, alloc, 4)["CA"]; hasCA {
		t.Error("indirect non-conformant /CA must be removed")
	}
}

func TestFixAnnotOpacity_NoopWhenConformant(t *testing.T) {
	// An absent /CA and a /CA of exactly 1.0 both already conform; neither is
	// touched.
	pdf := annotPDF(
		"[4 0 R 5 0 R]",
		"<< /Type /Annot /Subtype /Text /Rect [0 0 10 10] >>",
		"<< /Type /Annot /Subtype /Text /Rect [0 0 10 10] /CA 1.0 >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixAnnotOpacity(context.Background(), alloc, doc, Options{}, nil, "PDF/A-1")
	if err != nil {
		t.Fatalf("fixAnnotOpacity: %v", err)
	}
	if len(descs) != 0 || len(alloc.Pending()) != 0 {
		t.Errorf("conformant annotations need no fix: descs=%v pending=%d", descs, len(alloc.Pending()))
	}
}

func TestFixAnnotOpacity_ComposesWithAnnotFlags(t *testing.T) {
	// An annotation that violates both §6.5.3 rules (Hidden /F, /CA 0.5) must be
	// repaired by both fixers in one run. annot-flags runs first and queues a
	// pending dict; annot-opacity must read that pending edit (not the original)
	// so the two edits stack instead of clobbering each other.
	pdf := annotPDF(
		"[4 0 R]",
		"<< /Type /Annot /Subtype /Text /Rect [0 0 10 10] /F 2 /CA 0.5 >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	if _, err := fixAnnotFlags(context.Background(), alloc, doc, Options{}, nil, "PDF/A-1"); err != nil {
		t.Fatalf("fixAnnotFlags: %v", err)
	}
	if _, err := fixAnnotOpacity(context.Background(), alloc, doc, Options{}, nil, "PDF/A-1"); err != nil {
		t.Fatalf("fixAnnotOpacity: %v", err)
	}

	annot := pendingDict(t, alloc, 4)
	// Print set (4), Hidden cleared: both flag edits survive.
	if annot["F"] != ast.PdfInt(4) {
		t.Errorf("/F: got %v, want 4 (Print set, Hidden cleared)", annot["F"])
	}
	if _, hasCA := annot["CA"]; hasCA {
		t.Error("/CA must be removed even after the flag fix rewrote the annotation")
	}
}

func TestFixAnnotOpacity_IntegerCA(t *testing.T) {
	// /CA given as an integer must follow the same parity: 2 is removed, 1 is kept
	// (an integer 1 equals the 1.0 default).
	pdf := annotPDF(
		"[4 0 R 5 0 R]",
		"<< /Type /Annot /Subtype /Text /Rect [0 0 10 10] /CA 2 >>",
		"<< /Type /Annot /Subtype /Text /Rect [0 0 10 10] /CA 1 >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixAnnotOpacity(context.Background(), alloc, doc, Options{}, nil, "PDF/A-1")
	if err != nil {
		t.Fatalf("fixAnnotOpacity: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 description, got %v", descs)
	}
	if _, hasCA := pendingDict(t, alloc, 4)["CA"]; hasCA {
		t.Error("integer /CA 2 must be removed")
	}
	if _, queued := alloc.Pending()[5]; queued {
		t.Error("integer /CA 1 already conforms and must not be rewritten")
	}
}

func TestFixAnnotOpacity_WidgetCA(t *testing.T) {
	// ISO 19005-1 §6.5.3 grants Widgets no /CA exemption, so a Widget with a
	// non-conformant /CA is repaired like any other annotation.
	pdf := annotPDF(
		"[4 0 R]",
		"<< /Type /Annot /Subtype /Widget /Rect [0 0 10 10] /CA 0.5 >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixAnnotOpacity(context.Background(), alloc, doc, Options{}, nil, "PDF/A-1")
	if err != nil {
		t.Fatalf("fixAnnotOpacity: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 description, got %v", descs)
	}
	if _, hasCA := pendingDict(t, alloc, 4)["CA"]; hasCA {
		t.Error("a Widget's non-conformant /CA must be removed")
	}
}

func TestFixAnnotOpacity_MultiPage(t *testing.T) {
	// Annotations on different pages must all be reached by the page-tree walk.
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [5 0 R] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [6 0 R] >>",
		"<< /Type /Annot /Subtype /Text /Rect [0 0 10 10] /CA 0.5 >>",
		"<< /Type /Annot /Subtype /Text /Rect [0 0 10 10] /CA 0.5 >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixAnnotOpacity(context.Background(), alloc, doc, Options{}, nil, "PDF/A-1")
	if err != nil {
		t.Fatalf("fixAnnotOpacity: %v", err)
	}
	if len(descs) != 1 || !strings.Contains(descs[0], "2 annotation") {
		t.Fatalf("expected both annotations fixed, got %v", descs)
	}
	for _, n := range []int{5, 6} {
		if _, hasCA := pendingDict(t, alloc, n)["CA"]; hasCA {
			t.Errorf("annotation %d /CA must be removed", n)
		}
	}
}

func TestFixAnnotOpacity_SkipsInlineAnnotation(t *testing.T) {
	// An inline annotation (a direct dict in /Annots, no object number) cannot be
	// rewritten in place, so it is skipped even with a non-conformant /CA.
	pdf := annotPDF(
		"[<< /Type /Annot /Subtype /Text /Rect [0 0 10 10] /CA 0.5 >>]",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixAnnotOpacity(context.Background(), alloc, doc, Options{}, nil, "PDF/A-1")
	if err != nil {
		t.Fatalf("fixAnnotOpacity: %v", err)
	}
	if len(descs) != 0 || len(alloc.Pending()) != 0 {
		t.Errorf("an inline annotation must be skipped: descs=%v pending=%d", descs, len(alloc.Pending()))
	}
}
