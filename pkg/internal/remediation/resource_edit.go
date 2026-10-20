package remediation

import (
	"context"
	"fmt"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// maxImageNameTries bounds the search for an XObject name a resource
// dictionary does not hold yet. Anti-DoS guard, not spec-mandated: a
// dictionary would have to hold this many names of the one series litho
// writes for the search to give up, which no real document does, and the image
// then stays inline, its finding a residual.
const maxImageNameTries = 10000

// resourceOwner says where the names of one content stream resolve and how to
// write them: load returns the /Resources value the stream's owner holds now,
// read through the allocator, or nil when it holds none; store writes a new
// direct /Resources dictionary into the owner, or is nil when the owner cannot
// be written. ISO 32000-1, 7.8.3 resolves the names of a page's content in the
// page's resources, of a form XObject, a tiling pattern or an annotation
// appearance in the stream's own, and of a Type 3 glyph in its font's (9.6.5).
type resourceOwner struct {
	load  func() ast.PdfObject
	store func(ast.PdfDict) bool
}

// resourceEdit gathers the image XObjects a repair of one content stream adds
// to the resources its names resolve in ([resourceOwner]), so the walk writes
// them before the stream that names them, or drops them with the stream.
//
// An edit takes a snapshot of the allocator before its first new object, and
// [resourceEdit.abandon] restores it, so a stream whose write cannot land
// leaves no image and no resource change behind.
type resourceEdit struct {
	alloc *writer.ObjAllocator
	doc   *document.Document
	owner resourceOwner
	added []resourceEntry
	snap  writer.Snapshot
	// snapped reports whether snap holds the state before the first object.
	snapped bool
}

// resourceEntry is one XObject a [resourceEdit] adds: its name and the object
// it names.
type resourceEntry struct {
	name ast.PdfName
	ref  ast.PdfIndirectRef
}

// newResourceEdit returns an edit of the resources owner describes.
func newResourceEdit(alloc *writer.ObjAllocator, doc *document.Document, owner resourceOwner) *resourceEdit {
	return &resourceEdit{alloc: alloc, doc: doc, owner: owner}
}

// resources returns the live resource dictionary the stream's names resolve
// in, or nil when the owner holds none or holds no dictionary.
func (e *resourceEdit) resources(ctx context.Context) ast.PdfDict {
	if e == nil || e.owner.load == nil {
		return nil
	}
	obj := e.owner.load()
	if obj == nil {
		return nil
	}
	return liveDictValue(ctx, e.alloc, e.doc, obj)
}

// colorSpace returns the value the /ColorSpace subdictionary of the stream's
// resources gives name, as written there, a reference included.
func (e *resourceEdit) colorSpace(ctx context.Context, name ast.PdfName) (ast.PdfObject, bool) {
	res := e.resources(ctx)
	if res == nil {
		return nil, false
	}
	cs := liveDictValue(ctx, e.alloc, e.doc, res["ColorSpace"])
	v, ok := cs[name]
	if !ok || v == nil {
		return nil, false
	}
	if _, isNull := v.(ast.PdfNull); isNull {
		return nil, false
	}
	return v, true
}

// addImage queues image as a new object and returns the name the stream's
// resources will give it under /XObject: the first of LithoIm1, LithoIm2, ...
// that neither the live /XObject subdictionary nor an earlier image of this
// edit holds. It returns false when the stream has no resource dictionary of
// its own to name it in: a form XObject with no /Resources resolves its names
// in the page's (ISO 32000-1, 7.8.3), and giving it one would hide them.
func (e *resourceEdit) addImage(ctx context.Context, image ast.PdfStream) (ast.PdfName, bool) {
	res := e.resources(ctx)
	if res == nil {
		return "", false
	}
	taken := liveDictValue(ctx, e.alloc, e.doc, res["XObject"])
	var name ast.PdfName
	for i := 1; i <= maxImageNameTries; i++ {
		candidate := ast.PdfName(fmt.Sprintf("LithoIm%d", i))
		if _, ok := taken[candidate]; ok {
			continue
		}
		if e.holds(candidate) {
			continue
		}
		name = candidate
		break
	}
	if name == "" {
		return "", false
	}
	if !e.snapped {
		e.snap = e.alloc.Snapshot()
		e.snapped = true
	}
	e.added = append(e.added, resourceEntry{name: name, ref: e.alloc.Alloc(image)})
	return name, true
}

// holds reports whether the edit already added name.
func (e *resourceEdit) holds(name ast.PdfName) bool {
	for _, a := range e.added {
		if a.name == name {
			return true
		}
	}
	return false
}

// commit writes the images the edit added into the /XObject subdictionary of
// the stream's resources, at the deepest indirect object that holds it: the
// subdictionary's own object, else the resource dictionary's, else the owner.
// It reports whether the write landed; an edit that added nothing has nothing
// to write.
func (e *resourceEdit) commit(ctx context.Context) bool {
	if e == nil || len(e.added) == 0 {
		return true
	}
	resObj := e.owner.load()
	res := e.resources(ctx)
	if res == nil {
		return false
	}
	xobj, hasX := res["XObject"]
	if ref, ok := xobj.(ast.PdfIndirectRef); ok && ref.ObjectNum > 0 {
		live := liveDictValue(ctx, e.alloc, e.doc, ref)
		if live == nil {
			return false
		}
		return e.alloc.AllocAt(ref.ObjectNum, e.withAdded(live))
	}
	var sub ast.PdfDict
	if hasX {
		d, ok := xobj.(ast.PdfDict)
		if !ok {
			return false
		}
		sub = d
	}
	newRes := maps.Clone(res)
	newRes["XObject"] = e.withAdded(sub)
	if ref, ok := resObj.(ast.PdfIndirectRef); ok && ref.ObjectNum > 0 {
		return e.alloc.AllocAt(ref.ObjectNum, newRes)
	}
	if e.owner.store == nil {
		return false
	}
	return e.owner.store(newRes)
}

// withAdded returns a copy of the /XObject subdictionary sub with the images
// the edit added.
func (e *resourceEdit) withAdded(sub ast.PdfDict) ast.PdfDict {
	out := make(ast.PdfDict, len(sub)+len(e.added))
	maps.Copy(out, sub)
	for _, a := range e.added {
		out[a.name] = a.ref
	}
	return out
}

// abandon drops every object the edit queued and every write after its
// snapshot, for a stream whose write did not land.
func (e *resourceEdit) abandon() {
	if e != nil && e.snapped {
		e.alloc.Restore(e.snap)
		e.snapped = false
		e.added = nil
	}
}
