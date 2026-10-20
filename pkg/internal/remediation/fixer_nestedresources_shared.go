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

// maxSharedNestedCopies caps the copies [fixSharedNestedResources] makes of
// one form XObject or tiling pattern, one per group of pages binding its names
// differently. Anti-DoS guard, not spec-mandated: each copy rewrites the
// object's bytes into the output, so a document whose every page binds a
// shared form differently would otherwise grow by the form's size per page.
// Real documents share a form across pages that bind it alike (one group, no
// copy), so 64 distinct bindings is far beyond any of them, and past it the
// object is left as it was, which keeps the finding rather than inventing one.
const maxSharedNestedCopies = 64

// nestedSite is one place a page's resources name a shared form XObject or
// tiling pattern: the name under category in the /Resources the page
// resolves its names against.
type nestedSite struct {
	page     annotPageEntry
	pageRes  ast.PdfDict
	resKey   string
	category ast.PdfName
	name     ast.PdfName
}

// nestedGroup gathers the sites whose page resources supply the shared
// object the same missing names, and the /Resources that supplies them.
type nestedGroup struct {
	key    string
	newRes ast.PdfDict
	added  int
	sites  []nestedSite
}

// fixSharedNestedResources repairs the form XObjects and tiling patterns
// several pages, or several names, draw: refCount counts the page-level
// references [fixNestedResources] found, and every object counted more than
// once is handled here.
//
// ISO 19005-2/3/4, 6.2.2 wants each nested content stream to resolve its names
// in its own /Resources. A shared stream sees each page's binding of the names
// it borrows, so the repair groups the pages by the entries their resources
// would add. When every page adds the same, the stream gets them once, which
// is what each page drew before. When they differ, the stream itself is left
// as it is and each group with something to add gets a copy carrying its own
// entries, which its pages are repointed to: the stream may be drawn from
// places outside the page resources (an appearance stream, another form), and
// writing one group's binding into it would change what those draw. A group
// whose pages cannot be written, and an object needing more than
// [maxSharedNestedCopies] copies, is left alone. A form tied into the
// structure tree (/StructParent, /StructParents) is never copied, since two
// copies would claim one parent tree entry (ISO 32000-1, 14.7.4.4).
func fixSharedNestedResources(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, pages []annotPageEntry, refCount map[int]int) (forms, patterns int, err error) {
	sites := make(map[int][]nestedSite)
	seenRes := make(map[string]bool)
	for _, pe := range pages {
		if err := ctx.Err(); err != nil {
			return 0, 0, err
		}
		holder, ok := resourcesHolder(ctx, alloc, doc, pe)
		if !ok {
			continue
		}
		key := resourcesKey(holder)
		if seenRes[key] {
			continue
		}
		seenRes[key] = true
		pageRes := liveDictValue(ctx, alloc, doc, holder.dict["Resources"])
		if pageRes == nil {
			continue
		}
		for _, category := range []ast.PdfName{"XObject", "Pattern"} {
			sub, err := document.ResolveDict(ctx, doc, pageRes[category])
			if err != nil || sub == nil {
				continue
			}
			for _, name := range slices.Sorted(maps.Keys(sub)) {
				ref, isRef := sub[name].(ast.PdfIndirectRef)
				if !isRef || ref.ObjectNum == 0 || refCount[ref.ObjectNum] < 2 {
					continue
				}
				sites[ref.ObjectNum] = append(sites[ref.ObjectNum], nestedSite{
					page: pe, pageRes: pageRes, resKey: key, category: category, name: name,
				})
			}
		}
	}

	for _, objNum := range slices.Sorted(maps.Keys(sites)) {
		if err := ctx.Err(); err != nil {
			return 0, 0, err
		}
		category := sites[objNum][0].category
		n := repairSharedNested(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: objNum}, category, sites[objNum])
		if category == "XObject" {
			forms += n
		} else {
			patterns += n
		}
	}
	return forms, patterns, nil
}

// resourcesKey names the /Resources dictionary a holder carries: its object
// when it is indirect, the holder's own place when it is inline.
func resourcesKey(holder annotPageEntry) string {
	if ref, ok := holder.dict["Resources"].(ast.PdfIndirectRef); ok && ref.ObjectNum > 0 {
		return fmt.Sprintf("r%d", ref.ObjectNum)
	}
	if holder.ref.ObjectNum > 0 {
		return fmt.Sprintf("n%d", holder.ref.ObjectNum)
	}
	if holder.parent != nil {
		return fmt.Sprintf("k%s/%d", resourcesKey(*holder.parent), holder.kid)
	}
	return "root"
}

// repairSharedNested repairs one shared object, and returns how many streams
// it wrote: the object itself, or the copies made of it.
func repairSharedNested(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, ref ast.PdfIndirectRef, category ast.PdfName, sites []nestedSite) int {
	stream, ok := resolveStreamFromAllocOrDoc(ctx, alloc, doc, ref, ref)
	if !ok || stream.Dict == nil {
		return 0
	}
	colourOff := false
	switch category {
	case "XObject":
		if subtype, _ := stream.Dict["Subtype"].(ast.PdfName); subtype != "Form" {
			return 0
		}
	case "Pattern":
		// A shading pattern has no cell (ISO 32000-1, 8.7.4.1), and an
		// uncoloured one ignores its colour operators (8.7.3.3), as
		// [fixTilingPatternResources] reads them.
		if patternType, err := document.ResolveInt(ctx, doc, stream.Dict["PatternType"]); err != nil || patternType != 1 {
			return 0
		}
		paintType, _ := document.ResolveInt(ctx, doc, stream.Dict["PaintType"])
		colourOff = paintType == 2
	default:
		return 0
	}
	// A name the same object appears under in a second category is not a
	// shape this repair knows: a form in /Pattern draws nothing.
	for _, s := range sites {
		if s.category != category {
			return 0
		}
	}
	data, err := doc.DecodeStream(ctx, stream)
	if err != nil || len(data) == 0 {
		return 0
	}
	own := liveDictValue(ctx, alloc, doc, stream.Dict["Resources"])

	var groups []*nestedGroup
	byKey := make(map[string]*nestedGroup)
	for _, s := range sites {
		newRes, added := resourcesForStreamColour(ctx, alloc, doc, data, own, s.pageRes, colourOff)
		key := ""
		if added > 0 {
			key = fmt.Sprintf("%v", newRes)
		}
		g, ok := byKey[key]
		if !ok {
			g = &nestedGroup{key: key, newRes: newRes, added: added}
			byKey[key] = g
			groups = append(groups, g)
		}
		g.sites = append(g.sites, s)
	}

	if len(groups) == 1 {
		g := groups[0]
		if g.added == 0 {
			return 0
		}
		alloc.AllocAt(ref.ObjectNum, ast.NewLazyStream(withResources(stream.Dict, g.newRes), stream.Bytes))
		return 1
	}

	if stream.Dict["StructParent"] != nil || stream.Dict["StructParents"] != nil {
		return 0
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
		if g.added == 0 || !groupWritable(ctx, alloc, doc, g) {
			continue
		}
		clone := alloc.Alloc(ast.NewLazyStream(withResources(stream.Dict, g.newRes), stream.Bytes))
		if repointGroup(ctx, alloc, doc, g, ref, clone) {
			written++
		}
	}
	return written
}

// withResources returns a copy of dict whose /Resources is res.
func withResources(dict, res ast.PdfDict) ast.PdfDict {
	out := make(ast.PdfDict, len(dict)+1)
	maps.Copy(out, dict)
	out["Resources"] = res
	return out
}

// groupWritable reports whether every /Resources a group's sites name can be
// rewritten: an indirect one at its own object number, an inline one through
// the page tree node that holds it.
func groupWritable(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, g *nestedGroup) bool {
	for _, s := range g.sites {
		holder, ok := resourcesHolder(ctx, alloc, doc, s.page)
		if !ok || resourcesKey(holder) != s.resKey {
			return false
		}
		if ref, ok := holder.dict["Resources"].(ast.PdfIndirectRef); ok && ref.ObjectNum > 0 {
			continue
		}
		if !treeNodeWritable(holder) {
			return false
		}
	}
	return true
}

// repointGroup points every name a group's sites give the shared object at
// its copy. An indirect category dictionary (/XObject, /Pattern) may be
// shared with resources of another group, so it is copied too rather than
// written in place. The /Resources dictionary itself is the group's alone,
// since every page resolving through it binds the names alike.
func repointGroup(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, g *nestedGroup, from, to ast.PdfIndirectRef) bool {
	done := make(map[string]bool)
	ok := true
	for _, s := range g.sites {
		if done[s.resKey] {
			continue
		}
		done[s.resKey] = true
		holder, found := resourcesHolder(ctx, alloc, doc, s.page)
		if !found {
			ok = false
			continue
		}
		resObj := holder.dict["Resources"]
		res := liveDictValue(ctx, alloc, doc, resObj)
		sub := liveDictValue(ctx, alloc, doc, res[s.category])
		if res == nil || sub == nil {
			ok = false
			continue
		}
		newSub := maps.Clone(sub)
		for name, entry := range sub {
			if r, isRef := entry.(ast.PdfIndirectRef); isRef && r.ObjectNum == from.ObjectNum {
				newSub[name] = to
			}
		}
		newRes := maps.Clone(res)
		if _, indirect := res[s.category].(ast.PdfIndirectRef); indirect {
			newRes[s.category] = alloc.Alloc(newSub)
		} else {
			newRes[s.category] = newSub
		}
		if ref, isRef := resObj.(ast.PdfIndirectRef); isRef && ref.ObjectNum > 0 {
			ok = alloc.AllocAt(ref.ObjectNum, newRes) && ok
			continue
		}
		work := maps.Clone(holder.dict)
		work["Resources"] = newRes
		ok = writeTreeNode(ctx, alloc, doc, holder, work) && ok
	}
	return ok
}
