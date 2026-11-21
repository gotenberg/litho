package remediation

import (
	"context"
	"fmt"
	"maps"
	"math"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// maxUserUnitShift bounds the power of two a page's user space unit grows
// by: 2^16 = 65,536 stays within the 75,000 ISO 32000-1, Annex C.2 names
// as the largest /UserUnit a viewer of the reference implementation takes,
// and a box past 14,400 units by more than that is left unrepaired.
const maxUserUnitShift = 16

// maxDestDepth bounds the direct dictionaries and arrays [scaleDests]
// descends through inside one object. Anti-DoS guard, not spec-mandated: a
// destination sits one or two levels deep (an outline item's /A, a name
// tree's leaf array), and a forged nest cannot exhaust the stack.
const maxDestDepth = 32

// pageBoxKeys are the page boundaries of ISO 32000-1, 14.11.2, Table 30.
var pageBoxKeys = []ast.PdfName{"MediaBox", "CropBox", "BleedBox", "TrimBox", "ArtBox"}

// layoutLengthKeys are the attributes of the Layout owner whose values are
// lengths in default user space units (ISO 32000-1, 14.8.5.4, Tables 343
// to 346), which a page's new unit divides.
var layoutLengthKeys = []ast.PdfName{
	"SpaceBefore", "SpaceAfter", "StartIndent", "EndIndent", "TextIndent",
	"BBox", "Width", "Height", "LineHeight", "BaselineShift",
	"TextDecorationThickness", "BorderThickness", "Padding", "ColumnGap", "ColumnWidths",
}

// userUnitScale returns the smallest power of two k that brings every side
// of boxes within [pdfAPageMax] once divided by it, keeping each at least
// [pdfAPageMin], and false when no k up to 2^[maxUserUnitShift] does.
//
// A power of two divides every coordinate exactly in binary floating
// point, and 1/k is a short decimal, so the rewritten page holds the
// source's numbers to the last bit, on every machine; the smallest integer
// would leave thirds and sevenths rounded in every box and rectangle.
func userUnitScale(boxes []ast.PdfArray) (float64, bool) {
	longest, shortest := 0.0, math.Inf(1)
	for _, b := range boxes {
		w := math.Abs(document.Float(b[2]) - document.Float(b[0]))
		h := math.Abs(document.Float(b[3]) - document.Float(b[1]))
		longest = max(longest, w, h)
		shortest = min(shortest, w, h)
	}
	for shift := 1; shift <= maxUserUnitShift; shift++ {
		k := math.Ldexp(1, shift)
		if longest/k <= pdfAPageMax {
			return k, shortest/k >= pdfAPageMin
		}
	}
	return 0, false
}

// userUnitPage is a page [rewriteUserUnit] gave a larger user space unit:
// its object and the factor its coordinates were divided by.
type userUnitPage struct {
	ref ast.PdfIndirectRef
	k   float64
}

// rewriteUserUnit returns page, the repaired dictionary of pe, rewritten so
// that every page boundary lies within [pdfAPageMax] units without losing
// any of the page: its /UserUnit multiplied by the smallest power of two k
// that does it ([userUnitScale]), and every coordinate the page gives in
// default user space divided by k (ISO 32000-1, 7.7.3.3, Table 30: one unit
// is /UserUnit times 1/72 inch). The page prints at the size it had.
//
//   - Each page boundary, the inherited media and crop boxes written on the
//     page.
//   - The content, under one "1/k 0 0 1/k 0 0 cm" ahead of the page's
//     content streams (7.8.2: they read as one stream), which maps what
//     they draw into the new units; forms, images, text, shadings painted
//     through the CTM follow.
//   - The patterns of the page's resources, whose matrix maps pattern space
//     to the page's default space, not the CTM (8.7.3.1): copies, their
//     matrix divided by k, so another page sharing one keeps it.
//   - The annotations: /Rect, which places the appearance (12.5.5), and
//     /QuadPoints, /Vertices, /L, /InkList, /CL, /RD, the border widths and
//     dashes of /Border and /BS, and the font size of /DA.
//   - The beads of the page's threads (/B, 12.4.3) and the bounding boxes
//     of its viewports (/VP, 12.9).
//
// The destinations into the page and the structure attributes on it are
// in other objects: [scaleUserUnitRefs] divides them once every page is
// rewritten. It returns false, the page unchanged, when the page cannot be
// rewritten safely: a box no viewer uses or no power of two that fits, an
// oversized box a node of the page tree states, or a pattern or content
// stream it cannot read.
func rewriteUserUnit(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, pe annotPageEntry, page ast.PdfDict) (ast.PdfDict, float64, bool) {
	boxes := map[ast.PdfName]ast.PdfArray{}
	var all []ast.PdfArray
	for _, key := range pageBoxKeys {
		entry, own := page[key]
		if !own || entry == nil {
			if key != "MediaBox" && key != "CropBox" {
				continue
			}
			// The media and crop boxes are inherited (7.7.3.4): the page
			// is written with its own, divided too.
			entry = inheritedEntry(pe, key)
			if entry == nil {
				continue
			}
		}
		arr, err := document.ResolveArray(ctx, doc, entry)
		if err != nil || arr == nil {
			return nil, 0, false
		}
		arr = document.ResolveRectangle(ctx, doc, arr)
		if !document.UsableBox(arr) {
			return nil, 0, false
		}
		boxes[key] = arr
		all = append(all, arr)
	}
	if boxes["MediaBox"] == nil {
		return nil, 0, false
	}
	for node := pe.parent; node != nil; node = node.parent {
		if oversizedBoxes(ctx, doc, node.dict) {
			// A node's box holds every page under it: rewriting one page
			// leaves the node's violation, and the rest of its pages.
			return nil, 0, false
		}
	}
	k, ok := userUnitScale(all)
	if !ok {
		return nil, 0, false
	}
	out := make(ast.PdfDict, len(page)+2)
	maps.Copy(out, page)
	for key, arr := range boxes {
		out[key] = scaleNumbers(arr, k)
	}
	unit := 1.0
	if u := document.Float(page["UserUnit"]); u > 0 {
		unit = u
	}
	out["UserUnit"] = ast.PdfReal(unit * k)

	prefix := alloc.Alloc(ast.NewStream(ast.PdfDict{}, fmt.Appendf(nil, "%s 0 0 %s 0 0 cm\n", writer.FormatReal(1/k), writer.FormatReal(1/k))))
	switch c := page["Contents"].(type) {
	case nil:
	case ast.PdfIndirectRef:
		if arr, isArray := liveObject(ctx, alloc, doc, c).(ast.PdfArray); isArray {
			out["Contents"] = append(ast.PdfArray{prefix}, arr...)
		} else {
			out["Contents"] = ast.PdfArray{prefix, c}
		}
	case ast.PdfArray:
		out["Contents"] = append(ast.PdfArray{prefix}, c...)
	default:
		return nil, 0, false
	}

	res := liveDictValue(ctx, alloc, doc, nearestResources(pe, page))
	if patterns := liveDictValue(ctx, alloc, doc, res["Pattern"]); len(patterns) > 0 {
		scaled := make(ast.PdfDict, len(patterns))
		for name, p := range patterns {
			copied, ok := scaledPattern(ctx, alloc, doc, p, k)
			if !ok {
				return nil, 0, false
			}
			scaled[name] = copied
		}
		newRes := make(ast.PdfDict, len(res))
		maps.Copy(newRes, res)
		newRes["Pattern"] = scaled
		out["Resources"] = newRes
	}

	if annots := liveArray(ctx, alloc, doc, page["Annots"]); len(annots) > 0 {
		inline := make(ast.PdfArray, len(annots))
		for i, entry := range annots {
			inline[i] = entry
			annot := liveAnnotation(ctx, alloc, doc, entry)
			if annot == nil {
				continue
			}
			scaled := scaleAnnotation(annot, k)
			if ref, isRef := entry.(ast.PdfIndirectRef); isRef && ref.ObjectNum > 0 {
				alloc.AllocAt(ref.ObjectNum, scaled)
			} else {
				inline[i] = scaled
			}
		}
		if _, isRef := page["Annots"].(ast.PdfIndirectRef); !isRef {
			out["Annots"] = inline
		} else if ref := page["Annots"].(ast.PdfIndirectRef); ref.ObjectNum > 0 {
			alloc.AllocAt(ref.ObjectNum, inline)
		}
	}
	for _, bead := range liveArray(ctx, alloc, doc, page["B"]) {
		ref, isRef := bead.(ast.PdfIndirectRef)
		d := liveDictValue(ctx, alloc, doc, bead)
		if !isRef || d == nil {
			continue
		}
		if r, ok := d["R"].(ast.PdfArray); ok {
			nd := make(ast.PdfDict, len(d))
			maps.Copy(nd, d)
			nd["R"] = scaleNumbers(document.ResolveRectangle(ctx, doc, r), k)
			alloc.AllocAt(ref.ObjectNum, nd)
		}
	}
	if vps := liveArray(ctx, alloc, doc, page["VP"]); len(vps) > 0 {
		scaled := make(ast.PdfArray, len(vps))
		for i, vp := range vps {
			scaled[i] = vp
			if d := liveDictValue(ctx, alloc, doc, vp); d != nil {
				if b, ok := d["BBox"].(ast.PdfArray); ok {
					nd := make(ast.PdfDict, len(d))
					maps.Copy(nd, d)
					nd["BBox"] = scaleNumbers(document.ResolveRectangle(ctx, doc, b), k)
					scaled[i] = nd
				}
			}
		}
		out["VP"] = scaled
	}
	return out, k, true
}

// inheritedEntry returns key as the nearest node above pe states it, nil
// for none (ISO 32000-1, 7.7.3.4).
func inheritedEntry(pe annotPageEntry, key ast.PdfName) ast.PdfObject {
	for node, depth := pe.parent, 0; node != nil && depth <= maxPageTreeDepth; node, depth = node.parent, depth+1 {
		if hasNonNullEntry(node.dict, key) {
			return node.dict[key]
		}
	}
	return nil
}

// oversizedBoxes reports whether dict states a page boundary with a side
// past [pdfAPageMax].
func oversizedBoxes(ctx context.Context, doc *document.Document, dict ast.PdfDict) bool {
	for _, key := range pageBoxKeys {
		arr, err := document.ResolveArray(ctx, doc, dict[key])
		if err != nil || len(arr) != 4 {
			continue
		}
		arr = document.ResolveRectangle(ctx, doc, arr)
		w := math.Abs(document.Float(arr[2]) - document.Float(arr[0]))
		h := math.Abs(document.Float(arr[3]) - document.Float(arr[1]))
		if w > pdfAPageMax || h > pdfAPageMax {
			return true
		}
	}
	return false
}

// scaleNumbers returns a copy of arr with every number divided by k and
// every array in it handled the same way; other values are kept.
func scaleNumbers(arr ast.PdfArray, k float64) ast.PdfArray {
	out := make(ast.PdfArray, len(arr))
	for i, v := range arr {
		switch n := v.(type) {
		case ast.PdfInt, ast.PdfReal:
			out[i] = ast.PdfReal(document.Float(n) / k)
		case ast.PdfArray:
			out[i] = scaleNumbers(n, k)
		default:
			out[i] = v
		}
	}
	return out
}

// scaledPattern returns a copy of the pattern p names with its /Matrix
// divided by k (ISO 32000-1, 8.7.3.1, Table 75 and Table 76), allocated as
// an object of its own, and false when p reads as no pattern.
func scaledPattern(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, p ast.PdfObject, k float64) (ast.PdfObject, bool) {
	ref, isRef := p.(ast.PdfIndirectRef)
	if !isRef {
		return nil, false
	}
	obj := liveObject(ctx, alloc, doc, ref)
	var dict ast.PdfDict
	var raw []byte
	stream, isStream := obj.(ast.PdfStream)
	switch {
	case isStream:
		data, err := stream.Bytes(ctx)
		if err != nil {
			return nil, false
		}
		dict, raw = stream.Dict, data
	default:
		d, ok := obj.(ast.PdfDict)
		if !ok {
			return nil, false
		}
		dict = d
	}
	m := ast.PdfArray{ast.PdfInt(1), ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(1), ast.PdfInt(0), ast.PdfInt(0)}
	if given, err := document.ResolveArray(ctx, doc, dict["Matrix"]); err == nil && len(given) == 6 {
		m = given
	}
	nd := make(ast.PdfDict, len(dict))
	maps.Copy(nd, dict)
	nd["Matrix"] = scaleNumbers(m, k)
	if isStream {
		return alloc.Alloc(ast.NewStream(nd, raw)), true
	}
	return alloc.Alloc(nd), true
}

// scaleAnnotation returns a copy of annot with every coordinate and length
// it gives in its page's default user space divided by k (ISO 32000-1,
// 12.5.2, Table 164, and the subtype tables of 12.5.6).
func scaleAnnotation(annot ast.PdfDict, k float64) ast.PdfDict {
	out := make(ast.PdfDict, len(annot))
	maps.Copy(out, annot)
	for _, key := range []ast.PdfName{"Rect", "QuadPoints", "Vertices", "L", "InkList", "CL", "RD", "Border"} {
		if arr, ok := annot[key].(ast.PdfArray); ok {
			out[key] = scaleNumbers(arr, k)
		}
	}
	if bs, ok := annot["BS"].(ast.PdfDict); ok {
		nbs := make(ast.PdfDict, len(bs))
		maps.Copy(nbs, bs)
		if w, ok := bs["W"]; ok {
			nbs["W"] = ast.PdfReal(document.Float(w) / k)
		}
		if d, ok := bs["D"].(ast.PdfArray); ok {
			nbs["D"] = scaleNumbers(d, k)
		}
		out["BS"] = nbs
	}
	if da, ok := annot["DA"].(ast.PdfString); ok {
		out["DA"] = ast.PdfString(scaleFontSize(string(da), k))
	}
	return out
}

// scaleFontSize returns the default appearance string da (ISO 32000-1,
// 12.7.3.3) with the size operand of its Tf divided by k; da unchanged when
// it holds no Tf with a number before it. A size of 0, auto, stays 0.
func scaleFontSize(da string, k float64) string {
	fields := strings.Fields(da)
	for i, f := range fields {
		if f != "Tf" || i == 0 {
			continue
		}
		size, err := strconv.ParseFloat(fields[i-1], 64)
		if err != nil {
			return da
		}
		fields[i-1] = writer.FormatReal(size / k)
		return strings.Join(fields, " ")
	}
	return da
}

// scaleUserUnitRefs divides, by each page's factor, the coordinates other
// objects give in the default user space of a page [rewriteUserUnit]
// rewrote: the explicit destinations to it (ISO 32000-1, 12.3.2.2, Table
// 151), wherever they are, outline items, named destinations, link and
// go-to actions; and the Layout attributes of the structure elements whose
// /Pg is that page (14.8.5.4), or of every element when the document has
// that one page. It returns how many objects it rewrote.
func scaleUserUnitRefs(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, pages []userUnitPage, onePage bool) (int, error) {
	byNum := make(map[int]float64, len(pages))
	for _, p := range pages {
		byNum[p.ref.ObjectNum] = p.k
	}
	only := 0.0
	if onePage && len(pages) == 1 {
		only = pages[0].k
	}
	return walkIndirectObjects(ctx, alloc, doc,
		func(d ast.PdfDict) (ast.PdfDict, bool) {
			nd, changed := scaleDests(d, byNum, 0)
			out, _ := nd.(ast.PdfDict)
			if attrs, ok := scaleStructAttrs(out, byNum, only); ok {
				return attrs, true
			}
			return out, changed
		},
		func(a ast.PdfArray) (ast.PdfArray, bool) {
			na, changed := scaleDests(a, byNum, 0)
			out, _ := na.(ast.PdfArray)
			return out, changed
		})
}

// scaleDests returns obj with every explicit destination to a page of
// byNum divided by its factor, descending through direct dictionaries and
// arrays, and whether it changed anything. A destination is an array whose
// first element is the page and second the kind of Table 151; /XYZ zoom
// and a null coordinate are kept.
func scaleDests(obj ast.PdfObject, byNum map[int]float64, depth int) (ast.PdfObject, bool) {
	if depth > maxDestDepth {
		return obj, false
	}
	switch v := obj.(type) {
	case ast.PdfArray:
		if k, ok := destScale(v, byNum); ok {
			return scaleDest(v, k), true
		}
		var out ast.PdfArray
		for i, e := range v {
			ne, changed := scaleDests(e, byNum, depth+1)
			if !changed {
				continue
			}
			if out == nil {
				out = make(ast.PdfArray, len(v))
				copy(out, v)
			}
			out[i] = ne
		}
		if out != nil {
			return out, true
		}
	case ast.PdfDict:
		var out ast.PdfDict
		for key, e := range v {
			ne, changed := scaleDests(e, byNum, depth+1)
			if !changed {
				continue
			}
			if out == nil {
				out = make(ast.PdfDict, len(v))
				maps.Copy(out, v)
			}
			out[key] = ne
		}
		if out != nil {
			return out, true
		}
	}
	return obj, false
}

// destScale reports whether a is an explicit destination to a page of
// byNum, and that page's factor.
func destScale(a ast.PdfArray, byNum map[int]float64) (float64, bool) {
	if len(a) < 2 {
		return 0, false
	}
	ref, isRef := a[0].(ast.PdfIndirectRef)
	kind, isName := a[1].(ast.PdfName)
	if !isRef || !isName {
		return 0, false
	}
	k, ok := byNum[ref.ObjectNum]
	if !ok {
		return 0, false
	}
	switch kind {
	case "XYZ", "Fit", "FitH", "FitV", "FitR", "FitB", "FitBH", "FitBV":
		return k, true
	}
	return 0, false
}

// scaleDest returns the destination d with its coordinates divided by k:
// left and top of /XYZ, its zoom kept; the one coordinate of /FitH, /FitV,
// /FitBH and /FitBV; the rectangle of /FitR (Table 151).
func scaleDest(d ast.PdfArray, k float64) ast.PdfArray {
	out := make(ast.PdfArray, len(d))
	copy(out, d)
	last := len(d)
	if d[1] == ast.PdfName("XYZ") {
		last = min(len(d), 4)
	}
	for i := 2; i < last; i++ {
		switch n := d[i].(type) {
		case ast.PdfInt, ast.PdfReal:
			out[i] = ast.PdfReal(document.Float(n) / k)
		}
	}
	return out
}

// scaleStructAttrs returns elem, a structure element, with the lengths of
// its Layout attributes divided by the factor of its page: its /Pg in
// byNum, or only when it names none and the document has one page; false
// when it is no such element or has no such attribute.
func scaleStructAttrs(elem ast.PdfDict, byNum map[int]float64, only float64) (ast.PdfDict, bool) {
	if elem == nil || elem["S"] == nil || elem["A"] == nil {
		return elem, false
	}
	k := only
	if pg, ok := elem["Pg"].(ast.PdfIndirectRef); ok {
		k = byNum[pg.ObjectNum]
	}
	if k == 0 {
		return elem, false
	}
	scaleOne := func(attr ast.PdfObject) (ast.PdfObject, bool) {
		d, ok := attr.(ast.PdfDict)
		if !ok || d["O"] != ast.PdfName("Layout") {
			return attr, false
		}
		nd := make(ast.PdfDict, len(d))
		maps.Copy(nd, d)
		changed := false
		for _, key := range layoutLengthKeys {
			switch v := d[key].(type) {
			case ast.PdfInt, ast.PdfReal:
				nd[key] = ast.PdfReal(document.Float(v) / k)
				changed = true
			case ast.PdfArray:
				nd[key] = scaleNumbers(v, k)
				changed = true
			}
		}
		return nd, changed
	}
	out := make(ast.PdfDict, len(elem))
	maps.Copy(out, elem)
	switch a := elem["A"].(type) {
	case ast.PdfDict:
		na, changed := scaleOne(a)
		if !changed {
			return elem, false
		}
		out["A"] = na
	case ast.PdfArray:
		na := make(ast.PdfArray, len(a))
		changed := false
		for i, e := range a {
			ne, c := scaleOne(e)
			na[i], changed = ne, changed || c
		}
		if !changed {
			return elem, false
		}
		out["A"] = na
	default:
		return elem, false
	}
	return out, true
}
