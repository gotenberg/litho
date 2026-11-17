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
	"github.com/gotenberg/litho/pkg/internal/typography/encodings"
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
	shown := &shownCodes{alloc: alloc, doc: doc}
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
			if dropSymbolicEncoding(ctx, alloc, doc, objNum, fontDict, shown) {
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
// Only where no glyph changes. ISO 32000-1, 9.6.6.4 reads a code of a font
// with a base encoding through its glyph name and the (3,1) or (1,0) subtable,
// and a symbolic font with none through the code itself and the (3,0) or (1,0)
// subtable; readers differ on which a symbolic font with an encoding takes.
// When the program's only cmap subtable is the Microsoft Symbol (3,0) one, both
// readings end there. When it also carries a (3,1) or (1,0) subtable, the
// encoding goes only if, for every code the document shows with the font, the
// glyph name's route selects nothing or the glyph the code's route selects
// ([codeRoutesAgree]). A font the interactive form's default resources name
// can be typed with any code, so all 256 must agree.
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
//   - litho symbolenc_2b.pdf: S1 and S2 lose their base encoding, S3, whose
//     (3,1) subtable selects the glyphs its (3,0) one does, S4, whose (1,0)
//     subtable does on the codes it shows, lose it too, and S5, whose (1,0)
//     subtable selects another glyph for a code it shows, keeps it; the
//     outputs at the manifest's eight profiles, default and aggressive,
//     report the rule set veraPDF 1.30.3 reports, Poppler draws them as it
//     draws the source, and litho extracts the same text.
//   - govdocs thread_000_000594.pdf and 13 others: two to four symbolic
//     TrueType fonts each, under /WinAnsiEncoding over a (3,0)-only program.
//     Their PDF/A-2b outputs, which kept 6.2.11.6 t3 alone on veraPDF 1.30.3,
//     pass it, and Poppler draws their 235 pages and extracts their text as it
//     does from the outputs that kept the encoding.
func dropSymbolicEncoding(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, objNum int, fontDict ast.PdfDict, shown *shownCodes) bool {
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
	if err != nil || !slices.Contains(subs, truetype.CmapSubtable{PlatformID: 3, EncodingID: 0}) {
		return false
	}
	if slices.Contains(subs, truetype.CmapSubtable{PlatformID: 3, EncodingID: 1}) ||
		slices.Contains(subs, truetype.CmapSubtable{PlatformID: 1, EncodingID: 0}) {
		codes, all := shown.of(ctx, objNum)
		if !codeRoutesAgree(ctx, doc, fontDict, program, codes, all) {
			return false
		}
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

// shownCodes reads, on first use, the codes each font of the document shows
// ([collectFontUses]) and the fonts the interactive form's default resources
// name ([formFontNumbers]), for [dropSymbolicEncoding].
type shownCodes struct {
	alloc  *writer.ObjAllocator
	doc    *document.Document
	loaded bool
	// paged reports a page tree the walk read; without one, no code a font
	// shows is known.
	paged bool
	codes map[int]map[byte]bool
	form  map[int]bool
}

// of returns the codes the font objNum holds is shown with, and whether every
// code must be taken as shown instead: for a font the form names, or when the
// document has no page tree to read.
func (s *shownCodes) of(ctx context.Context, objNum int) (map[byte]bool, bool) {
	if !s.loaded {
		s.loaded = true
		s.form = formFontNumbers(ctx, s.alloc, s.doc)
		if catalog, err := document.ResolveCatalog(ctx, s.doc); err == nil {
			if pages, err := document.ResolvePages(ctx, s.doc, catalog); err == nil {
				s.paged = true
				s.codes = collectFontUses(ctx, s.alloc, s.doc, pages, pageTreeEntries(ctx, s.alloc, s.doc, pages)).codes
			}
		}
	}
	return s.codes[objNum], !s.paged || s.form[objNum]
}

// codeRoutesAgree reports whether each of codes, or each of the 256 when all
// is set, selects the same glyph of the TrueType program through either route
// ISO 32000-1, 9.6.6.4 gives a simple font, so a reader draws it the same with
// the font's base encoding and without it.
//
// The code's route looks the code up in the (3,0) subtable in the ranges
// 0x0000, 0xF000, 0xF100 and 0xF200, then in the (1,0) subtable. The glyph
// name's route reads the name the font's encoding gives the code, base
// encoding and /Differences, and looks up its character in the (3,1)
// subtable, then its Mac OS Roman code in the (1,0) subtable. A name route
// that selects nothing agrees: a reader finding nothing there takes the code's
// route, as the renderer and PDF.js do.
func codeRoutesAgree(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, program []byte, codes map[byte]bool, all bool) bool {
	lookup := func(platform, encoding uint16) map[uint32]uint16 {
		m, err := truetype.SubtableCodeToGID(ctx, program, truetype.CmapSubtable{PlatformID: platform, EncodingID: encoding})
		if err != nil {
			return nil
		}
		return m
	}
	symbol, unicode, roman := lookup(3, 0), lookup(3, 1), lookup(1, 0)

	names := maps.Clone(compliance.PredefinedEncodingNames(string(baseEncodingName(ctx, doc, fontDict["Encoding"]))))
	if names == nil {
		names = make(map[byte]string)
	}
	maps.Copy(names, compliance.DifferencesGlyphNames(ctx, doc, fontDict))
	macCodes := make(map[string]byte)
	for code, name := range compliance.PredefinedEncodingNames("MacRomanEncoding") {
		macCodes[name] = code
	}

	byCode := func(b byte) uint16 {
		for _, hi := range [...]uint32{0x0000, 0xF000, 0xF100, 0xF200} {
			if gid := symbol[hi|uint32(b)]; gid != 0 {
				return gid
			}
		}
		return roman[uint32(b)]
	}
	byName := func(b byte) uint16 {
		name := names[b]
		if name == "" {
			return 0
		}
		if r, ok := encodings.GlyphToRuneFull(name); ok && r != 0 {
			if gid := unicode[uint32(r)]; gid != 0 { //nolint:gosec // a glyph name's rune is a Unicode scalar value.
				return gid
			}
		}
		if mc, ok := macCodes[name]; ok {
			return roman[uint32(mc)]
		}
		return 0
	}
	agree := func(b byte) bool {
		gid := byName(b)
		return gid == 0 || gid == byCode(b)
	}
	if all {
		for b := range 256 {
			if !agree(byte(b)) {
				return false
			}
		}
		return true
	}
	for b := range codes {
		if !agree(b) {
			return false
		}
	}
	return true
}

// baseEncodingName returns the predefined encoding enc names, directly or as
// the /BaseEncoding of its dictionary, "" when it names none.
func baseEncodingName(ctx context.Context, doc *document.Document, enc ast.PdfObject) ast.PdfName {
	resolved, err := document.ResolveObject(ctx, doc, enc)
	if err != nil {
		return ""
	}
	switch v := resolved.(type) {
	case ast.PdfName:
		return v
	case ast.PdfDict:
		name, _ := document.ResolveName(ctx, doc, v["BaseEncoding"])
		return name
	}
	return ""
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
