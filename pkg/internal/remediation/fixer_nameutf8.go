package remediation

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strconv"
	"unicode/utf8"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// matchNameUTF8 matches PDF name hygiene violations: invalid UTF-8
// bytes (ISO 19005-4 §6.1.7, ISO 19005-2 §6.1.8) and name-length
// overflows (ISO 19005-1 §6.1.12 test 4, ISO 19005-2/4 §6.1.13 test 4).
func matchNameUTF8(v compliance.Violation) bool {
	if clausePrefix(v, "6.1.7", "6.1.8") && contextContains(v, "not valid UTF-8") {
		return true
	}
	if clausePrefix(v, "6.1.12", "6.1.13") && contextContains(v, "name length") {
		return true
	}
	return false
}

// pdfANameMaxLen is the 127-byte cap on PDF names from ISO 32000-1
// Table C.1, imposed as a conformance limit by PDF/A-1 §6.1.12 test 4
// and PDF/A-2/4 §6.1.13 test 4. It aliases [compliance.MaxNameBytes] so
// detection and repair can never drift apart.
const pdfANameMaxLen = compliance.MaxNameBytes

// maxNameCollisionTries bounds the search for a free disambiguated name.
// Anti-DoS guard, not spec-mandated: ISO 32000-1 Table C.1 caps a
// dictionary at 4095 entries, so at most that many keys can collide and
// one extra attempt always finds a free slot in a conforming dictionary.
// The bound stops a crafted dictionary from spinning here.
const maxNameCollisionTries = 4096

// maxResourceScanDepth bounds the recursive scan for resource
// sub-dictionaries inside one indirect object. Anti-DoS guard, not
// spec-mandated: the scan never follows an indirect reference, so the
// only depth it sees is direct dict/array nesting, which stays in the
// single digits in real documents.
const maxResourceScanDepth = 32

// resourceCategories are the resource-dictionary entries whose
// sub-dictionary keys a content stream addresses by name: /Font for Tf,
// /ColorSpace for cs and CS, /ExtGState for gs, /XObject for Do,
// /Shading for sh, /Pattern for scn and SCN, /Properties for BDC and DP.
// Per ISO 32000-1 Table 33 (Entries in a resource dictionary).
var resourceCategories = map[ast.PdfName]bool{
	"ColorSpace": true,
	"ExtGState":  true,
	"Font":       true,
	"Pattern":    true,
	"Properties": true,
	"Shading":    true,
	"XObject":    true,
}

// fixNameUTF8 rewrites PdfName values and dictionary keys that carry
// bytes which are not valid UTF-8 (ISO 19005-2 §6.1.8, ISO 19005-4
// §6.1.7) or that exceed the 127-byte cap of ISO 19005-1 §6.1.12 test 4
// and ISO 19005-2/4 §6.1.13 test 4. Replacement uses '_' for the
// offending bytes so the new name stays visible and distinct.
//
// Names used as keys of a resource-category dictionary are deliberately
// left alone. A content stream addresses such a key by name, and this
// package cannot rewrite every reference site in lockstep:
//
//   - Not every reference lives in a content stream. An interactive
//     form's /DA default-appearance string (ISO 32000-1 §12.7.3.3,
//     Table 222) names a /DR font key from inside a PdfString, which no
//     name-level rewrite of the object graph can reach.
//   - The one content-rewrite path here (livePageContentOps →
//     serializeOps → replaceContentStream) cannot round-trip an inline
//     image: the parser drops the binary payload off-token, as
//     TestFixContentStreams_PreservesInlineImage pins. An inline image's
//     /CS operand is itself a reference into /Resources/ColorSpace, so
//     the very streams that would need rewriting are the ones that
//     cannot be re-serialized.
//   - That path also only reaches page /Contents. Form XObjects, tiling
//     patterns, Type 3 glyph procedures and annotation appearance
//     streams are content streams too (ISO 32000-1 §7.8.2, §8.10.1,
//     §8.7.3.1, §9.6.5) and are not reachable through it.
//
// Renaming the key regardless buys nothing: the over-long or malformed
// name still sits verbatim in the content stream, so the violation
// survives while the operand no longer resolves to any resource. A
// dangling reference is worse than an honest residual, so the violation
// is left for the post-fix validation to report.
func fixNameUTF8(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	pinned, err := collectResourceKeys(ctx, alloc, doc)
	if err != nil {
		return nil, fmt.Errorf("collecting resource dictionary keys: %w", err)
	}
	logDeclinedNames(doc, pinned)

	dictFn := func(d ast.PdfDict) (ast.PdfDict, bool) { return sanitizeDictNames(d, pinned) }
	arrayFn := func(a ast.PdfArray) (ast.PdfArray, bool) { return sanitizeArrayNames(a, pinned) }

	count, err := walkIndirectObjects(ctx, alloc, doc, dictFn, arrayFn)
	if err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, nil
	}
	return []string{fmt.Sprintf("sanitised non-UTF-8 names in %d object(s)", count)}, nil
}

// logDeclinedNames reports at Debug the resource keys this fixer refuses
// to rename, so a skipped repair is traceable rather than silent.
func logDeclinedNames(doc *document.Document, pinned map[string]bool) {
	declined := 0
	for name := range pinned {
		if !utf8.ValidString(name) || len(name) > pdfANameMaxLen {
			declined++
		}
	}
	if declined == 0 {
		return
	}
	doc.Logger.Debug("remediation: leaving malformed resource dictionary keys in place, renaming one would dangle its content-stream operand",
		slog.Int("names", declined))
}

// collectResourceKeys returns every name used as a key of a
// resource-category sub-dictionary anywhere in the document. Those names
// are addressed by name from content streams and /DA strings, so
// [fixNameUTF8] must not rewrite them.
//
// It scans the objects [walkIndirectObjects] rewrites, the ones stored in an
// object stream included. A page stored there holds its /Resources, and a
// scan that skipped it left that dictionary's keys unpinned for a walk that
// renames names in the same object.
func collectResourceKeys(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) (map[string]bool, error) {
	out := make(map[string]bool)
	for objNum, entry := range doc.XRefEntries() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if entry.Free || objNum == 0 {
			continue
		}
		// Read alloc-first for the same reason walkIndirectObjects does:
		// an earlier fixer may have queued a replacement whose resource
		// keys differ from the parsed original.
		obj, pending := alloc.Pending()[objNum]
		if !pending {
			resolved, err := doc.Resolve(ctx, ast.PdfIndirectRef{ObjectNum: objNum, GenNum: entry.Generation})
			if err != nil || resolved == nil {
				// An object that cannot be resolved contributes no keys.
				// walkIndirectObjects skips it for the same reason, so the
				// two passes stay in step.
				doc.Logger.Debug("remediation: skipping object in resource key scan",
					slog.Int("obj", objNum), slog.Any("err", err))
				continue
			}
			obj = resolved
		}
		scanResourceKeys(ctx, alloc, doc, obj, out, 0)
	}
	return out, nil
}

// scanResourceKeys walks obj and records the keys of every
// resource-category sub-dictionary it finds. Indirect references are
// never followed except for the one hop that reaches a category
// dictionary, so the walk terminates without cycle tracking.
func scanResourceKeys(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, obj ast.PdfObject, out map[string]bool, depth int) {
	if depth > maxResourceScanDepth || ctx.Err() != nil {
		return
	}
	switch v := obj.(type) {
	case ast.PdfStream:
		scanResourceKeys(ctx, alloc, doc, v.Dict, out, depth+1)
	case ast.PdfArray:
		for _, elem := range v {
			scanResourceKeys(ctx, alloc, doc, elem, out, depth+1)
		}
	case ast.PdfDict:
		for k, val := range v {
			if resourceCategories[k] {
				for name := range categoryDict(ctx, alloc, doc, val) {
					out[string(name)] = true
				}
			}
			scanResourceKeys(ctx, alloc, doc, val, out, depth+1)
		}
	}
}

// categoryDict resolves a resource-category value to its dictionary,
// reading alloc-first so a replacement queued by an earlier fixer is
// seen. A category entry that is not a dictionary contributes no keys:
// an image XObject's /ColorSpace is a name or array, and /Pattern names
// a colour space family in a cs operand.
func categoryDict(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, val ast.PdfObject) ast.PdfDict {
	switch v := val.(type) {
	case ast.PdfDict:
		return v
	case ast.PdfIndirectRef:
		return resolveFromAllocOrDoc(ctx, alloc, doc, v)
	}
	return nil
}

// sanitizeDictNames walks a PdfDict and replaces every PdfName that
// needs repair, leaving pinned resource keys untouched. Keys are visited
// in sorted order: truncation can map two distinct over-long keys onto
// the same 127-byte prefix, and the disambiguating suffix must not
// depend on Go's randomized map iteration order.
func sanitizeDictNames(dict ast.PdfDict, pinned map[string]bool) (ast.PdfDict, bool) {
	newDict := make(ast.PdfDict, len(dict))
	changed := false

	keys := slices.Sorted(maps.Keys(dict))
	taken := make(map[ast.PdfName]bool, len(dict))
	for _, k := range keys {
		if !nameNeedsFix(string(k), pinned) {
			taken[k] = true
		}
	}

	for _, k := range keys {
		newKey := k
		if nameNeedsFix(string(k), pinned) {
			if cand, ok := uniqueName(sanitizeName(string(k)), taken); ok {
				newKey = cand
				changed = true
			}
		}
		taken[newKey] = true

		switch val := dict[k].(type) {
		case ast.PdfName:
			if nameNeedsFix(string(val), pinned) {
				newDict[newKey] = ast.PdfName(sanitizeName(string(val)))
				changed = true
				continue
			}
		case ast.PdfArray:
			if sub, c := sanitizeArrayNames(val, pinned); c {
				newDict[newKey] = sub
				changed = true
				continue
			}
		case ast.PdfDict:
			if sub, c := sanitizeDictNames(val, pinned); c {
				newDict[newKey] = sub
				changed = true
				continue
			}
		}
		newDict[newKey] = dict[k]
	}
	if !changed {
		return nil, false
	}
	return newDict, true
}

// sanitizeArrayNames behaves like sanitizeDictNames but for arrays,
// which carry no keys and so need no collision handling.
func sanitizeArrayNames(arr ast.PdfArray, pinned map[string]bool) (ast.PdfArray, bool) {
	newArr := make(ast.PdfArray, len(arr))
	changed := false
	for i, v := range arr {
		switch val := v.(type) {
		case ast.PdfName:
			if nameNeedsFix(string(val), pinned) {
				newArr[i] = ast.PdfName(sanitizeName(string(val)))
				changed = true
				continue
			}
		case ast.PdfArray:
			if sub, c := sanitizeArrayNames(val, pinned); c {
				newArr[i] = sub
				changed = true
				continue
			}
		case ast.PdfDict:
			if sub, c := sanitizeDictNames(val, pinned); c {
				newArr[i] = sub
				changed = true
				continue
			}
		}
		newArr[i] = v
	}
	if !changed {
		return nil, false
	}
	return newArr, true
}

// nameNeedsFix reports whether a PDF name has invalid UTF-8 bytes or
// exceeds PDF/A's 127-byte length cap and may be rewritten safely. Names
// pinned as resource-dictionary keys are exempt; see [fixNameUTF8].
func nameNeedsFix(s string, pinned map[string]bool) bool {
	if pinned[s] {
		return false
	}
	return !utf8.ValidString(s) || len(s) > pdfANameMaxLen
}

// uniqueName returns base, or base plus a numeric suffix when base is
// already used by another key of the same dictionary. Truncating two
// distinct over-long keys to [pdfANameMaxLen] bytes can collapse them
// onto the same name; without disambiguation the second would silently
// overwrite the first and the dictionary would lose an entry. Reports
// false when no free name is found, in which case the caller keeps the
// original key rather than drop data.
func uniqueName(base string, taken map[ast.PdfName]bool) (ast.PdfName, bool) {
	if !taken[ast.PdfName(base)] {
		return ast.PdfName(base), true
	}
	for i := 1; i <= maxNameCollisionTries; i++ {
		suffix := strconv.Itoa(i)
		stem := base
		if len(stem)+len(suffix) > pdfANameMaxLen {
			stem = truncateUTF8(stem, pdfANameMaxLen-len(suffix))
		}
		if cand := ast.PdfName(stem + suffix); !taken[cand] {
			return cand, true
		}
	}
	return "", false
}

// sanitizeName replaces bytes outside valid UTF-8 and truncates the name
// to [pdfANameMaxLen] bytes. ISO 19005-2 §6.1.8 and ISO 19005-4 §6.1.7
// require names to decode as UTF-8; ISO 19005-1 §6.1.12 test 4 and
// ISO 19005-2/4 §6.1.13 test 4 cap them at 127 bytes.
func sanitizeName(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size <= 1 {
			out = append(out, '_')
			i++
			continue
		}
		out = append(out, s[i:i+size]...)
		i += size
	}
	if len(out) == 0 {
		return "_"
	}
	return truncateUTF8(string(out), pdfANameMaxLen)
}

// truncateUTF8 cuts s to at most n bytes on a rune boundary. Cutting in
// the middle of a multi-byte sequence would leave the name invalid UTF-8
// and re-violate ISO 19005-2 §6.1.8 / ISO 19005-4 §6.1.7, the rules this
// fixer exists to satisfy.
func truncateUTF8(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
