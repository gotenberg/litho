package pdf

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/flatten"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/icc"
	"github.com/gotenberg/litho/pkg/internal/mutations"
	"github.com/gotenberg/litho/pkg/internal/recolor"
	"github.com/gotenberg/litho/pkg/internal/render/page"
)

// groupSpaceReads returns the 0-based indices of the pages of pages that
// litho's renderer reads in the grey or CMYK space their transparency group's
// /CS names ([page.PageBlendSpace]): the pages whose content paints with
// transparency. It is taken before the annotations painting with
// transparency are drawn into their pages, which a viewer draws over the page
// group without making its content transparent.
func groupSpaceReads(ctx context.Context, doc *document.Document, pages []*document.Page) map[int]bool {
	reads := make(map[int]bool)
	for i, pg := range pages {
		if ctx.Err() != nil {
			return reads
		}
		if page.PageBlendSpace(ctx, doc, pg) != nil {
			reads[i] = true
		}
	}
	return reads
}

// groupSpaceOutcome is what [convertGroupSpaces] did to the pages it was
// given, by 0-based index.
type groupSpaceOutcome struct {
	converted, unconverted map[int]bool
}

// convertGroupSpaces converts the content of each flattened page whose
// composites the flattening wrote in the grey or CMYK space its transparency
// group's /CS names ([flatten.PageReport.GroupSpace]), and that litho's
// renderer read in that space (reads, see [groupSpaceReads]), to that space:
// its colour operators, images, shadings and colour spaces, and its
// annotations' appearances, as [Writer.ConvertColors] converts a page.
//
// ISO 32000-1, 11.4.7 and Table 147 convert every colour painted into the
// page group into its colour space, and blend there, so a page whose content
// paints with transparency reads in it, the composites included. Flattening
// removes the group with the transparency (ISO 19005-1, 6.4 forbids it under
// a PDF/A-1 claim, and with no transparency left litho's renderer reads the
// page in sRGB whether it stays or not, [page.PageBlendSpace]), so what the
// composites leave of the page would otherwise read in sRGB: an RGB colour
// outside the CMYK gamut, or any colour on a grey page, would change.
// Converted, it reads as it did, through the conversion the renderer blended
// it with ([groupEngine]).
//
// The conversion rewrites shared objects in place, so the pages converting to
// one space convert together, and a page that paints an object the
// conversion rewrites and a page left out also paints, a page converted to
// another space or not at all, is left out too, reading in sRGB as before,
// rather than changing the page that shares it. An object queued before the
// conversion, which the flattening or an earlier edit wrote, cannot be told
// rewritten, so sharing one leaves a page out too. So is a page whose content
// the conversion cannot read whole, or whose group's space it cannot write (a
// CalGray group, or an ICC profile litho cannot use).
//
// A colour painted under /UseBlackPtComp /OFF (ISO 32000-2, Table 57) enters
// the group's space without compensation, as the renderer blends it, and is
// then read under the same state, where the group was read compensated: its
// shadows read up to 11 levels lighter through a CMYK profile whose black is
// lighter than sRGB's (the version 4 profile v4-cmyk-lut.icc), as those of a
// page [Writer.ConvertColors] converts read.
//
// # Corpus evidence
//
//   - All corpora: 46 files name a grey or CMYK page group; flattening
//     converts 68 pages of 30 of them, each drawing on litho as before
//     within the flatten cell's bars, govdocs thread_200_200620.pdf page 3
//     closer (220 differing pixels at 72 dpi before, none after).
//   - litho: converted to CMYK and then flattened, the 248 files give the
//     same bytes as before; flattened alone, groupspace_2b.pdf alone
//     changes, its pages 1 to 3 drawing within 2 levels of their source at
//     300 dpi where they drew up to 104, 102 and 170 levels apart.
func convertGroupSpaces(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, pages []*document.Page, result *flatten.Report, reads map[int]bool, jpegQuality int) (groupSpaceOutcome, error) {
	out := groupSpaceOutcome{converted: make(map[int]bool), unconverted: make(map[int]bool)}
	byTarget := make(map[string][]int)
	spaces := make(map[string]ast.PdfObject)
	for _, p := range result.Pages {
		if p.GroupSpace == nil || !reads[p.Page] {
			continue
		}
		key := fmt.Sprintf("%v", p.GroupSpace)
		byTarget[key] = append(byTarget[key], p.Page)
		spaces[key] = p.GroupSpace
		out.unconverted[p.Page] = true
	}
	if len(byTarget) == 0 {
		return out, nil
	}
	queued := make(map[int]bool, len(alloc.Pending()))
	for n := range alloc.Pending() {
		queued[n] = true
	}
	painted := make([]map[int]bool, len(pages))
	for i, pg := range pages {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		painted[i] = paintedObjects(ctx, alloc, doc, pg)
	}
	gray, _, cmyk, _ := deviceProfiles(ctx, alloc, doc)
	for _, key := range slices.Sorted(maps.Keys(byTarget)) {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		t, ok := groupTarget(ctx, alloc, doc, spaces[key], gray, cmyk)
		if !ok {
			doc.Logger.Debug("flatten: page group space is no conversion target", slog.Any("cs", spaces[key]))
			continue
		}
		c := groupConversion{
			doc:     doc,
			alloc:   alloc,
			pages:   pages,
			painted: painted,
			queued:  queued,
			target:  t,
			opts: recolor.Options{
				// The renderer reads every colour, and blends it into the
				// page group, under the relative colorimetric intent with
				// black point compensation unless /UseBlackPtComp turns it
				// off (see [groupEngine]).
				Intent:                 icc.RelativeColorimetric,
				BlackPointCompensation: true,
				JPEGQuality:            jpegQuality,
				DeviceGray:             gray,
				DeviceCMYK:             cmyk,
				Engine:                 groupEngine{},
			},
		}
		converted, err := c.convert(ctx, byTarget[key])
		if err != nil {
			return out, err
		}
		for _, i := range converted {
			out.converted[i] = true
			delete(out.unconverted, i)
		}
	}
	return out, nil
}

// groupConversion is the conversion of a set of pages to one page group
// space ([convertGroupSpaces]).
type groupConversion struct {
	doc   *document.Document
	alloc *writer.ObjAllocator
	pages []*document.Page
	// painted holds the objects each page paints ([paintedObjects]), and
	// queued the objects queued on alloc before any conversion.
	painted []map[int]bool
	queued  map[int]bool
	target  recolor.Target
	opts    recolor.Options
}

// convert converts the pages of candidates, returning those it converted.
// Each try that rewrites an object a page left out paints is undone, and the
// pages painting that object are left out of the next: every try leaves at
// least one page out, so there are at most as many tries as candidates.
func (c *groupConversion) convert(ctx context.Context, candidates []int) ([]int, error) {
	members := slices.Clone(candidates)
	for {
		var outside map[int]bool
		members, outside = c.separate(members)
		if len(members) == 0 {
			return nil, nil
		}
		conflicts, ok, err := c.try(ctx, members, outside)
		if err != nil || !ok {
			return nil, err
		}
		if len(conflicts) == 0 {
			return members, nil
		}
		kept := members[:0:0]
		for _, i := range members {
			if !sharesAny(c.painted[i], conflicts) {
				kept = append(kept, i)
			}
		}
		if len(kept) == len(members) {
			// The rewritten object is one no member's walk reached.
			c.doc.Logger.Debug("flatten: page content left in its own colour spaces", slog.String("why", "an object shared with another page"))
			return nil, nil
		}
		members = kept
	}
}

// separate returns the pages of members that paint no queued object a page
// outside them paints, and the objects the pages outside the result paint. A
// page left out joins the others, which can leave out more, so it repeats
// until none is.
func (c *groupConversion) separate(members []int) ([]int, map[int]bool) {
	for {
		in := make(map[int]bool, len(members))
		for _, i := range members {
			in[i] = true
		}
		outside := make(map[int]bool)
		for i, objs := range c.painted {
			if !in[i] {
				maps.Copy(outside, objs)
			}
		}
		kept := members[:0:0]
		for _, i := range members {
			shared := false
			for n := range c.painted[i] {
				if c.queued[n] && outside[n] {
					shared = true
					break
				}
			}
			if !shared {
				kept = append(kept, i)
			}
		}
		if len(kept) == len(members) {
			return kept, outside
		}
		members = kept
	}
}

// try converts the pages of members, and keeps the conversion unless it
// rewrote an object of outside in place, which it returns, undone. It
// reports false, undone, when the conversion fails or cannot read the pages
// whole.
func (c *groupConversion) try(ctx context.Context, members []int, outside map[int]bool) (map[int]bool, bool, error) {
	targetPages := make([]*document.Page, 0, len(members))
	for _, i := range members {
		targetPages = append(targetPages, c.pages[i])
	}
	before := maps.Clone(c.alloc.Pending())
	next := c.alloc.NextObjNum()
	snap := c.alloc.Snapshot()
	discard := func(why string, err error) (map[int]bool, bool, error) {
		c.alloc.Restore(snap)
		c.doc.Logger.Debug("flatten: page content left in its own colour spaces", slog.String("why", why), slog.Any("err", err))
		return nil, false, nil
	}
	session, err := recolor.NewSession(c.doc, c.alloc, c.target, c.opts)
	if err != nil {
		return discard("target", err)
	}
	if err := session.Pages(ctx, targetPages); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			c.alloc.Restore(snap)
			return nil, false, ctxErr
		}
		return discard("conversion", err)
	}
	if skipped := session.Report().Skipped; len(skipped) > 0 {
		return discard("content the conversion cannot read", skipped[0].Err)
	}
	// An object the conversion wrote at a number it did not allocate, and
	// that was not queued before, is one of the file's, rewritten in place.
	conflicts := make(map[int]bool)
	for num := range c.alloc.Pending() {
		if _, queued := before[num]; num < next && !queued && outside[num] {
			conflicts[num] = true
		}
	}
	if len(conflicts) > 0 {
		c.alloc.Restore(snap)
	}
	return conflicts, true, nil
}

// sharesAny reports whether a and b hold a common object number.
func sharesAny(a, b map[int]bool) bool {
	for n := range a {
		if b[n] {
			return true
		}
	}
	return false
}

// groupTarget returns the recolouring target the page group colour space cs
// names: DeviceGray or DeviceCMYK, read through gray or cmyk, or a one- or
// four-component ICCBased space through its own profile. It returns false for
// any other space.
func groupTarget(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, cs ast.PdfObject, gray, cmyk []byte) (recolor.Target, bool) {
	resolved, err := document.ResolveObject(ctx, doc, cs)
	if err != nil {
		return recolor.Target{}, false
	}
	switch v := resolved.(type) {
	case ast.PdfName:
		switch v {
		case graphics.FamilyDeviceGray:
			return recolor.Target{Family: graphics.FamilyDeviceGray, Profile: gray}, true
		case graphics.FamilyDeviceCMYK:
			return recolor.Target{Family: graphics.FamilyDeviceCMYK, Profile: cmyk}, true
		}
	case ast.PdfArray:
		if len(v) < 2 {
			return recolor.Target{}, false
		}
		family, err := document.ResolveName(ctx, doc, v[0])
		if err != nil || family != graphics.FamilyICCBased {
			return recolor.Target{}, false
		}
		profile := decodedStream(ctx, alloc, doc, v[1])
		p, err := icc.Parse(profile)
		if err != nil {
			return recolor.Target{}, false
		}
		switch p.Components() {
		case 1, 4:
			return recolor.Target{Family: graphics.FamilyICCBased, Profile: profile, ColorSpace: cs}, true
		}
	}
	return recolor.Target{}, false
}

// groupEngine converts colour as litho's renderer blends it into a page
// group: into a CMYK group through the ICC engine, as the renderer does from
// the colour's sRGB, and into a grey group by the luminance ISO 32000-1, 10.3
// weighs DeviceRGB's components with, 0.3 red, 0.59 green and 0.11 blue, of
// the colour's sRGB, which is how the renderer blends any colour into a grey
// space. A grey colour stays grey.
type groupEngine struct{}

func (groupEngine) Transform(src, dst recolor.Endpoint, opts recolor.Options) (recolor.Converter, error) {
	base := recolor.ICCEngine()
	if dst.Space != recolor.SpaceGray || src.Space == recolor.SpaceGray {
		conv, err := base.Transform(src, dst, opts)
		if err != nil {
			return nil, fmt.Errorf("converting to the page group's space: %w", err)
		}
		return conv, nil
	}
	toRGB, err := base.Transform(src, recolor.Endpoint{Space: recolor.SpaceRGB, Profile: icc.SRGBProfile()}, opts)
	if err != nil {
		return nil, fmt.Errorf("converting to sRGB on the way to the page group's grey: %w", err)
	}
	return &luminance{toRGB: toRGB}, nil
}

// luminance converts a colour to its sRGB, then to grey by the luminance of
// [groupEngine].
type luminance struct {
	toRGB recolor.Converter
	rgb   [3]float64
}

func (l *luminance) Convert(in, out []float64) {
	l.toRGB.Convert(in, l.rgb[:])
	out[0] = min(max(0.3*l.rgb[0]+0.59*l.rgb[1]+0.11*l.rgb[2], 0), 1)
}

// paintedObjects returns the numbers of the indirect objects pg's painting
// reaches, as the queued edits on alloc leave it: its resources, its content
// streams and its annotations, with their appearances, down through the
// forms, patterns, Type 3 glyphs, soft masks and colour spaces they name. A
// font other than Type 3, whose glyphs paint in the colour the content sets,
// is neither counted nor descended into.
//
// The walk keeps its own stack rather than recursing, and expands each
// indirect object, an array written as one included, once (see
// [document.VisitedBefore]), so its work is bounded by the objects the file
// holds.
func paintedObjects(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, pg *document.Page) map[int]bool {
	painted, visited := make(map[int]bool), make(map[int]bool)
	dict := mutations.LatestPageDict(alloc, pg)
	stack := []ast.PdfObject{dict["Contents"]}
	if res := dict["Resources"]; res != nil {
		stack = append(stack, res)
	} else {
		stack = append(stack, pg.InheritedResources)
	}
	if ref, isRef := dict["Annots"].(ast.PdfIndirectRef); isRef {
		painted[ref.ObjectNum] = true
	}
	if annots, err := resolvePending(ctx, alloc, doc, dict["Annots"]); err == nil {
		if arr, isArr := annots.(ast.PdfArray); isArr {
			for _, a := range arr {
				if ref, isRef := a.(ast.PdfIndirectRef); isRef {
					painted[ref.ObjectNum] = true
				}
				if d, err := resolvePending(ctx, alloc, doc, a); err == nil {
					if ad, isDict := d.(ast.PdfDict); isDict {
						stack = append(stack, ad["AP"])
					}
				}
			}
		}
	}
	for len(stack) > 0 {
		if ctx.Err() != nil {
			return painted
		}
		obj := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if obj == nil || document.VisitedBefore(obj, visited) {
			continue
		}
		resolved, err := resolvePending(ctx, alloc, doc, obj)
		if err != nil {
			continue
		}
		var dict ast.PdfDict
		switch v := resolved.(type) {
		case ast.PdfArray:
			stack = append(stack, v...)
		case ast.PdfDict:
			dict = v
		case ast.PdfStream:
			dict = v.Dict
		}
		if colourlessFont(dict) {
			continue
		}
		if ref, isRef := obj.(ast.PdfIndirectRef); isRef {
			painted[ref.ObjectNum] = true
		}
		for _, k := range slices.Sorted(maps.Keys(dict)) {
			switch k {
			case "Parent", "P", "Pg", "StructParent", "StructParents", "Metadata", "Popup", "IRT", "A", "AA", "Dest", "Next":
				// Structure and navigation point back up the document.
				continue
			}
			stack = append(stack, dict[k])
		}
	}
	return painted
}

// colourlessFont reports a font dictionary other than Type 3: its glyphs
// paint in the colour the content sets (ISO 32000-1, 9.2), so converting a
// page's colour leaves it as it is.
func colourlessFont(d ast.PdfDict) bool {
	if t, _ := d["Type"].(ast.PdfName); t != "Font" {
		return false
	}
	st, _ := d["Subtype"].(ast.PdfName)
	return st != "Type3"
}

// resolvePending resolves obj as the queued edits on alloc leave it: the
// object queued at its number, else the document's.
func resolvePending(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, obj ast.PdfObject) (ast.PdfObject, error) {
	if ref, isRef := obj.(ast.PdfIndirectRef); isRef {
		if pending, ok := alloc.Pending()[ref.ObjectNum]; ok {
			return pending, nil
		}
	}
	resolved, err := document.ResolveObject(ctx, doc, obj)
	if err != nil {
		return nil, fmt.Errorf("resolving a painted object: %w", err)
	}
	return resolved, nil
}
