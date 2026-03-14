package typography

import (
	"fmt"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/filter"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
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
	WMode              int                // 0 = horizontal (default), 1 = vertical
	DW2                [2]float64         // Default vertical metrics [vY w1] (default: [880 -1000])
	CIDVerticalMetrics map[int][3]float64 // CID → [w1Y vX vY] from /W2 array
}

// GetGlyphWidth returns the advance width (in 1/1000 text-space units) for the
// given char code. For CIDFonts the argument is the CID; for simple fonts it is
// the raw single-byte char code.
func (f *Font) GetGlyphWidth(charCode int) float64 {
	// Type 3 fonts: look up glyph name via encoding, then apply FontMatrix.
	if f.Type3Widths != nil && charCode >= 0 && charCode <= 255 {
		if r, ok := f.Encoding[byte(charCode)]; ok { //nolint:gosec // charCode bounds-checked above
			glyphName := encodings.RuneToGlyph(r)
			if w, ok := f.Type3Widths[glyphName]; ok {
				// Width is in glyph space; multiply by FontMatrix[0] to
				// convert to text space, then scale to 1/1000 units.
				return w * f.FontMatrix[0] * 1000
			}
		}
		// Fall back to standard Widths array if present (some Type 3 fonts
		// provide both /CharProcs and a /Widths array).
		idx := charCode - f.FirstChar
		if idx >= 0 && idx < len(f.Widths) {
			return f.Widths[idx]
		}
		return f.MissingWidth
	}
	if f.CIDWidths != nil {
		if w, ok := f.CIDWidths[charCode]; ok {
			return w
		}
		return f.MissingWidth
	}
	idx := charCode - f.FirstChar
	if idx >= 0 && idx < len(f.Widths) {
		return f.Widths[idx]
	}
	return f.MissingWidth
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
		startCID := int(getFloat(w2Arr[i]))
		i++
		switch next := w2Arr[i].(type) {
		case ast.PdfArray: // individual form: c1 [w1_1 vx_1 vy_1 ...]
			for j := 0; j+2 < len(next); j += 3 {
				metrics[startCID+j/3] = [3]float64{
					getFloat(next[j]),
					getFloat(next[j+1]),
					getFloat(next[j+2]),
				}
			}
			i++
		case ast.PdfInt, ast.PdfReal: // range form: c1 c2 w1 vx vy
			endCID := int(getFloat(next))
			i++
			if i+2 >= len(w2Arr) {
				break
			}
			w1 := getFloat(w2Arr[i])
			vx := getFloat(w2Arr[i+1])
			vy := getFloat(w2Arr[i+2])
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
		startCID := int(getFloat(wArr[i]))
		i++
		switch next := wArr[i].(type) {
		case ast.PdfArray: // individual form: c1 [w1 w2 ...]
			for j, wObj := range next {
				widths[startCID+j] = getFloat(wObj)
			}
			i++
		case ast.PdfInt, ast.PdfReal: // range form: c1 c2 w
			endCID := int(getFloat(next))
			i++
			if i >= len(wArr) {
				break
			}
			w := getFloat(wArr[i])
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

// GetFont extracts and resolves a font from a font dictionary.
func GetFont(doc *document.Document, fontDict ast.PdfDict) (*Font, error) {
	subtypeObj, ok := fontDict["Subtype"]
	if !ok {
		return nil, fmt.Errorf("font dictionary missing /Subtype")
	}

	subtypeResolved, err := resolveObject(doc, subtypeObj)
	if err != nil {
		return nil, err
	}

	subtypeName, ok := subtypeResolved.(ast.PdfName)
	if !ok {
		return nil, fmt.Errorf("font /Subtype is not a name")
	}

	font := &Font{
		Subtype:      string(subtypeName),
		Encoding:     copyEncoding(encodings.WinAnsiEncoding), // Default to WinAnsiEncoding
		MissingWidth: 1000.0,                                  // PDF spec fallback
	}

	// Try to extract ToUnicode CMap
	if tuObj, ok := fontDict["ToUnicode"]; ok {
		tuResolved, err := resolveObject(doc, tuObj)
		if err == nil {
			if stream, ok := tuResolved.(ast.PdfStream); ok {
				decoded, err := filter.DecodeStream(stream)
				if err == nil {
					c, err := cmap.Parse(decoded)
					if err == nil {
						font.ToUnicode = c
					}
				}
			}
		}
	}

	// Try to extract Encoding
	if encObj, ok := fontDict["Encoding"]; ok {
		encResolved, err := resolveObject(doc, encObj)
		if err == nil {
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
					baseRes, _ := resolveObject(doc, baseObj)
					if baseName, ok := baseRes.(ast.PdfName); ok {
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
					diffRes, _ := resolveObject(doc, diffObj)
					if diffArr, ok := diffRes.(ast.PdfArray); ok {
						currentIndex := 0
						for _, item := range diffArr {
							itemRes, _ := resolveObject(doc, item)
							switch v := itemRes.(type) {
							case ast.PdfInt:
								currentIndex = int(v)
							case ast.PdfName:
								if r, ok := encodings.GlyphToRune[string(v)]; ok {
									font.Encoding[byte(currentIndex)] = r
								}
								currentIndex++
							}
						}
					}
				}
			}
		}
	}

	// Type 3 fonts: extract widths from /CharProcs streams via d0/d1 operators.
	if subtypeName == "Type3" {
		// /FontMatrix maps glyph space → text space. Default is [0.001 0 0 0.001 0 0].
		font.FontMatrix = [6]float64{0.001, 0, 0, 0.001, 0, 0}
		if fmObj, ok := fontDict["FontMatrix"]; ok {
			fmRes, _ := resolveObject(doc, fmObj)
			if fmArr, ok := fmRes.(ast.PdfArray); ok && len(fmArr) == 6 {
				for i := range 6 {
					font.FontMatrix[i] = getFloat(fmArr[i])
				}
			}
		}

		// /CharProcs maps glyph names → streams containing d0/d1 width operators.
		if cpObj, ok := fontDict["CharProcs"]; ok {
			cpRes, _ := resolveObject(doc, cpObj)
			if cpDict, ok := cpRes.(ast.PdfDict); ok {
				font.Type3Widths = make(map[string]float64, len(cpDict))
				for name, streamObj := range cpDict {
					streamRes, err := resolveObject(doc, streamObj)
					if err != nil {
						continue
					}
					stream, ok := streamRes.(ast.PdfStream)
					if !ok {
						continue
					}
					w, err := extractType3GlyphWidth(stream)
					if err != nil {
						continue
					}
					font.Type3Widths[string(name)] = w
				}
			}
		}

		// Also extract /FirstChar, /LastChar, /Widths as fallback.
		if fcObj, ok := fontDict["FirstChar"]; ok {
			font.FirstChar = int(getFloat(fcObj))
		}
		if lcObj, ok := fontDict["LastChar"]; ok {
			font.LastChar = int(getFloat(lcObj))
		}
		if wObj, ok := fontDict["Widths"]; ok {
			wResolved, _ := resolveObject(doc, wObj)
			if wArr, ok := wResolved.(ast.PdfArray); ok {
				font.Widths = make([]float64, len(wArr))
				for i, item := range wArr {
					font.Widths[i] = getFloat(item)
				}
			}
		}

		return font, nil
	}

	// Extract glyph width metrics.
	if subtypeName == "Type0" {
		// Default vertical metrics per PDF spec Table 117.
		font.DW2 = [2]float64{880, -1000}

		// CIDFont metrics live in the first element of /DescendantFonts.
		if dfObj, ok := fontDict["DescendantFonts"]; ok {
			dfResolved, _ := resolveObject(doc, dfObj)
			if dfArr, ok := dfResolved.(ast.PdfArray); ok && len(dfArr) > 0 {
				cidResolved, _ := resolveObject(doc, dfArr[0])
				if cidDict, ok := cidResolved.(ast.PdfDict); ok {
					// /DW is the default glyph width (default 1000 per spec).
					if dwObj, ok := cidDict["DW"]; ok {
						font.MissingWidth = getFloat(dwObj)
					}
					// /W encodes per-CID or range widths.
					if wObj, ok := cidDict["W"]; ok {
						wResolved, _ := resolveObject(doc, wObj)
						if wArr, ok := wResolved.(ast.PdfArray); ok {
							font.CIDWidths = parseCIDWidths(wArr)
						}
					}
					// /WMode: 0 = horizontal, 1 = vertical.
					if wmObj, ok := cidDict["WMode"]; ok {
						font.WMode = int(getFloat(wmObj))
					}
					// /DW2: default vertical displacement [vY w1].
					if dw2Obj, ok := cidDict["DW2"]; ok {
						dw2Res, _ := resolveObject(doc, dw2Obj)
						if dw2Arr, ok := dw2Res.(ast.PdfArray); ok && len(dw2Arr) == 2 {
							font.DW2 = [2]float64{getFloat(dw2Arr[0]), getFloat(dw2Arr[1])}
						}
					}
					// /W2: per-CID vertical metrics [w1Y vX vY].
					if w2Obj, ok := cidDict["W2"]; ok {
						w2Res, _ := resolveObject(doc, w2Obj)
						if w2Arr, ok := w2Res.(ast.PdfArray); ok {
							font.CIDVerticalMetrics = parseCIDVerticalWidths(w2Arr)
						}
					}
				}
			}
		}

		// Also check /WMode on the font dictionary itself (some producers put it here).
		if font.WMode == 0 {
			if wmObj, ok := fontDict["WMode"]; ok {
				font.WMode = int(getFloat(wmObj))
			}
		}
	} else {
		// Simple fonts (Type1, TrueType, Type3, MMType1): /FirstChar, /LastChar, /Widths.
		if fcObj, ok := fontDict["FirstChar"]; ok {
			font.FirstChar = int(getFloat(fcObj))
		}
		if lcObj, ok := fontDict["LastChar"]; ok {
			font.LastChar = int(getFloat(lcObj))
		}
		if wObj, ok := fontDict["Widths"]; ok {
			wResolved, _ := resolveObject(doc, wObj)
			if wArr, ok := wResolved.(ast.PdfArray); ok {
				font.Widths = make([]float64, len(wArr))
				for i, item := range wArr {
					font.Widths[i] = getFloat(item)
				}
			}
		}
		// /MissingWidth from /FontDescriptor overrides the 1000 default.
		if fdObj, ok := fontDict["FontDescriptor"]; ok {
			fdResolved, _ := resolveObject(doc, fdObj)
			if fdDict, ok := fdResolved.(ast.PdfDict); ok {
				if mwObj, ok := fdDict["MissingWidth"]; ok {
					font.MissingWidth = getFloat(mwObj)
				}
			}
		}

		// Standard 14 fallback: if /Widths is missing and the /BaseFont
		// matches one of the 14 standard fonts, inject the hardcoded
		// Adobe Font Metrics so spatial text reconstruction succeeds.
		if len(font.Widths) == 0 {
			if bfObj, ok := fontDict["BaseFont"]; ok {
				bfRes, _ := resolveObject(doc, bfObj)
				if bfName, ok := bfRes.(ast.PdfName); ok {
					injectStandard14Widths(font, string(bfName))
				}
			}
		}
	}

	// Embedded TrueType/OpenType cmap fallback: if there is no ToUnicode CMap,
	// extract the glyph-to-Unicode mapping from the embedded font binary so the
	// text extraction pipeline can still produce meaningful UTF-8.
	if font.ToUnicode == nil {
		extractEmbeddedCmap(doc, fontDict, font)
	}

	return font, nil
}

// extractEmbeddedCmap attempts to extract a GID→Unicode mapping from the
// embedded TrueType (/FontFile2) or OpenType (/FontFile3) font program.
// For Type0 fonts the FontDescriptor lives inside the CIDFont descendant.
func extractEmbeddedCmap(doc *document.Document, fontDict ast.PdfDict, font *Font) {
	fdDict := findFontDescriptor(doc, fontDict)
	if fdDict == nil {
		return
	}

	// Try /FontFile2 (TrueType), then /FontFile3 (OpenType/CFF).
	fontData := decodeFontStream(doc, fdDict, "FontFile2")
	if fontData == nil {
		fontData = decodeFontStream(doc, fdDict, "FontFile3")
	}
	if fontData == nil {
		return
	}

	gidMap, err := truetype.GlyphToUnicode(fontData)
	if err != nil || len(gidMap) == 0 {
		return
	}

	font.GIDToUnicode = gidMap
}

// findFontDescriptor locates the /FontDescriptor dictionary. For Type0 fonts,
// it is inside the first element of /DescendantFonts.
func findFontDescriptor(doc *document.Document, fontDict ast.PdfDict) ast.PdfDict {
	// Direct /FontDescriptor on the font dict (simple fonts).
	if fdObj, ok := fontDict["FontDescriptor"]; ok {
		fdRes, _ := resolveObject(doc, fdObj)
		if fdDict, ok := fdRes.(ast.PdfDict); ok {
			return fdDict
		}
	}

	// For Type0: /DescendantFonts[0]/FontDescriptor.
	if dfObj, ok := fontDict["DescendantFonts"]; ok {
		dfRes, _ := resolveObject(doc, dfObj)
		if dfArr, ok := dfRes.(ast.PdfArray); ok && len(dfArr) > 0 {
			cidRes, _ := resolveObject(doc, dfArr[0])
			if cidDict, ok := cidRes.(ast.PdfDict); ok {
				if fdObj, ok := cidDict["FontDescriptor"]; ok {
					fdRes, _ := resolveObject(doc, fdObj)
					if fdDict, ok := fdRes.(ast.PdfDict); ok {
						return fdDict
					}
				}
			}
		}
	}

	return nil
}

// decodeFontStream resolves and decodes an embedded font stream by key name.
func decodeFontStream(doc *document.Document, fdDict ast.PdfDict, key ast.PdfName) []byte {
	obj, ok := fdDict[key]
	if !ok {
		return nil
	}
	resolved, err := resolveObject(doc, obj)
	if err != nil {
		return nil
	}
	stream, ok := resolved.(ast.PdfStream)
	if !ok {
		return nil
	}
	data, err := filter.DecodeStream(stream)
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
		return
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
func extractType3GlyphWidth(stream ast.PdfStream) (float64, error) {
	data, err := filter.DecodeStream(stream)
	if err != nil {
		return 0, err
	}

	ops, err := content.ParseStreamData(data)
	if err != nil {
		return 0, err
	}

	for _, op := range ops {
		switch op.Operator {
		case "d0":
			if len(op.Operands) >= 1 {
				return getFloat(op.Operands[0]), nil
			}
		case "d1":
			if len(op.Operands) >= 1 {
				return getFloat(op.Operands[0]), nil
			}
		}
	}

	return 0, fmt.Errorf("no d0/d1 operator found in CharProc stream")
}
