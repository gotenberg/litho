package mutations

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// letterMediaBox returns the media box a page is written with when neither it
// nor any ancestor states one the reader can use: US Letter, 612 by 792 points.
//
// ISO 32000-1, 7.7.3.3, Table 30 makes /MediaBox a required, inheritable page
// entry and gives it no default, so a page no node gives one describes no page
// size at all, and one whose box encloses no area describes none either. Letter
// is the size every mainstream reader assumes for such a page: pdf.js falls
// back to its LETTER_SIZE_MEDIABOX, poppler's PageAttrs start from 612 by 792,
// and qpdf reports "MediaBox is undefined; setting to letter / ANSI A".
// pdf.Reader.Page reports the same size for the page, so the written box says
// what litho already read. A fresh array each call, since the output holds it
// as a page's own entry.
func letterMediaBox() ast.PdfArray {
	return ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(612), ast.PdfInt(792)}
}

// pageBoundaries are the page boundaries of ISO 32000-1, 14.11.2, each a
// rectangle (7.9.5) a page tree node or a page states under the name Table 30
// gives it. /MediaBox and /CropBox are inheritable (7.7.3.4), the other three
// are not.
var pageBoundaries = []ast.PdfName{"MediaBox", "CropBox", "BleedBox", "TrimBox", "ArtBox"}

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
// 32000-1, 7.7.3.4), as the reader reads them, and what the walk knows of the
// node listing an entry.
type inheritedEntries struct {
	// mediaBox is the /MediaBox of the nearest ancestor stating one the
	// reader takes, an array, as the output holds it, and nil when none does
	// (see traversePageTree).
	mediaBox ast.PdfArray
	// sourceCrop reports whether a page stating no /CropBox inherits, in the
	// source, one the reader can use (see [pageTreeRepair.readBoundary]),
	// rather than the default, its media box.
	sourceCrop bool
	// cropBox is the /CropBox the reader can use that a page stating none
	// inherits in the output, and nil when it takes the default. It differs
	// from the source's where the output drops a crop box the reader could not
	// use from a node, and the one of a node above it shows through.
	cropBox ast.PdfArray
	// resources is the nearest ancestor's /Resources entry, as written, and
	// nil when none states one (see [document.Page]).
	resources ast.PdfObject
	// written reports whether the output writes the node listing the entry.
	written bool
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
// listing it, every page a /MediaBox and /Resources it states or inherits, and
// every page boundary the rectangle the reader reads.
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
//     /MediaBox required, the last two inheritable (7.7.3.4), and each page
//     boundary a rectangle (7.9.5). Every boundary a node or a page states is
//     written as the rectangle the reader reads it as (see
//     [pageTreeRepair.repairBoundary]). A page no node gives a /MediaBox the
//     reader can use gets [letterMediaBox], and a crop, bleed, trim or art box
//     the reader cannot use is dropped for the default Table 30 gives it. One
//     no node gives /Resources, or whose /Resources is no dictionary, gets an
//     empty dictionary, which is what Table 30 writes for a page needing no
//     resources, as the flat page tree of a split already writes it (see
//     [flatResources]).
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
//   - All corpora: 7 page boundaries in 5 files are not four numbers or enclose
//     no area. A full rewrite of every file changes the boxes of 3: litho
//     annotshare_ua1.pdf loses a /CropBox of three numbers and an /ArtBox
//     ending in null, pdfjs boundingBox_invalid.pdf gets US Letter for a
//     /MediaBox of [0 0 0 0] and loses a /CropBox of [0 0 0 0], and pdfjs
//     issue17190.pdf gets US Letter for its root's [0 0 0 0]. litho
//     directstream_2b.pdf's [0 0 612 792 5 0 R] and pdfjs issue13783.pdf's
//     damaged box already came out as they read.
//   - All corpora: over the full rewrite, split, merge, insertion, page move
//     and PDF/A-1b remediation of those 5 files and pdfjs
//     poppler-937-0-fuzzed.pdf, veraPDF fails ISO 19005-2, 6.1.13 t11 on 12
//     outputs, 0 now, and reports nothing else new at PDF/A-2b. qpdf reports
//     what it did.
//   - litho annotshare_ua1.pdf: page 3 lists a Link above a crop box of no
//     height, which ISO 14289-1, 7.18 exempts. Its outputs show the whole
//     page, and litho and veraPDF both count one more check of 7.18.1 t2 and
//     7.18.5 t1 and t2 for each copy of the page, the rule sets unchanged.
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

	// A rewrite writes every node. An update writes the ones alloc holds, and
	// a node written directly in /Kids goes out with the node listing it.
	writes := !stream && (s.doc == nil || inherited.written)
	if s.doc != nil && self.ObjectNum > 0 {
		_, held := s.alloc.Pending()[self.ObjectNum]
		writes = held && !stream
	}

	pages := 1
	if role == "Pages" {
		pages, err = s.nodeKids(ctx, dict, out, self, depth, inherited, writes)
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

// nodeKids completes the page boundaries, /Kids and /Count of dict, a page tree
// node at self, into out, walking its kids, and returns the number of pages
// under it. writes reports whether the output writes the node.
func (s *pageTreeRepair) nodeKids(ctx context.Context, dict ast.PdfDict, out *cowDict, self ast.PdfIndirectRef, depth int, inherited inheritedEntries, writes bool) (int, error) {
	below, err := s.nodeBoundaries(ctx, dict, out, inherited, writes)
	if err != nil {
		return 0, err
	}
	below.written = writes
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

// pageEntries completes the page boundaries and /Resources of dict, a page,
// into out.
func (s *pageTreeRepair) pageEntries(ctx context.Context, dict ast.PdfDict, out *cowDict, inherited inheritedEntries) error {
	media, err := s.pageMediaBox(ctx, dict, out, inherited)
	if err != nil {
		return err
	}
	if err := s.pageCropBox(ctx, dict, out, inherited, media); err != nil {
		return err
	}
	for _, key := range []ast.PdfName{"BleedBox", "TrimBox", "ArtBox"} {
		box, stated, err := s.boundary(ctx, dict, key)
		if err != nil {
			return err
		}
		switch {
		case box != nil:
			if _, _, err := s.repairBoundary(ctx, key, box, out); err != nil {
				return err
			}
		case stated:
			// The reader passes over a boundary that is no array, and takes
			// the default (see [document.Page]).
			out.del(key)
		}
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

// nodeBoundaries completes into out the page boundaries dict, a page tree
// node, states (see [pageTreeRepair.repairBoundary]), and returns the entries
// its kids inherit. writes reports whether the output writes the node: when it
// does not, its kids inherit its boundaries as the source states them.
func (s *pageTreeRepair) nodeBoundaries(ctx context.Context, dict ast.PdfDict, out *cowDict, inherited inheritedEntries, writes bool) (inheritedEntries, error) {
	below := inherited
	for _, key := range pageBoundaries {
		box, stated, err := s.boundary(ctx, dict, key)
		if err != nil {
			return below, err
		}
		if box == nil {
			if stated {
				// The reader passes over a boundary that is no array, and so
				// the kids inherit what they did (see traversePageTree).
				out.del(key)
			}
			continue
		}
		held, usable, err := s.repairBoundary(ctx, key, box, out)
		if err != nil {
			return below, err
		}
		if !writes {
			held = box
		}
		switch key {
		case "MediaBox":
			below.mediaBox = held
		case "CropBox":
			// A crop box the output drops leaves the one of a node above
			// showing through, where the reader reads the default.
			below.sourceCrop = usable
			switch {
			case usable:
				below.cropBox = held
			case !writes:
				below.cropBox = nil
			}
		}
	}
	return below, nil
}

// pageMediaBox completes the /MediaBox of dict, a page, into out, and returns
// the media box the page has in the output: its own, the one it inherits when
// the reader can use it (see [pageTreeRepair.readBoundary]), or
// [letterMediaBox] when neither the page nor any node gives it one.
func (s *pageTreeRepair) pageMediaBox(ctx context.Context, dict ast.PdfDict, out *cowDict, inherited inheritedEntries) (ast.PdfArray, error) {
	box, stated, err := s.boundary(ctx, dict, "MediaBox")
	if err != nil {
		return nil, err
	}
	if box == nil {
		if stated {
			out.del("MediaBox")
		}
		// A node the output does not write keeps the box it states, and the
		// page states the one it reads in its place.
		box = inherited.mediaBox
	}
	if box == nil {
		letter := letterMediaBox()
		out.set("MediaBox", letter)
		return letter, nil
	}
	held, _, err := s.repairBoundary(ctx, "MediaBox", box, out)
	return held, err
}

// pageCropBox completes the /CropBox of dict, a page whose media box in the
// output is media, into out.
//
// The reader reads a crop box it cannot use as the default, the media box
// (see [pageTreeRepair.readBoundary]), and the output drops it. Where a node
// above states a crop box the reader can use, that one would show through in
// its place, so the page states its media box as its crop box instead, and
// shows the region it showed. So does a page inheriting a crop box the output
// drops from a node.
func (s *pageTreeRepair) pageCropBox(ctx context.Context, dict ast.PdfDict, out *cowDict, inherited inheritedEntries, media ast.PdfArray) error {
	box, stated, err := s.boundary(ctx, dict, "CropBox")
	if err != nil {
		return err
	}
	sourceCrop := inherited.sourceCrop
	switch {
	case box != nil:
		_, usable, err := s.repairBoundary(ctx, "CropBox", box, out)
		if err != nil || usable {
			return err
		}
		sourceCrop = false
	case stated:
		// The reader passes over a boundary that is no array, and takes the
		// inherited one (see traversePageTree).
		out.del("CropBox")
	}
	if !sourceCrop && inherited.cropBox != nil {
		out.set("CropBox", slices.Clone(media))
	}
	return nil
}

// boundary returns the page boundary dict states under key as the reader takes
// it, an array, and whether dict states the entry: holds it with a value other
// than null (ISO 32000-1, 7.3.7). A stated boundary that is no array comes back
// nil, and the reader passes over it (see traversePageTree).
func (s *pageTreeRepair) boundary(ctx context.Context, dict ast.PdfDict, key ast.PdfName) (ast.PdfArray, bool, error) {
	entry, ok := dict[key]
	if !ok || isNullEntry(entry) {
		return nil, false, nil
	}
	resolved, err := s.resolve(ctx, entry)
	if err != nil {
		return nil, false, err
	}
	if isNullEntry(resolved) {
		return nil, false, nil
	}
	box, _ := resolved.(ast.PdfArray)
	return box, true, nil
}

// repairBoundary completes into out box, the page boundary a node or a page
// states under key, and returns the box out holds under key, nil when it holds
// none, and whether the reader takes box as a boundary (see
// [pageTreeRepair.readBoundary]).
//
// ISO 32000-1, 7.7.3.3, Table 30 makes each page boundary a rectangle, four
// numbers (7.9.5). A box the reader takes is written as the rectangle it reads,
// where it is not that rectangle as written. A media box the reader cannot
// take is replaced by [letterMediaBox], the size pdf.Reader.Page reports for
// the page, since Table 30 gives the media box no default. Any other box it
// cannot take is dropped: Table 30 defaults the crop box to the media box, and
// the bleed, trim and art boxes to the crop box, which is what the reader
// reads in its place.
func (s *pageTreeRepair) repairBoundary(ctx context.Context, key ast.PdfName, box ast.PdfArray, out *cowDict) (ast.PdfArray, bool, error) {
	rect, usable, exact, err := s.readBoundary(ctx, box)
	if err != nil {
		return nil, false, err
	}
	switch {
	case usable && exact:
		return box, true, nil
	case usable:
		out.set(key, rect)
		return rect, true, nil
	case key == "MediaBox":
		letter := letterMediaBox()
		out.set(key, letter)
		return letter, false, nil
	default:
		out.del(key)
		return nil, false, nil
	}
}

// readBoundary reads box, a page boundary a node or a page states, as litho
// reads it: rect is the rectangle of its first four entries, a missing one or
// one that is no number read as 0, the reading compliance, remediation and the
// page copies share (see [compliance.PageBoundaryRect]). usable reports that
// rect encloses an area, and exact that box is rect as written: four numbers,
// each of which may be written as a reference (ISO 32000-1, 7.3.10).
//
// It is how pdf.Reader.Page reads a box of four entries or more: [document.Page]
// reads a crop, bleed, trim or art box enclosing no area as absent, with the
// default ISO 32000-1, Table 30 gives it (see clampToMediaBox), and
// pdf.Reader.Page measures the media box on its first four entries, one
// enclosing no area as US Letter. [document.Page] reads a crop, bleed, trim or
// art box of more than four entries as absent too, as pdf.js and poppler do,
// and pdf.Reader.Page a media box of fewer than four as US Letter. Such a box
// that encloses an area is written as the rectangle compliance measured.
//
// # Corpus evidence
//
//   - All corpora: no file holds a crop, bleed, trim or art box of more than
//     four entries, or a media box of fewer, that encloses an area, where the
//     two readings part.
func (s *pageTreeRepair) readBoundary(ctx context.Context, box ast.PdfArray) (rect ast.PdfArray, usable, exact bool, err error) {
	// The corners are read with no document to resolve against: the
	// references among them are resolved here, alloc first, as the output
	// holds them.
	resolved, cloned := box, false
	for i := range min(len(box), 4) {
		if _, isRef := box[i].(ast.PdfIndirectRef); !isRef {
			continue
		}
		obj, err := s.resolve(ctx, box[i])
		if err != nil {
			return nil, false, false, err
		}
		if _, isRef := obj.(ast.PdfIndirectRef); isRef {
			obj = ast.PdfNull{}
		}
		if !cloned {
			resolved, cloned = slices.Clone(box), true
		}
		resolved[i] = obj
	}
	rect, differs := compliance.PageBoundaryRect(ctx, nil, resolved)
	return rect, enclosesArea(rect), !differs, nil
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
