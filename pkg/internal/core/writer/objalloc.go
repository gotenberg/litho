package writer

import "github.com/gotenberg/litho/pkg/internal/core/ast"

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
// an existing object in an incremental update.
func (a *ObjAllocator) AllocAt(objNum int, obj ast.PdfObject) {
	a.pending[objNum] = obj
	if objNum >= a.nextObjNum {
		a.nextObjNum = objNum + 1
	}
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
