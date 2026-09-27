package remediation

import (
	"context"
	"maps"
	"slices"

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
	// drawn holds, at each page's index, the fonts the streams that page draws
	// select, in the order the walk first reaches them: the page's content, the
	// forms it draws, the tiling patterns it paints with and the glyph
	// procedures of the Type 3 fonts it shows, then its annotations'
	// appearances. Each font object is listed once, at the first page that
	// reaches it, and only a font with an object number of its own is listed.
	drawn [][]ast.PdfObject
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
// codes): a form a Do draws, a coloured tiling pattern an scn or SCN selects,
// every glyph procedure of a Type 3 font a stream shows text with, and every
// appearance stream of every annotation, under /N, /R and /D. A stream that
// carries no /Resources of its own, or does not decode or parse, is left out
// as compliance leaves it. Every object is read through the allocator, so a
// stream an earlier fixer rewrote is read as it now stands. Each stream is
// walked once, and the nesting below a page stops at the document's
// form-nesting budget.
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
		alloc:   alloc,
		doc:     doc,
		uses:    fontUses{codes: make(map[int]map[byte]bool), drawn: make([][]ast.PdfObject, len(pages))},
		visited: make(map[int]bool),
		listed:  make(map[int]bool),
	}
	for i, page := range pages {
		if ctx.Err() != nil {
			break
		}
		w.page = i
		pe := pageTreeEntry(ctx, alloc, doc, entries[i])
		if data, _, err := livePageContentData(ctx, alloc, doc, page); err == nil && len(data) > 0 {
			w.parsed(ctx, data, pageResourcesFor(ctx, alloc, doc, pe), 0)
		}
		w.annotations(ctx, pe.dict)
	}
	return w.uses
}

// fontCodeWalk carries one [collectFontUses] run. page is the index of the page
// being walked, visited the object numbers of the streams already walked, and
// listed the font objects already listed in drawn.
type fontCodeWalk struct {
	alloc   *writer.ObjAllocator
	doc     *document.Document
	uses    fontUses
	page    int
	visited map[int]bool
	listed  map[int]bool
}

// parsed walks the content stream whose decoded bytes are data, resolving its
// names against res, at depth streams below the page.
func (w *fontCodeWalk) parsed(ctx context.Context, data []byte, res ast.PdfDict, depth int) {
	ops, err := content.ParseStreamData(ctx, data)
	if err != nil {
		w.doc.Logger.Debug("remediation: fonts: cannot parse a content stream to read its codes", "error", err)
		return
	}
	w.stream(ctx, ops, res, depth)
}

// stream records the codes ops shows with each font res names, then walks the
// streams ops draws.
func (w *fontCodeWalk) stream(ctx context.Context, ops []content.Operation, res ast.PdfDict, depth int) {
	if ctx.Err() != nil || res == nil {
		return
	}
	fonts := liveDictValue(ctx, w.alloc, w.doc, res["Font"])
	shown := compliance.ShownCharCodes(ops)
	for _, name := range slices.Sorted(maps.Keys(shown)) {
		entry, ok := fonts[ast.PdfName(name)]
		if !ok {
			continue
		}
		w.record(entry, shown[name])
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
				w.child(ctx, patterns[name], isColouredTilingPattern, depth)
			}
		}
	}
	for _, name := range slices.Sorted(maps.Keys(shown)) {
		w.type3Glyphs(ctx, fonts[ast.PdfName(name)], depth)
	}
}

// record adds codes to what the font entry names shows, and lists it for the
// page being walked when no page listed it before.
func (w *fontCodeWalk) record(entry ast.PdfObject, codes map[byte]bool) {
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
	if !w.listed[ref.ObjectNum] {
		w.listed[ref.ObjectNum] = true
		w.uses.drawn[w.page] = append(w.uses.drawn[w.page], entry)
	}
}

// child walks the stream entry names, a form XObject or a tiling pattern
// cell, when keep accepts its dictionary, against its own /Resources.
func (w *fontCodeWalk) child(ctx context.Context, entry ast.PdfObject, keep func(context.Context, *document.Document, ast.PdfDict) bool, depth int) {
	stream, ok := w.firstStream(ctx, entry)
	if !ok || !keep(ctx, w.doc, stream.Dict) {
		return
	}
	w.walkStream(ctx, stream, depth+1)
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
		w.parsed(ctx, data, res, depth+1)
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
					w.walkStream(ctx, stream, 1)
				}
			}
		}
	}
}

// walkStream walks a form-like stream, a form XObject, a pattern cell or an
// appearance, at depth against its own /Resources, and leaves one that states
// none, as compliance does.
func (w *fontCodeWalk) walkStream(ctx context.Context, stream ast.PdfStream, depth int) {
	res := liveDictValue(ctx, w.alloc, w.doc, stream.Dict["Resources"])
	if res == nil {
		return
	}
	data, err := w.doc.DecodeStream(ctx, stream)
	if err != nil {
		w.doc.Logger.Debug("remediation: fonts: cannot decode a content stream to read its codes", "error", err)
		return
	}
	w.parsed(ctx, data, res, depth)
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

// isColouredTilingPattern reports whether a pattern dictionary is a coloured
// tiling pattern (ISO 32000-1, 8.7.3.1, Table 75: PatternType 1, PaintType 1).
// compliance's font walk leaves an uncoloured one out, as veraPDF does.
func isColouredTilingPattern(ctx context.Context, doc *document.Document, dict ast.PdfDict) bool {
	return isTilingPattern(ctx, doc, dict) && !isUncolouredPattern(ctx, doc, dict)
}
