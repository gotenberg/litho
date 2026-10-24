package remediation

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// type3Site is one resource dictionary whose /Font names a Type 3 font that
// states no /Resources: a glyph of the font shown from a stream resolving its
// names there takes binding, the resources that stream draws with.
type type3Site struct {
	// key names the resource dictionary: its object when it is indirect, its
	// container when it is inline.
	key string
	// own is the /Resources value as its container holds it.
	own ast.PdfObject
	// binding is what the glyph names resolve against, as the renderer reads
	// it: a form's resources with the page's names it lacks, another
	// stream's resources alone.
	binding ast.PdfDict
	// store writes an inline own dictionary back into its container, nil when
	// own is indirect or the container cannot be written.
	store func(res ast.PdfDict) bool
}

// type3Group gathers the sites that give a font's glyphs the same names.
type type3Group struct {
	newRes ast.PdfDict
	added  int
	sites  []type3Site
}

// type3ScopeWalk collects, for every Type 3 font with no /Resources, the
// resource dictionaries that name it ([type3Site]).
type type3ScopeWalk struct {
	alloc *writer.ObjAllocator
	doc   *document.Document
	sites map[int][]type3Site
	// seen holds the dictionaries walked, by site key and page, so a cycle
	// through /Resources ends and a form drawn by two pages is read once per
	// page, since its binding takes each page's names.
	seen map[string]bool
}

// fixResourcelessType3Fonts gives every Type 3 font with no /Resources the
// names its glyph descriptions resolve where the font is shown, as ISO
// 19005-2/3/4, 6.2.2 requires a content stream to name its resources.
//
// ISO 32000-1, 9.6.5, Table 112 makes a Type 3 font's /Resources optional: a
// glyph description without them takes, in PDF 1.1 practice, the resources of
// the page it is shown on, and litho's renderer, like poppler, draws it against
// the resources of the stream showing it (render/page, renderer.type3Glyph).
// So the binding depends on the showing stream: a page and a form naming the
// same font may each bind the glyph's /Bar to a different form. Every resource
// dictionary naming the font is a site, and the sites are grouped by the
// entries their bindings would add. One group gets the names in the font
// itself, which is what the font draws at every site. Several groups leave the
// font as it is and give each group with something to add a copy of the font
// carrying its own entries, the sites' /Font entries repointed at it, as
// [fixSharedNestedResources] does for forms. A site whose binding differs by
// the page drawing it (a form two pages draw, whose names it lacks each page
// supplies apart) cannot be repointed per page, and leaves the font unrepaired,
// a residual rather than a wrong drawing; so does a group whose sites cannot
// be written, and a font needing more than [maxSharedNestedCopies] copies.
//
// # Corpus evidence
//
//   - litho t3scope_2b.pdf: a page and a form name the same resourceless
//     font, binding /Bar apart. The font was given the page's binding, and
//     the glyphs the form shows drew the page's short bar; each scope now
//     gets its own copy and the output renders as its source.
func fixResourcelessType3Fonts(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, pages []annotPageEntry) (int, error) {
	w := &type3ScopeWalk{alloc: alloc, doc: doc, sites: make(map[int][]type3Site), seen: make(map[string]bool)}
	for _, pe := range pages {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		holder, ok := resourcesHolder(ctx, alloc, doc, pe)
		if !ok {
			continue
		}
		pageRes := liveDictValue(ctx, alloc, doc, holder.dict["Resources"])
		pageKey := resourcesKey(holder)
		store := func(res ast.PdfDict) bool {
			live, found := resourcesHolder(ctx, alloc, doc, pe)
			if !found || resourcesKey(live) != pageKey || !treeNodeWritable(live) {
				return false
			}
			work := maps.Clone(live.dict)
			work["Resources"] = res
			return writeTreeNode(ctx, alloc, doc, live, work)
		}
		w.scope(ctx, pageKey, holder.dict["Resources"], pageRes, pageKey, pageRes, store, 0)
		w.annotations(ctx, pe.dict, pageKey, pageRes)
	}

	fixed := 0
	for _, objNum := range slices.Sorted(maps.Keys(w.sites)) {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		fixed += repairResourcelessType3(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: objNum}, w.sites[objNum])
	}
	return fixed, nil
}

// siteKey names a /Resources value: its object when it is indirect, else
// inline, the container's place.
func siteKey(own ast.PdfObject, inline string) string {
	if ref, ok := own.(ast.PdfIndirectRef); ok && ref.ObjectNum > 0 {
		return fmt.Sprintf("r%d", ref.ObjectNum)
	}
	return inline
}

// scope records the resourceless Type 3 fonts one resource dictionary names,
// then walks the forms, tiling patterns and Type 3 fonts with resources of
// their own that it names, each a scope of its own. The nesting is bounded by
// the document's form-nesting budget, as the renderer bounds what it draws.
func (w *type3ScopeWalk) scope(ctx context.Context, key string, own ast.PdfObject, binding ast.PdfDict, pageKey string, pageRes ast.PdfDict, store func(ast.PdfDict) bool, depth int) {
	if ctx.Err() != nil || depth > w.doc.Limits().MaxFormDepth || w.seen[key+"|"+pageKey] {
		return
	}
	w.seen[key+"|"+pageKey] = true
	res := liveDictValue(ctx, w.alloc, w.doc, own)
	if res == nil {
		return
	}
	if _, indirect := own.(ast.PdfIndirectRef); indirect {
		store = nil
	}

	fonts := liveDictValue(ctx, w.alloc, w.doc, res["Font"])
	for _, name := range slices.Sorted(maps.Keys(fonts)) {
		ref, isRef := fonts[name].(ast.PdfIndirectRef)
		if !isRef || ref.ObjectNum <= 0 {
			continue
		}
		fd := resolveFromAllocOrDoc(ctx, w.alloc, w.doc, ref)
		if subtype, _ := fd["Subtype"].(ast.PdfName); subtype != "Type3" {
			continue
		}
		fontRes := fd["Resources"]
		if liveDictValue(ctx, w.alloc, w.doc, fontRes) == nil {
			w.sites[ref.ObjectNum] = append(w.sites[ref.ObjectNum], type3Site{key: key, own: own, binding: binding, store: store})
			continue
		}
		inline := fmt.Sprintf("f%d", ref.ObjectNum)
		w.scope(ctx, siteKey(fontRes, inline), fontRes, liveDictValue(ctx, w.alloc, w.doc, fontRes), pageKey, pageRes,
			w.dictStore(ctx, ref), depth+1)
	}

	xobjects := liveDictValue(ctx, w.alloc, w.doc, res["XObject"])
	for _, name := range slices.Sorted(maps.Keys(xobjects)) {
		w.stream(ctx, xobjects[name], isFormXObject, true, pageKey, pageRes, depth)
	}
	patterns := liveDictValue(ctx, w.alloc, w.doc, res["Pattern"])
	for _, name := range slices.Sorted(maps.Keys(patterns)) {
		w.stream(ctx, patterns[name], isTilingPattern, false, pageKey, pageRes, depth)
	}
}

// stream walks the resources of one form XObject, tiling pattern or
// appearance stream, when it states some: a stream stating none draws with
// the resources of the stream drawing it, already a scope. A form draws with
// its own names and the page's it lacks (render/page, withPageNames); a
// pattern cell and an appearance with their own alone.
func (w *type3ScopeWalk) stream(ctx context.Context, entry ast.PdfObject, keep func(context.Context, *document.Document, ast.PdfDict) bool, mergePage bool, pageKey string, pageRes ast.PdfDict, depth int) {
	ref, isRef := entry.(ast.PdfIndirectRef)
	if !isRef || ref.ObjectNum <= 0 {
		return
	}
	s, ok := liveStream(ctx, w.alloc, w.doc, ref)
	if !ok || (keep != nil && !keep(ctx, w.doc, s.Dict)) {
		return
	}
	own := s.Dict["Resources"]
	res := liveDictValue(ctx, w.alloc, w.doc, own)
	if res == nil {
		return
	}
	binding := res
	if mergePage {
		binding = withPageNames(ctx, w.doc, res, pageRes)
	}
	store := func(newRes ast.PdfDict) bool {
		live, found := liveStream(ctx, w.alloc, w.doc, ref)
		if !found {
			return false
		}
		return w.alloc.AllocAt(ref.ObjectNum, ast.NewLazyStream(withResources(live.Dict, newRes), live.Bytes))
	}
	w.scope(ctx, siteKey(own, fmt.Sprintf("s%d", ref.ObjectNum)), own, binding, pageKey, pageRes, store, depth+1)
}

// dictStore returns the store of an inline /Resources inside the dictionary
// at ref, a Type 3 font.
func (w *type3ScopeWalk) dictStore(ctx context.Context, ref ast.PdfIndirectRef) func(ast.PdfDict) bool {
	return func(res ast.PdfDict) bool {
		live := resolveFromAllocOrDoc(ctx, w.alloc, w.doc, ref)
		if live == nil {
			return false
		}
		return w.alloc.AllocAt(ref.ObjectNum, withResources(live, res))
	}
}

// annotations walks the appearance streams of a page's annotations, under
// /N, /R and /D, each a stream or a subdictionary of appearance states (ISO
// 32000-1, 12.5.5).
func (w *type3ScopeWalk) annotations(ctx context.Context, pageDict ast.PdfDict, pageKey string, pageRes ast.PdfDict) {
	for _, annotObj := range liveArray(ctx, w.alloc, w.doc, pageDict["Annots"]) {
		if ctx.Err() != nil {
			return
		}
		annot := liveDictValue(ctx, w.alloc, w.doc, annotObj)
		ap := liveDictValue(ctx, w.alloc, w.doc, annot["AP"])
		for _, key := range []ast.PdfName{"N", "R", "D"} {
			entry, ok := ap[key]
			if !ok {
				continue
			}
			if ref, isRef := entry.(ast.PdfIndirectRef); isRef {
				if _, isStream := liveStream(ctx, w.alloc, w.doc, ref); isStream {
					w.stream(ctx, entry, nil, false, pageKey, pageRes, 0)
					continue
				}
			}
			states := liveDictValue(ctx, w.alloc, w.doc, entry)
			for _, state := range slices.Sorted(maps.Keys(states)) {
				w.stream(ctx, states[state], nil, false, pageKey, pageRes, 0)
			}
		}
	}
}

// withPageNames returns own with the page's names it lacks, per category, as
// the renderer resolves a form's names (render/page, withPageNames): own wins,
// and the page's default colour spaces stay the page's (ISO 32000-1, 8.6.5.6).
func withPageNames(ctx context.Context, doc *document.Document, own, pageRes ast.PdfDict) ast.PdfDict {
	if pageRes == nil {
		return own
	}
	merged := maps.Clone(own)
	for _, category := range slices.Sorted(maps.Keys(pageRes)) {
		inherited, err := document.ResolveDict(ctx, doc, pageRes[category])
		if err != nil || len(inherited) == 0 {
			continue
		}
		mine, err := document.ResolveDict(ctx, doc, own[category])
		if err != nil {
			continue
		}
		entries := maps.Clone(inherited)
		if category == "ColorSpace" {
			for _, name := range []ast.PdfName{"DefaultGray", "DefaultRGB", "DefaultCMYK"} {
				delete(entries, name)
			}
		}
		maps.Copy(entries, mine)
		merged[category] = entries
	}
	return merged
}

// repairResourcelessType3 repairs one Type 3 font with no /Resources against
// its sites, and returns how many font dictionaries it wrote: the font itself,
// or the copies made of it.
func repairResourcelessType3(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, ref ast.PdfIndirectRef, sites []type3Site) int {
	fd := resolveFromAllocOrDoc(ctx, alloc, doc, ref)
	charProcs, err := document.ResolveDict(ctx, doc, fd["CharProcs"])
	if err != nil || charProcs == nil {
		return 0
	}
	var glyphs [][]byte
	for _, name := range slices.Sorted(maps.Keys(charProcs)) {
		stream, err := document.ResolveStream(ctx, doc, charProcs[name])
		if err != nil {
			continue
		}
		data, err := doc.DecodeStream(ctx, stream)
		if err != nil || len(data) == 0 {
			continue
		}
		glyphs = append(glyphs, data)
	}

	var groups []*type3Group
	byKey := make(map[string]*type3Group)
	siteGroup := make(map[string]string)
	for _, s := range sites {
		var newRes ast.PdfDict
		added := 0
		for _, data := range glyphs {
			res, n := resourcesForStream(ctx, alloc, doc, data, newRes, s.binding)
			if n > 0 {
				newRes = res
				added += n
			}
		}
		key := ""
		if added > 0 {
			key = fmt.Sprintf("%v", newRes)
		}
		// One dictionary bound apart by the pages drawing it would need a copy
		// per page of what names the font, which this repair does not make.
		if prev, seen := siteGroup[s.key]; seen && prev != key {
			doc.Logger.Debug("remediation.nestedresources: a Type 3 font's site binds its names per page", "obj", ref.ObjectNum)
			return 0
		}
		siteGroup[s.key] = key
		g, ok := byKey[key]
		if !ok {
			g = &type3Group{newRes: newRes, added: added}
			byKey[key] = g
			groups = append(groups, g)
		}
		g.sites = append(g.sites, s)
	}

	if len(groups) == 1 {
		g := groups[0]
		if g.added == 0 || !alloc.AllocAt(ref.ObjectNum, withResources(fd, g.newRes)) {
			return 0
		}
		return 1
	}

	copies := 0
	for _, g := range groups {
		if g.added > 0 {
			copies++
		}
	}
	if copies > maxSharedNestedCopies {
		return 0
	}
	written := 0
	for _, g := range groups {
		if g.added == 0 || !type3GroupWritable(g) {
			continue
		}
		clone := alloc.Alloc(withResources(fd, g.newRes))
		if repointType3Group(ctx, alloc, doc, g, ref, clone) {
			written++
		}
	}
	return written
}

// type3GroupWritable reports whether every site of a group can be rewritten:
// an indirect /Resources at its own object number, an inline one through its
// container.
func type3GroupWritable(g *type3Group) bool {
	for _, s := range g.sites {
		if ref, ok := s.own.(ast.PdfIndirectRef); ok && ref.ObjectNum > 0 {
			continue
		}
		if s.store == nil {
			return false
		}
	}
	return true
}

// repointType3Group points every /Font entry a group's sites give the font at
// its copy. An indirect /Font dictionary may be shared with a site of another
// group, so it is copied rather than written in place.
func repointType3Group(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, g *type3Group, from, to ast.PdfIndirectRef) bool {
	done := make(map[string]bool)
	ok := true
	for _, s := range g.sites {
		if done[s.key] {
			continue
		}
		done[s.key] = true
		res := liveDictValue(ctx, alloc, doc, s.own)
		fonts := liveDictValue(ctx, alloc, doc, res["Font"])
		if res == nil || fonts == nil {
			ok = false
			continue
		}
		newFonts := maps.Clone(fonts)
		for name, entry := range fonts {
			if r, isRef := entry.(ast.PdfIndirectRef); isRef && r.ObjectNum == from.ObjectNum {
				newFonts[name] = to
			}
		}
		newRes := maps.Clone(res)
		if _, indirect := res["Font"].(ast.PdfIndirectRef); indirect {
			newRes["Font"] = alloc.Alloc(newFonts)
		} else {
			newRes["Font"] = newFonts
		}
		if r, isRef := s.own.(ast.PdfIndirectRef); isRef && r.ObjectNum > 0 {
			ok = alloc.AllocAt(r.ObjectNum, newRes) && ok
			continue
		}
		ok = s.store(newRes) && ok
	}
	return ok
}
