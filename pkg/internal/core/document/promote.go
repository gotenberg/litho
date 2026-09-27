package document

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// PromoteDirectKids gives every page tree node written directly in a /Kids
// array, a page or an intermediate node, an object number of its own, and
// stages the node listing it with a reference in its place. It does the same
// for a page tree root written directly in the catalog, staging the catalog
// with a reference to it, and for a catalog written directly in the trailer,
// which the trailer then names by reference. Every object it stages is the one
// d reads at that number from then on ([Document.Resolve]), and one a writer's
// output has to carry ([Document.PromotedObjects]).
//
// ISO 32000-1, 7.7.3.2, Table 29 makes /Kids an array of indirect references,
// and what names a page names it by one: an annotation's /P (Table 164), a
// structure element's /Pg (Table 323), an explicit destination (12.3.2.2). A
// writer also replaces a page it edits at its object number. A page written
// directly in /Kids has none, so a writer edited it nowhere: a stamp left it
// unstamped, and a page tree rebuilt from the pages' references listed it as
// object 0, which every reader drops. Giving the page a number of its own
// repairs the tree the way the table asks, and every edit then reaches the
// page as it reaches any other. The root and the catalog are the same case one
// and two levels up: ISO 32000-1, 7.7.2, Table 28 makes the catalog's /Pages
// an indirect reference, and 7.5.5, Table 15 makes the trailer's /Root one. A
// writer rebuilding the page tree replaces the root at its number, and one
// editing the catalog replaces the catalog at its number, so a root or a
// catalog with none failed every such edit.
//
// The numbers follow the highest one the file uses
// ([Document.HighestObjectNumber]): the catalog's first, then the root's, then
// the kids' in the order a walk of the tree from its root reaches them, so the
// same file always stages the same objects. A kid keeps every entry it states.
// A node is staged only when a kid of its /Kids moved, and an array that /Kids
// names by reference is staged in the node's place. A lone dictionary written
// as /Kids in place of the array is left as it is.
//
// It runs once per Document, and a later call returns at once. A done ctx
// stops it before it stages anything, and a later call runs it again. It is
// part of a writer's setup: call it on a fork ([Document.Fork]) the writer
// owns, before anything else reads it.
//
// # Corpus evidence
//
//   - All corpora: four files write a page directly in /Kids, pdfjs
//     issue9105_other.pdf and issue9540.pdf one each, litho kidsrepair_1b.pdf
//     two in an array its node holds inline and litho kidsarray_2b.pdf two in
//     an array its node names by reference, all under a root the catalog
//     names by reference. No file writes an intermediate node, the root or a
//     lone /Kids dictionary directly: veraPDF 1.30.2 refuses a file holding
//     an intermediate node written directly in /Kids.
//   - litho kidsrepair_1b.pdf: a text stamp on every page left its two direct
//     pages unstamped, and moving its first page to the end wrote a tree
//     listing them as object 0, which litho read as one page of four.
//   - pdfjs issue9105_other.pdf: is the one file whose trailer writes the
//     catalog directly, and inserting pages, setting the metadata and the
//     PDF/UA-1 remediation each failed on it for want of a catalog reference.
func (d *Document) PromoteDirectKids(ctx context.Context) error {
	if d.promotedDone.Load() {
		return nil
	}
	catalog, err := ResolveCatalog(ctx, d)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("promoting the page tree's direct kids: %w", ctxErr)
		}
		// A document with no catalog litho can read has no page tree to
		// promote, and reads none either.
		d.promotedDone.Store(true)
		return nil
	}
	directCatalog := d.writesCatalogDirectly(ctx)
	directRoot, rootIsDict := catalog.Dict["Pages"].(ast.PdfDict)
	root, rootIsRef := catalog.Dict["Pages"].(ast.PdfIndirectRef)
	rootIsRef = rootIsRef && root.ObjectNum > 0

	// The number the catalog is staged at when the root it names moves: a new
	// one when the trailer writes it directly, its own otherwise. A catalog
	// the reader synthesised names its root by reference (see scanForCatalog),
	// so one with no number of its own leaves nothing to stage.
	var catalogRef ast.PdfIndirectRef
	if rootIsDict && !directCatalog {
		catalogRef, rootIsDict = d.CatalogRef(ctx)
	}
	if !directCatalog && !rootIsDict && !rootIsRef {
		d.promotedDone.Store(true)
		return nil
	}
	next := d.HighestObjectNumber(ctx) + 1
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("promoting the page tree's direct kids: %w", err)
	}
	p := &kidPromoter{doc: d, next: next, staged: make(map[int]ast.PdfObject), done: make(map[int]bool)}
	if directCatalog {
		catalogRef = ast.PdfIndirectRef{ObjectNum: p.next}
		p.next++
	}

	stagedCatalog := catalog.Dict
	switch {
	case rootIsDict:
		rootRef := ast.PdfIndirectRef{ObjectNum: p.next}
		p.next++
		// Staged before its kids are walked, as a kid is (see
		// [kidPromoter.kids]), so a kid written directly in it takes the next
		// number after it.
		p.staged[rootRef.ObjectNum] = directRoot
		updated, changed, err := p.kids(ctx, directRoot, rootRef, 0)
		if err != nil {
			return fmt.Errorf("promoting the page tree's direct kids: %w", err)
		}
		if changed {
			p.staged[rootRef.ObjectNum] = updated
		}
		stagedCatalog = maps.Clone(catalog.Dict)
		stagedCatalog["Pages"] = rootRef
	case rootIsRef:
		if err := p.node(ctx, root, 0); err != nil {
			return fmt.Errorf("promoting the page tree's direct kids: %w", err)
		}
	}
	if directCatalog || rootIsDict {
		p.staged[catalogRef.ObjectNum] = stagedCatalog
	}
	if len(p.staged) > 0 {
		d.Logger.Debug("promoted the page tree's direct kids",
			slog.Int("objects", len(p.staged)),
			slog.Int("first", next),
		)
		d.promoted.Store(&p.staged)
	}
	if directCatalog {
		// The trailer is d's own (see [Document.Fork]), and the entry is
		// replaced, never the dictionary it held.
		d.Index.Trailer["Root"] = catalogRef
	}
	d.promotedDone.Store(true)
	return nil
}

// writesCatalogDirectly reports whether the trailer writes the catalog the
// reader reads as a dictionary in place of a reference: its /Root is a
// dictionary naming a page tree, which [ResolveCatalog] takes before any
// recovery.
func (d *Document) writesCatalogDirectly(ctx context.Context) bool {
	root, ok := d.Index.Trailer["Root"].(ast.PdfDict)
	return ok && catalogHasPages(ctx, d, root)
}

// PromotedObjects returns the objects [Document.PromoteDirectKids] staged, by
// object number, nil when it staged none. The map is a copy; the objects are
// the ones d resolves, which the caller must not modify.
func (d *Document) PromotedObjects() map[int]ast.PdfObject {
	staged := d.promoted.Load()
	if staged == nil {
		return nil
	}
	return maps.Clone(*staged)
}

// promotedObject returns the object [Document.PromoteDirectKids] staged at
// objNum, if any.
func (d *Document) promotedObject(objNum int) (ast.PdfObject, bool) {
	staged := d.promoted.Load()
	if staged == nil {
		return nil, false
	}
	obj, ok := (*staged)[objNum]
	return obj, ok
}

// highestPromoted returns the highest object number [Document.PromoteDirectKids]
// staged, 0 when it staged none.
func (d *Document) highestPromoted() int {
	staged := d.promoted.Load()
	if staged == nil {
		return 0
	}
	highest := 0
	for objNum := range *staged {
		highest = max(highest, objNum)
	}
	return highest
}

// kidPromoter walks a page tree for [Document.PromoteDirectKids]. staged holds
// what it stages, next the number the next promoted kid takes, and done the
// indirect nodes already walked: a page tree is a strict tree (ISO 32000-1,
// 7.7.3.2), so a node reached again, through a cycle or a second parent, is
// walked once, as [traversePageTree] walks it.
type kidPromoter struct {
	doc    *Document
	next   int
	staged map[int]ast.PdfObject
	done   map[int]bool
}

// node walks the node at ref, and stages it when a kid of its /Kids moved.
func (p *kidPromoter) node(ctx context.Context, ref ast.PdfIndirectRef, depth int) error {
	if p.done[ref.ObjectNum] {
		return nil
	}
	p.done[ref.ObjectNum] = true
	dict, err := ResolveDict(ctx, p.doc, ref)
	if err != nil || dict == nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		// The reader prunes a kid that does not resolve (see
		// traversePageTree), and there is nothing under it to promote.
		return nil
	}
	updated, changed, err := p.kids(ctx, dict, ref, depth)
	if err != nil {
		return err
	}
	if changed {
		p.staged[ref.ObjectNum] = updated
	}
	return nil
}

// kids walks the /Kids of dict, the node at self, promotes each kid written
// directly in it, and returns dict as it has to be staged, with changed true
// when a kid moved and the /Kids array is dict's own. An array /Kids names by
// reference is staged at its own number, and dict is left as it is.
func (p *kidPromoter) kids(ctx context.Context, dict ast.PdfDict, self ast.PdfIndirectRef, depth int) (ast.PdfDict, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	// The same bound, and the same test for a node, as the reader's walk:
	// a node deeper than it reads, or one it reads as a page, holds no kid
	// it reads.
	if depth > maxPageTreeDepth || inferPageTreeNodeType(ctx, p.doc, dict) != "Pages" {
		return dict, false, nil
	}
	kidsObj, ok := dict["Kids"]
	if !ok {
		return dict, false, nil
	}
	resolved, err := ResolveObject(ctx, p.doc, kidsObj)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, false, ctxErr
		}
		return dict, false, nil
	}
	kids, isArray := resolved.(ast.PdfArray)
	if !isArray {
		// A lone kid in place of the array, which the reader tolerates (see
		// traversePageTree), is walked when it is a reference.
		if ref, isRef := kidsObj.(ast.PdfIndirectRef); isRef {
			if _, isDict := resolved.(ast.PdfDict); isDict {
				return dict, false, p.node(ctx, ref, depth+1)
			}
		}
		return dict, false, nil
	}

	var promoted ast.PdfArray
	for i, kid := range kids {
		switch k := kid.(type) {
		case ast.PdfIndirectRef:
			if k.ObjectNum > 0 {
				if err := p.node(ctx, k, depth+1); err != nil {
					return nil, false, err
				}
			}
		case ast.PdfDict:
			ref := ast.PdfIndirectRef{ObjectNum: p.next}
			p.next++
			// Staged before its own kids are walked, so a kid written
			// directly in it takes the next number after it.
			p.staged[ref.ObjectNum] = k
			updated, changed, err := p.kids(ctx, k, ref, depth+1)
			if err != nil {
				return nil, false, err
			}
			if changed {
				p.staged[ref.ObjectNum] = updated
			}
			if promoted == nil {
				promoted = slices.Clone(kids)
			}
			promoted[i] = ref
		}
	}
	if promoted == nil {
		return dict, false, nil
	}
	if arrayRef, isRef := kidsObj.(ast.PdfIndirectRef); isRef && arrayRef.ObjectNum > 0 {
		p.staged[arrayRef.ObjectNum] = promoted
		return dict, false, nil
	}
	updated := maps.Clone(dict)
	updated["Kids"] = promoted
	p.doc.Logger.Debug("promoted direct page tree kids",
		slog.Int("node", self.ObjectNum),
	)
	return updated, true, nil
}
