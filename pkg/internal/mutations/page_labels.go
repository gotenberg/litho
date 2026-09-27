package mutations

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"math"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// maxPageLabelNodeEntries bounds what one node of a /PageLabels tree litho
// writes lists: 4095 ranges in a /Nums array, or 4095 children in a /Kids
// array. ISO 32000-1, Annex C, Table C.1 bounds an array at 8191 elements and
// ISO 19005-1, 6.1.12 holds a PDF/A-1 file to that bound, so a /Nums array of
// 4095 key and value pairs stays within it.
const maxPageLabelNodeEntries = 4095

// pageLabel is the label one page displays, as the range of the /PageLabels
// number tree covering it gives it (ISO 32000-1, 12.4.2, Table 159).
type pageLabel struct {
	// style is the numbering style, /D, /R, /r, /A or /a, or empty for a label
	// with no numeric portion.
	style ast.PdfName
	// prefix is the label prefix, the bytes of /P as the file holds them.
	prefix ast.PdfString
	// number is the value of the numeric portion, 1 or more. A label with no
	// style shows none.
	number int64
}

// continues reports whether next is the label the range giving prev gives the
// page after it, so one range covers both: a range numbers its pages
// sequentially from its start (Table 159, /St).
func (prev pageLabel) continues(next pageLabel) bool {
	if prev.style != next.style || prev.prefix != next.prefix {
		return false
	}
	return prev.style == "" || next.number == prev.number+1
}

// pageLabels holds the label each page of one document displays.
type pageLabels struct {
	labels []pageLabel
	// stated is false when the document states no labels, and its pages show
	// their page numbers.
	stated bool
}

// at returns the label the page at 0-based index displays. A document stating
// no labels shows its page numbers, which a range of decimal numbers starting
// at 1 reproduces.
func (l pageLabels) at(index int) pageLabel {
	if l.stated {
		return l.labels[index]
	}
	return pageLabel{style: "D", number: int64(index) + 1}
}

// readPageLabels returns the label each of a document's count pages displays
// under the /PageLabels of catalog, read from alloc first.
//
// ISO 32000-1, 12.4.2 keys the tree by page index and has each range run to
// the next key. A key naming no page, a negative one or one past the last
// page, labels nothing. Pages before the first range show an empty label, as
// pdf.js and qpdf show them: the clause requires a range at index 0, so a
// tree without one states no label for them.
//
// A tree whose ranges break Table 159 (a value that is not a dictionary, a
// /Type other than /PageLabel, a style it does not define, a prefix that is
// not a string, a start below 1) reads as no labels. pdf.js refuses such a
// tree whole and shows page numbers, and so does a tree the walk cannot read
// whole (see [readParentTree]), which viewers do not agree on either.
//
// # Corpus evidence
//
//   - All corpora: 520 files carry /PageLabels, 462 govdocs, 44 pdfjs, 13
//     pdfcpu and 1 pdf20, and pdf.js reads labels from 518 of them.
//   - pdfjs issue16114.pdf, a range starting at 0, and govdocs
//     thread_300_300234.pdf, a range that is no dictionary: pdf.js shows
//     their page numbers.
//   - govdocs thread_200_200788.pdf, thread_400_400141.pdf and
//     thread_400_400580.pdf: no range at page index 0, and pdf.js shows the
//     pages before the first one with an empty label, as qpdf does.
//   - govdocs thread_100_100556.pdf: its one range is keyed -2, and pdf.js
//     shows each of its three pages with an empty label.
func readPageLabels(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, catalog ast.PdfDict, count int) pageLabels {
	treeObj, has := catalog["PageLabels"]
	if !has || count <= 0 {
		return pageLabels{}
	}
	entries, _, ok := readParentTree(ctx, alloc, doc, treeObj)
	if !ok {
		doc.Logger.Debug("the /PageLabels number tree cannot be read whole: the pages show their numbers")
		return pageLabels{}
	}
	starts := make(map[int]pageLabel, len(entries))
	for key, value := range entries {
		if key < 0 || key >= count {
			continue
		}
		label, ok := pageLabelRange(ctx, alloc, doc, value, count)
		if !ok {
			doc.Logger.Debug("a /PageLabels range breaks ISO 32000-1, Table 159: the pages show their numbers",
				slog.Int("page_index", key),
			)
			return pageLabels{}
		}
		starts[key] = label
	}

	labels := make([]pageLabel, count)
	current := pageLabel{number: 1}
	for i := range labels {
		if start, ok := starts[i]; ok {
			current = start
		} else if i > 0 {
			current.number++
		}
		labels[i] = current
	}
	return pageLabels{labels: labels, stated: true}
}

// pageLabelRange reads the page label dictionary a /PageLabels range starts
// with (ISO 32000-1, Table 159), and false when it breaks that table. A start
// that would overflow numbering the document's count pages from it is refused
// too.
func pageLabelRange(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, value ast.PdfObject, count int) (pageLabel, bool) {
	dict, err := resolveDictAllocFirst(ctx, alloc, doc, value)
	if err != nil || dict == nil {
		return pageLabel{}, false
	}
	entry := func(key ast.PdfName) (ast.PdfObject, bool) {
		obj, has := dict[key]
		if !has {
			return nil, false
		}
		resolved, err := resolveAllocFirst(ctx, alloc, doc, obj)
		if err != nil {
			return nil, false
		}
		// ISO 32000-1, 7.3.9: an entry whose value is null does not exist.
		if _, isNull := resolved.(ast.PdfNull); isNull || resolved == nil {
			return nil, false
		}
		return resolved, true
	}

	label := pageLabel{number: 1}
	if typ, has := entry("Type"); has {
		if name, _ := typ.(ast.PdfName); name != "PageLabel" {
			return pageLabel{}, false
		}
	}
	if style, has := entry("S"); has {
		name, _ := style.(ast.PdfName)
		switch name {
		case "D", "R", "r", "A", "a":
			label.style = name
		default:
			return pageLabel{}, false
		}
	}
	if prefix, has := entry("P"); has {
		str, isString := prefix.(ast.PdfString)
		if !isString {
			return pageLabel{}, false
		}
		label.prefix = str
	}
	if start, has := entry("St"); has {
		n, ok := pageLabelStart(start)
		if !ok || n > math.MaxInt64-int64(count) {
			return pageLabel{}, false
		}
		label.number = n
	}
	return label, true
}

// pageLabelStart reads a range's /St, an integer of 1 or more (Table 159). A
// real with no fractional part is read as that integer, as pdf.js reads it:
// its parser makes no difference between the two.
func pageLabelStart(obj ast.PdfObject) (int64, bool) {
	switch v := obj.(type) {
	case ast.PdfInt:
		return int64(v), v >= 1
	case ast.PdfReal:
		f := float64(v)
		if f < 1 || f != math.Trunc(f) || f >= math.MaxInt64 {
			return 0, false
		}
		return int64(f), true
	}
	return 0, false
}

// pageLabelTree returns the /PageLabels number tree giving each page the label
// labels holds for it: one range per run of labels a single range numbers,
// the first at page index 0 as ISO 32000-1, 12.4.2 requires. Its nodes list
// at most [maxPageLabelNodeEntries] entries (see [numberTreeOf]).
//
// # Corpus evidence
//
//   - All corpora: the 520 files carrying /PageLabels, split whole, in half
//     and reversed, merged with themselves and after an unlabelled document,
//     given an unlabelled document's pages, stripped of their first page and
//     given their last page first, make 3947 outputs, each showing in pdf.js
//     the labels its pages showed in their sources. Before, 3367 of them
//     showed other labels.
//   - All corpora: qpdf warned on 18 of those outputs before and on 6 after,
//     the 6 the damage of pdfjs issue13783.pdf carries into every copy. The
//     other 12 were the trees of govdocs thread_200_200394.pdf,
//     thread_300_300234.pdf and thread_400_400580.pdf and pdfjs
//     TAMReview.pdf, which an insertion, a deletion and a move kept as they
//     were.
func pageLabelTree(alloc *writer.ObjAllocator, labels []pageLabel) ast.PdfDict {
	return numberTreeOf(alloc, pageLabelNums(labels), maxPageLabelNodeEntries)
}

// pageLabelNums returns the keys and values of the /PageLabels tree
// [pageLabelTree] builds, in key order.
func pageLabelNums(labels []pageLabel) ast.PdfArray {
	var nums ast.PdfArray
	for i, label := range labels {
		if i > 0 && labels[i-1].continues(label) {
			continue
		}
		rangeDict := ast.PdfDict{}
		if label.style != "" {
			rangeDict["S"] = label.style
			if label.number != 1 {
				rangeDict["St"] = ast.PdfInt(label.number)
			}
		}
		if label.prefix != "" {
			rangeDict["P"] = label.prefix
		}
		nums = append(nums, ast.PdfInt(i), rangeDict)
	}
	return nums
}

// numberTreeOf returns the root of a number tree holding nums, its keys and
// values in key order, whose nodes list at most perNode entries, at least 2.
//
// A tree of up to perNode keys is one root holding /Nums, written directly
// where the caller puts it. A larger one is split into leaves the allocator
// numbers, grouped under intermediate nodes until the root lists at most
// perNode of them: /Kids lists indirect references, and every node but the
// root carries the /Limits of its keys (ISO 32000-1, 7.9.7, Table 37).
func numberTreeOf(alloc *writer.ObjAllocator, nums ast.PdfArray, perNode int) ast.PdfDict {
	if len(nums) <= 2*perNode {
		return ast.PdfDict{"Nums": nums}
	}

	type node struct {
		ref         ast.PdfIndirectRef
		first, last ast.PdfObject
	}
	var level []node
	for start := 0; start < len(nums); start += 2 * perNode {
		end := min(start+2*perNode, len(nums))
		first, last := nums[start], nums[end-2]
		ref := alloc.Alloc(ast.PdfDict{
			"Limits": ast.PdfArray{first, last},
			"Nums":   nums[start:end:end],
		})
		level = append(level, node{ref: ref, first: first, last: last})
	}
	for len(level) > perNode {
		var parents []node
		for start := 0; start < len(level); start += perNode {
			group := level[start:min(start+perNode, len(level))]
			kids := make(ast.PdfArray, len(group))
			for i, child := range group {
				kids[i] = child.ref
			}
			first, last := group[0].first, group[len(group)-1].last
			ref := alloc.Alloc(ast.PdfDict{
				"Limits": ast.PdfArray{first, last},
				"Kids":   kids,
			})
			parents = append(parents, node{ref: ref, first: first, last: last})
		}
		level = parents
	}
	kids := make(ast.PdfArray, len(level))
	for i, child := range level {
		kids[i] = child.ref
	}
	return ast.PdfDict{"Kids": kids}
}

// heldPageLabels returns the label of each page a page tree listing refs
// holds, labelOf giving the label of the page it lists at index i.
//
// A page listed again is held once, where it is first listed, and a kid with
// no object number not at all, as the page tree a rewrite writes holds them:
// [pageTreeRepair] drops a kid it has already walked, a page object having one
// parent (ISO 32000-1, 7.7.3.3), so the labels follow the pages the output
// holds. A split or an insertion naming a page twice writes a copy for each
// naming (see [shareRepeatedPages]), and lists no page twice itself.
func heldPageLabels(refs []ast.PdfIndirectRef, labelOf func(i int) pageLabel) []pageLabel {
	seen := make(map[int]bool, len(refs))
	labels := make([]pageLabel, 0, len(refs))
	for i, ref := range refs {
		if ref.ObjectNum <= 0 || seen[ref.ObjectNum] {
			continue
		}
		seen[ref.ObjectNum] = true
		labels = append(labels, labelOf(i))
	}
	return labels
}

// copiedPageRefs returns the references [copyPageForOutput] returned for the
// pages a split or a merge copied. Each is an indirect reference; anything
// else has no object number, which [heldPageLabels] skips.
func copiedPageRefs(pageRefs []ast.PdfObject) []ast.PdfIndirectRef {
	refs := make([]ast.PdfIndirectRef, len(pageRefs))
	for i, obj := range pageRefs {
		refs[i], _ = obj.(ast.PdfIndirectRef)
	}
	return refs
}

// splitPageLabels returns the /PageLabels of a split whose page tree lists
// pageRefs, the copies of the pages at the 0-based indices of doc's count
// pages, each page the label it displays in doc. It returns false when doc
// states no labels: a split of a document with none has none either.
func splitPageLabels(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, count int, pageRefs []ast.PdfObject, indices []int) (ast.PdfDict, bool) {
	source := documentPageLabels(ctx, doc, count)
	if !source.stated {
		return nil, false
	}
	labels := heldPageLabels(copiedPageRefs(pageRefs), func(i int) pageLabel {
		return source.at(indices[i])
	})
	return pageLabelTree(alloc, labels), true
}

// mergePageLabels returns the /PageLabels of a merge whose page tree lists
// pageRefs, the copies of the pages of sources in order, each page the label
// it displays in its source. The pages of a source stating no labels show
// their page numbers in it, which is what a viewer showed them. It returns
// false when no source states labels.
func mergePageLabels(ctx context.Context, alloc *writer.ObjAllocator, sources []MergeSource, pageRefs []ast.PdfObject) (ast.PdfDict, bool) {
	listed := make([]pageLabel, 0, len(pageRefs))
	stated := false
	for _, src := range sources {
		source := documentPageLabels(ctx, src.Doc, len(src.Pages))
		stated = stated || source.stated
		for i := range src.Pages {
			listed = append(listed, source.at(i))
		}
	}
	if !stated || len(listed) != len(pageRefs) {
		return nil, false
	}
	labels := heldPageLabels(copiedPageRefs(pageRefs), func(i int) pageLabel { return listed[i] })
	return pageLabelTree(alloc, labels), true
}

// documentPageLabels returns the labels of the count pages of doc as its
// catalog states them.
func documentPageLabels(ctx context.Context, doc *document.Document, count int) pageLabels {
	catalogRef, ok := CatalogRef(ctx, doc)
	if !ok {
		return pageLabels{}
	}
	catalog, err := CatalogDict(ctx, doc, catalogRef)
	if err != nil {
		doc.Logger.Debug("resolving the catalog for its page labels failed", slog.Any("err", err))
		return pageLabels{}
	}
	return readPageLabels(ctx, nil, doc, catalog, count)
}

// livePageLabels returns, by object number, the label of each page doc holds
// as alloc stages it, current listing those pages, and whether doc states
// labels. An earlier edit on the same allocator wrote the tree for the pages
// it left, so the tree keys the pages current lists, each held once (see
// [heldPageLabels]).
func livePageLabels(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, catalogRef ast.PdfIndirectRef, current []ast.PdfIndirectRef) (map[int]pageLabel, bool, error) {
	catalog, err := LiveCatalogDict(ctx, alloc, doc, catalogRef)
	if err != nil {
		return nil, false, fmt.Errorf("resolving the catalog for its page labels: %w", err)
	}
	held := make([]ast.PdfIndirectRef, 0, len(current))
	seen := make(map[int]bool, len(current))
	for _, ref := range current {
		if ref.ObjectNum > 0 && !seen[ref.ObjectNum] {
			seen[ref.ObjectNum] = true
			held = append(held, ref)
		}
	}
	labels := readPageLabels(ctx, alloc, doc, catalog, len(held))
	byPage := make(map[int]pageLabel, len(held))
	for i, ref := range held {
		byPage[ref.ObjectNum] = labels.at(i)
	}
	return byPage, labels.stated, nil
}

// relabelPages rewrites the /PageLabels of doc's catalog, as alloc stages it,
// after an edit that changed its pages. labels holds the label each page now
// displays, and stated is false when none of them comes from a document
// stating labels: the catalog then carries none, and a tree it carried, one
// [readPageLabels] could not read, goes rather than stay keyed to pages that
// moved.
func relabelPages(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, catalogRef ast.PdfIndirectRef, labels []pageLabel, stated bool) error {
	catalog, err := LiveCatalogDict(ctx, alloc, doc, catalogRef)
	if err != nil {
		return fmt.Errorf("resolving the catalog for its page labels: %w", err)
	}
	if _, has := catalog["PageLabels"]; !has && !stated {
		return nil
	}
	updated := maps.Clone(catalog)
	if stated {
		updated["PageLabels"] = pageLabelTree(alloc, labels)
	} else {
		delete(updated, "PageLabels")
	}
	alloc.AllocAt(catalogRef.ObjectNum, updated)
	return nil
}

// insertPageLabels rewrites the /PageLabels of doc after an insertion put
// copied, the copies of the pages of srcDoc at the 0-based srcIndices of its
// srcCount pages, at index at of the pages current listed. Each page keeps the
// label it displayed in its document, the pages of a document stating no
// labels showing their page numbers in it, unless neither states any.
func insertPageLabels(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	catalogRef ast.PdfIndirectRef,
	current []ast.PdfIndirectRef,
	at int,
	copied []ast.PdfIndirectRef,
	srcDoc *document.Document,
	srcCount int,
	srcIndices []int,
) error {
	byPage, targetStated, err := livePageLabels(ctx, alloc, doc, catalogRef, current)
	if err != nil {
		return err
	}
	source := documentPageLabels(ctx, srcDoc, srcCount)
	refs := make([]ast.PdfIndirectRef, 0, len(current)+len(copied))
	refs = append(refs, current[:at]...)
	refs = append(refs, copied...)
	refs = append(refs, current[at:]...)
	labels := heldPageLabels(refs, func(i int) pageLabel {
		if i >= at && i < at+len(copied) {
			return source.at(srcIndices[i-at])
		}
		return byPage[refs[i].ObjectNum]
	})
	return relabelPages(ctx, alloc, doc, catalogRef, labels, targetStated || source.stated)
}

// keepPageLabels rewrites the /PageLabels of doc after a deletion kept, of the
// pages current listed, the pages kept lists. Each keeps the label it
// displayed.
func keepPageLabels(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, current, kept []ast.PdfIndirectRef) error {
	catalogRef, ok := CatalogRef(ctx, doc)
	if !ok {
		return fmt.Errorf("trailer missing /Root indirect reference")
	}
	byPage, stated, err := livePageLabels(ctx, alloc, doc, catalogRef, current)
	if err != nil {
		return err
	}
	labels := heldPageLabels(kept, func(i int) pageLabel { return byPage[kept[i].ObjectNum] })
	return relabelPages(ctx, alloc, doc, catalogRef, labels, stated)
}
