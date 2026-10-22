package page

import (
	"sync"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// maxCachedFormOperators bounds the operators of the parsed form XObjects
// one [formCache] keeps. Anti-DoS guard, not spec-mandated: a page that paints
// a form many times, as a two-up page paints the same form twice, parses it
// once, and past the bound a form is parsed again on each use. 1<<20 is
// twice the operators of the heaviest real page, 608,043 on page 1 of pdfjs
// issue21089.pdf (see [maxRenderOperators]).
const maxCachedFormOperators = 1 << 20

// formCache keeps the parsed content of form XObjects by reference: the
// renderer parses a form each time it paints it otherwise. The operations are
// shared, never changed.
//
// A formCache is safe for concurrent use.
type formCache struct {
	mu    sync.Mutex
	ops   map[ast.PdfIndirectRef][]content.Operation
	count int
}

func newFormCache() *formCache {
	return &formCache{ops: make(map[ast.PdfIndirectRef][]content.Operation)}
}

// get returns the operations kept for ref.
func (c *formCache) get(ref ast.PdfIndirectRef) ([]content.Operation, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ops, ok := c.ops[ref]
	return ops, ok
}

// put keeps ops for ref while the cache holds fewer than
// maxCachedFormOperators operators.
func (c *formCache) put(ref ast.PdfIndirectRef, ops []content.Operation) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.ops[ref]; ok || c.count+len(ops) > maxCachedFormOperators {
		return
	}
	c.ops[ref] = ops
	c.count += len(ops)
}

// formContent parses the content of the form XObject stream, ref, as
// [content.ParsePageContent] reads a form for the renderer, once per render,
// or once across the renders sharing [Options.Shared].
func (r *renderer) formContent(ref ast.PdfIndirectRef, stream ast.PdfStream) ([]content.Operation, error) {
	if ref.ObjectNum == 0 {
		return content.ParsePageContent(r.ctx, r.doc, stream, content.SplitGluedOperators(), content.InlineImageData(), content.ViewerFilterChains())
	}
	if ops, ok := r.forms.get(ref); ok {
		return ops, nil
	}
	ops, err := content.ParsePageContent(r.ctx, r.doc, stream, content.SplitGluedOperators(), content.InlineImageData(), content.ViewerFilterChains())
	if err != nil {
		return nil, err
	}
	r.forms.put(ref, ops)
	return ops, nil
}
