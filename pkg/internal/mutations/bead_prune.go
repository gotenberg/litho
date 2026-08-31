package mutations

import (
	"context"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// maxBeadsPerThread caps the walk around one article's bead ring. Anti-DoS
// guard, not spec-mandated: the ring is circular by construction (ISO 32000-1,
// 12.4.3), so a crafted /N that never returns to the first bead must not spin
// forever. The walk also tracks visited beads, so this only bounds ring length.
const maxBeadsPerThread = 100000

// pruneArticleBeads unlinks the article beads that sit on pages the operation
// removed, keeping each thread a valid ring.
//
// A bead names its page in /P and chains to its neighbours through /N and /V
// (ISO 32000-1, 12.4.3), and the thread names its first bead in /F. A bead left
// in the ring therefore keeps its page in the output, which is how govdocs
// thread_100_100184.pdf held on to 3649 bytes of the page it had just deleted.
//
// Splicing rather than dropping: unlike split, delete keeps the catalog, so
// /Threads still lists the articles and they are meant to keep working. A thread
// that loses every bead goes, since an article with no beads has nothing to say.
func pruneArticleBeads(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	catalogDict ast.PdfDict,
	removed map[int]bool,
) {
	threadsObj, hasThreads := catalogDict["Threads"]
	if !hasThreads {
		return
	}
	threads, err := document.ResolveArray(ctx, doc, threadsObj)
	if err != nil {
		return
	}

	keptThreads := make(ast.PdfArray, 0, len(threads))
	changed := false
	for _, threadObj := range threads {
		if spliceThreadBeads(ctx, alloc, doc, threadObj, removed) {
			changed = true
			continue
		}
		keptThreads = append(keptThreads, threadObj)
	}
	if !changed {
		return
	}
	if ref, isRef := threadsObj.(ast.PdfIndirectRef); isRef {
		alloc.AllocAt(ref.ObjectNum, keptThreads)
		return
	}
	catalogRef, ok := CatalogRef(ctx, doc)
	if !ok {
		return
	}
	// pruneCatalogDestinations may already have queued a catalog. Building this
	// one from the original would silently undo the destinations it dropped.
	base := catalogDict
	if pending, queued := alloc.Pending()[catalogRef.ObjectNum].(ast.PdfDict); queued {
		base = pending
	}
	newCatalog := make(ast.PdfDict, len(base))
	maps.Copy(newCatalog, base)
	newCatalog["Threads"] = keptThreads
	alloc.AllocAt(catalogRef.ObjectNum, newCatalog)
}

// spliceThreadBeads rebuilds one thread's ring from the beads whose pages
// survived, reporting whether the thread itself must be dropped.
func spliceThreadBeads(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	threadObj ast.PdfObject,
	removed map[int]bool,
) bool {
	thread, err := document.ResolveDict(ctx, doc, threadObj)
	if err != nil || thread == nil {
		return false
	}

	ring := readBeadRing(ctx, doc, thread["F"])
	survivors := make([]ast.PdfIndirectRef, 0, len(ring))
	for _, bead := range ring {
		dict, resolveErr := document.ResolveDict(ctx, doc, bead)
		if resolveErr != nil || dict == nil {
			continue
		}
		if pageRef, isRef := dict["P"].(ast.PdfIndirectRef); isRef && removed[pageRef.ObjectNum] {
			continue
		}
		survivors = append(survivors, bead)
	}
	if len(survivors) == len(ring) {
		return false
	}
	if len(survivors) == 0 {
		return true
	}

	for i, bead := range survivors {
		dict, resolveErr := document.ResolveDict(ctx, doc, bead)
		if resolveErr != nil || dict == nil {
			continue
		}
		newBead := make(ast.PdfDict, len(dict))
		maps.Copy(newBead, dict)
		newBead["N"] = survivors[(i+1)%len(survivors)]
		newBead["V"] = survivors[(i-1+len(survivors))%len(survivors)]
		alloc.AllocAt(bead.ObjectNum, newBead)
	}

	if ref, isRef := threadObj.(ast.PdfIndirectRef); isRef {
		newThread := make(ast.PdfDict, len(thread))
		maps.Copy(newThread, thread)
		newThread["F"] = survivors[0]
		alloc.AllocAt(ref.ObjectNum, newThread)
	}
	return false
}

// readBeadRing walks a thread's beads from its first one, following /N until it
// comes back round.
func readBeadRing(ctx context.Context, doc *document.Document, first ast.PdfObject) []ast.PdfIndirectRef {
	var ring []ast.PdfIndirectRef
	visited := make(map[int]bool)
	current := first
	for range maxBeadsPerThread {
		ref, isRef := current.(ast.PdfIndirectRef)
		if !isRef || visited[ref.ObjectNum] {
			return ring
		}
		visited[ref.ObjectNum] = true
		ring = append(ring, ref)

		dict, err := document.ResolveDict(ctx, doc, current)
		if err != nil || dict == nil {
			return ring
		}
		current = dict["N"]
	}
	return ring
}
