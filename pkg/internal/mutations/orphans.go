package mutations

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// maxFreeGeneration is the generation an object number keeps once freed at
// it: ISO 32000-1, 7.5.4 never reuses a number whose free entry reaches
// 65535, so freeing it again cannot raise the generation.
const maxFreeGeneration = 65535

// OrphanedObjects returns the objects an incremental update of doc leaves
// nothing referencing, each with the generation its free entry carries: the
// generation the source gives it plus one (ISO 32000-1, 7.5.4). alloc holds
// the objects the update writes, and trailer the trailer it writes. The
// update writes none of them, and frees each (see
// [writer.IncrementalConfig.Freed]).
//
// An edit that replaces an object can drop the last reference to another: a
// remediation that points every output intent at one profile leaves the other
// profile's stream behind, and one that writes new XMP at a new number leaves
// the old metadata stream behind. A full rewrite copies only what the trailer
// reaches, so it never writes them. An incremental update keeps every byte of
// its source, and without a free entry the older cross-reference section keeps
// listing each such object as in use, a stream nothing shows that readers
// still enumerate. ISO 32000-1, 7.5.6 deletes an object in an update by giving
// it a free entry in the section the update appends.
//
// Only what the edit orphaned is freed: an object the update adds, or one a
// replaced object referenced in the source, or one only such objects lead to,
// that nothing the update leaves reaches from the trailer. A source object the
// update writes again is never freed, nor anything it leads to, even when
// nothing references it any more: its new version is what the edit made of
// it, a profile repaired or a file specification stripped of its embedded
// file. A free entry carries the next generation (ISO 32000-1, 7.5.4), so a
// reader that keeps the older section's entry at the old generation would
// read the version the edit removed; only an in-use entry replaces it for
// every reader. An object the source already left unreferenced stays as the
// source lists it, since the update did not delete it, unless an object it
// frees leads to it. When an object the update leaves cannot be read,
// whatever it references is unknown, so nothing is freed: freeing an object
// something still references would lose it. A cancelled ctx is the only
// error.
//
// # Corpus evidence
//
//   - verapdf: PDF_UA-2__8.9.2.4.10-t01-fail-a remediated aggressively at
//     PDF/A-4 and PDF/A-4e: remediation strips /EF from the file
//     specification (object 29) whose annotation it removes. Freeing the
//     stripped version made veraPDF 1.30.2 report ISO 19005-4 6.9 tests 3
//     and 4 on the source's object 29 0, which qpdf reads as freed.
func OrphanedObjects(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, trailer ast.PdfDict) (map[int]int, error) {
	pending := alloc.Pending()
	src := sourceGraph{doc: doc, promoted: doc.PromotedObjects()}
	dropped, err := dropsAReference(ctx, src, pending, trailer)
	if err != nil || !dropped {
		return nil, err
	}

	roots := appendRefs(nil, trailer)
	for num := range pending {
		if src.defines(num) {
			roots = append(roots, num)
		}
	}
	reached, complete, err := reachedObjects(ctx, src, pending, roots)
	if err != nil || !complete {
		return nil, err
	}

	var start []int
	for num := range pending {
		start = append(start, num)
		if !src.defines(num) {
			continue
		}
		before, err := src.read(ctx, num)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, fmt.Errorf("reading replaced object %d: %w", num, ctxErr)
			}
			continue
		}
		start = appendRefs(start, before)
	}

	orphaned, err := unreachedClosure(ctx, src, pending, reached, start)
	if err != nil || len(orphaned) == 0 {
		return nil, err
	}
	freed := make(map[int]int, len(orphaned))
	for _, num := range orphaned {
		freed[num] = min(doc.ObjectGeneration(num)+1, maxFreeGeneration)
	}
	return freed, nil
}

// dropsAReference reports whether the update can orphan anything: whether an
// object it writes over a source object no longer references something the
// source object did, or an object it adds is referenced by nothing it writes.
// A source object references none of the objects an update adds, so nothing
// else can reach one. Most updates drop nothing, a rotated page keeping its
// contents and parent, and this spares them a walk of the whole graph.
//
// A source object it leaves unreachable lost its last reference in an object
// it writes, so an update dropping nothing orphans none. Added objects naming
// only each other escape the check and are written as before: a missed
// orphan costs bytes, a wrong one would lose an object.
func dropsAReference(ctx context.Context, src sourceGraph, pending map[int]ast.PdfObject, trailer ast.PdfDict) (bool, error) {
	kept := make(map[int]bool)
	for _, num := range appendRefs(nil, trailer) {
		kept[num] = true
	}
	for _, obj := range pending {
		for _, num := range appendRefs(nil, obj) {
			kept[num] = true
		}
	}
	for num, obj := range pending {
		if !src.defines(num) {
			if !kept[num] {
				return true, nil
			}
			continue
		}
		before, err := src.read(ctx, num)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return false, fmt.Errorf("reading replaced object %d: %w", num, ctxErr)
			}
			continue
		}
		now := make(map[int]bool)
		for _, ref := range appendRefs(nil, obj) {
			now[ref] = true
		}
		for _, ref := range appendRefs(nil, before) {
			if !now[ref] {
				return true, nil
			}
		}
	}
	return false, nil
}

// unreachedClosure returns, ascending, the objects start names that reached
// leaves out, with the objects they lead to that it leaves out too, each read
// from pending where the update writes it and from doc otherwise. An object
// the update writes leads also where its source version did, since the update
// may have dropped that reference. Anything a reached object leads to is
// reached, so the walk never descends into one. An object neither the update
// nor doc defines is null (ISO 32000-1, 7.3.10) and is left out.
func unreachedClosure(ctx context.Context, src sourceGraph, pending map[int]ast.PdfObject, reached map[int]bool, start []int) ([]int, error) {
	seen := make(map[int]bool)
	var out []int
	stack := start
	for len(stack) > 0 {
		num := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if reached[num] || document.VisitedBefore(ast.PdfIndirectRef{ObjectNum: num}, seen) {
			continue
		}
		obj, written := pending[num]
		defined := src.defines(num)
		if !written && !defined {
			continue
		}
		out = append(out, num)
		if written {
			stack = appendRefs(stack, obj)
		}
		if !defined {
			continue
		}
		before, err := src.read(ctx, num)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, fmt.Errorf("reading orphaned object %d: %w", num, ctxErr)
			}
			continue
		}
		stack = appendRefs(stack, before)
	}
	slices.Sort(out)
	return out, nil
}

// reachedObjects returns the object numbers roots name and everything they
// lead to, an object read from pending when the update writes it and from doc
// otherwise.
// complete is false when an object doc defines cannot be read, so what it
// references is unknown.
//
// The walk keeps a stack instead of recursing, and visits each object number
// once, the arrays and dictionaries written as objects of their own included
// (see [document.VisitedBefore]), so neither a cycle nor a fan-out of
// references walks anything twice and no depth cap is needed: the number of
// objects the file and the update define bounds it.
func reachedObjects(ctx context.Context, src sourceGraph, pending map[int]ast.PdfObject, roots []int) (map[int]bool, bool, error) {
	reached := make(map[int]bool)
	stack := roots
	for len(stack) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, false, fmt.Errorf("walking the updated object graph: %w", err)
		}
		num := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if document.VisitedBefore(ast.PdfIndirectRef{ObjectNum: num}, reached) {
			continue
		}
		if obj, written := pending[num]; written {
			stack = appendRefs(stack, obj)
			continue
		}
		if !src.defines(num) {
			// ISO 32000-1, 7.3.10 reads a reference to an object the file
			// does not define as null, which references nothing.
			continue
		}
		obj, err := src.read(ctx, num)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, false, fmt.Errorf("walking the updated object graph: %w", ctxErr)
			}
			src.doc.Logger.Debug("incremental update: an object it keeps cannot be read, freeing nothing",
				slog.Int("object", num), slog.Any("err", err))
			return nil, false, nil
		}
		stack = appendRefs(stack, obj)
	}
	return reached, true, nil
}

// sourceGraph reads the objects of the document an update applies to, as the
// document reads them. That is the file's objects and also the page tree nodes
// [document.Document.PromoteDirectKids] staged: a page a /Kids array spelled
// out directly has a number of its own there, and its contents and resources
// are what that page referenced in the file.
type sourceGraph struct {
	doc      *document.Document
	promoted map[int]ast.PdfObject
}

// defines reports whether the document defines objNum: the file lists it in
// use or a promotion staged it.
func (s sourceGraph) defines(objNum int) bool {
	if _, ok := s.promoted[objNum]; ok {
		return true
	}
	return s.doc.DefinesObject(objNum)
}

// read returns the object the document reads at objNum.
func (s sourceGraph) read(ctx context.Context, objNum int) (ast.PdfObject, error) {
	return s.doc.Resolve(ctx, ast.PdfIndirectRef{ObjectNum: objNum})
}

// appendRefs appends to refs the number of each indirect reference obj holds,
// descending through its direct arrays, dictionaries and stream dictionaries.
// A direct object lives in the one container that spells it out, so the
// descent is a walk of a tree the parser already bounded in depth.
func appendRefs(refs []int, obj ast.PdfObject) []int {
	stack := []ast.PdfObject{obj}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		switch v := cur.(type) {
		case ast.PdfIndirectRef:
			refs = append(refs, v.ObjectNum)
		case ast.PdfArray:
			stack = append(stack, v...)
		case ast.PdfDict:
			for _, value := range v {
				stack = append(stack, value)
			}
		case ast.PdfStream:
			for _, value := range v.Dict {
				stack = append(stack, value)
			}
		}
	}
	return refs
}
