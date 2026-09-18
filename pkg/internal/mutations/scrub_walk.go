package mutations

import (
	"context"
	"fmt"
	"sort"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// scrubDocumentPages validates the tokens, walks every page of the document,
// and returns the object numbers the per-page scrub rewrote, ascending.
//
// The document-wide passes share this because the vector they clean differs but
// the walk does not: an annotation, a form field value or anything else holding
// a copy of removed text can sit on any page, so each has to cross the whole
// file rather than the page a redaction touched.
//
// The result is sorted because the collection is a map, and a caller reporting
// what was cleaned should see the same order every run.
func scrubDocumentPages(
	ctx context.Context,
	doc *document.Document,
	tokens []string,
	perPage func(page *document.Page, scrubbed map[int]bool) error,
) ([]int, error) {
	for _, tok := range tokens {
		if tok == "" {
			return nil, fmt.Errorf("%w: an empty token matches every object", ErrScanTokenEmpty)
		}
	}
	if len(tokens) == 0 {
		return nil, nil
	}

	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil || catalog == nil {
		// No catalog means no page tree to walk, and nothing to clean.
		return nil, nil
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		return nil, fmt.Errorf("resolving pages: %w", err)
	}

	scrubbed := make(map[int]bool)
	for _, page := range pages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := perPage(page, scrubbed); err != nil {
			return nil, err
		}
	}

	out := make([]int, 0, len(scrubbed))
	for objNum := range scrubbed {
		out = append(out, objNum)
	}
	sort.Ints(out)
	return out, nil
}
