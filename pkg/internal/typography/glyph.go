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
			// Multi-byte codespace consumed bytes without a mapping
			// and the caller opted out of emitting anything. Advance
			// the byte stream but leave the text matrix and elements
			// untouched.
			data = data[n:]
			continue
		}

		// Decompose standard ligatures to their component characters so
		// that text search and comparison works with plain ASCII.
		char = decomposeLigature(char)

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

		// Unresolved single-byte glyphs (char == "") still get a
		// models.TextElement so downstream layout code sees the
		// width they occupy. CoalesceElements drops them before the
		// public API returns, and ReconstructText treats them as
		// phantom anchors for the gap heuristic without emitting
		// characters or word boundaries.
		*elements = append(*elements, models.TextElement{
			Text:          char,
			X:             x,
			Y:             y,
			Width:         pageWidth,
			FontSize:      effectiveFontSize,
			SpaceWidth:    spaceAdvance,
			Chunk:         machine.Chunk,
			MCID:          mcid,
			Invisible:     textIsInvisible(state),
			Hidden:        textIsClipped(state, x, y),
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
	if font.SuppressExtraction {
		// No route to Unicode exists for this font (see
		// [Font.SuppressExtraction]), so any character would be invented.
		// Consume the codespace width without emitting.
		consume := min(len(data), 2)
		return "", consume, true
	}
	var ok bool
	if font.ToUnicode != nil {
		n, char, ok = font.ToUnicode.Map(data)
	}
	if ok {
		if n == 0 {
			// cmap.CMap.Map reports at least one byte for a match, so
			// this only fires if that contract ever breaks. Keep the
			// floor the fallback paths below apply: processText advances
			// by n and would not terminate on a zero.
			n = 1
		}
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
	// Type0 fonts whose /ToUnicode CMap declares a limited codespace
	// (for example <0086> <0374> covering only subset glyphs) return
	// n=0 when the input code falls outside that range. The font is
	// still Identity-H encoded, so two bytes belong together; consume
	// them and reveal the raw CID as a Unicode codepoint rather than
	// splitting across the WinAnsi single-byte path.
	if font.Subtype == "Type0" && font.ToUnicode != nil && n == 0 && len(data) >= 2 {
		cp := rune(uint16(data[0])<<8 | uint16(data[1]))
		if cp > 0x1F {
			return string(cp), 2, false
		}
	}

	// Single-byte fallback paths. Font.LigatureOverrides is checked
	// before the regular encoding map because composite /Differences
	// glyph names decompose to multi-rune strings that the byte→rune
	// encoding cannot represent.
	if s, ok := font.LigatureOverrides[data[0]]; ok {
		return s, 1, false
	}
	if font.Subtype == "Type0" && font.CIDIdentityFallback {
		char = resolveGlyphFallback(font, data, &n)
	} else if r, ok := type0TableRune(font, data); ok {
		char, n = string(r), 2
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

// type0TableRune looks a two-byte Type 0 code up in [Font.GIDToUnicode].
//
// A Type 0 font's codes select CIDs through its CMap (ISO 32000-1, 9.7.6.2),
// never through a simple font's single-byte encoding, so a code the table
// names reads as that character before the default WinAnsi map can split it
// and read each byte as Latin-1. A code the table does not name keeps the
// single-byte reading: under an embedded CMap stream a code can be one byte.
func type0TableRune(font *Font, data []byte) (rune, bool) {
	if font.Subtype != "Type0" || font.GIDToUnicode == nil || len(data) < 2 {
		return 0, false
	}
	r, ok := font.GIDToUnicode[uint16(data[0])<<8|uint16(data[1])]
	return r, ok
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

// textIsClipped reports whether a glyph drawn at (x, y) falls outside the
// clipping path the page put in force.
//
// It answers only when a W or W* operator took part: [graphics.NewMachine]
// seeds the clipping path with the page box, so testing the bounds alone would
// repeat what [models.TextElement.IsClipped] already says about the CropBox and
// report every page as clipping its overflow. The glyph origin is the test
// point, matching the CropBox check downstream and the viewers that drop a
// glyph whose text-matrix position lands outside.
func textIsClipped(state *graphics.State, x, y float64) bool {
	if !state.ClipApplied {
		return false
	}
	clip := state.ClippingPath
	return x < clip.MinX || x > clip.MaxX || y < clip.MinY || y > clip.MaxY
}

// computeGlyphPageWidth returns the painted advance width of a glyph in
// page-space units, EXCLUDING character spacing (Tc) and word spacing
// (Tw). Tc/Tw are inter-glyph adjustments and live in the gap between
// elements, not in the glyph itself. Capturing the post-glyph position
// before Tc/Tw applies lets downstream gap heuristics distinguish
// tracked text from real spaces.
//
// Horizontal scaling (Tz) is folded in because it scales the painted
// glyph itself, not the inter-glyph gap.
func computeGlyphPageWidth(state *graphics.State, font *Font, fontSize float64, _char string, charCode int) float64 {
	tm := state.Text.Tm
	ctm := state.CTM
	if font.WMode == 1 {
		w1, _, _ := font.VerticalMetrics(charCode)
		advance := (w1 / 1000.0) * fontSize
		awx := advance * (tm[2]*ctm[0] + tm[3]*ctm[2])
		awy := advance * (tm[2]*ctm[1] + tm[3]*ctm[3])
		return math.Sqrt(awx*awx + awy*awy)
	}
	glyphWidth := font.GlyphWidth(charCode)
	advance := (glyphWidth / 1000.0) * fontSize * (state.Text.Tz / 100.0)
	awx := advance * (tm[0]*ctm[0] + tm[1]*ctm[2])
	awy := advance * (tm[0]*ctm[1] + tm[1]*ctm[3])
	return math.Sqrt(awx*awx + awy*awy)
}

// advanceTextMatrix shifts the text matrix by the FULL per-glyph advance
// after emitting its [models.TextElement]. Per ISO 32000-1 §9.4.4, the
// advance is `(glyph_width × Tfs + Tc + Tw) × Th` (horizontal) or
// `glyph_width × Tfs + Tc + Tw` (vertical), Tw applying only when the
// glyph is a space. This is the spec-correct Tm update; it differs from
// [computeGlyphPageWidth] which excludes Tc/Tw from the painted Width
// reported to consumers.
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

// resolveGlyphFallback resolves a character from font fallback tables
// when the Encoding table has no mapping. It modifies *n if a
// multi-byte code is consumed. An empty string signals "no mapping
// available"; callers treat that as an unresolved glyph and drop it
// from emitted text rather than printing a "?" placeholder that would
// surface in extracted content.
func resolveGlyphFallback(font *Font, data []byte, n *int) string {
	if font.GIDToUnicode != nil && font.CIDWidths != nil && len(data) >= 2 {
		// CIDFonts (Type0) feed 2-byte CIDs to the embedded font's
		// GID→Unicode cmap. Single-byte simple fonts must not follow
		// this path: their byte codes are char codes that need to go
		// through Font.Encoding → glyph-name lookups, and treating
		// those bytes as raw GIDs resurrects ghost glyphs (e.g. byte
		// 0x0D, which WinAnsiEncoding leaves unmapped, would resolve
		// to whichever glyph happens to live at GID 13 in the embedded
		// face: "*" for Arial, producing stray asterisks in running
		// text).
		gid := uint16(data[0])<<8 | uint16(data[1])
		if r, ok := font.GIDToUnicode[gid]; ok {
			*n = 2
			return string(r)
		}
		// Fall through to CIDIdentityFallback when a non-embedded font
		// overlays a partial GID table via stdfonts: the table lists
		// only the handful of extended-Latin positions that don't
		// follow CID + 29, so GIDs missing from it should still go
		// through the offset path instead of resolving to empty.
	}
	if font.CIDIdentityFallback && len(data) >= 2 {
		// CIDToGIDMap /Identity without embedded font: treat the
		// 2-byte character code as a direct Unicode code point,
		// applying any detected CID offset.
		cp := rune(uint16(data[0])<<8|uint16(data[1])) + rune(font.CIDOffset) //nolint:gosec // CIDOffset is 0 or 29, no overflow
		*n = 2
		if cp > 0 {
			return string(cp)
		}
		return ""
	}
	return ""
}
