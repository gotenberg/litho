package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// pageCtxsFor builds the per-page context the page-scoped checks expect, the
// same way check.go does.
func pageCtxsFor(ctx context.Context, doc *document.Document, cat ast.PdfDict) []pageCtx {
	return collectPageContexts(ctx, doc, cat)
}

// pageCtxsForPages wraps explicit page dictionaries the same way.
//
// Several tests build a page without a /Pages tree for the catalog to reach, so
// collectPages finds nothing and [pageCtxsFor] cannot serve them. The content
// stream is resolved here because the checks now read their operations from
// [pageCtx.parsedOps]: leaving it unset would hand them a page that paints
// nothing, and the test would pass while exercising far less than it claims.
func pageCtxsForPages(ctx context.Context, doc *document.Document, pages ...ast.PdfDict) []pageCtx {
	out := make([]pageCtx, len(pages))
	for i, page := range pages {
		contentBytes, contentErr := resolvePageContentBytes(ctx, doc, page)
		out[i] = pageCtx{
			pageObject:        pageObject{dict: page},
			resources:         resolveResources(ctx, doc, page),
			content:           contentBytes,
			parseFailed:       contentErr != nil,
			contentUnreadable: contentErr != nil,
		}
	}
	return out
}

// TestSharedExtGStateJudgedOnce pins that an ExtGState dictionary several pages
// share is one finding, not one per page.
//
// These rules forbid keys OF THE DICTIONARY (ISO 19005-2 §6.2.5 t1 forbids /TR),
// so the violation belongs to the state, not to each page that selects it.
// veraPDF states the rule against its single PDExtGState.
//
// # Corpus evidence
//
//   - govdocs thread_100_100666.pdf: references object 190 from 30 pages, and
//     veraPDF reports its /TR once, where litho reported it 30 times.
func TestSharedExtGStateJudgedOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Three pages, all naming the same indirect ExtGState (object 6).
	page := "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792]" +
		" /Resources << /ExtGState << /GS0 6 0 R >> >> >>"
	doc, cat := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R 5 0 R] /Count 3 >>",
		page, page, page,
		"<< /Type /ExtGState /TR 7 0 R >>",
		"<< /FunctionType 2 /Domain [0 1] /C0 [0] /C1 [1] /N 1 >>",
	)

	got := checkGraphicsGroup(ctx, &validationCtx{
		doc: doc, catalog: cat, profile: "PDF/A-2b", part: 2,
		pages: pageCtxsFor(ctx, doc, cat),
	})

	n := 0
	for _, v := range got {
		if v.Rule == Rule_19005_2_6_2_5_t01 {
			n++
		}
	}
	if n != 1 {
		t.Errorf("one shared ExtGState must report §6.2.5 t1 once, got %d (%v)", n, got)
	}
}

// TestDistinctExtGStatesEachReport pins the other side: two different state
// dictionaries are two findings.
func TestDistinctExtGStatesEachReport(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	doc, cat := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792]"+
			" /Resources << /ExtGState << /GS0 5 0 R >> >> >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792]"+
			" /Resources << /ExtGState << /GS1 6 0 R >> >> >>",
		"<< /Type /ExtGState /TR 7 0 R >>",
		"<< /Type /ExtGState /TR 7 0 R >>",
		"<< /FunctionType 2 /Domain [0 1] /C0 [0] /C1 [1] /N 1 >>",
	)

	got := checkGraphicsGroup(ctx, &validationCtx{
		doc: doc, catalog: cat, profile: "PDF/A-2b", part: 2,
		pages: pageCtxsFor(ctx, doc, cat),
	})

	n := 0
	for _, v := range got {
		if v.Rule == Rule_19005_2_6_2_5_t01 {
			n++
		}
	}
	if n != 2 {
		t.Errorf("two distinct ExtGStates must each report §6.2.5 t1, got %d (%v)", n, got)
	}
}
