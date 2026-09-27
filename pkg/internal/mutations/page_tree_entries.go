package mutations

import (
	"context"
	"fmt"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// letterMediaBox returns the media box a page is written with when neither it
// nor any ancestor states one: US Letter, 612 by 792 points.
//
// ISO 32000-1, 7.7.3.3, Table 30 makes /MediaBox a required, inheritable page
// entry and gives it no default, so a page no node gives one describes no page
// size at all. Letter is the size every mainstream reader assumes for such a
// page: pdf.js falls back to its LETTER_SIZE_MEDIABOX, poppler's PageAttrs
// start from 612 by 792, and qpdf reports "MediaBox is undefined; setting to
// letter / ANSI A". pdf.Reader.Page reports the same size for the page, so the
// written box says what litho already read. A fresh array each call, since the
// output holds it as a page's own entry.
func letterMediaBox() ast.PdfArray {
	return ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(612), ast.PdfInt(792)}
}

// pageTreeRepair walks a page tree the way the reader walks it (see
// [document.ResolvePages]) and writes into alloc the entries ISO 32000-1
// requires of the catalog, of each page tree node and of each page, where the
// object states none or a wrong one (see [completeRewrittenPageTree]).
//
// doc is nil when alloc holds the whole output, a rewrite; a reference to a
// number alloc does not hold is then a null object (ISO 32000-1, 7.3.10). When
// doc is set, the output is an update of doc (an incremental one), objects
// alloc does not hold are doc's, and only what alloc holds is written: the
// repairs of an object the update does not write are left out.
type pageTreeRepair struct {
	alloc *writer.ObjAllocator
	doc   *document.Document
	// done holds the nodes already walked. A page tree is a strict tree (ISO
	// 32000-1, 7.7.3.2), and the reader walks a node reached a second time,
	// through a cycle or a second parent, once (see traversePageTree).
	done map[int]bool
}

// inheritedEntries are the inheritable page entries an ancestor states (ISO
// 32000-1, 7.7.3.4), as the reader reads them.
type inheritedEntries struct {
	// mediaBox is the /MediaBox of the nearest ancestor stating one the
	// reader takes, an array, and nil when none does (see traversePageTree).
	mediaBox ast.PdfArray
	// resources is the nearest ancestor's /Resources entry, as written, and
	// nil when none states one (see [document.Page]).
	resources ast.PdfObject
}

// walkedKid is what [pageTreeRepair.node] makes of one entry of a /Kids array:
// the pages under it, and the value the array holds for it from then on. A nil
// value drops the entry: the reader reads nothing there.
type walkedKid struct {
	pages int
	value ast.PdfObject
	moved bool
}

// completeRewrittenPageTree completes the catalog alloc holds at catalogRef and
// the page tree it names, for a rewrite whose output is every object alloc
// holds. Each object it repairs is written back at its number, and a node or a
// catalog it has to add is allocated.
//
// The page tree comes out as the reader read it: every node and page carries
// the /Type its role in the walk gives it, every node the /Kids the walk kept
// and the /Count of the pages under it, every kid but the root the /Parent
// listing it, every page a /MediaBox and /Resources it states or inherits.
//
//   - ISO 32000-1, 7.7.2, Table 28 makes the catalog's /Type /Catalog and
//     /Pages required, /Pages an indirect reference to the page tree root. A
//     root written directly is numbered. A root the reader reads as a page is
//     given a node to list it, and one it reads as neither gets an empty node
//     in its place, since the reader reads no page from it either.
//   - 7.7.3.2, Table 29 makes a node's /Type /Pages, /Kids and /Count
//     required, /Parent required on every node but the root, where it is
//     prohibited. /Count is the number of pages the walk reads under the node,
//     and /Kids lists the kids the walk reads: an entry the reader passes over,
//     one that is no dictionary, whose role it cannot tell or that it reached
//     already, is dropped, so the output reads as its source did.
//   - 7.7.3.3, Table 30 makes a page's /Type /Page, /Parent, /Resources and
//     /MediaBox required, the last two inheritable (7.7.3.4). A page no node
//     gives a /MediaBox, or whose /MediaBox is no rectangle (7.9.5), gets
//     [letterMediaBox]. One no node gives /Resources, or whose /Resources is
//     no dictionary, gets an empty dictionary, which is what Table 30 writes
//     for a page needing no resources, as the flat page tree of a split
//     already writes it (see [flatResources]).
//
// # Corpus evidence
//
//   - pdfjs issue9105_other.pdf: its catalog, root and page carry no /Type,
//     its root no /Count and its page no /MediaBox, and each rewrite wrote them
//     so: qpdf warned four times on the remediated output, and veraPDF parsed
//     none of the outputs.
//   - All corpora: a full rewrite changes 30 files. 14 hold a page no node
//     gives /Resources and pdfjs issue15150.pdf one whose /Resources is no
//     dictionary, 2 a page no node gives a /MediaBox and 2 one whose /MediaBox
//     is no rectangle, 3 a catalog with no /Type, 6 a /Count missing, wrong or
//     no integer, 5 a kid whose /Parent is missing or names another node, and
//     pdfjs bug1020226.pdf and issue15590.pdf a root the reader reads as no
//     node.
//   - pdfjs Pages-tree-refs.pdf: a node lists its ancestor, and qpdf refused
//     every rewrite of the cycle.
//   - All corpora: over eleven write paths of the 32 files the census finds,
//     qpdf's page tree warnings go from 241 to 25, each left on an update that
//     does not write the object, but the 5 of pdfjs poppler-937-0-fuzzed.pdf's
//     split, merge and insertion, which write a stream inside a dictionary.
//     veraPDF parses 244 of the outputs, 206 before.
func completeRewrittenPageTree(ctx context.Context, alloc *writer.ObjAllocator, catalogRef ast.PdfIndirectRef) error {
	s := &pageTreeRepair{alloc: alloc, done: make(map[int]bool)}
	if err := s.run(ctx, catalogRef); err != nil {
		return fmt.Errorf("completing the page tree: %w", err)
	}
	return nil
}

// CompleteWrittenPageTree completes, as [completeRewrittenPageTree] does, the
// catalog, page tree nodes and pages alloc holds for an update of doc, reading
// every object alloc does not hold from doc. It writes only into the objects
// alloc already holds: an update that writes a page or a node writes it
// complete, and one that does not leaves the page tree as doc has it. It
// numbers nothing and adds no node, which [document.Document.PromoteDirectKids]
// and a rewrite do.
func CompleteWrittenPageTree(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) error {
	catalogRef, ok := CatalogRef(ctx, doc)
	if !ok || !holdsPageTreeObject(alloc, catalogRef) {
		return nil
	}
	s := &pageTreeRepair{alloc: alloc, doc: doc, done: make(map[int]bool)}
	if err := s.run(ctx, catalogRef); err != nil {
		return fmt.Errorf("completing the page tree: %w", err)
	}
	return nil
}

// holdsPageTreeObject reports whether alloc holds the catalog at catalogRef or
// an object whose own entries read as a page tree node or a page, the only
// objects an update's completion can write. Most updates hold none, and they
// skip the walk of the whole page tree.
func holdsPageTreeObject(alloc *writer.ObjAllocator, catalogRef ast.PdfIndirectRef) bool {
	pending := alloc.Pending()
	if _, ok := pending[catalogRef.ObjectNum]; ok {
		return true
	}
	for _, obj := range pending {
		dict, ok := obj.(ast.PdfDict)
		if !ok {
			continue
		}
		typeName, _ := dict["Type"].(ast.PdfName)
		if role := document.PageTreeNodeRole(typeName, dict); role == "Pages" || role == "Page" {
			return true
		}
	}
	return false
}

// run completes the catalog at catalogRef and the page tree it names.
func (s *pageTreeRepair) run(ctx context.Context, catalogRef ast.PdfIndirectRef) error {
	obj, err := s.resolve(ctx, catalogRef)
	if err != nil {
		return err
	}
	catalog, ok := obj.(ast.PdfDict)
	if !ok {
		return nil
	}
	cat := &cowDict{dict: catalog}
	if typeName, _ := s.name(ctx, catalog["Type"]); typeName != "Catalog" {
		cat.set("Type", ast.PdfName("Catalog"))
	}

	var rootRef ast.PdfIndirectRef
	switch root := catalog["Pages"].(type) {
	case ast.PdfIndirectRef:
		rootRef = root
	case ast.PdfDict:
		if s.doc != nil {
			// The promotion numbers a root written directly before any update
			// is written; an update has no business adding one.
			s.write(catalogRef, cat)
			return nil
		}
		rootRef = s.alloc.Alloc(root)
		cat.set("Pages", rootRef)
	default:
		// A catalog naming no page tree is the rewrite's to give an empty
		// one (see [ensurePageTree]), and an update's to leave as it is.
		s.write(catalogRef, cat)
		return nil
	}
	if s.doc == nil {
		if rootRef, err = s.adoptRoot(ctx, rootRef, cat); err != nil {
			return err
		}
	}
	if _, err := s.node(ctx, rootRef, ast.PdfIndirectRef{}, 0, inheritedEntries{}); err != nil {
		return err
	}
	s.write(catalogRef, cat)
	return nil
}

// adoptRoot returns the page tree root a rewrite's catalog names: rootRef when
// the reader reads it as a node, a new node listing it when the reader reads
// it as a page, and a new empty node when the reader reads it as neither,
// which reads no page. A root that is no dictionary is left to
// [ensurePageTree].
func (s *pageTreeRepair) adoptRoot(ctx context.Context, rootRef ast.PdfIndirectRef, cat *cowDict) (ast.PdfIndirectRef, error) {
	obj, err := s.resolve(ctx, rootRef)
	if err != nil {
		return rootRef, err
	}
	var dict ast.PdfDict
	switch v := obj.(type) {
	case ast.PdfDict:
		dict = v
	case ast.PdfStream:
		dict = v.Dict
	default:
		return rootRef, nil
	}
	role, _, err := s.role(ctx, dict)
	if err != nil {
		return rootRef, err
	}
	switch role {
	case "Pages":
		return rootRef, nil
	case "Page":
		// /Count and the page's /Parent are the walk's to write.
		node := s.alloc.Alloc(ast.PdfDict{"Type": ast.PdfName("Pages"), "Kids": ast.PdfArray{rootRef}})
		cat.set("Pages", node)
		return node, nil
	default:
		node := s.alloc.Alloc(ast.PdfDict{"Type": ast.PdfName("Pages"), "Kids": ast.PdfArray{}, "Count": ast.PdfInt(0)})
		cat.set("Pages", node)
		return node, nil
	}
}

// node walks the page tree entry kid, listed by the node at parent (none for
// the root), completes it and what lies under it, and returns what the listing
// /Kids holds for it.
func (s *pageTreeRepair) node(ctx context.Context, kid ast.PdfObject, parent ast.PdfIndirectRef, depth int, inherited inheritedEntries) (walkedKid, error) {
	if err := ctx.Err(); err != nil {
		return walkedKid{}, err
	}
	// The reader's bound: a node deeper than it is one the reader never
	// reaches.
	if depth > document.MaxPageTreeDepth {
		return walkedKid{moved: true}, nil
	}
	numbered := false
	if direct, ok := kid.(ast.PdfDict); ok && s.doc == nil {
		// ISO 32000-1, 7.7.3.2, Table 29: /Kids holds references.
		kid, numbered = s.alloc.Alloc(direct), true
	}

	var self ast.PdfIndirectRef
	var obj ast.PdfObject
	switch k := kid.(type) {
	case ast.PdfIndirectRef:
		if k.ObjectNum <= 0 || s.done[k.ObjectNum] {
			return walkedKid{moved: true}, nil
		}
		s.done[k.ObjectNum] = true
		self = k
		resolved, err := s.resolve(ctx, k)
		if err != nil {
			return walkedKid{}, err
		}
		obj = resolved
	case ast.PdfDict:
		obj = k
	default:
		return walkedKid{moved: true}, nil
	}
	var dict ast.PdfDict
	stream := false
	switch v := obj.(type) {
	case ast.PdfDict:
		dict = v
	case ast.PdfStream:
		// The reader reads a stream's dictionary as the node (see
		// resolveKidDict). Its entries are not rewritten here.
		dict, stream = v.Dict, true
	default:
		return walkedKid{moved: true}, nil
	}
	if dict == nil {
		return walkedKid{moved: true}, nil
	}
	role, typed, err := s.role(ctx, dict)
	if err != nil {
		return walkedKid{}, err
	}
	if role != "Pages" && role != "Page" {
		return walkedKid{moved: true}, nil
	}

	out := &cowDict{dict: dict}
	if !typed {
		out.set("Type", ast.PdfName(role))
	}
	switch {
	case parent.ObjectNum > 0:
		if p, ok := dict["Parent"].(ast.PdfIndirectRef); !ok || p.ObjectNum != parent.ObjectNum {
			out.set("Parent", parent)
		}
	case depth == 0:
		if _, has := dict["Parent"]; has {
			out.del("Parent")
		}
	}

	pages := 1
	if role == "Pages" {
		pages, err = s.nodeKids(ctx, dict, out, self, depth, inherited)
	} else {
		err = s.pageEntries(ctx, dict, out, inherited)
	}
	if err != nil {
		return walkedKid{}, err
	}

	switch {
	case !out.copied || stream:
		return walkedKid{pages: pages, value: kid, moved: numbered}, nil
	case self.ObjectNum > 0:
		s.write(self, out)
		return walkedKid{pages: pages, value: kid, moved: numbered}, nil
	default:
		// A node written directly in an update's /Kids goes back there.
		return walkedKid{pages: pages, value: out.dict, moved: true}, nil
	}
}

// nodeKids completes the /Kids and /Count of dict, a page tree node at self,
// into out, walking its kids, and returns the number of pages under it.
func (s *pageTreeRepair) nodeKids(ctx context.Context, dict ast.PdfDict, out *cowDict, self ast.PdfIndirectRef, depth int, inherited inheritedEntries) (int, error) {
	below := inherited
	box, err := s.mediaBox(ctx, dict)
	if err != nil {
		return 0, err
	}
	if box != nil {
		below.mediaBox = box
	}
	if entry, ok := dict["Resources"]; ok && !isNullEntry(entry) {
		below.resources = entry
	}

	// What the reader takes /Kids as: an array, a lone dictionary written in
	// its place, or nothing (see traversePageTree).
	kidsObj, hasKids := dict["Kids"]
	var kids ast.PdfArray
	rewriteKids := true
	if hasKids {
		resolved, err := s.resolve(ctx, kidsObj)
		if err != nil {
			return 0, err
		}
		switch v := resolved.(type) {
		case ast.PdfArray:
			kids, rewriteKids = v, false
		case ast.PdfDict:
			kids = ast.PdfArray{kidsObj}
		}
	}

	kept := make(ast.PdfArray, 0, len(kids))
	pages := 0
	for _, kid := range kids {
		walked, err := s.node(ctx, kid, self, depth+1, below)
		if err != nil {
			return 0, err
		}
		rewriteKids = rewriteKids || walked.moved
		if walked.value == nil {
			continue
		}
		kept = append(kept, walked.value)
		pages += walked.pages
	}
	if rewriteKids {
		out.set("Kids", kept)
	}
	count, err := s.resolve(ctx, dict["Count"])
	if err != nil {
		return 0, err
	}
	if n, isInt := count.(ast.PdfInt); !isInt || int(n) != pages {
		out.set("Count", ast.PdfInt(pages))
	}
	return pages, nil
}

// pageEntries completes the /MediaBox and /Resources of dict, a page, into
// out.
func (s *pageTreeRepair) pageEntries(ctx context.Context, dict ast.PdfDict, out *cowDict, inherited inheritedEntries) error {
	box, err := s.mediaBox(ctx, dict)
	if err != nil {
		return err
	}
	if box == nil {
		box = inherited.mediaBox
	}
	rectangle, err := s.isRectangle(ctx, box)
	if err != nil {
		return err
	}
	if !rectangle {
		out.set("MediaBox", letterMediaBox())
	}

	entry := inherited.resources
	if own, ok := dict["Resources"]; ok && !isNullEntry(own) {
		entry = own
	}
	if entry == nil {
		out.set("Resources", ast.PdfDict{})
		return nil
	}
	resolved, err := s.resolve(ctx, entry)
	if err != nil {
		return err
	}
	if _, isDict := resolved.(ast.PdfDict); !isDict {
		out.set("Resources", ast.PdfDict{})
	}
	return nil
}

// role returns the role the reader gives dict in the page tree (see
// [document.PageTreeNodeRole]), and whether its /Type names it.
func (s *pageTreeRepair) role(ctx context.Context, dict ast.PdfDict) (string, bool, error) {
	typeName, err := s.name(ctx, dict["Type"])
	if err != nil {
		return "", false, err
	}
	return document.PageTreeNodeRole(typeName, dict), typeName != "", nil
}

// mediaBox returns the /MediaBox dict states as the reader takes it, any array,
// and nil when it states none (see traversePageTree).
func (s *pageTreeRepair) mediaBox(ctx context.Context, dict ast.PdfDict) (ast.PdfArray, error) {
	box, ok := dict["MediaBox"]
	if !ok {
		return nil, nil
	}
	resolved, err := s.resolve(ctx, box)
	if err != nil {
		return nil, err
	}
	arr, _ := resolved.(ast.PdfArray)
	return arr, nil
}

// isRectangle reports whether box is a rectangle, an array of four numbers
// (ISO 32000-1, 7.9.5), each of which may be written as a reference.
func (s *pageTreeRepair) isRectangle(ctx context.Context, box ast.PdfArray) (bool, error) {
	if len(box) != 4 {
		return false, nil
	}
	for _, elem := range box {
		resolved, err := s.resolve(ctx, elem)
		if err != nil {
			return false, err
		}
		switch resolved.(type) {
		case ast.PdfInt, ast.PdfReal:
		default:
			return false, nil
		}
	}
	return true, nil
}

// name resolves obj to a name, empty when it is none.
func (s *pageTreeRepair) name(ctx context.Context, obj ast.PdfObject) (ast.PdfName, error) {
	if obj == nil {
		return "", nil
	}
	resolved, err := s.resolve(ctx, obj)
	if err != nil {
		return "", err
	}
	name, _ := resolved.(ast.PdfName)
	return name, nil
}

// resolve returns the object obj names: alloc's when alloc holds it, else
// doc's, else null. Only a done ctx is an error: an object that does not
// resolve is null (ISO 32000-1, 7.3.10), as the copier writes it.
func (s *pageTreeRepair) resolve(ctx context.Context, obj ast.PdfObject) (ast.PdfObject, error) {
	ref, ok := obj.(ast.PdfIndirectRef)
	if !ok {
		return obj, nil
	}
	if pending, held := s.alloc.Pending()[ref.ObjectNum]; held {
		return pending, nil
	}
	if s.doc == nil {
		return ast.PdfNull{}, nil
	}
	resolved, err := s.doc.Resolve(ctx, ref)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return ast.PdfNull{}, nil
	}
	return resolved, nil
}

// write queues out at ref when it changed: always in a rewrite, and in an
// update only when alloc already holds the object.
func (s *pageTreeRepair) write(ref ast.PdfIndirectRef, out *cowDict) {
	if !out.copied || ref.ObjectNum <= 0 {
		return
	}
	if _, held := s.alloc.Pending()[ref.ObjectNum]; !held && s.doc != nil {
		return
	}
	s.alloc.AllocAt(ref.ObjectNum, out.dict)
}

// isNullEntry reports whether a dictionary value is null, which ISO 32000-1,
// 7.3.7 makes the same as an absent entry.
func isNullEntry(obj ast.PdfObject) bool {
	if obj == nil {
		return true
	}
	_, isNull := obj.(ast.PdfNull)
	return isNull
}

// cowDict edits a dictionary on a copy taken at the first edit, so the object
// read, which alloc, the source's cache or an earlier edit may share, is never
// changed in place.
type cowDict struct {
	dict   ast.PdfDict
	copied bool
}

// set writes key into the copy.
func (c *cowDict) set(key ast.PdfName, value ast.PdfObject) {
	c.own()
	c.dict[key] = value
}

// del removes key from the copy.
func (c *cowDict) del(key ast.PdfName) {
	c.own()
	delete(c.dict, key)
}

// own takes the copy the edits go to.
func (c *cowDict) own() {
	if !c.copied {
		c.dict = maps.Clone(c.dict)
		c.copied = true
	}
}
