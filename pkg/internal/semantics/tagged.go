package semantics

import (
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// IsTagged reports whether the document is a Tagged PDF: its catalog holds a
// structure tree root and a mark information dictionary whose Marked entry is
// true (ISO 32000-1, 14.8.1 and 14.7.1).
//
// This is introspection, not a verdict: it reads what the document declares
// and does not check the structure tree against the content.
func IsTagged(ctx context.Context, doc *document.Document) (bool, error) {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return false, err
	}

	if _, ok := catalog.Dict["StructTreeRoot"]; !ok {
		return false, nil
	}

	miObj, ok := catalog.Dict["MarkInfo"]
	if !ok {
		return false, nil
	}

	mi, err := document.ResolveDict(ctx, doc, miObj)
	if err != nil {
		return false, fmt.Errorf("resolving /MarkInfo: %w", err)
	}

	markedObj, ok := mi["Marked"]
	if !ok {
		return false, nil
	}

	marked, err := document.ResolveBool(ctx, doc, markedObj)
	if err != nil {
		return false, fmt.Errorf("resolving /MarkInfo /Marked: %w", err)
	}

	return bool(marked), nil
}
