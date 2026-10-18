package compliance

import (
	"context"
	"maps"
	"math"
	"slices"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/typography"
	"github.com/gotenberg/litho/pkg/internal/typography/cff"
	"github.com/gotenberg/litho/pkg/internal/typography/cmap"
	"github.com/gotenberg/litho/pkg/internal/typography/encodings"
	"github.com/gotenberg/litho/pkg/internal/typography/truetype"
)

// WidthMismatchContext is the wording every glyph-width failure carries in
// its Violation Context.
//
// It is exported because remediation's matchFontWidth claims a width
// violation on this substring alone, carrying no rule identity at all, and
// every emit site across this file and fonts.go must not drift from it.
// Same reasoning as [PDFUA2ExpectedRev]: one symbol, so the checker and the
// fixer cannot disagree about the wording.
const WidthMismatchContext = "width mismatch"

// widthTolerance is the maximum allowed difference between PDF-declared
// and binary font widths, in 1/1000 text space units.
const widthTolerance = 1.0

// widthEpsilon absorbs the rounding a width computation carries before the
// comparison against [widthTolerance].
//
// The program advance reaches the comparison through a scale read from the
// font: a Type 1 /FontMatrix of 0.001 parses to 0.0010000000000000000208, so an
// advance of 334 against a declared 333 yields a difference of
// 1.0000000000000191 rather than 1. The rules admit a difference of one unit,
// ISO 19005-2, 6.2.11.5 and ISO 14289-2, 8.4.5.6 among them, and veraPDF writes
// its test as abs(...) <= 1, which such a font passes. Comparing strictly
// against 1.0 turns the last bits of a binary fraction into a violation. A
// millionth of a text-space unit is far below any metric a font records, so
// the allowance cannot mask a real mismatch.
//
// # Corpus evidence
//
//   - pdfjs bug921760.pdf: the case the allowance was written for, a one-unit
//     difference the /FontMatrix scale carries past 1, and veraPDF's
//     abs(...) <= 1 test passes it.
const widthEpsilon = 1e-6

// widthsInconsistent reports whether a declared width and a program advance
// differ by more than the rules allow.
func widthsInconsistent(declared, program float64) bool {
	return math.Abs(declared-program) > widthTolerance+widthEpsilon
}

// checkGlyphWidths validates glyph width consistency (rule 5).
// Compares PDF-declared widths against the embedded font program's binary widths.
func checkGlyphWidths(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string, usage *fontUsageInfo) []Violation {
	rule := glyphWidthRule(profile)
	if rule.Specification == "" {
		return nil
	}

	// Type3 fonts: compare /Widths against CharProc d0/d1 widths.
	if isType3Font(ctx, doc, fontDict) {
		return checkType3FontWidths(ctx, doc, fontDict, fontName, pageNum, rule, usage)
	}

	binaryWidths, unitsPerEm, charCodeToGID, ok := simpleFontGlyphMetrics(ctx, doc, fontDict)
	if !ok {
		// A classic Type1 (/FontFile) program carries advance widths in each
		// glyph's charstring rather than a GID-keyed hmtx/CFF table, so
		// [typography.ExtractBinaryWidths] yields nothing. Compare against those
		// charstring widths by glyph name instead.
		return checkType1FontWidths(ctx, doc, fontDict, fontName, pageNum, rule, usage)
	}

	subtype := fontSubtype(ctx, doc, fontDict)

	// Simple fonts (Type1, TrueType, MMType1): compare /Widths array using FirstChar offset.
	if subtype == "Type1" || subtype == "TrueType" || subtype == "MMType1" {
		return checkSimpleFontWidths(ctx, doc, fontDict, binaryWidths, unitsPerEm, fontName, pageNum, rule, charCodeToGID, usage)
	}

	return nil
}

// simpleFontGlyphMetrics extracts the embedded font program's per-GID advance
// widths, its units-per-em, and a char-code to GID map for a simple font.
// Returns ok=false when the font program is absent or unparseable. Shared by the
// width-consistency check and the [ExpectedSimpleFontWidths] repair helper so the
// two always agree on the authoritative widths.
func simpleFontGlyphMetrics(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) (map[uint16]float64, uint16, map[uint16]uint16, bool) {
	g, ok := resolveSimpleFontGlyphs(ctx, doc, fontDict)
	return g.widths, g.unitsPerEm, g.codeToGID, ok
}

// simpleFontGlyphs is what [resolveSimpleFontGlyphs] learns about a simple
// font's embedded program.
type simpleFontGlyphs struct {
	widths     map[uint16]float64
	unitsPerEm uint16
	codeToGID  map[uint16]uint16
	// symbolCmap is set when the codes resolve through a (3,0) or (1,0) cmap
	// subtable the program carries, so a code absent from codeToGID has no
	// glyph at all rather than an unknown one.
	symbolCmap bool
	// encodingNames holds the glyph names a non-symbolic TrueType font's
	// predefined encoding gives its codes, so a code it leaves unnamed is known
	// to select no glyph. It is nil whenever the names are not fully known.
	encodingNames map[byte]string
	// notdefNamed holds the codes a CFF program's encoding names .notdef. They
	// map to GID 0 in codeToGID for their width, but the charset holds that
	// glyph, so it is present.
	notdefNamed map[byte]bool
}

// The OpenType head table bounds unitsPerEm to 16..16384 ("Valid range is from
// 16 to 16384"), so a value outside it is a corrupt head, not a scale. Every
// width derived from one is meaningless, and scaling by it can push /Widths
// past the PDF/A-1 real-number ceiling of 32767 (ISO 19005-1, 6.1.12). Fall
// back to the same 1000 the unreadable-head paths already use.
//
// # Corpus evidence
//
//   - pdfjs issue9462.pdf: carries a font whose program yields a nonsense
//     unitsPerEm, and scaling by it produced /Widths near 1.5 million, which
//     then broke the PDF/A-1 real-number ceiling.
const (
	minUnitsPerEm     = 16
	maxUnitsPerEm     = 16384
	defaultUnitsPerEm = 1000
)

// headlessUnitsPerEm is the em veraPDF weighs a TrueType program's advances
// against when the program carries no 'head' table. The OpenType spec makes
// 'head' a required table and states unitsPerEm there alone, so such a program
// states no em and no value is mandated. veraPDF's TrueTypeHeadTable starts
// its unitsPerEm at 2048 and keeps it when there is no table to read, and
// litho takes the same em so the two agree on ISO 19005-2, 6.2.11.5 and its
// restatements.
// Measured with veraPDF 1.30.2 at PDF/A-2b on headless programs: advances of
// 1229 pass /Widths 600 and fail 1229, advances of 1000 pass 488 and fail
// 1000 and 490, advances of 2048 pass 1000, and a CIDFontType2 advancing 600
// passes /W 293 and fails 600. litho fontnohead_2b.pdf plants both kinds.
//
// # Corpus evidence
//
//   - All corpora: no TrueType program a font reads lacks 'head', so no cell
//     moves.
const headlessUnitsPerEm = 2048

// FontProgramUnitsPerEm returns the units per em the width rules scale an
// embedded font program's advances by: the program's own, read from 'head'
// or from a CFF /FontMatrix, and [defaultUnitsPerEm] for a value outside
// the range the OpenType head table allows. A TrueType-outline program with
// no 'head' takes [headlessUnitsPerEm]; an OpenType program with CFF outlines
// keeps the CFF scale it carries. Remediation scales the widths it writes by
// the same value, so the repair and the check agree.
func FontProgramUnitsPerEm(fontData []byte) uint16 {
	if isSfntFontProgram(fontData) && string(fontData[:4]) != "OTTO" && !sfntHasTable(fontData, "head") {
		return headlessUnitsPerEm
	}
	unitsPerEm := typography.ReadUnitsPerEm(fontData)
	if unitsPerEm < minUnitsPerEm || unitsPerEm > maxUnitsPerEm {
		return defaultUnitsPerEm
	}
	return unitsPerEm
}

// sfntHasTable reports whether an sfnt's table directory reads and lists tag.
func sfntHasTable(data []byte, tag string) bool {
	records, err := truetype.ParseTableDirectory(data)
	if err != nil {
		return false
	}
	return slices.ContainsFunc(records, func(r truetype.TableRecord) bool { return r.Tag == tag })
}

// resolveSimpleFontGlyphs does the work of [simpleFontGlyphMetrics] and also
// reports whether a symbol cmap subtable decided the codes.
//
// # Corpus evidence
//
//   - pdfjs issue2618.pdf: its /R641 names code 5 "space". Following that
//     name through the program's Unicode cmap drew a 930-unit glyph, while the
//     (3,0) subtable draws the 352-unit one /Widths declares. It backs reading
//     a symbolic font's codes through its symbol subtable despite an
//     /Encoding.
//   - govdocs thread_300_300492.pdf: taking the cmap route for a symbolic
//     Type 1 or CFF font left every TeX Computer Modern subset in the file with
//     no mapping and no width check at all.
func resolveSimpleFontGlyphs(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) (simpleFontGlyphs, bool) {
	fd := document.ResolveFontDescriptor(ctx, doc, fontDict)
	if fd == nil {
		return simpleFontGlyphs{}, false
	}

	fontData := extractFontProgramData(ctx, doc, fd)
	if fontData == nil {
		return simpleFontGlyphs{}, false
	}

	binaryWidths := typography.ExtractBinaryWidths(ctx, fontData)
	if binaryWidths == nil {
		return emptyCFFGlyphs(ctx, doc, fontDict, fontData)
	}

	unitsPerEm := FontProgramUnitsPerEm(fontData)

	// Extract charCode → GID mapping from font's cmap table (TrueType/OTTO).
	charCodeToGID := typography.ExtractCharCodeToGID(ctx, fontData)
	var encNames map[byte]string
	var notdefNamed map[byte]bool

	// A symbolic TrueType font resolves its single-byte codes through the
	// embedded (3,0)/(1,0) cmap, not through glyph names: ISO 32000-1, 9.6.6.4
	// takes that route "when the font has no Encoding entry, or the font
	// descriptor's Symbolic flag is set (in which case the Encoding entry is
	// ignored)". So a symbolic font that carries an /Encoding is read the same
	// way whenever its program has a symbol subtable, which is the route
	// veraPDF's TrueTypeFontProgram.getWidth takes too.
	//
	// With no symbol subtable, a font that has neither an /Encoding nor a
	// Unicode cmap leaves its codes unresolvable, so an empty (non-nil) map makes
	// the width and presence checks skip them rather than misreport against an
	// empty encoding or an identity GID. One that has either keeps the glyph-name
	// route below.
	//
	// The cmap route is an sfnt one. A symbolic Type 1 or CFF font has no cmap
	// to be missing: its codes resolve through the program's own encoding
	// (ISO 32000-1 §9.6.6.2), which the glyph-name path below reads, so
	// short-circuiting it here leaves such a font with no mapping and no width
	// check at all.
	if isSfntFontProgram(fontData) && isSymbolicFont(ctx, doc, fd) {
		if sym := typography.ExtractSymbolCharCodeToGID(ctx, fontData); sym != nil {
			return simpleFontGlyphs{widths: binaryWidths, unitsPerEm: unitsPerEm, codeToGID: sym, symbolCmap: true}, true
		}
		if charCodeToGID == nil && fontDict["Encoding"] == nil {
			return simpleFontGlyphs{widths: binaryWidths, unitsPerEm: unitsPerEm, codeToGID: map[uint16]uint16{}}, true
		}
	}

	// For fonts without a cmap (Type1/CFF), build charCode→GID via glyph names:
	// PDF encoding maps charCode→glyphName; font program maps GID→glyphName.
	if charCodeToGID == nil {
		glyphNames := typography.ExtractGlyphNames(ctx, fontData)
		if glyphNames != nil {
			enc := resolveSimpleFontEncoding(ctx, doc, fontDict)
			if len(enc) > 0 {
				nameToGID := buildNameToGID(glyphNames)
				// The font program's own built-in encoding is authoritative for a
				// code whose PDF /Encoding name labels a glyph the program does not
				// carry. A CFF names its WinAnsi 0xA0 (NO-BREAK SPACE) slot "space",
				// but reverse Adobe Glyph List resolution yields the synthetic
				// "uni00A0", which no charset holds; the code then falls to GID 0
				// (.notdef) and misreports both a width mismatch and an absent glyph.
				// Falling back to the program's built-in name for the code resolves
				// the glyph the font author intended (mirrors [checkType1FontWidths]).
				builtin := typography.ExtractCFFEncodingNames(fontData)
				charCodeToGID = make(map[uint16]uint16)
				for code, name := range enc {
					if name == "" {
						continue
					}
					// A code the encoding names .notdef selects the program's
					// .notdef glyph, GID 0 in every CFF charset (Adobe Technical
					// Note #5176, 13), and veraPDF weighs its width
					// (CFFType1FontProgram.getWidth) while holding the glyph
					// present.
					if name == ".notdef" {
						charCodeToGID[uint16(code)] = 0
						if notdefNamed == nil {
							notdefNamed = make(map[byte]bool)
						}
						notdefNamed[code] = true
						continue
					}
					gid, ok := nameToGID[name]
					if !ok {
						if bn := builtin[code]; bn != "" && bn != ".notdef" {
							gid, ok = nameToGID[bn]
						}
					}
					if ok {
						charCodeToGID[uint16(code)] = gid
					} else {
						// Glyph name absent from both the PDF /Encoding's charset
						// and the program's built-in encoding → GID 0 (.notdef).
						// Include it so width comparison can detect the mismatch.
						charCodeToGID[uint16(code)] = 0
					}
				}
			}
		}
	} else if fontDict["Encoding"] != nil || !isSymbolicFont(ctx, doc, fd) {
		// A non-symbolic TrueType font (or one with an explicit /Encoding)
		// resolves a code through its glyph name (ISO 32000-1 §9.6.6.4): the name
		// maps to a Unicode value via the Adobe Glyph List, and that Unicode is
		// looked up in the font's cmap (charCodeToGID is keyed by the cmap's
		// Unicode value, see [typography.ExtractCharCodeToGID]). This differs from
		// matching the glyph literally named `name`: a font may carry a distinct
		// glyph under that name (e.g. a Greek "mu" beside the micro-sign "mu1"
		// that WinAnsi code 0xB5 actually selects), and a raw code lookup picks
		// the wrong glyph too (MacRoman 0xD1=emdash read as U+00D1=Ntilde). The
		// AGL/cmap path resolves both. Fall back to a direct name match only when
		// that path is empty, keeping codes the program does not name at their
		// cmap value rather than forcing .notdef.
		glyphNames := typography.ExtractGlyphNames(ctx, fontData)
		enc := resolveSimpleFontEncoding(ctx, doc, fontDict)
		if len(enc) > 0 {
			unicodeToGID := maps.Clone(charCodeToGID)
			nameToGID := buildNameToGID(glyphNames)
			for code, name := range enc {
				if name == ".notdef" || name == "" {
					continue
				}
				if r, ok := encodings.GlyphToRuneFull(name); ok && r >= 0 && r <= 0xFFFF {
					if gid, ok := unicodeToGID[uint16(r)]; ok {
						charCodeToGID[uint16(code)] = gid
						continue
					}
				}
				if gid, ok := nameToGID[name]; ok {
					charCodeToGID[uint16(code)] = gid
				}
			}
			// The same clause makes the name the only route to a glyph, so a code
			// the encoding leaves unnamed selects none. That holds only where the
			// names are wholly known: a predefined encoding, named or under
			// /BaseEncoding. A /Differences-only dictionary rests on an implicit
			// base litho derives (Table 114) and veraPDF does not, so it stays out.
			if fd["FontFile2"] != nil && !isSymbolicFont(ctx, doc, fd) && encodingFullyPredefined(ctx, doc, fontDict) {
				encNames = enc
			}
		}
	}

	return simpleFontGlyphs{widths: binaryWidths, unitsPerEm: unitsPerEm, codeToGID: charCodeToGID, encodingNames: encNames, notdefNamed: notdefNamed}, true
}

// emptyCFFGlyphs resolves the glyphs of a simple font whose name-keyed bare
// CFF program reaches a CharStrings INDEX holding no charstring
// ([cff.EmptyCharStrings]). Such a program reads, so the font is embedded
// ([fontProgramReadable]), but it defines no glyph: every code maps to GID 0
// and none of them to a glyph the program holds, so each one shown fails ISO
// 19005-2, 6.2.11.4.1 t2. A code the encoding names .notdef is the exception:
// veraPDF finds that name in the charset (CFFType1FontProgram.containsGlyph)
// and reports the code under 6.2.11.8 t1 instead.
//
// The width rule (6.2.11.5) still weighs each glyph, against the width veraPDF
// reads for a glyph the program lacks: the defaultWidthX of the Private DICT,
// as for any charstring that states no width (Adobe Technical Note #5177),
// or, for a subset ([cffSubsetName]), whose widths veraPDF reads up front and
// finds none of, the /MissingWidth the font dictionary declares (ISO 32000-1,
// Table 111, default 0). Measured with veraPDF 1.30.2 at
// PDF/A-2b on an empty INDEX under defaultWidthX 700: /Widths 700 passes and
// 600 or 0 fails; under the name ABCDEF+S7, /Widths 0 passes, 700 fails, and
// 300 passes beside /MissingWidth 300. litho fontemptycs_2b.pdf plants each.
//
// # Corpus evidence
//
//   - All corpora: no CFF program reaches an empty CharStrings INDEX, so no
//     cell moves.
func emptyCFFGlyphs(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontData []byte) (simpleFontGlyphs, bool) {
	defaultWidthX, cidKeyed, ok := cff.EmptyCharStrings(fontData)
	if !ok || cidKeyed {
		return simpleFontGlyphs{}, false
	}
	unitsPerEm := FontProgramUnitsPerEm(fontData)
	advance := defaultWidthX
	if cffSubsetName(fontBaseFont(ctx, doc, fontDict)) {
		advance = simpleFontMissingWidth(ctx, doc, fontDict) * float64(unitsPerEm) / 1000
	}
	codeToGID := make(map[uint16]uint16, 256)
	for code := range uint16(256) {
		codeToGID[code] = 0
	}
	var notdefNamed map[byte]bool
	for code, name := range resolveSimpleFontEncoding(ctx, doc, fontDict) {
		if name != ".notdef" {
			continue
		}
		if notdefNamed == nil {
			notdefNamed = make(map[byte]bool)
		}
		notdefNamed[code] = true
	}
	return simpleFontGlyphs{
		widths:      map[uint16]float64{0: advance},
		unitsPerEm:  unitsPerEm,
		codeToGID:   codeToGID,
		notdefNamed: notdefNamed,
	}, true
}

// encodingFullyPredefined reports whether a simple font's /Encoding names its
// codes entirely from a predefined encoding (ISO 32000-1, Annex D): a named
// encoding, or an encoding dictionary whose /BaseEncoding names one.
func encodingFullyPredefined(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) bool {
	encObj, ok := fontDict["Encoding"]
	if !ok {
		return false
	}
	if name, err := document.ResolveName(ctx, doc, encObj); err == nil && name != "" {
		return predefinedEncodingNames(string(name)) != nil
	}
	encDict, err := document.ResolveDict(ctx, doc, encObj)
	if err != nil || encDict == nil {
		return false
	}
	base, err := document.ResolveName(ctx, doc, encDict["BaseEncoding"])
	return err == nil && predefinedEncodingNames(string(base)) != nil
}

// buildNameToGID inverts a GID→glyphName table into a glyphName→GID lookup,
// keeping the lowest GID for a name that labels several glyphs.
func buildNameToGID(glyphNames map[uint16]string) map[string]uint16 {
	nameToGID := make(map[string]uint16, len(glyphNames))
	for gid, name := range glyphNames {
		if _, exists := nameToGID[name]; !exists {
			nameToGID[name] = gid
		}
	}
	return nameToGID
}

// checkType1FontWidths compares a classic Type1 (/FontFile) simple font's
// /Widths array against the advance widths in its embedded charstrings. It is
// the counterpart to [checkSimpleFontWidths] for programs whose widths live in
// each glyph's hsbw/sbw operator (which the GID-keyed [typography.ExtractBinaryWidths]
// cannot read). The charstring advance is scaled by the /FontMatrix to reach
// the 1000-unit text space of /Widths.
//
// [type1WidthGlyphName] picks the glyph as veraPDF does. A glyph the program
// does not define is taken at the /MissingWidth where that choice is veraPDF's
// too, and so is a code outside /FirstChar to /LastChar on the dictionary side
// (ISO 32000-1, 9.6.2.1, Table 111).
//
// The scaled advance is weighed as the fraction it is. ISO 19005-2, 6.2.11.5
// and ISO 19005-1, 6.3.6 ask the widths of the font dictionary and of the
// embedded program to be consistent, and litho keeps that comparison exact
// ([widthsInconsistent]). veraPDF stores a Type 1 program's advances as
// integers (Type1FontProgram.glyphWidths), so a /FontMatrix that makes an
// advance fractional loses the fraction there, and such a glyph can pass one
// tool and fail the other. The divergence is deliberate: litho does not copy
// the truncation.
//
// # Corpus evidence
//
//   - pdfjs bug1354114.pdf: shows WinAnsi code 10, below its /FirstChar, and
//     veraPDF weighs the 500-unit .notdef it selects against 0.
//   - pdfjs issue11150_reduced.pdf: embeds a Symbol program with no /Widths
//     whose code 113 advances 521 units, and veraPDF reports it against 0.
//   - govdocs thread_300_300705.pdf: shows code 13 through a Helvetica-Light
//     whose /Widths starts at 32, and veraPDF reports the 278-unit charstring
//     against the 0 the dictionary implies. Skipping codes the array does not
//     cover left this whole class of Type 1 fonts unjudged.
//   - govdocs thread_300_300035.pdf, thread_300_300083.pdf and
//     thread_300_300084.pdf: embed Type 1 programs whose /FontMatrix makes the
//     advances fractional. At PDF/A-1b and PDF/A-2b litho states 24, 30 and
//     37 width glyphs against veraPDF's 1, 12 and 12, and truncating litho's
//     program widths leaves exactly veraPDF's counts.
//   - govdocs thread_200_200155.pdf: the same truncation accounts for 148 of
//     the 217 width glyphs litho states, leaving veraPDF's 69.
func checkType1FontWidths(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, rule Rule, usage *fontUsageInfo) []Violation {
	if usage == nil || len(usage.charCodes) == 0 || !usage.visible {
		return nil
	}
	if subtype := fontSubtype(ctx, doc, fontDict); subtype != "Type1" && subtype != "MMType1" {
		return nil
	}
	fd := document.ResolveFontDescriptor(ctx, doc, fontDict)
	if fd == nil {
		return nil
	}
	fontData := extractFontProgramData(ctx, doc, fd)
	if fontData == nil {
		return nil
	}
	nameWidths := typography.ExtractType1AdvanceWidths(fontData)
	if len(nameWidths) == 0 {
		return nil
	}
	builtin := builtinEncodingNames(fontData)
	pdfEnc := resolveSimpleFontEncoding(ctx, doc, fontDict)
	scale, _ := typography.ExtractType1FontMatrixScale(fontData)
	table := type1EncodingTable(ctx, doc, fontDict)
	differences := fontDifferencesNames(ctx, doc, fontDict)
	// A predefined encoding the program references names .notdef wherever it
	// defines nothing, as a vector the program fills with .notdef does; a bare
	// "256 array" names nothing there, and veraPDF then finds no glyph for such a
	// code and takes the /MissingWidth.
	raw := typography.ExtractType1BuiltinEncoding(fontData)
	fillsNotdef := (raw[0] != "" && raw[0] == raw[255]) || typography.ExtractType1NotdefFilled(fontData)
	definedGlyphs := make(map[string]bool)
	for _, name := range typography.ExtractGlyphNames(ctx, fontData) {
		definedGlyphs[name] = true
	}

	// A font stating no /Widths, or none it can be indexed by, declares the
	// /MissingWidth for every code it shows (ISO 32000-1, 9.6.2.1, Table 111),
	// as [checkSimpleFontWidths] reads it for a program with a GID-keyed table:
	// veraPDF reads an array only when /Widths, /FirstChar and /LastChar are all
	// present and the array is not empty, and weighs every other code at the
	// /MissingWidth.
	widths, firstChar, lastChar := type1WidthsArray(ctx, doc, fontDict)

	// A shown code the array does not cover takes the descriptor's
	// /MissingWidth (ISO 32000-1, Table 111, default 0), which is the same
	// comparison [checkSimpleFontUncoveredWidths] makes on the GID-keyed path.
	// Skipping those codes here left a whole class of Type 1 fonts unjudged.
	missingWidth := simpleFontMissingWidth(ctx, doc, fontDict)

	faults := newGlyphFaults(rule)
	for _, code := range sortedByteKeys(usage.charCodes) {
		if code == 0 {
			continue
		}
		name, absentIsMissing := type1WidthGlyphName(code, pdfEnc, builtin, differences, table, fillsNotdef, nameWidths)
		expected := missingWidth
		if rawWidth, ok := nameWidths[name]; ok && name != "" {
			expected = rawWidth * scale * 1000.0
		} else if !absentIsMissing || definedGlyphs[name] {
			// A glyph the program defines but whose advance litho could not
			// read is not missing: skip it rather than weigh it at /MissingWidth.
			continue
		}
		pdfWidth := missingWidth
		source := " from /MissingWidth"
		if idx := int(code) - firstChar; idx >= 0 && idx < len(widths) && int(code) <= lastChar {
			w, wOK := widthValue(ctx, doc, widths[idx])
			if !wOK {
				continue
			}
			pdfWidth, source = w, ""
		}
		if pdfWidth < math.MinInt32 || pdfWidth > math.MaxInt32 {
			continue
		}
		if widthsInconsistent(pdfWidth, expected) {
			faults.add(uint32(code), "font %s on page %d: char %d "+WidthMismatchContext+" (PDF=%.1f%s, font=%.1f)",
				fontName, pageNum, code, pdfWidth, source, expected)
		}
	}
	return faults.violations()
}

// type1WidthsArray returns a simple font's /Widths array with the /FirstChar
// and /LastChar that bound it, or a nil array when any of the three is absent,
// not of its type, or the array is empty: every code is then uncovered and
// takes the /MissingWidth.
func type1WidthsArray(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) (widths ast.PdfArray, firstChar, lastChar int) {
	arr, err := document.ResolveArray(ctx, doc, fontDict["Widths"])
	if err != nil || len(arr) == 0 {
		return nil, 0, 0
	}
	first, errFirst := document.ResolveObject(ctx, doc, fontDict["FirstChar"])
	last, errLast := document.ResolveObject(ctx, doc, fontDict["LastChar"])
	fc, okFirst := first.(ast.PdfInt)
	lc, okLast := last.(ast.PdfInt)
	if errFirst != nil || errLast != nil || !okFirst || !okLast {
		return nil, 0, 0
	}
	return arr, int(fc), int(lc)
}

// widthValue resolves a width-array entry and returns its numeric value. A
// /Widths or /W entry may be an indirect reference (ISO 32000-1 §7.3.10);
// [document.Float] cannot chase references, so an unresolved one reads as 0 and
// fires a spurious width mismatch veraPDF (which resolves it) does not. ok is
// false only for an unresolvable reference, so the caller skips it rather than
// misreading the declared width as zero.
func widthValue(ctx context.Context, doc *document.Document, obj ast.PdfObject) (float64, bool) {
	resolved, err := document.ResolveObject(ctx, doc, obj)
	if err != nil {
		return 0, false
	}
	return document.Float(resolved), true
}

// checkSimpleFontWidths compares /Widths array against binary widths for simple fonts.
// charCodeToGID is the cmap-derived charCode → GID mapping (may be nil for non-TrueType).
//
// # Corpus evidence
//
//   - pdfjs issue11150_reduced.pdf: embeds a Symbol program whose code 113
//     advances 521 units and declares neither /Widths nor /MissingWidth, so
//     its declared width is 0 and veraPDF reports the glyph under ISO 19005-2
//     §6.2.11.5 t1.
func checkSimpleFontWidths(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, binaryWidths map[uint16]float64, unitsPerEm uint16, fontName string, pageNum int, rule Rule, charCodeToGID map[uint16]uint16, usage *fontUsageInfo) []Violation {
	// Width consistency is a property of a glyph the document actually
	// paints: the rule compares the program advance against the declared
	// width only when both are resolvable and the glyph is rendered visibly
	// (rendering mode != 3). A font used only invisibly is exempt. An
	// unresolvable /FirstChar leaves the array unindexable and keeps the
	// conservative answer below; that case was not measured against veraPDF.
	if usage == nil || len(usage.charCodes) == 0 || !usage.visible {
		return nil
	}

	// A font stating no /Widths still declares a width for every code it
	// shows: ISO 32000-1, 9.6.2.1 and Table 111 give a code the array does not
	// cover the descriptor's /MissingWidth, default 0. With no array at all
	// every shown code is uncovered, so they all take that same comparison,
	// and veraPDF reports such a glyph under ISO 19005-2 §6.2.11.5 t1. The
	// missing-key rules (§6.2.11.2 t4 to t6) report the absent array itself
	// and say nothing about the widths it would have held.
	widthsObj, hasWidths := fontDict["Widths"]
	if !hasWidths {
		return checkSimpleFontUncoveredWidths(ctx, doc, fontDict, binaryWidths, unitsPerEm,
			fontName, pageNum, rule, charCodeToGID, 0, 0, usage)
	}

	widths, err := document.ResolveArray(ctx, doc, widthsObj)
	if err != nil {
		doc.Logger.Debug("compliance: fonts: cannot resolve /Widths", "font", fontName, "error", err)
		return nil
	}

	firstChar, err := document.ResolveInt(ctx, doc, fontDict["FirstChar"])
	if err != nil {
		return nil
	}

	// Code 0 is a width-checkable glyph only when a text operator shows it.
	// An unrendered NUL listed in /Widths is not a painted glyph, so it is
	// not tested. When code 0 IS rendered, the comparison resolves it the
	// same way as any code, the resolved glyph's program advance (the
	// .notdef glyph's advance when the code maps to no glyph) against the
	// declared width: /Widths[0-FirstChar] when code 0 is in range, else
	// /MissingWidth (default 0, ISO 32000-1 Table 111).
	// Every shown code is its own glyph; see [fontJudgements.glyphs].
	var results []Violation
	if usage.charCodes[0] {
		results = checkSimpleFontCode0Width(ctx, doc, fontDict, binaryWidths, unitsPerEm, fontName, pageNum, rule, charCodeToGID, int(firstChar), widths)
	}

	faults := newGlyphFaults(rule)
	for i, wObj := range widths {
		charCode := int(firstChar) + i
		if charCode < 0 || charCode > 255 {
			continue
		}
		// Without a code→GID mapping litho cannot resolve the code to a glyph.
		// A nil map means the font's cmap used a format litho does not parse (a
		// format-2 CJK subtable, say) and no glyph names were available. Assuming
		// identity (GID == code) then misreads a proportionally-encoded font: a
		// WinAnsi space (code 32) would read GID 32's advance instead of the
		// cmap's space glyph and fire a spurious mismatch veraPDF, which parses
		// the cmap, does not. Skip the code rather than guess.
		if charCodeToGID == nil {
			continue
		}
		mappedGID, ok := charCodeToGID[uint16(charCode)] //nolint:gosec // G115: charCode is bounded to 0..255 by the guard above.
		if !ok {
			continue
		}
		gid := mappedGID
		// Width consistency applies only to glyphs referenced for
		// rendering: skip charCodes not used in text operators.
		charUsed := usage.charCodes[byte(charCode)] //nolint:gosec // G115: charCode is bounded to 0..255 by the guard above.
		if !charUsed {
			continue
		}
		// Code 0 is handled above only when rendered. The main loop walks
		// the /Widths array, which can list code 0 without it ever being
		// shown by a text operator, so an unrendered /Widths[0] is not a
		// painted glyph and is skipped here regardless of the GID mapping.
		if charCode == 0 {
			continue
		}
		pdfWidth, wOK := widthValue(ctx, doc, wObj)
		if !wOK {
			continue
		}
		// A width outside the 32-bit integer range cannot be a real
		// metric: it is crafted overflow input (implementation-limits
		// fixtures write -2157483648). ISO 32000-2 §7.3.3 leaves
		// out-of-range integer handling unspecified, so litho treats
		// such values as unparseable rather than mismatching and skips
		// the comparison instead of flagging it.
		if pdfWidth < math.MinInt32 || pdfWidth > math.MaxInt32 {
			continue
		}
		// A code that maps to no glyph resolves to GID 0 (.notdef), so the
		// program width is the .notdef glyph's own advance. The width rule
		// is independent of glyph presence: both can fire on the same glyph
		// when the declared width disagrees with the .notdef advance.
		bw, ok := binaryWidths[gid]
		if !ok {
			continue
		}
		expectedWidth := bw * 1000.0 / float64(unitsPerEm)
		if widthsInconsistent(pdfWidth, expectedWidth) {
			faults.add(uint32(charCode), "font %s on page %d: char %d "+WidthMismatchContext+" (PDF=%.1f, font=%.1f)", //nolint:gosec // G115: charCode is bounded to 0..255 by the guard above.
				fontName, pageNum, charCode, pdfWidth, expectedWidth)
		}
	}
	results = append(results, faults.violations()...)

	return append(results, checkSimpleFontUncoveredWidths(ctx, doc, fontDict, binaryWidths, unitsPerEm, fontName, pageNum, rule, charCodeToGID, int(firstChar), len(widths), usage)...)
}

// checkSimpleFontUncoveredWidths applies the glyph-width rule to the shown codes
// the /Widths array does not cover. Their declared width is the descriptor's
// /MissingWidth (ISO 32000-1, Table 111, default 0), so the comparison is the
// same one the array codes get, and veraPDF reports a mismatch under ISO
// 19005-2 §6.2.11.5.
//
// Only a code that resolves to a glyph with a known program advance is judged.
// Code 0 is left to [checkSimpleFontCode0Width], which resolves it through the
// .notdef fallback the other codes do not get.
//
// # Corpus evidence
//
//   - govdocs thread_100_100945.pdf: shows a space at code 32, below its
//     /FirstChar of 48, against a 222-unit program glyph, which veraPDF
//     reports under ISO 19005-2 §6.2.11.5 and litho never looked at.
func checkSimpleFontUncoveredWidths(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, binaryWidths map[uint16]float64, unitsPerEm uint16, fontName string, pageNum int, rule Rule, charCodeToGID map[uint16]uint16, firstChar, widthCount int, usage *fontUsageInfo) []Violation {
	if charCodeToGID == nil {
		return nil
	}
	missingWidth := simpleFontMissingWidth(ctx, doc, fontDict)
	if missingWidth < math.MinInt32 || missingWidth > math.MaxInt32 {
		return nil
	}
	faults := newGlyphFaults(rule)
	for _, code := range sortedByteKeys(usage.charCodes) {
		if code == 0 {
			continue
		}
		if idx := int(code) - firstChar; idx >= 0 && idx < widthCount {
			continue
		}
		// GID 0 is what [simpleFontGlyphMetrics] records for a code whose name
		// the program's charset does not hold, so comparing /MissingWidth
		// against the .notdef advance would test a glyph the font never
		// selects. Glyph presence is the rule for that; leave it there.
		gid, mapped := charCodeToGID[uint16(code)]
		if !mapped || gid == 0 {
			continue
		}
		bw, ok := binaryWidths[gid]
		if !ok {
			continue
		}
		expectedWidth := bw * 1000.0 / float64(unitsPerEm)
		if widthsInconsistent(missingWidth, expectedWidth) {
			faults.add(uint32(code), "font %s on page %d: char %d "+WidthMismatchContext+" (PDF=%.1f from /MissingWidth, font=%.1f)",
				fontName, pageNum, code, missingWidth, expectedWidth)
		}
	}
	return faults.violations()
}

// checkSimpleFontCode0Width applies the glyph-width rule to a rendered code 0.
// The main /Widths loop skips code 0 (it often falls below /FirstChar and so is
// absent from the array), but a code 0 that is shown is a painted glyph and is
// width-checked: it resolves the glyph the same way as any code (charCode → GID,
// falling back to GID 0 /.notdef when the code maps to no glyph name) and
// compares the program advance against the declared width. The declared width is
// /Widths[0-FirstChar] when code 0 is covered by the array, otherwise
// /MissingWidth (default 0, ISO 32000-1 Table 111).
func checkSimpleFontCode0Width(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, binaryWidths map[uint16]float64, unitsPerEm uint16, fontName string, pageNum int, rule Rule, charCodeToGID map[uint16]uint16, firstChar int, widths []ast.PdfObject) []Violation {
	// Symbolic fonts (FontDescriptor /Flags bit 3) map code 0 through the
	// embedded program's built-in encoding, which litho does not reconstruct
	// for CFF; the GID-0 fallback below would then read the wrong glyph and
	// misfire (a symbolic CFF such as CMSY8 resolves code 0 to a real glyph,
	// not .notdef). Restrict the code-0 check to non-symbolic fonts, where the
	// /Encoding name path and the .notdef fallback are reliable.
	if isSymbolicFont(ctx, doc, document.ResolveFontDescriptor(ctx, doc, fontDict)) {
		return nil
	}

	// A code with no glyph-name mapping resolves to GID 0 (.notdef), so its
	// program width is that glyph's own advance.
	gid := uint16(0)
	if charCodeToGID != nil {
		if mapped, ok := charCodeToGID[0]; ok {
			gid = mapped
		}
	}
	bw, ok := binaryWidths[gid]
	if !ok {
		// No program advance for the resolved glyph: the program width is
		// unresolvable, so there is nothing to compare and the rule passes.
		return nil
	}

	// Declared width: /Widths[0-FirstChar] when code 0 is covered by the array,
	// else /MissingWidth (default 0, ISO 32000-1 Table 111).
	pdfWidth := simpleFontMissingWidth(ctx, doc, fontDict)
	if firstChar <= 0 && -firstChar < len(widths) {
		if wv, wOK := widthValue(ctx, doc, widths[-firstChar]); wOK {
			pdfWidth = wv
		}
	}
	if pdfWidth < math.MinInt32 || pdfWidth > math.MaxInt32 {
		return nil
	}

	expectedWidth := bw * 1000.0 / float64(unitsPerEm)
	if widthsInconsistent(pdfWidth, expectedWidth) {
		return glyphFailf(rule, 0, "font %s on page %d: char 0 "+WidthMismatchContext+" (PDF=%.1f, font=%.1f)",
			fontName, pageNum, pdfWidth, expectedWidth)
	}
	return nil
}

// simpleFontMissingWidth returns the FontDescriptor /MissingWidth for a simple
// font, the declared width for a code outside the /Widths array. Defaults to
// 0 when absent (ISO 32000-1 Table 111).
func simpleFontMissingWidth(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) float64 {
	fd := document.ResolveFontDescriptor(ctx, doc, fontDict)
	if fd == nil {
		return 0
	}
	mw, _ := document.ResolveObject(ctx, doc, fd["MissingWidth"])
	return document.Float(mw)
}

// glyphPresenceRule selects the per-part "embedded font program defines all
// glyphs referenced for rendering" rule: ISO 19005-1 §6.3.5 t1, ISO 19005-2/3
// §6.2.11.4.1 t2, ISO 19005-4 §6.2.10.4.1 t2. ISO 19005-1 §6.3.5 t1 applies to
// both PDF/A-1A and PDF/A-1B (the glyph-presence requirement); ISO 19005-1
// §6.3.8 t1 is the unrelated ToUnicode requirement and is not this rule.
func glyphPresenceRule(profile string) Rule {
	// PDF/UA carries the same glyph-presence requirement: ISO 14289-1
	// §7.21.4.1 t2 for UA-1 and ISO 14289-2 §8.4.5.5.1 t2 for UA-2, which
	// fire alongside the notdef and ToUnicode rules on the same glyph.
	if IsPDFUA(profile) {
		if PDFUAPart(profile) == 2 {
			return Rule_14289_2_8_4_5_5_1_t02
		}
		return Rule_14289_1_7_21_4_1_t02
	}
	return ruleByPart(PDFAPart(profile), Rule_19005_1_6_3_5_t01, Rule_19005_2_6_2_11_4_1_t02, Rule_19005_3_6_2_11_4_1_t02, Rule_19005_4_6_2_10_4_1_t02)
}

// checkSimpleGlyphPresence validates that every glyph referenced for visible
// rendering exists in the embedded font program. It applies only to glyphs
// painted visibly (rendering mode != 3): a used charCode whose glyph name is
// absent from the program maps to GID 0 in [simpleFontGlyphMetrics], which is
// the "glyph not present" signal. Codes litho cannot map at all stay unchecked,
// since glyph presence is then unresolvable.
//
// # Corpus evidence
//
//   - verapdf: the .notdef fixtures fail the .notdef rule alone, since veraPDF
//     counts the code 0 glyph as present.
//   - pdfjs issue8234.pdf: its code 32 meets a (3,0) subtable holding only
//     0xF021 to 0xF027, and veraPDF reports the glyph absent.
//   - govdocs thread_300_300708.pdf: veraPDF reports the glyph for its
//     WinAnsiEncoding code 31 absent, a code the encoding leaves unnamed.
func checkSimpleGlyphPresence(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string, usage *fontUsageInfo) []Violation {
	rule := glyphPresenceRule(profile)
	if rule.Specification == "" {
		return nil
	}
	if usage == nil || len(usage.charCodes) == 0 || !usage.visible {
		return nil
	}
	// A Type3 font's glyph programs are its /CharProcs, not a font file, so
	// [simpleFontGlyphMetrics] has nothing to read and presence is decided
	// against the procedure dictionary instead.
	if isType3Font(ctx, doc, fontDict) {
		return checkType3GlyphPresence(ctx, doc, fontDict, fontName, pageNum, rule, usage)
	}
	glyphs, ok := resolveSimpleFontGlyphs(ctx, doc, fontDict)
	if !ok || glyphs.codeToGID == nil {
		// A classic Type 1 (/FontFile) program names its glyphs in the eexec
		// /CharStrings dictionary rather than a GID-keyed table, so there is no
		// GID to read (mirrors [checkType1FontWidths]). Decide presence by name.
		return checkType1GlyphPresence(ctx, doc, fontDict, fontName, pageNum, rule, usage)
	}
	faults := newGlyphFaults(rule)
	for _, code := range sortedByteKeys(usage.charCodes) {
		// Code 0 references .notdef, which the .notdef rule judges; veraPDF
		// counts its glyph as present (GFGlyph.initForNotType3).
		if code == 0 {
			continue
		}
		gid, mapped := glyphs.codeToGID[uint16(code)]
		// A symbolic TrueType font draws a code through its symbol cmap
		// subtable (ISO 32000-1, 9.6.6.4), so a code that subtable does not hold
		// selects no glyph: it is resolved, not unknown. veraPDF
		// (TrueTypeFontProgram.containsCode) reports it absent.
		if !mapped && glyphs.symbolCmap {
			gid, mapped = 0, true
		}
		// A non-symbolic TrueType font reaches a glyph only through the name its
		// encoding gives the code, so a code left unnamed, or named .notdef,
		// selects none. veraPDF reports it absent.
		if !mapped && glyphs.encodingNames != nil {
			if name := glyphs.encodingNames[code]; name == "" || name == ".notdef" {
				gid, mapped = 0, true
			}
		}
		// A CFF charset holds .notdef, so a code its encoding names .notdef
		// selects a glyph the program defines: veraPDF answers the presence
		// test by that name (CFFType1FontProgram.containsGlyph).
		if mapped && gid == 0 && !glyphs.notdefNamed[code] {
			faults.add(uint32(code), "font %s on page %d: glyph for charCode %d not present in embedded font program", fontName, pageNum, code)
		}
	}
	return faults.violations()
}

// checkType1GlyphPresence decides glyph presence for a Type 1 font whose
// program carries no GID-keyed glyph table. Per ISO 32000-1 §9.6.6.2 a simple
// font resolves a code to a glyph name through its encoding, and §9.9.1 places
// the glyph programs of a Type 1 font in the /CharStrings dictionary keyed by
// that name, so a name the dictionary does not hold paints no glyph. veraPDF
// reports it under ISO 19005-2 §6.2.11.4.1 t2.
//
// A program whose charstrings will not parse leaves presence unanswerable and
// goes unchecked, as does a code neither the PDF encoding nor the program's
// built-in encoding names.
//
// # Corpus evidence
//
//   - govdocs thread_300_300516.pdf: shows code 202, which its /Differences
//     names "blank", against a program whose 229 charstrings define no such
//     glyph, and veraPDF reports it.
//   - pdfjs bug1354114.pdf, issue6721_reduced.pdf, issue6901.pdf: show
//     WinAnsi code 10, StandardEncoding code 224, and a code the Type 1 vector
//     skips, respectively. veraPDF reports all of them, so a code no encoding
//     names fails whenever either encoding is readable.
func checkType1GlyphPresence(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, rule Rule, usage *fontUsageInfo) []Violation {
	if subtype := fontSubtype(ctx, doc, fontDict); subtype != "Type1" && subtype != "MMType1" {
		return nil
	}
	fd := document.ResolveFontDescriptor(ctx, doc, fontDict)
	if fd == nil {
		return nil
	}
	fontData := extractFontProgramData(ctx, doc, fd)
	if fontData == nil {
		return nil
	}
	defined := make(map[string]bool)
	for _, name := range typography.ExtractGlyphNames(ctx, fontData) {
		defined[name] = true
	}
	if len(defined) == 0 {
		return nil
	}

	builtin := builtinEncodingNames(fontData)
	differences := fontDifferencesNames(ctx, doc, fontDict)
	pdfEnc := resolveSimpleFontEncoding(ctx, doc, fontDict)
	faults := newGlyphFaults(rule)
	for _, code := range sortedByteKeys(usage.charCodes) {
		name := pdfEnc[code]
		// Outside /Differences the program's own name for the code is the better
		// answer when the base encoding names a glyph the program does not carry:
		// a subset commonly keeps synthetic G-names in its built-in encoding
		// (mirrors [checkType1FontWidths]). A name /Differences states is the
		// file's own choice for that code (ISO 32000-1, 9.6.6.1) and stands.
		if _, override := differences[code]; !override && !defined[name] && builtin[code] != "" {
			name = builtin[code]
		}
		if name == "" {
			// A code no encoding in force names has nothing to look up in
			// /CharStrings, so it selects no glyph at all. Answering that needs
			// the program's own charset, which this check reads. Both
			// encodings unreadable leaves the question open instead.
			if len(pdfEnc) == 0 && len(builtin) == 0 {
				continue
			}
			faults.add(uint32(code), "font %s on page %d: charCode %d is named by no encoding, so it selects no glyph",
				fontName, pageNum, code)
			continue
		}
		// A code that selects .notdef paints no glyph: veraPDF's Type 1
		// containsGlyph holds .notdef absent, so a /Differences entry naming it
		// fails the rule too.
		if name == ".notdef" {
			faults.add(uint32(code), "font %s on page %d: charCode %d selects .notdef, which paints no glyph", fontName, pageNum, code)
			continue
		}
		if !defined[name] {
			faults.add(uint32(code), "font %s on page %d: glyph %s for charCode %d not present in embedded font program",
				fontName, pageNum, name, code)
		}
	}
	return faults.violations()
}

// checkType3GlyphPresence validates that every character code a Type3 font
// shows names a glyph procedure the font defines. Per ISO 32000-1 §9.6.5 a
// Type3 glyph is looked up by mapping the code through /Encoding to a name and
// that name through /CharProcs to a procedure; a code the encoding leaves
// undefined, or one naming an absent procedure, paints nothing, which is the
// same "no glyph in the program" condition the presence rule states. veraPDF
// reports it under ISO 19005-2 §6.2.11.4.1 t2.
//
// A font whose /CharProcs or /Encoding will not resolve is left unchecked,
// since presence is then unanswerable rather than failed.
//
// # Corpus evidence
//
//   - pdfjs Type3WordSpacing.pdf: shows a space its font defines no procedure
//     for, and veraPDF reports it.
func checkType3GlyphPresence(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, rule Rule, usage *fontUsageInfo) []Violation {
	charProcs, err := document.ResolveDict(ctx, doc, fontDict["CharProcs"])
	if err != nil || len(charProcs) == 0 {
		return nil
	}
	encoding := resolveSimpleFontEncoding(ctx, doc, fontDict)
	if len(encoding) == 0 {
		return nil
	}

	// Sorted so a font missing several glyphs always names the same one.
	codes := make([]byte, 0, len(usage.charCodes))
	for code := range usage.charCodes {
		codes = append(codes, code)
	}
	slices.Sort(codes)

	faults := newGlyphFaults(rule)
	for _, code := range codes {
		glyphName := encoding[code]
		if glyphName == "" {
			faults.add(uint32(code), "font %s on page %d: charCode %d is not defined by the Type3 /Encoding",
				fontName, pageNum, code)
			continue
		}
		if _, ok := charProcs[ast.PdfName(glyphName)]; !ok {
			faults.add(uint32(code), "font %s on page %d: glyph %s for charCode %d has no /CharProcs entry",
				fontName, pageNum, glyphName, code)
		}
	}
	return faults.violations()
}

// ExpectedCIDFontWidths returns each CID's correct width (in 1000-unit glyph
// space) for a CIDFont, computed from the embedded font program. The map is
// keyed by CID, which the width check treats as equal to the glyph index
// (the Identity CIDToGIDMap assumption); a CID absent from the map has no glyph
// in the program and should keep its declared width. Returns ok=false when the
// font program is absent or unparseable. Remediation uses this to repair §6.2.11.5
// width mismatches in the /W array, sharing the same metrics extraction as the
// width check so the two stay consistent.
func ExpectedCIDFontWidths(ctx context.Context, doc *document.Document, cidFontDict ast.PdfDict) (map[int]int, bool) {
	fd := document.ResolveFontDescriptor(ctx, doc, cidFontDict)
	if fd == nil {
		return nil, false
	}
	fontData := extractFontProgramData(ctx, doc, fd)
	if fontData == nil {
		return nil, false
	}
	// CID-keyed map: shares typography.ExtractCIDBinaryWidths with the
	// width check so detector and repair agree on the authoritative widths
	// (CID-keyed CFF subsets renumber GIDs; /W stays CID-indexed).
	binaryWidths := typography.ExtractCIDBinaryWidths(ctx, fontData)
	if binaryWidths == nil {
		return nil, false
	}
	unitsPerEm := FontProgramUnitsPerEm(fontData)

	out := make(map[int]int, len(binaryWidths))
	for cid, bw := range binaryWidths {
		out[int(cid)] = int(math.Round(float64(bw) * 1000.0 / float64(unitsPerEm)))
	}
	return out, true
}

// checkType3FontWidths compares the width a Type 3 font's dictionary declares
// for each shown code against the one its glyph procedure declares with d0 or
// d1 (ISO 32000-1, 9.6.5).
//
// veraPDF takes both widths as it takes them for a simple font (PDFont.getWidth,
// PDType3Font.getWidthFromProgram): the dictionary's is the /Widths entry for a
// code within /FirstChar to /LastChar, and otherwise the descriptor's
// /MissingWidth, or 0; the program's is the wx a procedure opens with, and
// otherwise, for a code whose encoding names no procedure, whose procedure the
// font does not hold, or whose procedure does not begin with d0 or d1, the
// /MissingWidth again, or 0. It compares the two as the file writes them, in
// glyph space, with the one-unit tolerance of ISO 19005-2, 6.2.11.5, and so
// does litho: the /FontMatrix plays no part. Measured with veraPDF 1.30 at
// PDF/A-1b and PDF/A-2b: a code named /b with no procedure fails ISO 19005-1,
// 6.3.6 t1 and ISO 19005-2, 6.2.11.5 t1 against a /Widths entry of 500 or 2
// and passes against 1 or 0, as does a code the encoding does not name, and
// passes against 500 when the descriptor's /MissingWidth is 500; under a
// /FontMatrix of [0.01 0 0 0.01 0 0], a procedure declaring 50 passes against
// 50.5 and fails against 52, and a code with no procedure passes against 0.5
// and fails against 2.
//
// # Corpus evidence
//
//   - pdfjs issue5039.pdf: shows code 32 through a Type 3 font that names it
//     uni0020 and holds no procedure for it, against a /Widths entry of 602;
//     veraPDF fails the width rule for it at PDF/A-1b, PDF/A-2b and PDF/A-2u,
//     and litho now does too.
//   - pdfjs issue14724.pdf: its Type 3 fonts carry a /FontMatrix of [1 0 0 1
//     0 0], and scaling both widths by it turned a sub-unit difference into a
//     failure veraPDF does not state; litho now states none either.
//   - pdfjs poppler-90-0-fuzzed.pdf: the width count that moved away, from 1
//     to 18 against veraPDF's 0. The 18 codes are shown on pages 7, 12 and
//     16, whose content streams do not decode: veraPDF reads no operator from
//     them, while litho recovers operators from the damaged data. Their glyph
//     procedures are damaged too, so each code is weighed at the missing width
//     of 0.
func checkType3FontWidths(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, rule Rule, usage *fontUsageInfo) []Violation {
	charProcs, err := document.ResolveDict(ctx, doc, fontDict["CharProcs"])
	if err != nil || charProcs == nil {
		return nil
	}
	enc := resolveSimpleFontEncoding(ctx, doc, fontDict)
	widths, firstChar, lastChar := type1WidthsArray(ctx, doc, fontDict)
	missingWidth := type3MissingWidth(ctx, doc, fontDict)

	// Width consistency applies only to glyphs referenced for rendering, the
	// codes a stream shows: an unrendered /Widths entry (a code mapped to a
	// .notdef placeholder such as /g0 with declared width 0, whose procedure
	// still carries a non-zero advance) is not compared. With no usage, every
	// code the array declares is.
	var codes []byte
	if usage != nil {
		codes = sortedByteKeys(usage.charCodes)
	} else {
		for i := range widths {
			if code := firstChar + i; code >= 0 && code <= 255 {
				codes = append(codes, byte(code))
			}
		}
	}

	procWidths := make(map[string]float64)
	glyphWidthOf := func(name string) float64 {
		if w, ok := procWidths[name]; ok {
			return w
		}
		w, declared := type3ProcWidth(ctx, doc, charProcs, name)
		if !declared {
			w = missingWidth
		}
		procWidths[name] = w
		return w
	}

	// Every shown code is its own glyph; see [fontJudgements.glyphs].
	faults := newGlyphFaults(rule)
	for _, code := range codes {
		declared := missingWidth
		if idx := int(code) - firstChar; widths != nil && idx >= 0 && int(code) <= lastChar {
			declared = 0
			if idx < len(widths) {
				w, ok := widthValue(ctx, doc, widths[idx])
				if !ok {
					continue
				}
				declared = w
			}
		}
		glyphName := enc[code]
		glyphWidth := missingWidth
		if glyphName != "" {
			glyphWidth = glyphWidthOf(glyphName)
		}
		if widthsInconsistent(declared, glyphWidth) {
			faults.add(uint32(code), "font %s on page %d: char %d (%s) "+WidthMismatchContext+" (PDF=%.1f, glyph=%.1f)",
				fontName, pageNum, code, glyphName, declared, glyphWidth)
		}
	}
	// With no usage every declared code was judged, and naming each would
	// report glyphs nothing shows.
	if usage == nil {
		return faults.first()
	}
	return faults.violations()
}

// type3MissingWidth returns the /MissingWidth of a Type 3 font's descriptor,
// the width veraPDF takes for a code the /Widths array does not cover and for
// a glyph whose procedure declares none, or 0 when the font has no descriptor
// or the descriptor no numeric /MissingWidth (ISO 32000-1, 9.8.1, Table 122).
func type3MissingWidth(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) float64 {
	fd, err := document.ResolveDict(ctx, doc, fontDict["FontDescriptor"])
	if err != nil || fd == nil {
		return 0
	}
	mw, err := document.ResolveObject(ctx, doc, fd["MissingWidth"])
	if err != nil {
		return 0
	}
	if w, ok := document.Number(mw); ok {
		return w
	}
	return 0
}

// type3ProcWidth returns the glyph width the procedure charProcs holds under
// name declares, the wx operand of the d0 or d1 operator that must open it
// (ISO 32000-1, 9.6.5, Table 113). declared is false when the font holds no
// such procedure, when its stream does not decode or parse, or when it does not
// open with d0 or d1 and a numeric wx: veraPDF reads a width only from those
// leading tokens of the decoded stream (Type3CharProcParser) and takes the
// /MissingWidth otherwise. Measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b
// on a procedure whose /FlateDecode data is not a zlib stream: a /Widths entry
// of 500 fails ISO 19005-1, 6.3.6 t1 and ISO 19005-2, 6.2.11.5 t1, and 0
// passes.
func type3ProcWidth(ctx context.Context, doc *document.Document, charProcs ast.PdfDict, name string) (width float64, declared bool) {
	stream, err := document.ResolveStream(ctx, doc, charProcs[ast.PdfName(name)])
	if err != nil {
		return 0, false
	}
	data, reads := decodeContentStream(ctx, doc, stream)
	if !reads {
		return 0, false
	}
	ops, err := content.ParseStreamData(ctx, data)
	if err != nil {
		doc.Logger.Debug("compliance: cannot parse CharProc stream", "error", err)
		return 0, false
	}
	if len(ops) == 0 {
		return 0, false
	}
	// d0: wx wy d0. d1: wx wy llx lly urx ury d1.
	op := ops[0]
	if (op.Operator != "d0" || len(op.Operands) != 2) && (op.Operator != "d1" || len(op.Operands) != 6) {
		return 0, false
	}
	return document.Number(op.Operands[0])
}

// checkCIDFontWidths compares /W array against binary widths for CIDFonts.
//
// The comparison is usage-scoped to match veraPDF, which resolves every shown
// code to a GID and checks only those glyphs (see [checkType0NotdefWidth]). A
// /W entry for a CID the document never renders is not compared: its glyph may
// legitimately carry a different advance (a subset placeholder, a zero-advance
// combining glyph) without affecting any drawn text. usedCIDs nil means usage
// could not be collected, so every declared CID is checked as a fail-safe.
//
// # Corpus evidence
//
//   - pdfjs issue7901.pdf: pairs an OpenType CFF that is not CID-keyed with a
//     /CIDToGIDMap /W was written against, and veraPDF, ignoring the map,
//     reports 15 glyph widths.
func checkCIDFontWidths(ctx context.Context, doc *document.Document, cidFontDict ast.PdfDict, binaryWidths map[uint16]float64, unitsPerEm uint16, fontName string, pageNum int, rule Rule, usedCIDs map[uint16]bool) []Violation {
	// covered records the CIDs /W states a width for. Everything else falls to
	// /DW, which is checked once the array has been walked.
	covered := make(map[uint16]bool)
	// Every rendered CID is its own glyph; see [fontJudgements.glyphs].
	faults := newGlyphFaults(rule)

	var wArr ast.PdfArray
	if wObj, ok := cidFontDict["W"]; ok {
		var err error
		wArr, err = document.ResolveArray(ctx, doc, wObj)
		if err != nil {
			doc.Logger.Debug("compliance: fonts: resolving CID width array", "font", fontName, "error", err)
			return nil
		}
	}

	// A CIDFontType2 maps CID→GID through /CIDToGIDMap; the embedded program's
	// per-GID widths must be looked up by the mapped GID, not the CID. Assuming
	// Identity here misreads a non-Identity map's widths and fires a spurious
	// mismatch (the map is authoritative, veraPDF follows it). A nil lookup means
	// Identity (CID==GID); ok=false means the map stream is unreadable, so the
	// GID is unknowable and the check is skipped rather than guessed.
	//
	// A CIDFontType0 has no such map: ISO 32000-1, 9.7.4.2, Table 117 gives the
	// entry to Type 2 CIDFonts only, and a Type 0 CIDFont selects its glyph
	// through the CFF charset, or takes the CID as the GID when the CFF is not
	// CID-keyed. A map it carries is ignored, as veraPDF ignores it.
	var cidToGID func(uint16) uint16
	if fontSubtype(ctx, doc, cidFontDict) != "CIDFontType0" {
		var ok bool
		cidToGID, ok = cidToGIDMapping(ctx, doc, cidFontDict)
		if !ok {
			return nil
		}
	}

	// /W array format: [cid [w1 w2 ...]] or [cidFirst cidLast w]
	i := 0
	for i < len(wArr) {
		cidStartRes, err := document.ResolveObject(ctx, doc, wArr[i])
		if err != nil {
			doc.Logger.Debug("compliance: fonts: resolving CID start value", "error", err)
		}
		cidStartInt, ok := cidStartRes.(ast.PdfInt)
		if !ok {
			i++
			continue
		}
		cidStart := int(cidStartInt)
		i++
		if i >= len(wArr) {
			break
		}

		nextRes, err := document.ResolveObject(ctx, doc, wArr[i])
		if err != nil {
			doc.Logger.Debug("compliance: fonts: resolving CID width entry", "error", err)
		}
		switch next := nextRes.(type) {
		case ast.PdfArray:
			// [cidStart [w1 w2 w3 ...]]
			for j, wEntry := range next {
				cid := cidStart + j
				// Skip .notdef (CID 0), no visible outlines.
				if cid == 0 {
					continue
				}
				// A CID is 16 bits (ISO 32000-1, 9.7.4.3). Testing that
				// separately from usage matters: fused into the clause below it
				// was skipped whenever usedCIDs was nil, and the uint16
				// narrowing then aliased CID 65537 onto CID 1, reporting a width
				// mismatch against a CID no font can hold.
				if cid < 0 || cid > maxCID {
					continue
				}
				// Usage-scoped: skip CIDs the document never renders.
				if usedCIDs != nil && !usedCIDs[uint16(cid)] {
					continue
				}
				covered[uint16(cid)] = true //nolint:gosec // G115: guarded by the 16-bit range check above.
				gid := uint16(cid)          //nolint:gosec // G115: cid is a CIDFont CID within the 16-bit CID space.
				if cidToGID != nil {
					gid = cidToGID(gid)
				}
				// A CID mapping to GID 0 renders .notdef; glyph presence is the
				// coverage rule's concern, not width consistency.
				if gid == 0 {
					continue
				}
				// A width of 0 is a width like any other: veraPDF compares it.
				pdfWidth, wOK := widthValue(ctx, doc, wEntry)
				if !wOK {
					continue
				}
				// Out-of-int32-range widths are crafted overflow input,
				// not metrics; see checkSimpleFontWidths.
				if pdfWidth < math.MinInt32 || pdfWidth > math.MaxInt32 {
					continue
				}
				bw, ok := binaryWidths[gid]
				if !ok {
					continue
				}
				expectedWidth := bw * 1000.0 / float64(unitsPerEm)
				if widthsInconsistent(pdfWidth, expectedWidth) {
					faults.add(uint32(cid), "font %s on page %d: CID %d "+WidthMismatchContext+" (PDF=%.1f, font=%.1f)", //nolint:gosec // G115: guarded by the 16-bit range check above.
						fontName, pageNum, cid, pdfWidth, expectedWidth)
				}
			}
			i++
		case ast.PdfInt:
			// [cidStart cidEnd w]
			cidEnd := int(next)
			i++
			if i >= len(wArr) {
				break
			}
			// A range declaring 0 is compared like any other, and covers its
			// CIDs, so /DW is not their width.
			pdfWidth, wOK := widthValue(ctx, doc, wArr[i])
			i++
			if !wOK {
				continue
			}
			// Out-of-int32-range widths are crafted overflow input,
			// not metrics; see checkSimpleFontWidths.
			if pdfWidth < math.MinInt32 || pdfWidth > math.MaxInt32 {
				continue
			}
			// A CID is 16 bits (ISO 32000-1, 9.7.4.3), so a range naming more
			// describes no glyph. Bounding it here stops two things: a walk as
			// long as the file cares to declare, and the uint16 narrowing
			// below, which would alias an out-of-range CID onto a real one.
			lo, hi := cidStart, cidEnd
			if lo < 0 {
				lo = 0
			}
			if hi > maxCID {
				hi = maxCID
			}
			for cid := lo; cid <= hi; cid++ {
				// Skip .notdef (CID 0), no visible outlines.
				if cid == 0 {
					continue
				}
				// Usage-scoped: skip CIDs the document never renders. The
				// range check the fused form used to carry here is dead now
				// that lo and hi are bounded above.
				if usedCIDs != nil && !usedCIDs[uint16(cid)] {
					continue
				}
				covered[uint16(cid)] = true //nolint:gosec // G115: guarded by the 16-bit range check above.
				gid := uint16(cid)          //nolint:gosec // G115: cid is a CIDFont CID within the 16-bit CID space.
				if cidToGID != nil {
					gid = cidToGID(gid)
				}
				// A CID mapping to GID 0 renders .notdef; glyph presence is the
				// coverage rule's concern, not width consistency.
				if gid == 0 {
					continue
				}
				bw, ok := binaryWidths[gid]
				if !ok {
					continue
				}
				expectedWidth := bw * 1000.0 / float64(unitsPerEm)
				if widthsInconsistent(pdfWidth, expectedWidth) {
					faults.add(uint32(cid), "font %s on page %d: CID %d "+WidthMismatchContext+" (PDF=%.1f, font=%.1f)", //nolint:gosec // G115: lo and hi are bounded to the 16-bit CID space above.
						fontName, pageNum, cid, pdfWidth, expectedWidth)
				}
			}
		default:
			i++
		}
	}

	// With no usage every declared CID was judged, and naming each would
	// report glyphs nothing shows.
	if usedCIDs == nil {
		return faults.first()
	}
	return append(faults.violations(), checkCIDDefaultWidths(ctx, doc, cidFontDict, binaryWidths, unitsPerEm,
		fontName, pageNum, rule, usedCIDs, covered, cidToGID)...)
}

// checkCIDDefaultWidths compares the embedded program's advance against /DW for
// every rendered CID the /W array states no width for.
//
// ISO 32000-2, 9.7.4.3 makes /DW the width of every CID /W does not cover, and
// its own default 1000. A CIDFont may therefore carry no /W at all and still
// declare a width for every glyph it shows, so skipping the font when /W is
// absent leaves the widths unchecked.
//
// A program advance of 0 is a width like any other, and an explicit /DW 0 too
// ([defaultCIDWidth]). Measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b on
// a CID whose TrueType glyph advances 0: /DW 1000, /DW 500 or no /DW fails ISO
// 19005-1, 6.3.6 t1 and ISO 19005-2, 6.2.11.5 t1, and /DW 0 passes, as do
// /W [1 [0]] and /W [1 1 0], which cover the CID ([checkCIDFontWidths]).
//
// # Corpus evidence
//
//   - pdfjs issue6612.pdf: embeds a CIDFontType2 with /DW 595 and no /W whose
//     glyphs advance 1229 at 2048 units per em, which is 600.098. veraPDF
//     reports the mismatch on every shown glyph and fontTools confirms the
//     program advance, so litho's silence was a miss.
//   - pdfjs issue14821.pdf: shows four CIDs whose glyphs advance 0 under /DW
//     1000, and veraPDF fails the width rule for each, which litho skipped as
//     missing information.
//   - pdfjs complex_ttf_font.pdf, issue7426.pdf: declare /W ranges of 0, such
//     as [988 989 0] and [51 57 0], over glyphs that advance 0, and veraPDF
//     reports none of them. litho left a range of 0 out of the CIDs /W covers
//     and compared those glyphs to /DW, which the skip of a zero advance
//     hid.
//   - pdfjs JBIG2Globals.pdf: maps CID 40 into a two-glyph program, which
//     holds no advance for it; see [checkType2MissingGlyphWidths].
func checkCIDDefaultWidths(ctx context.Context, doc *document.Document, cidFontDict ast.PdfDict, binaryWidths map[uint16]float64, unitsPerEm uint16, fontName string, pageNum int, rule Rule, usedCIDs, covered map[uint16]bool, cidToGID func(uint16) uint16) []Violation {
	// Only the rendered CIDs are in scope, and with no recorded usage there is
	// no set to walk: the rule is raised per shown glyph.
	if len(usedCIDs) == 0 {
		return nil
	}

	dw := defaultCIDWidth(ctx, doc, cidFontDict)
	faults := newGlyphFaults(rule)
	for _, cid := range sortedCIDs(usedCIDs) {
		if ctx.Err() != nil {
			return nil
		}
		// CID 0 renders .notdef, whose width [checkType0NotdefWidth] owns.
		if cid == 0 || covered[cid] {
			continue
		}
		gid := cid
		if cidToGID != nil {
			gid = cidToGID(gid)
		}
		if gid == 0 {
			continue
		}
		// A glyph the program holds no advance for is left to
		// [checkType2MissingGlyphWidths]. An advance of 0 is compared: veraPDF
		// reads it as the glyph's width (BaseTrueTypeProgram.getWidthWithCheck).
		bw, ok := binaryWidths[gid]
		if !ok {
			continue
		}
		expectedWidth := bw * 1000.0 / float64(unitsPerEm)
		if widthsInconsistent(dw, expectedWidth) {
			faults.add(uint32(cid), "font %s on page %d: CID %d "+WidthMismatchContext+" (PDF=%.1f, font=%.1f)",
				fontName, pageNum, cid, dw, expectedWidth)
		}
	}
	return faults.violations()
}

// checkType2MissingGlyphWidths applies the glyph-width rule to the used CIDs a
// TrueType CIDFont's program holds no glyph for ([missingType2CIDs]), which
// [checkCIDFontWidths] and [checkCIDDefaultWidths] leave out for want of an
// advance of their own.
//
// ISO 19005-2, 6.2.11.5 still asks such a glyph's widths to agree, and veraPDF
// weighs one: its TrueType program weighs a glyph index past its glyph count
// that its metrics do not reach at the advance of glyph 0, and gives a CID a
// map stream does not reach the glyph index 0
// (BaseTrueTypeProgram.getWidthWithCheck, CIDToGIDMapping.getGID), against the
// /W or /DW width the dictionary declares (ISO 32000-1, 9.7.4.3). Measured
// with veraPDF 1.30 at PDF/A-1b and PDF/A-2b on CID 256 of a 38-glyph program
// whose glyph 0 advances 443.36: /DW 1000 or no /DW fails ISO 19005-1, 6.3.6
// t1 and ISO 19005-2, 6.2.11.5 t1, /DW 443 or /W [256 [443]] passes, and
// /DW 443 with /W [256 [500]] fails; with glyph 0 advancing 0, /DW 1000 fails
// and /DW 0 passes, so no zero advance is excused here. A CID a map stream
// sends past the program, or does not reach, fails alike.
func checkType2MissingGlyphWidths(ctx context.Context, doc *document.Document, cidFontDict ast.PdfDict, fontData []byte, binaryWidths map[uint16]float64, unitsPerEm uint16, fontName string, pageNum int, rule Rule, usedCIDs map[uint16]bool) []Violation {
	if fontSubtype(ctx, doc, cidFontDict) != "CIDFontType2" {
		return nil
	}
	notdefAdvance, ok := binaryWidths[0]
	if !ok {
		return nil
	}
	mapped, ok := cidToGIDMapped(ctx, doc, cidFontDict)
	if !ok {
		return nil
	}
	expected := notdefAdvance * 1000.0 / float64(unitsPerEm)
	faults := newGlyphFaults(rule)
	for _, cid := range missingType2CIDs(ctx, doc, cidFontDict, fontData, usedCIDs) {
		gid, covered := cid, true
		if mapped != nil {
			gid, covered = mapped(cid)
		}
		// A glyph index the program's metrics still reach carries an advance of
		// its own, which the checks above weigh.
		if _, has := binaryWidths[gid]; covered && has {
			continue
		}
		declared := lookupCIDWidth(ctx, doc, cidFontDict, cid)
		if widthsInconsistent(declared, expected) {
			faults.add(uint32(cid), "font %s on page %d: CID %d names no glyph, "+WidthMismatchContext+" against glyph 0 (PDF=%.1f, font=%.1f)",
				fontName, pageNum, cid, declared, expected)
		}
	}
	return faults.violations()
}

// cidFontType0Glyphs returns whether the CFF program fontData of a
// CIDFontType0 holds a glyph for a CID. ok is false when the program is no CFF
// litho can read.
//
// ISO 32000-1, 9.7.4.2 selects the glyph of a CIDFontType0 in one of two ways.
// A CID-keyed program, whose Top DICT uses the CIDFont operators, maps a CID to
// a glyph index through its charset, so a CID the charset does not list has no
// glyph. A name-keyed program takes the CID as the glyph index itself, so a CID
// at or past the number of its charstrings has none. veraPDF reads both ways
// (CFFCIDFontProgram.containsCID, and CFFType1FontProgram.containsGID through
// PDCIDFont.glyphIsPresent), a program wrapped in OpenType included.
//
// nameKeyed reports the second kind, and defaultWidthX is then the default
// width of its Private DICT, in font units.
//
// Deliberate divergence: CID 0 of a name-keyed program is glyph index 0, the
// .notdef glyph, and litho states ISO 19005-2, 6.2.11.8 t1 for it
// ([checkType0NotdefGlyph]). veraPDF reads CID 0 of such a program through the
// program's own encoding instead, by the code that selects it
// (PDCIDFont.glyphIsPresent, CFFType1FontProgram.containsCode), and finds
// the name .notdef, which its charset holds, so it passes the code. Measured
// with veraPDF 1.30 at PDF/A-2b on <0000> under Identity-H: the .notdef rule
// passes, and a /W width other than glyph 0's advance fails the width rule.
//
// # Corpus evidence
//
//   - pdfjs bug816075.pdf, bug898853.pdf, bug921409.pdf, issue17689.pdf and
//     issue9534_reduced.pdf: embed a name-keyed program under a CIDFontType0,
//     as issue20426.pdf, issue5334.pdf, issue6782.pdf and issue7901.pdf do
//     inside OpenType, and none shows a CID past its program, so no count
//     moves at PDF/A-1b, PDF/A-2b or PDF/A-2u. litho fontclaims_2b.pdf shows
//     one.
func cidFontType0Glyphs(fontData []byte) (holds func(cid uint16) bool, nameKeyed bool, defaultWidthX float64, ok bool) {
	// A program whose CharStrings INDEX is empty reads and holds no glyph,
	// whichever way it is keyed ([emptyCFFGlyphs]): veraPDF reports every CID
	// shown other than 0 under ISO 19005-2, 6.2.11.4.1 t2 and 6.2.11.8 t1,
	// and weighs its width as it weighs any CID the program lacks. Measured
	// with veraPDF 1.30.2 at PDF/A-2b on CIDs 1 and 2: a CID-keyed program
	// fails /W 700 and 500 with no /DW, a name-keyed one under defaultWidthX
	// 700 passes /W 700 and fails 1000 and 0.
	if dw, cidKeyed, empty := cff.EmptyCharStrings(fontData); empty {
		return func(uint16) bool { return false }, !cidKeyed, dw, true
	}
	if charset := typography.ExtractValidCIDs(fontData); charset != nil {
		return func(cid uint16) bool { return charset[cid] }, false, 0, true
	}
	glyphs, defaultWidthX, ok := typography.ExtractNameKeyedCFFGlyphs(fontData)
	if !ok {
		return nil, false, 0, false
	}
	return func(cid uint16) bool { return int(cid) < glyphs }, true, defaultWidthX, true
}

// cffSubsetName reports whether veraPDF reads a CFF program as a subset's,
// which decides the width it weighs a glyph the program lacks at: the part of
// the CIDFont's /BaseFont before the first plus sign is six characters long
// (PDFont.isSubset). The test is looser than the subset tag ISO 32000-1,
// 9.9.2 describes, six uppercase letters and a plus sign ([isSubsetFont]): a
// /BaseFont of six characters and no plus sign passes it.
func cffSubsetName(baseFont string) bool {
	before, _, _ := strings.Cut(baseFont, "+")
	return len(before) == cffSubsetTagLength
}

// cffSubsetTagLength is the length of the subset tag ISO 32000-1, 9.9.2 puts
// before the plus sign of a subset font's name.
const cffSubsetTagLength = 6

// checkCFFMissingGlyphWidths applies the glyph-width rule to the used CIDs a
// CIDFontType0's CFF program holds no glyph for ([cidFontType0Glyphs]), which
// [checkCIDFontWidths] and [checkCIDDefaultWidths] leave out for want of an
// advance of their own. unitsPerEm scales the program's own widths.
//
// ISO 19005-2, 6.2.11.5 asks such a glyph's widths to agree as well, and
// veraPDF weighs one: its CID-keyed CFF program finds no glyph index for a CID
// its charset does not list and answers -1 (CFFCIDFontProgram.getWidth), which
// the glyph takes as the CIDFont's default width, /DW or 1000 (GFGlyph,
// PDCIDFont.getDefaultWidth), against the /W or /DW width the dictionary
// declares (ISO 32000-1, 9.7.4.3). So such a CID fails only where /W states it
// a width other than the default. Measured with veraPDF 1.30 at PDF/A-1b and
// PDF/A-2b on CID 3 of a program whose charset holds CIDs 1 and 2: /DW 1000
// with /W [3 [500]] or /W [3 5 500], /W [3 [0]] with no /DW, and /DW 500 with
// /W [3 [1000]] fail ISO 19005-1, 6.3.6 t1 and ISO 19005-2, 6.2.11.5 t1; /W [3
// [1000]] with no /DW, and /DW 1000 or /DW 500 with no /W, pass; shown only in
// rendering mode 3 it passes.
//
// A name-keyed program past its last glyph answers the same for a subset
// ([cffSubsetName]), whose widths veraPDF reads up front and which has none
// there (CharStringsWidths.getWidth). For any other font it reads the missing
// charstring as empty, which carries no width, and weighs the glyph at the
// defaultWidthX of the Private DICT, scaled by the font matrix. Measured with
// veraPDF 1.30 at PDF/A-1b and PDF/A-2b on CID 3 of a three-glyph program with
// defaultWidthX 0, under /DW 1000: /W [3 [500]] fails whatever the name; no /W
// fails for the CIDFont /BaseFont LithoT1 and passes for ABCDEF+LithoT1 and
// Lith01, and /W [3 [0]] passes for LithoT1 and fails for Lith01; the name of
// the Type0 font does not matter. With defaultWidthX 700, /W [3 [700]] passes
// for LithoT1.
//
// # Corpus evidence
//
//   - govdocs thread_100_100870.pdf: page 20 shows, in a rendering mode no
//     earlier page used, a CID its Times-Roman CFF charset does not hold,
//     which veraPDF fails the width rule for at PDF/A-1b, PDF/A-2b and
//     PDF/A-2u and litho now does too, 1 of veraPDF's 2. The other is a CID
//     whose charstring advances 333, as /W declares, which veraPDF reads as
//     400.
func checkCFFMissingGlyphWidths(ctx context.Context, doc *document.Document, cidFontDict ast.PdfDict, fontData []byte, unitsPerEm uint16, fontName string, pageNum int, rule Rule, usedCIDs map[uint16]bool) []Violation {
	if fontSubtype(ctx, doc, cidFontDict) != "CIDFontType0" {
		return nil
	}
	holds, nameKeyed, defaultWidthX, ok := cidFontType0Glyphs(fontData)
	if !ok {
		return nil
	}
	expected := defaultCIDWidth(ctx, doc, cidFontDict)
	if nameKeyed && !cffSubsetName(fontBaseFont(ctx, doc, cidFontDict)) {
		expected = defaultWidthX * 1000.0 / float64(unitsPerEm)
	}
	faults := newGlyphFaults(rule)
	for _, cid := range sortedCIDs(usedCIDs) {
		// CID 0 is the .notdef glyph every program holds, whose width
		// [checkType0NotdefWidth] owns.
		if cid == 0 || holds(cid) {
			continue
		}
		declared := lookupCIDWidth(ctx, doc, cidFontDict, cid)
		// Out-of-int32-range widths are crafted overflow input, not metrics;
		// see checkSimpleFontWidths.
		if declared < math.MinInt32 || declared > math.MaxInt32 {
			continue
		}
		if widthsInconsistent(declared, expected) {
			faults.add(uint32(cid), "font %s on page %d: CID %d is not in the font charset, "+WidthMismatchContext+" against the default width (PDF=%.1f, font=%.1f)",
				fontName, pageNum, cid, declared, expected)
		}
	}
	return faults.violations()
}

// sortedCIDs returns the keys of set in ascending order, so the violation a
// font raises does not depend on map iteration order.
func sortedCIDs(set map[uint16]bool) []uint16 {
	out := make([]uint16, 0, len(set))
	for cid := range set {
		out = append(out, cid)
	}
	slices.Sort(out)
	return out
}

// extractFontProgramData gets the raw font program bytes from FontDescriptor.
//
// The bytes come from [decodeFontProgram] and are shared with every other
// caller, so they must not be modified.
func extractFontProgramData(ctx context.Context, doc *document.Document, fdDict ast.PdfDict) []byte {
	for _, key := range []ast.PdfName{"FontFile2", "FontFile3", "FontFile"} {
		ffObj, ok := fdDict[key]
		if !ok {
			continue
		}
		stream, err := document.ResolveStream(ctx, doc, ffObj)
		if err != nil {
			doc.Logger.Debug("compliance: fonts: resolving font program stream", "key", key, "error", err)
		}
		if stream.Dict == nil {
			continue
		}
		data, err := decodeFontProgram(ctx, doc, ffObj, stream)
		if err != nil {
			// Fall back to raw data.
			rawData, rawErr := stream.Bytes(ctx)
			if rawErr == nil && len(rawData) > 0 {
				return rawData
			}
			doc.Logger.Debug("compliance: fonts: failed to decode font program stream", "key", key, "error", err)
			continue
		}
		// A program veraPDF's parser cannot finish is no program to it: no rule
		// weighs its glyphs, widths or cmap ([fontProgramReadable]).
		if len(data) > 0 && !fontProgramReadable(key, data) {
			continue
		}
		return data
	}
	return nil
}

// checkType0PerGlyphToUnicode validates that every code shown through a
// Type0 font resolves to a Unicode value via the font's /ToUnicode CMap. The
// requirement is per shown glyph (ISO 14289-1 §7.21.7 t1 for UA-1,
// ISO 14289-2 §8.4.5.8 t1 for UA-2), so it fires alongside the presence and
// notdef rules on the same glyph. A missing /ToUnicode entirely is the
// font-level rule's job; this check covers a present map with gaps.
//
// Text rendering mode 3 grants no exemption here. Every profile spells this
// rule on object Glyph as "toUnicode != null" with no renderingMode term: ISO
// 19005-1 §6.3.8 t1, ISO 19005-2/3 §6.2.11.7.2 t1, ISO 19005-4 §6.2.10.7 t1,
// ISO 14289-1 §7.21.7 t1 and ISO 14289-2 §8.4.5.8 t1. The embedding and
// glyph-presence rules do carry one, each beginning "renderingMode == 3 ||"
// (ISO 14289-2 §8.4.5.5.1 t1 and t2), because those bear on the visual
// representation of a glyph. A Unicode value does not: invisible text is
// exactly what a reader extracts and what assistive technology speaks, which
// is why a scanned page's OCR layer is drawn with 3 Tr. Measured against
// veraPDF on an Identity-H font showing <017F> past its /ToUnicode range,
// which it fails at 8.4.5.8 t1 under both 0 Tr and 3 Tr while passing
// 8.4.5.5.1 under 3 Tr.
//
// # Corpus evidence
//
//   - pdfjs bug898853.pdf: writes Identity-H codes against a /ToUnicode map
//     with a one-byte codespace and a one-byte bfrange covering 9 to 64, which
//     backs reading every Identity code as two bytes.
func checkType0PerGlyphToUnicode(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string, usage *fontUsageInfo) []Violation {
	rule := perGlyphToUnicodeRule(profile)
	if rule.Specification == "" {
		return nil
	}
	if usage == nil || len(usage.rawStrings) == 0 {
		return nil
	}
	tuStream, err := document.ResolveStream(ctx, doc, fontDict["ToUnicode"])
	if err != nil {
		return nil
	}
	data, err := doc.DecodeStream(ctx, tuStream)
	if err != nil {
		return nil
	}
	tu, err := cmap.Parse(ctx, data)
	if err == nil && tu != nil {
		tu = resolveUsedCMaps(ctx, doc, fontDict["ToUnicode"], tuStream.Dict, tu)
	}
	if err != nil || tu == nil || len(tu.Mapping) == 0 {
		return nil
	}
	// A code only a bfrange crossing a first-byte boundary reaches has no
	// Unicode value a conforming reader derives (Adobe Technical Note #5411,
	// 1.4.1, see [cmap.CMap.StrictMapping]), so it counts as unmapped here.
	strict := tu.StrictMapping()

	// The per-glyph ToUnicode rule applies only to a code that resolves to a
	// real renderable glyph. CID 0 is .notdef (covered by the notdef rule),
	// and a CIDFontType0 (CFF) charset-absent CID has no glyph in the program
	// at all. Both are exempt. The exemption is CFF-specific for charset gaps:
	// a CIDFontType2 (TrueType) maps any CID to a GID slot, so even an empty
	// glyph is a renderable slot and still triggers the rule. The absent set
	// is therefore restricted to CFF descendants. For Identity-H/V an unmapped
	// two-byte code is the CID directly, so skip it without disturbing the
	// codespace-aware scan that handles mixed-width maps.
	encName, _ := document.ResolveName(ctx, doc, fontDict["Encoding"])
	identity := encName == "Identity-H" || encName == "Identity-V"
	var cffAbsent map[uint16]bool
	cff := false
	if identity {
		if absent, subtype, _, _ := absentType0CIDs(ctx, doc, fontDict, usage); subtype == "CIDFontType0" {
			cffAbsent = absent
			cff = true
		}
	}

	faults := newGlyphFaults(rule)
	for _, s := range usage.rawStrings {
		b := []byte(s)
		for off := 0; off < len(b); {
			consumed, _, ok := tu.Map(b[off:])
			if consumed <= 0 {
				consumed = 1
			}
			// An Identity CMap makes every character code two bytes (ISO
			// 32000-1, 9.7.5.2), and a /ToUnicode CMap describes those same
			// codes (9.10.3). A one-byte codespace declared there does not
			// shorten the font's codes, but reading the string through it split
			// "\x00\x24" into two one-byte codes and reported the leading zero
			// byte as unmapped. veraPDF keys its /ToUnicode entries by code
			// value (CMap.getUnicode(int)) and never consults that codespace, so
			// it maps 0x0024 through a one-byte bfrange covering 9 to 64.
			if identity {
				consumed = 2
				if off+consumed > len(b) {
					consumed = len(b) - off
				}
				ok = toUnicodeCoversCode(strict, b[off:off+consumed])
			} else if ok {
				_, ok = strict[string(b[off:off+consumed])]
			}
			if !ok {
				// The exemption is CFF-specific. For a CIDFontType0 descendant
				// no glyph in the program for CID 0 (.notdef) or a
				// charset-absent CID, so there is no renderable glyph to test.
				// A CIDFontType2 (TrueType) CID 0 maps to GID 0, a real glyph
				// slot, so it is renderable and the rule still applies.
				skip := false
				if cff && consumed == 2 && off+1 < len(b) {
					cid := uint16(b[off])<<8 | uint16(b[off+1])
					skip = cid == 0 || cffAbsent[cid]
				}
				switch {
				case skip:
				case identity && consumed == 2:
					// Under an Identity CMap the code is the CID, so every such
					// glyph is stated once; see [fontJudgements.glyphs].
					cid := uint32(b[off])<<8 | uint32(b[off+1])
					faults.add(cid, "font %s on page %d: rendered code <%04X> has no ToUnicode mapping", fontName, pageNum, cid)
				default:
					// Any other encoding needs its CMap to name the CID, so the
					// first such code stands for the font.
					return append(faults.violations(), failf(rule, "font %s on page %d: rendered code has no ToUnicode mapping", fontName, pageNum)...)
				}
			}
			off += consumed
		}
	}
	return faults.violations()
}

// toUnicodeCoversCode reports whether a /ToUnicode mapping holds an entry for
// code, matching by code value rather than by byte length. veraPDF keys its
// entries by integer (CMap.getUnicode(int)), so an entry written with one byte
// covers the same code written with leading zero bytes.
func toUnicodeCoversCode(mapping map[string]string, code []byte) bool {
	if _, ok := mapping[string(code)]; ok {
		return true
	}
	trimmed := code
	for len(trimmed) > 1 && trimmed[0] == 0 {
		trimmed = trimmed[1:]
	}
	_, ok := mapping[string(trimmed)]
	return ok
}

// ExpectedSimpleFontWidths returns the authoritative width (in 1/1000 text
// space units) for each charCode of a simple font, using exactly the
// resolution the width check applies: the font program's metrics through
// the cmap or glyph-name mapping, with codes that map to GID 0 carrying the
// .notdef glyph's own advance width. Codes litho cannot map are absent from
// the map and must keep their declared width, since their authoritative
// width is unresolvable. Remediation uses this so the repair and the check
// can never drift.
func ExpectedSimpleFontWidths(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) (map[int]float64, bool) {
	binaryWidths, unitsPerEm, charCodeToGID, ok := simpleFontGlyphMetrics(ctx, doc, fontDict)
	if !ok || charCodeToGID == nil {
		return nil, false
	}
	out := make(map[int]float64, len(charCodeToGID))
	for code, gid := range charCodeToGID {
		bw, ok := binaryWidths[gid]
		if !ok {
			continue
		}
		out[int(code)] = bw * 1000.0 / float64(unitsPerEm)
	}
	return out, true
}
