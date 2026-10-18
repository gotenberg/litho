package typography

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/typography/cff"
	"github.com/gotenberg/litho/pkg/internal/typography/cmap"
	"github.com/gotenberg/litho/pkg/internal/typography/encodings"
	"github.com/gotenberg/litho/pkg/internal/typography/stdfonts"
	"github.com/gotenberg/litho/pkg/internal/typography/truetype"
	"github.com/gotenberg/litho/pkg/internal/typography/type1"
)

// Font represents a resolved PDF font.
//
// A Font is read-only once [ResolveFont] returns. [document.Document] caches
// one per font object number and hands the same pointer to every caller, so a
// write to a field, or to one of the maps a field holds, is visible to every
// other holder and to the concurrent readers the document's contract permits.
// Resolve a new Font rather than editing a resolved one. [cmap.CMap] and the
// buffer [document.Document.CachedFontProgram] returns carry the same
// contract for the same reason.
type Font struct {
	Subtype            string
	Encoding           map[byte]rune
	ToUnicode          *cmap.CMap
	FirstChar          int                // Simple fonts: first defined char code
	LastChar           int                // Simple fonts: last defined char code
	Widths             []float64          // Simple fonts: Widths[charCode-FirstChar] in 1/1000 text-space units
	MissingWidth       float64            // Fallback width for codes outside the defined range
	DWStated           bool               // CIDFont (Type0): the CIDFont dictionary states /DW
	CIDWidths          map[int]float64    // CIDFont (Type0): CID → width in 1/1000 units
	FontMatrix         [6]float64         // Type 3: maps glyph space to text space (default: [0.001 0 0 0.001 0 0])
	Type3Widths        map[string]float64 // Type 3: glyph name → width from d0/d1 operators (in glyph space)
	GIDToUnicode       map[uint16]rune    // Glyph ID → Unicode rune from embedded TrueType/OpenType cmap
	HmtxWidths         map[uint16]uint16  // GID → advance width from TrueType hmtx/CFF charstring (in font design units)
	UnitsPerEm         uint16             // Font design units per em (from head table; default 1000)
	AvgWidth           uint16             // OS/2 xAvgCharWidth (in font design units; 0 if unavailable)
	FontBBox           [4]float64         // [xMin yMin xMax yMax] from /FontDescriptor /FontBBox
	WMode              int                // 0 = horizontal (default), 1 = vertical
	DW2                [2]float64         // Default vertical metrics [vY w1] (default: [880 -1000])
	CIDVerticalMetrics map[int][3]float64 // CID → [w1Y vX vY] from /W2 array

	// encodingCIDs maps a Type 0 font's character codes to the CIDs its /W
	// and /W2 arrays are indexed by, when its /Encoding is an embedded or a
	// predefined non-Identity CMap; nil otherwise. See [Font.metricCID].
	encodingCIDs *encodingCIDs

	// widthStated marks the [Font.Widths] entries a simple font's /Widths
	// array states as numbers, index for index; nil when [Font.Widths] came
	// from elsewhere ([injectStandard14Widths]). It tells a stated width of
	// 0 from an entry litho could not read. See [Font.simpleWidth].
	widthStated []bool

	CIDIdentityFallback bool // True when CIDToGIDMap is /Identity and no ToUnicode/embedded font
	CIDOffset           int  // Offset to add to CID values before treating as Unicode (detected from /W array)

	// type3BBoxHeight is the height of a Type 3 font's bounding box in glyph
	// space, and type3CharBBox reports that it was read off the glyphs
	// because /FontBBox is empty ([resolveType3Font]). See
	// [Font.type3HeightScale].
	type3BBoxHeight float64
	type3CharBBox   bool

	// SuppressExtraction marks a reference-only Type0 font that ISO
	// 32000-1, 9.10.2 gives no route to Unicode: a CMap name outside
	// Table 118, or an Adobe CJK collection litho holds no UCS2 table for.
	// [decodeNextChar] consumes the CID width and emits no element.
	SuppressExtraction bool

	// LigatureOverrides maps a byte code to a pre-decoded multi-rune
	// string for /Differences entries that declare composite glyph
	// names like "f_t" or "T_h". The regular [Font.Encoding] map can
	// only hold a single rune per byte, so glyph decoding consults this
	// map first and returns the stored string verbatim when present.
	LigatureOverrides map[byte]string

	// ProgramGlyphRunes holds the characters the embedded program of a
	// symbolic TrueType font that states no /Encoding gives its codes
	// ([applySymbolicTrueTypeGlyphs]), and only those; nil for any other
	// font. [Font.Encoding] also carries the default encoding's reading of
	// every code the program leaves unnamed, a guess a caller that persists
	// a mapping must not write.
	ProgramGlyphRunes map[byte]rune
}

// GlyphWidth returns the advance width (in 1/1000 text-space units) for the
// given char code using a strict 5-step fallback chain:
//
//   - PDF width arrays: /Widths (simple) or /W (CID). Type 3 uses /CharProcs.
//   - CID default width: /DW from the CIDFont (defaults to 1000 per spec).
//   - Simple font /MissingWidth from the /FontDescriptor.
//   - TrueType binary hmtx: read advance width from the embedded font program.
//   - /FontBBox average, then absolute fallback of 1000 (= 1 em).
//
// A /W entry of 0 is a width and stands: only the CIDs /W omits fall through.
// So is a /Widths entry of 0: only the entries that are not numbers fall
// through ([Font.simpleWidth]).
//
// # Corpus evidence
//
//   - pdfjs complex_ttf_font.pdf: combining marks carry /W 0 under /DW 1000;
//     reading 1000 clipped 85 of 395 characters off the page.
//   - pdfjs, govdocs, pdfcpu, facturx, pdf20: honouring /W 0 changes three
//     files, all toward PDF.js (complex_ttf_font, bug1046314,
//     ThuluthFeatures), and no file moves away from it.
//   - pdfjs issue7855.pdf: a Calibri subset states /Widths 0 for 154 codes,
//     the space among them. Read at a fallback width, they pushed the tail
//     of "28.11.2016" past the page clip, and litho read "28.11.2".
//   - pdfjs issue18208.pdf: 72 Helvetica-Bold dictionaries state /Widths 0
//     for 246 of their codes. Read at a fallback width, those glyphs ran the
//     words together ("TorredeMonsanto") where PDF.js reads the spaces.
func (f *Font) GlyphWidth(charCode int) float64 {
	// Type 3 fonts: look up glyph name via encoding, then apply FontMatrix.
	if f.Type3Widths != nil && charCode >= 0 && charCode <= 255 {
		if r, ok := f.Encoding[byte(charCode)]; ok { //nolint:gosec // charCode bounds-checked above
			glyphName := encodings.RuneToGlyph(r)
			if w, ok := f.Type3Widths[glyphName]; ok && w > 0 {
				return w * f.FontMatrix[0] * 1000
			}
		}
		// A Type 3 /Widths entry is in glyph space (ISO 32000-1, 9.6.5,
		// Table 112), so the FontMatrix maps it to text space as it maps
		// the d0/d1 width above.
		idx := charCode - f.FirstChar
		if idx >= 0 && idx < len(f.Widths) && f.Widths[idx] > 0 {
			return f.Widths[idx] * f.FontMatrix[0] * 1000
		}
		if f.MissingWidth > 0 {
			return f.MissingWidth
		}
		return f.resolveWidth(charCode)
	}

	// CIDFont (Type0): check /W array.
	if f.CIDWidths != nil {
		// A /W entry of 0 is a width, not an omission: ISO 32000-1, 9.7.4.3
		// gives /DW only to the CIDs /W does not list, and PDF.js keeps a
		// listed 0 (src/core/evaluator.js extractWidths, fonts.js charToGlyph).
		// Combining marks carry it, and reading /DW for them pushes the rest
		// of the line past the page clip.
		if w, ok := f.CIDWidths[charCode]; ok && w >= 0 {
			return w
		}
		// Try hmtx before an implied DW: many CIDFonts leave /DW to its
		// default of 1000 while the embedded font program has the real
		// per-glyph widths, and the binary hmtx widths reconstruct their
		// spacing much better. A stated /DW is the width ISO 32000-1,
		// 9.7.4.3 gives every CID /W omits, and PDF.js reads it, so it wins:
		// the hmtx lookup indexes by CID, which names another glyph when a
		// /CIDToGIDMap stream reorders them (pdfjs issue19494.pdf, /DW 556,
		// read 278 for "e" and spaced every word).
		if !f.DWStated && f.HmtxWidths != nil && f.UnitsPerEm > 0 {
			gid := uint16(charCode) //nolint:gosec // CID≈GID for Identity-mapped CIDFonts
			if advance, ok := f.HmtxWidths[gid]; ok && advance > 0 {
				return float64(advance) * 1000.0 / float64(f.UnitsPerEm)
			}
		}
		// CID default width (/DW, defaults to 1000 per ISO 32000).
		if f.MissingWidth > 0 {
			return f.MissingWidth
		}
		return f.resolveWidth(charCode)
	}

	// Simple fonts: /Widths array.
	if w, ok := f.simpleWidth(charCode); ok {
		return w
	}

	if f.MissingWidth > 0 {
		return f.MissingWidth
	}

	return f.resolveWidth(charCode)
}

// simpleWidth returns the /Widths entry of a simple font for charCode and
// whether it gives the code a width. ISO 32000-1, 9.6.2.1, Table 111 gives
// /Widths the width of every code from /FirstChar to /LastChar and leaves
// /MissingWidth to the codes outside that range, so an entry the array states
// as 0 is a width, not an omission. PDF.js keeps it too (src/core/evaluator.js
// extractWidths, src/core/fonts.js _charToGlyph): only an entry that is not a
// number falls to the default width. An entry [injectStandard14Widths] left
// at 0 states nothing, and falls through as before.
func (f *Font) simpleWidth(charCode int) (float64, bool) {
	idx := charCode - f.FirstChar
	if idx < 0 || idx >= len(f.Widths) {
		return 0, false
	}
	w := f.Widths[idx]
	if w > 0 {
		return w, true
	}
	return 0, w == 0 && idx < len(f.widthStated) && f.widthStated[idx]
}

// resolveWidth implements steps 4-5 of the fallback chain.
func (f *Font) resolveWidth(charCode int) float64 {
	if f.HmtxWidths != nil && f.UnitsPerEm > 0 {
		gid := uint16(charCode) //nolint:gosec // charCode is a glyph index in this context
		if advance, ok := f.HmtxWidths[gid]; ok && advance > 0 {
			// Convert from font design units to 1/1000 text-space units.
			return float64(advance) * 1000.0 / float64(f.UnitsPerEm)
		}
	}

	bboxWidth := f.FontBBox[2] - f.FontBBox[0] // xMax - xMin
	if bboxWidth > 0 {
		return bboxWidth
	}

	// OS/2 xAvgCharWidth provides a better fallback than a hard-coded 1 em.
	if f.AvgWidth > 0 {
		return float64(f.AvgWidth) * 1000.0 / float64(max(f.UnitsPerEm, 1))
	}

	return 1000 // 1 em, prevents zero-width glyphs
}

// VerticalMetrics returns the vertical displacement vector (vX, vY) and the
// vertical advance width (w1) for a given CID, in 1/1000 text-space units.
// Per PDF spec Table 117:
//   - w1  = vertical advance (negative = downward)
//   - vX  = X component of the position vector
//   - vY  = Y component of the position vector
func (f *Font) VerticalMetrics(charCode int) (w1, vX, vY float64) {
	if m, ok := f.CIDVerticalMetrics[charCode]; ok {
		return m[0], m[1], m[2]
	}
	// Default: vX = half the horizontal width, vY and w1 from DW2
	halfW0 := f.GlyphWidth(charCode) / 2.0
	return f.DW2[1], halfW0, f.DW2[0]
}

// maxCIDValue is the largest CID a CIDFont can address. Per ISO 32000-1,
// 9.7.4.3 a CID is a 16-bit value, so a /W or /W2 range naming a larger one
// describes glyphs no font can hold.
//
// This is a spec bound doing anti-DoS duty, because the range forms expand to
// one map entry per CID they name and nothing else stops them. Measured before
// it existed: /W2 [0 5000000 ...] built 5,000,001 entries costing 640 MB, which
// extrapolates to roughly 275 GB for the 2147483647 a crafted file can state,
// from about 40 bytes of array. pkg/internal/compliance/file_limits.go records
// the same limit as maxCID for the validator's own use.
const maxCIDValue = 65535

// parseCIDVerticalWidths decodes a /W2 array into a flat CID → [w1Y, vX, vY] map.
// The /W2 array uses similar forms to /W but with 3 values per CID:
//   - Individual form: c1 [w1_1 vx_1 vy_1 w1_2 vx_2 vy_2 ...]
//   - Range form:      c1 c2 w1 vx vy
func parseCIDVerticalWidths(w2Arr ast.PdfArray) map[int][3]float64 {
	metrics := make(map[int][3]float64)
	i := 0
	for i < len(w2Arr) {
		if i+1 >= len(w2Arr) {
			break
		}
		startCID := int(document.Float(w2Arr[i]))
		i++
		switch next := w2Arr[i].(type) {
		case ast.PdfArray: // individual form: c1 [w1_1 vx_1 vy_1 ...]
			for j := 0; j+2 < len(next); j += 3 {
				metrics[startCID+j/3] = [3]float64{
					document.Float(next[j]),
					document.Float(next[j+1]),
					document.Float(next[j+2]),
				}
			}
			i++
		case ast.PdfInt, ast.PdfReal: // range form: c1 c2 w1 vx vy
			endCID := int(document.Float(next))
			i++
			if i+2 >= len(w2Arr) {
				break
			}
			w1 := document.Float(w2Arr[i])
			vx := document.Float(w2Arr[i+1])
			vy := document.Float(w2Arr[i+2])
			i += 3
			// The range is file-supplied and sizes nothing but this loop, so
			// bound it to the CID space before walking it.
			lo, hi := startCID, endCID
			if lo < 0 {
				lo = 0
			}
			if hi > maxCIDValue {
				hi = maxCIDValue
			}
			for cid := lo; cid <= hi; cid++ {
				metrics[cid] = [3]float64{w1, vx, vy}
			}
		default:
			i++
		}
	}
	return metrics
}

// resolveMetricsArray returns a copy of a /W, /W2 or /Widths array whose elements,
// and the elements of the arrays it nests, are direct objects. ISO 32000-1,
// 7.3.10 lets any array element be an indirect reference, and a /W array
// whose individual form holds its widths in an indirect array, [0 14 0 R],
// is one producers write. The parsers read direct objects only, so without
// this they skipped such an entry and every CID it covered fell back to the
// default width. Only the two levels the /W and /W2 forms define are
// resolved, so the walk needs no depth bound, and an element that does not
// resolve is kept as it was, which the parsers skip.
//
// # Corpus evidence
//
//   - pdfjs issue16300.pdf: Nunito CIDFonts state /W [0 14 0 R], and each
//     glyph took /DW 1000, so "SIRET N°" read "SIRETN°" where PDF.js reads
//     the widths and the space.
func resolveMetricsArray(ctx context.Context, doc *document.Document, arr ast.PdfArray) ast.PdfArray {
	resolve := func(obj ast.PdfObject) ast.PdfObject {
		if _, ok := obj.(ast.PdfIndirectRef); !ok {
			return obj
		}
		resolved, err := document.ResolveObject(ctx, doc, obj)
		if err != nil {
			doc.Logger.Debug("resolving CIDFont metrics entry", slog.Any("err", err))
			return obj
		}
		return resolved
	}
	out := make(ast.PdfArray, len(arr))
	for i, obj := range arr {
		obj = resolve(obj)
		if nested, ok := obj.(ast.PdfArray); ok {
			direct := make(ast.PdfArray, len(nested))
			for j, el := range nested {
				direct[j] = resolve(el)
			}
			obj = direct
		}
		out[i] = obj
	}
	return out
}

// parseCIDWidths decodes a CIDFont /W array into a flat CID → width map.
// The /W array alternates between two forms:
//   - Range form:      c1 c2 w   (all CIDs c1..c2 share width w)
//   - Individual form: c1 [w1 w2 ...]  (sequential CIDs starting at c1)
func parseCIDWidths(wArr ast.PdfArray) map[int]float64 {
	widths := make(map[int]float64)
	i := 0
	for i < len(wArr) {
		if i+1 >= len(wArr) {
			break
		}
		startCID := int(document.Float(wArr[i]))
		i++
		switch next := wArr[i].(type) {
		case ast.PdfArray: // individual form: c1 [w1 w2 ...]
			for j, wObj := range next {
				widths[startCID+j] = document.Float(wObj)
			}
			i++
		case ast.PdfInt, ast.PdfReal: // range form: c1 c2 w
			endCID := int(document.Float(next))
			i++
			if i >= len(wArr) {
				break
			}
			w := document.Float(wArr[i])
			i++
			// The range is file-supplied and sizes nothing but this loop, so
			// bound it to the CID space before walking it.
			lo, hi := startCID, endCID
			if lo < 0 {
				lo = 0
			}
			if hi > maxCIDValue {
				hi = maxCIDValue
			}
			for cid := lo; cid <= hi; cid++ {
				widths[cid] = w
			}
		default:
			i++
		}
	}
	return widths
}

// copyEncoding creates an independent copy of an encoding map.
func copyEncoding(src map[byte]rune) map[byte]rune {
	dst := make(map[byte]rune, len(src))
	maps.Copy(dst, src)
	return dst
}

// ResolveFont extracts and resolves a [Font] from a PDF font dictionary.
//
// # Corpus evidence
//
//   - govdocs: fonts that ship only a Mac Roman cmap subtable are common in
//     its older Mac-authored scanned filings.
//   - pdfjs noembed-eucjp.pdf, noembed-sjis.pdf, noembed-jis7.pdf,
//     noembed-identity.pdf, noembed-identity-2.pdf: reference-only Adobe-Japan1
//     Type0 fonts under a predefined or Identity CMap. Read through 9.10.2,
//     their text matches PDF.js given its CMaps. Suppressed, it matched only
//     the text goldens PDF.js wrote without them, which were empty.
//   - All corpora: 29 files change text once unembedded composite fonts read
//     through 9.10.2, 22 of them pdfjs files that then match PDF.js, and none
//     moves away from it.
func ResolveFont(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) (*Font, error) {
	subtypeObj, ok := fontDict["Subtype"]
	if !ok {
		return nil, fmt.Errorf("font dictionary missing /Subtype")
	}

	subtypeName, err := document.ResolveName(ctx, doc, subtypeObj)
	if err != nil {
		return nil, err
	}
	if subtypeName == "" {
		return nil, fmt.Errorf("font /Subtype is not a name")
	}

	// FontDescriptor /Flags bit 3 (Symbolic) marks fonts whose glyph
	// repertoire is bespoke. Custom glyph names (H11537, etc.) are
	// laid out at WinAnsi-style byte positions for PostScript-printed
	// docs (Universal-NewswithCommPi, OCR symbols, math notation), so
	// those bytes need the WinAnsi fallback. Pulling the flag before
	// defaultEncoding selection routes Type 1 symbolic fonts past the
	// StandardEncoding default below.
	symbolicByFlags := fontIsSymbolicByFlags(ctx, doc, fontDict)

	// Detect Symbol, ZapfDingbats, and the other 14 standard Type 1
	// fonts by BaseFont name to select the correct default encoding.
	// Per ISO 32000-1 §9.6.6.1 a non-symbolic Type 1 font that omits
	// /Encoding inherits StandardEncoding; Symbol and ZapfDingbats use
	// their own built-in encoding; the standard 14 base fonts already
	// fall in the StandardEncoding bucket. Custom Type 1 fonts (e.g.
	// /Melior, /Minion-Pro) hit the same StandardEncoding default.
	// [readsAsStandardSymbol] and [readsAsStandardDingbats] say which fonts
	// those two built-in encodings are.
	isSymbolFont := false
	isDingbatsFont := false
	if bfObj, ok := fontDict["BaseFont"]; ok {
		bfName, err := document.ResolveName(ctx, doc, bfObj)
		if err != nil {
			doc.Logger.Debug("resolving font BaseFont name", slog.Any("err", err))
		}
		if bfName != "" {
			embedded := hasEmbeddedFontFile(ctx, doc, fontDict)
			isSymbolFont = readsAsStandardSymbol(string(bfName), symbolicByFlags, embedded)
			isDingbatsFont = readsAsStandardDingbats(string(bfName), symbolicByFlags, embedded)
		}
	}

	// Default encoding selection. ISO 32000-1 §9.6.6.1 makes
	// StandardEncoding the implicit base for non-symbolic Type 1
	// programs that omit /Encoding (Melior, Minion, Garamond, …); per
	// §9.6.6.4 TrueType non-symbolic fonts are expected to declare
	// WinAnsi/MacRoman/MacExpert and fall through to WinAnsi when they
	// don't, since their cmap stays in the Latin-1 range. Type 3 fonts
	// in the wild commonly bake glyphs into direct Latin-1 slots and
	// rely on a WinAnsi default too. Symbol/ZapfDingbats always use
	// their own built-in encoding regardless of subtype.
	subtype := string(subtypeName)
	isType1Like := subtype == "Type1" || subtype == "MMType1"
	defaultEncoding := copyEncoding(encodings.WinAnsiEncoding)
	if isType1Like && !symbolicByFlags {
		defaultEncoding = copyEncoding(encodings.StandardEncoding)
	}
	if subtype == "TrueType" && trueTypeTakesStandardEncoding(ctx, doc, fontDict, symbolicByFlags) {
		defaultEncoding = copyEncoding(encodings.StandardEncoding)
	}
	switch {
	case isSymbolFont:
		defaultEncoding = encodings.BuildSymbolEncoding()
	case isDingbatsFont:
		defaultEncoding = encodings.BuildZapfDingbatsEncoding()
	}

	font := &Font{
		Subtype:  string(subtypeName),
		Encoding: defaultEncoding,
		// MissingWidth defaults to 0 for simple fonts (PDF spec does not
		// mandate a default). CIDFont /DW defaults to 1000 per spec and is set below.
	}

	// Try to extract ToUnicode CMap. Cache by stream object number so fonts
	// sharing the same ToUnicode stream avoid redundant decode+parse.
	if tuObj, ok := fontDict["ToUnicode"]; ok {
		if ref, isRef := tuObj.(ast.PdfIndirectRef); isRef {
			if cached, hit := doc.CachedCMap(ref.ObjectNum); hit {
				if c, ok := cached.(*cmap.CMap); ok {
					font.ToUnicode = c
				}
			} else {
				font.ToUnicode = parseAndCacheToUnicode(ctx, doc, tuObj, ref.ObjectNum)
			}
		} else {
			font.ToUnicode = parseToUnicode(ctx, doc, tuObj)
		}
		// When /ToUnicode is a name like /Identity-H (not a valid CMap stream),
		// the PDF producer intends character codes to be direct Unicode values.
		// Set the Identity fallback for Type0 fonts in this case.
		if font.ToUnicode == nil && subtypeName == "Type0" {
			if tuName, ok := tuObj.(ast.PdfName); ok && (tuName == "Identity-H" || tuName == "Identity-V") {
				font.CIDIdentityFallback = true
			}
		}
	}

	resolveEncoding(ctx, doc, fontDict, font, isDingbatsFont, isSymbolFont)

	// Type 3 fonts: extract widths from /CharProcs streams via d0/d1 operators.
	if subtypeName == "Type3" {
		resolveType3Font(ctx, doc, fontDict, font)
		return font, nil
	}

	// Extract glyph width metrics (subtype-specific).
	if subtypeName == "Type0" {
		resolveType0Metrics(ctx, doc, fontDict, font)
	} else {
		resolveSimpleFontMetrics(ctx, doc, fontDict, font)
	}

	// Type1 font program encoding: parse the encoding vector from the
	// Embedded font program encoding. When the PDF-level /Encoding is absent,
	// the font program's encoding is authoritative and overrides the default.
	_, hasExplicitEncoding := fontDict["Encoding"]
	extractType1Encoding(ctx, doc, fontDict, font, !hasExplicitEncoding)
	// Also try CFF/Type1C encoding from /FontFile3 for fonts like TeX CMR
	// that use OT1 encoding internally.
	if !hasExplicitEncoding {
		extractCFFEncoding(ctx, doc, fontDict, font)
	}

	// TrueType cmap-derived implicit encoding (ISO 32000-1 §9.6.6.4).
	// When a non-symbolic TrueType font omits /Encoding, the font's
	// embedded cmap subtables decide the implicit base. Microsoft
	// Unicode subtables expose the Latin-1 range that aligns with
	// WinAnsi (already the litho default). Fonts that only ship a Mac
	// Roman subtable (older Mac-authored documents) put accented glyphs
	// at MacRoman byte positions:
	// byte 0xD0 is en-dash, 0x96 is ñ, 0xA9 is ©. Switching to
	// MacRomanEncoding on the cmap signal surfaces those bytes as
	// real characters instead of WinAnsi look-alikes.
	if !hasExplicitEncoding && subtypeName == "TrueType" && !isSymbolFont && !isDingbatsFont {
		applyTrueTypeBuiltinEncoding(ctx, doc, fontDict, font, symbolicByFlags)
	}
	if !hasExplicitEncoding && subtypeName == "TrueType" && symbolicByFlags {
		applySymbolicTrueTypeGlyphs(ctx, doc, fontDict, font, isDingbatsFont, isSymbolFont)
	}

	// Extract /FontBBox and embedded font metrics from the FontDescriptor.
	extractFontDescriptorMetrics(ctx, doc, fontDict, font)

	// Predefined CJK encoding CMap fallback for Type0 fonts: when the
	// /Encoding is a predefined CMap name (e.g., GBK-EUC-H, EUC-H), load
	// the byte→CID mapping from the bundled bcmap and compose with the
	// Adobe CID→Unicode table to synthesize a ToUnicode CMap. Per ISO
	// 32000-1, 9.10.2, steps a-e, the character code maps to a CID through
	// the predefined CMap and the CID to Unicode through the
	// <Registry>-<Ordering>-UCS2 CMap: the font program takes no part, so an
	// unembedded font decodes as an embedded one does.
	if subtypeName == "Type0" && font.ToUnicode == nil && !font.CIDIdentityFallback {
		if encName := encodingCMapName(ctx, doc, fontDict); encName != "" && encName != "Identity-H" && encName != "Identity-V" {
			if cm := cmap.LoadPredefinedCMap(encName); cm != nil {
				font.ToUnicode = cm
			} else if !hasEmbeddedFontFile(ctx, doc, fontDict) {
				// A name outside Table 118, or a collection without a UCS2
				// table, leaves 9.10.2 no route to Unicode, and no font
				// program offers one either: any character litho emitted
				// would be invented.
				font.SuppressExtraction = true
			}
		}
	}

	// CIDToGIDMap handling for Type0 fonts. When CIDToGIDMap is a stream,
	// parse it to build CID→GID mapping and combine with the embedded font's
	// GID→Unicode cmap to create a synthetic ToUnicode. When it's /Identity,
	// CID=GID and the Identity fallback is used as a last resort below.
	if subtypeName == "Type0" && font.ToUnicode == nil && !font.CIDIdentityFallback {
		applyCIDToGIDIdentityFallback(ctx, doc, fontDict, font)
	}

	// Embedded TrueType/OpenType cmap fallback: extract the glyph-to-Unicode
	// mapping from the embedded font binary when no ToUnicode CMap exists.
	// For Type0 fonts, only use the embedded cmap when a CIDToGIDMap stream
	// was parsed (font.ToUnicode set above), since without CIDToGIDMap the
	// CID→GID mapping is unreliable for subset fonts.
	if font.ToUnicode == nil && !font.CIDIdentityFallback && subtypeName != "Type0" {
		extractEmbeddedCmap(ctx, doc, fontDict, font)
	}

	// Type1 /FontFile fallback: when a simple Type1/MMType1 font has no
	// /Encoding entry and no /ToUnicode, the byte-to-glyph mapping lives
	// inside the PostScript /FontFile preamble. Extracting it recovers
	// text from TeX/Computer-Modern family fonts (CMSY, CMMI, CMR, ...)
	// whose custom encodings have no standard counterpart.
	if font.ToUnicode == nil && (subtypeName == "Type1" || subtypeName == "MMType1") {
		if _, hasEnc := fontDict["Encoding"]; !hasEnc {
			applyType1FontFileEncoding(ctx, doc, fontDict, font, isDingbatsFont, isSymbolFont)
		}
	}

	// Type1 CharStrings width fallback: fill in advance widths that the
	// PDF-level /Widths array left unreadable or did not cover. TeX-subset
	// Type 1 fonts commonly emit /FirstChar, /LastChar, and a /Widths
	// array that only spans a narrow range, yet use byte codes outside
	// that range for math/symbol glyphs. Reading the widths straight
	// from the embedded font program gives the space-insertion
	// heuristic in ReconstructText accurate per-glyph spans.
	if subtypeName == "Type1" || subtypeName == "MMType1" {
		applyType1CharStringWidths(ctx, doc, fontDict, font)
	}

	// Adobe predefined CID→Unicode fallback for Type0 fonts. ISO 32000-1,
	// 9.10.2 extends steps a-e to a descendant CIDFont of the Adobe-GB1,
	// Adobe-CNS1, Adobe-Japan1 or Adobe-Korea1 collection under any CMap,
	// Identity-H and Identity-V included: the CID indexes the collection,
	// whose UCS2 CMap names its character whether the font is embedded or
	// left to the host. The table is keyed by CID. Under Identity-H or
	// Identity-V the two-byte code is the CID (9.7.5.2, Table 118); under a
	// CMap whose cidchar and cidrange mappings give the CIDs
	// ([Font.encodingCIDKeyed]) the code is read at its CID
	// ([encodingCIDTableChar]), as PDF.js composes the CMap with the
	// collection's UCS2 CMap (evaluator.js, buildToUnicode). Otherwise only an
	// embedded program takes the table, read at the two-byte code: an encoding
	// CMap written with bfchar alone declares no CID litho could read it at.
	if subtypeName == "Type0" && font.ToUnicode == nil && font.GIDToUnicode == nil && !font.CIDIdentityFallback {
		if isIdentityEncoding(ctx, doc, fontDict) || font.encodingCIDKeyed() || hasEmbeddedFontFile(ctx, doc, fontDict) {
			applyAdobeCIDFallback(ctx, doc, fontDict, font)
		}
	}

	// Identity-H/V fallback: last resort for Type0 fonts with no other
	// mapping. Treats 2-byte character codes as direct Unicode codepoints.
	// A reference-only font of an Adobe CJK collection reaches this branch
	// only when its collection's UCS2 table is unavailable; its CIDs index
	// the collection (ISO 32000-1, 9.7.3), not Unicode, so the +29
	// heuristic would decode to garbage and extraction is suppressed.
	if subtypeName == "Type0" && font.ToUnicode == nil && font.GIDToUnicode == nil && !font.CIDIdentityFallback && !font.SuppressExtraction {
		if isIdentityEncoding(ctx, doc, fontDict) && isCJKOrdering(ctx, doc, fontDict) && !hasEmbeddedFontFile(ctx, doc, fontDict) {
			font.SuppressExtraction = true
		} else if isIdentityEncoding(ctx, doc, fontDict) {
			font.CIDIdentityFallback = true
			applyCIDIdentityHeuristics(ctx, doc, fontDict, font)
		}
	}

	// CJK heuristic for simple fonts: when a TrueType or Type1 font has no
	// ToUnicode CMap and the BaseFont name contains CJK characters, the font
	// is likely mis-declared with WinAnsiEncoding but actually uses GBK byte
	// encoding. Embedded fonts get a synthesised GBK ToUnicode so their
	// Chinese text extracts. A reference-only font gets it too when it
	// states WinAnsiEncoding under one of the Chinese face names of
	// [isChineseFaceName]. Every reference-only CJK-named font has its
	// Encoding cleared, which avoids emitting WinAnsi-decoded garbage and
	// keeps that encoding out of the /ToUnicode remediation writes.
	if font.ToUnicode == nil && (subtypeName == "TrueType" || subtypeName == "Type1") {
		if bfObj, ok := fontDict["BaseFont"]; ok {
			bfName, _ := document.ResolveName(ctx, doc, bfObj)
			if hasCJKFontName(string(bfName)) {
				embedded := hasEmbeddedFontFile(ctx, doc, fontDict)
				if embedded || isChineseFaceName(string(bfName)) && simpleBaseEncodingName(ctx, doc, fontDict) == "WinAnsiEncoding" {
					if cm := cmap.LoadPredefinedCMap("GBK-EUC-H"); cm != nil {
						font.ToUnicode = cm
					}
				}
				if !embedded {
					font.Encoding = nil
				}
			}
		}
	}

	// Determine the font name for logging.
	fontLogName := string(subtypeName)
	if bfObj, ok := fontDict["BaseFont"]; ok {
		bfName, err := document.ResolveName(ctx, doc, bfObj)
		if err != nil {
			doc.Logger.Debug("resolving BaseFont for logging", slog.Any("err", err))
		}
		if bfName != "" {
			fontLogName = string(bfName)
		}
	}

	doc.Logger.Debug("font resolved",
		slog.String("name", fontLogName),
		slog.String("subtype", string(subtypeName)),
		slog.Bool("has_tounicode", font.ToUnicode != nil),
		slog.Bool("has_gid_to_unicode", font.GIDToUnicode != nil),
		slog.Bool("has_cid_widths", font.CIDWidths != nil),
		slog.Int("encoding_entries", len(font.Encoding)),
		slog.Int("wmode", font.WMode),
		slog.Bool("cid_identity_fallback", font.CIDIdentityFallback),
	)

	return font, nil
}

// parseAndCacheToUnicode parses a ToUnicode CMap stream and caches the result
// by object number for reuse by other fonts referencing the same stream.
func parseAndCacheToUnicode(ctx context.Context, doc *document.Document, tuObj ast.PdfObject, objNum int) *cmap.CMap {
	c := parseToUnicode(ctx, doc, tuObj)
	if c != nil {
		doc.SetCachedCMap(objNum, c)
	}
	return c
}

// parseToUnicode resolves and parses a ToUnicode CMap stream. Returns
// nil when the stream resolves to an empty mapping so downstream
// fallbacks (Identity, Adobe CID tables, font-file cmap) can take over
// instead of being blocked by a hollow CMap that never resolves any
// character code.
//
// A CMap that runs usecmap takes the named predefined CMap's mappings for the
// codes it leaves unmapped, as PDF.js reads it. A /UseCMap entry in the stream
// dictionary is not followed: PDF.js reads a /ToUnicode CMap without it.
//
// # Corpus evidence
//
//   - All corpora: two files change text. pdfjs issue5010.pdf, whose
//     /ToUnicode runs /Adobe-Korea1-UCS2 usecmap over Identity-H codes, now
//     reads 인터뷰●홍인혜카피라이터 as PDF.js does, where each code read as its
//     own value. litho usecmap_2u.pdf's usecmap fonts read their base too.
func parseToUnicode(ctx context.Context, doc *document.Document, tuObj ast.PdfObject) *cmap.CMap {
	stream, err := document.ResolveStream(ctx, doc, tuObj)
	if err != nil || stream.Dict == nil {
		return nil
	}
	decoded, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		return nil
	}
	c, err := cmap.Parse(ctx, decoded)
	if err != nil {
		return nil
	}
	if c != nil && c.UseCMap() != "" {
		// Per ISO 32000-1, 9.7.5.4, usecmap makes the named CMap's mappings
		// part of this one. PDF.js's extendCMap fills each code the program
		// leaves unmapped from the base, wherever the operator stands, so a
		// code the program maps itself keeps its own value.
		if base := cmap.BuiltInUnicode(c.UseCMap()); len(base) > 0 {
			c = c.WithBase(nil, base, false)
		}
	}
	if c == nil || len(c.Mapping) == 0 {
		return nil
	}
	return c
}

// resolveEncoding resolves the /Encoding entry from the font dictionary into
// the font's encoding map. Handles both named encodings (WinAnsiEncoding, etc.)
// and dictionary-based encodings with /BaseEncoding and /Differences.
//
// # Corpus evidence
//
//   - govdocs thread_400_400235.pdf, thread_300_300511.pdf, pdfjs TAMReview.pdf:
//     symbolic Type 1 programs whose built-in encodings leave 0x27 and 0x60
//     without a usable name. Filled from StandardEncoding they read as curly
//     quotes, where PDF.js reads straight ones.
func resolveEncoding(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, font *Font, isDingbats, isSymbol bool) {
	encObj, ok := fontDict["Encoding"]
	if !ok {
		return
	}
	encResolved, err := document.ResolveObject(ctx, doc, encObj)
	if err != nil {
		return
	}
	switch enc := encResolved.(type) {
	case ast.PdfName:
		switch enc {
		case "WinAnsiEncoding":
			font.Encoding = encodings.WinAnsiDecoding()
		case "MacRomanEncoding":
			font.Encoding = copyEncoding(encodings.MacRomanEncoding)
		case "StandardEncoding":
			font.Encoding = copyEncoding(encodings.StandardEncoding)
		case "MacExpertEncoding":
			font.Encoding = copyEncoding(encodings.MacExpertEncoding)
		}
	case ast.PdfDict:
		// Dictionary-based Encoding.
		haveNamedBase := false
		var baseMap map[byte]rune
		if baseObj, ok := enc["BaseEncoding"]; ok {
			baseName, err := document.ResolveName(ctx, doc, baseObj)
			if err != nil {
				doc.Logger.Debug("resolving font BaseEncoding name", slog.Any("err", err))
			}
			if baseName != "" {
				switch baseName {
				case "WinAnsiEncoding":
					baseMap = encodings.WinAnsiDecoding()
					haveNamedBase = true
				case "MacRomanEncoding":
					baseMap = encodings.MacRomanEncoding
					haveNamedBase = true
				case "StandardEncoding":
					baseMap = encodings.StandardEncoding
					haveNamedBase = true
				case "MacExpertEncoding":
					baseMap = encodings.MacExpertEncoding
					haveNamedBase = true
				}
			}
		}
		if haveNamedBase {
			font.Encoding = copyEncoding(baseMap)
		} else {
			// No /BaseEncoding: per ISO 32000-2 §9.6.6.4, symbolic
			// Type1 fonts fall back to the font program's built-in
			// encoding (TeX cmbx12 et al expose ff/fi/ffl ligatures at
			// bytes 0x0B-0x0F this way) and non-symbolic Type 1 fonts
			// fall back to StandardEncoding. TrueType / Type 3 keep
			// WinAnsi as the pragmatic default since their producers
			// almost always store glyphs at Latin-1 byte positions.
			// Seed from the FontFile / FontFile3 first so the program's
			// own encoding wins, then overlay the appropriate built-in
			// on any unfilled slot. A symbolic Type 1 font whose program
			// leaves a slot without a usable name takes the WinAnsi
			// default ResolveFont gives symbolic fonts: StandardEncoding
			// is the nonsymbolic default (ISO 32000-1, 9.6.6.1), and it
			// reads 0x27 and 0x60 as curly quotes where PDF.js, whose
			// symbolic default is MacRoman, reads straight ones.
			font.Encoding = make(map[byte]rune)
			applyType1FontFileEncoding(ctx, doc, fontDict, font, isDingbats, isSymbol)
			extractCFFEncodingAsBase(ctx, doc, fontDict, font)
			isType1Like := font.Subtype == "Type1" || font.Subtype == "MMType1"
			var overlay map[byte]rune
			switch {
			case isSymbol:
				overlay = encodings.BuildSymbolEncoding()
			case isDingbats:
				overlay = encodings.BuildZapfDingbatsEncoding()
			case isType1Like && !fontIsSymbolicByFlags(ctx, doc, fontDict):
				overlay = encodings.StandardEncoding
			default:
				overlay = encodings.WinAnsiEncoding
			}
			for code, r := range overlay {
				if _, set := font.Encoding[code]; !set {
					font.Encoding[code] = r
				}
			}
		}

		// Process Differences array
		if diffObj, ok := enc["Differences"]; ok {
			diffArr, err := document.ResolveArray(ctx, doc, diffObj)
			if err != nil {
				doc.Logger.Debug("resolving font Differences array", slog.Any("err", err))
			}
			if diffArr != nil {
				currentIndex := 0
				for _, item := range diffArr {
					itemRes, err := document.ResolveObject(ctx, doc, item)
					if err != nil {
						doc.Logger.Debug("resolving Differences item", slog.Any("err", err))
					}
					switch v := itemRes.(type) {
					case ast.PdfInt:
						currentIndex = int(v)
					case ast.PdfName:
						if r, ok := resolveGlyphRune(string(v), isDingbats, isSymbol); ok {
							font.Encoding[byte(currentIndex)] = r
						} else if decomposed, ok := decomposeGlyphName(string(v), isDingbats, isSymbol); ok {
							// Type1 fonts stack ligature components with
							// underscores ("f_t", "T_h"). When no single
							// rune mapping exists the decomposed string
							// lives in Font.LigatureOverrides and beats
							// the byte's default-encoding rune during
							// extraction.
							if font.LigatureOverrides == nil {
								font.LigatureOverrides = make(map[byte]string)
							}
							font.LigatureOverrides[byte(currentIndex)] = decomposed
							// Anchor the encoding map at the first rune so
							// width calculation and encoding-derived
							// fallbacks still have a value.
							if first, _ := utf8.DecodeRuneInString(decomposed); first != utf8.RuneError {
								font.Encoding[byte(currentIndex)] = first
							}
						}
						currentIndex++
					}
				}
			}
		}
	}
}

// defaultFontMatrix is the matrix of a font whose glyph space is 1000 units
// to the text space unit (ISO 32000-1, 9.2.4), the /FontMatrix every font
// type but Type 3 has.
//
//nolint:gochecknoglobals // constant matrix, arrays cannot be const
var defaultFontMatrix = [6]float64{0.001, 0, 0, 0.001, 0, 0}

// type3HeightScale returns the factor PDF.js scales the height of the text
// of a Type 3 font by, set at size by Tf, and 1 for any other font. PDF.js
// sizes a Type 3 glyph across its line by the height of the font's bounding
// box, mapped to text space by /FontMatrix, when the size is at most 1 or
// the box was read off the glyphs, and the matrix is not the default one
// (src/core/evaluator.js, getTextContent, getCurrentTextTransform). A font
// shown at size 1 and drawn in glyph space units through an identity
// matrix otherwise reads as a fraction of a unit high. ISO 32000-1, 9.6.5
// leaves the size across the line of a Type 3 glyph to its description, so
// the factor is the reference's, not the specification's.
//
// # Corpus evidence
//
//   - pdfjs issue19624.pdf page 2: drawing labels in Type 3 fonts with an
//     empty /FontBBox and an identity /FontMatrix, shown at 0.43 units on
//     the page. Each label that jumps back to the left of the one before it
//     on the same line, 0.36 units lower, broke the line at half that size
//     ("AMINE SERVICE"), where PDF.js, which sizes the glyphs from the
//     32-unit bounds of their paths, keeps the line ("AMINESERVICE").
//   - All corpora: words match PDF.js on 1 more file (pdfjs issue19624.pdf)
//     and on none fewer; facturx, pdf20 and pdfcpu do not move.
func (f *Font) type3HeightScale(size float64) float64 {
	if f == nil || f.Subtype != "Type3" || f.type3BBoxHeight <= 0 || f.FontMatrix == defaultFontMatrix {
		return 1
	}
	if size > 1 && !f.type3CharBBox {
		return 1
	}
	return math.Abs(f.type3BBoxHeight * f.FontMatrix[3])
}

// resolveType3Font extracts Type 3 font metrics: /FontMatrix, /CharProcs
// glyph widths from d0/d1 operators, and /FirstChar, /LastChar, /Widths
// as a fallback.
func resolveType3Font(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, font *Font) {
	// /FontMatrix maps glyph space → text space. Default is [0.001 0 0 0.001 0 0].
	font.FontMatrix = defaultFontMatrix
	if fmObj, ok := fontDict["FontMatrix"]; ok {
		fmArr, err := document.ResolveArray(ctx, doc, fmObj)
		if err != nil {
			doc.Logger.Debug("resolving Type3 FontMatrix", slog.Any("err", err))
		}
		if len(fmArr) == 6 {
			for i := range 6 {
				font.FontMatrix[i] = document.Float(fmArr[i])
			}
		}
	}

	var bbox [4]float64
	if bbObj, ok := fontDict["FontBBox"]; ok {
		bbArr, err := document.ResolveArray(ctx, doc, bbObj)
		if err != nil {
			doc.Logger.Debug("resolving Type3 FontBBox", slog.Any("err", err))
		}
		if len(bbArr) == 4 {
			for i := range 4 {
				bbox[i] = document.Float(bbArr[i])
			}
		}
	}

	// /CharProcs maps glyph names → streams containing d0/d1 width operators.
	var guessed *[4]float64
	if cpObj, ok := fontDict["CharProcs"]; ok {
		cpDict, err := document.ResolveDict(ctx, doc, cpObj)
		if err != nil {
			doc.Logger.Debug("resolving Type3 CharProcs", slog.Any("err", err))
		}
		if cpDict != nil {
			font.Type3Widths = make(map[string]float64, len(cpDict))
			for name, streamObj := range cpDict {
				if err := ctx.Err(); err != nil {
					return
				}
				stream, err := document.ResolveStream(ctx, doc, streamObj)
				if err != nil {
					continue
				}
				if stream.Dict == nil {
					continue
				}
				w, glyphBox, err := extractType3Glyph(ctx, doc, stream)
				if err != nil {
					continue
				}
				font.Type3Widths[string(name)] = w
				if glyphBox != nil {
					if guessed == nil {
						guessed = glyphBox
					} else {
						guessed[0] = min(guessed[0], glyphBox[0])
						guessed[1] = min(guessed[1], glyphBox[1])
						guessed[2] = max(guessed[2], glyphBox[2])
						guessed[3] = max(guessed[3], glyphBox[3])
					}
				}
			}
		}
	}

	// The height of the font's bounding box comes from /FontBBox (ISO
	// 32000-1, 9.6.5, Table 112), or, when the box it states is empty, from
	// the paths of the glyphs that begin with d0, as PDF.js reads it
	// (src/core/evaluator.js, TranslatedFont, loadType3Data and
	// #guessType3FontBBox). See [Font.type3HeightScale].
	if math.Hypot(bbox[2]-bbox[0], bbox[3]-bbox[1]) == 0 {
		if guessed != nil {
			font.type3BBoxHeight = guessed[3] - guessed[1]
			font.type3CharBBox = true
		}
	} else {
		font.type3BBoxHeight = bbox[3] - bbox[1]
	}

	// Also extract /FirstChar, /LastChar, /Widths as fallback.
	if fcObj, ok := fontDict["FirstChar"]; ok {
		font.FirstChar = int(document.Float(fcObj))
	}
	if lcObj, ok := fontDict["LastChar"]; ok {
		font.LastChar = int(document.Float(lcObj))
	}
	if wObj, ok := fontDict["Widths"]; ok {
		wArr, err := document.ResolveArray(ctx, doc, wObj)
		if err != nil {
			doc.Logger.Debug("resolving Type3 Widths array", slog.Any("err", err))
		}
		if wArr != nil {
			font.Widths = make([]float64, len(wArr))
			for i, item := range wArr {
				font.Widths[i] = document.Float(item)
			}
		}
	}
}

// resolveType0Metrics extracts CIDFont metrics from the /DescendantFonts
// array: /DW (default width), /W (per-CID widths), /WMode, /DW2
// (default vertical metrics), and /W2 (per-CID vertical metrics).
func resolveType0Metrics(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, font *Font) {
	// Default vertical metrics per PDF spec Table 117.
	font.DW2 = [2]float64{880, -1000}
	font.encodingCIDs = resolveEncodingCIDs(ctx, doc, fontDict)

	// CIDFont metrics live in the first element of /DescendantFonts.
	if dfObj, ok := fontDict["DescendantFonts"]; ok {
		dfArr, err := document.ResolveArray(ctx, doc, dfObj)
		if err != nil {
			doc.Logger.Debug("resolving DescendantFonts array", slog.Any("err", err))
		}
		if len(dfArr) > 0 {
			cidDict, err := document.ResolveDict(ctx, doc, dfArr[0])
			if err != nil {
				doc.Logger.Debug("resolving CIDFont dict", slog.Any("err", err))
			}
			if cidDict != nil {
				// /DW is the default glyph width for CIDFonts (default 1000 per spec).
				font.MissingWidth = 1000.0
				if dwObj, ok := cidDict["DW"]; ok {
					font.MissingWidth = document.Float(dwObj)
					font.DWStated = true
				}
				// /W encodes per-CID or range widths.
				if wObj, ok := cidDict["W"]; ok {
					wArr, err := document.ResolveArray(ctx, doc, wObj)
					if err != nil {
						doc.Logger.Debug("resolving CIDFont /W array", slog.Any("err", err))
					}
					if wArr != nil {
						font.CIDWidths = parseCIDWidths(resolveMetricsArray(ctx, doc, wArr))
					}
				}
				// /WMode: 0 = horizontal, 1 = vertical.
				if wmObj, ok := cidDict["WMode"]; ok {
					font.WMode = int(document.Float(wmObj))
				}
				// /DW2: default vertical displacement [vY w1].
				if dw2Obj, ok := cidDict["DW2"]; ok {
					dw2Arr, err := document.ResolveArray(ctx, doc, dw2Obj)
					if err != nil {
						doc.Logger.Debug("resolving CIDFont /DW2 array", slog.Any("err", err))
					}
					if len(dw2Arr) == 2 {
						font.DW2 = [2]float64{document.Float(dw2Arr[0]), document.Float(dw2Arr[1])}
					}
				}
				// /W2: per-CID vertical metrics [w1Y vX vY].
				if w2Obj, ok := cidDict["W2"]; ok {
					w2Arr, err := document.ResolveArray(ctx, doc, w2Obj)
					if err != nil {
						doc.Logger.Debug("resolving CIDFont /W2 array", slog.Any("err", err))
					}
					if w2Arr != nil {
						font.CIDVerticalMetrics = parseCIDVerticalWidths(resolveMetricsArray(ctx, doc, w2Arr))
					}
				}
			}
		}
	}

	// Also check /WMode on the font dictionary itself (some producers put it here).
	if font.WMode == 0 {
		if wmObj, ok := fontDict["WMode"]; ok {
			font.WMode = int(document.Float(wmObj))
		}
	}

	if cmapWritingMode(ctx, doc, fontDict) == 1 {
		font.WMode = 1
	}
}

// cmapWritingMode returns the writing mode a Type 0 font's /Encoding CMap
// declares: 0 horizontal, 1 vertical. Per ISO 32000-1, 9.7.4.3 and 9.7.5.3,
// the CMap, not the CIDFont, carries the writing mode. A predefined CMap
// declares it by name (9.7.5.2, Table 118: Identity-V and every -V CMap are
// vertical), an embedded CMap stream through its /WMode entry (Table 120,
// default 0). A name or stream litho cannot read is horizontal.
//
// # Corpus evidence
//
//   - pdfjs bug1905623.pdf (UniJIS-UTF16-V), vertical.pdf and issue6387.pdf
//     (Identity-V): read as horizontal, each column ran off the page edge
//     to the right and its tail was dropped as text outside the CropBox.
//     Read as vertical, all three recover every character PDF.js extracts.
func cmapWritingMode(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) int {
	encObj, ok := fontDict["Encoding"]
	if !ok {
		return 0
	}
	resolved, err := document.ResolveObject(ctx, doc, encObj)
	if err != nil {
		doc.Logger.Debug("resolving Type0 /Encoding for writing mode", slog.Any("err", err))
		return 0
	}
	switch enc := resolved.(type) {
	case ast.PdfName:
		switch enc {
		case "Identity-V":
			return 1
		case "Identity-H":
			return 0
		}
		if m := cmap.LoadPredefinedCIDMap(string(enc)); m != nil {
			return m.WMode()
		}
	case ast.PdfStream:
		if wm, ok := enc.Dict["WMode"]; ok && document.Float(wm) == 1 {
			return 1
		}
	}
	return 0
}

// resolveSimpleFontMetrics extracts metrics for simple fonts (Type1, TrueType,
// MMType1): /FirstChar, /LastChar, /Widths array, /MissingWidth from the
// /FontDescriptor, and Standard 14 width fallback.
func resolveSimpleFontMetrics(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, font *Font) {
	if fcObj, ok := fontDict["FirstChar"]; ok {
		font.FirstChar = int(document.Float(fcObj))
	}
	if lcObj, ok := fontDict["LastChar"]; ok {
		font.LastChar = int(document.Float(lcObj))
	}
	if wObj, ok := fontDict["Widths"]; ok {
		wArr, err := document.ResolveArray(ctx, doc, wObj)
		if err != nil {
			doc.Logger.Debug("resolving simple font Widths array", slog.Any("err", err))
		}
		if wArr != nil {
			font.Widths = make([]float64, len(wArr))
			font.widthStated = make([]bool, len(wArr))
			for i, item := range resolveMetricsArray(ctx, doc, wArr) {
				font.Widths[i], font.widthStated[i] = document.Number(item)
			}
		}
	}
	// /MissingWidth from /FontDescriptor overrides the 1000 default.
	if fdObj, ok := fontDict["FontDescriptor"]; ok {
		fdDict, err := document.ResolveDict(ctx, doc, fdObj)
		if err != nil {
			doc.Logger.Debug("resolving FontDescriptor dict", slog.Any("err", err))
		}
		if fdDict != nil {
			if mwObj, ok := fdDict["MissingWidth"]; ok {
				font.MissingWidth = document.Float(mwObj)
			}
		}
	}

	// Standard 14 fallback: if /Widths is missing and the /BaseFont
	// matches one of the 14 standard fonts, inject the hardcoded
	// Adobe Font Metrics so spatial text reconstruction succeeds.
	if len(font.Widths) == 0 {
		if bfObj, ok := fontDict["BaseFont"]; ok {
			bfName, err := document.ResolveName(ctx, doc, bfObj)
			if err != nil {
				doc.Logger.Debug("resolving BaseFont for Standard 14 fallback", slog.Any("err", err))
			}
			if bfName != "" {
				_, described := fontDict["FontDescriptor"]
				injectStandard14Widths(font, string(bfName), substituteIsSerif(string(bfName), described))
			}
		}
	}
}

// extractFontDescriptorMetrics populates /FontBBox and TrueType hmtx widths
// from the /FontDescriptor. These serve as fallbacks when the PDF-level width
// arrays (/Widths, /W) are missing or incomplete.
func extractFontDescriptorMetrics(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, font *Font) {
	fdDict := document.ResolveFontDescriptor(ctx, doc, fontDict)
	if fdDict == nil {
		return
	}

	// /FontBBox [xMin yMin xMax yMax], used as a width fallback.
	if bboxObj, ok := fdDict["FontBBox"]; ok {
		bboxArr, err := document.ResolveArray(ctx, doc, bboxObj)
		if err != nil {
			doc.Logger.Debug("resolving FontBBox array", slog.Any("err", err))
		}
		if len(bboxArr) == 4 {
			for i := range 4 {
				font.FontBBox[i] = document.Float(bboxArr[i])
			}
		}
	}

	// Binary font widths from the embedded font program.
	fontData := decodeFontStream(ctx, doc, fdDict, "FontFile2")
	if fontData == nil {
		fontData = decodeFontStream(ctx, doc, fdDict, "FontFile3")
	}
	if fontData == nil {
		return
	}

	// OS/2 xAvgCharWidth provides a better fallback than FontBBox or 1000.
	font.AvgWidth = readOS2AvgWidth(fontData)

	widths := ExtractBinaryWidths(ctx, fontData)
	if len(widths) == 0 {
		return
	}

	// Text extraction positions a glyph by its whole-unit advance, as it has
	// always done; the fractional part ExtractBinaryWidths now preserves
	// matters only to the width-consistency check, which reads the program
	// directly.
	font.HmtxWidths = make(map[uint16]uint16, len(widths))
	for gid, w := range widths {
		if w < 0 || w > math.MaxUint16 {
			continue
		}
		font.HmtxWidths[gid] = uint16(math.Round(w))
	}

	// Read unitsPerEm for correct width scaling.
	// CFF fonts use 1000 units per em by default. For OTTO fonts (CFF inside
	// OpenType), the head table may report a different value (e.g., 2048) but
	// CFF charstring widths are always in the CFF coordinate system.
	if isCFFFont(fontData) {
		font.UnitsPerEm = 1000 //nolint:mnd // CFF standard
	} else {
		font.UnitsPerEm = ReadUnitsPerEm(fontData)
	}
}

// isCFFFont returns true if the font data is bare CFF or OTTO (CFF-in-OpenType).
func isCFFFont(data []byte) bool {
	if len(data) < 4 {
		return false
	}
	// Bare CFF: major version 1
	if data[0] == 1 && data[1] == 0 {
		return true
	}
	// OTTO: OpenType with CFF outlines
	return string(data[:4]) == "OTTO"
}

// fontFormat identifies the binary format of embedded font data.
type fontFormat int

const (
	fontFormatTrueType fontFormat = iota
	fontFormatOTTO
	fontFormatBareCFF
	fontFormatUnknown
)

// detectFontFormat classifies the font binary by its leading signature bytes.
func detectFontFormat(data []byte) fontFormat {
	if len(data) < 4 {
		return fontFormatUnknown
	}
	sig := data[:4]
	if (sig[0] == 0 && sig[1] == 1 && sig[2] == 0 && sig[3] == 0) || string(sig) == "true" {
		return fontFormatTrueType
	}
	if string(sig) == "OTTO" {
		return fontFormatOTTO
	}
	if sig[0] == 1 && sig[1] == 0 {
		return fontFormatBareCFF
	}
	return fontFormatUnknown
}

// extractCFFFromOTTO extracts the "CFF " table from an OpenType (OTTO) font.
func extractCFFFromOTTO(fontData []byte) ([]byte, error) {
	records, err := truetype.ParseTableDirectory(fontData)
	if err != nil {
		return nil, err
	}
	return truetype.TableData(fontData, records, "CFF ")
}

// ExtractBinaryWidths dispatches to TrueType or CFF width extraction based
// on the font data signature.
func ExtractBinaryWidths(ctx context.Context, fontData []byte) map[uint16]float64 {
	switch detectFontFormat(fontData) {
	case fontFormatTrueType:
		w, _ := truetype.GlyphWidths(ctx, fontData)
		return w
	case fontFormatOTTO:
		// OpenType requires an hmtx table, and per the OpenType spec its advance
		// widths are authoritative for the glyphs, including CFF-outline fonts.
		// Read hmtx directly: the embedded CFF charstring widths can encode a
		// nominal value that does not equal the real advance, which would make
		// litho's width check disagree with the /Widths array (and veraPDF).
		w, _ := truetype.GlyphWidths(ctx, fontData)
		return w
	case fontFormatBareCFF:
		w, _ := cff.GlyphWidths(ctx, fontData)
		return w
	case fontFormatUnknown:
		// Try TrueType as fallback.
		w, _ := truetype.GlyphWidths(ctx, fontData)
		return w
	}
	return nil
}

// ExtractCIDBinaryWidths extracts advance widths keyed by CID for a CIDFont
// program. CID-keyed CFF fonts (CIDFontType0) map CID to GID through the
// CFF charset, so the GID-keyed [ExtractBinaryWidths] reads the wrong
// charstrings on subsets; TrueType CIDFonts (CIDFontType2) keep the
// GID-keyed map under the Identity CIDToGIDMap assumption the callers
// already make.
func ExtractCIDBinaryWidths(ctx context.Context, fontData []byte) map[uint16]float64 {
	switch detectFontFormat(fontData) {
	case fontFormatOTTO:
		cffData, err := extractCFFFromOTTO(fontData)
		if err != nil {
			return nil
		}
		w, _ := cff.CIDWidths(ctx, cffData)
		return w
	case fontFormatBareCFF:
		w, _ := cff.CIDWidths(ctx, fontData)
		return w
	case fontFormatTrueType, fontFormatUnknown:
		return ExtractBinaryWidths(ctx, fontData)
	}
	return nil
}

// ExtractCharCodeToGID returns a charCode → GID mapping for TrueType fonts.
// Returns nil for non-TrueType fonts (CFF/Type1).
func ExtractCharCodeToGID(ctx context.Context, fontData []byte) map[uint16]uint16 {
	if len(fontData) < 4 {
		return nil
	}
	sig := fontData[:4]

	// TrueType or OTTO with cmap table.
	if (sig[0] == 0 && sig[1] == 1 && sig[2] == 0 && sig[3] == 0) ||
		string(sig) == "true" || string(sig) == "OTTO" {
		m, _ := truetype.CharCodeToGID(ctx, fontData)
		return m
	}

	return nil
}

// ExtractSymbolCharCodeToGID returns a character-code → GID mapping for a
// symbolic TrueType font, resolved through its (3,0) Microsoft Symbol or (1,0)
// Macintosh cmap subtable (ISO 32000-1 §9.6.6.4). Returns nil for non-TrueType
// programs or when the font carries neither subtable.
func ExtractSymbolCharCodeToGID(ctx context.Context, fontData []byte) map[uint16]uint16 {
	if len(fontData) < 4 {
		return nil
	}
	sig := fontData[:4]
	if (sig[0] == 0 && sig[1] == 1 && sig[2] == 0 && sig[3] == 0) ||
		string(sig) == "true" || string(sig) == "OTTO" {
		m, _ := truetype.SymbolCharCodeToGID(ctx, fontData)
		return m
	}
	return nil
}

// ExtractGlyphNames returns a GID → glyph name mapping from a font program.
// For TrueType/OpenType: uses the post table.
// For CFF: uses the charset + string table.
// Returns nil if the font type is unrecognized or has no glyph names.
func ExtractGlyphNames(ctx context.Context, fontData []byte) map[uint16]string {
	if len(fontData) < 4 {
		return nil
	}
	sig := fontData[:4]

	// TrueType or OTTO: use post table.
	if (sig[0] == 0 && sig[1] == 1 && sig[2] == 0 && sig[3] == 0) ||
		string(sig) == "true" || string(sig) == "OTTO" {
		m, _ := truetype.PostGlyphNames(ctx, fontData)
		return m
	}

	// Bare CFF font.
	if sig[0] == 1 && sig[1] == 0 {
		m, _ := cff.GlyphNames(ctx, fontData)
		return m
	}

	// Type1 (PFA "%!" or PFB 0x80): parse the eexec /CharStrings for glyph
	// names. The GID keys are synthetic; callers that compare against /CharSet
	// use the name values, so the keys need only be distinct.
	if (sig[0] == '%' && sig[1] == '!') || sig[0] == 0x80 {
		names := type1.ParseCharStringNames(fontData)
		if len(names) == 0 {
			return nil
		}
		m := make(map[uint16]string, len(names))
		for i, n := range names {
			m[uint16(i)] = n //nolint:gosec // A Type1 font holds at most 256 glyphs, so i fits in uint16.
		}
		return m
	}

	return nil
}

// ExtractNumGlyphs returns the glyph count (maxp numGlyphs) of a TrueType font
// program, or 0 when fontData is not a parseable TrueType (sfnt) program.
func ExtractNumGlyphs(fontData []byte) int {
	if len(fontData) < 4 {
		return 0
	}
	sig := fontData[:4]
	if (sig[0] == 0 && sig[1] == 1 && sig[2] == 0 && sig[3] == 0) || string(sig) == "true" {
		n, _ := truetype.NumGlyphs(fontData)
		return n
	}
	return 0
}

// ExtractNumberOfHMetrics returns the hhea numberOfHMetrics of a TrueType font
// program, or 0 when fontData is not a parseable TrueType (sfnt) program.
func ExtractNumberOfHMetrics(fontData []byte) int {
	if len(fontData) < 4 {
		return 0
	}
	sig := fontData[:4]
	if (sig[0] == 0 && sig[1] == 1 && sig[2] == 0 && sig[3] == 0) || string(sig) == "true" {
		n, _ := truetype.NumberOfHMetrics(fontData)
		return n
	}
	return 0
}

// ExtractValidCIDs returns the set of CIDs present in a CID-keyed CFF font.
// Returns nil for non-CID or non-CFF fonts.
func ExtractValidCIDs(fontData []byte) map[uint16]bool {
	if len(fontData) < 4 {
		return nil
	}
	// Bare CFF font.
	if fontData[0] == 1 && fontData[1] == 0 {
		m, _ := cff.ValidCIDs(fontData)
		return m
	}
	// OTTO (CFF inside OpenType).
	if string(fontData[:4]) == "OTTO" {
		records, err := truetype.ParseTableDirectory(fontData)
		if err != nil {
			return nil
		}
		cffData, err := truetype.TableData(fontData, records, "CFF ")
		if err != nil {
			return nil
		}
		m, _ := cff.ValidCIDs(cffData)
		return m
	}
	return nil
}

// ExtractNameKeyedCFFGlyphs is [cff.NameKeyedGlyphs] for a font program that
// is a bare CFF or an OpenType font carrying a CFF table.
func ExtractNameKeyedCFFGlyphs(fontData []byte) (glyphs int, defaultWidthX float64, ok bool) {
	switch detectFontFormat(fontData) {
	case fontFormatBareCFF:
		return cff.NameKeyedGlyphs(fontData)
	case fontFormatOTTO:
		cffData, err := extractCFFFromOTTO(fontData)
		if err != nil {
			return 0, 0, false
		}
		return cff.NameKeyedGlyphs(cffData)
	case fontFormatTrueType, fontFormatUnknown:
	}
	return 0, 0, false
}

// readOS2AvgWidth extracts xAvgCharWidth from the OS/2 table. Returns 0 if
// the table is absent or invalid. The value is in font design units.
func readOS2AvgWidth(data []byte) uint16 {
	records, err := truetype.ParseTableDirectory(data)
	if err != nil {
		return 0
	}
	os2Data, err := truetype.TableData(data, records, "OS/2")
	if err != nil || len(os2Data) < 4 {
		return 0
	}
	// xAvgCharWidth is at offset 2 (after version at offset 0).
	return binary.BigEndian.Uint16(os2Data[2:4])
}

// ReadUnitsPerEm extracts unitsPerEm from the TrueType/OpenType head table.
func ReadUnitsPerEm(data []byte) uint16 {
	if len(data) < 4 {
		return 1000 //nolint:mnd // safe default
	}
	// Bare CFF font: use CFF FontMatrix instead of TrueType head table.
	if data[0] == 1 && data[1] == 0 {
		if upm := cff.ReadUnitsPerEm(data); upm > 0 {
			return upm
		}
		return 1000 //nolint:mnd // A CFF with an unreadable FontMatrix falls back to its 1000-upm default (CFF spec, Tech Note #5176, Table 9).
	}
	records, err := truetype.ParseTableDirectory(data)
	if err != nil {
		return 1000 //nolint:mnd // safe default
	}
	headData, err := truetype.TableData(data, records, "head")
	if err != nil || len(headData) < 20 { //nolint:mnd // unitsPerEm occupies head bytes 18:20 (OpenType spec, head), so a shorter table cannot hold it.
		return 1000 //nolint:mnd // The conventional default when head is unusable; 1000 upm matches the CFF default and most Type 1 derived fonts.
	}
	// unitsPerEm is at offset 18 in the head table.
	return binary.BigEndian.Uint16(headData[18:20]) //nolint:mnd // unitsPerEm is a uint16 at head offset 18 (OpenType spec, head).
}

// type1EncodingRe matches "dup <code> /<name> put" lines in Type1 font programs.
var type1EncodingRe = regexp.MustCompile(`dup\s+(\d+)\s+/(\S+)\s+put`)

// extractType1Encoding parses the encoding from an embedded Type1 font program
// (/FontFile) and fills any holes in font.Encoding. This is needed when the
// PDF font dictionary has no /Encoding or an incomplete /Differences array,
// because the font program itself defines the authoritative encoding.
func extractType1Encoding(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, font *Font, overwrite bool) {
	fdDict := document.ResolveFontDescriptor(ctx, doc, fontDict)
	if fdDict == nil {
		return
	}

	fontData := decodeFontStream(ctx, doc, fdDict, "FontFile")
	if fontData == nil {
		return
	}

	// Look for the encoding section between "/Encoding" and "readonly def"
	encStart := bytes.Index(fontData, []byte("/Encoding"))
	if encStart == -1 {
		return
	}
	encEnd := bytes.Index(fontData[encStart:], []byte("readonly def"))
	if encEnd == -1 {
		encEnd = len(fontData) - encStart
	}
	encSection := fontData[encStart : encStart+encEnd]

	// Parse "dup <code> /<name> put" entries.
	for _, match := range type1EncodingRe.FindAllSubmatch(encSection, -1) {
		code, err := strconv.Atoi(string(match[1]))
		if err != nil || code < 0 || code > 255 {
			continue
		}
		glyphName := string(match[2])
		// When overwrite is false, only fill holes (PDF-level /Encoding
		// takes precedence). When true, the font program is authoritative.
		if !overwrite {
			if _, exists := font.Encoding[byte(code)]; exists {
				continue
			}
		}
		if builtinNumericNameMisreads(glyphName, code) {
			continue
		}
		if r, ok := encodings.GlyphToRuneFull(glyphName); ok {
			if font.Encoding == nil {
				font.Encoding = make(map[byte]rune)
			}
			font.Encoding[byte(code)] = r
		}
	}
}

// ExtractCFFEncodingNames reads the built-in encoding from a CFF (Type1C /
// FontFile3) font program and returns a map of charCode → glyphName, or nil
// when the program carries no encoding. It is the CFF analogue of
// [ExtractType1EncodingNames].
func ExtractCFFEncodingNames(fontData []byte) map[byte]string {
	return cff.ExtractEncoding(fontData)
}

// ExtractCFFPredefinedEncodingNames returns the complete predefined encoding a
// CFF program declares, Standard or Expert, without intersecting it against the
// font's charset. Returns nil when the program builds its own encoding. See
// [cff.PredefinedEncodingNames] for why the two differ.
func ExtractCFFPredefinedEncodingNames(fontData []byte) map[byte]string {
	return cff.PredefinedEncodingNames(fontData)
}

// ExtractType1AdvanceWidths returns the advance width, in the font's own design
// units (before the /FontMatrix scale), of every glyph a raw Type 1 (/FontFile)
// program defines, keyed by glyph name. Scale the values by
// [ExtractType1FontMatrixScale] to reach text space. Returns nil when no
// charstring widths can be read.
func ExtractType1AdvanceWidths(fontData []byte) map[string]float64 {
	return type1.ParseCharStringWidths(fontData)
}

// ExtractType1FontMatrixScale returns the x-scale of a raw Type 1 program's
// /FontMatrix, which converts a charstring advance to text space (a 1000-unit
// space corresponds to scale 0.001). The bool reports whether an explicit
// matrix was found; false means the 0.001 default was assumed.
func ExtractType1FontMatrixScale(fontData []byte) (float64, bool) {
	return type1.FontMatrixScale(fontData)
}

// ExtractType1BuiltinEncoding returns the raw Type 1 program's built-in
// encoding as a charCode → glyphName array. A subset font commonly maps codes
// to synthetic G-names here that the PDF /Encoding's standard names do not
// cover.
func ExtractType1BuiltinEncoding(fontData []byte) [256]string {
	return type1.ParseEncodingPS(fontData)
}

// ExtractType1NotdefFilled reports whether a raw Type 1 font program fills its
// built-in encoding vector with .notdef before listing codes, so a code the
// vector does not list names .notdef rather than nothing.
func ExtractType1NotdefFilled(fontData []byte) bool {
	return type1.EncodingFillsNotdef(fontData)
}

// ExtractType1EncodingNames reads the built-in encoding from a raw Type 1
// font program (/FontFile stream) and returns a map of charCode → glyphName.
// Returns nil if the encoding section cannot be found or parsed.
func ExtractType1EncodingNames(fontData []byte) map[byte]string {
	encStart := bytes.Index(fontData, []byte("/Encoding"))
	if encStart == -1 {
		return nil
	}
	encEnd := bytes.Index(fontData[encStart:], []byte("readonly def"))
	if encEnd == -1 {
		encEnd = len(fontData) - encStart
	}
	encSection := fontData[encStart : encStart+encEnd]

	result := make(map[byte]string)
	for _, match := range type1EncodingRe.FindAllSubmatch(encSection, -1) {
		code, err := strconv.Atoi(string(match[1]))
		if err != nil || code < 0 || code > 255 { //nolint:mnd // A simple font addresses one byte of character codes, 0-255 (ISO 32000-1, 9.6.6.2).
			continue
		}
		glyphName := string(match[2])
		if glyphName != ".notdef" {
			result[byte(code)] = glyphName //nolint:gosec // the guard above rejects code outside 0-255
		}
	}

	if len(result) == 0 {
		return nil
	}
	return result
}

// applyType1FontFileEncoding parses the PostScript /Encoding vector
// from a Type 1 font's /FontFile stream and populates font.Encoding
// with the glyph names that resolve through [resolveGlyphRune]. The
// font dict must not already declare /Encoding; the caller is
// responsible for honouring the PDF-level override. Predefined
// encoding references (StandardEncoding, WinAnsiEncoding, and so on)
// swap in the matching map from [encodings]; explicit "dup N /name
// put" vectors are applied one byte at a time.
func applyType1FontFileEncoding(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, font *Font, isDingbats, isSymbol bool) {
	fdDict := document.ResolveFontDescriptor(ctx, doc, fontDict)
	if fdDict == nil {
		return
	}
	fontData := decodeFontStream(ctx, doc, fdDict, "FontFile")
	if fontData == nil {
		return
	}
	enc := type1.ParseEncoding(fontData)
	if isType1NamedEncoding(enc) {
		// Bulk swap on recognised predefined encodings; unrecognised
		// names fall through without altering the default because a
		// slot-by-slot fill would repeat the same name across every
		// byte, which is worse than leaving the caller-set default.
		swapType1NamedEncoding(font, enc[0])
		return
	}
	for i, name := range enc {
		if name == "" || name == ".notdef" || builtinNumericNameMisreads(name, i) {
			continue
		}
		if r, ok := resolveGlyphRune(name, isDingbats, isSymbol); ok {
			font.Encoding[byte(i)] = r //nolint:gosec // i is bounded by 0..255 via the array size
		}
	}
}

// builtinNumericNameMisreads reports whether name, found at code in a font
// program's built-in encoding, is a numeric glyph name ("Gxx", "g00xx", "Cdd",
// "Cxx") whose number names a character other than code.
//
// ISO 32000-1, 9.10.2 maps a simple font's codes to Unicode through glyph
// names the Adobe Glyph List carries. A numeric name is not one of them, so
// reading it as a number is a guess at the producer's convention. Producers
// that number glyphs by their code write the number in decimal or in
// hexadecimal, and the two readings of "G97" disagree: a guess that lands on
// the code itself confirms the convention, one that lands elsewhere reads
// the wrong one and names a character the code does not carry. The caller
// then leaves the code to the default encoding, which is what PDF.js reads,
// since it never takes a built-in encoding's numeric names as numbers
// (src/core/fonts.js, adjustType1ToUnicode, through getUnicodeForGlyph).
//
// # Corpus evidence
//
//   - govdocs thread_300_300161.pdf, thread_200_200155.pdf, thread_000_000364.pdf,
//     thread_400_400786.pdf: Times New Roman programs whose built-in
//     encodings name code c "G<c in decimal>". Read as hexadecimal, "G97"
//     became U+0097 and "G32" became "2", so every "a" and every space came
//     out wrong.
//   - govdocs thread_300_300606.pdf: a program naming code c "G<c in
//     hexadecimal>" keeps its reading, which PDF.js shares.
func builtinNumericNameMisreads(name string, code int) bool {
	r, ok := encodings.NumericGlyphName(name)
	return ok && r != rune(code) //nolint:gosec // code is a one-byte character code, 0..255
}

// applyType1CharStringWidths fills gaps in font.Widths from the
// advance widths stored in each glyph's charstring. The byte→glyph
// name mapping comes from the same Type 1 encoding vector the
// encoding-fill step uses, so the lookup does not rely on a lossy
// Unicode round-trip that would miss symbol/dingbats names outside
// the main Adobe Glyph List. Every width the /Widths array states, 0
// included, stays authoritative ([Font.simpleWidth]); only entries it
// leaves unreadable get filled.
func applyType1CharStringWidths(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, font *Font) {
	fdDict := document.ResolveFontDescriptor(ctx, doc, fontDict)
	if fdDict == nil {
		return
	}
	fontData := decodeFontStream(ctx, doc, fdDict, "FontFile")
	if fontData == nil {
		return
	}
	widths := type1.ParseCharStringWidths(fontData)
	if len(widths) == 0 {
		return
	}
	names := type1.ParseEncoding(fontData)

	for code := 0; code <= 255; code++ {
		name := names[code]
		if name == "" || name == ".notdef" {
			continue
		}
		idx := code - font.FirstChar
		if idx < 0 || idx >= len(font.Widths) {
			continue
		}
		if _, stated := font.simpleWidth(code); stated {
			continue
		}
		w, ok := widths[name]
		if !ok || w == 0 {
			continue
		}
		font.Widths[idx] = w
	}
}

// isType1NamedEncoding reports whether the parsed encoding represents
// a predefined-reference form where every slot carries the same name.
// [type1.ParseEncoding] signals that shape by repeating the name;
// detecting it here lets the caller pick between a bulk swap and a
// slot-by-slot fill.
func isType1NamedEncoding(enc [256]string) bool {
	first := enc[0]
	if first == "" {
		return false
	}
	for _, name := range enc[1:] {
		if name != first {
			return false
		}
	}
	return true
}

// swapType1NamedEncoding replaces font.Encoding with the predefined
// table that the parsed name references. Unrecognised names are a
// no-op: the caller's default encoding stays in place.
func swapType1NamedEncoding(font *Font, name string) {
	switch name {
	case "StandardEncoding":
		font.Encoding = copyEncoding(encodings.StandardEncoding)
	case "WinAnsiEncoding":
		font.Encoding = encodings.WinAnsiDecoding()
	case "MacRomanEncoding":
		font.Encoding = copyEncoding(encodings.MacRomanEncoding)
	case "MacExpertEncoding":
		font.Encoding = copyEncoding(encodings.MacExpertEncoding)
	}
}

// extractEmbeddedCmap attempts to extract a GID→Unicode mapping from the
// embedded TrueType (/FontFile2) or OpenType (/FontFile3) font program.
// For Type0 fonts the FontDescriptor lives inside the CIDFont descendant.
func extractEmbeddedCmap(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, font *Font) {
	fdDict := document.ResolveFontDescriptor(ctx, doc, fontDict)
	if fdDict == nil {
		return
	}

	// Try /FontFile2 (TrueType), then /FontFile3 (OpenType/CFF).
	fontData := decodeFontStream(ctx, doc, fdDict, "FontFile2")
	if fontData == nil {
		fontData = decodeFontStream(ctx, doc, fdDict, "FontFile3")
	}
	if fontData == nil {
		return
	}

	gidMap := ExtractGIDToUnicode(ctx, fontData)
	if len(gidMap) == 0 {
		return
	}

	font.GIDToUnicode = gidMap
}

// ExtractGIDToUnicodeAuthoritative returns a glyph ID to Unicode mapping only
// when the font states one that is genuinely Unicode.
//
// [ExtractGIDToUnicode] will fall back to a TrueType (1,0) Macintosh subtable
// and read its byte as a code point. That is a reasonable last resort for
// DISPLAYING text, but it is not a fact about the font: a symbolic subset
// numbers its glyphs from 1, so inverting that subtable claims glyph 1 means
// U+0001. Writing such a claim into a /ToUnicode CMap is worse than writing
// nothing, because extraction prefers /ToUnicode over the font's own encoding
// and readable text becomes control bytes and stray punctuation.
//
// Callers that PERSIST a mapping use this; callers that merely read text use
// [ExtractGIDToUnicode]. CFF glyph names resolve through the Adobe Glyph List
// and are authoritative, so only the TrueType path is gated.
func ExtractGIDToUnicodeAuthoritative(ctx context.Context, fontData []byte) map[uint16]rune {
	switch detectFontFormat(fontData) {
	case fontFormatTrueType, fontFormatUnknown:
		if !hasUnicodeCmapSubtable(fontData) {
			return nil
		}
	case fontFormatOTTO, fontFormatBareCFF:
	}
	return ExtractGIDToUnicode(ctx, fontData)
}

// hasUnicodeCmapSubtable reports whether a TrueType font declares a cmap
// subtable whose character codes are Unicode: platform 0 (Unicode), or
// platform 3 with encoding 1 (BMP) or 10 (full repertoire). Platform 3
// encoding 0 is symbolic and platform 1 is Macintosh; neither states Unicode
// (OpenType spec, 'cmap' platform IDs).
func hasUnicodeCmapSubtable(fontData []byte) bool {
	subtables, err := truetype.CmapSubtables(fontData)
	if err != nil {
		return false
	}
	for _, s := range subtables {
		if s.PlatformID == 0 || (s.PlatformID == 3 && (s.EncodingID == 1 || s.EncodingID == 10)) {
			return true
		}
	}
	return false
}

// ExtractGIDToUnicode dispatches font data to the appropriate parser
// (TrueType or CFF) based on the font signature and returns a
// glyph ID to Unicode rune mapping.
func ExtractGIDToUnicode(ctx context.Context, fontData []byte) map[uint16]rune {
	switch detectFontFormat(fontData) {
	case fontFormatTrueType:
		gidMap, _ := truetype.GlyphToUnicode(ctx, fontData)
		return gidMap
	case fontFormatOTTO:
		cffData, err := extractCFFFromOTTO(fontData)
		if err != nil {
			return nil
		}
		gidMap, _ := cff.GlyphToUnicode(ctx, cffData)
		return gidMap
	case fontFormatBareCFF:
		gidMap, _ := cff.GlyphToUnicode(ctx, fontData)
		return gidMap
	case fontFormatUnknown:
		// Try TrueType as fallback.
		gidMap, _ := truetype.GlyphToUnicode(ctx, fontData)
		return gidMap
	}
	return nil
}

// applyAdobeCIDFallback checks the CIDSystemInfo of a Type0 font's descendant
// CIDFont. If the registry is "Adobe" and the ordering matches a known character
// collection (Japan1, GB1, CNS1, Korea1), it populates font.GIDToUnicode from
// the predefined Adobe CID→Unicode map, enabling text extraction for CJK fonts
// without ToUnicode CMaps or embedded font programs.
func applyAdobeCIDFallback(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, font *Font) {
	dfObj, ok := fontDict["DescendantFonts"]
	if !ok {
		return
	}
	dfArr, err := document.ResolveArray(ctx, doc, dfObj)
	if err != nil {
		doc.Logger.Debug("resolving DescendantFonts for Adobe CID fallback", slog.Any("err", err))
	}
	if len(dfArr) == 0 {
		return
	}
	cidDict, err := document.ResolveDict(ctx, doc, dfArr[0])
	if err != nil {
		doc.Logger.Debug("resolving CIDFont dict for Adobe CID fallback", slog.Any("err", err))
	}
	if cidDict == nil {
		return
	}

	// Extract /CIDSystemInfo.
	csObj, ok := cidDict["CIDSystemInfo"]
	if !ok {
		return
	}
	csDict, err := document.ResolveDict(ctx, doc, csObj)
	if err != nil {
		doc.Logger.Debug("resolving CIDSystemInfo dict", slog.Any("err", err))
	}
	if csDict == nil {
		return
	}

	registry, err := document.ResolveString(ctx, doc, csDict["Registry"])
	if err != nil {
		doc.Logger.Debug("resolving CIDSystemInfo Registry", slog.Any("err", err))
	}
	if registry == "" || string(registry) != "Adobe" {
		return
	}

	ordering, err := document.ResolveString(ctx, doc, csDict["Ordering"])
	if err != nil {
		doc.Logger.Debug("resolving CIDSystemInfo Ordering", slog.Any("err", err))
	}
	if ordering == "" {
		return
	}

	cidToUnicode := cmap.AdobeCIDToUnicode(string(ordering))
	if cidToUnicode == nil {
		return
	}

	// Build GIDToUnicode from the predefined map.
	// For CIDFonts, CID ≈ GID when CIDToGIDMap is /Identity or absent.
	font.GIDToUnicode = make(map[uint16]rune, len(cidToUnicode))
	for cid, uni := range cidToUnicode {
		if cid <= 0xFFFF {
			font.GIDToUnicode[uint16(cid)] = uni //nolint:gosec // cid bounds-checked
		}
	}
}

// applyCIDToGIDIdentityFallback checks whether a Type0 font's CIDFont descendant
// declares /CIDToGIDMap /Identity. When present, CID values equal GID values,
// and in the absence of a ToUnicode CMap or embedded font program the CID is
// treated as a direct Unicode code point. This is the common pattern for CJK
// fonts that rely on system fonts.
func applyCIDToGIDIdentityFallback(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, font *Font) {
	dfObj, ok := fontDict["DescendantFonts"]
	if !ok {
		return
	}
	dfArr, err := document.ResolveArray(ctx, doc, dfObj)
	if err != nil {
		doc.Logger.Debug("resolving DescendantFonts for CIDToGID fallback", slog.Any("err", err))
	}
	if len(dfArr) == 0 {
		return
	}
	cidDict, err := document.ResolveDict(ctx, doc, dfArr[0])
	if err != nil {
		doc.Logger.Debug("resolving CIDFont dict for CIDToGID fallback", slog.Any("err", err))
	}
	if cidDict == nil {
		return
	}
	mapObj, ok := cidDict["CIDToGIDMap"]
	if !ok {
		return
	}

	// /CIDToGIDMap /Identity: CID = GID, treat as direct Unicode.
	//
	// Only where a CID is Unicode-like. Under an Adobe CJK collection a CID
	// indexes that collection (ISO 32000-1, 9.7.3), so reading it as a code
	// point yields mojibake: Adobe-Japan1 CID 843 is あ, not U+034B. Those
	// fonts already resolve through the registry's CID to Unicode table, and
	// an absent /CIDToGIDMap means Identity anyway, so writing the key
	// explicitly must not change the text a document yields.
	mapName, _ := document.ResolveName(ctx, doc, mapObj)
	if mapName == "Identity" {
		if !isCJKOrdering(ctx, doc, fontDict) {
			font.CIDIdentityFallback = true
			applyStandardFontGlyphMap(ctx, doc, fontDict, cidDict, font)
		}
		return
	}

	// /CIDToGIDMap stream: 2-byte big-endian GIDs indexed by CID.
	// Parse the stream and combine with the embedded font's GID→Unicode
	// cmap to build a CID→Unicode mapping.
	applyCIDToGIDStream(ctx, doc, fontDict, font, mapObj)
}

// applyStandardFontGlyphMap gives a Type0 font that draws a non-embedded
// standard face through an Identity CMap PDF.js's glyph table for that face,
// and reports whether it did.
//
// Under an Identity CMap the code is the CID (ISO 32000-1, 9.7.5.2), and with
// no /CIDToGIDMap stream the CID is the glyph index of the font program
// (9.7.4.2), so the codes of such a font are glyph IDs of the program
// installed where the file was produced, which the file does not ship.
// [ResolveFont] comes here only when no /ToUnicode CMap parsed, and then none
// of the methods of ISO 32000-1, 9.10.2 applies: the font is composite, and
// its Identity-H or Identity-V CMap and Identity ordering are excluded from
// the predefined CMap route. The clause leaves the character to the reader.
// litho follows PDF.js (fonts.js, fallbackToSystemFont), the reader its text
// extraction is measured against. PDF.js reads such a font through a glyph
// table of the Arial glyph order (standard_fonts.js,
// getGlyphMapForStandardFonts), with supplements for Arial Black and Calibri,
// whose orders differ; [stdfonts] carries the tables, generated from PDF.js
// at the revision of the text goldens. The table is a relaxation of 9.10.2,
// grounded in the glyph order of the named host font rather than in the
// file. A code the table does not name reads as the code point PDF.js gives
// it, the code itself, with no offset.
//
// The gate is PDF.js's own: no embedded program, a CIDFontType2 under an
// Identity CMap, and the name PDF.js identifies the font by
// ([stdfonts.PDFJSFontName]) naming a standard font or one PDF.js maps to a
// standard font ([stdfonts.IdentityGlyphMap]). litho removes a subset tag
// first, as [normalizeBase14Name] does for widths, where PDF.js requires the
// bare face name: ISO 32000-1, 9.6.4 gives the tag to an embedded subset, so
// on a font that embeds nothing it names no other face. The font must not
// belong to an Adobe CJK collection:
// PDF.js reads such a font's codes through the collection's UCS2 CMap first,
// and so does litho, before reaching here.
//
// # Corpus evidence
//
//   - pdfjs issue9195.pdf: written by Fineco Software v2.05, its three
//     non-embedded faces, Calibri, Calibri,Bold and Calibri,Italic, state
//     /CIDToGIDMap /Identity and carry a /ToUnicode CMap written in UTF-16,
//     which litho does not parse. The page matched 1 of the 151 words of the
//     PDF.js golden and matches all 151 with the table.
//   - pdfjs issue19550.pdf, issue20453.pdf: a non-embedded Arial under
//     Identity-H shows Cyrillic at the glyph IDs of the Arial order (У is
//     589). Reading them as CID + 29 gave IPA letters ("ɪɺʂʅɹɷʖ" for
//     "Угловая"); with the table both pages match the PDF.js golden.
//   - pdfjs issue5801.pdf: a non-embedded ArialBlack,Bold whose /ToUnicode
//     is a copy of the Identity-H CMap, which PDF.js reads as no mapping at
//     all. The table matches its golden.
//   - pdfjs issue15044.pdf (ArialUnicodeMS), issue13916.pdf (ArialMT),
//     bug921760.pdf and issue6127.pdf (TimesNewRoman): non-embedded faces
//     under Identity-H. The first matches its golden, and the other three
//     now differ from theirs only in spacing or, for issue13916.pdf, in the
//     U+0000 PDF.js emits for glyph ID 0.
func applyStandardFontGlyphMap(ctx context.Context, doc *document.Document, fontDict, cidDict ast.PdfDict, font *Font) bool {
	if cidFontHasEmbeddedProgram(ctx, doc, fontDict) || !isIdentityEncoding(ctx, doc, fontDict) || isCJKOrdering(ctx, doc, fontDict) {
		return false
	}
	subtype, err := document.ResolveName(ctx, doc, cidDict["Subtype"])
	if err != nil {
		doc.Logger.Debug("resolving CIDFont Subtype for the standard font glyph map", slog.Any("err", err))
		return false
	}
	if subtype != "CIDFontType2" {
		return false
	}
	baseFont, err := document.ResolveName(ctx, doc, cidDict["BaseFont"])
	if err != nil {
		doc.Logger.Debug("resolving CIDFont BaseFont for the standard font glyph map", slog.Any("err", err))
	}
	var fontName ast.PdfName
	if fd, err := document.ResolveDict(ctx, doc, cidDict["FontDescriptor"]); err == nil && fd != nil {
		if fontName, err = document.ResolveName(ctx, doc, fd["FontName"]); err != nil {
			doc.Logger.Debug("resolving FontName for the standard font glyph map", slog.Any("err", err))
		}
	}
	m := stdfonts.IdentityGlyphMap(stdfonts.PDFJSFontName(stripSubsetTag(string(fontName)), stripSubsetTag(string(baseFont))))
	if m == nil {
		return false
	}
	if font.GIDToUnicode == nil {
		font.GIDToUnicode = make(map[uint16]rune, len(m))
	}
	maps.Copy(font.GIDToUnicode, m)
	return true
}

// cidFontHasEmbeddedProgram reports whether a Type0 font's descendant
// CIDFont ships an embedded font program (FontFile, FontFile2, or
// FontFile3) in its FontDescriptor. Non-embedded fonts are rendered by
// viewers using the host system's matching face and therefore follow
// the host's standard TrueType GID layout; the detector uses this
// signal to decide whether to apply a CID→Unicode offset by default.
func cidFontHasEmbeddedProgram(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) bool {
	fdDict := document.ResolveFontDescriptor(ctx, doc, fontDict)
	if fdDict == nil {
		return false
	}
	for _, key := range []ast.PdfName{"FontFile", "FontFile2", "FontFile3"} {
		if _, ok := fdDict[key]; ok {
			return true
		}
	}
	return false
}

// stripSubsetTag removes the subset tag of ISO 32000-1, 9.6.4, six
// uppercase letters and a plus sign, from the front of a font name.
func stripSubsetTag(name string) string {
	tag, rest, ok := strings.Cut(name, "+")
	if !ok || len(tag) != 6 || strings.IndexFunc(tag, func(r rune) bool { return r < 'A' || r > 'Z' }) >= 0 {
		return name
	}
	return rest
}

// descendantCIDFont returns the CIDFont dictionary a Type0 font names first
// in /DescendantFonts, or nil when it names none.
func descendantCIDFont(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) ast.PdfDict {
	dfArr, err := document.ResolveArray(ctx, doc, fontDict["DescendantFonts"])
	if err != nil || len(dfArr) == 0 {
		return nil
	}
	cidDict, err := document.ResolveDict(ctx, doc, dfArr[0])
	if err != nil {
		doc.Logger.Debug("resolving the descendant CIDFont", slog.Any("err", err))
		return nil
	}
	return cidDict
}

// hasCIDToGIDMapStream reports whether a Type0 font's descendant CIDFont
// declares /CIDToGIDMap as an indirect reference to a stream (as opposed
// to being absent or set to /Identity). Such a stream indicates the
// producer is explicitly remapping CIDs to subset glyph IDs, which means
// the CID values carry their own semantic identity and should not be
// shifted by the GID-space heuristic in [detectCIDOffset].
func hasCIDToGIDMapStream(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) bool {
	dfObj, ok := fontDict["DescendantFonts"]
	if !ok {
		return false
	}
	dfArr, err := document.ResolveArray(ctx, doc, dfObj)
	if err != nil || len(dfArr) == 0 {
		return false
	}
	cidDict, err := document.ResolveDict(ctx, doc, dfArr[0])
	if err != nil || cidDict == nil {
		return false
	}
	mapObj, ok := cidDict["CIDToGIDMap"]
	if !ok {
		return false
	}
	if name, err := document.ResolveName(ctx, doc, mapObj); err == nil && name != "" {
		return false
	}
	stream, err := document.ResolveStream(ctx, doc, mapObj)
	if err != nil || stream.Dict == nil {
		return false
	}
	return true
}

// applyCIDToGIDStream parses a CIDToGIDMap stream and combines it with the
// embedded font's GID→Unicode cmap to build a synthetic ToUnicode CMap, or,
// when the font's encoding CMap maps codes to other CIDs
// ([Font.encodingCIDKeyed]), the CID-keyed [Font.GIDToUnicode] table.
func applyCIDToGIDStream(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, font *Font, mapObj ast.PdfObject) {
	table := cidToGIDStreamUnicode(ctx, doc, mapObj, ExtractGIDToUnicode(ctx, decodeFontStreamFromDescriptor(ctx, doc, fontDict)))
	if len(table) == 0 {
		return
	}

	// The table is keyed by CID, which a two-byte code is only under
	// Identity-H or Identity-V (ISO 32000-1, 9.7.5.2). Under an encoding CMap
	// that maps codes to other CIDs it stays a CID-keyed table, read at the CID
	// the CMap gives each code ([encodingCIDTableChar]): written as a
	// /ToUnicode CMap it would be read at the code.
	if font.encodingCIDKeyed() {
		font.GIDToUnicode = table
		return
	}

	mapping := make(map[string]string, len(table))
	for cid, uni := range table {
		key := string([]byte{byte(cid >> 8), byte(cid)}) //nolint:gosec // splits a uint16 CID into its two bytes; truncation is the intent
		mapping[key] = string(uni)
	}
	font.ToUnicode = &cmap.CMap{
		Mapping: mapping,
		CodeSpaceRanges: []cmap.CodeSpaceRange{
			{Low: []byte{0x00, 0x00}, High: []byte{0xFF, 0xFF}},
		},
	}
}

// cidToGIDStreamUnicode composes the /CIDToGIDMap stream mapObj, two-byte
// big-endian GIDs indexed by CID (ISO 32000-1, 9.7.4.2, Table 117), with
// gidToUni, the program's glyph-to-Unicode table, into a CID-keyed table. A
// CID the stream sends to GID 0, the .notdef glyph, or whose glyph the table
// gives U+0000 or nothing, stays out. CIDs past the two-byte range stay out
// too: no code of a two-byte codespace selects them.
func cidToGIDStreamUnicode(ctx context.Context, doc *document.Document, mapObj ast.PdfObject, gidToUni map[uint16]rune) map[uint16]rune {
	if len(gidToUni) == 0 {
		return nil
	}
	stream, err := document.ResolveStream(ctx, doc, mapObj)
	if err != nil || stream.Dict == nil {
		return nil
	}
	mapData, err := doc.DecodeStream(ctx, stream)
	if err != nil || len(mapData) < 2 {
		return nil
	}
	numCIDs := min(len(mapData)/2, math.MaxUint16+1)
	table := make(map[uint16]rune)
	for cid := range numCIDs {
		gid := uint16(mapData[cid*2])<<8 | uint16(mapData[cid*2+1])
		if uni, ok := gidToUni[gid]; ok && gid != 0 && uni != 0 {
			table[uint16(cid)] = uni //nolint:gosec // cid <= math.MaxUint16 above.
		}
	}
	return table
}

// ProgramCIDToUnicode returns the CID-keyed character table a Type 0 font's
// /CIDToGIDMap stream and embedded program state: the stream takes a CID to a
// glyph (ISO 32000-1, 9.7.4.2) and the program's Unicode cmap subtable or
// glyph names take the glyph to its character
// ([ExtractGIDToUnicodeAuthoritative]). It is the composition text extraction
// reads such a font through ([applyCIDToGIDStream]), restricted to a program
// that states Unicode, for a caller that persists the mapping. It returns nil
// when the descendant CIDFont has no /CIDToGIDMap stream or the program states
// no Unicode.
func ProgramCIDToUnicode(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) map[uint16]rune {
	if !hasCIDToGIDMapStream(ctx, doc, fontDict) {
		return nil
	}
	cidDict := descendantCIDFont(ctx, doc, fontDict)
	if cidDict == nil {
		return nil
	}
	return cidToGIDStreamUnicode(ctx, doc, cidDict["CIDToGIDMap"], ExtractGIDToUnicodeAuthoritative(ctx, decodeFontStreamFromDescriptor(ctx, doc, fontDict)))
}

// decodeFontStreamFromDescriptor extracts and decodes the embedded font binary
// from the FontDescriptor, looking in DescendantFonts for Type0 fonts.
func decodeFontStreamFromDescriptor(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) []byte {
	fdDict := document.ResolveFontDescriptor(ctx, doc, fontDict)
	if fdDict == nil {
		return nil
	}
	if data := decodeFontStream(ctx, doc, fdDict, "FontFile2"); data != nil {
		return data
	}
	return decodeFontStream(ctx, doc, fdDict, "FontFile3")
}

// readsAsStandardSymbol reports whether a simple font reads its codes as the
// standard Symbol font does: through Symbol's built-in encoding and the
// Symbol glyph table.
//
// ISO 32000-1, 9.6.2.2 names one Symbol font among the standard 14, and
// 9.6.6.1 gives a font with no /Encoding its program's built-in encoding.
// Symbol's own built-in encoding (Annex D.5) is therefore a fact about the
// font named Symbol: once [canonicalFontName] strips the subset tag (9.6.4)
// and a style suffix, the name is "Symbol". Any other name, "WPIconicSymbolsA"
// or "AppleSymbols" or "ItcSymbol-Book" (a Latin text face), says nothing
// about its glyphs, so an embedded program's built-in encoding, cmap and
// glyph names, or the Adobe Glyph List, answer instead.
//
// An unembedded symbolic font whose name holds "symbol" in any case still
// reads as Symbol. With no program, 9.6.6.1 leaves its built-in encoding
// unknown, and PDF.js, which substitutes Symbol for such a font
// (evaluator.js, extractDataStructures, at the pinned revision), reads its
// codes through SymbolSetEncoding. Relaxing 9.6.6.1 there matches the reader
// the text corpora are measured against rather than inventing a Latin reading.
//
// # Corpus evidence
//
//   - govdocs thread_300_300089.pdf: an embedded symbolic WPTypographicSymbols
//     subset with no /Encoding and only a (1,0) cmap subtable read code 0x96
//     through the Symbol encoding, which leaves it empty. Read as Mac OS Roman
//     through that subtable (9.6.6.4), it is the "ñ" PDF.js reads, and the
//     file matches PDF.js.
//   - govdocs thread_200_200611.pdf: unembedded symbolic WPIconicSymbolsA and
//     WPIconicSymbolsB read as PDF.js reads them only through the Symbol
//     encoding. Read through WinAnsi, the file no longer matches PDF.js.
//   - govdocs thread_400_400895.pdf: an embedded WPIconicSymbolsA Type 1
//     program names code 0x8E "c142", which no glyph list resolves. The
//     Symbol encoding left the code empty; the WinAnsi default of a symbolic
//     Type 1 font reads "Ž", the /Zcaron its /CharSet lists, where PDF.js
//     reads Mac OS Roman "é". The page matched PDF.js under neither.
//   - All corpora: the fonts that no longer read as Symbol sit in 79 files,
//     69 of which give them a /ToUnicode CMap. Text changes on the three
//     files above alone, compliance verdicts and rule sets on none, and
//     remediated output only on pdfjs issue14203.pdf, whose generated
//     /ToUnicode CMaps change on codes no page shows: its text and veraPDF's
//     verdicts on the output are unchanged.
func readsAsStandardSymbol(baseFont string, symbolic, embedded bool) bool {
	name, _, _ := strings.Cut(canonicalFontName(baseFont), "-")
	if name == "Symbol" {
		return true
	}
	return !embedded && symbolic && strings.Contains(strings.ToLower(baseFont), "symbol")
}

// readsAsStandardDingbats reports whether a simple font reads its codes as the
// standard ZapfDingbats font does: through its built-in encoding and the
// ZapfDingbats glyph table.
//
// The reasoning of [readsAsStandardSymbol] holds unchanged. ISO 32000-1,
// 9.6.2.2 names one ZapfDingbats among the standard 14, and its built-in
// encoding (Annex D.6) is a fact about the font of that name once
// [canonicalFontName] strips the subset tag (9.6.4) and a style suffix. An
// embedded "ZapfDingbatsITC" or "Dingbats" program states its own encoding,
// cmap and glyph names, which 9.6.6.1 and 9.6.6.4 read instead.
//
// An unembedded font keeps the reading PDF.js gives it (evaluator.js,
// extractDataStructures, at the pinned revision): a symbolic font, or one
// named in its symbol-font list, whose name holds "Dingbats" in any case takes
// ZapfDingbatsEncoding. "Dingbats" is the one name of that list the standard
// name does not already cover.
//
// # Corpus evidence
//
//   - All corpora: the fonts that no longer read as ZapfDingbats are embedded
//     ZapfDingbatsITC, ZapfDingbatsStd and Dingbats programs in nine files,
//     one govdocs and eight pdfjs. Text changes on one file alone, compliance
//     verdicts and rule sets on none, and remediated output on none of the
//     nine at PDF/A-2u and PDF/UA-1 in either mode.
//   - govdocs thread_100_100852.pdf: an embedded AEJJAA+ZapfDingbatsITC Type 1
//     subset read an end-of-article mark as "■" through the ZapfDingbats
//     table. Its own /ToUnicode CMap answers instead, which reads no visible
//     character there, as PDF.js does. The file now matches PDF.js on the
//     character comparison; its words still differ elsewhere.
func readsAsStandardDingbats(baseFont string, symbolic, embedded bool) bool {
	name, _, _ := strings.Cut(canonicalFontName(baseFont), "-")
	if name == "ZapfDingbats" {
		return true
	}
	return !embedded && (symbolic || name == "Dingbats") && strings.Contains(strings.ToLower(baseFont), "dingbats")
}

// hasEmbeddedFontFile reports whether a font dict's FontDescriptor carries
// any of the embedded-program streams (/FontFile, /FontFile2, /FontFile3).
// Used to gate heuristics that should only fire for fonts whose glyph
// payload travels with the PDF, so reference-only system fonts are not
// forced through fallbacks that would emit host-dependent text.
func hasEmbeddedFontFile(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) bool {
	fdDict := document.ResolveFontDescriptor(ctx, doc, fontDict)
	if fdDict == nil {
		return false
	}
	for _, key := range []ast.PdfName{"FontFile", "FontFile2", "FontFile3"} {
		if _, ok := fdDict[key]; ok {
			return true
		}
	}
	return false
}

// isCJKOrdering reports whether a Type0 font's CIDSystemInfo /Ordering
// identifies one of Adobe's CJK character collections (Japan1, GB1, CNS1,
// Korea1 / GB-Chinese, KR). A CID of such a collection indexes the
// collection (ISO 32000-1, 9.7.3), so it is never read as a code point.
func isCJKOrdering(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) bool {
	dfObj, ok := fontDict["DescendantFonts"]
	if !ok {
		return false
	}
	dfArr, err := document.ResolveArray(ctx, doc, dfObj)
	if err != nil || len(dfArr) == 0 {
		return false
	}
	cidDict, err := document.ResolveDict(ctx, doc, dfArr[0])
	if err != nil || cidDict == nil {
		return false
	}
	csObj, ok := cidDict["CIDSystemInfo"]
	if !ok {
		return false
	}
	csDict, err := document.ResolveDict(ctx, doc, csObj)
	if err != nil || csDict == nil {
		return false
	}
	ordering, err := document.ResolveString(ctx, doc, csDict["Ordering"])
	if err != nil {
		return false
	}
	switch string(ordering) {
	case "Japan1", "GB1", "CNS1", "Korea1":
		return true
	}
	return false
}

// hasCJKFontName reports whether a BaseFont name indicates a CJK font.
// Detects GBK/GB2312-encoded names (e.g., 黑体 = bytes 0xBA 0xDA 0xCC 0xE5)
// that appear as raw high bytes after PDF name hex decoding (#BA#DA#CC#E5).
func hasCJKFontName(s string) bool {
	highByteCount := 0
	for i := range len(s) {
		b := s[i]
		if b >= 0x80 {
			highByteCount++
		} else {
			highByteCount = 0
		}
		if highByteCount >= 2 {
			return true
		}
	}
	return false
}

// chineseFaceNames lists the GBK-coded BaseFont names of the Chinese faces a
// producer leaves unembedded under a WinAnsiEncoding its codes do not follow.
// It mirrors the list PDF.js (evaluator.js, extractDataStructures) builds from
// PDFium's and MuPDF's tables.
var chineseFaceNames = map[string]struct{}{
	"\xCB\xCE\xCC\xE5":         {}, // SimSun
	"\xBA\xDA\xCC\xE5":         {}, // SimHei
	"\xBF\xAC\xCC\xE5":         {}, // SimKai
	"\xB7\xC2\xCB\xCE":         {}, // SimFang
	"\xBF\xAC\xCC\xE5_GB2312":  {}, // SimKai
	"\xB7\xC2\xCB\xCE_GB2312":  {}, // SimFang
	"\xC1\xA5\xCA\xE9":         {}, // SimLi
	"\xD0\xC2\xCB\xCE":         {}, // SimSun
	"\xB7\xC2\xCB\xCE\xCC\xE5": {}, // SimFang variant
	"\xD0\xA1\xB1\xEA\xCB\xCE": {}, // XiaoBiaoSong
}

// isChineseFaceName reports whether baseFont is one of [chineseFaceNames].
//
// ISO 32000-1, 9.6.6.1 makes a stated WinAnsiEncoding the byte-to-glyph map of
// a simple font, so a code reads as Annex D.2 says. litho relaxes that clause
// for these faces, as PDF.js does: their producers write WinAnsiEncoding over
// two-byte GBK codes, so Annex D.2 yields Latin-1 garbage where GBK-EUC-H
// (Table 118) yields the Chinese the host's face draws.
//
// # Corpus evidence
//
//   - pdfjs SimFang-variant.pdf, XiaoBiaoSong.pdf, issue20489.pdf: each
//     draws its Chinese text through an unembedded TrueType face named here,
//     under /WinAnsiEncoding. Cleared, the text is lost. Read through
//     GBK-EUC-H, issue20489.pdf matches PDF.js given its CMaps, and the other
//     two carry its characters with a space between each.
func isChineseFaceName(baseFont string) bool {
	_, ok := chineseFaceNames[baseFont]
	return ok
}

// simpleBaseEncodingName returns the base encoding a simple font states:
// its /Encoding name, or the /BaseEncoding of an /Encoding dictionary
// (ISO 32000-1, 9.6.6.1, Table 114). It returns "" when the font states none.
func simpleBaseEncodingName(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) string {
	encObj, ok := fontDict["Encoding"]
	if !ok {
		return ""
	}
	if name, err := document.ResolveName(ctx, doc, encObj); err == nil && name != "" {
		return string(name)
	}
	encDict, err := document.ResolveDict(ctx, doc, encObj)
	if err != nil {
		doc.Logger.Debug("resolving simple font /Encoding dictionary", slog.Any("err", err))
	}
	if encDict == nil {
		return ""
	}
	name, err := document.ResolveName(ctx, doc, encDict["BaseEncoding"])
	if err != nil {
		doc.Logger.Debug("resolving simple font /BaseEncoding", slog.Any("err", err))
	}
	return string(name)
}

// applyCIDIdentityHeuristics fills in what the identity CID fallback needs to
// read text: PDF.js's glyph table for a standard face a PDF references
// without embedding ([applyStandardFontGlyphMap]), or else the CID-to-Unicode
// offset.
//
// TODO: [applyCIDToGIDIdentityFallback] turns [Font.CIDIdentityFallback] on for
// an explicit /CIDToGIDMap /Identity without calling this, so that path reads a
// CID as a code point with no offset. An absent /CIDToGIDMap already means
// Identity (ISO 32000-1, 9.7.4.2), so writing the key changes extracted text,
// and a PDF/A fixer writes that key. Calling this from there fixes the shift
// but moves two text goldens whose expectations were captured against the
// short-circuit, so the offset heuristics need settling first: the +29 guess
// for a non-embedded font overrides /W evidence that the font already states
// (Tahoma declares CIDs 1-3).
//
// # Corpus evidence
//
//   - pdfjs ThuluthFeatures.pdf: writing the /CIDToGIDMap /Identity key turns
//     the extracted "Dotless Forms" into "'RWOHVV )RUPV".
//   - pdfjs issue11242_reduced.pdf, issue15719.pdf: the two text goldens that
//     calling this from [applyCIDToGIDIdentityFallback] moves.
func applyCIDIdentityHeuristics(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, font *Font) {
	// Skipped when the CIDFont has an explicit /CIDToGIDMap stream: the
	// stream already remaps CIDs to subset GIDs, so the CID values carry
	// Unicode semantics directly and adding +29 would shift the text
	// (e.g. "6XPPD" becomes "Summa").
	if hasCIDToGIDMapStream(ctx, doc, fontDict) {
		return
	}
	// A non-embedded standard face reads through PDF.js's glyph table for
	// it, and a code the table does not name as the code itself.
	if cidDict := descendantCIDFont(ctx, doc, fontDict); cidDict != nil && applyStandardFontGlyphMap(ctx, doc, fontDict, cidDict, font) {
		return
	}
	// Many non-embedded fonts use GIDs where the first printable character
	// (space, U+0020) starts at GID 3 (offset 29). If /W starts at CID 3,
	// assume CID + 29 = Unicode.
	font.CIDOffset = detectCIDOffset(font.CIDWidths)
	// Non-embedded fonts (e.g. /Impact referenced by name, never shipped
	// with the PDF) follow the host font's standard TrueType GID table
	// where space is GID 3. When no /FontFile2 or /FontFile3 is present
	// the /W array often exposes unusual values on CIDs 0-2 that defeat
	// the width-based detection above; apply the +29 offset
	// unconditionally so CID 26 → '7', CID 22 → '3', etc.
	if font.CIDOffset == 0 && !cidFontHasEmbeddedProgram(ctx, doc, fontDict) {
		font.CIDOffset = 29
	}
}

// detectCIDOffset infers the CID-to-Unicode offset from the /W (widths) map.
// Non-embedded TrueType fonts commonly place space (U+0020) at GID 3,
// meaning CID + 29 = Unicode. Detection requires:
//   - at least 5 CIDs in the widths map
//   - CID 3 has a space-like width (200-300 units)
//   - CIDs 0-2 are absent or have negligible width (< 10 units)
func detectCIDOffset(cidWidths map[int]float64) int {
	if len(cidWidths) < 5 {
		return 0
	}
	w3, has3 := cidWidths[3]
	if !has3 || w3 < 200 || w3 > 300 {
		return 0
	}
	// CIDs below 3 must be absent or have negligible width (< 10 units).
	// GIDs 0-2 are typically .notdef and control characters with zero/tiny widths.
	for cid := range 3 {
		if w, ok := cidWidths[cid]; ok && w >= 10 {
			return 0
		}
	}
	return 29
}

// fontIsSymbolicByFlags reports whether the FontDescriptor's /Flags entry
// has bit 3 (Symbolic) set per ISO 32000-1 Table 123. The flag tells the
// PDF interpreter that the font's glyph repertoire is bespoke and the
// character codes do not follow the Adobe glyph list, so a fallback to
// StandardEncoding would surface the wrong characters at high bytes.
// Returns false when no FontDescriptor is reachable.
func fontIsSymbolicByFlags(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) bool {
	fdDict := document.ResolveFontDescriptor(ctx, doc, fontDict)
	if fdDict == nil {
		return false
	}
	flagsObj, ok := fdDict["Flags"]
	if !ok {
		return false
	}
	flagsInt, ok := flagsObj.(ast.PdfInt)
	if !ok {
		return false
	}
	const symbolicBit = 1 << 2 // bit 3 in Adobe's 1-indexed numbering
	return int(flagsInt)&symbolicBit != 0
}

// trueTypeTakesStandardEncoding reports whether a TrueType font that states
// no /Encoding reads its codes through StandardEncoding: a font that embeds
// no program and sets the Nonsymbolic flag (ISO 32000-1, Table 123) without
// the Symbolic one.
//
// ISO 32000-1, 9.6.6.1 makes StandardEncoding the encoding of a nonsymbolic
// font that states no other, while 9.6.6.4 names only MacRomanEncoding and
// WinAnsiEncoding for a nonsymbolic TrueType font and leaves an absent
// /Encoding open. With no program there is no cmap to settle it, and litho
// reads the font as PDF.js does (evaluator.js, extractDataStructures, which
// cites Table 114): StandardEncoding when the Nonsymbolic flag is set, the
// WinAnsiEncoding default otherwise. Compliance and the font-embedding
// remediation already name such a font's codes through StandardEncoding
// (compliance.UnembeddedFontGlyphNames), so extraction now reads the glyphs
// they judge and embed. An embedded program keeps the
// WinAnsiEncoding default and the cmap-derived reading
// [applyTrueTypeBuiltinEncoding] gives it.
//
// # Corpus evidence
//
//   - pdfjs issue7580.pdf: a non-embedded ArialMT with /Flags 32 and no
//     /Encoding shows code 0xF1 for "æ" and 0xF9 for "ø", the
//     StandardEncoding glyphs ae and oslash; WinAnsiEncoding read them as
//     "ñ" and "ù". With StandardEncoding the page matches the PDF.js golden.
//   - All corpora: applying the same reading to embedded TrueType programs
//     changes no text cell.
func trueTypeTakesStandardEncoding(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, symbolic bool) bool {
	if _, ok := fontDict["Encoding"]; ok || symbolic {
		return false
	}
	if hasEmbeddedFontFile(ctx, doc, fontDict) {
		return false
	}
	fdDict := document.ResolveFontDescriptor(ctx, doc, fontDict)
	if fdDict == nil {
		return false
	}
	flags, ok := fdDict["Flags"].(ast.PdfInt)
	const nonsymbolicBit = 1 << 5 // bit 6 in Adobe's 1-indexed numbering
	return ok && int(flags)&nonsymbolicBit != 0
}

// applyTrueTypeBuiltinEncoding switches font.Encoding to MacRomanEncoding
// when the embedded TrueType program ships a Mac Roman cmap subtable
// (platform 1, encoding 0). ISO 32000-1 §9.6.6.4 says non-symbolic
// TrueType fonts that omit /Encoding inherit their cmap-derived
// encoding. The Mac Roman subtable is treated as authoritative whenever
// it is present, even alongside a Microsoft Unicode subtable, because
// high-byte glyphs are positioned at MacRoman slots (0x96 = ñ, 0xA9 = ©,
// 0xD0 = en-dash). Without this override these bytes surface as WinAnsi
// look-alikes on Mac-authored documents.
//
// A symbolic font whose (1,0) subtable copies its (3,1) subtable keeps the
// encoding it has: [macSubtableCopiesUnicode] says why that subtable is no
// Mac OS Roman mapping, and [applySymbolicTrueTypeGlyphs] reads its codes.
//
// # Corpus evidence
//
//   - govdocs: its Mac-authored filings surface these bytes as WinAnsi
//     look-alikes without this override.
func applyTrueTypeBuiltinEncoding(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, font *Font, symbolic bool) {
	fdDict := document.ResolveFontDescriptor(ctx, doc, fontDict)
	if fdDict == nil {
		return
	}
	fontData := decodeFontStream(ctx, doc, fdDict, "FontFile2")
	if fontData == nil {
		return
	}
	subtables, err := truetype.CmapSubtables(fontData)
	if err != nil || len(subtables) == 0 {
		return
	}
	hasMacRoman := false
	for _, st := range subtables {
		if st.PlatformID == 1 && st.EncodingID == 0 {
			hasMacRoman = true
			break
		}
	}
	if hasMacRoman && (!symbolic || !macSubtableCopiesUnicode(ctx, doc, fontData)) {
		font.Encoding = copyEncoding(encodings.MacRomanEncoding)
	}
}

// applySymbolicTrueTypeGlyphs reads each code of an embedded symbolic
// TrueType font that states no /Encoding and carries a (3,0) cmap subtable as
// the glyph the code selects.
//
// ISO 32000-1, 9.6.6.4 maps such a font's single-byte codes to glyphs through
// the program's (3,0) subtable, under the 0xF000, 0xF100 or 0xF200 range. The
// Microsoft Symbol encoding gives those codes no character, and no base
// encoding is in play, so neither the WinAnsi default nor a built-in encoding
// picked by font name says which character a code shows. The program does:
// its post table names the glyph, and a name the Adobe Glyph List carries is
// the character (9.10.2 reads characters off glyph names). A glyph the post
// table leaves unnamed, or names with an identifier like "glyph00074", reads
// as the code point the program's Unicode subtable maps to it, (3,1), (3,10)
// or platform 0. A code neither table characterises keeps the reading it has.
// So does a code they place in the Private Use Area or among the control
// characters while the reading it has is a graphic character: neither area
// names a character a reader can recover (Unicode 16.0, 23.1 and 23.5).
// Every reading the program gives is also recorded in
// [Font.ProgramGlyphRunes], apart from the default encoding's, so a caller
// that persists a mapping writes the program's reading and nothing else.
//
// A font with only a (1,0) subtable stays out: that subtable's codes are Mac
// OS Roman characters by the subtable's own declaration (OpenType, 'cmap',
// platform 1 encoding 0), which [applyTrueTypeBuiltinEncoding] reads, and a
// post name or Unicode mapping that disagrees contradicts the font rather than
// completing it. A (1,0) subtable that copies the (3,1) subtable does not stay
// out: [macSubtableCopiesUnicode] says why its declaration means nothing, and
// its codes then read as the glyphs they select, as a (3,0) font's do.
//
// This departs from PDF.js, which reads these codes through MacRomanEncoding,
// or WinAnsiEncoding when every name record is a Windows one
// (src/core/evaluator.js, extractDataStructures; src/core/fonts.js,
// adjustTrueTypeToUnicode), and never consults the post table for them.
//
// # Corpus evidence
//
//   - govdocs thread_400_400935.pdf: WPTypographicSymbols names the glyph at
//     code 0x53 "S" in its (1,0) and (3,0) subtables. Read through the Symbol
//     built-in encoding its "Symbol"-bearing name selected, it came out "Σ".
//   - govdocs thread_200_200393.pdf, thread_300_300082.pdf,
//     thread_300_300237.pdf, thread_200_200149.pdf: Symbol subsets whose post
//     tables name codes 0xB7, 0x6D and 0xD4 bullet, mu and trademarkserif, the
//     readings the Symbol encoding gave them. PDF.js reads "∑", "m" and "‘".
//   - pdfjs bug894572.pdf: TSCInaimathi, a legacy Tamil font with (1,0) and
//     (3,1) subtables, names its glyphs as Windows Latin characters. Read
//     through those names, its Mac Roman codes came out as Latin text no
//     reader could use either, and the page no longer matched PDF.js. Its
//     (1,0) subtable maps 24 of its 120 codes in 0x80-0xFF to other glyphs
//     than (3,1), so it is no copy and stays out.
func applySymbolicTrueTypeGlyphs(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, font *Font, isDingbats, isSymbol bool) {
	fdDict := document.ResolveFontDescriptor(ctx, doc, fontDict)
	if fdDict == nil {
		return
	}
	fontData := decodeFontStream(ctx, doc, fdDict, "FontFile2")
	if fontData == nil {
		return
	}
	if !hasSymbolCmapSubtable(fontData) && !macSubtableCopiesUnicode(ctx, doc, fontData) {
		return
	}
	codeToGID := ExtractSymbolCharCodeToGID(ctx, fontData)
	if len(codeToGID) == 0 {
		return
	}
	names := ExtractGlyphNames(ctx, fontData)
	unicodeOf := ExtractGIDToUnicodeAuthoritative(ctx, fontData)
	if font.Encoding == nil {
		font.Encoding = make(map[byte]rune, len(codeToGID))
	}
	for code, gid := range codeToGID {
		if code > 0xFF {
			continue
		}
		r, ok := resolveGlyphRune(names[gid], isDingbats, isSymbol)
		if !ok {
			r, ok = unicodeOf[gid]
		}
		if !ok {
			continue
		}
		c := byte(code)
		if current, has := font.Encoding[c]; has && unreadableRune(r) && !unreadableRune(current) {
			continue
		}
		font.Encoding[c] = r
		if font.ProgramGlyphRunes == nil {
			font.ProgramGlyphRunes = make(map[byte]rune, len(codeToGID))
		}
		font.ProgramGlyphRunes[c] = r
	}
}

// hasSymbolCmapSubtable reports whether a TrueType program declares the (3,0)
// Microsoft Symbol cmap subtable, the one ISO 32000-1, 9.6.6.4 has a symbolic
// font's codes select glyphs through first.
func hasSymbolCmapSubtable(fontData []byte) bool {
	subtables, err := truetype.CmapSubtables(fontData)
	if err != nil {
		return false
	}
	for _, s := range subtables {
		if s.PlatformID == 3 && s.EncodingID == 0 {
			return true
		}
	}
	return false
}

// macDivergentCodesStart is the first single-byte code Mac OS Roman and the
// Windows reading of a code (WinAnsiEncoding, or the code point a (3,1)
// subtable takes the code as) give different characters. Below it both read
// the printable codes as ASCII (ISO 32000-1, Annex D.2), so a (1,0) subtable
// agreeing with (3,1) there says nothing about which encoding it follows.
const macDivergentCodesStart = 0x80

// macSubtableCopiesUnicode reports whether a TrueType program's (1,0)
// Macintosh subtable is a copy of its (3,1) Microsoft Unicode subtable: it maps
// at least one code from 0x80 to 0xFF to a glyph, and maps every such code to
// the glyph (3,1) maps the code point of the same value to.
//
// ISO 32000-1, 9.6.6.4 has a symbolic font with no (3,0) subtable select
// glyphs through (1,0), whose codes are Mac OS Roman characters by the
// subtable's own declaration. A copy relaxes that reading: a producer that
// writes the (3,1) mapping under the (1,0) record declares a Mac table without
// building one, so the declaration carries no meaning the (3,1) subtable lacks.
// The glyph a code selects is the same through either table. Only its
// character changes: Mac OS Roman would name code 0xE4 "‰" where the glyph the
// font draws there is the one it maps U+00E4 "ä" to.
//
// The test runs over the codes the subtable covers in 0x80-0xFF, not over the
// codes a page shows: a font is read before any content stream that uses it,
// and below [macDivergentCodesStart] the two readings agree. A single code
// mapped to a different glyph makes the subtable a Mac mapping of its own.
//
// # Corpus evidence
//
//   - pdfjs issue15910.pdf: an Amyuni Document Converter subset of Arial maps
//     all 128 codes in 0x80-0xFF through (1,0) to the glyphs (3,1) maps them
//     to. Read as Mac OS Roman, "Käferhofen" came out "K‰ferhofen"; PDF.js
//     reads "ä".
//   - All corpora: 7 embedded symbolic TrueType fonts in 4 files state no
//     /Encoding and carry (1,0) and (3,1) subtables without (3,0).
//     issue15910's is the only copy, and text extraction changes on no other
//     file.
func macSubtableCopiesUnicode(ctx context.Context, doc *document.Document, fontData []byte) bool {
	mac, err := truetype.SubtableCodeToGID(ctx, fontData, truetype.CmapSubtable{PlatformID: 1, EncodingID: 0})
	if err != nil {
		if !errors.Is(err, truetype.ErrTableNotFound) {
			doc.Logger.Debug("reading the (1,0) cmap subtable", slog.Any("err", err))
		}
		return false
	}
	uni, err := truetype.SubtableCodeToGID(ctx, fontData, truetype.CmapSubtable{PlatformID: 3, EncodingID: 1})
	if err != nil {
		if !errors.Is(err, truetype.ErrTableNotFound) {
			doc.Logger.Debug("reading the (3,1) cmap subtable", slog.Any("err", err))
		}
		return false
	}
	compared := 0
	for code := uint32(macDivergentCodesStart); code <= 0xFF; code++ {
		gid := mac[code]
		if gid == 0 {
			continue
		}
		if uni[code] != gid {
			return false
		}
		compared++
	}
	return compared > 0
}

// unreadableRune reports whether r is a control character or a Private Use
// Area code point: neither names a character a reader can recover (Unicode
// 16.0, 23.1 and 23.5).
func unreadableRune(r rune) bool {
	return r < 0x20 || (r >= 0x7F && r <= 0x9F) || (r >= 0xE000 && r <= 0xF8FF)
}

// extractCFFEncoding reads the CFF encoding from a /FontFile3 (Type1C) font
// and populates font.Encoding. This is needed for TeX fonts (CMR, CMSY, etc.)
// that use OT1 encoding internally. The byte→glyph mapping differs from
// StandardEncoding/WinAnsiEncoding.
func extractCFFEncoding(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, font *Font) {
	fdDict := document.ResolveFontDescriptor(ctx, doc, fontDict)
	if fdDict == nil {
		return
	}
	fontData := decodeFontStream(ctx, doc, fdDict, "FontFile3")
	if fontData == nil {
		return
	}
	encMap := cff.ExtractEncoding(fontData)
	if len(encMap) == 0 {
		return
	}
	// Convert glyph names to runes and overwrite the encoding table.
	for code, name := range encMap {
		if builtinNumericNameMisreads(name, int(code)) {
			continue
		}
		if r, ok := encodings.GlyphToRuneFull(name); ok {
			font.Encoding[code] = r
		}
	}
}

// extractCFFEncodingAsBase fills holes in font.Encoding from a /FontFile3
// (Type1C/CFF) built-in encoding without overwriting entries that
// /Differences will set. Used when a dict /Encoding has no /BaseEncoding:
// the font program's own mapping is a better base than WinAnsi.
func extractCFFEncodingAsBase(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, font *Font) {
	fdDict := document.ResolveFontDescriptor(ctx, doc, fontDict)
	if fdDict == nil {
		return
	}
	fontData := decodeFontStream(ctx, doc, fdDict, "FontFile3")
	if fontData == nil {
		return
	}
	encMap := cff.ExtractEncoding(fontData)
	if len(encMap) == 0 {
		return
	}
	if font.Encoding == nil {
		font.Encoding = make(map[byte]rune)
	}
	for code, name := range encMap {
		if _, set := font.Encoding[code]; set || builtinNumericNameMisreads(name, int(code)) {
			continue
		}
		if r, ok := encodings.GlyphToRuneFull(name); ok {
			font.Encoding[code] = r
		}
	}
}

// encodingCMapName returns the predefined CMap name from a Type0 font's
// /Encoding entry, or empty string if the encoding is not a recognized name.
func encodingCMapName(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) string {
	encObj, ok := fontDict["Encoding"]
	if !ok {
		return ""
	}
	encName, err := document.ResolveName(ctx, doc, encObj)
	if err != nil || encName == "" {
		return ""
	}
	return string(encName)
}

// isIdentityEncoding checks whether a Type0 font uses Identity-H or Identity-V
// encoding, which means character codes are direct CIDs.
func isIdentityEncoding(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) bool {
	encObj, ok := fontDict["Encoding"]
	if !ok {
		return false
	}
	encName, err := document.ResolveName(ctx, doc, encObj)
	if err != nil {
		return false
	}
	return encName == "Identity-H" || encName == "Identity-V"
}

// decodeFontStream resolves and decodes an embedded font stream by key name.
func decodeFontStream(ctx context.Context, doc *document.Document, fdDict ast.PdfDict, key ast.PdfName) []byte {
	obj, ok := fdDict[key]
	if !ok {
		return nil
	}
	stream, err := document.ResolveStream(ctx, doc, obj)
	if err != nil {
		return nil
	}
	if stream.Dict == nil {
		return nil
	}
	data, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		return nil
	}
	return data
}

// injectStandard14Widths populates font.Widths from the Standard 14 Adobe
// Font Metrics if the base font name matches. It iterates over char codes
// 0-255, uses the font's encoding to map each code to a glyph name, and
// looks up that glyph's width in the canonical AFM table. A name no standard
// font answers to takes Times-Roman's metrics when serif is set, and
// Helvetica's otherwise ([substituteIsSerif]).
func injectStandard14Widths(font *Font, baseFont string, serif bool) {
	name := normalizeBase14Name(baseFont)
	if !isStandard14(name) {
		// Last resort: use Helvetica (sans-serif) or Times-Roman (serif)
		// metrics as a best-effort fallback for width-based space inference.
		if serif {
			name = "Times-Roman"
		} else {
			name = "Helvetica"
		}
	}

	widths := make([]float64, 256)
	if isCourier(name) {
		for i := range widths {
			widths[i] = courierWidth
		}
	} else {
		glyphWidths := standard14GlyphWidths[name]
		if glyphWidths == nil {
			return
		}
		builtin, builtinUnicode, builtinByRune := builtinGlyphNames(name)
		for code := range 256 {
			r, ok := font.Encoding[byte(code)] //nolint:gosec // code is 0-255
			if !ok {
				continue
			}
			glyphName := encodings.RuneToGlyph(r)
			if glyphName == "" {
				continue
			}
			if w, ok := glyphWidths[glyphName]; ok {
				widths[code] = float64(w)
				continue
			}
			// Symbol and ZapfDingbats name their glyphs outside the Adobe
			// Glyph List ("phi", "minute", "a1"), so the name the code's
			// character maps back to misses their metrics. The font's own
			// glyph names hold them (ISO 32000-1, Annex D.5, D.6): the
			// built-in encoding's name for the code when the code still
			// shows that glyph, else the glyph that carries the character,
			// as a /Differences entry naming it gives.
			if builtin == nil {
				continue
			}
			n := builtin[code]
			if n == "" || builtinUnicode[n] != r {
				n = builtinByRune[r]
			}
			if w, ok := glyphWidths[n]; ok {
				widths[code] = float64(w)
			}
		}
	}

	font.FirstChar = 0
	font.LastChar = 255
	font.Widths = widths
	font.widthStated = nil
}

// substituteIsSerif reports whether a simple font with no /Widths, named
// baseFont, takes Times-Roman's metrics rather than Helvetica's. The widths
// are a guess either way: ISO 32000-1, 9.6.2.1, Table 111 requires /Widths
// of every simple font but the standard 14. litho guesses as PDF.js does, so
// the spaces it infers from the widths are the ones PDF.js infers
// (evaluator.js, extractWidths and translateFont through getBaseFontMetrics
// and isSerifFont). PDF.js reads the name alone, never the Serif flag of
// /Flags (9.8.2, Table 123), which it consults only to pick a rendering
// fallback, and it counts "SansSerif" as serif, so litho does too. It reads
// the /BaseFont as written when the font has a font descriptor (described),
// and in its normalised form when it has none. See [stdfonts.IsSerifFont].
//
// # Corpus evidence
//
//   - govdocs, pdfjs, pdfcpu, facturx, pdf20: one font reaches this choice
//     with a name no standard font answers to (pdfjs issue2391-2.pdf,
//     Monaco, no font descriptor). It is sans under the former substring
//     list and under PDF.js's rule, so no extracted text changes.
func substituteIsSerif(baseFont string, described bool) bool {
	if !described {
		baseFont = stdfonts.NormalizeFontName(baseFont)
	}
	return stdfonts.IsSerifFont(baseFont)
}

// symbolByRune and dingbatsByRune invert the glyph-name to Unicode maps of
// Symbol and ZapfDingbats. Where two names carry one character, the first
// in sorted order wins, so the lookup is deterministic.
var (
	symbolByRune   = sync.OnceValue(func() map[rune]string { return invertGlyphUnicode(encodings.SymbolGlyphsUnicode) })
	dingbatsByRune = sync.OnceValue(func() map[rune]string { return invertGlyphUnicode(encodings.DingbatsGlyphsUnicode) })
)

func invertGlyphUnicode(m map[string]rune) map[rune]string {
	names := slices.Sorted(maps.Keys(m))
	out := make(map[rune]string, len(names))
	for _, n := range names {
		if _, ok := out[m[n]]; !ok {
			out[m[n]] = n
		}
	}
	return out
}

// builtinGlyphNames returns, for the two Standard 14 fonts whose glyph
// names the Adobe Glyph List does not carry, Symbol and ZapfDingbats
// (ISO 32000-1, Annex D.5 and D.6): the built-in encoding (code to glyph
// name), the Unicode value of each glyph, and the glyph name of each
// Unicode value. Other fonts return nil.
//
// # Corpus evidence
//
//   - govdocs thread_400_400739.pdf: Symbol's 0xA2 (minute, 247 units)
//     read as 1000 units, so the space after the two primes of 35′′ fell
//     11 points before the next word started.
//   - govdocs thread_400_400392.pdf: a Symbol font whose /Differences name
//     phi and theta read both as 1000 units wide.
//   - govdocs: with both lookups, 5 more files extract the words PDF.js
//     does, none fewer, and 15 files come closer to it, none further.
func builtinGlyphNames(name string) (*[256]string, map[string]rune, map[rune]string) {
	switch name {
	case "Symbol":
		return &encodings.SymbolSetEncoding, encodings.SymbolGlyphsUnicode, symbolByRune()
	case "ZapfDingbats":
		return &encodings.ZapfDingbatsEncoding, encodings.DingbatsGlyphsUnicode, dingbatsByRune()
	}
	return nil, nil, nil
}

// extractType3Glyph parses a Type 3 CharProc stream and returns the glyph
// width from the d0 or d1 operator.
//
// d0 (setcharwidth): wx wy d0 sets the glyph width.
// d1 (setcachedevice): wx wy llx lly urx ury d1 sets width and cache bbox.
//
// The first operand (wx) is the horizontal advance width in glyph space units.
//
// It also returns the bounds of the paths a glyph description that begins
// with d0 constructs, in glyph space, or nil for one that begins otherwise
// or constructs none. PDF.js reads those bounds as the font's bounding box
// when the font states an empty /FontBBox (src/core/evaluator.js,
// TranslatedFont, #guessType3FontBBox): see [Font.type3HeightScale].
func extractType3Glyph(ctx context.Context, doc *document.Document, stream ast.PdfStream) (float64, *[4]float64, error) {
	data, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		return 0, nil, err
	}

	ops, err := content.ParseStreamData(ctx, data, content.SplitGluedOperators())
	if err != nil {
		return 0, nil, err
	}

	for i, op := range ops {
		switch op.Operator {
		case "d0":
			if len(op.Operands) >= 1 {
				var bbox *[4]float64
				if i == 0 {
					bbox = type3PathBounds(ops[1:])
				}
				return document.Float(op.Operands[0]), bbox, nil
			}
		case "d1":
			if len(op.Operands) >= 1 {
				return document.Float(op.Operands[0]), nil, nil
			}
		}
	}

	return 0, nil, fmt.Errorf("no d0/d1 operator found in CharProc stream")
}

// type3PathBounds returns the bounds of the points the path construction
// operators of ops pass through, in the coordinates they are given in, or
// nil when ops construct no path. A Bezier segment counts by its own
// extent, not by its control points, as PDF.js bounds it
// (src/core/evaluator.js, buildPath, Util.bezierBoundingBox).
func type3PathBounds(ops []content.Operation) *[4]float64 {
	bounds := [4]float64{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	found := false
	add := func(x, y float64) {
		bounds[0] = min(bounds[0], x)
		bounds[1] = min(bounds[1], y)
		bounds[2] = max(bounds[2], x)
		bounds[3] = max(bounds[3], y)
		found = true
	}
	var cx, cy float64
	for _, op := range ops {
		v, ok := pathOperands(op)
		if !ok {
			continue
		}
		switch op.Operator {
		case "m", "l":
			cx, cy = v[0], v[1]
			add(cx, cy)
		case "re":
			cx, cy = v[0], v[1]
			add(v[0], v[1])
			add(v[0]+v[2], v[1]+v[3])
		case "c":
			bezierBounds([4][2]float64{{cx, cy}, {v[0], v[1]}, {v[2], v[3]}, {v[4], v[5]}}, add)
			cx, cy = v[4], v[5]
		case "v":
			bezierBounds([4][2]float64{{cx, cy}, {cx, cy}, {v[0], v[1]}, {v[2], v[3]}}, add)
			cx, cy = v[2], v[3]
		case "y":
			bezierBounds([4][2]float64{{cx, cy}, {v[0], v[1]}, {v[2], v[3]}, {v[2], v[3]}}, add)
			cx, cy = v[2], v[3]
		}
	}
	if !found {
		return nil
	}
	return &bounds
}

// pathOperandCounts is how many numbers each path construction operator
// takes (ISO 32000-1, 8.5.2.1, Table 59).
//
//nolint:gochecknoglobals // static lookup table mirroring ISO 32000-1 Table 59
var pathOperandCounts = map[string]int{"m": 2, "l": 2, "re": 4, "c": 6, "v": 4, "y": 4}

// pathOperands returns the operands of op, a path construction operator,
// as numbers, or false when op is not one or carries too few.
func pathOperands(op content.Operation) ([]float64, bool) {
	n, ok := pathOperandCounts[op.Operator]
	if !ok || len(op.Operands) < n {
		return nil, false
	}
	v := make([]float64, n)
	for k := range n {
		v[k] = document.Float(op.Operands[k])
	}
	return v, true
}

// bezierBounds passes add the end points of the cubic Bezier segment p,
// from p[0] through the control points p[1] and p[2] to p[3], and every
// point where it turns along either axis.
func bezierBounds(p [4][2]float64, add func(x, y float64)) {
	add(p[0][0], p[0][1])
	add(p[3][0], p[3][1])
	at := func(t float64) {
		u := 1 - t
		a, b, c, d := u*u*u, 3*u*u*t, 3*u*t*t, t*t*t
		add(a*p[0][0]+b*p[1][0]+c*p[2][0]+d*p[3][0], a*p[0][1]+b*p[1][1]+c*p[2][1]+d*p[3][1])
	}
	for axis := range 2 {
		p0, p1, p2, p3 := p[0][axis], p[1][axis], p[2][axis], p[3][axis]
		// The derivative of the segment along one axis is the quadratic
		// a t^2 + b t + c, whose roots in (0, 1) are where it turns.
		a := 3 * (-p0 + 3*p1 - 3*p2 + p3)
		b := 6 * (p0 - 2*p1 + p2)
		c := 3 * (p1 - p0)
		if a == 0 {
			if b != 0 {
				if t := -c / b; t > 0 && t < 1 {
					at(t)
				}
			}
			continue
		}
		disc := b*b - 4*a*c
		if disc < 0 {
			continue
		}
		sq := math.Sqrt(disc)
		for _, t := range [2]float64{(-b + sq) / (2 * a), (-b - sq) / (2 * a)} {
			if t > 0 && t < 1 {
				at(t)
			}
		}
	}
}

// resolveGlyphRune maps a glyph name to a Unicode rune. For Dingbats and
// Symbol fonts, their specialised tables are checked first. For all fonts,
// the standard AGL is checked, followed by the Symbol and Dingbats tables
// as a last resort (some non-symbol fonts use glyph names like "asteriskmath"
// from the Symbol encoding in their /Differences arrays).
func resolveGlyphRune(name string, isDingbats, isSymbol bool) (rune, bool) {
	if isDingbats {
		if r, ok := encodings.DingbatsGlyphsUnicode[name]; ok {
			return r, true
		}
	}
	if isSymbol {
		if r, ok := encodings.SymbolGlyphsUnicode[name]; ok {
			return r, true
		}
	}
	if r, ok := encodings.GlyphToRuneFull(name); ok {
		// The extended AGL includes ZapfDingbats "aNN" glyph names (e.g.,
		// a45→✲, a100→❞) that map to symbol ranges. For non-Dingbats fonts,
		// these are false matches from font-internal identifiers.
		if !isDingbats && isDingbatsGlyphName(name) {
			return 0, false
		}
		return r, ok
	}
	// Last resort: check the Symbol table for math/Greek glyph names.
	// These names (asteriskmath, theta, sigma, etc.) appear in non-Symbol
	// fonts via /Differences arrays (e.g., TeX CMSY math fonts).
	// They are NOT in GlyphToRune to avoid affecting compliance checks
	// (which use GlyphToRuneFull), but are useful for text extraction.
	if !isSymbol {
		if r, ok := encodings.SymbolGlyphsUnicode[name]; ok {
			return r, true
		}
	}
	// Stylistic-variant glyph names ("A.sc", "zero.oldstyle", "a.smcp")
	// split off a dot-suffix and fall back to the base name. Small-caps
	// and old-style /Differences entries extract as their underlying
	// character.
	if dot := strings.IndexByte(name, '.'); dot > 0 {
		base := name[:dot]
		if r, ok := encodings.GlyphToRuneFull(base); ok {
			if !isDingbats && isDingbatsGlyphName(base) {
				return 0, false
			}
			return r, true
		}
		if !isSymbol {
			if r, ok := encodings.SymbolGlyphsUnicode[base]; ok {
				return r, true
			}
		}
	}
	return 0, false
}

// decomposeGlyphName splits a composite Adobe glyph name of the form
// "part1_part2[_partN]" and returns the concatenated Unicode runes of
// each component when every part resolves. Returns ("", false) when
// the input is not a composite or any part is unresolvable. The
// decomposition lets /Differences entries that declare Type1 ligature
// glyph names (e.g. "f_t", "T_h", "c_h") surface as their constituent
// characters during text extraction.
func decomposeGlyphName(name string, isDingbats, isSymbol bool) (string, bool) {
	if !strings.Contains(name, "_") {
		return "", false
	}
	parts := strings.Split(name, "_")
	if len(parts) < 2 {
		return "", false
	}
	var b strings.Builder
	b.Grow(len(parts))
	for _, p := range parts {
		if p == "" {
			return "", false
		}
		r, ok := resolveGlyphRune(p, isDingbats, isSymbol)
		if !ok {
			return "", false
		}
		b.WriteRune(r)
	}
	return b.String(), true
}

// isDingbatsGlyphName returns true if the name matches the ZapfDingbats
// "aNN"/"aNNN" naming convention (letter 'a' followed by 1-3 digits).
func isDingbatsGlyphName(name string) bool {
	if len(name) < 2 || len(name) > 4 || name[0] != 'a' {
		return false
	}
	for _, c := range name[1:] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
