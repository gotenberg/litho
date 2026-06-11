package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestMatchPageTabs guards against the description-substring matcher that
// silently skipped every PDF/UA-1 document: the UA-1 rule (§7.18.3) and the
// UA-2 rule (§8.9.3.3) word the requirement differently, so the matcher must
// key off the clause, not the wording.
func TestMatchPageTabs(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		rule compliance.Rule
		want bool
	}{
		{"PDF/UA-1 §7.18.3", compliance.Rule_14289_1_7_18_3_t01, true},
		{"PDF/UA-2 §8.9.3.3", compliance.Rule_14289_2_8_9_3_3_t01, true},
		{"unrelated UA rule", compliance.Rule_14289_1_5_t01, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := matchPageTabs(compliance.Violation{Rule: tc.rule}); got != tc.want {
				t.Errorf("matchPageTabs(%s) = %v, want %v", tc.rule.Clause, got, tc.want)
			}
		})
	}
}

func TestFixPageTabs_AddsTabsToAnnotatedPage(t *testing.T) {
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /Link /Rect [0 0 10 10] >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixPageTabs(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixPageTabs: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 description, got %v", descs)
	}
	if pendingDict(t, alloc, 3)["Tabs"] != ast.PdfName("S") {
		t.Errorf("page /Tabs: got %v, want S", pendingDict(t, alloc, 3)["Tabs"])
	}
}

// TestFixPageTabs_RespectsPendingPageDict guards the alloc-first read: when
// an earlier fixer queued a page rewrite, the /Tabs patch must start from
// that pending dict, not from the parsed original, or the earlier fix is
// clobbered on AllocAt.
func TestFixPageTabs_RespectsPendingPageDict(t *testing.T) {
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /Link /Rect [0 0 10 10] >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	alloc.AllocAt(3, ast.PdfDict{
		"Type":     ast.PdfName("Page"),
		"Parent":   ast.PdfIndirectRef{ObjectNum: 2},
		"MediaBox": ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(612), ast.PdfInt(792)},
		"Annots":   ast.PdfArray{ast.PdfIndirectRef{ObjectNum: 4}},
		"Marker":   ast.PdfName("FromPending"),
	})

	if _, err := fixPageTabs(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1"); err != nil {
		t.Fatalf("fixPageTabs: %v", err)
	}
	got := pendingDict(t, alloc, 3)
	if got["Marker"] != ast.PdfName("FromPending") {
		t.Error("fixPageTabs clobbered the pending page rewrite")
	}
	if got["Tabs"] != ast.PdfName("S") {
		t.Errorf("page /Tabs: got %v, want S", got["Tabs"])
	}
}

// TestFixPageTabs_SkipsWhenPendingPageDroppedAnnots guards against
// resurrecting annotations: when an earlier fixer removed the page's
// /Annots, the stale original (which still has them) must not be written
// back just to add /Tabs.
func TestFixPageTabs_SkipsWhenPendingPageDroppedAnnots(t *testing.T) {
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /Sound /Rect [0 0 10 10] >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	alloc.AllocAt(3, ast.PdfDict{
		"Type":     ast.PdfName("Page"),
		"Parent":   ast.PdfIndirectRef{ObjectNum: 2},
		"MediaBox": ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(612), ast.PdfInt(792)},
	})

	descs, err := fixPageTabs(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixPageTabs: %v", err)
	}
	if len(descs) != 0 {
		t.Errorf("expected no fixes, got %v", descs)
	}
	got := pendingDict(t, alloc, 3)
	if _, resurrect := got["Annots"]; resurrect {
		t.Error("fixPageTabs resurrected annotations removed by an earlier fixer")
	}
	if _, hasTabs := got["Tabs"]; hasTabs {
		t.Error("a page without annotations needs no /Tabs")
	}
}

func TestFixPageTabs_SkipsPageWithoutAnnotations(t *testing.T) {
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixPageTabs(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixPageTabs: %v", err)
	}
	if len(descs) != 0 || len(alloc.Pending()) != 0 {
		t.Errorf("a page without annotations needs no /Tabs: descs=%v pending=%d", descs, len(alloc.Pending()))
	}
}
