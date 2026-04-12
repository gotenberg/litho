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
	// Clause 6.1.3 t01/t04: trailer /ID requirement.
	// Exclude t02 (Encrypt) which is handled by catalog-cleanup.
	return v.Rule.Clause == "6.1.3" && v.Rule.TestNumber != 2
}

func fixTrailerID(_ context.Context, _ *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	if _, ok := doc.Index.Trailer["ID"]; ok {
		return nil, nil
	}
	id := make([]byte, 16)
	// crypto/rand.Read never fails on supported platforms; the Go
	// runtime aborts on getrandom/EAGAIN. Suppressing the error here
	// is the same contract Go's stdlib relies on.
	_, _ = rand.Read(id)
	doc.Index.Trailer["ID"] = ast.PdfArray{
		ast.PdfString(string(id)),
		ast.PdfString(string(id)),
	}
	return []string{"added /ID to trailer"}, nil
}
