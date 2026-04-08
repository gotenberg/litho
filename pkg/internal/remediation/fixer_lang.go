package remediation

import (
	"context"
	"fmt"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

func matchCatalogLang(v compliance.Violation) bool {
	return descriptionContains(v, "Lang") && (descriptionContains(v, "natural language") || descriptionContains(v, "lang entry") || descriptionContains(v, "primary language"))
}

func fixCatalogLang(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, opts Options, _ []compliance.Violation, _ string) ([]string, error) {
	catalogRef, ok := doc.Index.Trailer["Root"].(ast.PdfIndirectRef)
	if !ok {
		return nil, fmt.Errorf("trailer missing /Root")
	}
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return nil, fmt.Errorf("resolving catalog")
	}

	if _, ok := catalogDict["Lang"]; ok {
		return nil, nil // already present
	}

	lang := opts.Lang
	if lang == "" {
		lang = "en"
	}

	newCatalog := make(ast.PdfDict, len(catalogDict)+1)
	maps.Copy(newCatalog, catalogDict)
	newCatalog["Lang"] = ast.PdfString(lang)
	alloc.AllocAt(catalogRef.ObjectNum, newCatalog)

	return []string{fmt.Sprintf("set catalog /Lang to %q", lang)}, nil
}
