package remediation

import (
	"context"
	"fmt"
	"maps"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// matchTrueTypeEncoding matches the family of non-symbolic TrueType
// /Encoding violations: ISO 19005-1 §6.3.7, ISO 19005-2/3 §6.2.11.6,
// ISO 19005-4 §6.2.10.6, and the PDF/UA restatements of the same
// requirement, ISO 14289-1 §7.21.6 and ISO 14289-2 §8.4.5.7.
func matchTrueTypeEncoding(v compliance.Violation) bool {
	if !clausePrefix(v, "6.2.10.6", "6.2.11.6", "6.3.7", "7.21.6", "8.4.5.7") {
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
	for objNum, entry := range doc.XRefEntries() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if entry.Type != 1 || objNum == 0 {
			continue
		}
		// Alloc-aware read. The registry drives every fixer against one
		// allocator and never flushes between them, and the font fixers ahead
		// of this one (cid-gidmap, font-keys, tounicode-ranges, fonts,
		// font-widths) rewrite the same object. Rebuilding the dictionary from
		// the on-disk copy discards whatever they left pending, and the
		// fix-to-fixpoint loop then repairs the damage on the next round, so
		// the only symptom is a fix reported twice.
		ref := ast.PdfIndirectRef{ObjectNum: objNum, GenNum: entry.Generation}
		fontDict := resolveFromAllocOrDoc(ctx, alloc, doc, ref)
		if fontDict == nil {
			continue
		}
		if typ, _ := fontDict["Type"].(ast.PdfName); typ != "Font" {
			continue
		}
		subtype, _ := fontDict["Subtype"].(ast.PdfName)
		if subtype != "TrueType" {
			continue
		}
		// A symbolic TrueType font resolves codes through its own built-in
		// encoding, and ISO 19005-2, 6.2.11.6 t3 (ISO 19005-1, 6.3.7 t2, and
		// ISO 19005-4, 6.2.10.6 t3) forbid it an /Encoding entry outright.
		// Normalising one onto it would introduce the very violation this
		// package exists to remove.
		if keepsBuiltInEncoding(ctx, doc, fontDict) {
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
			// Alloc-aware for the same reason as the font dictionary above:
			// this writes the encoding object back, so it must not rebuild it
			// from a stale copy.
			encDict := resolveFromAllocOrDoc(ctx, alloc, doc, enc)
			if encDict == nil {
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

// symbolicFontFlag is bit 3 of the font descriptor /Flags, which per ISO
// 32000-1, Table 123 marks a font whose character set lies outside the
// Adobe standard Latin set.
const symbolicFontFlag = 1 << 2

// keepsBuiltInEncoding reports whether a TrueType font resolves character codes
// through an encoding of its own, so writing a standard /Encoding onto it would
// change what its text says rather than only how it validates.
func keepsBuiltInEncoding(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) bool {
	// Symbol and ZapfDingbats define their own encodings (ISO 32000-1,
	// 9.6.2.2 and Annex D.5/D.6) and a PDF may reference either without a
	// descriptor at all. Code 0x61 is alpha in Symbol and 0xB7 is bullet;
	// under WinAnsi the same codes read as "a" and middledot, so a chemistry
	// paper's "α-keto-glutarate" silently became "a-keto-glutarate".
	if base, _ := document.ResolveName(ctx, doc, fontDict["BaseFont"]); hasBuiltInEncoding(string(base)) {
		return true
	}
	// A font with no readable descriptor cannot be established as symbolic
	// and keeps the non-symbolic treatment, as
	// TestFixTrueTypeEncoding_NoDescriptorIsTreatedNonSymbolic records.
	descriptor, err := document.ResolveDict(ctx, doc, fontDict["FontDescriptor"])
	if err != nil || descriptor == nil {
		return false
	}
	flags, err := document.ResolveInt(ctx, doc, descriptor["Flags"])
	if err != nil {
		return false
	}
	return flags&symbolicFontFlag != 0
}

// hasBuiltInEncoding reports whether a /BaseFont names one of the standard
// fonts that carry their own encoding, ignoring any subset prefix
// ("ABCDEF+Symbol").
func hasBuiltInEncoding(baseFont string) bool {
	if len(baseFont) > 7 && baseFont[6] == '+' {
		baseFont = baseFont[7:]
	}
	if i := strings.IndexByte(baseFont, ','); i >= 0 {
		baseFont = baseFont[:i]
	}
	return baseFont == "Symbol" || baseFont == "ZapfDingbats"
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
