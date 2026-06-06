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
