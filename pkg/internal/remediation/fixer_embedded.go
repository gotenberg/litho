package remediation

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// matchEmbeddedSubtype matches ISO 19005-4 §6.9 violations fixable at
// the filespec/stream dict level: invalid MIME type on /Subtype (test 1)
// and missing /F or /UF filespec keys (test 2).
func matchEmbeddedSubtype(v compliance.Violation) bool {
	if !clausePrefix(v, "6.9") {
		return false
	}
	return contextContains(v, "invalid /Subtype") ||
		contextContains(v, "MIME") ||
		contextContains(v, "missing F=") ||
		contextContains(v, "missing UF=")
}

// fixEmbeddedSubtype rewrites every EmbeddedFile stream's /Subtype to a
// valid MIME type. PDF/A-4 §6.9 permits any RFC 2046 media type; when
// the existing value is malformed, fall back to application/octet-stream
// as the spec's explicit default.
func fixEmbeddedSubtype(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	catalogRef, ok := catalogRefForFixer(ctx, doc)
	if !ok {
		return nil, nil
	}
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return nil, nil
	}
	namesObj, ok := catalogDict["Names"]
	if !ok {
		return nil, nil
	}
	namesDict, err := document.ResolveDict(ctx, doc, namesObj)
	if err != nil || namesDict == nil {
		return nil, nil
	}
	efObj, ok := namesDict["EmbeddedFiles"]
	if !ok {
		return nil, nil
	}
	efDict, err := document.ResolveDict(ctx, doc, efObj)
	if err != nil || efDict == nil {
		return nil, nil
	}

	count := 0
	walkEmbeddedFileTree(ctx, alloc, doc, efDict, &count, make(map[int]bool), 0)
	if count == 0 {
		return nil, nil
	}
	return []string{fmt.Sprintf("normalised /Subtype on %d embedded file(s)", count)}, nil
}

// maxNameTreeWalkDepth caps name-tree recursion in remediation fixers. Anti-DoS
// guard mirroring the compliance and core name-tree depth caps (64).
const maxNameTreeWalkDepth = 64

// walkEmbeddedFileTree recurses through an EmbeddedFiles name tree, patching
// every filespec's /EF stream entries. seen visits each node once and depth is
// capped: a name tree is a strict tree, so a /Kids tree whose every node lists
// the same child twice would otherwise fan out 2^depth (a denial of service).
func walkEmbeddedFileTree(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, node ast.PdfDict, count *int, seen map[int]bool, depth int) {
	if ctx.Err() != nil || depth > maxNameTreeWalkDepth {
		return
	}
	if names, err := document.ResolveArray(ctx, doc, node["Names"]); err == nil {
		for i := 1; i < len(names); i += 2 {
			patchFileSpec(ctx, alloc, doc, names[i], count)
		}
	}
	if kids, err := document.ResolveArray(ctx, doc, node["Kids"]); err == nil {
		for _, kid := range kids {
			if ref, ok := kid.(ast.PdfIndirectRef); ok {
				if seen[ref.ObjectNum] {
					continue
				}
				seen[ref.ObjectNum] = true
			}
			child, err := document.ResolveDict(ctx, doc, kid)
			if err != nil || child == nil {
				continue
			}
			walkEmbeddedFileTree(ctx, alloc, doc, child, count, seen, depth+1)
		}
	}
}

// patchFileSpec reaches through a filespec dict's /EF entries to fix
// /Subtype on each referenced stream, and fills in missing /F or /UF
// filespec-level keys with the other side when only one is present.
func patchFileSpec(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, spec ast.PdfObject, count *int) {
	specRef, isRef := spec.(ast.PdfIndirectRef)
	specDict, err := document.ResolveDict(ctx, doc, spec)
	if err != nil || specDict == nil {
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

	efDict, err := document.ResolveDict(ctx, doc, specDict["EF"])
	if err != nil || efDict == nil {
		return
	}
	for _, key := range []ast.PdfName{"F", "UF", "DOS", "Mac", "Unix"} {
		streamRef, ok := efDict[key].(ast.PdfIndirectRef)
		if !ok {
			continue
		}
		raw, err := doc.Resolve(ctx, streamRef)
		if err != nil {
			continue
		}
		stream, ok := raw.(ast.PdfStream)
		if !ok {
			continue
		}
		subtype, _ := stream.Dict["Subtype"].(ast.PdfName)
		if isValidMIMEType(string(subtype)) {
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

// isValidMIMEType reports whether s looks like "type/subtype". PDF/A-4
// §6.9 test 1 requires the Subtype value to parse as an RFC 2046 media
// type. Empty prefixes or multiple slashes are rejected.
func isValidMIMEType(s string) bool {
	if s == "" {
		return false
	}
	slash := strings.IndexByte(s, '/')
	if slash <= 0 || slash == len(s)-1 {
		return false
	}
	if strings.IndexByte(s[slash+1:], '/') >= 0 {
		return false
	}
	return true
}
