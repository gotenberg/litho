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
)

// matchEmbeddedSubtype matches the embedded-file violations fixable at the
// filespec/stream dict level: ISO 19005-4 §6.9, an invalid MIME type on
// /Subtype (test 1) and missing /F or /UF filespec keys (test 2), and ISO
// 19005-3 §6.8 test 1, which states the same MIME type requirement.
//
// Both media type rules match on the full specification, clause and test
// number, never on the words of the message: clause 6.8 holds the /F and /UF,
// /AFRelationship and /AF rules of parts 2 and 3 too, which [fixFileSpecFUF]
// and [fixAFRelationship] repair, and the message of a /Subtype that is
// missing or not a name mentions no MIME type.
//
// # Corpus evidence
//
//   - pdfjs issue9367.pdf: its three embedded files carry no /Subtype, and
//     each kept its ISO 19005-3, 6.8 t1 finding after a PDF/A-3b
//     remediation, while a PDF/A-4f one gave them application/octet-stream.
//   - All corpora: 32 files of the 264 that embed files kept one or more
//     6.8 t1 findings after a PDF/A-3b remediation, and none keeps one now.
func matchEmbeddedSubtype(v compliance.Violation) bool {
	if v.Rule.TestNumber == 1 &&
		(v.Rule.Specification == "ISO_19005_3" && v.Rule.Clause == "6.8" ||
			v.Rule.Specification == "ISO_19005_4" && v.Rule.Clause == "6.9") {
		return true
	}
	if !clausePrefix(v, "6.9") {
		return false
	}
	return contextContains(v, "missing F=") || contextContains(v, "missing UF=")
}

// fixEmbeddedSubtype rewrites every EmbeddedFile stream's /Subtype to a
// valid MIME type. PDF/A-3 §6.8 and PDF/A-4 §6.9 permit any RFC 2046 media
// type; when the existing value is malformed, fall back to
// application/octet-stream as the spec's explicit default.
//
// It reaches the file specifications the validator judges: the values of the
// /EmbeddedFiles name tree, whatever their /Type (ISO 32000-1, 7.11.4), whose
// missing /F or /UF it also fills, and every other /Type /Filespec dictionary,
// indirect or written inline in any object ([rewriteInlineFileSpecs]), whose
// embedded files alone it repairs, leaving their /F and /UF to
// [fixFileSpecFUF]. An embedded file is a stream, and so an indirect object
// (ISO 32000-1, 7.3.8), whoever names it: the repair rewrites the stream and
// never the object holding an inline file specification.
//
// Every dictionary and stream is read through the allocator: string-limit and
// the other object sweeps ahead of this fixer may have rewritten the name
// tree, a file specification or an embedded file, and a copy built on the
// document's would put back what they repaired.
//
// # Corpus evidence
//
//   - pdfjs bug1230933.pdf, litho annotstruct_ua2.pdf: two FileAttachment
//     annotations each name a /Filespec outside the tree whose embedded file
//     has no /Subtype, or /text#232Fplain, and each kept its ISO 19005-3, 6.8
//     t1 or ISO 19005-4, 6.9 t1 finding after a PDF/A-3b or PDF/A-4
//     remediation.
//   - litho inlinefs_4.pdf: two Links list an embedded file with no valid
//     media type in an inline /AF entry, one of them a Link written inline in
//     the page's /Annots, and each kept its ISO 19005-4, 6.9 t1 finding after
//     a PDF/A-4 remediation.
func fixEmbeddedSubtype(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	catalogRef, ok := catalogRefForFixer(ctx, doc)
	if !ok {
		return nil, nil
	}
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return nil, nil
	}

	count := 0
	if namesDict := liveDictValue(ctx, alloc, doc, catalogDict["Names"]); namesDict != nil {
		if efDict := liveDictValue(ctx, alloc, doc, namesDict["EmbeddedFiles"]); efDict != nil {
			walkEmbeddedFileTree(ctx, alloc, doc, efDict, &count, make(map[int]bool), 0)
		}
	}
	for objNum, entry := range sweptObjects(doc) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if entry.Free || objNum == 0 {
			continue
		}
		spec := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: objNum, GenNum: entry.Generation})
		if spec == nil {
			continue
		}
		if typeName, _ := document.ResolveName(ctx, doc, spec["Type"]); typeName != "Filespec" {
			continue
		}
		patchEmbeddedFileTypes(ctx, alloc, doc, spec, &count)
	}
	if _, err := rewriteInlineFileSpecs(ctx, alloc, doc, func(_ int, _ string, spec ast.PdfDict) (ast.PdfObject, bool) {
		patchEmbeddedFileTypes(ctx, alloc, doc, spec, &count)
		return nil, false
	}); err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, nil
	}
	return []string{fmt.Sprintf("normalised /Subtype on %d embedded file(s)", count)}, nil
}

// maxNameTreeWalkDepth caps name-tree recursion in remediation fixers. Anti-DoS
// guard, not spec-mandated: name trees in real documents rarely exceed a
// handful of levels. It mirrors the compliance and core name-tree depth caps
// (64).
const maxNameTreeWalkDepth = 64

// walkEmbeddedFileTree recurses through an EmbeddedFiles name tree, patching
// every filespec's /EF stream entries. seen visits each node once and depth is
// capped: a name tree is a strict tree, so a /Kids tree whose every node lists
// the same child twice would otherwise fan out 2^depth (a denial of service).
func walkEmbeddedFileTree(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, node ast.PdfDict, count *int, seen map[int]bool, depth int) {
	if ctx.Err() != nil || depth > maxNameTreeWalkDepth {
		return
	}
	names := liveArray(ctx, alloc, doc, node["Names"])
	for i := 1; i < len(names); i += 2 {
		patchFileSpec(ctx, alloc, doc, names[i], count)
	}
	for _, kid := range liveArray(ctx, alloc, doc, node["Kids"]) {
		if ref, ok := kid.(ast.PdfIndirectRef); ok {
			if seen[ref.ObjectNum] {
				continue
			}
			seen[ref.ObjectNum] = true
		}
		child := liveDictValue(ctx, alloc, doc, kid)
		if child == nil {
			continue
		}
		walkEmbeddedFileTree(ctx, alloc, doc, child, count, seen, depth+1)
	}
}

// patchFileSpec reaches through a filespec dict's /EF entries to fix
// /Subtype on each referenced stream, and fills in missing /F or /UF
// filespec-level keys with the other side when only one is present.
func patchFileSpec(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, spec ast.PdfObject, count *int) {
	specRef, isRef := spec.(ast.PdfIndirectRef)
	specDict := liveDictValue(ctx, alloc, doc, spec)
	if specDict == nil {
		return
	}

	// Test 2: filespec must carry both /F and /UF.
	fVal, hasF := specDict["F"]
	ufVal, hasUF := specDict["UF"]
	if hasF != hasUF {
		newSpec := make(ast.PdfDict, len(specDict)+1)
		maps.Copy(newSpec, specDict)
		if !hasUF {
			newSpec["UF"] = fVal
		} else {
			newSpec["F"] = ufVal
		}
		if isRef && specRef.ObjectNum > 0 {
			alloc.AllocAt(specRef.ObjectNum, newSpec)
			specDict = newSpec
			*count++
		}
	}

	patchEmbeddedFileTypes(ctx, alloc, doc, specDict, count)
}

// patchEmbeddedFileTypes gives each embedded file the file specification
// specDict names in its /EF a media type the validator accepts, and adds the
// embedded files it rewrote to count.
func patchEmbeddedFileTypes(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, specDict ast.PdfDict, count *int) {
	efDict := liveDictValue(ctx, alloc, doc, specDict["EF"])
	if efDict == nil {
		return
	}
	for _, key := range []ast.PdfName{"F", "UF", "DOS", "Mac", "Unix"} {
		streamRef, ok := efDict[key].(ast.PdfIndirectRef)
		if !ok {
			continue
		}
		stream, ok := liveStream(ctx, alloc, doc, streamRef)
		if !ok {
			continue
		}
		// The validator's own test (ISO 19005-3, 6.8 t1 and ISO 19005-4,
		// 6.9 t1): a looser one kept a media type with a parameter, which
		// the validator refuses, and left its finding in the output.
		subtype, _ := document.ResolveName(ctx, doc, stream.Dict["Subtype"]) // Best-effort: an unreadable /Subtype is no media type.
		if compliance.IsValidMIMEType(string(subtype)) {
			continue
		}
		newDict := make(ast.PdfDict, len(stream.Dict))
		maps.Copy(newDict, stream.Dict)
		newDict["Subtype"] = ast.PdfName("application/octet-stream")
		data, err := stream.Bytes(ctx)
		if err != nil {
			// Cannot read the embedded file body: skip the rewrite so
			// the original payload stays intact rather than being
			// replaced by an empty stream.
			doc.Logger.Debug("remediation: skipping embedded-file Subtype rewrite, body read failed",
				slog.Int("obj", streamRef.ObjectNum), slog.Any("err", err))
			continue
		}
		alloc.AllocAt(streamRef.ObjectNum, ast.NewStream(newDict, data))
		*count++
	}
}
