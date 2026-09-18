package mutations

import (
	"context"
	"fmt"
	"maps"
	"sort"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// ScrubOutlineTitles empties the outline titles that quote one of tokens, and
// returns the object numbers it rewrote, ascending.
//
// Bookmark titles routinely repeat the heading they point at, so a document
// whose page text was cleared can still spell it in the outline panel of every
// viewer that opens it. The outline is rooted at the catalog rather than at a
// page, so this walks the tree from /Outlines rather than page by page.
//
// The title is emptied, not deleted. /Title is required on an outline item
// (ISO 32000-1, Table 153), so removing the entry would leave an item the spec
// does not allow; the leak is the text it carries, not the item itself. The
// bookmark survives, points where it did, and says nothing.
//
// It refuses when an item quoting a token is not an indirect object, reporting
// [ErrOutlineItemNotAddressable]: there is nothing to replace, and passing over
// it would leave the title in the file.
func ScrubOutlineTitles(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	tokens []string,
) ([]int, error) {
	// Items are rewritten one at a time, so refusing on a later one would
	// otherwise leave the earlier ones queued. Restore is lossless because
	// every edit is a fresh dictionary handed to AllocAt.
	snap := alloc.Snapshot()
	scrubbed, err := scrubOutlineTitles(ctx, alloc, doc, tokens)
	if err != nil {
		alloc.Restore(snap)
		return nil, err
	}
	return scrubbed, nil
}

func scrubOutlineTitles(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	tokens []string,
) ([]int, error) {
	for _, tok := range tokens {
		if tok == "" {
			return nil, fmt.Errorf("%w: an empty token matches every title", ErrScanTokenEmpty)
		}
	}
	if len(tokens) == 0 {
		return nil, nil
	}

	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil || catalog == nil {
		return nil, nil
	}
	root, err := document.ResolveDict(ctx, doc, catalog.Dict["Outlines"])
	if err != nil || root == nil {
		// No outline tree, so no bookmark can be quoting anything.
		return nil, nil
	}

	scrubbed := make(map[int]bool)
	visited := make(map[int]bool)
	if err := scrubOutlineItems(ctx, alloc, doc, root["First"], tokens, visited, scrubbed, 0); err != nil {
		return nil, err
	}

	out := make([]int, 0, len(scrubbed))
	for objNum := range scrubbed {
		out = append(out, objNum)
	}
	sort.Ints(out)
	return out, nil
}

// scrubOutlineItems walks one sibling chain and recurses into each item's
// children, mirroring the traversal [pruneOutlineItems] uses.
//
// visited is checked before it is marked and a repeat ends the chain: an
// outline is a tree threaded by /Next and /Parent, so an item reached twice
// means a cycle, and without the guard a crafted /Next ring never returns.
func scrubOutlineItems(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	itemObj ast.PdfObject,
	tokens []string,
	visited map[int]bool,
	scrubbed map[int]bool,
	depth int,
) error {
	if depth > maxOutlineDepth {
		return nil
	}
	for current := itemObj; current != nil; {
		if err := ctx.Err(); err != nil {
			return err
		}
		ref, isRef := current.(ast.PdfIndirectRef)
		if !isRef {
			// A direct item cannot be replaced, and the walk cannot continue
			// past it either: /Next lives inside it, so every later sibling is
			// reachable only through an item this pass cannot rewrite. Checking
			// only whether this one quotes a token and then returning would end
			// the chain silently, hiding the titles of every sibling behind it.
			// Refusing names the problem instead of reporting a clean outline.
			return fmt.Errorf("%w: a direct item ends the chain", ErrOutlineItemNotAddressable)
		}
		if visited[ref.ObjectNum] {
			return nil
		}
		visited[ref.ObjectNum] = true

		item, err := document.ResolveDict(ctx, doc, current)
		if err != nil || item == nil {
			return nil
		}
		if outlineTitleQuotes(ctx, doc, item, tokens) {
			newItem := make(ast.PdfDict, len(item))
			maps.Copy(newItem, item)
			// Emptied rather than deleted: /Title is required (Table 153).
			newItem["Title"] = document.EncodePdfString("")
			if !alloc.AllocAt(ref.ObjectNum, newItem) {
				return fmt.Errorf("%w: object %d", ErrOutlineItemNotAddressable, ref.ObjectNum)
			}
			scrubbed[ref.ObjectNum] = true
		}

		if err := scrubOutlineItems(ctx, alloc, doc, item["First"], tokens, visited, scrubbed, depth+1); err != nil {
			return err
		}
		current = item["Next"]
	}
	return nil
}

// outlineTitleQuotes reports whether the item's /Title contains one of tokens.
func outlineTitleQuotes(ctx context.Context, doc *document.Document, item ast.PdfDict, tokens []string) bool {
	raw, present := item["Title"]
	if !present {
		return false
	}
	resolved, err := document.ResolveObject(ctx, doc, raw)
	if err != nil {
		return false
	}
	title, isStr := resolved.(ast.PdfString)
	if !isStr {
		return false
	}
	text := document.DecodePdfString(title)
	for _, tok := range tokens {
		if strings.Contains(text, tok) {
			return true
		}
	}
	return false
}
