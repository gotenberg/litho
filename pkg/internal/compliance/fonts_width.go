package compliance

import (
	"context"
	"maps"
	"math"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/typography"
	"github.com/gotenberg/litho/pkg/internal/typography/cmap"
	"github.com/gotenberg/litho/pkg/internal/typography/encodings"
)

// widthTolerance is the maximum allowed difference between PDF-declared
// and binary font widths, in 1/1000 text space units.
const widthTolerance = 1.0

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
func simpleFontGlyphMetrics(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) (map[uint16]uint16, uint16, map[uint16]uint16, bool) {
	fd := document.ResolveFontDescriptor(ctx, doc, fontDict)
	if fd == nil {
		return nil, 0, nil, false
	}

	fontData := extractFontProgramData(ctx, doc, fd)
	if fontData == nil {
		return nil, 0, nil, false
	}

	binaryWidths := typography.ExtractBinaryWidths(ctx, fontData)
	if binaryWidths == nil {
		return nil, 0, nil, false
	}

	unitsPerEm := typography.ReadUnitsPerEm(fontData)
	if unitsPerEm == 0 {
		unitsPerEm = 1000
	}

	// Extract charCode → GID mapping from font's cmap table (TrueType/OTTO).
	charCodeToGID := typography.ExtractCharCodeToGID(ctx, fontData)

	// A symbolic TrueType font carries no PDF /Encoding; its single-byte codes
	// resolve through the embedded (3,0)/(1,0) cmap (ISO 32000-1 §9.6.6.4), not
	// through glyph names. When that cmap is absent the codes are unresolvable,
	// so an empty (non-nil) map makes the width and presence checks skip them
	// rather than misreport against an empty encoding or an identity GID.
	if charCodeToGID == nil && isSymbolicFont(ctx, doc, fd) && fontDict["Encoding"] == nil {
		sym := typography.ExtractSymbolCharCodeToGID(ctx, fontData)
		if sym == nil {
			sym = map[uint16]uint16{}
		}
		return binaryWidths, unitsPerEm, sym, true
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
					if name == ".notdef" || name == "" {
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
		}
	}

	return binaryWidths, unitsPerEm, charCodeToGID, true
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
// cannot read). A code is resolved to a glyph name through the program's own
// built-in encoding (subset fonts routinely map codes to synthetic G-names the
// PDF /Encoding's standard names do not cover), and the charstring advance is
// scaled by the /FontMatrix to reach the 1000-unit text space of /Widths. A
// glyph the program does not define is skipped so an incomplete map cannot
// produce a false positive.
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
	builtin := typography.ExtractType1BuiltinEncoding(fontData)
	pdfEnc := resolveSimpleFontEncoding(ctx, doc, fontDict)
	scale, _ := typography.ExtractType1FontMatrixScale(fontData)

	widths, err := document.ResolveArray(ctx, doc, fontDict["Widths"])
	if err != nil || len(widths) == 0 {
		return nil
	}
	firstChar, err := document.ResolveInt(ctx, doc, fontDict["FirstChar"])
	if err != nil {
		return nil
	}

	for _, code := range sortedByteKeys(usage.charCodes) {
		if code == 0 {
			continue
		}
		idx := int(code) - int(firstChar)
		if idx < 0 || idx >= len(widths) {
			continue
		}
		// Prefer the PDF /Encoding name when the program defines a glyph for it;
		// otherwise fall back to the program's built-in name for the code (the
		// G-name subset case). Skip a code that resolves to no defined glyph.
		name := pdfEnc[code]
		if _, ok := nameWidths[name]; name == "" || name == ".notdef" || !ok {
			name = builtin[code]
		}
		rawWidth, ok := nameWidths[name]
		if !ok || name == ".notdef" {
			continue
		}
		expected := rawWidth * scale * 1000.0
		pdfWidth, wOK := widthValue(ctx, doc, widths[idx])
		if !wOK {
			continue
		}
		if pdfWidth < math.MinInt32 || pdfWidth > math.MaxInt32 {
			continue
		}
		if math.Abs(pdfWidth-expected) > widthTolerance {
			return failf(rule, "font %s on page %d: char %d width mismatch (PDF=%.1f, font=%.1f)",
				fontName, pageNum, code, pdfWidth, expected)
		}
	}
	return nil
}

// checkSimpleFontWidths compares /Widths array against binary widths for simple fonts.
// charCodeToGID is the cmap-derived charCode → GID mapping (may be nil for non-TrueType).
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

func checkSimpleFontWidths(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, binaryWidths map[uint16]uint16, unitsPerEm uint16, fontName string, pageNum int, rule Rule, charCodeToGID map[uint16]uint16, usage *fontUsageInfo) []Violation {
	// Width consistency is a property of a glyph the document actually
	// paints: the rule compares the program advance against the declared
	// width only when both are resolvable and the glyph is rendered
	// visibly (rendering mode != 3). A missing /Widths or /FirstChar makes
	// the declared width unresolvable, so it falls to the font-dict key
	// rules rather than this one, and a font used only invisibly is exempt.
	if usage == nil || len(usage.charCodes) == 0 || !usage.visible {
		return nil
	}

	widthsObj, hasWidths := fontDict["Widths"]
	if !hasWidths {
		return nil
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
	if usage.charCodes[0] {
		if v := checkSimpleFontCode0Width(ctx, doc, fontDict, binaryWidths, unitsPerEm, fontName, pageNum, rule, charCodeToGID, int(firstChar), widths); v != nil {
			return v
		}
	}

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
		expectedWidth := float64(bw) * 1000.0 / float64(unitsPerEm)
		if math.Abs(pdfWidth-expectedWidth) > widthTolerance {
			return failf(rule, "font %s on page %d: char %d width mismatch (PDF=%.1f, font=%.1f)",
				fontName, pageNum, charCode, pdfWidth, expectedWidth)
		}
	}

	return nil
}

// checkSimpleFontCode0Width applies the glyph-width rule to a rendered code 0.
// The main /Widths loop skips code 0 (it often falls below /FirstChar and so is
// absent from the array), but a code 0 that is shown is a painted glyph and is
// width-checked: it resolves the glyph the same way as any code (charCode → GID,
// falling back to GID 0 /.notdef when the code maps to no glyph name) and
// compares the program advance against the declared width. The declared width is
// /Widths[0-FirstChar] when code 0 is covered by the array, otherwise
// /MissingWidth (default 0, ISO 32000-1 Table 111).
func checkSimpleFontCode0Width(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, binaryWidths map[uint16]uint16, unitsPerEm uint16, fontName string, pageNum int, rule Rule, charCodeToGID map[uint16]uint16, firstChar int, widths []ast.PdfObject) []Violation {
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

	expectedWidth := float64(bw) * 1000.0 / float64(unitsPerEm)
	if math.Abs(pdfWidth-expectedWidth) > widthTolerance {
		return failf(rule, "font %s on page %d: char 0 width mismatch (PDF=%.1f, font=%.1f)",
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
	_, _, charCodeToGID, ok := simpleFontGlyphMetrics(ctx, doc, fontDict)
	if !ok || charCodeToGID == nil {
		return nil
	}
	for code := range usage.charCodes {
		gid, mapped := charCodeToGID[uint16(code)]
		if mapped && gid == 0 {
			return failf(rule, "font %s on page %d: glyph for charCode %d not present in embedded font program", fontName, pageNum, code)
		}
	}
	return nil
}

// checkType3GlyphPresence validates that every character code a Type3 font
// shows names a glyph procedure the font defines. Per ISO 32000-1 §9.6.5 a
// Type3 glyph is looked up by mapping the code through /Encoding to a name and
// that name through /CharProcs to a procedure; a code the encoding leaves
// undefined, or one naming an absent procedure, paints nothing, which is the
// same "no glyph in the program" condition the presence rule states. veraPDF
// reports it under ISO 19005-2 §6.2.11.4.1 t2: pdfjs Type3WordSpacing.pdf
// shows a space its font defines no procedure for.
//
// A font whose /CharProcs or /Encoding will not resolve is left unchecked,
// since presence is then unanswerable rather than failed.
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

	for _, code := range codes {
		glyphName := encoding[code]
		if glyphName == "" {
			return failf(rule, "font %s on page %d: charCode %d is not defined by the Type3 /Encoding",
				fontName, pageNum, code)
		}
		if _, ok := charProcs[ast.PdfName(glyphName)]; !ok {
			return failf(rule, "font %s on page %d: glyph %s for charCode %d has no /CharProcs entry",
				fontName, pageNum, glyphName, code)
		}
	}
	return nil
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
	unitsPerEm := typography.ReadUnitsPerEm(fontData)
	if unitsPerEm == 0 {
		unitsPerEm = 1000
	}

	out := make(map[int]int, len(binaryWidths))
	for cid, bw := range binaryWidths {
		out[int(cid)] = int(math.Round(float64(bw) * 1000.0 / float64(unitsPerEm)))
	}
	return out, true
}

// checkType3FontWidths compares /Widths against glyph widths declared by
// d0/d1 operators in Type3 font CharProcs.
func checkType3FontWidths(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, rule Rule, usage *fontUsageInfo) []Violation {
	widthsObj, hasWidths := fontDict["Widths"]
	if !hasWidths {
		return nil
	}
	widths, err := document.ResolveArray(ctx, doc, widthsObj)
	if err != nil || len(widths) == 0 {
		return nil
	}
	firstChar, err := document.ResolveInt(ctx, doc, fontDict["FirstChar"])
	if err != nil {
		doc.Logger.Debug("compliance: cannot resolve FirstChar", "error", err)
		return nil
	}

	// Build charCode→glyphName from /Encoding /Differences.
	enc := resolveSimpleFontEncoding(ctx, doc, fontDict)

	// Resolve CharProcs dict.
	charProcs, err := document.ResolveDict(ctx, doc, fontDict["CharProcs"])
	if err != nil || charProcs == nil {
		return nil
	}

	// Both the /Widths entries and the d0/d1 wx operands of a Type 3 font are
	// expressed in glyph space (ISO 32000-1 §9.6.5.4). The FontMatrix maps
	// glyph space to text space; normalise BOTH widths to text space (1/1000
	// units) before comparing, so the 1-unit tolerance is applied in the same
	// space veraPDF uses. Scaling only one operand (the original bug) turned a
	// consistent font into a spurious ~x1000 mismatch. Standard FontMatrix
	// [0.001 0 0 0.001 0 0] yields scale 1; a [1 0 0 -1 0 0] matrix yields 1000.
	fmScale := 1.0
	if fmArr, err := document.ResolveArray(ctx, doc, fontDict["FontMatrix"]); err == nil && len(fmArr) >= 1 {
		if sx := document.Float(fmArr[0]); sx != 0 {
			fmScale = math.Abs(sx) * 1000.0
		}
	}

	// Extract widths from each CharProc by finding d0/d1 operator.
	charProcWidths := make(map[string]float64)
	for glyphName, cpObj := range charProcs {
		stream, err := document.ResolveStream(ctx, doc, cpObj)
		if err != nil {
			doc.Logger.Debug("compliance: cannot resolve CharProc stream", "error", err)
			continue
		}
		streamData, err := doc.DecodeStream(ctx, stream)
		if err != nil {
			doc.Logger.Debug("compliance: cannot decode CharProc stream", "error", err)
			continue
		}
		ops, err := content.ParseStreamData(ctx, streamData)
		if err != nil {
			doc.Logger.Debug("compliance: cannot parse CharProc stream", "error", err)
			continue
		}
		for _, op := range ops {
			// d1: wx wy llx lly urx ury d1
			// d0: wx wy d0
			if (op.Operator == "d1" && len(op.Operands) >= 6) ||
				(op.Operator == "d0" && len(op.Operands) >= 2) {
				wx := document.Float(op.Operands[0])
				charProcWidths[string(glyphName)] = wx
				break
			}
		}
	}

	// Compare /Widths against CharProc widths.
	for i, wObj := range widths {
		charCode := byte(int(firstChar) + i) //nolint:gosec // G115: simple-font char codes occupy the 8-bit space; a /Widths index past 255 is not a valid code and truncation is harmless.
		// Width consistency applies only to glyphs referenced for rendering:
		// veraPDF checks only shown codes. An unrendered /Widths entry (e.g. a
		// code mapped to a .notdef placeholder such as /g0 with declared width 0,
		// whose CharProc still carries a non-zero d0/d1 advance) is not compared.
		// A nil usage map falls back to checking every code.
		if usage != nil && !usage.charCodes[charCode] {
			continue
		}
		glyphName := enc[charCode]
		if glyphName == "" {
			continue
		}
		cpWidth, ok := charProcWidths[glyphName]
		if !ok {
			continue
		}
		pdfWidth, wOK := widthValue(ctx, doc, wObj)
		if !wOK {
			continue
		}
		pdfWidth *= fmScale
		glyphWidth := cpWidth * fmScale
		if math.Abs(pdfWidth-glyphWidth) > widthTolerance {
			return failf(rule, "font %s on page %d: char %d (%s) width mismatch (PDF=%.1f, glyph=%.1f)",
				fontName, pageNum, int(firstChar)+i, glyphName, pdfWidth, glyphWidth)
		}
	}
	return nil
}

// checkCIDFontWidths compares /W array against binary widths for CIDFonts.
//
// The comparison is usage-scoped to match veraPDF, which resolves every shown
// code to a GID and checks only those glyphs (see [checkType0NotdefWidth]). A
// /W entry for a CID the document never renders is not compared: its glyph may
// legitimately carry a different advance (a subset placeholder, a zero-advance
// combining glyph) without affecting any drawn text. usedCIDs nil means usage
// could not be collected, so every declared CID is checked as a fail-safe.
func checkCIDFontWidths(ctx context.Context, doc *document.Document, cidFontDict ast.PdfDict, binaryWidths map[uint16]uint16, unitsPerEm uint16, fontName string, pageNum int, rule Rule, usedCIDs map[uint16]bool) []Violation {
	wObj, ok := cidFontDict["W"]
	if !ok {
		return nil
	}

	wArr, err := document.ResolveArray(ctx, doc, wObj)
	if err != nil {
		doc.Logger.Debug("compliance: fonts: resolving CID width array", "font", fontName, "error", err)
		return nil
	}
	if wArr == nil {
		return nil
	}

	// A CIDFontType2 maps CID→GID through /CIDToGIDMap; the embedded program's
	// per-GID widths must be looked up by the mapped GID, not the CID. Assuming
	// Identity here misreads a non-Identity map's widths and fires a spurious
	// mismatch (the map is authoritative, veraPDF follows it). A nil lookup means
	// Identity (CID==GID); ok=false means the map stream is unreadable, so the
	// GID is unknowable and the check is skipped rather than guessed.
	cidToGID, ok := cidToGIDMapping(ctx, doc, cidFontDict)
	if !ok {
		return nil
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
				// Usage-scoped: skip CIDs the document never renders.
				if usedCIDs != nil && (cid < 0 || cid > 0xFFFF || !usedCIDs[uint16(cid)]) {
					continue
				}
				gid := uint16(cid) //nolint:gosec // G115: cid is a CIDFont CID within the 16-bit CID space.
				if cidToGID != nil {
					gid = cidToGID(gid)
				}
				// A CID mapping to GID 0 renders .notdef; glyph presence is the
				// coverage rule's concern, not width consistency.
				if gid == 0 {
					continue
				}
				pdfWidth, wOK := widthValue(ctx, doc, wEntry)
				if !wOK || pdfWidth == 0 {
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
				expectedWidth := float64(bw) * 1000.0 / float64(unitsPerEm)
				if math.Abs(pdfWidth-expectedWidth) > widthTolerance {
					return failf(rule, "font %s on page %d: CID %d width mismatch (PDF=%.1f, font=%.1f)",
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
			pdfWidth, wOK := widthValue(ctx, doc, wArr[i])
			i++
			if !wOK || pdfWidth == 0 {
				continue
			}
			// Out-of-int32-range widths are crafted overflow input,
			// not metrics; see checkSimpleFontWidths.
			if pdfWidth < math.MinInt32 || pdfWidth > math.MaxInt32 {
				continue
			}
			for cid := cidStart; cid <= cidEnd; cid++ {
				// Skip .notdef (CID 0), no visible outlines.
				if cid == 0 {
					continue
				}
				// Usage-scoped: skip CIDs the document never renders.
				if usedCIDs != nil && (cid < 0 || cid > 0xFFFF || !usedCIDs[uint16(cid)]) {
					continue
				}
				gid := uint16(cid) //nolint:gosec // G115: cid is a CIDFont CID within the 16-bit CID space.
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
				expectedWidth := float64(bw) * 1000.0 / float64(unitsPerEm)
				if math.Abs(pdfWidth-expectedWidth) > widthTolerance {
					return failf(rule, "font %s on page %d: CID %d width mismatch (PDF=%.1f, font=%.1f)",
						fontName, pageNum, cid, pdfWidth, expectedWidth)
				}
			}
		default:
			i++
		}
	}

	return nil
}

// extractFontProgramData gets the raw font program bytes from FontDescriptor.
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
		data, err := doc.DecodeStream(ctx, stream)
		if err != nil {
			// Fall back to raw data.
			rawData, rawErr := stream.Bytes(ctx)
			if rawErr == nil && len(rawData) > 0 {
				return rawData
			}
			doc.Logger.Debug("compliance: fonts: failed to decode font program stream", "key", key, "error", err)
			continue
		}
		return data
	}
	return nil
}

// checkType0PerGlyphToUnicode validates that every code rendered through a
// Type0 font resolves to a Unicode value via the font's /ToUnicode CMap. The
// requirement is per painted glyph (ISO 14289-1 §7.21.7 t1 for UA-1,
// ISO 14289-2 §8.4.5.8 t1 for UA-2), so it fires alongside the presence and
// notdef rules on the same glyph. A missing /ToUnicode entirely is the
// font-level rule's job; this check covers a present map with gaps.
func checkType0PerGlyphToUnicode(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string, usage *fontUsageInfo) []Violation {
	rule := perGlyphToUnicodeRule(profile)
	if rule.Specification == "" {
		return nil
	}
	if usage == nil || len(usage.rawStrings) == 0 || !usage.visible {
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
	if err != nil || tu == nil || len(tu.Mapping) == 0 {
		return nil
	}

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

	for _, s := range usage.rawStrings {
		b := []byte(s)
		for off := 0; off < len(b); {
			consumed, _, ok := tu.Map(b[off:])
			if consumed <= 0 {
				consumed = 1
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
				if !skip {
					return failf(rule, "font %s on page %d: rendered code has no ToUnicode mapping", fontName, pageNum)
				}
			}
			off += consumed
		}
	}
	return nil
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
		out[int(code)] = float64(bw) * 1000.0 / float64(unitsPerEm)
	}
	return out, true
}
