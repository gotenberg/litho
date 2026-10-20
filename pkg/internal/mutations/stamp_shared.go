package mutations

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// stampDispatchPrefix starts the XObject name a shared /Contents array draws
// each page's stamp form under (see [stampDispatcher]). The object number of
// the /Contents object the array was built from follows it.
const stampDispatchPrefix = "StampShared"

// stampSharing records how one stamp call stamps the pages that share an
// indirect /Contents object, keyed by that object's number.
//
// Pages sharing one /Contents object keep sharing one, so that veraPDF, which
// parses a /Contents object once for the document, still reads a marked form
// the shared content draws once: ISO 14289-1, 7.20 test 2 fails a form holding
// MCIDs that is referenced more than once. A /Contents array cannot nest (ISO
// 32000-1, 7.8.2), so the pages cannot name one array of the shared content
// and a stamp of their own. Two shapes keep them sharing:
//
//   - shared: every page naming the object takes the same stamp in this call,
//     so they all name one array holding the content and that stamp.
//   - dispatch: the pages take different stamps, or some take none. They all
//     name one array holding the content and a Do of [stampDispatchPrefix]
//     name, and each page's own /Resources maps the name to a form holding the
//     stamps it took, empty on a page no stamp named. ISO 32000-1, 7.8.3 reads
//     a content stream under the resources of the page it draws, so each page
//     draws its own stamps and no other's.
//
// Measured with veraPDF 1.30.2 at PDF/UA-1 on hand-built variants of the
// fixtures below, each page stamped by its own call: a /Contents array for
// each page fails 7.20 test 2, and so does each page naming an array of its own
// that holds one stream with the shared content, bracketed, and its stamp,
// whether the arrays are direct or indirect objects. The dispatch shape adds no
// 7.20 rule, and adds only the 7.21.4.1 test 1 of the stamp's own unembedded
// font, as the shared shape does.
//
// # Corpus evidence
//
//   - litho formshare_ua1.pdf, sharedtag_ua1.pdf, sharedclose_ua1.pdf: their
//     pages share one /Contents stream drawing a marked form. Stamping each
//     page by its own call, as the corpus gate's mutate-stamp cell does, gave
//     each page an array of its own, and veraPDF added 7.20 test 2.
type stampSharing struct {
	// members lists, for each object number an indirect /Contents names, the
	// indexes of the pages naming it, read alloc-first.
	members map[int][]int
	// shared and dispatch mark the objects stamped in the shape they name.
	shared   map[int]bool
	dispatch map[int]bool
	// drawsStream reports whether a page of this call draws the stamp stream
	// itself, outside a dispatch form.
	drawsStream bool
}

// planStampSharing reads which shape each stamped page sharing its /Contents
// object takes (see [stampSharing]). It allocates nothing, so a call stamping
// no page that shares its /Contents writes the objects it wrote before.
func planStampSharing(ctx context.Context, alloc *writer.ObjAllocator, pages []*document.Page, pageNums []int, contentOps string) stampSharing {
	plan := stampSharing{
		members:  make(map[int][]int),
		shared:   make(map[int]bool),
		dispatch: make(map[int]bool),
	}
	for i, page := range pages {
		if ctx.Err() != nil {
			break
		}
		if ref, ok := latestPageDict(alloc, page)["Contents"].(ast.PdfIndirectRef); ok {
			plan.members[ref.ObjectNum] = append(plan.members[ref.ObjectNum], i)
		}
	}
	stamps := make(map[int]string)
	for _, pageNum := range pageNums {
		if pageNum >= 1 && pageNum <= len(pages) {
			stamps[pageNum-1] = pageStampOps(pages[pageNum-1], contentOps)
		}
	}
	for _, pageNum := range pageNums {
		if pageNum < 1 || pageNum > len(pages) {
			continue
		}
		ref, ok := latestPageDict(alloc, pages[pageNum-1])["Contents"].(ast.PdfIndirectRef)
		if !ok || len(plan.members[ref.ObjectNum]) < 2 {
			plan.drawsStream = true
			continue
		}
		num := ref.ObjectNum
		if plan.shared[num] || plan.dispatch[num] {
			continue
		}
		if _, built := pendingStampDispatch(ctx, alloc, ref); built {
			plan.dispatch[num] = true
			continue
		}
		if sameStamp(plan.members[num], stamps) {
			plan.shared[num] = true
			plan.drawsStream = true
			continue
		}
		plan.dispatch[num] = true
	}
	return plan
}

// sameStamp reports whether every page of members takes one stamp in this
// call, as stamps lists them by page index.
func sameStamp(members []int, stamps map[int]string) bool {
	first, ok := stamps[members[0]]
	if !ok {
		return false
	}
	for _, i := range members[1:] {
		if ops, stamped := stamps[i]; !stamped || ops != first {
			return false
		}
	}
	return true
}

// pageStampOps returns the operators that draw the stamp on page: contentOps,
// translated where the page's box does not start at the origin (see
// [stampOriginShift]).
func pageStampOps(page *document.Page, contentOps string) string {
	if dx, dy, ok := stampOriginShift(page); ok {
		return stampShiftedOps(FormatFloat(dx)+" "+FormatFloat(dy), contentOps)
	}
	return contentOps
}

// stampShiftedOps returns contentOps translated by key, "dx dy".
func stampShiftedOps(key, contentOps string) string {
	return "q 1 0 0 1 " + key + " cm " + contentOps + " Q"
}

// pendingStampDispatch returns the XObject name the /Contents array ref names
// draws its pages' stamp forms under, when an earlier stamp call of this
// writer built that array for a dispatch (see [stampSharing]). Only an array
// alloc holds counts: a source file's own objects are never read as one.
func pendingStampDispatch(ctx context.Context, alloc *writer.ObjAllocator, ref ast.PdfIndirectRef) (ast.PdfName, bool) {
	arr, ok := alloc.Pending()[ref.ObjectNum].(ast.PdfArray)
	if !ok || len(arr) == 0 {
		return "", false
	}
	last, ok := arr[len(arr)-1].(ast.PdfIndirectRef)
	if !ok {
		return "", false
	}
	stream, ok := alloc.Pending()[last.ObjectNum].(ast.PdfStream)
	if !ok {
		return "", false
	}
	data, err := stream.Bytes(ctx)
	if err != nil {
		return "", false
	}
	name, ok := strings.CutPrefix(string(data), "/")
	if !ok {
		return "", false
	}
	name, ok = strings.CutSuffix(name, " Do\n")
	if !ok || !strings.HasPrefix(name, stampDispatchPrefix) {
		return "", false
	}
	return ast.PdfName(name), true
}

// stampDispatch is a dispatch array (see [stampSharing]): the indirect
// /Contents array the pages share and the XObject name it draws.
type stampDispatch struct {
	contents ast.PdfIndirectRef
	name     ast.PdfName
}

// stampDispatcher stamps the pages of one call that share their /Contents
// object in the dispatch shape (see [stampSharing]).
type stampDispatcher struct {
	doc       *document.Document
	alloc     *writer.ObjAllocator
	pages     []*document.Page
	plan      stampSharing
	brackets  func() (ast.PdfIndirectRef, ast.PdfIndirectRef)
	resources []stampResource
	// stamping holds the indexes of the pages this call stamps.
	stamping map[int]bool
	// built holds the dispatch of each object number this call reached it
	// from: the shared /Contents object and the array built from it.
	built map[int]stampDispatch
}

func newStampDispatcher(
	doc *document.Document,
	alloc *writer.ObjAllocator,
	pages []*document.Page,
	pageNums []int,
	plan stampSharing,
	brackets func() (ast.PdfIndirectRef, ast.PdfIndirectRef),
	resources []stampResource,
) *stampDispatcher {
	stamping := make(map[int]bool, len(pageNums))
	for _, pageNum := range pageNums {
		stamping[pageNum-1] = true
	}
	return &stampDispatcher{
		doc:       doc,
		alloc:     alloc,
		pages:     pages,
		plan:      plan,
		brackets:  brackets,
		resources: resources,
		stamping:  stamping,
		built:     make(map[int]stampDispatch),
	}
}

// forContents returns the dispatch a page whose /Contents is ref stamps
// through, and false when the page takes no dispatch. On first reaching a
// shared object it builds the array, and gives every page naming the object
// that this call does not stamp the array and an empty form of its own.
func (d *stampDispatcher) forContents(ctx context.Context, base ast.PdfDict, ref ast.PdfIndirectRef) (stampDispatch, bool) {
	if got, ok := d.built[ref.ObjectNum]; ok {
		return got, true
	}
	if !d.plan.dispatch[ref.ObjectNum] {
		return stampDispatch{}, false
	}
	if name, ok := pendingStampDispatch(ctx, d.alloc, ref); ok {
		got := stampDispatch{contents: ref, name: name}
		d.built[ref.ObjectNum] = got
		return got, true
	}
	name := d.freeDispatchName(ctx, ref.ObjectNum)
	draw := d.alloc.Alloc(ast.NewStream(ast.PdfDict{}, []byte("/"+string(name)+" Do\n")))
	got := stampDispatch{
		contents: d.alloc.Alloc(appendPageContents(ctx, d.doc, d.alloc, base, d.brackets, draw)),
		name:     name,
	}
	d.built[ref.ObjectNum] = got
	d.built[got.contents.ObjectNum] = got
	for _, i := range d.plan.members[ref.ObjectNum] {
		if d.stamping[i] {
			continue
		}
		page := d.pages[i]
		d.rewrite(ctx, i+1, page, latestPageDict(d.alloc, page), got, d.emptyForm())
	}
	return got, true
}

// freeDispatchName returns the XObject name the dispatch array built from the
// /Contents object contentsNum draws: [stampDispatchPrefix] and the object
// number, with a counter after it when a page naming the object already maps
// that name in its /XObject resources. The page's own content draws its
// XObjects by name through those resources, so mapping a name it already uses
// to the stamp form would replace what the page drew.
func (d *stampDispatcher) freeDispatchName(ctx context.Context, contentsNum int) ast.PdfName {
	var taken []ast.PdfDict
	for _, i := range d.plan.members[contentsNum] {
		page := d.pages[i]
		xobjects, err := resolveDictAllocFirst(ctx, d.alloc, d.doc, d.pageResources(ctx, page, latestPageDict(d.alloc, page))["XObject"])
		if err == nil && xobjects != nil {
			taken = append(taken, xobjects)
		}
	}
	base := fmt.Sprintf("%s%d", stampDispatchPrefix, contentsNum)
	name := ast.PdfName(base)
	for n := 2; slices.ContainsFunc(taken, func(x ast.PdfDict) bool { _, used := x[name]; return used }); n++ {
		name = ast.PdfName(fmt.Sprintf("%s_%d", base, n))
	}
	return name
}

// stamp gives page the dispatch array and a form drawing the stamps it took
// before plus ops.
func (d *stampDispatcher) stamp(ctx context.Context, pageNum int, page *document.Page, base ast.PdfDict, got stampDispatch, ops string) error {
	form, err := d.stampForm(ctx, page, base, got.name, ops)
	if err != nil {
		return fmt.Errorf("stamping page %d: %w", pageNum, err)
	}
	d.rewrite(ctx, pageNum, page, base, got, form)
	return nil
}

// rewrite queues page naming the dispatch array, its resources mapping the
// dispatch name to form.
func (d *stampDispatcher) rewrite(ctx context.Context, pageNum int, page *document.Page, base ast.PdfDict, got stampDispatch, form ast.PdfIndirectRef) {
	newDict := make(ast.PdfDict, len(base)+1)
	maps.Copy(newDict, base)
	newDict["Resources"] = mergeStampResources(ctx, d.doc, d.alloc, base, page,
		[]stampResource{{category: "XObject", name: got.name, ref: form}})
	newDict["Contents"] = got.contents
	if !d.alloc.AllocAt(page.Ref.ObjectNum, newDict) {
		d.doc.Logger.Debug("skipping stamp on a page with no indirect reference",
			slog.Int("page", pageNum),
		)
	}
}

// stampForm queues the form page draws through the dispatch name: the stamps
// its earlier form drew, then ops, under the resources of both. The form
// replaces the earlier one at its number, which no other page draws (see
// [stampDispatcher.emptyForm]). Its bounding box is the region a viewer shows
// (see [document.Page.ViewedBox]), which clips nothing a viewer would show:
// ISO 32000-1, 14.11.2 clips the page's content to its crop box.
func (d *stampDispatcher) stampForm(ctx context.Context, page *document.Page, base ast.PdfDict, name ast.PdfName, ops string) (ast.PdfIndirectRef, error) {
	var data []byte
	resources := make(ast.PdfDict)
	prevRef, prev, replaces := d.pageForm(ctx, page, base, name)
	if replaces {
		prevData, err := prev.Bytes(ctx)
		if err != nil {
			return ast.PdfIndirectRef{}, fmt.Errorf("reading the earlier stamp form: %w", err)
		}
		if len(prevData) > 0 {
			data = append(append(data, prevData...), '\n')
		}
		if prevRes, ok := prev.Dict["Resources"].(ast.PdfDict); ok {
			for category, sub := range prevRes {
				if subDict, ok := sub.(ast.PdfDict); ok {
					resources[category] = maps.Clone(subDict)
				}
			}
		}
	}
	for _, r := range d.resources {
		sub, ok := resources[r.category].(ast.PdfDict)
		if !ok {
			sub = make(ast.PdfDict)
			resources[r.category] = sub
		}
		sub[r.name] = r.ref
	}
	d.defaultColorSpaces(ctx, page, base, resources)
	data = append(data, ops...)
	form := ast.NewStream(ast.PdfDict{
		"Type":      ast.PdfName("XObject"),
		"Subtype":   ast.PdfName("Form"),
		"BBox":      append(ast.PdfArray(nil), page.ViewedBox()...),
		"Resources": resources,
	}, data)
	if replaces {
		d.alloc.AllocAt(prevRef.ObjectNum, form)
		return prevRef, nil
	}
	return d.alloc.Alloc(form), nil
}

// pageResources returns the resources page reaches, read alloc-first, or nil.
func (d *stampDispatcher) pageResources(ctx context.Context, page *document.Page, base ast.PdfDict) ast.PdfDict {
	entry := pageResourcesEntry(base, page)
	if entry == nil {
		return nil
	}
	res, err := resolveDictAllocFirst(ctx, d.alloc, d.doc, entry)
	if err != nil {
		return nil
	}
	return res
}

// defaultColorSpaces copies into resources the default colour spaces page
// states. ISO 32000-1, 8.6.5.6 looks DefaultGray, DefaultRGB and DefaultCMYK
// up in the current resource dictionary, which inside a form is the form's
// own, so a stamp moved into a form would otherwise paint its device colours
// without the page's defaults: a PDF/A page whose /DefaultRGB covers the
// stamp's DeviceRGB then fails ISO 19005-2, 6.2.4.3 test 2.
//
// # Corpus evidence
//
//   - pdfjs issue14847.pdf: every page states /DefaultRGB and shares its
//     /Contents with another, and the stamp form without it added ISO
//     19005-1, 6.2.3.3 test 1 and ISO 19005-2, 6.2.4.3 test 2.
func (d *stampDispatcher) defaultColorSpaces(ctx context.Context, page *document.Page, base ast.PdfDict, resources ast.PdfDict) {
	spaces, err := resolveDictAllocFirst(ctx, d.alloc, d.doc, d.pageResources(ctx, page, base)["ColorSpace"])
	if err != nil {
		return
	}
	for _, name := range []ast.PdfName{"DefaultGray", "DefaultRGB", "DefaultCMYK"} {
		space, ok := spaces[name]
		if !ok {
			continue
		}
		sub, ok := resources["ColorSpace"].(ast.PdfDict)
		if !ok {
			sub = make(ast.PdfDict)
			resources["ColorSpace"] = sub
		}
		sub[name] = space
	}
}

// pageForm returns the form page's resources map name to, when an earlier
// stamp call of this writer queued it.
func (d *stampDispatcher) pageForm(ctx context.Context, page *document.Page, base ast.PdfDict, name ast.PdfName) (ast.PdfIndirectRef, ast.PdfStream, bool) {
	res := d.pageResources(ctx, page, base)
	if res == nil {
		return ast.PdfIndirectRef{}, ast.PdfStream{}, false
	}
	xobjects, err := resolveDictAllocFirst(ctx, d.alloc, d.doc, res["XObject"])
	if err != nil {
		return ast.PdfIndirectRef{}, ast.PdfStream{}, false
	}
	ref, ok := xobjects[name].(ast.PdfIndirectRef)
	if !ok {
		return ast.PdfIndirectRef{}, ast.PdfStream{}, false
	}
	form, ok := d.alloc.Pending()[ref.ObjectNum].(ast.PdfStream)
	return ref, form, ok
}

// emptyForm allocates the form a page the call does not stamp draws through
// the dispatch name. Each page gets one of its own, so a later call stamping
// the page replaces it in place (see [stampDispatcher.stampForm]) and leaves
// no form behind that nothing draws.
func (d *stampDispatcher) emptyForm() ast.PdfIndirectRef {
	return d.alloc.Alloc(ast.NewStream(ast.PdfDict{
		"Type":      ast.PdfName("XObject"),
		"Subtype":   ast.PdfName("Form"),
		"BBox":      ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(0)},
		"Resources": ast.PdfDict{},
	}, nil))
}
