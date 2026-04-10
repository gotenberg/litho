package bookmarks

import (
	"context"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// Input bundles the data needed to generate bookmarks. The caller (Reader or
// Writer) is responsible for resolving pages and extracting text elements,
// since this is an internal package without access to the public Reader.
type Input struct {
	// Doc is the parsed PDF document.
	Doc *document.Document
	// PageRefToNum maps page indirect refs to 1-based page numbers.
	PageRefToNum map[ast.PdfIndirectRef]int
	// Pages contains the text elements for each page (in page order).
	Pages []PageTextElements
}

// PageTextElements pairs a page number with its extracted text elements.
type PageTextElements = pageTextElements

// Generate analyzes the document and returns a hierarchical bookmark tree.
// When PreferStructureTree is set and a structure tree is present, the
// structure tree source is used. Otherwise, the font size histogram source
// is used.
func Generate(ctx context.Context, input Input, opts Options) ([]models.Bookmark, error) {
	opts.applyDefaults()

	// Try the structure tree source first if requested.
	if opts.PreferStructureTree {
		mcidTextByPage := buildMCIDTextIndex(ctx, input.Pages)
		bookmarks, err := GenerateFromStructTree(ctx, input.Doc, input.PageRefToNum, mcidTextByPage, opts)
		if err == nil && len(bookmarks) > 0 {
			return bookmarks, nil
		}
	}

	// Fall back to font-size heuristic.
	return GenerateFromFontSize(ctx, input.Pages, opts), nil
}

// buildMCIDTextIndex groups text elements by (pageNum, MCID) and reconstructs
// the text for each marked content section.
func buildMCIDTextIndex(_ context.Context, pages []pageTextElements) map[int]map[int]string {
	result := make(map[int]map[int]string, len(pages))
	for _, p := range pages {
		pageMap := make(map[int]string)
		// Group elements by MCID.
		groups := make(map[int][]models.TextElement)
		for _, el := range p.Elements {
			if el.MCID < 0 {
				continue
			}
			groups[el.MCID] = append(groups[el.MCID], el)
		}
		// Concatenate text per MCID in content-stream order.
		for mcid, elems := range groups {
			var text strings.Builder
			for i, el := range elems {
				if i > 0 {
					text.WriteString(" ")
				}
				text.WriteString(el.Text)
			}
			pageMap[mcid] = text.String()
		}
		result[p.PageNum] = pageMap
	}
	return result
}
