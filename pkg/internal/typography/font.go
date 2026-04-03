package typography

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"maps"
	"regexp"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/typography/cff"
	"github.com/gotenberg/litho/pkg/internal/typography/cmap"
	"github.com/gotenberg/litho/pkg/internal/typography/encodings"
	"github.com/gotenberg/litho/pkg/internal/typography/truetype"
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
}

// GetGlyphWidth returns the advance width (in 1/1000 text-space units) for the
// given char code using a strict 5-step fallback chain:
//
//  1. PDF width arrays: /Widths (simple) or /W (CID). Type 3 uses /CharProcs.
//  2. CID default width: /DW from the CIDFont (defaults to 1000 per spec).
//  3. Simple font /MissingWidth from the /FontDescriptor.
//  4. TrueType binary hmtx: read advance width from the embedded font program.
//  5. /FontBBox average, then absolute fallback of 1000 (= 1 em).
func (f *Font) GetGlyphWidth(charCode int) float64 {
	// --- Step 1: PDF width arrays ---

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
		// --- Step 3: MissingWidth from FontDescriptor ---
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
		// --- Step 2: CID default width (/DW, defaults to 1000 per ISO 32000) ---
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

	// --- Step 3: /MissingWidth from /FontDescriptor ---
	if f.MissingWidth > 0 {
		return f.MissingWidth
	}

	return f.resolveWidth(charCode)
}

// resolveWidth implements steps 4-5 of the fallback chain.
func (f *Font) resolveWidth(charCode int) float64 {
	// --- Step 4: TrueType binary hmtx table ---
	if f.HmtxWidths != nil && f.UnitsPerEm > 0 {
		gid := uint16(charCode) //nolint:gosec // charCode is a glyph index in this context
		if advance, ok := f.HmtxWidths[gid]; ok && advance > 0 {
			// Convert from font design units to 1/1000 text-space units.
			return float64(advance) * 1000.0 / float64(f.UnitsPerEm)
		}
	}

	// --- Step 5: /FontBBox width, OS/2 xAvgCharWidth, then absolute fallback ---
	bboxWidth := f.FontBBox[2] - f.FontBBox[0] // xMax - xMin
	if bboxWidth > 0 {
		return bboxWidth
	}

	// OS/2 xAvgCharWidth provides a better fallback than a hard-coded 1 em.
	if f.AvgWidth > 0 {
		return float64(f.AvgWidth) * 1000.0 / float64(max(f.UnitsPerEm, 1))
	}

	return 1000 // 1 em — prevents zero-width glyphs
}

// GetVerticalMetrics returns the vertical displacement vector (vX, vY) and the
// vertical advance width (w1) for a given CID, in 1/1000 text-space units.
// Per PDF spec Table 117:
//   - w1  = vertical advance (negative = downward)
//   - vX  = X component of the position vector
//   - vY  = Y component of the position vector
func (f *Font) GetVerticalMetrics(charCode int) (w1, vX, vY float64) {
	if m, ok := f.CIDVerticalMetrics[charCode]; ok {
		return m[0], m[1], m[2]
	}
	// Default: vX = half the horizontal width, vY and w1 from DW2
	halfW0 := f.GetGlyphWidth(charCode) / 2.0
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
		startCID := int(document.GetFloat(w2Arr[i]))
		i++
		switch next := w2Arr[i].(type) {
		case ast.PdfArray: // individual form: c1 [w1_1 vx_1 vy_1 ...]
			for j := 0; j+2 < len(next); j += 3 {
				metrics[startCID+j/3] = [3]float64{
					document.GetFloat(next[j]),
					document.GetFloat(next[j+1]),
					document.GetFloat(next[j+2]),
				}
			}
			i++
		case ast.PdfInt, ast.PdfReal: // range form: c1 c2 w1 vx vy
			endCID := int(document.GetFloat(next))
			i++
			if i+2 >= len(w2Arr) {
				break
			}
			w1 := document.GetFloat(w2Arr[i])
			vx := document.GetFloat(w2Arr[i+1])
			vy := document.GetFloat(w2Arr[i+2])
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
		startCID := int(document.GetFloat(wArr[i]))
		i++
		switch next := wArr[i].(type) {
		case ast.PdfArray: // individual form: c1 [w1 w2 ...]
			for j, wObj := range next {
				widths[startCID+j] = document.GetFloat(wObj)
			}
			i++
		case ast.PdfInt, ast.PdfReal: // range form: c1 c2 w
			endCID := int(document.GetFloat(next))
			i++
			if i >= len(wArr) {
				break
			}
			w := document.GetFloat(wArr[i])
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

// GetFont extracts and resolves a [Font] from a PDF font dictionary.
func GetFont(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) (*Font, error) {
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

	// Detect Symbol and ZapfDingbats fonts by BaseFont name to select
	// the correct default encoding per the PDF spec (these fonts use
	// their own built-in encodings, not WinAnsiEncoding).
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

	defaultEncoding := copyEncoding(encodings.WinAnsiEncoding)
	if isSymbolFont {
		defaultEncoding = encodings.BuildSymbolEncoding()
	} else if isDingbatsFont {
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
			if cached, hit := doc.GetCachedCMap(ref.ObjectNum); hit {
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
	// embedded Type1 font program (/FontFile). When the PDF-level /Encoding
	// is absent, the font program's encoding is authoritative and overrides
	// the default WinAnsiEncoding. When /Encoding IS present, the font
	// program only fills holes (codes not covered by Differences).
	_, hasExplicitEncoding := fontDict["Encoding"]
	extractType1Encoding(ctx, doc, fontDict, font, !hasExplicitEncoding)

	// Extract /FontBBox and embedded font metrics from the FontDescriptor.
	extractFontDescriptorMetrics(ctx, doc, fontDict, font)

	// Predefined CJK encoding CMap fallback for Type0 fonts: when the
	// /Encoding is a predefined CMap name (e.g., GBK-EUC-H, EUC-H), load
	// the byte→CID mapping from the embedded bcmap and compose with the
	// Adobe CID→Unicode table to synthesize a ToUnicode CMap.
	if subtypeName == "Type0" && font.ToUnicode == nil && !font.CIDIdentityFallback {
		if encName := encodingCMapName(ctx, doc, fontDict); encName != "" {
			if cm := cmap.LoadPredefinedCMap(encName); cm != nil {
				font.ToUnicode = cm
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

	// Adobe predefined CID→Unicode fallback for Type0 fonts.
	if subtypeName == "Type0" && font.ToUnicode == nil && font.GIDToUnicode == nil && !font.CIDIdentityFallback {
		applyAdobeCIDFallback(ctx, doc, fontDict, font)
	}

	// Identity-H/V fallback: last resort for Type0 fonts with no other
	// mapping. Treats 2-byte character codes as direct Unicode codepoints.
	if subtypeName == "Type0" && font.ToUnicode == nil && font.GIDToUnicode == nil && !font.CIDIdentityFallback {
		if isIdentityEncoding(ctx, doc, fontDict) {
			font.CIDIdentityFallback = true
		}
	}

	// CJK heuristic for simple fonts: when a TrueType or Type1 font has no
	// ToUnicode CMap and the BaseFont name contains CJK characters, the font
	// is likely mis-declared with WinAnsiEncoding but actually uses GBK byte
	// encoding. Load the GBK-EUC-H predefined CMap as a ToUnicode substitute.
	if font.ToUnicode == nil && (subtypeName == "TrueType" || subtypeName == "Type1") {
		if bfObj, ok := fontDict["BaseFont"]; ok {
			bfName, _ := document.ResolveName(ctx, doc, bfObj)
			if hasCJKFontName(string(bfName)) {
				if cm := cmap.LoadPredefinedCMap("GBK-EUC-H"); cm != nil {
					font.ToUnicode = cm
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
		doc.CacheCMap(objNum, c)
	}
	return c
}

// parseToUnicode resolves and parses a ToUnicode CMap stream.
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
		// Dictionary-based Encoding
		baseMap := encodings.WinAnsiEncoding // Default fallback
		if baseObj, ok := enc["BaseEncoding"]; ok {
			baseName, err := document.ResolveName(ctx, doc, baseObj)
			if err != nil {
				doc.Logger.Debug("resolving font BaseEncoding name", slog.Any("err", err))
			}
			if baseName != "" {
				switch baseName {
				case "WinAnsiEncoding":
					baseMap = encodings.WinAnsiEncoding
				case "MacRomanEncoding":
					baseMap = encodings.MacRomanEncoding
				case "StandardEncoding":
					baseMap = encodings.StandardEncoding
				case "MacExpertEncoding":
					baseMap = encodings.MacExpertEncoding
				}
			}
		}
		font.Encoding = copyEncoding(baseMap)

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
				font.FontMatrix[i] = document.GetFloat(fmArr[i])
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
		font.FirstChar = int(document.GetFloat(fcObj))
	}
	if lcObj, ok := fontDict["LastChar"]; ok {
		font.LastChar = int(document.GetFloat(lcObj))
	}
	if wObj, ok := fontDict["Widths"]; ok {
		wArr, err := document.ResolveArray(ctx, doc, wObj)
		if err != nil {
			doc.Logger.Debug("resolving Type3 Widths array", slog.Any("err", err))
		}
		if wArr != nil {
			font.Widths = make([]float64, len(wArr))
			for i, item := range wArr {
				font.Widths[i] = document.GetFloat(item)
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
					font.MissingWidth = document.GetFloat(dwObj)
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
					font.WMode = int(document.GetFloat(wmObj))
				}
				// /DW2: default vertical displacement [vY w1].
				if dw2Obj, ok := cidDict["DW2"]; ok {
					dw2Arr, err := document.ResolveArray(ctx, doc, dw2Obj)
					if err != nil {
						doc.Logger.Debug("resolving CIDFont /DW2 array", slog.Any("err", err))
					}
					if len(dw2Arr) == 2 {
						font.DW2 = [2]float64{document.GetFloat(dw2Arr[0]), document.GetFloat(dw2Arr[1])}
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
			font.WMode = int(document.GetFloat(wmObj))
		}
	}
}

// resolveSimpleFontMetrics extracts metrics for simple fonts (Type1, TrueType,
// MMType1): /FirstChar, /LastChar, /Widths array, /MissingWidth from the
// /FontDescriptor, and Standard 14 width fallback.
func resolveSimpleFontMetrics(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, font *Font) {
	if fcObj, ok := fontDict["FirstChar"]; ok {
		font.FirstChar = int(document.GetFloat(fcObj))
	}
	if lcObj, ok := fontDict["LastChar"]; ok {
		font.LastChar = int(document.GetFloat(lcObj))
	}
	if wObj, ok := fontDict["Widths"]; ok {
		wArr, err := document.ResolveArray(ctx, doc, wObj)
		if err != nil {
			doc.Logger.Debug("resolving simple font Widths array", slog.Any("err", err))
		}
		if wArr != nil {
			font.Widths = make([]float64, len(wArr))
			for i, item := range wArr {
				font.Widths[i] = document.GetFloat(item)
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
				font.MissingWidth = document.GetFloat(mwObj)
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
	fdDict := findFontDescriptor(ctx, doc, fontDict)
	if fdDict == nil {
		return
	}

	// /FontBBox [xMin yMin xMax yMax] — Step 5 fallback.
	if bboxObj, ok := fdDict["FontBBox"]; ok {
		bboxArr, err := document.ResolveArray(ctx, doc, bboxObj)
		if err != nil {
			doc.Logger.Debug("resolving FontBBox array", slog.Any("err", err))
		}
		if len(bboxArr) == 4 {
			for i := range 4 {
				font.FontBBox[i] = document.GetFloat(bboxArr[i])
			}
		}
	}

	// Binary font widths — Step 4 fallback.
	fontData := decodeFontStream(ctx, doc, fdDict, "FontFile2")
	if fontData == nil {
		fontData = decodeFontStream(ctx, doc, fdDict, "FontFile3")
	}
	if fontData == nil {
		return
	}

	// OS/2 xAvgCharWidth — a better fallback than FontBBox or 1000.
	font.AvgWidth = readOS2AvgWidth(fontData)

	widths := ExtractBinaryWidths(ctx, fontData)
	if len(widths) == 0 {
		return
	}

	font.HmtxWidths = widths

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
func ExtractBinaryWidths(ctx context.Context, fontData []byte) map[uint16]uint16 {
	switch detectFontFormat(fontData) {
	case fontFormatTrueType:
		w, _ := truetype.GlyphWidths(ctx, fontData)
		return w
	case fontFormatOTTO:
		cffData, err := extractCFFFromOTTO(fontData)
		if err != nil {
			return nil
		}
		w, _ := cff.GlyphWidths(cffData)
		return w
	case fontFormatBareCFF:
		w, _ := cff.GlyphWidths(fontData)
		return w
	case fontFormatUnknown:
		// Try TrueType as fallback.
		w, _ := truetype.GlyphWidths(ctx, fontData)
		return w
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
		m, _ := cff.GlyphNames(fontData)
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
		return 1000 //nolint:mnd
	}
	records, err := truetype.ParseTableDirectory(data)
	if err != nil {
		return 1000 //nolint:mnd // safe default
	}
	headData, err := truetype.TableData(data, records, "head")
	if err != nil || len(headData) < 20 { //nolint:mnd
		return 1000 //nolint:mnd
	}
	// unitsPerEm is at offset 18 in the head table.
	return binary.BigEndian.Uint16(headData[18:20]) //nolint:mnd
}

// type1EncodingRe matches "dup <code> /<name> put" lines in Type1 font programs.
var type1EncodingRe = regexp.MustCompile(`dup\s+(\d+)\s+/(\S+)\s+put`)

// extractType1Encoding parses the encoding from an embedded Type1 font program
// (/FontFile) and fills any holes in font.Encoding. This is needed when the
// PDF font dictionary has no /Encoding or an incomplete /Differences array,
// because the font program itself defines the authoritative encoding.
func extractType1Encoding(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, font *Font, overwrite bool) {
	fdDict := findFontDescriptor(ctx, doc, fontDict)
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
		if err != nil || code < 0 || code > 255 { //nolint:mnd
			continue
		}
		glyphName := string(match[2])
		if glyphName != ".notdef" {
			result[byte(code)] = glyphName //nolint:gosec
		}
	}

	if len(result) == 0 {
		return nil
	}
	return result
}

// extractEmbeddedCmap attempts to extract a GID→Unicode mapping from the
// embedded TrueType (/FontFile2) or OpenType (/FontFile3) font program.
// For Type0 fonts the FontDescriptor lives inside the CIDFont descendant.
func extractEmbeddedCmap(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, font *Font) {
	fdDict := findFontDescriptor(ctx, doc, fontDict)
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

	gidMap := extractGIDToUnicode(ctx, fontData)
	if len(gidMap) == 0 {
		return
	}

	font.GIDToUnicode = gidMap
}

// extractGIDToUnicode dispatches font data to the appropriate parser
// (TrueType or CFF) based on the font signature.
func extractGIDToUnicode(ctx context.Context, fontData []byte) map[uint16]rune {
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
	mapName, _ := document.ResolveName(ctx, doc, mapObj)
	if mapName == "Identity" {
		font.CIDIdentityFallback = true
		return
	}

	// /CIDToGIDMap stream: 2-byte big-endian GIDs indexed by CID.
	// Parse the stream and combine with the embedded font's GID→Unicode
	// cmap to build a CID→Unicode mapping.
	applyCIDToGIDStream(ctx, doc, fontDict, font, mapObj)
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
	gidToUni := extractGIDToUnicode(ctx, decodeFontStreamFromDescriptor(ctx, doc, fontDict))
	if len(gidToUni) == 0 {
		return
	}

	// Build CID→Unicode: CIDToGIDMap[CID] → GID, gidToUni[GID] → Unicode.
	mapping := make(map[string]string, 4096) //nolint:mnd
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
		key := string([]byte{byte(cid >> 8), byte(cid)}) //nolint:gosec
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
	fdDict := findFontDescriptor(ctx, doc, fontDict)
	if fdDict == nil {
		return nil
	}
	if data := decodeFontStream(ctx, doc, fdDict, "FontFile2"); data != nil {
		return data
	}
	return decodeFontStream(ctx, doc, fdDict, "FontFile3")
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

// findFontDescriptor locates the /FontDescriptor dictionary. For Type0 fonts,
// it is inside the first element of /DescendantFonts.
func findFontDescriptor(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) ast.PdfDict {
	// Direct /FontDescriptor on the font dict (simple fonts).
	if fdObj, ok := fontDict["FontDescriptor"]; ok {
		fdDict, err := document.ResolveDict(ctx, doc, fdObj)
		if err != nil {
			doc.Logger.Debug("resolving FontDescriptor dict", slog.Any("err", err))
		}
		if fdDict != nil {
			return fdDict
		}
	}

	// For Type0: /DescendantFonts[0]/FontDescriptor.
	if dfObj, ok := fontDict["DescendantFonts"]; ok {
		dfArr, err := document.ResolveArray(ctx, doc, dfObj)
		if err != nil {
			doc.Logger.Debug("resolving DescendantFonts for FontDescriptor lookup", slog.Any("err", err))
		}
		if len(dfArr) > 0 {
			cidDict, err := document.ResolveDict(ctx, doc, dfArr[0])
			if err != nil {
				doc.Logger.Debug("resolving CIDFont dict for FontDescriptor lookup", slog.Any("err", err))
			}
			if cidDict != nil {
				if fdObj, ok := cidDict["FontDescriptor"]; ok {
					fdDict, err := document.ResolveDict(ctx, doc, fdObj)
					if err != nil {
						doc.Logger.Debug("resolving CIDFont FontDescriptor dict", slog.Any("err", err))
					}
					if fdDict != nil {
						return fdDict
					}
				}
			}
		}
	}

	return nil
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
// d0 (setcharwidth): wx wy d0 — sets the glyph width.
// d1 (setcachedevice): wx wy llx lly urx ury d1 — sets width and cache bbox.
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
				return document.GetFloat(op.Operands[0]), nil
			}
		case "d1":
			if len(op.Operands) >= 1 {
				return document.GetFloat(op.Operands[0]), nil
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
	return 0, false
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
