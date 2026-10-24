package typography

import (
	"context"
	"math"
	"unicode"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/models"
)

// processText maps the raw bytes of a PDF string to UTF-8 and generates
// [models.TextElement] values. It is a thin orchestrator: each per-glyph
// concern (decoding, MCID inheritance, visibility checks, glyph metrics,
// matrix advance) lives in its own helper so the main loop reads
// top-down without deeply nested branches. When runs is not nil it records
// there, for each glyph the font gives no advance, the advance the embedded
// program gives it on the page ([recordProgramGapWidths]), for gap
// measurement.
//
// # Corpus evidence
//
//   - govdocs thread_000_000578.pdf: footnote marks set with 4.9 Ts read
//     glued to the word before them while the rise was ignored.
//   - All corpora: honouring the rise alone makes words match PDF.js on 7
//     more files and on none fewer.
func processText(ctx context.Context, str ast.PdfString, font *Font, machine *graphics.Machine, elements *[]models.TextElement, runs *runRecorder) error {
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

		char, n, skip, standIn := decodeNextCode(font, data)

		charCode := int(data[0])
		if n == 2 && len(data) >= 2 {
			charCode = int(data[0])<<8 | int(data[1])
		}
		// Metrics are keyed by CID, which the encoding CMap gives the code
		// (ISO 32000-1, 9.7.4.3 and 9.7.6.2); see [Font.metricCID].
		cid := font.metricCID(data[:min(n, len(data))], charCode)

		aligned := font.codeAligned(data, n)
		if skip && !placesUnmappedCode(font, aligned) {
			// No glyph is placed for the code, but it still moves the text
			// position by the width read for the bytes taken, as any shown
			// code does (ISO 32000-1, 9.4.4).
			before := state.Text.Tm
			advanceTextMatrix(state, font, fontSize, n, charCode, cid)
			machine.RecordGlyphAdvance(graphics.GlyphAdvance{
				Bytes: n, Aligned: aligned,
				Before: before, After: state.Text.Tm, RenderingMode: state.Text.Tmode,
			})
			data = data[n:]
			continue
		}
		if skip {
			// A glyph with no character, placed as an unresolved
			// single-byte glyph is ([placesUnmappedCode]).
			char, standIn = "", false
		}

		// Decompose standard ligatures to their component characters so
		// that text search and comparison works with plain ASCII.
		char = decomposeLigature(char)
		if standIn && machine.MarkUnvaluedText {
			char = string(unicode.ReplacementChar)
		}

		tm := state.Text.Tm
		ctm := state.CTM
		// The text rendering matrix of ISO 32000-1, 9.4.4 places the
		// glyph origin at (0, Trise) in text space, so a rise set by Ts
		// (9.3.7) lifts a superscript off the baseline it was shown on.
		rise := state.Text.Ts
		tx, ty := rise*tm[2]+tm[4], rise*tm[3]+tm[5]
		x := tx*ctm[0] + ty*ctm[2] + ctm[4]
		y := tx*ctm[1] + ty*ctm[3] + ctm[5]

		pageWidth := computeGlyphPageWidth(state, font, fontSize, char, cid)

		// Compute text direction angle from the effective transform
		// (Tm × CTM). The text baseline direction (1,0) in text space
		// maps to (Tm[0]*CTM[0]+Tm[1]*CTM[2], Tm[0]*CTM[1]+Tm[1]*CTM[3]),
		// and the glyph advances along it or against it as Tfs × Th has
		// it ([advancesBackward]).
		dirX := tm[0]*ctm[0] + tm[1]*ctm[2]
		dirY := tm[0]*ctm[1] + tm[1]*ctm[3]
		if advancesBackward(state, font, fontSize) {
			dirX, dirY = -dirX, -dirY
		}
		angle := math.Atan2(dirY, dirX)

		// Unresolved single-byte glyphs (char == "") still get a
		// models.TextElement so downstream layout code sees the
		// width they occupy. CoalesceElements drops them before the
		// public API returns, and ReconstructText treats them as
		// phantom anchors for the gap heuristic: they emit no
		// character, and the word gaps around them still read as spaces.
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
			InkRight:      glyphsInkRight(state, font),
			InkBox:        glyphInkBox(ctx, machine, font, fontSize, cid, x, y),
			PatternType:   state.Fill.PatternType,
			Language:      mcLang,
			ExpansionText: mcExpansion,
		})
		if machine.RecordTextPaints || machine.RecordWhiteFills {
			machine.RecordTextPaint(graphics.TextPaint{
				Mode:        state.Text.Tmode,
				Fill:        state.Fill,
				Stroke:      state.Stroke,
				FillAlpha:   state.FillAlpha,
				StrokeAlpha: state.StrokeAlpha,
				BlendMode:   state.BlendMode,
				SoftMask:    state.SoftMask != nil,
				Intent:      state.RenderingIntent,
				Resources:   machine.Resources,
				Bold:        font.Bold,
				Type3:       font.Subtype == "Type3",
				WhiteFill:   state.Text.Tmode != 3 && fillIsWhite(state),
				PaintsBefore: [3]int{
					len(machine.PathBoxes()), len(machine.ImageBoxes()), len(machine.ShadingBoxes()),
				},
			})
		}
		if runs != nil && pageWidth == 0 && font.WMode != 1 {
			if w, ok := font.programGapWidth(cid); ok {
				runs.recordGapWidth(len(*elements)-1, glyphPageAdvance(state, w, fontSize))
			}
		}

		before := state.Text.Tm
		advanceTextMatrix(state, font, fontSize, n, charCode, cid)
		machine.RecordGlyphAdvance(graphics.GlyphAdvance{
			Bytes: n, Aligned: aligned, Emitted: true,
			Before: before, After: state.Text.Tm, RenderingMode: state.Text.Tmode,
		})

		data = data[n:]
	}
	return nil
}

// placesUnmappedCode reports whether processText places a glyph, as an
// element with no character, for a code [decodeNextCode] reads no character
// for. Such a code still selects a glyph, the .notdef glyph at worst
// (ISO 32000-1, 9.7.6.3), whose width the CIDFont's /W and /DW state
// (9.7.4.3), so it is drawn and advances the text position as any glyph
// does (9.4.4). PDF.js advances every code it shows by its width, whatever
// character it reads for it (src/core/evaluator.js, buildTextContentItem).
//
// The glyph is placed only when both its extent and its length are known:
// aligned reports that the code was split where the font's encoding splits
// it ([Font.codeAligned]), and a Type 0 font must name a CIDFont dictionary
// ([Font.cidFontMetrics]), without which 9.7.4.3 states no width. A code
// either leaves out still advances, by the width litho reads for the bytes
// taken, and is recorded unemitted, so a redaction keeping it refuses
// rather than trust a position no viewer is bound to share.
//
// # Corpus evidence
//
//   - litho cmapusecmap_2b.pdf, cmapverabase_2b.pdf, usecmap_2u.pdf: codes
//     a /ToUnicode CMap maps to nothing, or an empty one leaves unmapped, are
//     placed, and a region over a quarter of their page, or a band through
//     it, no longer refuses.
//   - litho cmaps_2b.pdf, type0nodesc_2b.pdf: a CMap name outside Table 118
//     and Type 0 fonts with no CIDFont stay unplaced, and redacting their
//     page still refuses.
//   - govdocs thread_300_300034.pdf pages 29 and 30: the unmapped <0004>
//     codes inside "<" and ">" left gaps read as word spaces, "< Population
//     Viability >". Placed, they cover their width, and the line reads
//     "<Population Viability>", as PDF.js reads it.
//   - litho fontcopies_2b.pdf page 16: a line of one unmapped code reads as
//     an empty line, where PDF.js reads a line too.
//   - All corpora: placing the codes makes no other file of the six text
//     corpora read other text, and changes the redaction outcome of no page
//     of pdfjs, govdocs or pdfcpu.
func placesUnmappedCode(font *Font, aligned bool) bool {
	return aligned && (font.Subtype != "Type0" || font.cidFontMetrics)
}

// advancesBackward reports whether a horizontal glyph advances against the
// text-space X axis: ISO 32000-1, 9.4.4 gives its horizontal displacement as
// tx = ((w0 - Tj/1000) × Tfs + Tc + Tw) × Th, so a negative font size or a
// negative horizontal scaling, one of the two and not both, runs the glyphs
// the other way. The rendering matrix then turns the glyphs a half turn
// (both its diagonal terms negative) or mirrors them, and the text reads
// along the reversed axis. PDF.js reads the direction off the same product,
// the first column of Tfs × Th × Tm × CTM (src/core/evaluator.js,
// getCurrentTextTransform and compareWithLastPosition). A vertical font's
// advance takes no Th and keeps the direction its metrics give it.
//
// A Type 3 glyph's width is in glyph space, which /FontMatrix maps to text
// space (9.6.5, Table 112), so a matrix with a negative X scale gives w0 the
// other sign and runs the glyphs the other way too ([type3XSign]).
//
// # Corpus evidence
//
//   - pdfjs issue14415.pdf: words set at -9 to -15 Tf on a page turned by
//     /Rotate 180 advance leftward.
//   - pdfcpu pkg_testdata__Paclitaxel.pdf: -12 Tf with -100 Tz under a CTM
//     that flips Y advances rightward, so the font size alone does not give
//     the direction.
//   - All corpora: no Type 3 font a page shows sets a negative X scale in its
//     /FontMatrix, so reading its sign moves no file's text.
func advancesBackward(state *graphics.State, font *Font, fontSize float64) bool {
	if font.WMode == 1 {
		return false
	}
	return fontSize*state.Text.Tz*type3XSign(font) < 0
}

// type3XSign returns -1 for a Type 3 font whose /FontMatrix maps the glyph
// space X axis onto the negative text space X axis (ISO 32000-1, 9.6.5), and
// 1 for any other font: the sign that matrix gives a glyph's advance and the
// reflection it gives the glyph along the baseline.
func type3XSign(font *Font) float64 {
	if font.Subtype == "Type3" && font.FontMatrix[0] < 0 {
		return -1
	}
	return 1
}

// glyphsInkRight reports whether the glyphs font paints in state ink the
// right of their advance in user space, which the advance direction alone
// cannot tell: a negative Th reverses the advance and reflects the glyph,
// while a negative Tfs reverses the advance and turns the glyph a half turn.
//
// The text rendering matrix of ISO 32000-1, 9.4.4, [Tfs×Th 0 0 Tfs 0 Trise]
// × Tm × CTM, has the determinant Th × Tfs² × det(Tm) × det(CTM), and Tfs²
// is never negative, so the sign of the other three factors says whether it
// reflects text space. Every font but Type 3 inks above the text-space
// baseline, which a reflection carries to the right of the advance. A Type 3
// font may draw below that baseline instead ([Font.type3InkBelow]), which
// crosses the ink to the other side once more, and its /FontMatrix may
// reverse the X axis, which reflects the glyph as a negative Th does
// ([type3XSign]).
//
// # Corpus evidence
//
//   - pdfjs bug1199237.pdf: "Éditorial" is drawn a second time through a Tm
//     of [1 0 0 -1], a reflection hanging below the word.
//   - govdocs thread_400_400243.pdf: a Type 3 font with an identity
//     /FontMatrix shown through a Tm that flips Y inks below the baseline;
//     rendered with poppler, 635 of the 657 runs whose side the rendering
//     settles have their ink there.
//   - pdfjs bug1001080.pdf, bug1245391_reduced.pdf: Type 3 bitmap fonts
//     whose /FontMatrix flips Y and whose d1 boxes lie below the glyph-space
//     baseline render upright above the text baseline. Reading the
//     reflection off the matrices alone put their ink on the wrong side.
func glyphsInkRight(state *graphics.State, font *Font) bool {
	tm, ctm := state.Text.Tm, state.CTM
	reflected := state.Text.Tz*type3XSign(font)*(tm[0]*tm[3]-tm[1]*tm[2])*(ctm[0]*ctm[3]-ctm[1]*ctm[2]) < 0
	below := font.Subtype == "Type3" && font.type3InkBelow
	return reflected != below
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
	spaceGlyphWidth := font.GlyphWidth(font.metricCID([]byte{0x20}, 0x20))
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

// spaceCharCode is the single-byte character code ISO 32000-1, 9.3.3 applies
// word spacing to: the word space of a simple font, whatever glyph it selects.
const spaceCharCode = 0x20

// decodeNextCode maps the next glyph code in data to a UTF-8 character.
// Returns the decoded character, the number of bytes consumed, and a
// skip flag reporting a code that reads as no character (unmapped CID
// fallbacks that should not produce visible output). The code still draws a
// glyph and advances the text position: [placesUnmappedCode] says whether
// the caller places that glyph.
//
// standIn reports a code of a Type 0 font that no mapping gives a character,
// so ISO 32000-1, 9.10.2 gives it no Unicode value: char is then the code's
// own value, the stand-in PDF.js reads for it, or nothing when the font's
// encoding CMap is one litho holds whole and the code selects .notdef
// ([codeSelectsNotdef]).
//
// A single-byte code no table maps reads as nothing, unless it is code 32:
// ISO 32000-1, 9.3.3 applies word spacing to the single-byte code 32
// whatever glyph it selects, so the specification itself names that code
// the word space of a simple font, and it reads as a space glyph. No other
// code reads as a space for want of a mapping. PDF.js shows an unmapped
// glyph as its code (src/core/fonts.js, _charToGlyph, String.fromCharCode)
// and so reads codes 0x09 to 0x0D and 0xA0 as word spaces too, but nothing
// in the font says such a glyph is blank: TeX text fonts set the "fi" and
// "fl" ligatures there. A glyph name that does say so, "space" or
// "nbspace" in a /Differences array or a program's encoding, already maps
// the code to a space and never reaches this fallback. Words that read
// right over agreement with the reference is a user decision (2026-10-19).
//
// # Corpus evidence
//
//   - govdocs thread_200_200612.pdf, thread_200_200408.pdf: TeX text fonts
//     set "fi" at code 0x0C and "fl" at 0x0D with no glyph name litho or
//     PDF.js maps. PDF.js reads "brie y" and "Veri cation"; litho keeps the
//     words whole, "briey" and "Verication", by decision.
//   - pdfjs bug810214.pdf, TAMReview.pdf, govdocs thread_400_400235.pdf,
//     thread_400_400786.pdf and 18 more: fonts that give codes 0x09 to 0x0D
//     glyph names no table maps, which PDF.js reads as word spaces. Reading them so would cut the space disagreements with
//     PDF.js on files whose characters match from 7160 to 2915; litho
//     keeps them as glyphs with no character by decision.
//   - govdocs thread_400_400235.pdf, thread_400_400786.pdf, pdfjs
//     TAMReview.pdf: subset fonts whose /Differences arrays name their glyphs
//     by index ("g55", "G40") give code 32 a name no table maps. The rule
//     reads those codes as the word spaces they separate; it reaches no
//     other file of the five text corpora.
//   - pdfjs outlines_se.pdf: Identity-H fonts whose /ToUnicode declares
//     <00> to <FF> one byte long and <0100> to <FFFF> two, then maps <0029>
//     and the other two-byte codes below <0100>. Split by that codespace,
//     "Hello" read "⤀䘀䴀䴀P". No other file of the six text corpora moves.
//   - govdocs thread_300_300034.pdf pages 29 and 30: <0004> codes under a
//     /ToUnicode whose only codespace range is <0006>, read a byte at a
//     time, put "ЀЀЀ" around "Population Viability".
//   - pdfjs issue18208.pdf: simple fonts whose /ToUnicode CMaps declare a
//     two-byte codespace read their codes two at a time, and the first 28
//     lines of page 1 read "R", "a", "F" where PDF.js reads "Pág. 1/1",
//     "Data de Vencimento". Read a byte at a time they read as PDF.js reads
//     them, and redacting a quarter of page 1, or a band through it, no
//     longer refuses. No other file of the six text corpora moves.
//   - pdfjs issue18208.pdf: the same CMaps map each shown code by its
//     two-byte value, <0050> for "P". Looked up by value, 459 codes read
//     through the CMap that read through the encoding before, to the same
//     characters. No file of the seven corpora shows a byte pair its simple
//     font's /ToUnicode maps; litho tounicodepair_2u.pdf plants one, and read
//     two bytes a mapped pair it read "HeXYworld" for "Hello world".
func decodeNextCode(font *Font, data []byte) (char string, n int, skip, standIn bool) {
	if font.SuppressExtraction {
		// No route to Unicode exists for this font (see
		// [Font.SuppressExtraction]), so any character would be invented.
		// Consume the codespace width without emitting.
		consume := min(len(data), 2)
		return "", consume, true, false
	}
	var ok bool
	if font.ToUnicode != nil && font.identityEncoding && len(data) >= 2 {
		// The font's encoding CMap splits the string into codes (ISO
		// 32000-1, 9.7.6.2), two bytes each under Identity-H and
		// Identity-V (9.7.5.2, Table 118), and /ToUnicode maps those
		// codes (9.10.3). An entry for the two-byte code reads even when
		// the CMap's own codespace ranges leave the code out, as PDF.js
		// reads it (src/core/evaluator.js, readToUnicode, keys its map by
		// the code's value): split by that codespace, the code would
		// break into bytes that shift every code after it.
		if s, found := font.ToUnicode.Mapping[string(data[:2])]; found {
			return s, 2, false, false
		}
	}
	if font.ToUnicode != nil && font.Subtype != "Type0" && !font.gbkCodes {
		// A simple font's codes are one byte each (ISO 32000-1, 9.6.1), so
		// a /ToUnicode CMap whose codespace ranges are wider does not make
		// two bytes one code, mapped or not: taken together, the second
		// code drew no glyph and moved nothing, and the line read one glyph
		// in two. An unmapped byte takes the single-byte paths below. A
		// font read through GBK-EUC-H ([Font.gbkCodes]) keeps its two-byte
		// codes, the leniency [isChineseFaceName] records.
		if s, found := font.ToUnicode.MapByte(data[0]); found {
			return s, 1, false, false
		}
	} else if font.ToUnicode != nil {
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
		return char, n, false, false
	}

	if font.Subtype == "Type0" && font.GIDToUnicode != nil {
		if code, ok := font.nextEncodingCode(data); ok {
			return encodingCIDTableChar(font, code)
		}
	}

	if n > 1 && font.Subtype != "Type0" {
		// Only a font read through GBK-EUC-H gets here. A two-byte code
		// that reading leaves unmapped takes the single-byte paths below,
		// as every other simple font's code does (ISO 32000-1, 9.6.1).
		n = 0
	}
	if n > 1 {
		// Multi-byte codespace consumed n>1 bytes but found no mapping.
		// For Type0 Identity-H fonts, treat the unmapped 2-byte code as
		// a direct Unicode codepoint.
		if font.Subtype == "Type0" && n == 2 && len(data) >= 2 {
			cp := rune(uint16(data[0])<<8|uint16(data[1])) + rune(font.CIDOffset) //nolint:gosec // CIDOffset is 0 or 29, no overflow
			if cp > 0x1F {
				return string(cp), n, false, true
			}
		}
		return "", n, true, false
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
			return string(cp), 2, false, true
		}
		// A value in the C0 control range names no character, as an
		// unmapped two-byte code does under a /ToUnicode CMap above. The
		// two bytes stay one code: read one byte at a time, they shift
		// every code after them.
		if font.identityEncoding {
			return "", 2, true, false
		}
	}

	// Single-byte fallback paths. Font.LigatureOverrides is checked
	// before the regular encoding map because composite /Differences
	// glyph names decompose to multi-rune strings that the byte→rune
	// encoding cannot represent.
	if s, ok := font.LigatureOverrides[data[0]]; ok {
		return s, 1, false, false
	}
	if font.Subtype == "Type0" && font.CIDIdentityFallback {
		char, standIn = resolveGlyphFallback(font, data, &n)
	} else if r, ok := type0TableRune(font, data); ok {
		char, n = string(r), 2
	} else if r, ok := font.Encoding[data[0]]; ok {
		char = string(r)
	} else {
		char, standIn = resolveGlyphFallback(font, data, &n)
	}
	if n == 0 {
		n = 1
	}
	if char == "" && n == 1 && data[0] == spaceCharCode {
		// ISO 32000-1, 9.3.3 makes the single-byte code 32 the word space
		// of a simple font, whatever glyph it selects.
		char, standIn = " ", false
	}
	return char, n, false, standIn
}

// encodingCIDTableChar reads code, which a Type 0 font's encoding CMap splits
// off a shown string ([Font.nextEncodingCode]), through the CID-keyed
// [Font.GIDToUnicode]: the character collection's UCS2 CMap, or the
// /CIDToGIDMap stream composed with the embedded program's cmap (ISO 32000-1,
// 9.10.2). A CID the table does not name reads as the code's own value, the
// identity reading PDF.js gives a composite font's code it has no character
// for (evaluator.js, buildToUnicode, and fonts.js, _charToGlyph), and a value
// in the C0 control range emits nothing, as an unmapped two-byte code does
// under a /ToUnicode CMap. A code that selects the .notdef glyph
// ([codeSelectsNotdef], 9.7.6.3) names no character: it advances as a glyph
// and emits none. standIn is as [decodeNextCode] reports it.
//
// # Corpus evidence
//
//   - pdfjs issue11768_reduced.pdf: shows <E0E0E0> under a UTF-8 encoding
//     CMap whose codespace holds no such code byte by byte. Read at the code
//     litho emitted "àààcm", and read through the cidchar <E0> the CMap
//     writes outside its codespace "---cm"; it now emits "cm". PDF.js
//     compares codespaces by value, reads the three bytes as one code it has
//     no character for, and emits U+E0E0 then "cm".
//   - All corpora: no other file's text changes, and no validation or
//     remediation result moves on the 112 files whose Type 0 fonts name a
//     non-Identity encoding.
//   - All corpora: a code a mapping sends to CID 0 reaches no file of the
//     six text corpora.
func encodingCIDTableChar(font *Font, code encodingCode) (char string, consumed int, skip, standIn bool) {
	if codeSelectsNotdef(code) {
		return "", code.n, false, !code.mapped && !code.undefined
	}
	if code.cid >= 0 && code.cid <= math.MaxUint16 {
		if r, ok := font.GIDToUnicode[uint16(code.cid)]; ok { //nolint:gosec // code.cid is bounded to a uint16 above.
			return string(r), code.n, false, false
		}
	}
	if code.value > 0x1F && code.value <= unicode.MaxRune {
		return string(rune(code.value)), code.n, false, true
	}
	return "", code.n, true, false
}

// codeSelectsNotdef reports whether code, split off a shown string by a Type 0
// font's encoding CMap ([Font.nextEncodingCode]), selects the .notdef glyph,
// CID 0, which names no character (ISO 32000-1, 9.7.6.3): bytes no codespace
// range holds, a code a mapping of the CMap sends to CID 0, and a code no
// cidchar or cidrange covers in a CMap litho holds whole, its bases
// included, which 9.7.6.3 sends to the .notdef character: CID 0, or the CID
// a notdef mapping gives it. A code a base litho cannot read might map keeps
// the reading PDF.js gives it, the code itself.
//
// # Corpus evidence
//
//   - litho cmapnotdef_2b.pdf, cmapverabase_2b.pdf: codes a notdef range or
//     no mapping covers read "C", "Ő" and "娠", their own values; they now
//     read as nothing. No file of the five text corpora changes, and both
//     pages stay unscored.
func codeSelectsNotdef(code encodingCode) bool {
	return code.undefined || code.mapped && code.cid == 0 || !code.mapped && code.closed
}

// type0TableRune looks a two-byte Type 0 code up in [Font.GIDToUnicode].
//
// A Type 0 font's codes select CIDs through its CMap (ISO 32000-1, 9.7.6.2),
// never through a simple font's single-byte encoding, so a code the table
// names reads as that character before the default WinAnsi map can split it
// and read each byte as Latin-1. A code the table does not name keeps the
// single-byte reading: under an embedded CMap stream a code can be one byte.
// A font whose encoding CMap gives its codes CIDs ([Font.encodingCIDKeyed])
// never reaches it: [decodeNextCode] reads its table at those CIDs first.
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
//
// White is read only in a device space the content stream named directly
// (ISO 32000-1, 8.6.8, Table 74): a /ColorSpace resource holding a device
// family is left visible, as is any other space, whose white depends on a
// conversion this predicate does not run.
func textIsInvisible(state *graphics.State) bool {
	return state.Text.Tmode == 3 || fillIsWhite(state)
}

// fillIsWhite reports the white-on-white half of [textIsInvisible]: a fill of
// white in a device space the content stream named directly. It judges the
// colour alone. What lies under the glyph is a renderer's to tell, and a
// caller holding one overturns the judgement where the glyph shows (see
// [graphics.TextPaint.WhiteFill]).
func fillIsWhite(state *graphics.State) bool {
	fill := state.Fill
	if fill.Space.Resource != "" {
		return false
	}
	c := fill.Components
	switch fill.Space.Family {
	case graphics.FamilyDeviceRGB:
		return len(c) == 3 && c[0] == 1 && c[1] == 1 && c[2] == 1
	case graphics.FamilyDeviceCMYK:
		return len(c) == 4 && c[0] == 0 && c[1] == 0 && c[2] == 0 && c[3] == 0
	case graphics.FamilyDeviceGray:
		return len(c) == 1 && c[0] == 1
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
func computeGlyphPageWidth(state *graphics.State, font *Font, fontSize float64, _char string, cid int) float64 {
	tm := state.Text.Tm
	ctm := state.CTM
	if font.WMode == 1 {
		w1, _, _ := font.VerticalMetrics(cid)
		advance := (w1 / 1000.0) * fontSize
		awx := advance * (tm[2]*ctm[0] + tm[3]*ctm[2])
		awy := advance * (tm[2]*ctm[1] + tm[3]*ctm[3])
		return math.Sqrt(awx*awx + awy*awy)
	}
	return glyphPageAdvance(state, font.GlyphWidth(cid), fontSize)
}

// glyphPageAdvance returns, in page-space units, the horizontal advance of a
// glyph glyphWidth 1/1000 text space units wide shown at fontSize, scaled by
// Th and carried through Tm and the CTM, as [computeGlyphPageWidth] measures
// it.
func glyphPageAdvance(state *graphics.State, glyphWidth, fontSize float64) float64 {
	tm := state.Text.Tm
	ctm := state.CTM
	advance := (glyphWidth / 1000.0) * fontSize * (state.Text.Tz / 100.0)
	awx := advance * (tm[0]*ctm[0] + tm[1]*ctm[2])
	awy := advance * (tm[0]*ctm[1] + tm[1]*ctm[3])
	return math.Sqrt(awx*awx + awy*awy)
}

// advanceTextMatrix shifts the text matrix by the FULL per-glyph advance
// after emitting its [models.TextElement]. Per ISO 32000-1 §9.4.4, the
// advance is `(glyph_width × Tfs + Tc + Tw) × Th` (horizontal) or
// `glyph_width × Tfs + Tc + Tw` (vertical). This is the spec-correct Tm
// update; it differs from [computeGlyphPageWidth] which excludes Tc/Tw
// from the painted Width reported to consumers.
//
// Tw applies to the single-byte character code 32 alone, whatever glyph
// or character it selects (ISO 32000-1, 9.3.3): a two-byte code that maps
// to a space, as Identity-H fonts give, takes no word spacing. codeLen is
// the byte length of the code, charCode its value, and cid the key its
// metrics are read by ([Font.metricCID]).
//
// # Corpus evidence
//
//   - pdfjs issue6127.pdf: an Identity-H Times New Roman sets Tw -0.35
//     at 12 pt and shows its spaces as <0003>. Applying Tw to them pulled
//     every word 4.2 points back over the space before it.
func advanceTextMatrix(state *graphics.State, font *Font, fontSize float64, codeLen, charCode, cid int) {
	wordSpace := 0.0
	if codeLen == 1 && charCode == ' ' {
		wordSpace = state.Text.Tw
	}
	if font.WMode == 1 {
		w1, _, _ := font.VerticalMetrics(cid)
		advance := (w1/1000.0)*fontSize + state.Text.Tc + wordSpace
		state.Text.Tm[4] += advance * state.Text.Tm[2]
		state.Text.Tm[5] += advance * state.Text.Tm[3]
		return
	}
	glyphWidth := font.GlyphWidth(cid)
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
// surface in extracted content. standIn reports a character that is the
// code's own value read as a code point ([decodeNextCode]).
func resolveGlyphFallback(font *Font, data []byte, n *int) (char string, standIn bool) {
	if font.GIDToUnicode != nil && (font.Subtype == "Type0" || font.CIDWidths != nil) && len(data) >= 2 {
		// CIDFonts (Type0) feed 2-byte CIDs to the embedded font's
		// GID→Unicode cmap, or to the glyph table of a non-embedded
		// standard face, whether or not the CIDFont states /W, which
		// ISO 32000-1, Table 117 makes optional. Single-byte simple
		// fonts must not follow this path: their byte codes are char
		// codes that need to go
		// through Font.Encoding → glyph-name lookups, and treating
		// those bytes as raw GIDs resurrects ghost glyphs (e.g. byte
		// 0x0D, which WinAnsiEncoding leaves unmapped, would resolve
		// to whichever glyph happens to live at GID 13 in the embedded
		// face: "*" for Arial, producing stray asterisks in running
		// text).
		gid := uint16(data[0])<<8 | uint16(data[1])
		if r, ok := font.GIDToUnicode[gid]; ok {
			*n = 2
			return string(r), false
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
			return string(cp), true
		}
		return "", false
	}
	return "", false
}
