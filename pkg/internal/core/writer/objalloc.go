package writer

import (
	"maps"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// ObjAllocator assigns object numbers for new and modified objects and
// maintains a registry of pending objects to be written.
type ObjAllocator struct {
	nextObjNum int
	pending    map[int]ast.PdfObject
}

// NewObjAllocator creates an allocator whose next object number starts after
// the highest existing object number in the document.
func NewObjAllocator(maxExisting int) *ObjAllocator {
	return &ObjAllocator{
		nextObjNum: maxExisting + 1,
		pending:    make(map[int]ast.PdfObject),
	}
}

// Alloc reserves a new object number and registers the object.
func (a *ObjAllocator) Alloc(obj ast.PdfObject) ast.PdfIndirectRef {
	num := a.nextObjNum
	a.nextObjNum++
	a.pending[num] = obj
	return ast.PdfIndirectRef{ObjectNum: num, GenNum: 0}
}

// AllocAt registers an object at a specific number. Use this to replace
// an existing object in an incremental update. Reports whether the number was
// usable: object 0 and negative numbers are refused.
//
// Per ISO 32000-1, 7.5.4, entry zero of a cross-reference table is always the
// head of the free list and carries generation 65535, so no object may live
// there. A page inlined in a /Kids array has no indirect reference and resolves
// with object number 0 (pdfjs issue9540), and writing it back at that number
// emitted a "0 0 obj" body plus an in-use entry for object 0. Every reader
// ignores it, so the mutation vanished silently and the file's xref became
// invalid; the caller learns here instead.
func (a *ObjAllocator) AllocAt(objNum int, obj ast.PdfObject) bool {
	if objNum <= 0 {
		return false
	}
	a.pending[objNum] = obj
	if objNum >= a.nextObjNum {
		a.nextObjNum = objNum + 1
	}
	return true
}

// Pending returns all registered objects as a map from object number to object.
func (a *ObjAllocator) Pending() map[int]ast.PdfObject {
	return a.pending
}

// NextObjNum returns the next available object number (also the /Size value
// for the xref table).
func (a *ObjAllocator) NextObjNum() int {
	return a.nextObjNum
}

// Snapshot is an opaque point-in-time state of an [ObjAllocator]. Pass it to
// [ObjAllocator.Restore] to undo allocations queued after the snapshot.
// Snapshot values are immutable and safe for concurrent reads.
type Snapshot struct {
	pending    map[int]ast.PdfObject
	nextObjNum int
}

// Snapshot captures the allocator's current state so a caller can roll back
// later edits. The returned value is independent of subsequent Alloc/AllocAt
// calls. Values are captured by reference: for Restore to be lossless,
// callers must replace pending entries via [ObjAllocator.AllocAt] rather
// than mutating AST objects in place. The in-tree fixers all construct
// fresh replacements, so a shallow map copy is sufficient today.
func (a *ObjAllocator) Snapshot() Snapshot {
	pending := make(map[int]ast.PdfObject, len(a.pending))
	maps.Copy(pending, a.pending)
	return Snapshot{pending: pending, nextObjNum: a.nextObjNum}
}

// Restore reverts the allocator to the state captured by snap. Any objects
// allocated since the snapshot are discarded. Restore mutates the pending
// map in place so callers that captured the map reference prior to the
// snapshot continue to see a coherent view.
func (a *ObjAllocator) Restore(snap Snapshot) {
	for k := range a.pending {
		delete(a.pending, k)
	}
	maps.Copy(a.pending, snap.pending)
	a.nextObjNum = snap.nextObjNum
}
