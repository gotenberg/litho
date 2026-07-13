package compliance

import (
	"context"
	"errors"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestResolvePageContentBytes_UndecodableFilterSkipped covers the §6.2.2
// false-positive guard: a content stream whose /Filter chain contains an
// undecodable filter (here a misspelled "Flatedecode") stays filter-encoded, so
// resolvePageContentBytes reports errUndecodableContent instead of returning the
// still-encoded bytes for operator parsing. A stream with no filter decodes
// normally.
func TestResolvePageContentBytes_UndecodableFilterSkipped(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	badDoc, _ := objsDocPDF(t,
		"<< /Type /Catalog >>",
		"<< /Type /Page /Contents 3 0 R >>",
		"<< /Filter /Flatedecode /Length 5 >>\nstream\nhello\nendstream",
	)
	badPage, err := document.ResolveDict(ctx, badDoc, ast.PdfIndirectRef{ObjectNum: 2})
	if err != nil {
		t.Fatalf("resolve page: %v", err)
	}
	if _, err := resolvePageContentBytes(ctx, badDoc, badPage); !errors.Is(err, errUndecodableContent) {
		t.Errorf("undecodable /Filter: want errUndecodableContent, got %v", err)
	}

	okDoc, _ := objsDocPDF(t,
		"<< /Type /Catalog >>",
		"<< /Type /Page /Contents 3 0 R >>",
		"<< /Length 5 >>\nstream\nBT ET\nendstream",
	)
	okPage, err := document.ResolveDict(ctx, okDoc, ast.PdfIndirectRef{ObjectNum: 2})
	if err != nil {
		t.Fatalf("resolve page: %v", err)
	}
	data, err := resolvePageContentBytes(ctx, okDoc, okPage)
	if err != nil {
		t.Fatalf("unfiltered content: unexpected error %v", err)
	}
	if len(data) == 0 {
		t.Error("unfiltered content: want decoded bytes, got none")
	}
}
