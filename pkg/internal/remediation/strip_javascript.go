package remediation

import (
	"context"
	"fmt"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// namesJavaScriptRemoved is the fix description [stripActions] reports when it
// drops the catalog's /Names/JavaScript tree.
const namesJavaScriptRemoved = "removed /Names/JavaScript from catalog"

// StripJavaScript cuts every script out of the document's action entries and
// drops the document-level /Names/JavaScript tree, whatever profile the
// document claims, and returns how many it removed: one per action cut, and
// one for the tree.
//
// Redaction composes it. A script is program text a viewer runs (ISO 32000-1,
// 12.6.4.16), so it can build a removed word at run time from pieces no
// comparison of the file's bytes recognises ('SEC' + 'RET', an escape, a
// character code), and the scan that proves a redaction clean cannot clear
// it. Nothing a page draws depends on one: the cost is form calculation,
// formatting and validation, and any custom navigation.
//
// The actions cut are JavaScript actions and Rendition actions carrying /JS
// (Table 214), wherever the action entries of the document reach them: the
// catalog's /OpenAction and /AA, each page's /AA, each annotation's /A and
// /AA, each form field's /AA and each outline item's /A, with every action
// their /Next chains hold (12.6.2, Table 193). A permitted action keeps its
// place, and one following a script moves up into the script's place, as the
// PDF/A JavaScript fixer splices them (see [actionSplice]). A script a 3D
// stream or an XFA form carries is not an action and is not cut; the scan
// still compares its text.
func StripJavaScript(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) (int, error) {
	s := newActionSpliceFunc(alloc, doc, func(ctx context.Context, action ast.PdfDict) bool {
		return isScriptAction(ctx, doc, action)
	})
	fixes, err := stripActions(ctx, alloc, doc, s, true)
	if err != nil {
		return 0, fmt.Errorf("stripping JavaScript: %w", err)
	}
	removed := s.cut
	if slices.Contains(fixes, namesJavaScriptRemoved) {
		removed++
	}
	return removed, nil
}

// isScriptAction reports whether action runs a script: a JavaScript action
// (ISO 32000-1, 12.6.4.16, Table 217) or a Rendition action whose /JS holds
// one (12.6.4.13, Table 214).
func isScriptAction(ctx context.Context, doc *document.Document, action ast.PdfDict) bool {
	s, _ := document.ResolveName(ctx, doc, action["S"])
	switch s {
	case "JavaScript":
		return true
	case "Rendition":
		_, hasJS := action["JS"]
		return hasJS
	}
	return false
}
