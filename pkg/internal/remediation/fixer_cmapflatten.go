package remediation

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"

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
// 3). The embedding rule requires the base CMap to be embedded or predefined,
// filed under the same clauses at test 1, and ISO 19005-1 states it at 6.3.3.3.
//
// All three repairs edit one dictionary entry, so one fixer owns them. The
// context gate separates them from the WMode disagreement filed under the same
// clauses, which [matchCIDSystemInfo] claims and settles differently.
func matchCMapFlatten(v compliance.Violation) bool {
	if !clausePrefix(v, "6.3.3.3", "6.2.10.3.3", "6.2.11.3.3", "7.21.3.3", "8.4.5.4") {
		return false
	}
	return contextContains(v, "references non-predefined") ||
		contextContains(v, "uses non-embedded CMap")
}

// fixCMapFlatten repairs the /UseCMap entry of every embedded encoding CMap a
// Type 0 font uses. Three shapes, three repairs, and the difference between
// them is what keeps the repair honest:
//
//   - a chain of embedded streams is inlined, so nothing is referenced.
//   - a name that can never denote a CMap is dropped: it resolves to nothing,
//     so it contributes nothing.
//   - a name that denotes a real predefined CMap litho ships is EMBEDDED,
//     because the mappings it contributes are real and ISO 19005-1, 6.3.3.3
//     wants them in the file.
func fixCMapFlatten(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, profile string) ([]string, error) {
	// Sorted so the fix list is identical across runs: Index.Entries is a map.
	nums := make([]int, 0, len(doc.Index.Entries))
	for objNum := range doc.Index.Entries {
		nums = append(nums, objNum)
	}
	slices.Sort(nums)

	var fixes []string
	done := make(map[int]bool)
	for _, objNum := range nums {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		entry := doc.Index.Entries[objNum]
		if entry.Type != 1 || objNum == 0 {
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
		case dropUnresolvableUseCMap(ctx, alloc, doc, encRef):
			fixes = append(fixes, fmt.Sprintf(
				"dropped a /UseCMap naming no CMap from the embedded CMap of font %s", name))
		case embedNamedUseCMap(ctx, alloc, doc, encRef, profile):
			fixes = append(fixes, fmt.Sprintf(
				"embedded the predefined CMap a /UseCMap named, for font %s", name))
		}
	}
	return fixes, nil
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

	// The new stream carries decoded bytes, so the entries describing how the
	// old bytes were stored go (ISO 32000-1, Table 5). /UseCMap goes because
	// the mapping it contributed is now written out in full. Everything that
	// names the CMap stays, so the font still declares the collection it did.
	dict := make(ast.PdfDict, len(stream.Dict))
	maps.Copy(dict, stream.Dict)
	delete(dict, "UseCMap")
	for _, k := range []ast.PdfName{"Length", "Filter", "DecodeParms", "DL", "F", "FFilter", "FDecodeParms"} {
		delete(dict, k)
	}
	return alloc.AllocAt(encRef.ObjectNum, ast.NewStream(dict, merged))
}

// dropUnresolvableUseCMap removes a /UseCMap whose value is a name that can
// never denote a CMap, reporting whether it rewrote the dictionary.
//
// ISO 19005-1, 6.3.3.3 and the test 1 of its restatements require the base
// CMap to be embedded or predefined. A name shaped
// "Adobe-<Registry>-<Supplement>" is a character collection, not one of the
// CMaps ISO 32000-1, 9.7.5.2, Table 118 names, so no reader resolves it: the
// entry contributes no mapping and dropping it costs none.
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
// The mapping comes from litho's bundled copy of the CMap, so the stream says
// exactly what the name said. A name litho does not ship is left alone:
// writing a CMap it cannot read would fabricate the mapping the font depends
// on.
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
	baseRef := alloc.Alloc(ast.NewStream(ast.PdfDict{
		"Type":     ast.PdfName("CMap"),
		"CMapName": name,
		"WMode":    ast.PdfInt(0),
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
