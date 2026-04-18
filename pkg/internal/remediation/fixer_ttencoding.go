package remediation

import (
	"context"
	"fmt"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// matchTrueTypeEncoding matches the family of non-symbolic TrueType
// /Encoding violations: ISO 19005-1 §6.3.7, ISO 19005-2/3 §6.2.11.6,
// ISO 19005-4 §6.2.10.6.
func matchTrueTypeEncoding(v compliance.Violation) bool {
	if !clausePrefix(v, "6.2.10.6", "6.2.11.6", "6.3.7") {
		return false
	}
	return contextContains(v, "BaseEncoding") ||
		contextContains(v, "TrueType") ||
		descriptionContains(v, "TrueType")
}

// fixTrueTypeEncoding scans every Font object whose /Subtype is
// TrueType and normalises its /Encoding/BaseEncoding to
// /WinAnsiEncoding when the current value is absent or non-standard.
// WinAnsiEncoding is the PDF/A-preferred default for non-symbolic
// TrueType fonts.
func fixTrueTypeEncoding(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, opts Options, _ []compliance.Violation, _ string) ([]string, error) {
	// Switching /Encoding to WinAnsi remaps character codes whose glyph
	// names differ between Custom and WinAnsi; the rendered text can
	// change. Gate behind PreserveContent.
	if opts.PreserveContent {
		return nil, nil
	}
	valid := map[ast.PdfName]bool{
		"MacRomanEncoding":  true,
		"WinAnsiEncoding":   true,
		"MacExpertEncoding": true,
	}

	count := 0
	for objNum, entry := range doc.Index.Entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
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
		subtype, _ := fontDict["Subtype"].(ast.PdfName)
		if subtype != "TrueType" {
			continue
		}

		encObj := fontDict["Encoding"]
		switch enc := encObj.(type) {
		case nil:
			// Missing /Encoding (or explicit null). Force WinAnsi so
			// non-symbolic TrueType fonts meet PDF/A encoding rules.
			newFont := make(ast.PdfDict, len(fontDict)+1)
			maps.Copy(newFont, fontDict)
			newFont["Encoding"] = ast.PdfName("WinAnsiEncoding")
			alloc.AllocAt(objNum, newFont)
			count++
		case ast.PdfName:
			if valid[enc] {
				continue
			}
			newFont := make(ast.PdfDict, len(fontDict))
			maps.Copy(newFont, fontDict)
			newFont["Encoding"] = ast.PdfName("WinAnsiEncoding")
			alloc.AllocAt(objNum, newFont)
			count++
		case ast.PdfDict:
			patched, changed := patchEncodingDict(enc, valid)
			if !changed {
				continue
			}
			newFont := make(ast.PdfDict, len(fontDict))
			maps.Copy(newFont, fontDict)
			newFont["Encoding"] = patched
			alloc.AllocAt(objNum, newFont)
			count++
		case ast.PdfIndirectRef:
			encDict, err := document.ResolveDict(ctx, doc, enc)
			if err != nil || encDict == nil {
				continue
			}
			patched, changed := patchEncodingDict(encDict, valid)
			if !changed {
				continue
			}
			alloc.AllocAt(enc.ObjectNum, patched)
			count++
		}
	}
	if count == 0 {
		return nil, nil
	}
	return []string{fmt.Sprintf("normalised TrueType /Encoding on %d font(s)", count)}, nil
}

// patchEncodingDict sets /BaseEncoding to WinAnsiEncoding when it is
// absent or set to a non-standard name. Returns a new dict and a flag
// indicating change.
func patchEncodingDict(enc ast.PdfDict, valid map[ast.PdfName]bool) (ast.PdfDict, bool) {
	base, hasBase := enc["BaseEncoding"].(ast.PdfName)
	if hasBase && valid[base] {
		return nil, false
	}
	newEnc := make(ast.PdfDict, len(enc)+1)
	maps.Copy(newEnc, enc)
	newEnc["BaseEncoding"] = ast.PdfName("WinAnsiEncoding")
	return newEnc, true
}
