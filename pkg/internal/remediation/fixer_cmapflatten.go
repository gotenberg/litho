package remediation

import (
	"context"
	"fmt"
	"log/slog"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/typography/cmap"
)

// maxCMapChainDepth bounds how far the /UseCMap chain is followed. Anti-DoS
// guard, not spec-mandated: real usecmap chains are 1-2 deep. It mirrors
// maxUseCMapChainDepth in the checker, so the fixer cannot be asked to settle
// a chain deeper than the one the checker reports.
const maxCMapChainDepth = 5

// matchCMapFlatten claims the two rules a /UseCMap entry can break.
//
// The chain rule rejects a reference to a CMap ISO 32000-1, 9.7.5.2, Table 118
// does not name (ISO 19005-2 and 19005-3, 6.2.11.3.3 test 3, ISO 19005-4,
// 6.2.10.3.3 test 3, ISO 14289-1, 7.21.3.3 test 3, ISO 14289-2, 8.4.5.4 test
// 3). The embedding rule requires the base CMap, and the CMap a font's
// /Encoding names, to be embedded or predefined, filed under the same clauses
// at test 1, and ISO 19005-1 states it at 6.3.3.3.
//
// Every repair edits one dictionary entry, so one fixer owns them. The
// context gate separates them from the WMode disagreement filed under the same
// clauses, which [matchCIDSystemInfo] claims and settles differently.
func matchCMapFlatten(v compliance.Violation) bool {
	if !clausePrefix(v, "6.3.3.3", "6.2.10.3.3", "6.2.11.3.3", "7.21.3.3", "8.4.5.4") {
		return false
	}
	return contextContains(v, "references non-predefined") ||
		contextContains(v, "uses non-embedded CMap") ||
		contextContains(v, "is not predefined and not embedded")
}

// fixCMapFlatten repairs the /UseCMap entry and the usecmap operators of every
// embedded encoding CMap a Type 0 font uses, and the /Encoding of a font
// naming a CMap outside Table 118, or under PDF/A-1 any CMap but the Identity
// ones, which [embedNamedEncoding] embeds. Four
// shapes of /UseCMap, four repairs, and the difference between them is what
// keeps the repair honest:
//
//   - a chain of embedded streams is inlined, so nothing is referenced.
//   - a base named outside Table 118, which a reader resolves
//     (Adobe-KR-9, Adobe-Korea1-2), is inlined from the copy veraPDF reads,
//     so every code keeps its CID.
//   - a name that can never denote a CMap is dropped: it resolves to nothing,
//     so it contributes nothing.
//   - a name that denotes a real predefined CMap litho ships is EMBEDDED,
//     because the mappings it contributes are real and ISO 19005-1, 6.3.3.3
//     wants them in the file.
//
// A Type 0 font stored in an object stream (ISO 32000-1, 7.5.7) has its CMap
// repaired too, for the reason [walkNumberedObjects] gives.
func fixCMapFlatten(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, profile string) ([]string, error) {
	// sweptObjects yields in object-number order, so the fix list is identical
	// across runs.
	var fixes []string
	done := make(map[int]bool)
	embedded := make(map[ast.PdfName]ast.PdfIndirectRef)
	for objNum, entry := range sweptObjects(doc) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if entry.Free || objNum == 0 {
			continue
		}
		obj, err := doc.Resolve(ctx, ast.PdfIndirectRef{ObjectNum: objNum, GenNum: entry.Generation})
		if err != nil || obj == nil {
			continue
		}
		fontDict, ok := obj.(ast.PdfDict)
		if !ok {
			continue
		}
		if typ, _ := fontDict["Type"].(ast.PdfName); typ != "Font" {
			continue
		}
		if subtype, _ := fontDict["Subtype"].(ast.PdfName); subtype != "Type0" {
			continue
		}
		if cmapName, ok := embedNamedEncoding(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: objNum, GenNum: entry.Generation}, embedded, profile); ok {
			fixes = append(fixes, fmt.Sprintf(
				"embedded the encoding CMap %s that font %s named", string(cmapName), fontBaseName(fontDict)))
			continue
		}
		encRef, isRef := fontDict["Encoding"].(ast.PdfIndirectRef)
		if !isRef || encRef.ObjectNum == 0 || done[encRef.ObjectNum] {
			continue
		}
		done[encRef.ObjectNum] = true

		name := fontBaseName(fontDict)
		switch {
		case flattenOneCMap(ctx, alloc, doc, encRef):
			fixes = append(fixes, fmt.Sprintf(
				"inlined the /UseCMap chain into the embedded CMap of font %s", name))
		case inlineNamedBases(ctx, alloc, doc, encRef):
			fixes = append(fixes, fmt.Sprintf(
				"inlined the base CMaps a usecmap or /UseCMap named into the embedded CMap of font %s", name))
		case dropUnresolvableUseCMap(ctx, alloc, doc, encRef):
			fixes = append(fixes, fmt.Sprintf(
				"dropped a /UseCMap naming no CMap from the embedded CMap of font %s", name))
		case embedNamedUseCMap(ctx, alloc, doc, encRef, profile):
			fixes = append(fixes, fmt.Sprintf(
				"embedded the predefined CMap a /UseCMap named, for font %s", name))
		}
	}
	return append(fixes, embedInlineNamedEncodings(ctx, alloc, doc, embedded, profile)...), nil
}

// flattenOneCMap rewrites one encoding CMap stream in place, reporting whether
// it did. The CMap is replaced at its own object number, so every font sharing
// it is repaired at once.
func flattenOneCMap(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, encRef ast.PdfIndirectRef) bool {
	// A pending stream is one an earlier repair already rewrote.
	if _, pending := alloc.Pending()[encRef.ObjectNum]; pending {
		return false
	}
	stream, err := document.ResolveStream(ctx, doc, encRef)
	if err != nil || stream.Dict == nil {
		return false
	}
	if _, has := stream.Dict["UseCMap"]; !has {
		return false
	}
	using, err := doc.DecodeStream(ctx, stream)
	if err != nil || len(using) == 0 {
		doc.Logger.Debug("remediation: encoding CMap decode failed, usecmap chain left as written",
			slog.Int("obj", encRef.ObjectNum), slog.Any("err", err))
		return false
	}
	bases := useCMapChain(ctx, doc, stream.Dict)
	if len(bases) == 0 {
		return false
	}

	merged, changed, err := cmap.FlattenUseCMap(ctx, using, bases)
	if err != nil {
		doc.Logger.Debug("remediation: usecmap chain unreadable, left as written",
			slog.Int("obj", encRef.ObjectNum), slog.Any("err", err))
		return false
	}
	if !changed {
		return false
	}
	return alloc.AllocAt(encRef.ObjectNum, ast.NewStream(flattenedCMapDict(stream.Dict), merged))
}

// inlineNamedBases folds into one encoding CMap stream the bases its usecmap
// operators and its /UseCMap name, when one of them is no predefined CMap
// of ISO 32000-1, 9.7.5.2, Table 118 ([compliance.IsPredefinedCMapName]),
// reporting whether it rewrote the stream.
//
// ISO 19005-2, 6.2.11.3.3 test 3 rejects the reference, not the mapping: a
// base such as Adobe-KR-9 or Adobe-Korea1-2 is a real CMap veraPDF 1.30.2
// loads and decodes the font's codes through, so dropping it would send every
// code it maps to CID 0 (ISO 32000-1, 9.7.6.3). Each base is rendered from
// the copy the checker reads ([cmap.ConformanceBaseProgram]) and folded in by
// [cmap.FlattenUseCMap], which keeps the rewrite only when it reads back as
// the merged mapping. A base litho cannot render leaves the stream as written.
func inlineNamedBases(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, encRef ast.PdfIndirectRef) bool {
	if _, pending := alloc.Pending()[encRef.ObjectNum]; pending {
		return false
	}
	stream, err := document.ResolveStream(ctx, doc, encRef)
	if err != nil || stream.Dict == nil {
		return false
	}
	using, err := doc.DecodeStream(ctx, stream)
	if err != nil || len(using) == 0 {
		return false
	}
	program, err := cmap.ParseCID(ctx, using)
	if err != nil {
		doc.Logger.Debug("remediation: encoding CMap unreadable, usecmap operators left as written",
			slog.Int("obj", encRef.ObjectNum), slog.Any("err", err))
		return false
	}
	names, ok := program.UseCMapOperands()
	if !ok {
		return false
	}
	_, hasUseCMap := stream.Dict["UseCMap"]
	if hasUseCMap {
		// A /UseCMap stream is flattenOneCMap's, and a value that is neither a
		// name nor a stream is left as written.
		name, err := document.ResolveName(ctx, doc, stream.Dict["UseCMap"])
		if err != nil || name == "" {
			return false
		}
		names = append(names, string(name))
	}
	refused := false
	for _, name := range names {
		if !compliance.IsPredefinedCMapName(name) {
			refused = true
		}
	}
	if !refused {
		return false
	}
	bases := make([][]byte, 0, len(names))
	for _, name := range names {
		prog, ok, err := cmap.ConformanceBaseProgram(ctx, name)
		if err != nil || !ok {
			doc.Logger.Debug("remediation: cannot render a named base CMap, reference left as written",
				slog.String("cmap", name), slog.Any("err", err))
			return false
		}
		bases = append(bases, prog)
	}
	merged, changed, err := cmap.FlattenUseCMap(ctx, using, bases)
	if err != nil || !changed {
		doc.Logger.Debug("remediation: named base CMaps not inlined, reference left as written",
			slog.Int("obj", encRef.ObjectNum), slog.Any("err", err))
		return false
	}
	return alloc.AllocAt(encRef.ObjectNum, ast.NewStream(flattenedCMapDict(stream.Dict), merged))
}

// flattenedCMapDict returns the dictionary of an encoding CMap stream whose
// program a repair rewrote with its bases inlined. The new stream carries
// decoded bytes, so the entries describing how the old bytes were stored go
// (ISO 32000-1, Table 5). /UseCMap goes because the mapping it contributed is
// now written out in full. Everything that names the CMap stays, so the font
// still declares the collection it did.
func flattenedCMapDict(old ast.PdfDict) ast.PdfDict {
	dict := make(ast.PdfDict, len(old))
	maps.Copy(dict, old)
	delete(dict, "UseCMap")
	for _, k := range []ast.PdfName{"Length", "Filter", "DecodeParms", "DL", "F", "FFilter", "FDecodeParms"} {
		delete(dict, k)
	}
	return dict
}

// dropUnresolvableUseCMap removes a /UseCMap whose value is a name that can
// never denote a CMap, reporting whether it rewrote the dictionary.
//
// ISO 19005-1, 6.3.3.3 and the test 1 of its restatements require the base
// CMap to be embedded or predefined. A name shaped
// "Adobe-<Registry>-<Supplement>" is a character collection, not one of the
// CMaps ISO 32000-1, 9.7.5.2, Table 118 names. Adobe publishes such a CMap
// for each supplement of its collections, and veraPDF bundles them, so
// [inlineNamedBases] inlines every one litho can render first; a name that
// reaches this repair resolves to nothing on either side, and dropping the
// entry costs no mapping.
//
// Only that shape is dropped. H, Identity-H and UniKS-UTF16-H are real
// predefined CMaps whose mappings a reader does resolve, and removing one
// would take those mappings with it, so they are left for
// [embedNamedUseCMap] or left alone. [compliance.IsCIDSystemInfoShapedCMapName]
// is the checker's own test, so the repair cannot reach further than the
// report.
func dropUnresolvableUseCMap(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, encRef ast.PdfIndirectRef) bool {
	if _, pending := alloc.Pending()[encRef.ObjectNum]; pending {
		return false
	}
	stream, err := document.ResolveStream(ctx, doc, encRef)
	if err != nil || stream.Dict == nil {
		return false
	}
	name, err := document.ResolveName(ctx, doc, stream.Dict["UseCMap"])
	if err != nil || name == "" || !compliance.IsCIDSystemInfoShapedCMapName(string(name)) {
		return false
	}

	// The body is carried across untouched, so the entries describing how it
	// is stored stay with it. Only the description changes.
	raw, err := stream.Bytes(ctx)
	if err != nil {
		doc.Logger.Debug("remediation: cannot carry a CMap body across to drop /UseCMap",
			slog.Int("obj", encRef.ObjectNum), slog.Any("err", err))
		return false
	}
	dict := make(ast.PdfDict, len(stream.Dict))
	maps.Copy(dict, stream.Dict)
	delete(dict, "UseCMap")
	return alloc.AllocAt(encRef.ObjectNum, ast.NewStream(dict, raw))
}

// embedNamedUseCMap writes the predefined CMap a /UseCMap names into the file
// and points the entry at it, reporting whether it did.
//
// ISO 19005-1, 6.3.3.3 requires every CMap but Identity-H and Identity-V
// embedded, so under part 1 a name is a violation however legitimate the CMap
// it denotes. Parts 2 and later, and PDF/UA, PERMIT the Table 118 names by
// name (ISO 19005-2, 6.2.11.3.3), so embedding there would rewrite a document
// for no compliance gain. Hence the part check: it follows the rule, not
// litho's checker.
//
// The mapping comes from the copy the conformance checks read under the name
// ([cmap.EmbedPredefined]), so the stream says exactly what the name said. A
// name litho does not ship is left alone: writing a CMap it cannot read would
// fabricate the mapping the font depends on.
func embedNamedUseCMap(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, encRef ast.PdfIndirectRef, profile string) bool {
	if compliance.PDFAPart(profile) != 1 {
		return false
	}
	if _, pending := alloc.Pending()[encRef.ObjectNum]; pending {
		return false
	}
	stream, err := document.ResolveStream(ctx, doc, encRef)
	if err != nil || stream.Dict == nil {
		return false
	}
	name, err := document.ResolveName(ctx, doc, stream.Dict["UseCMap"])
	if err != nil || name == "" {
		return false
	}
	// A CIDSystemInfo-shaped name denotes nothing, and
	// dropUnresolvableUseCMap settles that one.
	//
	// The Identity check is defence in depth, not a load-bearing guard:
	// litho ships no bundled data for Identity-H or Identity-V, so
	// cmap.EmbedPredefined already declines them, and removing this line
	// changes no observable behaviour today. It states the exemption ISO
	// 19005-1, 6.3.3.3 grants those two, so the intent survives a change to
	// what litho bundles.
	if name == "Identity-H" || name == "Identity-V" ||
		compliance.IsCIDSystemInfoShapedCMapName(string(name)) {
		return false
	}

	prog, ok, err := cmap.EmbedPredefined(ctx, string(name))
	if err != nil {
		doc.Logger.Debug("remediation: cannot render the named CMap, /UseCMap left as written",
			slog.String("cmap", string(name)), slog.Any("err", err))
		return false
	}
	if !ok {
		return false
	}

	reg, ord, sup, ok := cmap.PredefinedCMapCIDSystemInfo(string(name), false)
	if !ok {
		return false
	}
	// The dictionary's /WMode states the program's, which EmbedPredefined
	// writes from the named CMap: ISO 19005-2, 6.2.11.3.3 requires the two
	// identical, and a -V CMap declares 1.
	base := cmap.LoadConformanceCIDMap(string(name))
	if base == nil {
		return false
	}
	baseRef := alloc.Alloc(ast.NewStream(ast.PdfDict{
		"Type":     ast.PdfName("CMap"),
		"CMapName": name,
		"WMode":    ast.PdfInt(base.WMode()),
		"CIDSystemInfo": ast.PdfDict{
			"Registry":   ast.PdfString(reg),
			"Ordering":   ast.PdfString(ord),
			"Supplement": ast.PdfInt(sup),
		},
	}, prog))

	// Only the reference changes: the using CMap's own program is carried
	// across untouched, so the entries describing how it is stored stay too.
	raw, err := stream.Bytes(ctx)
	if err != nil {
		doc.Logger.Debug("remediation: cannot carry a CMap body across to embed /UseCMap",
			slog.Int("obj", encRef.ObjectNum), slog.Any("err", err))
		return false
	}
	dict := make(ast.PdfDict, len(stream.Dict))
	maps.Copy(dict, stream.Dict)
	dict["UseCMap"] = baseRef
	return alloc.AllocAt(encRef.ObjectNum, ast.NewStream(dict, raw))
}

// embedNamedEncoding embeds the CMap the /Encoding of the Type 0 font at ref
// names and points the entry at the stream, returning the name when it did.
// embedded holds the stream already written for a name, so fonts naming one
// CMap share it.
//
// Two rules ask for it:
//
//   - a CMap outside ISO 32000-1, 9.7.5.2, Table 118 must be embedded under
//     every profile: ISO 19005-2 and 19005-3, 6.2.11.3.3 test 1, ISO 19005-4,
//     6.2.10.3.3 test 1, ISO 14289-1, 7.21.3.3 test 1, ISO 14289-2, 8.4.5.4
//     test 1, ISO 19005-1, 6.3.3.3. veraPDF still decodes the font through the
//     file it bundles under such a name, but nothing requires a reader to hold
//     one: PDF.js ships no UniAKR-UTF16-H. The stream carries that file byte
//     for byte ([cmap.ConformanceProgram]), and its dictionary states the
//     CIDSystemInfo the program declares, which the name alone cannot supply
//     (6.2.11.3.1). A character collection's own CMap, Adobe-Korea1-2 or
//     Adobe-Japan1-2, is such a name too, and a real CMap: veraPDF and
//     PDF.js both bundle it and decode the font's codes through it. Its
//     stream carries it rendered from that data ([cmap.EncodingProgram]),
//     with the CIDSystemInfo the file declares.
//   - ISO 19005-1, 6.3.3.3 requires every CMap but Identity-H and Identity-V
//     embedded, so under part 1 a Table 118 name fails too. Parts 2 and later,
//     and PDF/UA, PERMIT it by name, so it stays a name there, as
//     [embedNamedUseCMap] keeps a /UseCMap name. The program is the copy the
//     conformance checks read under the name ([cmap.EncodingProgram]): veraPDF's
//     file where litho holds it, otherwise one rendered from the bundled data
//     with its usecmap chain flattened, so every code keeps its CID.
//
// Unlike [embedNamedUseCMap], the repair embeds a program past the range cap
// [cmap.EmbedPredefined] keeps: the CMap is the font's whole encoding, and no
// conforming reader of a part-1 file may be assumed to hold it, nor any
// reader at all to hold a name outside the table. A name litho holds no
// faithful copy of is left as written.
//
// # Corpus evidence
//
//   - All corpora: 34 files give a Type 0 font a named non-Identity
//     /Encoding, 2 of them (litho fontcopies_2b.pdf, pdfjs issue11555.pdf) in
//     a font written inline. Remediated at PDF/A-1a and 1b, default and
//     aggressive, veraPDF 1.30.2 reported 6.3.3.3 t1 on 116 of the 136
//     outputs before and on 20 after. 6.3.3.1 t1 and, at 1a, 6.3.8 t1
//     cleared with it on 9 files, no rule appeared, and litho and veraPDF
//     reported the same rule set on every output.
//   - verapdf 6-2-11-3-3-t01-fail-a, 6-2-10-3-3-t01-fail-a,
//     7.21.3.3-t01-fail-a, 8.4.5.4-t01-fail-a: 16 of the 20 named
//     Adobe-Korea1-2 or Adobe-Japan1-2, which litho once took to denote no
//     CMap. Embedding them cleared 6.3.3.3 t1 and 6.3.3.1 t1 on all 16, and
//     the embedding and CIDSystemInfo rules at PDF/A-2b, PDF/A-4, PDF/UA-1
//     and PDF/UA-2 on the 32 outputs there, default and aggressive: veraPDF
//     finds all 48 conforming.
//   - litho cmaps_2b.pdf: the other 4 name LithoCustomCMap, which no reader
//     holds, so nothing faithful can be embedded.
//   - All corpora: the other 40 files 6.3.3.3 fires on, remediated at
//     PDF/A-1a, 1b, 2b, 4, PDF/UA-1 and UA-2, default and aggressive, give
//     byte-identical outputs.
func embedNamedEncoding(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, ref ast.PdfIndirectRef, embedded map[ast.PdfName]ast.PdfIndirectRef, profile string) (ast.PdfName, bool) {
	font := resolveFromAllocOrDoc(ctx, alloc, doc, ref)
	if font == nil {
		return "", false
	}
	name, encRef, ok := namedEncodingStream(ctx, alloc, doc, font, embedded, profile)
	if !ok {
		return "", false
	}
	newFont := maps.Clone(font)
	newFont["Encoding"] = encRef
	if !alloc.AllocAt(ref.ObjectNum, newFont) {
		return "", false
	}
	return name, true
}

// embedInlineNamedEncodings is [embedNamedEncoding] for the Type 0 fonts
// written inline in a resource dictionary, which ISO 32000-1, 7.3.7 permits
// and no object number names, so the sweep over numbered objects never meets
// them while both checkers judge them wherever a stream selects them. Each
// moves to an object of its own, which its resource dictionary names in its
// place ([inlineFontSite.replace]), and carries the embedded stream. The
// fonts are those [collectFontUses] reaches, in the order it reaches them,
// so the fix list is identical across runs.
func embedInlineNamedEncodings(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, embedded map[ast.PdfName]ast.PdfIndirectRef, profile string) []string {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		doc.Logger.Debug("remediation: cmap: no catalog, inline fonts left as written", slog.Any("err", err))
		return nil
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		doc.Logger.Debug("remediation: cmap: no page tree, inline fonts left as written", slog.Any("err", err))
		return nil
	}
	uses := collectFontUses(ctx, alloc, doc, pages, pageTreeEntries(ctx, alloc, doc, pages))
	var fixes []string
	for _, use := range uses.inline {
		if ctx.Err() != nil {
			break
		}
		font := use.site.font(ctx, alloc, doc)
		if subtype, _ := font["Subtype"].(ast.PdfName); subtype != "Type0" || !use.site.writable(ctx, alloc, doc) {
			continue
		}
		name, encRef, ok := namedEncodingStream(ctx, alloc, doc, font, embedded, profile)
		if !ok {
			continue
		}
		newFont := maps.Clone(font)
		newFont["Encoding"] = encRef
		if !use.site.replace(ctx, alloc, doc, alloc.Alloc(newFont)) {
			doc.Logger.Debug("remediation: cannot write back an inline font, its /Encoding left as written",
				slog.String("font", fontBaseName(font)))
			continue
		}
		fixes = append(fixes, fmt.Sprintf(
			"embedded the encoding CMap %s that font %s, written inline, named", string(name), fontBaseName(font)))
	}
	return fixes
}

// namedEncodingStream returns the name font's /Encoding gives and a stream
// carrying the CMap it names, when [embedNamedEncoding] embeds it, writing
// the stream the first time a name is asked for and recording it in embedded.
func namedEncodingStream(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, font ast.PdfDict, embedded map[ast.PdfName]ast.PdfIndirectRef, profile string) (ast.PdfName, ast.PdfIndirectRef, bool) {
	// A stream fails to resolve as a name, so only a named encoding goes on.
	name, err := document.ResolveName(ctx, doc, font["Encoding"])
	if err != nil || name == "" || name == "Identity-H" || name == "Identity-V" {
		return "", ast.PdfIndirectRef{}, false
	}
	predefined := compliance.IsPredefinedCMapName(string(name))
	if predefined && compliance.PDFAPart(profile) != 1 {
		return "", ast.PdfIndirectRef{}, false
	}
	if encRef, done := embedded[name]; done {
		return name, encRef, true
	}
	program := cmap.ConformanceProgram
	// A character collection's own CMap (Adobe-Korea1-2) lies outside Table
	// 118, but [cmap.EncodingProgram] knows its CIDSystemInfo, so it is
	// rendered from the bundled data as a Table 118 CMap is.
	if predefined || compliance.IsCIDSystemInfoShapedCMapName(string(name)) {
		program = func(n string) (cmap.ConformanceCMap, bool, error) { return cmap.EncodingProgram(ctx, n) }
	}
	prog, ok, err := program(string(name))
	if err != nil {
		doc.Logger.Debug("remediation: cannot read the named encoding CMap, /Encoding left as written",
			slog.String("cmap", string(name)), slog.Any("err", err))
		return "", ast.PdfIndirectRef{}, false
	}
	if !ok {
		return "", ast.PdfIndirectRef{}, false
	}
	encRef := alloc.Alloc(flateStream(ast.PdfDict{
		"Type":     ast.PdfName("CMap"),
		"CMapName": name,
		"WMode":    ast.PdfInt(prog.WMode),
		"CIDSystemInfo": ast.PdfDict{
			"Registry":   ast.PdfString(prog.Registry),
			"Ordering":   ast.PdfString(prog.Ordering),
			"Supplement": ast.PdfInt(prog.Supplement),
		},
	}, prog.Program))
	embedded[name] = encRef
	return name, encRef, true
}

// useCMapChain returns the programs a CMap builds on, nearest first. It stops
// at a /UseCMap that names a CMap rather than referencing one, since a name
// has no program in the file to inline, and returns nothing for a cycle or a
// chain deeper than [maxCMapChainDepth].
func useCMapChain(ctx context.Context, doc *document.Document, dict ast.PdfDict) [][]byte {
	var out [][]byte
	seen := make(map[int]bool)
	for depth := 0; depth < maxCMapChainDepth; depth++ {
		ref, isRef := dict["UseCMap"].(ast.PdfIndirectRef)
		if !isRef || ref.ObjectNum == 0 {
			return out
		}
		if seen[ref.ObjectNum] {
			return nil
		}
		seen[ref.ObjectNum] = true

		stream, err := document.ResolveStream(ctx, doc, dict["UseCMap"])
		if err != nil || stream.Dict == nil {
			return nil
		}
		data, err := doc.DecodeStream(ctx, stream)
		if err != nil || len(data) == 0 {
			return nil
		}
		out = append(out, data)
		dict = stream.Dict
	}
	return nil
}
