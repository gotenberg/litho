package mutations

import (
	"context"
	"fmt"
	"maps"
	"slices"
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
	if slices.Contains(tokens, "") {
		return nil, fmt.Errorf("%w: an empty token matches every title", ErrScanTokenEmpty)
	}
	if len(tokens) == 0 {
		return nil, nil
	}

	// The outline as earlier stages staged it: an outline edit queues a new
	// tree at new object numbers, and walking the source's instead left every
	// title of the staged one in the output.
	catalogRef, ok := CatalogRef(ctx, doc)
	if !ok {
		return nil, nil
	}
	catalog, err := LiveCatalogDict(ctx, alloc, doc, catalogRef)
	if err != nil || catalog == nil {
		return nil, nil //nolint:nilerr // No readable catalog holds no outline to scrub.
	}
	root, err := resolveDictAllocFirst(ctx, alloc, doc, catalog["Outlines"])
	if err != nil || root == nil {
		// No outline tree, so no bookmark can be quoting anything.
		return nil, nil //nolint:nilerr // A deliberate fallback, see above.
	}

	scrubbed := make(map[int]bool)
	visited := make(map[int]bool)
	if err := scrubOutlineChildren(ctx, alloc, doc, root, tokens, visited, scrubbed, 0); err != nil {
		return nil, err
	}

	out := make([]int, 0, len(scrubbed))
	for objNum := range scrubbed {
		out = append(out, objNum)
	}
	sort.Ints(out)
	return out, nil
}

// scrubOutlineChildren walks the children of one outline node, the outline
// dictionary or an item.
//
// The children are a chain doubly linked by /Next and /Prev, with /First and
// /Last naming its ends (ISO 32000-1, Tables 152 and 153). The walk takes it
// from /First. When an item on the way does not resolve, the items after it
// are out of reach through /Next but not out of the output: the rewrite writes
// the unresolvable item as null and copies the rest, reached from /Last. So the
// chain is then taken again from /Last through /Prev, up to the items the
// first pass reached, and a title past the gap is cleared rather than left for
// the verification scan to refuse.
func scrubOutlineChildren(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	node ast.PdfDict,
	tokens []string,
	visited map[int]bool,
	scrubbed map[int]bool,
	depth int,
) error {
	broken, err := scrubOutlineItems(ctx, alloc, doc, node["First"], "Next", tokens, visited, scrubbed, depth)
	if err != nil || !broken {
		return err
	}
	_, err = scrubOutlineItems(ctx, alloc, doc, node["Last"], "Prev", tokens, visited, scrubbed, depth)
	return err
}

// scrubOutlineItems walks one sibling chain from itemObj along link, /Next or
// /Prev, and recurses into each item's children, mirroring the traversal
// [pruneOutlineItems] uses. It reports whether the chain ended on an item that
// does not resolve, which leaves the items past it unwalked.
//
// visited is checked before it is marked and a repeat ends the chain: an
// outline is a tree threaded by /Next and /Parent, so an item reached twice
// means a cycle, and without the guard a crafted /Next ring never returns.
func scrubOutlineItems(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	itemObj ast.PdfObject,
	link ast.PdfName,
	tokens []string,
	visited map[int]bool,
	scrubbed map[int]bool,
	depth int,
) (bool, error) {
	if depth > maxOutlineDepth {
		return false, nil
	}
	for current := itemObj; current != nil; {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if _, isNull := current.(ast.PdfNull); isNull {
			return false, nil
		}
		ref, isRef := current.(ast.PdfIndirectRef)
		if _, isDict := current.(ast.PdfDict); !isRef && !isDict {
			// No item at all: ISO 32000-1, Table 153 makes each link an
			// indirect reference to an outline item, and a number or a name
			// is neither. It holds no title and no link, so it ends this
			// chain the way an unresolvable item does.
			return true, nil
		}
		if !isRef {
			// A direct item cannot be replaced, and the walk cannot continue
			// past it either: /Next lives inside it, so every later sibling is
			// reachable only through an item this pass cannot rewrite. Checking
			// only whether this one quotes a token and then returning would end
			// the chain silently, hiding the titles of every sibling behind it.
			// Refusing names the problem instead of reporting a clean outline.
			return false, fmt.Errorf("%w: a direct item ends the chain", ErrOutlineItemNotAddressable)
		}
		if visited[ref.ObjectNum] {
			return false, nil
		}
		visited[ref.ObjectNum] = true

		item, err := resolveDictAllocFirst(ctx, alloc, doc, current)
		if err != nil || item == nil {
			// An unresolvable item is null (ISO 32000-1, 7.3.10), and the
			// rewrite writes it so: nothing of it reaches the output. The
			// siblings past it still do, and the caller walks them from the
			// other end.
			return true, nil //nolint:nilerr // A deliberate fallback, see above.
		}
		if outlineTitleQuotes(ctx, alloc, doc, item, tokens) {
			newItem := make(ast.PdfDict, len(item))
			maps.Copy(newItem, item)
			// Emptied rather than deleted: /Title is required (ISO 32000-1,
			// Table 153).
			newItem["Title"] = document.EncodePdfString("")
			if !alloc.AllocAt(ref.ObjectNum, newItem) {
				return false, fmt.Errorf("%w: object %d", ErrOutlineItemNotAddressable, ref.ObjectNum)
			}
			scrubbed[ref.ObjectNum] = true
		}

		if err := scrubOutlineChildren(ctx, alloc, doc, item, tokens, visited, scrubbed, depth+1); err != nil {
			return false, err
		}
		current = item[link]
	}
	return false, nil
}

// outlineTitleQuotes reports whether the item's /Title, read from alloc first,
// contains one of tokens.
func outlineTitleQuotes(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, item ast.PdfDict, tokens []string) bool {
	raw, present := item["Title"]
	if !present {
		return false
	}
	resolved, err := resolveAllocFirst(ctx, alloc, doc, raw)
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
