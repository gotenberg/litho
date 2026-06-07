package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

func TestMatchSuspects(t *testing.T) {
	t.Parallel()
	if !matchSuspects(compliance.Violation{Rule: compliance.Rule_14289_1_7_1_t04}) {
		t.Error("must match §7.1 t04 (Suspects)")
	}
	if matchSuspects(compliance.Violation{Rule: compliance.Rule_14289_1_7_1_t05}) {
		t.Error("must not match a different §7.1 test")
	}
	// The /MarkInfo (Marked) fixer must not claim the Suspects rule, and vice versa.
	if matchMarkInfo(compliance.Violation{Rule: compliance.Rule_14289_1_7_1_t04}) {
		t.Error("the mark-info fixer must not claim the Suspects rule")
	}
}

func TestFixSuspects_RemovesInlineSuspectsTrue(t *testing.T) {
	pdf := minimalCatalogPDF("/MarkInfo << /Marked true /Suspects true >>")
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixSuspects(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixSuspects: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 description, got %v", descs)
	}

	mi, ok := pendingDict(t, alloc, 1)["MarkInfo"].(ast.PdfDict)
	if !ok {
		t.Fatalf("catalog /MarkInfo: got %T, want inline dict", pendingDict(t, alloc, 1)["MarkInfo"])
	}
	if _, has := mi["Suspects"]; has {
		t.Error("/Suspects must be removed")
	}
	if mi["Marked"] != ast.PdfBool(true) {
		t.Errorf("/Marked must be preserved: got %v", mi["Marked"])
	}
}

func TestFixSuspects_IndirectMarkInfo(t *testing.T) {
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /MarkInfo 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Marked true /Suspects true >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixSuspects(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixSuspects: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 description, got %v", descs)
	}
	if _, has := pendingDict(t, alloc, 4)["Suspects"]; has {
		t.Error("/Suspects must be removed from the indirect /MarkInfo object")
	}
	if _, queued := alloc.Pending()[1]; queued {
		t.Error("the catalog must not be rewritten when /MarkInfo is indirect")
	}
}

func TestFixSuspects_NoopWhenFalse(t *testing.T) {
	pdf := minimalCatalogPDF("/MarkInfo << /Marked true /Suspects false >>")
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixSuspects(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixSuspects: %v", err)
	}
	if len(descs) != 0 || len(alloc.Pending()) != 0 {
		t.Errorf("a false /Suspects already conforms: descs=%v pending=%d", descs, len(alloc.Pending()))
	}
}

func TestFixSuspects_NoopWhenAbsent(t *testing.T) {
	pdf := minimalCatalogPDF("/MarkInfo << /Marked true >>")
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixSuspects(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixSuspects: %v", err)
	}
	if len(descs) != 0 || len(alloc.Pending()) != 0 {
		t.Errorf("an absent /Suspects needs no fix: descs=%v pending=%d", descs, len(alloc.Pending()))
	}
}
