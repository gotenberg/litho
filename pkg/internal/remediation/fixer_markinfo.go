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

func matchMarkInfo(v compliance.Violation) bool {
	return descriptionContains(v, "MarkInfo")
}

func fixMarkInfo(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	catalogRef, ok := doc.Index.Trailer["Root"].(ast.PdfIndirectRef)
	if !ok {
		return nil, fmt.Errorf("trailer missing /Root")
	}
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return nil, fmt.Errorf("resolving catalog")
	}

	newCatalog := make(ast.PdfDict, len(catalogDict)+1)
	maps.Copy(newCatalog, catalogDict)
	// ISO 14289-1 §7.1 / ISO 32000-1 §14.7: a tagged document's catalog
	// requires /MarkInfo << /Marked true >>.
	newCatalog["MarkInfo"] = ast.PdfDict{"Marked": ast.PdfBool(true)}
	alloc.AllocAt(catalogRef.ObjectNum, newCatalog)

	return []string{"added /MarkInfo << /Marked true >> to catalog"}, nil
}
