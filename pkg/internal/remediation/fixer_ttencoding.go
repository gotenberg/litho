package remediation

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/typography"
	"github.com/gotenberg/litho/pkg/internal/typography/truetype"
)

// matchTrueTypeEncoding matches the family of TrueType /Encoding violations: ISO 19005-1 §6.3.7, ISO 19005-2/3 §6.2.11.6,
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
// TrueType fonts. A symbolic one loses the encoding the clauses forbid it
// where that changes no glyph ([dropSymbolicEncoding]).
//
// Fonts stored in an object stream (ISO 32000-1, 7.5.7) are repaired too, for
// the reason [walkNumberedObjects] gives.
//
// # Corpus evidence
//
//   - verapdf PDF_UA-1__7.21.6-t02-fail-a.pdf, PDF_UA-1__7.21.6-t02-fail-b.pdf,
//     PDF_UA-1__7.21.6-t02-fail-c.pdf: their aggressive PDF/A-2b remediations
//     left 6.2.11.6 t2 standing on a TrueType font stored in an object stream.
func fixTrueTypeEncoding(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, opts Options, _ []compliance.Violation, _ string) ([]string, error) {
	valid := map[ast.PdfName]bool{
		"MacRomanEncoding":  true,
		"WinAnsiEncoding":   true,
		"MacExpertEncoding": true,
	}

	count, dropped := 0, 0
	for objNum, entry := range sweptObjects(doc) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if entry.Free || objNum == 0 {
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
			if dropSymbolicEncoding(ctx, alloc, doc, objNum, fontDict) {
				dropped++
			}
			continue
		}
		// Switching /Encoding to WinAnsi remaps character codes whose glyph
		// names differ between Custom and WinAnsi; the rendered text can
		// change. Gate behind PreserveContent.
		if opts.PreserveContent {
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
	var fixed []string
	if count > 0 {
		fixed = append(fixed, fmt.Sprintf("normalised TrueType /Encoding on %d font(s)", count))
	}
	if dropped > 0 {
		fixed = append(fixed, fmt.Sprintf("removed the /Encoding of %d symbolic TrueType font(s)", dropped))
	}
	return fixed, nil
}

// dropSymbolicEncoding removes the base encoding of the symbolic TrueType font
// objNum holds, which ISO 19005-2 and -3, 6.2.11.6 t3 (ISO 19005-1, 6.3.7 t2;
// ISO 19005-4, 6.2.10.6 t3; ISO 14289-1, 7.21.6 t3; ISO 14289-2, 8.4.5.7 t3)
// forbid it, and reports whether it did. A named /Encoding goes; a dictionary
// loses its /BaseEncoding and keeps its /Differences, which the clauses allow
// ([compliance.SymbolicEncodingNamed] gives the reading).
//
// Only where no glyph changes: when the embedded program's only cmap subtable
// a reader can look a code up in is the Microsoft Symbol (3,0) one. ISO
// 32000-1, 9.6.6.4 reads a code of a font with a base encoding through its
// glyph name and the (3,1) or (1,0) subtable, and a symbolic font with none
// through the (3,0) one; with neither (3,1) nor (1,0) in the program, both
// readings end at the (3,0) subtable. A program that carries either may draw a
// code another glyph once the encoding goes, and keeps the residual.
//
// The encoding is rewritten on the font, never in a shared encoding object,
// which other fonts may name.
//
// The encoding also named what each code reads as, and text extraction reads
// a symbolic font without one through its program's glyph names instead
// (ISO 32000-1, 9.10.2). A font with no /ToUnicode receives one holding the
// reading its encoding gave, so the repair changes what the file reads no more
// than what it draws. A font whose reading litho cannot resolve keeps the
// residual.
//
// # Corpus evidence
//
//   - litho symbolenc_2b.pdf: S1 and S2 lose their base encoding and S3,
//     whose program also carries a (3,1) subtable, keeps it; the outputs at
//     the manifest's eight profiles, default and aggressive, report the rule
//     set veraPDF 1.30.3 reports, Poppler draws them as it draws the source,
//     and litho extracts the same text.
//   - govdocs thread_000_000594.pdf and 13 others: two to four symbolic
//     TrueType fonts each, under /WinAnsiEncoding over a (3,0)-only program.
//     Their PDF/A-2b outputs, which kept 6.2.11.6 t3 alone on veraPDF 1.30.3,
//     pass it, and Poppler draws their 235 pages and extracts their text as it
//     does from the outputs that kept the encoding.
func dropSymbolicEncoding(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, objNum int, fontDict ast.PdfDict) bool {
	if !compliance.SymbolicEncodingNamed(ctx, doc, fontDict["Encoding"]) {
		return false
	}
	descriptor, err := document.ResolveDict(ctx, doc, fontDict["FontDescriptor"])
	if err != nil || descriptor == nil {
		return false
	}
	flags, err := document.ResolveInt(ctx, doc, descriptor["Flags"])
	if err != nil || flags&symbolicFontFlag == 0 {
		return false
	}
	stream, err := document.ResolveStream(ctx, doc, descriptor["FontFile2"])
	if err != nil {
		return false
	}
	program, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		return false
	}
	subs, err := truetype.CmapSubtables(program)
	if err != nil ||
		!slices.Contains(subs, truetype.CmapSubtable{PlatformID: 3, EncodingID: 0}) ||
		slices.Contains(subs, truetype.CmapSubtable{PlatformID: 3, EncodingID: 1}) ||
		slices.Contains(subs, truetype.CmapSubtable{PlatformID: 1, EncodingID: 0}) {
		return false
	}

	newFont := make(ast.PdfDict, len(fontDict)+1)
	maps.Copy(newFont, fontDict)
	delete(newFont, "Encoding")
	if _, mapped := fontDict["ToUnicode"]; !mapped {
		font, err := typography.ResolveFont(ctx, doc, fontDict)
		if err != nil || font == nil {
			return false
		}
		mapping := make(map[uint16]rune, len(font.Encoding))
		for code, r := range font.Encoding {
			if isValidUnicodeMapping(r) {
				mapping[uint16(code)] = r
			}
		}
		if data := generateToUnicodeCMap(mapping, 1); data != nil {
			newFont["ToUnicode"] = alloc.Alloc(ast.NewStream(ast.PdfDict{}, data))
		}
	}
	// SymbolicEncodingNamed holds only for a name, which goes whole, or a
	// dictionary with a /BaseEncoding, which keeps the rest of its entries.
	if enc, err := document.ResolveDict(ctx, doc, fontDict["Encoding"]); err == nil && enc != nil {
		rest := make(ast.PdfDict, len(enc))
		maps.Copy(rest, enc)
		delete(rest, "BaseEncoding")
		if _, ok := rest["Differences"]; ok {
			newFont["Encoding"] = rest
		}
	}
	alloc.AllocAt(objNum, newFont)
	return true
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
