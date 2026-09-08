package typography

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"maps"
	"math"
	"regexp"
	"strconv"
	"strings"
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
type Font struct {
	Subtype            string
	Encoding           map[byte]rune
	ToUnicode          *cmap.CMap
	FirstChar          int                // Simple fonts: first defined char code
	LastChar           int                // Simple fonts: last defined char code
	Widths             []float64          // Simple fonts: Widths[charCode-FirstChar] in 1/1000 text-space units
	MissingWidth       float64            // Fallback width for codes outside the defined range
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

	CIDIdentityFallback bool // True when CIDToGIDMap is /Identity and no ToUnicode/embedded font
	CIDOffset           int  // Offset to add to CID values before treating as Unicode (detected from /W array)

	// SuppressExtraction marks a font whose text cannot be decoded
	// without the system font that would render it (reference-only
	// CJK Type0 with a predefined CMap /Encoding and no /FontFile).
	// [decodeNextChar] consumes the CID width and emits no element.
	SuppressExtraction bool

	// LigatureOverrides maps a byte code to a pre-decoded multi-rune
	// string for /Differences entries that declare composite glyph
	// names like "f_t" or "T_h". The regular [Font.Encoding] map can
	// only hold a single rune per byte, so glyph decoding consults this
	// map first and returns the stored string verbatim when present.
	LigatureOverrides map[byte]string
}

// GlyphWidth returns the advance width (in 1/1000 text-space units) for the
// given char code using a strict 5-step fallback chain:
//
//   - PDF width arrays: /Widths (simple) or /W (CID). Type 3 uses /CharProcs.
//   - CID default width: /DW from the CIDFont (defaults to 1000 per spec).
//   - Simple font /MissingWidth from the /FontDescriptor.
//   - TrueType binary hmtx: read advance width from the embedded font program.
//   - /FontBBox average, then absolute fallback of 1000 (= 1 em).
func (f *Font) GlyphWidth(charCode int) float64 {
	// Type 3 fonts: look up glyph name via encoding, then apply FontMatrix.
	if f.Type3Widths != nil && charCode >= 0 && charCode <= 255 {
		if r, ok := f.Encoding[byte(charCode)]; ok { //nolint:gosec // charCode bounds-checked above
			glyphName := encodings.RuneToGlyph(r)
			if w, ok := f.Type3Widths[glyphName]; ok && w > 0 {
				return w * f.FontMatrix[0] * 1000
			}
		}
		idx := charCode - f.FirstChar
		if idx >= 0 && idx < len(f.Widths) && f.Widths[idx] > 0 {
			return f.Widths[idx]
		}
		if f.MissingWidth > 0 {
			return f.MissingWidth
		}
		return f.resolveWidth(charCode)
	}

	// CIDFont (Type0): check /W array.
	if f.CIDWidths != nil {
		if w, ok := f.CIDWidths[charCode]; ok && w > 0 {
			return w
		}
		// Try hmtx before DW: many CIDFonts have /DW=1000 (spec default) but the
		// embedded font program has the real per-glyph widths. Using the binary
		// hmtx widths produces much better spatial reconstruction.
		if f.HmtxWidths != nil && f.UnitsPerEm > 0 {
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
	idx := charCode - f.FirstChar
	if idx >= 0 && idx < len(f.Widths) && f.Widths[idx] > 0 {
		return f.Widths[idx]
	}

	if f.MissingWidth > 0 {
		return f.MissingWidth
	}

	return f.resolveWidth(charCode)
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
			for cid := startCID; cid <= endCID; cid++ {
				metrics[cid] = [3]float64{w1, vx, vy}
			}
		default:
			i++
		}
	}
	return metrics
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
			for cid := startCID; cid <= endCID; cid++ {
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

	// Detect Symbol, ZapfDingbats, and the other 14 standard Type 1
	// fonts by BaseFont name to select the correct default encoding.
	// Per ISO 32000-1 §9.6.6.1 a non-symbolic Type 1 font that omits
	// /Encoding inherits StandardEncoding; Symbol and ZapfDingbats use
	// their own built-in encoding; the standard 14 base fonts already
	// fall in the StandardEncoding bucket. Custom Type 1 fonts (e.g.
	// /Melior, /Minion-Pro) hit the same StandardEncoding default.
	isSymbolFont := false
	isDingbatsFont := false
	if bfObj, ok := fontDict["BaseFont"]; ok {
		bfName, err := document.ResolveName(ctx, doc, bfObj)
		if err != nil {
			doc.Logger.Debug("resolving font BaseFont name", slog.Any("err", err))
		}
		if bfName != "" {
			name := strings.ToLower(string(bfName))
			isSymbolFont = strings.Contains(name, "symbol")
			isDingbatsFont = strings.Contains(name, "dingbat") || strings.Contains(name, "zapfdingbats")
		}
	}

	// FontDescriptor /Flags bit 3 (Symbolic) marks fonts whose glyph
	// repertoire is bespoke. Custom glyph names (H11537, etc.) are
	// laid out at WinAnsi-style byte positions for PostScript-printed
	// docs (Universal-NewswithCommPi, OCR symbols, math notation), so
	// those bytes need the WinAnsi fallback. Pulling the flag before
	// defaultEncoding selection lets us route Type 1 symbolic fonts
	// past the StandardEncoding default below.
	symbolicByFlags := fontIsSymbolicByFlags(ctx, doc, fontDict)

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
	// Roman subtable (older Mac-authored documents, common in govdocs
	// scanned filings) put accented glyphs at MacRoman byte positions:
	// byte 0xD0 is en-dash, 0x96 is ñ, 0xA9 is ©. Switching to
	// MacRomanEncoding on the cmap signal surfaces those bytes as
	// real characters instead of WinAnsi look-alikes.
	if !hasExplicitEncoding && subtypeName == "TrueType" && !isSymbolFont && !isDingbatsFont {
		applyTrueTypeBuiltinEncoding(ctx, doc, fontDict, font)
	}

	// Extract /FontBBox and embedded font metrics from the FontDescriptor.
	extractFontDescriptorMetrics(ctx, doc, fontDict, font)

	// Predefined CJK encoding CMap fallback for Type0 fonts: when the
	// /Encoding is a predefined CMap name (e.g., GBK-EUC-H, EUC-H), load
	// the byte→CID mapping from the embedded bcmap and compose with the
	// Adobe CID→Unicode table to synthesize a ToUnicode CMap.
	if subtypeName == "Type0" && font.ToUnicode == nil && !font.CIDIdentityFallback {
		if encName := encodingCMapName(ctx, doc, fontDict); encName != "" {
			// Gated on embedded font: a reference-only Type0 font uses
			// the host's system font to render glyphs, so the byte
			// stream carries no information litho can decode without
			// that font. Synthesising text from bundled predefined
			// CMaps produces characters that do not match the glyphs
			// actually drawn (noembed-eucjp, noembed-sjis).
			if hasEmbeddedFontFile(ctx, doc, fontDict) {
				if cm := cmap.LoadPredefinedCMap(encName); cm != nil {
					font.ToUnicode = cm
				}
			} else if encName != "Identity-H" && encName != "Identity-V" {
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
	// PDF-level /Widths array left at zero or did not cover. TeX-subset
	// Type 1 fonts commonly emit /FirstChar, /LastChar, and a /Widths
	// array that only spans a narrow range, yet use byte codes outside
	// that range for math/symbol glyphs. Reading the widths straight
	// from the embedded font program gives the space-insertion
	// heuristic in [ReconstructText] accurate per-glyph spans.
	if subtypeName == "Type1" || subtypeName == "MMType1" {
		applyType1CharStringWidths(ctx, doc, fontDict, font)
	}

	// Adobe predefined CID→Unicode fallback for Type0 fonts. Gated on
	// an embedded font program. Reference-only CJK fonts carry no
	// glyph data, so synthesising through Adobe-Japan1/GB1/CNS1/Korea1
	// emits characters that do not match what the host's system font
	// would draw (noembed-eucjp, noembed-sjis, noembed-jis7,
	// noembed-identity).
	if subtypeName == "Type0" && font.ToUnicode == nil && font.GIDToUnicode == nil && !font.CIDIdentityFallback {
		if hasEmbeddedFontFile(ctx, doc, fontDict) {
			applyAdobeCIDFallback(ctx, doc, fontDict, font)
		}
	}

	// Identity-H/V fallback: last resort for Type0 fonts with no other
	// mapping. Treats 2-byte character codes as direct Unicode codepoints.
	// Reference-only CJK fonts (Adobe-Japan1/GB1/CNS1/Korea1 with no
	// /FontFile*) would decode to garbage through the +29 heuristic;
	// suppress extraction instead.
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
	// Chinese text extracts; reference-only fonts (no embedded program)
	// cannot be extracted without a host CJK font, so clearing Encoding
	// avoids emitting WinAnsi-decoded garbage or synthesised Chinese that
	// does not match the rendered glyph (SimFang-variant, issue20489
	// "Fcpdf0").
	if font.ToUnicode == nil && (subtypeName == "TrueType" || subtypeName == "Type1") {
		if bfObj, ok := fontDict["BaseFont"]; ok {
			bfName, _ := document.ResolveName(ctx, doc, bfObj)
			if hasCJKFontName(string(bfName)) {
				if hasEmbeddedFontFile(ctx, doc, fontDict) {
					if cm := cmap.LoadPredefinedCMap("GBK-EUC-H"); cm != nil {
						font.ToUnicode = cm
					}
				} else {
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
	if c == nil || len(c.Mapping) == 0 {
		return nil
	}
	return c
}

// resolveEncoding resolves the /Encoding entry from the font dictionary into
// the font's encoding map. Handles both named encodings (WinAnsiEncoding, etc.)
// and dictionary-based encodings with /BaseEncoding and /Differences.
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
			font.Encoding = copyEncoding(encodings.WinAnsiEncoding)
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
					baseMap = encodings.WinAnsiEncoding
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
			// on any unfilled slot.
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
			case isType1Like:
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
							// lives in [Font.LigatureOverrides] and beats
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

// resolveType3Font extracts Type 3 font metrics: /FontMatrix, /CharProcs
// glyph widths from d0/d1 operators, and /FirstChar, /LastChar, /Widths
// as a fallback.
func resolveType3Font(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, font *Font) {
	// /FontMatrix maps glyph space → text space. Default is [0.001 0 0 0.001 0 0].
	font.FontMatrix = [6]float64{0.001, 0, 0, 0.001, 0, 0}
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

	// /CharProcs maps glyph names → streams containing d0/d1 width operators.
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
				w, err := extractType3GlyphWidth(ctx, doc, stream)
				if err != nil {
					continue
				}
				font.Type3Widths[string(name)] = w
			}
		}
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
				}
				// /W encodes per-CID or range widths.
				if wObj, ok := cidDict["W"]; ok {
					wArr, err := document.ResolveArray(ctx, doc, wObj)
					if err != nil {
						doc.Logger.Debug("resolving CIDFont /W array", slog.Any("err", err))
					}
					if wArr != nil {
						font.CIDWidths = parseCIDWidths(wArr)
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
						font.CIDVerticalMetrics = parseCIDVerticalWidths(w2Arr)
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
			for i, item := range wArr {
				font.Widths[i] = document.Float(item)
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
				injectStandard14Widths(font, string(bfName))
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
	// always done; the fractional part [ExtractBinaryWidths] now preserves
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

// ExtractEmptyGIDs returns the set of GIDs with empty glyph data in a
// TrueType font (where loca[gid] == loca[gid+1]). Returns nil for non-TrueType.
func ExtractEmptyGIDs(ctx context.Context, fontData []byte) map[uint16]bool {
	if len(fontData) < 4 {
		return nil
	}
	sig := fontData[:4]
	if (sig[0] == 0 && sig[1] == 1 && sig[2] == 0 && sig[3] == 0) ||
		string(sig) == "true" {
		m, _ := truetype.EmptyGIDs(ctx, fontData)
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
	return type1.ParseEncoding(fontData)
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
		if name == "" || name == ".notdef" {
			continue
		}
		if r, ok := resolveGlyphRune(name, isDingbats, isSymbol); ok {
			font.Encoding[byte(i)] = r //nolint:gosec // i is bounded by 0..255 via the array size
		}
	}
}

// applyType1CharStringWidths fills gaps in font.Widths from the
// advance widths stored in each glyph's charstring. The byte→glyph
// name mapping comes from the same Type 1 encoding vector the
// encoding-fill step uses, so the lookup does not rely on a lossy
// Unicode round-trip that would miss symbol/dingbats names outside
// the main Adobe Glyph List. Existing non-zero /Widths entries from
// the PDF dictionary stay authoritative; only zeros get filled.
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
		if font.Widths[idx] > 0 {
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
		font.Encoding = copyEncoding(encodings.WinAnsiEncoding)
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
		}
		return
	}

	// /CIDToGIDMap stream: 2-byte big-endian GIDs indexed by CID.
	// Parse the stream and combine with the embedded font's GID→Unicode
	// cmap to build a CID→Unicode mapping.
	applyCIDToGIDStream(ctx, doc, fontDict, font, mapObj)
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
// embedded font's GID→Unicode cmap to build a synthetic ToUnicode CMap.
func applyCIDToGIDStream(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, font *Font, mapObj ast.PdfObject) {
	stream, err := document.ResolveStream(ctx, doc, mapObj)
	if err != nil || stream.Dict == nil {
		return
	}
	mapData, err := doc.DecodeStream(ctx, stream)
	if err != nil || len(mapData) < 2 {
		return
	}

	// Extract GID→Unicode from the embedded font.
	gidToUni := ExtractGIDToUnicode(ctx, decodeFontStreamFromDescriptor(ctx, doc, fontDict))
	if len(gidToUni) == 0 {
		return
	}

	// Build CID→Unicode: CIDToGIDMap[CID] → GID, gidToUni[GID] → Unicode.
	mapping := make(map[string]string, 4096) //nolint:mnd // Capacity hint sized for a large CMap, not a limit; the map grows freely.
	numCIDs := len(mapData) / 2
	for cid := range numCIDs {
		gid := uint16(mapData[cid*2])<<8 | uint16(mapData[cid*2+1])
		if gid == 0 {
			continue
		}
		uni, ok := gidToUni[gid]
		if !ok || uni == 0 {
			continue
		}
		key := string([]byte{byte(cid >> 8), byte(cid)}) //nolint:gosec // splits a uint16 CID into its two bytes; truncation is the intent
		mapping[key] = string(uni)
	}

	if len(mapping) == 0 {
		return
	}

	font.ToUnicode = &cmap.CMap{
		Mapping: mapping,
		CodeSpaceRanges: []cmap.CodeSpaceRange{
			{Low: []byte{0x00, 0x00}, High: []byte{0xFF, 0xFF}},
		},
	}
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
// Korea1 / GB-Chinese, KR). Used as a signal that a non-embedded font
// would need the host's system CJK font to render: without one, any
// fallback would emit host-dependent text.
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

// applyCIDIdentityHeuristics fills in what the identity CID fallback needs to
// read text: the CID-to-Unicode offset, and a partial GID table for the
// standard faces a PDF may reference without embedding.
//
// TODO: [applyCIDToGIDIdentityFallback] turns [Font.CIDIdentityFallback] on for
// an explicit /CIDToGIDMap /Identity without calling this, so that path reads a
// CID as a code point with no offset. An absent /CIDToGIDMap already means
// Identity (ISO 32000-1, 9.7.4.2), so writing the key changes extracted text:
// "Dotless Forms" becomes "'RWOHVV )RUPV" (ThuluthFeatures.pdf), and a PDF/A
// fixer writes that key. Calling this from there fixes the shift but moves two
// pdfjs text goldens (issue11242_reduced, issue15719), whose expectations were
// captured against the short-circuit, so the offset heuristics need settling
// first: the +29 guess for a non-embedded font overrides /W evidence that the
// font already states (Tahoma declares CIDs 1-3).
func applyCIDIdentityHeuristics(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, font *Font) {
	// Skipped when the CIDFont has an explicit /CIDToGIDMap stream: the
	// stream already remaps CIDs to subset GIDs, so the CID values carry
	// Unicode semantics directly and adding +29 would shift the text
	// (e.g. "6XPPD" becomes "Summa").
	if hasCIDToGIDMapStream(ctx, doc, fontDict) {
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
	// Overlay a partial GID→Unicode table for the handful of Windows
	// standard fonts (Arial, Helvetica) that PDFs reference by name without
	// embedding. The CID + 29 heuristic covers ASCII; extended-Latin
	// positions (e.g. GID 207 = Ó in Arial, GID 179-182 = smart quotes) do
	// not follow a linear offset and need an explicit lookup.
	// [stdfonts.CMap] provides the curated subset.
	if cidFontHasEmbeddedProgram(ctx, doc, fontDict) {
		return
	}
	bf, ok := fontDict["BaseFont"]
	if !ok {
		return
	}
	name, _ := document.ResolveName(ctx, doc, bf)
	m := stdfonts.CMap(string(name))
	if m == nil {
		return
	}
	if font.GIDToUnicode == nil {
		font.GIDToUnicode = make(map[uint16]rune, len(m))
	}
	maps.Copy(font.GIDToUnicode, m)
}

// detectCIDOffset infers the CID-to-Unicode offset from the /W (widths) map.
// Non-embedded TrueType fonts commonly use GIDs starting at 29 for space
// (U+0020), meaning CID + 29 = Unicode. Detection requires:
//   - CID 3 has a space-like width (200-350 units)
//   - CIDs 0-2 are absent from the widths map
//   - The lowest CID is 3 (no CIDs below 3 have widths)
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

// applyTrueTypeBuiltinEncoding switches font.Encoding to MacRomanEncoding
// when the embedded TrueType program ships a Mac Roman cmap subtable
// (platform 1, encoding 0). ISO 32000-1 §9.6.6.4 says non-symbolic
// TrueType fonts that omit /Encoding inherit their cmap-derived
// encoding. The Mac Roman subtable is treated as authoritative whenever
// it is present, even alongside a Microsoft Unicode subtable, because
// high-byte glyphs are positioned at MacRoman slots (0x96 = ñ, 0xA9 = ©,
// 0xD0 = en-dash). Without this override these bytes surface as WinAnsi
// look-alikes on Mac-authored govdocs filings.
func applyTrueTypeBuiltinEncoding(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, font *Font) {
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
	if hasMacRoman {
		font.Encoding = copyEncoding(encodings.MacRomanEncoding)
	}
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
		if _, set := font.Encoding[code]; set {
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
// looks up that glyph's width in the canonical AFM table.
func injectStandard14Widths(font *Font, baseFont string) {
	name := normalizeBase14Name(baseFont)
	if !isStandard14(name) {
		// Last resort: use Helvetica (sans-serif) or Times-Roman (serif)
		// metrics as a best-effort fallback for width-based space inference.
		if isSerifFont(baseFont) {
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
			}
		}
	}

	font.FirstChar = 0
	font.LastChar = 255
	font.Widths = widths
}

// extractType3GlyphWidth parses a Type 3 CharProc stream and returns the
// glyph width from the d0 or d1 operator.
//
// d0 (setcharwidth): wx wy d0 sets the glyph width.
// d1 (setcachedevice): wx wy llx lly urx ury d1 sets width and cache bbox.
//
// The first operand (wx) is the horizontal advance width in glyph space units.
func extractType3GlyphWidth(ctx context.Context, doc *document.Document, stream ast.PdfStream) (float64, error) {
	data, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		return 0, err
	}

	ops, err := content.ParseStreamData(ctx, data)
	if err != nil {
		return 0, err
	}

	for _, op := range ops {
		switch op.Operator {
		case "d0":
			if len(op.Operands) >= 1 {
				return document.Float(op.Operands[0]), nil
			}
		case "d1":
			if len(op.Operands) >= 1 {
				return document.Float(op.Operands[0]), nil
			}
		}
	}

	return 0, fmt.Errorf("no d0/d1 operator found in CharProc stream")
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
