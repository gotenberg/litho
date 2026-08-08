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

		newDict["Contents"] = appendPageContents(ctx, doc, baseDict, stampRef)

		alloc.AllocAt(page.Ref.ObjectNum, newDict)
	}
	return nil
}

// appendPageContents returns the /Contents value for a page after appending a
// new content stream. A page's /Contents is a single stream or a flat array of
// streams (ISO 32000-1 §7.8.2). When the page already holds an array, from
// multiple original streams or a prior stamp, the new stream is appended to it;
// wrapping the array as one element would nest it, and a conforming reader
// treats the nested array as a non-stream entry and drops all the content it
// holds.
func appendPageContents(ctx context.Context, doc *document.Document, baseDict ast.PdfDict, stampRef ast.PdfIndirectRef) ast.PdfObject {
	existing, ok := baseDict["Contents"]
	if !ok {
		return stampRef
	}
	if arr, isArr := existing.(ast.PdfArray); isArr {
		out := make(ast.PdfArray, 0, len(arr)+1)
		out = append(out, arr...)
		out = append(out, stampRef)
		return out
	}
	// The array may be reached through a reference, which a type assertion on
	// the raw value misses: pdfjs issue12963.pdf holds /Contents as a reference
	// to a two-stream array, and wrapping that reference produced
	// [[streamA streamB] stamp]. A conforming reader drops the nested array, so
	// the page lost every one of its 1241 characters.
	if ref, isRef := existing.(ast.PdfIndirectRef); isRef {
		if arr, err := document.ResolveArray(ctx, doc, ref); err == nil && arr != nil {
			out := make(ast.PdfArray, 0, len(arr)+1)
			out = append(out, arr...)
			out = append(out, stampRef)
			return out
		}
	}
	return ast.PdfArray{existing, stampRef}
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
