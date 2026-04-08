package remediation

import (
	"context"
	"crypto/rand"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

func matchTrailerID(v compliance.Violation) bool {
	return descriptionContains(v, "file trailer dictionary shall contain the ID")
}

func fixTrailerID(_ context.Context, _ *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	if _, ok := doc.Index.Trailer["ID"]; ok {
		return nil, nil
	}
	id := make([]byte, 16)
	_, _ = rand.Read(id)
	doc.Index.Trailer["ID"] = ast.PdfArray{
		ast.PdfString(string(id)),
		ast.PdfString(string(id)),
	}
	return []string{"added /ID to trailer"}, nil
}
