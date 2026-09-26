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
// A name used as a key of a resource-category dictionary is addressed by
// name from content streams, and from the /DA default-appearance strings of
// an interactive form (ISO 32000-1 §12.7.3.3, Table 222), which name a /DR
// font key from inside a PdfString. Renaming the key alone would leave the
// old name in those places, addressing nothing, so such a key is renamed only
// together with every place that writes it. When a name-length violation is
// among the ones matched, [planContentNames] gives every over-long name of the
// content streams and resource dictionaries one new spelling, which
// [renameContentStreams] splices into the streams and the object walk writes
// in the dictionaries and /DA strings. A key the plan cannot rename in every
// place is left as written, a residual: a dangling reference is worse than an
// honest one. A key with invalid UTF-8 bytes and no length defect is left
// alone, since ISO 19005-2 §6.1.8 and ISO 19005-4 §6.1.7 judge the names of
// fonts, colorants and structure types, not resource keys.
//
// The object walk offers every object [everyObject] reads, the ones stored in
// an object stream and the ones an earlier fixer queued under a new number
// included, the same objects the plan scans. A dictionary that cannot take a
// rename, because it already holds the new spelling as a key or because the
// stream it belongs to cannot be read back, refuses it, and the whole repair
// runs again from where it started without that rename.
//
// # Corpus evidence
//
//   - pdfjs issue6151.pdf: selects its font with Tf by a /Font key of 139
//     bytes, and litho limits_2b.pdf paints with cs by a /ColorSpace key of
//     203 bytes. Both stayed residuals, in the key and in the content stream,
//     after every remediation. Their PDF/A-1b and PDF/A-2b remediations,
//     default and aggressive, now pass the name limit for litho and veraPDF
//     1.30.
//   - pdfjs bug1778692.pdf: stores 6 over-long names in an object stream,
//     which the object walk did not reach, and its PDF/A-2b remediations now
//     pass the name limit for both validators.
//   - All corpora: of the 29 files holding a name this repair rewrites,
//     remediated at PDF/A-1b, PDF/A-2b and PDF/A-4, default and aggressive,
//     no other finding moves for litho or veraPDF, and the extracted text,
//     the images and the inline image data of the outputs do not change.
func fixNameUTF8(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, violations []compliance.Violation, _ string) ([]string, error) {
	scan, err := scanObjects(ctx, alloc, doc)
	if err != nil {
		return nil, fmt.Errorf("collecting resource dictionary keys: %w", err)
	}
	plan := &contentNamePlan{spelling: map[string]ast.PdfName{}}
	if slices.ContainsFunc(violations, isNameLengthViolation) {
		if plan, err = planContentNames(ctx, alloc, doc, scan); err != nil {
			return nil, fmt.Errorf("planning the rename of over-long names: %w", err)
		}
	}

	var snap writer.Snapshot
	if len(plan.keys) > 0 {
		snap = alloc.Snapshot()
	}
	var tally contentRenameTally
	var repair *nameRepair
	for attempt := 1; ; attempt++ {
		var refused map[string]bool
		tally, refused, err = renameContentStreams(ctx, alloc, doc, plan, scan.pinned)
		if err != nil {
			return nil, err
		}
		repair = &nameRepair{pinned: scan.pinned, spelling: plan.spelling, refused: refused}
		if err := repair.walk(ctx, alloc, doc); err != nil {
			return nil, err
		}
		if len(repair.refused) == 0 {
			break
		}
		alloc.Restore(snap)
		if attempt == maxRenameAttempts {
			// The round before withdrew every rename of a resource key,
			// which is all a place can refuse, so no round reaches here.
			// Should one, the document stays as it was rather than keep a
			// rename some place refused.
			doc.Logger.Debug("remediation: over-long name renames did not settle, leaving every name in place",
				slog.Int("rounds", attempt))
			return nil, nil
		}
		plan.withdraw(repair.refused, attempt+1 == maxRenameAttempts)
	}
	logDeclinedNames(doc, scan.pinned, plan.spelling)

	var fixes []string
	if repair.renamedKeys > 0 {
		fixes = append(fixes, fmt.Sprintf("renamed %d over-long resource name(s) in their resource dictionaries and at %d place(s) content streams write them",
			len(plan.keys), tally.keyUses))
	}
	if tally.otherUses > 0 {
		fixes = append(fixes, fmt.Sprintf("shortened %d over-long name(s) in content streams", tally.otherUses))
	}
	if repair.appearances > 0 {
		fixes = append(fixes, fmt.Sprintf("renamed over-long resource names in %d default-appearance string(s)", repair.appearances))
	}
	if repair.objects > 0 {
		fixes = append(fixes, fmt.Sprintf("sanitised non-UTF-8 names in %d object(s)", repair.objects))
	}
	return fixes, nil
}

// logDeclinedNames reports at Debug the over-long resource keys this fixer
// leaves in place, so a skipped repair is traceable rather than silent.
func logDeclinedNames(doc *document.Document, pinned map[string]bool, spelling map[string]ast.PdfName) {
	declined := 0
	for name := range pinned {
		if _, renamed := spelling[name]; overLongName(name) && !renamed {
			declined++
		}
	}
	if declined == 0 {
		return
	}
	doc.Logger.Debug("remediation: leaving over-long resource dictionary keys in place, some place that writes them cannot be rewritten",
		slog.Int("names", declined))
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

// nameRepair is one object walk of [fixNameUTF8]: it renames the names
// spelling holds, and sanitises every other name that needs repair but the
// pinned resource keys.
type nameRepair struct {
	// pinned holds every resource key of the document ([objectScan]).
	pinned map[string]bool
	// spelling maps the over-long names to rename to their new spelling
	// ([contentNamePlan]).
	spelling map[string]ast.PdfName
	// refused holds the resource keys some place could not take the rename
	// of.
	refused map[string]bool
	// touched holds the resource keys renamed in the object being walked,
	// which it refuses when the object cannot be written back.
	touched map[string]bool
	// sanitised reports that the object being walked has a name repaired
	// by [sanitizeName] rather than renamed by spelling.
	sanitised bool
	// objects counts the objects rewritten with a name [sanitizeName]
	// repaired, appearances the /DA strings rewritten, and renamedKeys the
	// resource keys renamed.
	objects, appearances, renamedKeys int
}

// walk offers every object [everyObject] reads to the repair and writes back
// the ones it changes. A stream keeps its body as read.
func (r *nameRepair) walk(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) error {
	if r.refused == nil {
		r.refused = make(map[string]bool)
	}
	write := func(objNum int, obj ast.PdfObject) {
		if alloc.AllocAt(objNum, obj) && r.sanitised {
			r.objects++
		}
	}
	return everyObject(ctx, alloc, doc, func(objNum int, obj ast.PdfObject) {
		r.touched = make(map[string]bool)
		r.sanitised = false
		switch v := obj.(type) {
		case ast.PdfDict:
			if patched, changed := r.dict(ctx, v); changed {
				write(objNum, patched)
			}
		case ast.PdfArray:
			if patched, changed := r.array(ctx, v); changed {
				write(objNum, patched)
			}
		case ast.PdfStream:
			patched, changed := r.dict(ctx, v.Dict)
			if !changed {
				return
			}
			raw, err := v.Bytes(ctx)
			if err != nil {
				// Reading the raw stream body failed (cancelled context,
				// decryption error, I/O fault). Leaving the original object
				// in place is safer than allocating a new stream with an
				// empty body and silently dropping the payload, and the
				// resource keys it keeps cannot be renamed elsewhere.
				doc.Logger.Debug("remediation: skipping stream rewrite, body read failed",
					slog.Int("obj", objNum), slog.Any("err", err))
				maps.Copy(r.refused, r.touched)
				return
			}
			write(objNum, ast.NewStream(patched, raw))
		}
	})
}

// needsFix reports whether a name is one the repair rewrites: a name spelling
// renames, or one with invalid UTF-8 bytes or past PDF/A's 127-byte cap that
// is no resource key.
func (r *nameRepair) needsFix(s string) bool {
	if _, ok := r.spelling[s]; ok {
		return true
	}
	if r.pinned[s] {
		return false
	}
	return !utf8.ValidString(s) || len(s) > pdfANameMaxLen
}

// respell returns the name a value that needs repair is written as: its
// spelling for the whole document, or [sanitizeName]'s repair of it.
func (r *nameRepair) respell(s string) ast.PdfName {
	if spelled, ok := r.spelling[s]; ok {
		return spelled
	}
	r.sanitised = true
	return ast.PdfName(sanitizeName(s))
}

// dict walks a PdfDict and replaces every PdfName that needs repair, leaving
// the pinned resource keys spelling does not rename untouched. Keys are
// visited in sorted order: truncation can map two distinct over-long keys onto
// the same 127-byte prefix, and the disambiguating suffix must not depend on
// Go's randomized map iteration order. A /DA default-appearance string has the
// names spelling renames respelled in it ([nameRepair.defaultAppearance]).
func (r *nameRepair) dict(ctx context.Context, dict ast.PdfDict) (ast.PdfDict, bool) {
	newDict := make(ast.PdfDict, len(dict))
	changed := false

	keys := slices.Sorted(maps.Keys(dict))
	taken := make(map[ast.PdfName]bool, len(dict))
	for _, k := range keys {
		if !r.needsFix(string(k)) {
			taken[k] = true
		}
	}

	for _, k := range keys {
		newKey := r.key(k, taken)
		changed = changed || newKey != k
		taken[newKey] = true

		switch val := dict[k].(type) {
		case ast.PdfName:
			if r.needsFix(string(val)) {
				newDict[newKey] = r.respell(string(val))
				changed = true
				continue
			}
		case ast.PdfString:
			if k == "DA" {
				if s, c := r.defaultAppearance(ctx, val); c {
					newDict[newKey] = s
					changed = true
					continue
				}
			}
		case ast.PdfArray:
			if sub, c := r.array(ctx, val); c {
				newDict[newKey] = sub
				changed = true
				continue
			}
		case ast.PdfDict:
			if sub, c := r.dict(ctx, val); c {
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

// key returns the key k is written as in a dictionary whose keys taken holds
// so far.
//
// A key spelling renames takes its spelling, which the plan chose clear of
// every resource key. A dictionary that already holds that spelling as a key
// refuses the rename of a resource key and keeps the key, since any other
// spelling would part it from the content streams that write it. Any other
// name falls back to a disambiguated [sanitizeName] repair, or keeps its
// spelling when [uniqueName] finds no free one.
func (r *nameRepair) key(k ast.PdfName, taken map[ast.PdfName]bool) ast.PdfName {
	if !r.needsFix(string(k)) {
		return k
	}
	if spelled, ok := r.spelling[string(k)]; ok {
		if !taken[spelled] {
			if r.pinned[string(k)] {
				r.touched[string(k)] = true
				r.renamedKeys++
			}
			return spelled
		}
		if r.pinned[string(k)] {
			r.refused[string(k)] = true
			return k
		}
	}
	if cand, ok := uniqueName(sanitizeName(string(k)), taken); ok {
		r.sanitised = true
		return cand
	}
	return k
}

// array behaves like [nameRepair.dict] but for arrays, which carry no keys
// and so need no collision handling.
func (r *nameRepair) array(ctx context.Context, arr ast.PdfArray) (ast.PdfArray, bool) {
	newArr := make(ast.PdfArray, len(arr))
	changed := false
	for i, v := range arr {
		switch val := v.(type) {
		case ast.PdfName:
			if r.needsFix(string(val)) {
				newArr[i] = r.respell(string(val))
				changed = true
				continue
			}
		case ast.PdfArray:
			if sub, c := r.array(ctx, val); c {
				newArr[i] = sub
				changed = true
				continue
			}
		case ast.PdfDict:
			if sub, c := r.dict(ctx, val); c {
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

// defaultAppearance returns a /DA default-appearance string with every name
// spelling renames written in its new spelling. A /DA string is
// content-stream syntax (ISO 32000-1, 12.7.3.3), so the names are spliced as
// in a content stream ([nameSplices]). A resource key the splice cannot reach
// is refused.
func (r *nameRepair) defaultAppearance(ctx context.Context, s ast.PdfString) (ast.PdfString, bool) {
	if len(r.spelling) == 0 {
		return "", false
	}
	splices, missed, _ := nameSplices(ctx, []byte(s), func(name string) (ast.PdfName, bool) {
		spelled, ok := r.spelling[name]
		return spelled, ok
	})
	for name := range missed {
		if r.pinned[name] {
			r.refused[name] = true
		}
	}
	if len(splices) == 0 {
		return "", false
	}
	r.appearances++
	return ast.PdfString(applyTokenSplices([]byte(s), splices)), true
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
