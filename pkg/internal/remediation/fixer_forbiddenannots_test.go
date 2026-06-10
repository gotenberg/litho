package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

func TestMatchForbiddenAnnots(t *testing.T) {
	t.Parallel()
	for _, rule := range []compliance.Rule{
		compliance.Rule_14289_2_8_9_2_4_11_t01, // Sound
		compliance.Rule_14289_2_8_9_2_4_11_t02, // Movie
		compliance.Rule_14289_2_8_9_2_4_15_t01, // TrapNet
	} {
		if !matchForbiddenAnnots(compliance.Violation{Rule: rule}) {
			t.Errorf("must match forbidden-annot rule %s/%s t%d", rule.Specification, rule.Clause, rule.TestNumber)
		}
	}
	// Must not claim the FileAttachment /AFRelationship rule (adjacent clause).
	if matchForbiddenAnnots(compliance.Violation{Rule: compliance.Rule_14289_2_8_9_2_4_10_t01}) {
		t.Error("must not claim the FileAttachment /AFRelationship rule")
	}
}

// annotsPDF builds a single-page PDF whose page (object 3) carries an /Annots
// array referencing the given annotation objects (objects 4+).
func annotsPDF(annotRefs string, annots ...string) []byte {
	bodies := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots " + annotRefs + " >>",
	}
	bodies = append(bodies, annots...)
	return buildStructPDF(bodies...)
}

func TestFixForbiddenAnnots_RemovesForbidden(t *testing.T) {
	// Page has a Movie (4), a Link (5), and a Sound (6); only the Link survives.
	pdf := annotsPDF("[4 0 R 5 0 R 6 0 R]",
		"<< /Type /Annot /Subtype /Movie /Rect [0 0 10 10] >>",
		"<< /Type /Annot /Subtype /Link /Rect [0 0 10 10] >>",
		"<< /Type /Annot /Subtype /Sound /Rect [0 0 10 10] >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixForbiddenAnnots(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-2")
	if err != nil {
		t.Fatalf("fixForbiddenAnnots: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 fix description, got %v", descs)
	}
	page := pendingDict(t, alloc, 3)
	annots, ok := page["Annots"].(ast.PdfArray)
	if !ok {
		t.Fatalf("/Annots is not an array: %T", page["Annots"])
	}
	if len(annots) != 1 {
		t.Fatalf("expected 1 surviving annotation, got %d", len(annots))
	}
	if ref, ok := annots[0].(ast.PdfIndirectRef); !ok || ref.ObjectNum != 5 {
		t.Errorf("surviving annotation = %v, want the Link (obj 5)", annots[0])
	}
}

func TestFixForbiddenAnnots_NoForbiddenUntouched(t *testing.T) {
	// A page whose annotations are all permitted is not rewritten.
	pdf := annotsPDF("[4 0 R]",
		"<< /Type /Annot /Subtype /Link /Rect [0 0 10 10] >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixForbiddenAnnots(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-2")
	if err != nil {
		t.Fatalf("fixForbiddenAnnots: %v", err)
	}
	if len(descs) != 0 {
		t.Errorf("no forbidden annots: got %v, want no fixes", descs)
	}
	if _, queued := alloc.Pending()[3]; queued {
		t.Error("page with only permitted annots must not be rewritten")
	}
}

func TestFixForbiddenAnnots_RewritesIndirectAnnotsArray(t *testing.T) {
	// /Annots is an indirect reference to an array object (5); the array object
	// is rewritten in place, not the page.
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots 5 0 R >>",
		"<< /Type /Annot /Subtype /TrapNet /Rect [0 0 10 10] >>",
		"[4 0 R]",
	)
	doc, alloc := newTestDoc(t, pdf)

	if _, err := fixForbiddenAnnots(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-2"); err != nil {
		t.Fatalf("fixForbiddenAnnots: %v", err)
	}
	if _, queued := alloc.Pending()[3]; queued {
		t.Error("page must not be rewritten when /Annots is indirect")
	}
	arr, ok := alloc.Pending()[5].(ast.PdfArray)
	if !ok {
		t.Fatalf("array object 5 not rewritten: %T", alloc.Pending()[5])
	}
	if len(arr) != 0 {
		t.Errorf("expected the TrapNet annotation removed, got %d entries", len(arr))
	}
}
