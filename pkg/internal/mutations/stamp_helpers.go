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
// starts at (0, 0). They often do not: 4% of the corpus files measured carry a
// non-zero origin, including govdocs thread_000_000018.pdf at (-306, -396),
// where an unshifted stamp landed in the middle of the page and ran off the
// right edge, losing its last character to the crop.
//
// The CropBox is the right box because it is the area a viewer shows (ISO
// 32000-1, 14.11.2), and [document.Page] already resolves it with the
// MediaBox fallback the spec requires.
func stampOriginShift(page *document.Page) (dx, dy float64, ok bool) {
	box := page.CropBox
	if len(box) < 2 {
		box = page.MediaBox
	}
	if len(box) < 2 {
		return 0, 0, false
	}
	x0, xOK := numeric(box[0])
	y0, yOK := numeric(box[1])
	if !xOK || !yOK || (x0 == 0 && y0 == 0) {
		return 0, 0, false
	}
	return x0, y0, true
}

// numeric reads a PDF number as a float, reporting whether it was one.
func numeric(obj ast.PdfObject) (float64, bool) {
	switch v := obj.(type) {
	case ast.PdfInt:
		return float64(v), true
	case ast.PdfReal:
		return float64(v), true
	}
	return 0, false
}

// applyStampToPages appends a content stream of operations to each selected
// page, and merges the given named resources into each page's /Resources
// dictionary. All updates for a page are applied in a single AllocAt call so
// that multiple resource categories (font + ExtGState + XObject) survive
// together.
//
// contentOps must be a self-contained sequence of operators, typically
// wrapped in q/Q to avoid leaking graphics state into the host page.
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
	stampStream := ast.NewStream(ast.PdfDict{}, []byte(contentOps))
	stampRef := alloc.Alloc(stampStream)

	// Pages whose box starts at the origin share one stamp stream. A page whose
	// box starts elsewhere needs its own, translated copy: see stampOriginShift.
	shifted := make(map[string]ast.PdfIndirectRef)

	brackets := newContentBrackets(alloc)

	for _, pageNum := range pageNums {
		if pageNum < 1 || pageNum > len(pages) {
			doc.Logger.Debug("skipping out-of-range stamp page",
				slog.Int("page", pageNum),
				slog.Int("total", len(pages)),
			)
			continue
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

		newDict["Contents"] = appendPageContents(ctx, doc, alloc, baseDict, brackets, pageStampRef)

		// A page inlined in a /Kids array has no object number to replace. Skip
		// it rather than abort the whole stamp over one unaddressable page, but
		// say so: the alternative was writing it back at object 0, which every
		// reader ignores (see [writer.ObjAllocator.AllocAt]).
		if !alloc.AllocAt(page.Ref.ObjectNum, newDict) {
			doc.Logger.Debug("skipping stamp on a page with no indirect reference",
				slog.Int("page", pageNum),
			)
		}
	}
	return nil
}

// newContentBrackets returns a function yielding the pair of streams that wrap
// a page's existing content in q/Q, allocating them on first use.
//
// A page's operators run to the end of the stream, so any graphics state the
// page leaves unbalanced is still in force when a stamp is appended (ISO
// 32000-1, 8.4.4). Bracketing the original content restores the default state
// first, which is what a stamp's absolute coordinates assume: pdfjs bug946506
// opens with a bare "16 0 0 16 0 0 cm" and no q, so an unbracketed 36pt stamp
// was drawn at 576pt and ran off the page.
//
// The pair is allocated at most once per call and shared by every page: the
// streams are immutable and carry no page-specific state. Allocating lazily
// keeps them out of a document whose pages all lack /Contents, where nothing
// would reference them and the writer does not prune unreachable objects.
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
// may be separate elements. When the page already holds an array, from multiple
// original streams or a prior stamp, the new stream is appended to it; wrapping
// the array as one element would nest it, and a conforming reader treats the
// nested array as a non-stream entry and drops all the content it holds.
func appendPageContents(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, baseDict ast.PdfDict, brackets func() (ast.PdfIndirectRef, ast.PdfIndirectRef), stampRef ast.PdfIndirectRef) ast.PdfObject {
	existing, ok := baseDict["Contents"]
	if !ok {
		return stampRef
	}
	bracket := func(elems ...ast.PdfObject) ast.PdfArray {
		saveRef, restoreRef := brackets()
		out := make(ast.PdfArray, 0, len(elems)+3)
		out = append(out, saveRef)
		out = append(out, contentStreamElements(ctx, doc, alloc, elems)...)
		out = append(out, restoreRef, stampRef)
		return out
	}
	if arr, isArr := existing.(ast.PdfArray); isArr {
		return bracket(arr...)
	}
	// The array may be reached through a reference, which a type assertion on
	// the raw value misses: pdfjs issue12963.pdf holds /Contents as a reference
	// to a two-stream array, and wrapping that reference produced
	// [[streamA streamB] stamp]. A conforming reader drops the nested array, so
	// the page lost every one of its 1241 characters.
	if ref, isRef := existing.(ast.PdfIndirectRef); isRef {
		if arr, err := document.ResolveArray(ctx, doc, ref); err == nil && arr != nil {
			return bracket(arr...)
		}
	}
	return bracket(existing)
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

// mergeStampResources reads the current /Resources sub-dictionaries from
// baseDict and returns a new /Resources dict that includes the stamp's
// additions. Existing resources are preserved.
func mergeStampResources(
	ctx context.Context,
	doc *document.Document,
	_alloc *writer.ObjAllocator,
	baseDict ast.PdfDict,
	page *document.Page,
	additions []stampResource,
) ast.PdfDict {
	// Group additions by category so we only rebuild each sub-dict once.
	byCategory := make(map[ast.PdfName][]stampResource)
	for _, r := range additions {
		byCategory[r.category] = append(byCategory[r.category], r)
	}

	// Clone the current /Resources sub-tree one level deep. When the page
	// object carries no /Resources of its own it inherits them from an
	// ancestor /Pages node (ISO 32000-1 §7.7.3.4). Writing a page-level
	// /Resources holding only the stamp additions would, for a conforming
	// reader that stops inheriting once a page defines /Resources, hide the
	// inherited fonts and break the page's original content. Seed from the
	// resolved (inheritance-merged) resources so the stamped page carries
	// everything it needs.
	var current ast.PdfDict
	if resObj, ok := baseDict["Resources"]; ok {
		if d, err := document.ResolveDict(ctx, doc, resObj); err == nil && d != nil {
			current = d
		}
	} else if page != nil && page.Resources != nil {
		current = page.Resources
	}
	newResources := make(ast.PdfDict, len(current)+len(byCategory))
	maps.Copy(newResources, current)

	for category, entries := range byCategory {
		// Start from the existing sub-dict for this category, if any.
		subDict := ast.PdfDict{}
		if existing, ok := newResources[category]; ok {
			if d, err := document.ResolveDict(ctx, doc, existing); err == nil && d != nil {
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
// array is therefore lossless to drop and harmful to keep: pdfjs issue18973.pdf
// page 45 begins its /Contents with a null, and appending a flattened appearance
// after it produced a page poppler rendered as nothing at all. The page had
// carried 163 characters of signature text through the annotation before the
// flatten, and 0 after. Dropping the null recovers 542, because the same null was
// already hiding the page's own body text in the source.
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
