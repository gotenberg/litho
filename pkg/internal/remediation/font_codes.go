package remediation

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// fontUses is what [collectFontUses] reads from the content the pages of a
// document draw.
type fontUses struct {
	// codes holds, by font object number, every one-byte code a text operator
	// shows with that font, in any stream that selects it.
	codes map[int]map[byte]bool
	// composite holds, by font object number, the distinct strings a text
	// operator shows with each Type0 font, whose codes may be longer than one
	// byte and so do not fit codes.
	composite map[int][]ast.PdfString
	// drawn holds, at each page's index, the fonts the streams that page draws
	// select, in the order the walk first reaches them: the page's content, the
	// forms it draws, the tiling patterns it paints with and the glyph
	// procedures of the Type 3 fonts it shows, then its annotations'
	// appearances. Each font object is listed once, at the first page that
	// reaches it, and only a font with an object number of its own is listed.
	drawn [][]ast.PdfObject
	// inline holds the fonts written inline in a resource dictionary, which
	// codes and composite, keyed by object number, cannot hold, in the order
	// the walk first reaches them. A dictionary every stream resolving it
	// reaches through the same object is listed once.
	inline []*inlineFontUse
}

// inlineFontUse is what a font dictionary written inline in a resource
// dictionary shows, and where it sits.
type inlineFontUse struct {
	site inlineFontSite
	// font is the dictionary as the walk first read it, which stands for a
	// site no object number reaches.
	font ast.PdfDict
	// codes and composite mean what the fields of [fontUses] keyed by object
	// number mean.
	codes     map[byte]bool
	composite []ast.PdfString
	shownAt   map[ast.PdfString]bool
}

// inlineFontSite places a font dictionary written inline: the entry name of
// the /Font dictionary of the /Resources that owner holds.
type inlineFontSite struct {
	owner resourcesOwner
	name  ast.PdfName
}

// resourcesOwner names the object holding a /Resources dictionary a stream
// resolves its names against: a node of the page tree, which a page's content
// resolves through ([resourcesHolder]), or the stream or dictionary at ref, a
// form XObject, a tiling pattern or an appearance stream, or a Type 3 font.
// The zero value names none: the resources of a Type 3 font written inline,
// which no object number reaches.
type resourcesOwner struct {
	node   *annotPageEntry
	ref    ast.PdfIndirectRef
	stream bool
}

// dict returns the owner's dictionary, read through the allocator, nil when
// it names none or no longer resolves to one.
func (o resourcesOwner) dict(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) ast.PdfDict {
	switch {
	case o.node != nil:
		return liveTreeNode(ctx, alloc, doc, *o.node)
	case o.ref.ObjectNum <= 0:
		return nil
	case o.stream:
		s, ok := liveStream(ctx, alloc, doc, o.ref)
		if !ok {
			return nil
		}
		return s.Dict
	}
	return resolveFromAllocOrDoc(ctx, alloc, doc, o.ref)
}

// write queues dict as the owner's dictionary, a stream keeping its bytes,
// and reports whether the write landed.
func (o resourcesOwner) write(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, dict ast.PdfDict) bool {
	switch {
	case o.node != nil:
		return writeTreeNode(ctx, alloc, doc, *o.node, dict)
	case o.ref.ObjectNum <= 0:
		return false
	case o.stream:
		s, ok := liveStream(ctx, alloc, doc, o.ref)
		if !ok {
			return false
		}
		// The dictionary changes, the bytes do not, so a filtered stream is
		// carried across raw.
		return alloc.AllocAt(o.ref.ObjectNum, ast.NewLazyStream(dict, s.Bytes))
	}
	return alloc.AllocAt(o.ref.ObjectNum, dict)
}

// key names the owner among the others the walk reaches.
func (o resourcesOwner) key() string {
	if o.node == nil {
		return fmt.Sprintf("O%d", o.ref.ObjectNum)
	}
	return treeNodeKey(*o.node)
}

// treeNodeKey names a page tree node: its object number, or its place in the
// /Kids of the node listing it when it has none.
func treeNodeKey(n annotPageEntry) string {
	if n.ref.ObjectNum > 0 || n.parent == nil {
		return fmt.Sprintf("N%d", n.ref.ObjectNum)
	}
	return fmt.Sprintf("%s/%d", treeNodeKey(*n.parent), n.kid)
}

// siteChain is the chain of dictionaries above a font dictionary written
// inline, read through the allocator: the owner's dictionary, the /Resources
// it holds and the /Font dictionary that holds, and key, naming the first
// link of the chain with an object number of its own, the /Font dictionary,
// the /Resources or the owner, and the font's name in it. Every stream
// resolving the font reaches it through that link.
type siteChain struct {
	owner, res, fonts ast.PdfDict
	key               string
}

// chain returns the site's [siteChain], zero when the owner or its
// /Resources no longer resolve.
func (s inlineFontSite) chain(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) siteChain {
	var c siteChain
	if c.owner = s.owner.dict(ctx, alloc, doc); c.owner == nil {
		return siteChain{}
	}
	if c.res = liveDictValue(ctx, alloc, doc, c.owner["Resources"]); c.res == nil {
		return siteChain{}
	}
	c.fonts = liveDictValue(ctx, alloc, doc, c.res["Font"])
	fontsRef, fontsIndirect := c.res["Font"].(ast.PdfIndirectRef)
	resRef, resIndirect := c.owner["Resources"].(ast.PdfIndirectRef)
	switch {
	case fontsIndirect && fontsRef.ObjectNum > 0:
		c.key = fmt.Sprintf("F%d", fontsRef.ObjectNum)
	case resIndirect && resRef.ObjectNum > 0:
		c.key = fmt.Sprintf("R%d", resRef.ObjectNum)
	default:
		c.key = s.owner.key()
	}
	c.key += "/" + string(s.name)
	return c
}

// font returns the font dictionary written inline at the site, read through
// the allocator, nil when the site no longer holds one there.
func (s inlineFontSite) font(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) ast.PdfDict {
	d, _ := s.chain(ctx, alloc, doc).fonts[s.name].(ast.PdfDict)
	return d
}

// writable reports whether [inlineFontSite.replace] has a place to write
// the site: the /Font dictionary, the /Resources or the owner, whichever
// first has an object number, or a page tree node written directly in /Kids.
func (s inlineFontSite) writable(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) bool {
	c := s.chain(ctx, alloc, doc)
	if _, ok := c.fonts[s.name].(ast.PdfDict); !ok {
		return false
	}
	if !strings.HasPrefix(c.key, "N") && !strings.HasPrefix(c.key, "O") {
		// The /Font dictionary or the /Resources has an object number.
		return true
	}
	if s.owner.node != nil {
		return treeNodeWritable(*s.owner.node)
	}
	return s.owner.ref.ObjectNum > 0
}

// replace writes ref in place of the font dictionary written inline at the
// site, at the first link of the chain above it with an object number of its
// own ([writeResourceCategory]), and reports whether the write landed.
func (s inlineFontSite) replace(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, ref ast.PdfIndirectRef) bool {
	c := s.chain(ctx, alloc, doc)
	if _, ok := c.fonts[s.name].(ast.PdfDict); !ok {
		return false
	}
	rebuilt := maps.Clone(c.fonts)
	rebuilt[s.name] = ref
	work := maps.Clone(c.owner)
	written, ownerDirty := writeResourceCategory(alloc, work, c.res, "Font", rebuilt)
	if written {
		return true
	}
	return ownerDirty && s.owner.write(ctx, alloc, doc, work)
}

// collectFontUses walks the content streams each of pages draws, entries being
// their page tree entries ([pageTreeEntries]), and returns the codes each font
// shows and the fonts each page reaches.
//
// A generated /ToUnicode is attached only when it maps every code the font
// shows, the set ISO 19005-2, 6.2.11.7.2 and ISO 14289-1, 7.21.7 hold it to,
// and those rules judge a font wherever a stream selects it: veraPDF reaches
// the glyphs of a form XObject (ISO 32000-1, 8.10), a tiling pattern cell
// (8.7.3.1), a Type 3 glyph procedure (9.6.5) and an annotation appearance
// (12.5.5) as it reaches a page's. The repair used to read the codes of the
// first page whose /Resources named a font and mark the font done there, so a
// page naming a font without showing it left every later page without a
// mapping, and a font only such a stream names was never reached.
//
// The walk follows what compliance judges ([compliance.ShownCharCodes] for the
// codes): a form a Do draws, a tiling pattern an scn or SCN selects, coloured
// or uncoloured (an uncoloured cell ignores its colour operators, not its
// text, ISO 32000-1, 8.7.3.3), every glyph procedure of a Type 3 font a stream
// shows text with, and every appearance stream of every annotation, under /N,
// /R and /D. It also follows the patterns selected inside an uncoloured cell,
// which compliance does not judge: a font reached only that way gains codes
// that a generated map must also cover, so the gate stays the stricter one.
// A stream that carries no /Resources of its own, or does not decode or parse,
// is left out as compliance leaves it. Every object is read through the allocator, so a
// stream an earlier fixer rewrote is read as it now stands. Each stream is
// walked once, and the nesting below a page stops at the document's
// form-nesting budget. A font dictionary written inline, which ISO 32000-1,
// 7.3.7 permits and no object number names, is recorded with the object
// holding the /Resources that names it ([inlineFontSite]), so a fixer can
// write it back there.
//
// # Corpus evidence
//
//   - All corpora: over 1480 remediations, the PDF/A-2u and PDF/UA-1 ones of
//     the files whose forms, patterns, Type 3 glyphs or appearances name a
//     font among them, 98 outputs lose a rule class on litho and none gains
//     one. 92 lose 6.2.11.7.2 t1 or 7.21.7 t1, and 6 a width or /CIDSet rule
//     of a font no page's /Resources name, which the repair now reaches.
//   - All corpora: veraPDF, run on 151 of the changed outputs, reports 35
//     fewer rule classes and none new, and the outputs litho and veraPDF
//     disagree on are the ones they disagreed on before.
//   - pdfjs js-buttons.pdf, resetform.pdf, govdocs thread_200_200931.pdf: the
//     fonts only form field appearances show, ZapfDingbats among them, gain a
//     /ToUnicode, and 6.2.11.7.2 t1 at PDF/A-2u clears on both tools.
//   - litho laterreads_2u.pdf: a font named by two pages and shown on the
//     second, one only a form names and one only an appearance names kept
//     6.2.11.7.2 t1 at PDF/A-2a and 2u, 7.21.7 t1 at PDF/UA-1 and 8.4.5.8 t1
//     at PDF/UA-2, six times each, on litho and veraPDF.
func collectFontUses(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, pages []*document.Page, entries []annotPageEntry) fontUses {
	w := &fontCodeWalk{
		alloc:    alloc,
		doc:      doc,
		uses:     fontUses{codes: make(map[int]map[byte]bool), composite: make(map[int][]ast.PdfString), drawn: make([][]ast.PdfObject, len(pages))},
		visited:  make(map[int]bool),
		listed:   make(map[int]bool),
		shownAt:  make(map[int]map[ast.PdfString]bool),
		subtypes: make(map[int]string),
		inlineAt: make(map[string]*inlineFontUse),
	}
	for i, page := range pages {
		if ctx.Err() != nil {
			break
		}
		w.page = i
		pe := pageTreeEntry(ctx, alloc, doc, entries[i])
		if data, _, err := livePageContentData(ctx, alloc, doc, page); err == nil && len(data) > 0 {
			if holder, ok := resourcesHolder(ctx, alloc, doc, pe); ok {
				w.parsed(ctx, data, liveDictValue(ctx, alloc, doc, holder.dict["Resources"]), resourcesOwner{node: &holder}, 0)
			}
		}
		w.annotations(ctx, pe.dict)
	}
	return w.uses
}

// fontCodeWalk carries one [collectFontUses] run. page is the index of the page
// being walked, visited the object numbers of the streams already walked,
// listed the font objects already listed in drawn, and inlineAt the entries of
// uses.inline by the key of their [siteChain].
type fontCodeWalk struct {
	alloc   *writer.ObjAllocator
	doc     *document.Document
	uses    fontUses
	page    int
	visited map[int]bool
	listed  map[int]bool
	// shownAt holds the strings already recorded in uses.composite, by font
	// object number, and subtypes each font's /Subtype once read.
	shownAt  map[int]map[ast.PdfString]bool
	subtypes map[int]string
	inlineAt map[string]*inlineFontUse
}

// parsed walks the content stream whose decoded bytes are data, resolving its
// names against res, the /Resources owner holds, at depth streams below the
// page.
func (w *fontCodeWalk) parsed(ctx context.Context, data []byte, res ast.PdfDict, owner resourcesOwner, depth int) {
	ops, err := content.ParseStreamData(ctx, data)
	if err != nil {
		w.doc.Logger.Debug("remediation: fonts: cannot parse a content stream to read its codes", "error", err)
		return
	}
	w.stream(ctx, ops, res, owner, depth)
}

// stream records the codes ops shows with each font res names, then walks the
// streams ops draws.
func (w *fontCodeWalk) stream(ctx context.Context, ops []content.Operation, res ast.PdfDict, owner resourcesOwner, depth int) {
	if ctx.Err() != nil || res == nil {
		return
	}
	fonts := liveDictValue(ctx, w.alloc, w.doc, res["Font"])
	shown := compliance.ShownCharCodes(ops)
	strs := compliance.ShownStrings(ops)
	for _, name := range slices.Sorted(maps.Keys(shown)) {
		entry, ok := fonts[ast.PdfName(name)]
		if !ok {
			continue
		}
		if font, inline := entry.(ast.PdfDict); inline {
			w.recordInline(ctx, inlineFontSite{owner: owner, name: ast.PdfName(name)}, font, shown[name], strs[name])
			continue
		}
		w.record(ctx, entry, shown[name], strs[name])
	}
	if depth >= w.doc.Limits().MaxFormDepth {
		return
	}

	xobjects := liveDictValue(ctx, w.alloc, w.doc, res["XObject"])
	patterns := liveDictValue(ctx, w.alloc, w.doc, res["Pattern"])
	for _, op := range ops {
		switch op.Operator {
		case "Do":
			if name, ok := lastName(op); ok {
				w.child(ctx, xobjects[name], isFormXObject, depth)
			}
		case "scn", "SCN":
			if name, ok := lastName(op); ok {
				w.child(ctx, patterns[name], isTilingPattern, depth)
			}
		}
	}
	for _, name := range slices.Sorted(maps.Keys(shown)) {
		w.type3Glyphs(ctx, fonts[ast.PdfName(name)], depth)
	}
}

// record adds codes to what the font entry names shows, strs too when it is a
// Type0 font, and lists it for the page being walked when no page listed it
// before.
func (w *fontCodeWalk) record(ctx context.Context, entry ast.PdfObject, codes map[byte]bool, strs []ast.PdfString) {
	ref, ok := entry.(ast.PdfIndirectRef)
	if !ok || ref.ObjectNum <= 0 {
		return
	}
	set := w.uses.codes[ref.ObjectNum]
	if set == nil {
		set = make(map[byte]bool, len(codes))
		w.uses.codes[ref.ObjectNum] = set
	}
	maps.Copy(set, codes)
	if len(strs) > 0 && w.subtype(ctx, ref) == "Type0" {
		seen := w.shownAt[ref.ObjectNum]
		if seen == nil {
			seen = make(map[ast.PdfString]bool)
			w.shownAt[ref.ObjectNum] = seen
		}
		for _, s := range strs {
			if !seen[s] {
				seen[s] = true
				w.uses.composite[ref.ObjectNum] = append(w.uses.composite[ref.ObjectNum], s)
			}
		}
	}
	if !w.listed[ref.ObjectNum] {
		w.listed[ref.ObjectNum] = true
		w.uses.drawn[w.page] = append(w.uses.drawn[w.page], entry)
	}
}

// recordInline adds codes, and strs for a Type0 font, to what the font
// dictionary written inline at site shows.
func (w *fontCodeWalk) recordInline(ctx context.Context, site inlineFontSite, font ast.PdfDict, codes map[byte]bool, strs []ast.PdfString) {
	key := site.chain(ctx, w.alloc, w.doc).key
	if key == "" {
		// The owner no longer resolves to the dictionary the walk read, so the
		// site names no place, and the use is listed under the walk's own.
		key = fmt.Sprintf("?%d", len(w.uses.inline))
	}
	use := w.inlineAt[key]
	if use == nil {
		use = &inlineFontUse{site: site, font: font, codes: make(map[byte]bool, len(codes)), shownAt: make(map[ast.PdfString]bool)}
		w.inlineAt[key] = use
		w.uses.inline = append(w.uses.inline, use)
	}
	maps.Copy(use.codes, codes)
	if subtype, _ := document.ResolveName(ctx, w.doc, font["Subtype"]); subtype != "Type0" {
		return
	}
	for _, s := range strs {
		if !use.shownAt[s] {
			use.shownAt[s] = true
			use.composite = append(use.composite, s)
		}
	}
}

// subtype returns the /Subtype of the font ref names, read through the
// allocator once.
func (w *fontCodeWalk) subtype(ctx context.Context, ref ast.PdfIndirectRef) string {
	if st, ok := w.subtypes[ref.ObjectNum]; ok {
		return st
	}
	// A /Subtype that is no name names no Type0 font, which is all this
	// answers.
	st, _ := document.ResolveName(ctx, w.doc, resolveFromAllocOrDoc(ctx, w.alloc, w.doc, ref)["Subtype"])
	w.subtypes[ref.ObjectNum] = string(st)
	return string(st)
}

// child walks the stream entry names, a form XObject or a tiling pattern
// cell, when keep accepts its dictionary, against its own /Resources.
func (w *fontCodeWalk) child(ctx context.Context, entry ast.PdfObject, keep func(context.Context, *document.Document, ast.PdfDict) bool, depth int) {
	stream, ok := w.firstStream(ctx, entry)
	if !ok || !keep(ctx, w.doc, stream.Dict) {
		return
	}
	w.walkStream(ctx, entry, stream, depth+1)
}

// type3Glyphs walks every glyph procedure of the Type 3 font entry names
// against the font's /Resources. veraPDF reads a Type 3 font's whole
// /CharProcs once a stream shows text with it, the procedures of glyphs no
// string selects included, as compliance's walk does.
func (w *fontCodeWalk) type3Glyphs(ctx context.Context, entry ast.PdfObject, depth int) {
	font := liveDictValue(ctx, w.alloc, w.doc, entry)
	if subtype, _ := document.ResolveName(ctx, w.doc, font["Subtype"]); subtype != "Type3" {
		return
	}
	res := liveDictValue(ctx, w.alloc, w.doc, font["Resources"])
	if res == nil {
		return
	}
	var owner resourcesOwner
	if ref, ok := entry.(ast.PdfIndirectRef); ok {
		owner.ref = ref
	}
	procs := liveDictValue(ctx, w.alloc, w.doc, font["CharProcs"])
	for _, glyph := range slices.Sorted(maps.Keys(procs)) {
		stream, ok := w.firstStream(ctx, procs[glyph])
		if !ok {
			continue
		}
		data, err := w.doc.DecodeStream(ctx, stream)
		if err != nil {
			continue
		}
		w.parsed(ctx, data, res, owner, depth+1)
	}
}

// annotations walks every appearance stream of the annotations the page
// dictionary lists, as a form drawn one level below the page.
func (w *fontCodeWalk) annotations(ctx context.Context, pageDict ast.PdfDict) {
	for _, annotObj := range liveArray(ctx, w.alloc, w.doc, pageDict["Annots"]) {
		if ctx.Err() != nil {
			return
		}
		ap := liveDictValue(ctx, w.alloc, w.doc, liveAnnotation(ctx, w.alloc, w.doc, annotObj)["AP"])
		for _, key := range []ast.PdfName{"N", "R", "D"} {
			entry, ok := ap[key]
			if !ok {
				continue
			}
			for _, streamObj := range appearanceStreams(ctx, w.alloc, w.doc, entry) {
				if stream, ok := w.firstStream(ctx, streamObj); ok {
					w.walkStream(ctx, streamObj, stream, 1)
				}
			}
		}
	}
}

// walkStream walks a form-like stream, a form XObject, a pattern cell or an
// appearance, the one entry names, at depth against its own /Resources, and
// leaves one that states none, as compliance does.
func (w *fontCodeWalk) walkStream(ctx context.Context, entry ast.PdfObject, stream ast.PdfStream, depth int) {
	res := liveDictValue(ctx, w.alloc, w.doc, stream.Dict["Resources"])
	if res == nil {
		return
	}
	data, err := w.doc.DecodeStream(ctx, stream)
	if err != nil {
		w.doc.Logger.Debug("remediation: fonts: cannot decode a content stream to read its codes", "error", err)
		return
	}
	ref, _ := entry.(ast.PdfIndirectRef)
	w.parsed(ctx, data, res, resourcesOwner{ref: ref, stream: true}, depth)
}

// firstStream returns the stream entry names, read through the allocator, and
// marks it walked. It reports false for a direct value, which names no stream
// (ISO 32000-1, 7.3.8.1), and for a stream walked already.
func (w *fontCodeWalk) firstStream(ctx context.Context, entry ast.PdfObject) (ast.PdfStream, bool) {
	ref, ok := entry.(ast.PdfIndirectRef)
	if !ok || ref.ObjectNum <= 0 || w.visited[ref.ObjectNum] {
		return ast.PdfStream{}, false
	}
	stream, ok := liveStream(ctx, w.alloc, w.doc, ref)
	if !ok {
		return ast.PdfStream{}, false
	}
	w.visited[ref.ObjectNum] = true
	return stream, true
}

// lastName returns the name operand an operator ends with: the XObject Do
// draws, or the pattern scn and SCN select.
func lastName(op content.Operation) (ast.PdfName, bool) {
	if len(op.Operands) == 0 {
		return "", false
	}
	name, ok := op.Operands[len(op.Operands)-1].(ast.PdfName)
	return name, ok
}
