package mutations

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"math"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// stampResource describes a single named resource to add to each stamped
// page (a font, an XObject, or a graphics state).
type stampResource struct {
	// category is the resource dictionary key, e.g., "XObject", "Font",
	// "ExtGState".
	category ast.PdfName
	// name is the local resource name used in the content stream,
	// e.g., "Img1".
	name ast.PdfName
	// ref is the indirect reference to the allocated resource object.
	ref ast.PdfIndirectRef
}

// stampOriginShift reports how far a page's visible area starts from the PDF
// coordinate origin, and whether that shift is worth applying.
//
// A stamp's X and Y are documented as "points from the page's bottom-left
// corner", but the content stream draws in PDF user space, where (0, 0) is the
// coordinate origin rather than the corner. Those coincide only when the box
// starts at (0, 0), and real files often place it elsewhere, where an
// unshifted stamp lands away from the corner the caller named.
//
// The box is the region a viewer shows (see [document.Page.ViewedBox]): the
// crop box (ISO 32000-1, 14.11.2), reduced to the media box, the media box
// where the crop box is no box a viewer uses, and US Letter from (0, 0) where
// the media box is none either (see [document.UsableBox]). A page an insertion
// staged reads its boxes the same way (see [stagedPage]).
//
// # Corpus evidence
//
//   - All corpora: 4% of the files measured carry a non-zero box origin.
//   - govdocs thread_000_000018.pdf: its box starts at (-306, -396), where an
//     unshifted stamp landed in the middle of the page and ran off the right
//     edge, losing its last character to the crop.
//   - litho viewerboxes_2b.pdf: page 1 states /MediaBox [10 10 300], which a
//     viewer draws as US Letter from (0, 0). Read from its first two entries,
//     it moved the stamp by (10, 10), where the pinned pdf.js now finds it at
//     the X and Y given.
func stampOriginShift(page *document.Page) (dx, dy float64, ok bool) {
	box := page.ViewedBox()
	// A rectangle may name either pair of opposite corners (ISO 32000-1,
	// 7.9.5), and the page's bottom-left corner is the lesser of each.
	x0 := min(document.Float(box[0]), document.Float(box[2]))
	y0 := min(document.Float(box[1]), document.Float(box[3]))
	if x0 == 0 && y0 == 0 {
		return 0, 0, false
	}
	return x0, y0, true
}

// stampPagesInRange refuses a stamp that names a page outside [1,
// len(pages)], as every other page operation does. A stamp used to skip such a
// page with a debug log, so a caller that asked for page 9 of a five-page
// document got an unstamped output and no error. It runs before a builder
// allocates anything, so a refused stamp leaves nothing behind in the
// allocator. An empty pageNums never reaches here empty: [pagesToEdit] expands
// it to every staged page.
func stampPagesInRange(pages []*document.Page, pageNums []int) error {
	for _, pageNum := range pageNums {
		if pageNum < 1 || pageNum > len(pages) {
			return fmt.Errorf("stamping page %d: %w (document has %d pages)", pageNum, ErrInvalidPageNumber, len(pages))
		}
	}
	return nil
}

// applyStampToPages appends a content stream of operations to each selected
// page, and merges the given named resources into each page's /Resources
// dictionary. All updates for a page are applied in a single AllocAt call so
// that multiple resource categories (font + ExtGState + XObject) survive
// together.
//
// contentOps must be a self-contained sequence of operators, typically
// wrapped in q/Q to avoid leaking graphics state into the host page. It is
// marked as an /Artifact here, so callers pass the bare drawing operators.
func applyStampToPages(
	ctx context.Context,
	doc *document.Document,
	alloc *writer.ObjAllocator,
	pages []*document.Page,
	pageNums []int,
	resources []stampResource,
	contentOps string,
) error {
	if contentOps == "" {
		return nil
	}

	// A stamp is appended after any structure tree was built, so it can never
	// be tagged as real content. ISO 32000-1, 14.8.2.2 names watermarks among
	// the artifacts, and ISO 14289-1, 7.1 test 3 requires page content to be
	// one or the other, so stamping a tagged document without this marking
	// broke its PDF/UA conformance: measured, an autotagged page went from 4
	// violations to 7, this clause plus "text outside the structure tree".
	//
	// Marked HERE rather than in the three builders. contentOps is
	// materialised TWICE below, once as the shared stream and once as the
	// translated copy a non-origin MediaBox needs, and every stamp kind
	// funnels through this function, so one wrap covers both sites and any
	// stamp added later.
	//
	// A bare /Artifact is what both sides recognise: compliance's
	// classifyMarker keys on the tag name alone and needs no property list,
	// and the extractor's artifact stripping does the same.
	contentOps = "/Artifact BMC " + contentOps + " EMC"

	stampStream := ast.NewStream(ast.PdfDict{}, []byte(contentOps))
	stampRef := alloc.Alloc(stampStream)

	// Pages whose box starts at the origin share one stamp stream. A page whose
	// box starts elsewhere needs its own, translated copy: see stampOriginShift.
	shifted := make(map[string]ast.PdfIndirectRef)

	brackets := newContentBrackets(alloc)
	namers := stampedContentsNamers(alloc, pages, pageNums)
	stamped := make(map[[2]int]ast.PdfIndirectRef)

	for _, pageNum := range pageNums {
		// The builders refuse an out-of-range page before allocating anything
		// (see [stampPagesInRange]); this keeps the index below safe for a
		// caller that skipped them.
		if pageNum < 1 || pageNum > len(pages) {
			return fmt.Errorf("stamping page %d: %w (document has %d pages)", pageNum, ErrInvalidPageNumber, len(pages))
		}
		page := pages[pageNum-1]

		// Start from the latest version of this page dict: a previous
		// mutation (or stamp) may have already written an updated version
		// to the allocator.
		baseDict := latestPageDict(alloc, page)

		// Merge the requested resources into the current resource tree.
		newResources := mergeStampResources(ctx, doc, alloc, baseDict, page, resources)

		newDict := make(ast.PdfDict, len(baseDict)+1)
		maps.Copy(newDict, baseDict)
		newDict["Resources"] = newResources

		pageStampRef := stampRef
		if dx, dy, ok := stampOriginShift(page); ok {
			key := FormatFloat(dx) + " " + FormatFloat(dy)
			ref, built := shifted[key]
			if !built {
				ref = alloc.Alloc(ast.NewStream(ast.PdfDict{},
					[]byte("q 1 0 0 1 "+key+" cm "+contentOps+" Q")))
				shifted[key] = ref
			}
			pageStampRef = ref
		}

		// Pages sharing one /Contents object share its stamped content too, as
		// remediation shares its wrapped content, so a form the shared content
		// draws keeps the single reference ISO 14289-1, 7.20 t2 counts there.
		// A direct array cannot be shared, so the array the other pages name
		// is an object of its own.
		contentsRef, isRef := baseDict["Contents"].(ast.PdfIndirectRef)
		if isRef && namers[contentsRef.ObjectNum] > 1 {
			key := [2]int{contentsRef.ObjectNum, pageStampRef.ObjectNum}
			ref, built := stamped[key]
			if !built {
				ref = alloc.Alloc(appendPageContents(ctx, doc, alloc, baseDict, brackets, pageStampRef))
				stamped[key] = ref
			}
			newDict["Contents"] = ref
		} else {
			newDict["Contents"] = appendPageContents(ctx, doc, alloc, baseDict, brackets, pageStampRef)
		}

		// A page inlined in a /Kids array has no object number to replace. Skip
		// it rather than abort the whole stamp over one unaddressable page, but
		// say so: the alternative was writing it back at object 0, which every
		// reader ignores (see writer.ObjAllocator.AllocAt).
		if !alloc.AllocAt(page.Ref.ObjectNum, newDict) {
			doc.Logger.Debug("skipping stamp on a page with no indirect reference",
				slog.Int("page", pageNum),
			)
		}
	}
	return nil
}

// stampedContentsNamers counts, for each object number an indirect /Contents
// names, the stamped pages naming it, each page read alloc-first as the stamp
// reads it.
//
// # Corpus evidence
//
//   - litho formshare_ua1.pdf, sharedtag_ua1.pdf, sharedclose_ua1.pdf: a
//     stamped array for each page of two sharing one /Contents stream made
//     veraPDF parse the stream once per array, so the marked form it draws
//     gained a second reference and the stamp added ISO 14289-1, 7.20 t2 at
//     PDF/UA-1.
func stampedContentsNamers(alloc *writer.ObjAllocator, pages []*document.Page, pageNums []int) map[int]int {
	namers := make(map[int]int)
	for _, pageNum := range pageNums {
		if pageNum < 1 || pageNum > len(pages) {
			continue
		}
		if ref, ok := latestPageDict(alloc, pages[pageNum-1])["Contents"].(ast.PdfIndirectRef); ok {
			namers[ref.ObjectNum]++
		}
	}
	return namers
}

// newContentBrackets returns a function yielding the pair of streams that wrap
// a page's existing content in q/Q, allocating them on first use.
//
// A page's operators run to the end of the stream, so any graphics state the
// page leaves unbalanced is still in force when a stamp is appended (ISO
// 32000-1, 8.4.4). Bracketing the original content restores the default state
// first, which is what a stamp's absolute coordinates assume: a page that
// leaves a cm in force otherwise transforms every stamp appended after it.
//
// The pair is allocated at most once per call and shared by every page: the
// streams are immutable and carry no page-specific state. Allocating lazily
// keeps them out of a document whose pages all lack /Contents, where nothing
// would reference them and the writer does not prune unreachable objects.
//
// # Corpus evidence
//
//   - pdfjs bug946506.pdf: opens with a bare "16 0 0 16 0 0 cm" and no q, so
//     an unbracketed 36pt stamp was drawn at 576pt and ran off the page.
func newContentBrackets(alloc *writer.ObjAllocator) func() (ast.PdfIndirectRef, ast.PdfIndirectRef) {
	var saveRef, restoreRef ast.PdfIndirectRef
	var allocated bool
	return func() (ast.PdfIndirectRef, ast.PdfIndirectRef) {
		if !allocated {
			saveRef = alloc.Alloc(ast.NewStream(ast.PdfDict{}, []byte("q\n")))
			restoreRef = alloc.Alloc(ast.NewStream(ast.PdfDict{}, []byte("\nQ\n")))
			allocated = true
		}
		return saveRef, restoreRef
	}
}

// appendPageContents returns the /Contents value for a page after bracketing
// the existing content in q/Q and appending a new content stream. A page's
// /Contents is a single stream or a flat array of streams (ISO 32000-1 §7.8.2),
// and the array's elements are concatenated into one stream, so the brackets
// may be separate elements; [endDanglingContent] keeps the page's content from
// reading them into a string it never terminates. When the page already holds
// an array, from multiple
// original streams or a prior stamp, the new stream is appended to it; wrapping
// the array as one element would nest it, and a conforming reader treats the
// nested array as a non-stream entry and drops all the content it holds.
//
// # Corpus evidence
//
//   - pdfjs issue12963.pdf: holds /Contents as a reference to a two-stream
//     array, and wrapping that reference produced [[streamA streamB] stamp],
//     so the page lost every one of its 1241 characters.
func appendPageContents(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, baseDict ast.PdfDict, brackets func() (ast.PdfIndirectRef, ast.PdfIndirectRef), stampRef ast.PdfIndirectRef) ast.PdfObject {
	existing, ok := baseDict["Contents"]
	if !ok {
		return stampRef
	}
	bracket := func(elems ...ast.PdfObject) ast.PdfArray {
		saveRef, restoreRef := brackets()
		out := make(ast.PdfArray, 0, len(elems)+3)
		out = append(out, saveRef)
		out = append(out, endDanglingContent(ctx, doc, alloc, contentStreamElements(ctx, doc, alloc, elems))...)
		out = append(out, restoreRef, stampRef)
		return out
	}
	if arr, isArr := existing.(ast.PdfArray); isArr {
		return bracket(arr...)
	}
	// The array may be reached through a reference, which a type assertion on
	// the raw value misses: wrapping that reference produces
	// [[streamA streamB] stamp], and a conforming reader drops the nested
	// array along with every character it draws. An earlier stamp may have
	// queued that array, so it is read alloc-first: read from the document
	// alone, the array two pages sharing /Contents were given was no object
	// at all, and a second stamp left those pages only their new stamp.
	if ref, isRef := existing.(ast.PdfIndirectRef); isRef {
		if arr, err := resolveArrayAllocFirst(ctx, alloc, doc, ref); err == nil && arr != nil {
			return bracket(arr...)
		}
	}
	return bracket(existing)
}

// endDanglingContent returns the /Contents elements of a page with their
// content ended at [content.TerminatedEnd], so that what a writer appends
// after them is read as operators. ISO 32000-1, 7.8.2 reads the elements as
// one stream, and a literal or hex string the page never terminates (7.3.4.2,
// 7.3.4.3) runs to the end of it, so a restoring Q and a stamp appended after
// it were read as string bytes and the page showed nothing of them. The
// element the dangling token starts in is replaced by an unfiltered copy that
// stops at the last operator, and the elements after it, which lie inside the
// token, are dropped. They paint nothing and viewers discard a token
// unterminated at the end of the content, so the page renders as before. The
// source streams are left as they are, since another page may share them.
// Elements litho cannot decode are returned unchanged.
//
// # Corpus evidence
//
//   - litho danglingstr_ua1.pdf: the page ends inside "(dangling ( string",
//     and the stamp the mutate-stamp cell appended was read inside it, so qpdf
//     reported the stamped page as not carrying the stamp.
func endDanglingContent(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, elems []ast.PdfObject) []ast.PdfObject {
	datas := make([][]byte, len(elems))
	starts := make([]int, len(elems))
	var joined []byte
	for i, elem := range elems {
		resolved, err := resolveAllocFirst(ctx, alloc, doc, elem)
		if err != nil {
			doc.Logger.Debug("mutations: content element unresolved, appending after it unchecked", slog.Any("err", err))
			return elems
		}
		stream, ok := resolved.(ast.PdfStream)
		if !ok {
			return elems
		}
		data, err := doc.DecodeStream(ctx, stream)
		if err != nil {
			doc.Logger.Debug("mutations: content element undecodable, appending after it unchecked", slog.Any("err", err))
			return elems
		}
		// Joined as [collectContents] joins them, so the offsets address the
		// content every other reader of the page assembles.
		if i > 0 {
			joined = append(joined, '\n')
		}
		starts[i] = len(joined)
		joined = append(joined, data...)
		datas[i] = data
	}
	end := content.TerminatedEnd(ctx, joined)
	if end >= len(joined) {
		return elems
	}
	k := 0
	for i, start := range starts {
		if start <= end {
			k = i
		}
	}
	out := make([]ast.PdfObject, 0, k+1)
	out = append(out, elems[:k]...)
	if cut := min(end-starts[k], len(datas[k])); cut > 0 {
		kept := datas[k][:cut]
		out = append(out, alloc.Alloc(ast.NewStream(ast.PdfDict{"Length": ast.PdfInt(len(kept))}, kept)))
	}
	return out
}

// latestPageDict returns the most recent version of a page's dict, preferring
// the allocator's pending copy if the page has already been modified.
func latestPageDict(alloc *writer.ObjAllocator, page *document.Page) ast.PdfDict {
	if obj, ok := alloc.Pending()[page.Ref.ObjectNum]; ok {
		if d, ok := obj.(ast.PdfDict); ok {
			return d
		}
	}
	return page.Dict
}

// latestPage returns page as earlier stages staged it. Its dict comes from
// alloc when one is queued there, and so do its resources when that dict names
// its own; inherited resources it does not name stay those of the source.
func latestPage(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, page *document.Page) *document.Page {
	p := *page
	p.Dict = latestPageDict(alloc, page)
	if res, ok := p.Dict["Resources"]; ok {
		obj, err := resolveAllocFirst(ctx, alloc, doc, res)
		if err != nil {
			// Keeping the source's resources is safe: they are what the page
			// resolved to before any stage touched it, and a caller that needs
			// the staged ones fails on its own lookup rather than on this one.
			doc.Logger.Debug("latestPage: staged /Resources did not resolve, keeping the source's",
				slog.Int("page", page.Ref.ObjectNum), slog.Any("err", err))
		} else if d, ok := obj.(ast.PdfDict); ok {
			p.Resources = d
		}
	}
	return &p
}

// mergeStampResources reads the current /Resources sub-dictionaries from
// baseDict and returns a new /Resources dict that includes the stamp's
// additions. Existing resources are preserved.
//
// It reads them as earlier stages staged them, from alloc first. A remediation
// that keeps one round queues its repairs there, a resource dictionary it
// rewrote at its own number among them, and reading the source's copy instead
// wrote the unrepaired resources into the stamped page.
//
// A page stating no /Resources starts from the dictionary it inherits, whole
// (ISO 32000-1, 7.7.3.4), and one stating a null /Resources states none (ISO
// 32000-1, 7.3.7).
//
// # Corpus evidence
//
//   - pdfcpu pkg_testdata__schmager_plateau10.pdf: stamped with text after a
//     PDF/A-2b remediation, its page lost the Default colour space the
//     remediation had added, and 12 uses of DeviceRGB failed ISO 19005-2,
//     6.2.4.3 again.
//   - litho resinherit_2b.pdf: seeded from the Reader's key-by-key merge of
//     the page tree, two stamped pages reached the root's font, which the
//     dictionary they inherit hides, and the page whose /Resources is null
//     lost the font and colour space it inherits.
func mergeStampResources(
	ctx context.Context,
	doc *document.Document,
	alloc *writer.ObjAllocator,
	baseDict ast.PdfDict,
	page *document.Page,
	additions []stampResource,
) ast.PdfDict {
	// Group additions by category so each sub-dict is rebuilt only once.
	byCategory := make(map[ast.PdfName][]stampResource)
	for _, r := range additions {
		byCategory[r.category] = append(byCategory[r.category], r)
	}

	// Clone the current /Resources sub-tree one level deep. When the page
	// object states no /Resources of its own it inherits them from an
	// ancestor /Pages node (ISO 32000-1 §7.7.3.4). Writing a page-level
	// /Resources holding only the stamp additions would, for a conforming
	// reader that stops inheriting once a page defines /Resources, hide the
	// inherited fonts and break the page's original content. Seed from what
	// the page reaches, so the stamped page carries everything it needs and
	// nothing it could not name before (see [pageResourcesEntry]).
	var current ast.PdfDict
	if entry := pageResourcesEntry(baseDict, page); entry != nil {
		if d, err := resolveDictAllocFirst(ctx, alloc, doc, entry); err == nil && d != nil {
			current = d
		}
	}
	newResources := make(ast.PdfDict, len(current)+len(byCategory))
	maps.Copy(newResources, current)

	for category, entries := range byCategory {
		// Start from the existing sub-dict for this category, if any.
		subDict := ast.PdfDict{}
		if existing, ok := newResources[category]; ok {
			if d, err := resolveDictAllocFirst(ctx, alloc, doc, existing); err == nil && d != nil {
				subDict = make(ast.PdfDict, len(d)+len(entries))
				maps.Copy(subDict, d)
			}
		}
		for _, e := range entries {
			subDict[e.name] = e.ref
		}
		newResources[category] = subDict
	}

	return newResources
}

// buildExtGState allocates an ExtGState dictionary with /ca and /CA set to
// the given opacity. Returns the empty name and a zero ref when opacity is
// fully opaque (opacity >= 1 or opacity == 0, which is treated as 1.0 for
// back-compat with the original StampText API).
func buildExtGState(alloc *writer.ObjAllocator, opacity float64) (ast.PdfName, ast.PdfIndirectRef) {
	if opacity <= 0 || opacity >= 1 {
		return "", ast.PdfIndirectRef{}
	}
	dict := ast.PdfDict{
		"Type": ast.PdfName("ExtGState"),
		"ca":   ast.PdfReal(opacity),
		"CA":   ast.PdfReal(opacity),
	}
	ref := alloc.Alloc(dict)
	name := ast.PdfName(fmt.Sprintf("StampGS%d", ref.ObjectNum))
	return name, ref
}

// buildTransformMatrix emits a "sx r1 r2 sy tx ty cm" operator that
// translates by (tx, ty), rotates by the given angle in degrees, and scales
// by (sx, sy). The content stream callers append this string directly.
// An angle of 0 yields the identity rotation.
func buildTransformMatrix(tx, ty, sx, sy, rotationDegrees float64) string {
	if rotationDegrees == 0 {
		return fmt.Sprintf("%s 0 0 %s %s %s cm ",
			FormatFloat(sx), FormatFloat(sy),
			FormatFloat(tx), FormatFloat(ty),
		)
	}
	// PDF coordinate matrix: [sx*cos  sx*sin  -sy*sin  sy*cos  tx  ty]
	rad := rotationDegrees * math.Pi / 180
	c := math.Cos(rad)
	s := math.Sin(rad)
	return fmt.Sprintf("%s %s %s %s %s %s cm ",
		FormatFloat(sx*c), FormatFloat(sx*s),
		FormatFloat(-sy*s), FormatFloat(sy*c),
		FormatFloat(tx), FormatFloat(ty),
	)
}

// extGStateInvocation returns the content-stream fragment that activates the
// given ExtGState, or an empty string if the name is empty.
func extGStateInvocation(name ast.PdfName) string {
	if name == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("/")
	b.WriteString(string(name))
	b.WriteString(" gs ")
	return b.String()
}

// escapePDFString escapes parentheses and backslashes for a PDF literal
// string. Callers must still wrap the result in parentheses.
func escapePDFString(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "(", "\\(")
	s = strings.ReplaceAll(s, ")", "\\)")
	return s
}

// contentStreamElements drops the entries of a /Contents array that name no
// stream, keeping the order of the rest.
//
// ISO 32000-1, 7.8.2 makes a page's content the concatenation of the streams the
// array holds, and 7.3.9 makes a null, or a reference to a missing object,
// equivalent to the entry being absent. Carrying such an entry into a rewritten
// array is therefore lossless to drop and harmful to keep: a reader may render
// nothing of the content appended after it.
//
// # Corpus evidence
//
//   - pdfjs issue18973.pdf: page 45 begins its /Contents with a null, and
//     appending a flattened appearance after it produced a page poppler
//     rendered as nothing at all.
//   - pdfjs issue18973.pdf: page 45 carried 163 characters of signature text
//     through the annotation before the flatten, and 0 after. Dropping the
//     null recovers 542, because the same null was already hiding the page's
//     own body text in the source.
func contentStreamElements(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, elems []ast.PdfObject) []ast.PdfObject {
	out := make([]ast.PdfObject, 0, len(elems))
	for _, elem := range elems {
		if !namesContentStream(ctx, doc, alloc, elem) {
			continue
		}
		out = append(out, elem)
	}
	return out
}

// namesContentStream reports whether a /Contents array entry resolves to a
// stream. Anything else contributes no operators.
//
// The allocator is consulted before the document: a stream a previous stamp
// queued exists only there, and resolving it against the document alone reports
// no such object. Judging it a non-stream on that basis dropped the earlier
// stamp of a two-stamp run, which the writer's own regression test caught.
func namesContentStream(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, elem ast.PdfObject) bool {
	switch v := elem.(type) {
	case ast.PdfStream:
		return true
	case ast.PdfIndirectRef:
		if pending, queued := alloc.Pending()[v.ObjectNum]; queued {
			_, isStream := pending.(ast.PdfStream)
			return isStream
		}
		resolved, err := doc.Resolve(ctx, v)
		if err != nil {
			return false
		}
		_, isStream := resolved.(ast.PdfStream)
		return isStream
	default:
		return false
	}
}
