package typography

import (
	"context"
	"math"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/models"
)

// processText maps the raw bytes of a PDF string to UTF-8 and generates
// [models.TextElement] values. It is a thin orchestrator: each per-glyph
// concern (decoding, MCID inheritance, visibility checks, glyph metrics,
// matrix advance) lives in its own helper so the main loop reads
// top-down without deeply nested branches.
func processText(ctx context.Context, str ast.PdfString, font *Font, machine *graphics.Machine, elements *[]models.TextElement) error {
	data := []byte(str)
	state := machine.Current()
	fontSize := state.Text.Tfs
	if fontSize == 0 {
		fontSize = 12 // Fallback
	}

	effectiveFontSize, spaceAdvance := computeTextScale(state, font, fontSize)
	mcid, mcLang, mcExpansion := currentMCIDFromStack(machine.MarkedContent)

	charCount := 0
	for len(data) > 0 {
		charCount++
		if charCount%500 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}

		char, n, skip := decodeNextChar(font, data)
		if skip {
			data = data[n:]
			continue
		}

		// Decompose standard ligatures to their component characters so
		// that text search and comparison works with plain ASCII.
		char = decomposeLigature(char)

		// Skip known non-glyph control characters (NUL, CR, LF, DEL).
		// Do NOT skip low byte values in general — PDF fonts frequently
		// use 0x01–0x1F as valid glyph codes (e.g. ligatures).
		if char == "?" && len(data) > 0 {
			if b := data[0]; b == 0x00 || b == 0x0a || b == 0x0d || b == 0x7f {
				data = data[n:]
				continue
			}
		}

		tm := state.Text.Tm
		ctm := state.CTM
		tx, ty := tm[4], tm[5]
		x := tx*ctm[0] + ty*ctm[2] + ctm[4]
		y := tx*ctm[1] + ty*ctm[3] + ctm[5]

		charCode := int(data[0])
		if n == 2 && len(data) >= 2 {
			charCode = int(data[0])<<8 | int(data[1])
		}

		pageWidth := computeGlyphPageWidth(state, font, fontSize, char, charCode)

		// Compute text direction angle from the effective transform
		// (Tm × CTM). The text baseline direction (1,0) in text space
		// maps to (Tm[0]*CTM[0]+Tm[1]*CTM[2], Tm[0]*CTM[1]+Tm[1]*CTM[3]).
		dirX := tm[0]*ctm[0] + tm[1]*ctm[2]
		dirY := tm[0]*ctm[1] + tm[1]*ctm[3]
		angle := math.Atan2(dirY, dirX)

		*elements = append(*elements, models.TextElement{
			Text:          char,
			X:             x,
			Y:             y,
			Width:         pageWidth,
			FontSize:      effectiveFontSize,
			SpaceWidth:    spaceAdvance,
			MCID:          mcid,
			Invisible:     textIsInvisible(state),
			Vertical:      font.WMode == 1,
			Angle:         angle,
			PatternType:   state.FillPatternType,
			Language:      mcLang,
			ExpansionText: mcExpansion,
		})

		advanceTextMatrix(state, font, fontSize, char, charCode)

		data = data[n:]
	}
	return nil
}

// computeTextScale returns the effective page-space font size and the
// font-specific space-character advance. These are invariants for the
// current string and only need to be computed once per [processText] call.
func computeTextScale(state *graphics.State, font *Font, fontSize float64) (effectiveFontSize, spaceAdvance float64) {
	// The text rendering matrix is Tm × CTM. The horizontal text
	// direction in page space is (Tm[0]*CTM[0]+Tm[1]*CTM[2],
	// Tm[0]*CTM[1]+Tm[1]*CTM[3]). Its magnitude × fontSize is the
	// effective page-space font size.
	ctm := state.CTM
	tm := state.Text.Tm
	dx := tm[0]*ctm[0] + tm[1]*ctm[2]
	dy := tm[0]*ctm[1] + tm[1]*ctm[3]
	scale := math.Sqrt(dx*dx + dy*dy)
	effectiveFontSize = fontSize * scale
	if effectiveFontSize == 0 {
		effectiveFontSize = fontSize // last resort: use raw
	}

	// Space advance in page-space units, used by ReconstructText for
	// font-aware space inference.
	spaceGlyphWidth := font.GlyphWidth(0x20)
	spaceAdvance = (spaceGlyphWidth / 1000.0) * fontSize * scale * (state.Text.Tz / 100.0)
	return effectiveFontSize, spaceAdvance
}

// currentMCIDFromStack walks the marked-content stack from innermost to
// outermost and returns the first MCID, language, and expansion text it
// finds. Used to inherit PDF/UA tagging metadata into the emitted
// [models.TextElement] values.
func currentMCIDFromStack(stack []graphics.MarkedContentState) (mcid int, lang, expansion string) {
	mcid = -1
	for i := len(stack) - 1; i >= 0; i-- {
		mc := &stack[i]
		if mcid == -1 && mc.MCID != -1 {
			mcid = mc.MCID
		}
		if lang == "" && mc.Language != "" {
			lang = mc.Language
		}
		if expansion == "" && mc.ExpansionText != "" {
			expansion = mc.ExpansionText
		}
	}
	return mcid, lang, expansion
}

// decodeNextChar maps the next glyph code in data to a UTF-8 character.
// Returns the decoded character, the number of bytes consumed, and a
// skip flag that tells the caller to continue past the consumed bytes
// without emitting an element (used for unmapped CID fallbacks that
// should not produce visible output).
func decodeNextChar(font *Font, data []byte) (char string, n int, skip bool) {
	var ok bool
	if font.ToUnicode != nil {
		n, char, ok = font.ToUnicode.Map(data)
	}
	if ok {
		return char, n, false
	}

	if n > 1 {
		// Multi-byte codespace consumed n>1 bytes but found no mapping.
		// For Type0 Identity-H fonts, treat the unmapped 2-byte code as
		// a direct Unicode codepoint.
		if font.Subtype == "Type0" && n == 2 && len(data) >= 2 {
			cp := rune(uint16(data[0])<<8|uint16(data[1])) + rune(font.CIDOffset) //nolint:gosec // CIDOffset is 0 or 29, no overflow
			if cp > 0x1F {
				return string(cp), n, false
			}
		}
		return "", n, true
	}

	// Single-byte fallback paths.
	if font.Subtype == "Type0" && font.CIDIdentityFallback {
		char = resolveGlyphFallback(font, data, &n)
	} else if r, ok := font.Encoding[data[0]]; ok {
		char = string(r)
	} else {
		char = resolveGlyphFallback(font, data, &n)
	}
	if n == 0 {
		n = 1
	}
	return char, n, false
}

// textIsInvisible combines the two rendering-mode-3 and white-on-white
// invisibility heuristics into a single predicate. Pattern color spaces
// are exempt because they paint with a tiled or gradient fill.
func textIsInvisible(state *graphics.State) bool {
	if state.Text.Tmode == 3 {
		return true
	}
	if state.FillPatternType != "" {
		return false
	}
	switch state.FillColorSpace {
	case "DeviceRGB":
		return len(state.FillColor) == 3 &&
			state.FillColor[0] == 1 && state.FillColor[1] == 1 && state.FillColor[2] == 1
	case "DeviceCMYK":
		return len(state.FillColor) == 4 &&
			state.FillColor[0] == 0 && state.FillColor[1] == 0 &&
			state.FillColor[2] == 0 && state.FillColor[3] == 0
	case "DeviceGray":
		return len(state.FillColor) == 1 && state.FillColor[0] == 1
	}
	return false
}

// computeGlyphPageWidth returns the advance width of a glyph in
// page-space units, including character spacing (Tc), word spacing
// (Tw when the glyph is a space), and horizontal scaling (Tz).
// Vertical and horizontal writing modes are handled separately.
func computeGlyphPageWidth(state *graphics.State, font *Font, fontSize float64, char string, charCode int) float64 {
	tm := state.Text.Tm
	ctm := state.CTM
	wordSpace := 0.0
	if char == " " {
		wordSpace = state.Text.Tw
	}
	if font.WMode == 1 {
		w1, _, _ := font.VerticalMetrics(charCode)
		advance := (w1/1000.0)*fontSize + state.Text.Tc + wordSpace
		awx := advance * (tm[2]*ctm[0] + tm[3]*ctm[2])
		awy := advance * (tm[2]*ctm[1] + tm[3]*ctm[3])
		return math.Sqrt(awx*awx + awy*awy)
	}
	glyphWidth := font.GlyphWidth(charCode)
	advance := ((glyphWidth/1000.0)*fontSize + state.Text.Tc + wordSpace) * (state.Text.Tz / 100.0)
	awx := advance * (tm[0]*ctm[0] + tm[1]*ctm[2])
	awy := advance * (tm[0]*ctm[1] + tm[1]*ctm[3])
	return math.Sqrt(awx*awx + awy*awy)
}

// advanceTextMatrix shifts the text matrix by the glyph's advance width
// after emitting its [models.TextElement]. Horizontal and vertical modes
// both use the same word-/character-spacing terms but push the
// displacement along different axes (PDF spec §9.4.4, Table 108).
func advanceTextMatrix(state *graphics.State, font *Font, fontSize float64, char string, charCode int) {
	wordSpace := 0.0
	if char == " " {
		wordSpace = state.Text.Tw
	}
	if font.WMode == 1 {
		w1, _, _ := font.VerticalMetrics(charCode)
		advance := (w1/1000.0)*fontSize + state.Text.Tc + wordSpace
		state.Text.Tm[4] += advance * state.Text.Tm[2]
		state.Text.Tm[5] += advance * state.Text.Tm[3]
		return
	}
	glyphWidth := font.GlyphWidth(charCode)
	advance := ((glyphWidth/1000.0)*fontSize + state.Text.Tc + wordSpace) * (state.Text.Tz / 100.0)
	state.Text.Tm[4] += advance * state.Text.Tm[0]
	state.Text.Tm[5] += advance * state.Text.Tm[1]
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

// resolveGlyphFallback resolves a character from font fallback tables when the
// Encoding table has no mapping. It modifies *n if a multi-byte code is consumed.
func resolveGlyphFallback(font *Font, data []byte, n *int) string {
	switch {
	case font.GIDToUnicode != nil:
		// Use the embedded TrueType/OpenType cmap to map the raw character
		// code (treated as a glyph ID) to a Unicode rune.
		// For CIDFonts (Type0), try 2-byte big-endian code first.
		if font.CIDWidths != nil && len(data) >= 2 {
			gid := uint16(data[0])<<8 | uint16(data[1])
			if r, ok := font.GIDToUnicode[gid]; ok {
				*n = 2
				return string(r)
			}
		}
		if r, ok := font.GIDToUnicode[uint16(data[0])]; ok {
			return string(r)
		}
		return "?"
	case font.CIDIdentityFallback && len(data) >= 2:
		// CIDToGIDMap /Identity without embedded font: treat the
		// 2-byte character code as a direct Unicode code point,
		// applying any detected CID offset.
		cp := rune(uint16(data[0])<<8|uint16(data[1])) + rune(font.CIDOffset) //nolint:gosec // CIDOffset is 0 or 29, no overflow
		*n = 2
		if cp > 0 {
			return string(cp)
		}
		return ""
	default:
		return "?"
	}
}
