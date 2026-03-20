package typography

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/models"
)

// processText maps the raw bytes of a PDF string to UTF-8 and generates models.TextElements.
func processText(str ast.PdfString, font *Font, machine *graphics.Machine, elements *[]models.TextElement) {
	data := []byte(str)
	state := machine.Current()
	fontSize := state.Text.Tfs
	if fontSize == 0 {
		fontSize = 12 // Fallback
	}

	// Compute the effective font size in page-space units by applying the
	// text matrix and CTM scale. This ensures TextElement.FontSize is in
	// the same coordinate system as TextElement.Width and positions, which
	// is critical for correct space inference in ReconstructText.
	ctm := state.CTM
	tm := state.Text.Tm
	effectiveFontSize := math.Abs(fontSize * tm[0] * ctm[0])
	if effectiveFontSize == 0 {
		// Vertical or rotated text: try the Y scale.
		effectiveFontSize = math.Abs(fontSize * tm[3] * ctm[3])
	}
	if effectiveFontSize == 0 {
		effectiveFontSize = fontSize // Last resort: use raw.
	}

	mcid := -1
	var mcLang, mcExpansion string
	for i := len(machine.MarkedContent) - 1; i >= 0; i-- {
		mc := &machine.MarkedContent[i]
		if mcid == -1 && mc.MCID != -1 {
			mcid = mc.MCID
		}
		if mcLang == "" && mc.Language != "" {
			mcLang = mc.Language
		}
		if mcExpansion == "" && mc.ExpansionText != "" {
			mcExpansion = mc.ExpansionText
		}
	}

	for len(data) > 0 {
		var char string
		var n int
		var ok bool

		if font.ToUnicode != nil {
			n, char, ok = font.ToUnicode.Map(data)
		}

		if !ok {
			if n > 0 {
				// Codespace range consumed n bytes but found no mapping.
				// Consume those bytes without trying single-byte fallback
				// to avoid misinterpreting multi-byte codes.
				data = data[n:]
				continue
			}
			if r, ok := font.Encoding[data[0]]; ok {
				char = string(r)
			} else if font.GIDToUnicode != nil {
				// Fallback: use the embedded TrueType/OpenType cmap to map
				// the raw character code (treated as a glyph ID) to a Unicode rune.
				// For CIDFonts (Type0), try 2-byte big-endian code first.
				mapped := false
				if font.CIDWidths != nil && len(data) >= 2 {
					gid := uint16(data[0])<<8 | uint16(data[1])
					if r, ok := font.GIDToUnicode[gid]; ok {
						char = string(r)
						n = 2
						mapped = true
					}
				}
				if !mapped {
					if r, ok := font.GIDToUnicode[uint16(data[0])]; ok {
						char = string(r)
					} else {
						char = "?"
					}
				}
			} else if font.CIDIdentityFallback && len(data) >= 2 {
				// CIDToGIDMap /Identity without embedded font: treat the
				// 2-byte character code as a direct Unicode code point.
				cp := rune(uint16(data[0])<<8 | uint16(data[1]))
				if cp > 0 {
					char = string(cp)
				}
				n = 2
			} else {
				char = "?"
			}
			if n == 0 {
				n = 1
			}
		}

		// Decompose standard ligatures to their component characters so that
		// text search and comparison works with plain ASCII equivalents.
		char = decomposeLigature(char)

		// Skip known non-glyph control characters (NUL, CR, LF) that sometimes
		// appear in PDF strings. Do NOT skip low byte values in general — PDF
		// fonts frequently use 0x01–0x1F as valid glyph codes (e.g. ligatures).
		if char == "?" && len(data) > 0 {
			b := data[0]
			if b == 0x00 || b == 0x0a || b == 0x0d || b == 0x7f {
				data = data[n:]
				continue
			}
		}

		// Calculate absolute position
		tm := state.Text.Tm
		ctm := state.CTM
		tx := tm[4]
		ty := tm[5]
		x := tx*ctm[0] + ty*ctm[2] + ctm[4]
		y := tx*ctm[1] + ty*ctm[3] + ctm[5]

		// Visibility heuristics
		isHidden := false
		isInvisible := false

		// 1. Text rendering mode 3 is "Neither fill nor stroke text (invisible)"
		if state.Text.Tmode == 3 {
			isInvisible = true
		}

		// 2. Color checks (e.g. white text on white background), interpreted per active color space.
		// Pattern color spaces are never "invisible" — they paint with a tiled or gradient fill.
		if state.FillPatternType == "" {
			switch state.FillColorSpace {
			case "DeviceRGB":
				if len(state.FillColor) == 3 && state.FillColor[0] == 1 && state.FillColor[1] == 1 && state.FillColor[2] == 1 {
					isInvisible = true
				}
			case "DeviceCMYK":
				if len(state.FillColor) == 4 && state.FillColor[0] == 0 && state.FillColor[1] == 0 && state.FillColor[2] == 0 && state.FillColor[3] == 0 {
					isInvisible = true
				}
			default: // DeviceGray and any named space treated as single-channel gray
				if len(state.FillColor) == 1 && state.FillColor[0] == 1 {
					isInvisible = true
				}
			}
		}

		// Compute advance width before visibility checks (needed for clipping).
		charCode := int(data[0])
		if n == 2 && len(data) >= 2 {
			charCode = int(data[0])<<8 | int(data[1])
		}

		var pageWidth float64
		if font.WMode == 1 {
			w1, _, _ := font.GetVerticalMetrics(charCode)
			wordSpace := 0.0
			if char == " " {
				wordSpace = state.Text.Tw
			}
			advance := (w1/1000.0)*fontSize + state.Text.Tc + wordSpace
			dy := advance * state.Text.Tm[3] * ctm[3]
			pageWidth = math.Abs(dy)
		} else {
			glyphWidth := font.GetGlyphWidth(charCode)
			wordSpace := 0.0
			if char == " " {
				wordSpace = state.Text.Tw
			}
			advance := ((glyphWidth/1000.0)*fontSize + state.Text.Tc + wordSpace) * (state.Text.Tz / 100.0)
			dx := advance * state.Text.Tm[0] * ctm[0]
			pageWidth = math.Abs(dx)
		}

		// The graphics state clipping path (W/W*) is intentionally not checked
		// during text extraction. Only the page CropBox is used to determine
		// visibility, because W/W* clips are often used for decorative masking
		// (e.g., rounded corners, image crops) that should not suppress text.
		// The CropBox-based IsClipped flag is set later in ExtractElements.

		// Compute text direction angle from the effective transform (Tm × CTM).
		// The text baseline direction (1,0) in text space maps to:
		//   dx = Tm[0]*CTM[0] + Tm[1]*CTM[2]
		//   dy = Tm[0]*CTM[1] + Tm[1]*CTM[3]
		dirX := tm[0]*ctm[0] + tm[1]*ctm[2]
		dirY := tm[0]*ctm[1] + tm[1]*ctm[3]
		angle := math.Atan2(dirY, dirX)

		*elements = append(*elements, models.TextElement{
			Text:          char,
			X:             x,
			Y:             y,
			Width:         pageWidth,
			FontSize:      effectiveFontSize,
			MCID:          mcid,
			Hidden:        isHidden,
			Invisible:     isInvisible,
			Vertical:      font.WMode == 1,
			Angle:         angle,
			PatternType:   state.FillPatternType,
			Language:      mcLang,
			ExpansionText: mcExpansion,
		})

		// Advance text matrix using exact glyph metrics.
		if font.WMode == 1 {
			// Vertical writing mode (PDF spec §9.4.4, Table 108):
			// Text position advances downward by w1 (vertical advance).
			w1, _, _ := font.GetVerticalMetrics(charCode)
			wordSpace := 0.0
			if char == " " {
				wordSpace = state.Text.Tw
			}
			advance := (w1/1000.0)*fontSize + state.Text.Tc + wordSpace
			// In vertical mode, advance along Tm's Y axis (column 1).
			state.Text.Tm[4] += advance * state.Text.Tm[2]
			state.Text.Tm[5] += advance * state.Text.Tm[3]
		} else {
			// Horizontal writing mode.
			glyphWidth := font.GetGlyphWidth(charCode)
			wordSpace := 0.0
			if char == " " {
				wordSpace = state.Text.Tw
			}
			advance := ((glyphWidth/1000.0)*fontSize + state.Text.Tc + wordSpace) * (state.Text.Tz / 100.0)
			// Translate Tm by advance along the text baseline.
			state.Text.Tm[4] += advance * state.Text.Tm[0]
			state.Text.Tm[5] += advance * state.Text.Tm[1]
		}

		data = data[n:]
	}
}

// decomposeLigature replaces Unicode ligature characters with their component
// ASCII characters so that text search and comparison works correctly.
func decomposeLigature(s string) string {
	if len(s) == 0 {
		return s
	}
	switch s {
	case "\uFB00": // ff
		return "ff"
	case "\uFB01": // fi
		return "fi"
	case "\uFB02": // fl
		return "fl"
	case "\uFB03": // ffi
		return "ffi"
	case "\uFB04": // ffl
		return "ffl"
	default:
		return s
	}
}
