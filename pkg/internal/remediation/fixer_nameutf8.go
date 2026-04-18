package remediation

import (
	"context"
	"fmt"
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

// pdfANameMaxLen is the 127-byte cap on PDF names enforced by PDF/A-1
// §6.1.12 test 4 and PDF/A-2/4 §6.1.13 test 4.
const pdfANameMaxLen = 127

// fixNameUTF8 rewrites every indirect object's PdfName values to
// strip bytes that are not valid UTF-8. PDF/A-4 §6.1.7 requires names
// to decode as UTF-8 after hash-escape expansion; names written with
// raw high bytes violate this. Replacement uses the Unicode
// REPLACEMENT CHARACTER so the new name stays visible and distinct.
func fixNameUTF8(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	count, err := walkIndirectObjects(ctx, alloc, doc, sanitizeDictNames, sanitizeArrayNames)
	if err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, nil
	}
	return []string{fmt.Sprintf("sanitised non-UTF-8 names in %d object(s)", count)}, nil
}

// sanitizeDictNames walks a PdfDict and replaces every PdfName that
// contains invalid UTF-8 bytes.
//
//nolint:dupl // Dict and array walkers operate on distinct types.
func sanitizeDictNames(dict ast.PdfDict) (ast.PdfDict, bool) {
	newDict := make(ast.PdfDict, len(dict))
	changed := false
	for k, v := range dict {
		newKey := k
		if nameNeedsFix(string(k)) {
			newKey = ast.PdfName(sanitizeName(string(k)))
			changed = true
		}
		switch val := v.(type) {
		case ast.PdfName:
			if nameNeedsFix(string(val)) {
				newDict[newKey] = ast.PdfName(sanitizeName(string(val)))
				changed = true
				continue
			}
		case ast.PdfArray:
			if sub, c := sanitizeArrayNames(val); c {
				newDict[newKey] = sub
				changed = true
				continue
			}
		case ast.PdfDict:
			if sub, c := sanitizeDictNames(val); c {
				newDict[newKey] = sub
				changed = true
				continue
			}
		}
		newDict[newKey] = v
	}
	if !changed {
		return nil, false
	}
	return newDict, true
}

// sanitizeArrayNames behaves like sanitizeDictNames but for arrays.
//
//nolint:dupl // See sanitizeDictNames.
func sanitizeArrayNames(arr ast.PdfArray) (ast.PdfArray, bool) {
	newArr := make(ast.PdfArray, len(arr))
	changed := false
	for i, v := range arr {
		switch val := v.(type) {
		case ast.PdfName:
			if nameNeedsFix(string(val)) {
				newArr[i] = ast.PdfName(sanitizeName(string(val)))
				changed = true
				continue
			}
		case ast.PdfArray:
			if sub, c := sanitizeArrayNames(val); c {
				newArr[i] = sub
				changed = true
				continue
			}
		case ast.PdfDict:
			if sub, c := sanitizeDictNames(val); c {
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

// nameNeedsFix reports whether a PDF name has either invalid UTF-8
// bytes or exceeds PDF/A's 127-byte length cap.
func nameNeedsFix(s string) bool {
	return !utf8.ValidString(s) || len(s) > pdfANameMaxLen
}

// sanitizeName strips bytes outside valid UTF-8 and truncates the name
// at [pdfANameMaxLen] bytes. PDF/A-4 §6.1.7 requires UTF-8 compliance;
// PDF/A-1 §6.1.12 test 4 caps names at 127 bytes.
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
	if len(out) > pdfANameMaxLen {
		out = out[:pdfANameMaxLen]
	}
	return string(out)
}
