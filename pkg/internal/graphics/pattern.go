package graphics

import (
	"context"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// resolvePatternType looks up a pattern name in the current resources and
// returns "Tiling" (PatternType 1) or "Shading" (PatternType 2), or
// "Pattern" if the type cannot be determined.
func (m *Machine) resolvePatternType(name string) string {
	if m.Resources == nil {
		return "Pattern"
	}

	patObj, ok := m.Resources["Pattern"]
	if !ok {
		return "Pattern"
	}

	patDict, err := document.ResolveDict(m.Ctx, m.Doc, patObj)
	if err != nil {
		return "Pattern"
	}
	if patDict == nil {
		return "Pattern"
	}

	entryObj, ok := patDict[ast.PdfName(name)]
	if !ok {
		return "Pattern"
	}

	entryRes, err := document.ResolveObject(m.Ctx, m.Doc, entryObj)
	if err != nil {
		return "Pattern"
	}

	// Pattern can be a stream (Tiling) or dict (Shading).
	switch v := entryRes.(type) {
	case ast.PdfStream:
		return classifyPattern(m.Ctx, m.Doc, v.Dict)
	case ast.PdfDict:
		return classifyPattern(m.Ctx, m.Doc, v)
	}

	return "Pattern"
}

// classifyPattern examines a pattern dictionary's /PatternType to determine
// whether it is a Tiling pattern (1) or a Shading pattern (2).
func classifyPattern(ctx context.Context, doc *document.Document, dict ast.PdfDict) string {
	ptObj, ok := dict["PatternType"]
	if !ok {
		return "Pattern"
	}

	ptRes, err := document.ResolveObject(ctx, doc, ptObj)
	if err != nil {
		return "Pattern"
	}

	if ptInt, ok := ptRes.(ast.PdfInt); ok {
		switch int(ptInt) {
		case 1:
			return "Tiling"
		case 2:
			return "Shading"
		}
	}

	return "Pattern"
}
