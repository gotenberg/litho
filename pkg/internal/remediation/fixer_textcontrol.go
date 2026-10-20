package remediation

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/typography"
)

// matchTextControl matches ISO 19005-2 §6.2.11.8 and ISO 19005-4
// §6.2.10.9 violations: text-showing operators render the .notdef
// glyph because the character code resolves to a missing or undefined
// entry in the font's charset.
func matchTextControl(v compliance.Violation) bool {
	if !clausePrefix(v, "6.2.10.9", "6.2.11.8", "8.4.5.9", "7.21.8") {
		return false
	}
	return descriptionContains(v, ".notdef") || contextContains(v, ".notdef")
}

// fixTextControl strips ASCII control bytes (0x00-0x1F excluding TAB,
// LF, CR) from every text-showing operator in every page's content
// stream. These bytes typically map to .notdef in standard font
// encodings and are almost never intended glyph references; removing
// them eliminates the violation without affecting legitimate text.
func fixTextControl(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, opts Options, _ []compliance.Violation, _ string) ([]string, error) {
	// Dropping bytes from Tj/TJ operands removes whatever glyph those
	// codes mapped to. That is the standard .notdef-reference fix, but
	// it is still content loss; gate behind PreserveContent.
	if opts.PreserveContent {
		return nil, nil
	}
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("resolving catalog: %w", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		return nil, fmt.Errorf("resolving pages: %w", err)
	}

	// Pages share font objects, and describing a font parses its program.
	// Resolve each font object once per document.
	fontCache := make(map[int]textFontInfo)

	total := 0
	entries := pageTreeEntries(ctx, alloc, doc, pages)
	sharing := newContentSharing(entries)
	for pageNum, page := range pages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		data, contentsObj, err := livePageContentData(ctx, alloc, doc, page)
		if err != nil || contentsObj == nil {
			continue
		}
		fonts := pageTextFonts(ctx, doc, pageResourceCategory(ctx, alloc, doc, entries[pageNum], "Font"), fontCache)
		newData, removed, err := repairContentOps(ctx, data, func() opRepair {
			return (&controlByteStripper{fonts: fonts}).repair
		})
		if err != nil {
			doc.Logger.Debug("remediation: cannot rewrite a content stream for its control bytes",
				"page", pageNum+1, "error", err)
			continue
		}
		if removed == 0 {
			continue
		}
		if err := replaceContentStream(ctx, alloc, doc, sharing, contentsObj, newData, pageNum+1); err != nil {
			return nil, fmt.Errorf("replacing content stream on page %d: %w", pageNum+1, err)
		}
		total += removed
	}
	if total == 0 {
		return nil, nil
	}
	return []string{fmt.Sprintf("stripped %d control-byte(s) from text operators", total)}, nil
}

// controlByteStripper is the repair of [fixTextControl] for the operations of
// one content stream, handed to it in stream order ([opRepair]): it removes
// the bytes in the ASCII control range 0x00-0x1F, but TAB, LF and CR, from the
// strings every text-showing operator (Tj, TJ, ' and ") shows, and drops an
// operator whose string it empties, keeping the line move and spacing a ' or "
// sets and the position moves a TJ's numbers make (see [emptiedShowEffect]).
// A glyph it removes keeps the room it took on the line (see
// [controlByteStripper.keepAdvances]). Every byte it removes counts as an
// edit. It tracks the text font the strings are shown with, which the guard
// for a composite font reads, and the font size and character spacing the
// room of a removed glyph depends on. A string shown with a font name the
// page's /Resources do not define is kept.
//
// # Corpus evidence
//
//   - pdfjs issue2856.pdf: eating the 0x00 high bytes of multi-byte strings is
//     the corruption that cost this file a third of its text.
//   - pdfjs issue2856.pdf: byte-level stripping under a composite font turned
//     "A different method" into "㼼8E8AG@8" and cost issue2856 a third of its
//     text.
//   - verapdf: skipping composite fonts leaves ~56 .notdef references unfixed,
//     which is the safe side of the trade.
//   - pdfjs poppler-90-0-fuzzed.pdf: the only file showing a control byte with
//     a font name the page does not define, on page 2 with no font selected.
//     Keeping those strings leaves the findings litho and veraPDF share on its
//     aggressive PDF/A-2b, PDF/A-4 and PDF/UA-1 outputs as they were.
type controlByteStripper struct {
	fonts       map[string]textFontInfo
	currentFont string
	// fontSize is the size Tf sets and charSpacing the spacing Tc and " set,
	// both text state parameters starting at the page's defaults, unset and
	// 0 (ISO 32000-1, 9.3.1, Table 104).
	fontSize, charSpacing float64
	// saved holds the text state q saved. The text state is part of the
	// graphics state, so Q restores it (ISO 32000-1, 8.4.2 and Table 104).
	// Enumerate before you fix: a font-only restore in tagging's collector
	// missed the leading it also tracked and cost a second commit.
	//
	// Both directions are wrong without the font, and both were measured. A
	// simple font selected inside q…Q outliving the Q makes the composite
	// guard consult the wrong name and STRIP a multi-byte string: the CIDs
	// <0041 0020 0042> came back as "A B", their 0x00 high bytes eaten. A
	// composite font outliving the Q does the opposite and SKIPS a simple
	// string whose control byte should have gone.
	saved []strippedTextState
}

// strippedTextState is the text state a [controlByteStripper] carries, as q
// saves it.
type strippedTextState struct {
	font                  string
	fontSize, charSpacing float64
}

// repair returns op with the control bytes of its strings removed, only what
// op does besides showing text when it empties one of them, and how many bytes
// it removed. The operands of op are left as they are: a repaired operation
// holds a copy.
func (s *controlByteStripper) repair(op content.Operation) ([]content.Operation, int) {
	switch op.Operator {
	case "Tf":
		if len(op.Operands) > 0 {
			if name, ok := op.Operands[0].(ast.PdfName); ok {
				s.currentFont = string(name)
			}
		}
		if len(op.Operands) > 1 {
			s.fontSize = document.Float(op.Operands[1])
		}
	case "Tc":
		if len(op.Operands) > 0 {
			s.charSpacing = document.Float(op.Operands[0])
		}
	case "\"":
		// aw ac string " sets the character spacing to ac before it shows
		// the string (ISO 32000-1, 9.4.3, Table 109).
		if n := len(op.Operands); n >= 3 {
			s.charSpacing = document.Float(op.Operands[n-2])
		}
	case "q":
		s.saved = append(s.saved, strippedTextState{font: s.currentFont, fontSize: s.fontSize, charSpacing: s.charSpacing})
	case "Q":
		if n := len(s.saved); n > 0 {
			st := s.saved[n-1]
			s.currentFont, s.fontSize, s.charSpacing = st.font, st.fontSize, st.charSpacing
			s.saved = s.saved[:n-1]
		}
	}
	keep := []content.Operation{op}
	if op.Operator != "Tj" && op.Operator != "TJ" && op.Operator != "'" && op.Operator != "\"" {
		return keep, 0
	}
	// A composite font addresses glyphs with multi-byte codes
	// (ISO 32000-1, 9.7.6), so a byte below 0x20 is usually the high
	// half of an ordinary code, not a glyph reference: under
	// Identity-H every CID under U+2000 carries a 0x00 high byte.
	// Dropping those bytes deletes half of each code and slides the
	// rest of the string into garbage.
	//
	// TODO: a composite font could still be repaired by dropping whole
	// codes rather than bytes, which needs the /Encoding CMap's
	// codespace ranges to know each code's width (ISO 32000-1, 9.7.6.2);
	// Identity-H is two bytes but a predefined CMap may mix widths.
	// Skipping leaves those .notdef references unfixed, which is the safe
	// side of the trade.
	//
	// A font name the page's /Resources do not define selects no font (ISO
	// 32000-1, 9.3.1, Table 105 names Tf's operand in the /Font of the
	// current resource dictionary), so its strings reference no glyph, the
	// .notdef rules judge none of them (ISO 19005-2, 6.2.11.8), and whether
	// a byte is a whole code is unknown: they are kept.
	info, known := s.fonts[s.currentFont]
	if !known || info.composite {
		return keep, 0
	}
	var operands []ast.PdfObject
	removed, emptied := 0, false
	for j, operand := range op.Operands {
		var cleaned ast.PdfObject
		switch v := operand.(type) {
		case ast.PdfString:
			str, n := stripControlString(v, info.defined)
			if n == 0 {
				continue
			}
			removed += n
			cleaned = str
			emptied = emptied || len(str) == 0
		case ast.PdfArray:
			arr, n := stripControlTJArray(v, info.defined)
			if n == 0 {
				continue
			}
			removed += n
			cleaned = arr
			emptied = emptied || isEmptyTJArray(arr)
		default:
			continue
		}
		if operands == nil {
			operands = slices.Clone(op.Operands)
		}
		operands[j] = cleaned
	}
	if removed == 0 {
		return keep, 0
	}
	if kept, ok := s.keepAdvances(op, info); ok {
		return kept, removed
	}
	if emptied {
		return emptiedShowEffect(op, operands), removed
	}
	return []content.Operation{{Operator: op.Operator, Operands: operands}}, removed
}

// keepAdvances returns the operations that show what op shows less the
// glyphs the strip removes, each removed glyph's room on the line kept as a
// number in a TJ array, or false when every removed glyph took none.
//
// A glyph moves the text position by its width times the font size plus the
// character spacing (ISO 32000-1, 9.4.4), and a number n in a TJ array moves
// it by -n/1000 times the font size (9.4.3, Table 109), both times the
// horizontal scaling. Removing a glyph that took room drew every later glyph
// on the line that much further back, so the glyph gives way to the number
// that moves the position as far. A Tj, ' or " doing so becomes a TJ after
// what else it does, as Table 109 defines each.
//
// A font of size 0, a Type 3 font whose glyphs leave the text line, and an
// operation of a shape Table 109 gives no meaning keep the strip alone: no
// number moves the position as the glyph did.
//
// # Corpus evidence
//
//   - All corpora: of the 30 files a .notdef finding runs the strip on at
//     PDF/A-2b, PDF/UA-1 or the profile their name states, 18 remove a glyph
//     that took room with more glyphs after it on its line, in 511 operations
//     of govdocs thread_400_400240.pdf and one to four in each other file.
//   - litho repairleft_4.pdf: poppler drew the lines after its emptied " and
//     ' at x 72 once the strip removed their code, where the source draws
//     them at 80.2, and draws them at 80.2 with the room kept.
func (s *controlByteStripper) keepAdvances(op content.Operation, info textFontInfo) ([]content.Operation, bool) {
	if s.fontSize == 0 || info.offLine {
		return nil, false
	}
	var shown ast.PdfArray
	n := len(op.Operands)
	switch {
	case op.Operator == "TJ" && n == 1:
		arr, ok := op.Operands[0].(ast.PdfArray)
		if !ok {
			return nil, false
		}
		shown = arr
	case (op.Operator == "Tj" || op.Operator == "'") && n == 1, op.Operator == "\"" && n == 3:
		str, ok := op.Operands[n-1].(ast.PdfString)
		if !ok {
			return nil, false
		}
		shown = ast.PdfArray{str}
	default:
		return nil, false
	}

	elems, moved := s.spacedElements(shown, info)
	if !moved {
		return nil, false
	}
	var ops []content.Operation
	switch op.Operator {
	case "'":
		ops = append(ops, content.Operation{Operator: "T*"})
	case "\"":
		ops = append(ops,
			content.Operation{Operator: "Tw", Operands: []ast.PdfObject{op.Operands[0]}},
			content.Operation{Operator: "Tc", Operands: []ast.PdfObject{op.Operands[1]}},
			content.Operation{Operator: "T*"})
	}
	return append(ops, content.Operation{Operator: "TJ", Operands: []ast.PdfObject{elems}}), true
}

// spacedElements returns the elements of the TJ array shown with the control
// bytes of its strings removed, each run of removed glyphs replaced by the
// number that moves the text position as far as they did (see
// [controlByteStripper.keepAdvances]), and whether one such number is not 0.
// Numbers are rounded to a thousandth, a millionth of a unit of text space
// per unit of font size.
func (s *controlByteStripper) spacedElements(shown ast.PdfArray, info textFontInfo) (ast.PdfArray, bool) {
	out := make(ast.PdfArray, 0, len(shown))
	moved := false
	for _, elem := range shown {
		str, ok := elem.(ast.PdfString)
		if !ok {
			out = append(out, elem)
			continue
		}
		var run []byte
		var gap float64
		flush := func() {
			if len(run) > 0 {
				out = append(out, ast.PdfString(run))
				run = nil
			}
			if gap = math.Round(gap*1000) / 1000; gap != 0 {
				out = append(out, ast.PdfReal(gap))
				moved = true
			}
			gap = 0
		}
		for _, b := range []byte(str) {
			if !strippedByte(b, info.defined) {
				if gap != 0 {
					flush()
				}
				run = append(run, b)
				continue
			}
			room := -(info.advance[b]*1000 + s.charSpacing*1000/s.fontSize)
			if room == 0 {
				continue
			}
			if len(run) > 0 {
				out = append(out, ast.PdfString(run))
				run = nil
			}
			gap += room
		}
		flush()
	}
	return out, moved
}

// emptiedShowEffect returns the operations that do what the text-showing
// operation op does besides showing the string the repair emptied, operands
// being op's operands as the repair left them. ISO 32000-1, 9.4.3, Table 109
// defines string ' as T* string Tj, and aw ac string " as aw Tw ac Tc string ':
// dropping either whole would move every later line up by one and lose the
// spacing it set. A number in a TJ array moves the text position by that many
// thousandths of a unit of text space, so a TJ left holding numbers alone is
// written back with them: dropping it would start every later string on the
// line that much further back. A Tj does nothing else, and neither does a TJ
// whose numbers move nothing, nor a " short of its three operands, which
// Table 109 gives no meaning.
//
// # Corpus evidence
//
//   - All corpora: no page shows a " string holding a byte the strip removes,
//     and two files show a ' string the strip empties, govdocs
//     thread_100_100586.pdf and pdfjs bug854315.pdf, where no .notdef finding
//     runs the repair.
//   - litho repairleft_4.pdf: dropping its emptied ' and " lost two line
//     breaks from its text, which rolled the whole content tier back and left
//     its .notdef findings, in the aggressive remediation at PDF/A-2a, 2b,
//     2u, 3b and 4, PDF/UA-1 and PDF/UA-2.
func emptiedShowEffect(op content.Operation, operands []ast.PdfObject) []content.Operation {
	nextLine := content.Operation{Operator: "T*"}
	switch op.Operator {
	case "TJ":
		if len(operands) == 1 && movesTextPosition(operands[0]) {
			return []content.Operation{{Operator: op.Operator, Operands: operands}}
		}
		return nil
	case "'":
		return []content.Operation{nextLine}
	case "\"":
		// An interpreter takes an operator's operands from the top of its
		// stack, so aw and ac are the two below the string.
		n := len(op.Operands)
		if n < 3 {
			return nil
		}
		return []content.Operation{
			{Operator: "Tw", Operands: []ast.PdfObject{op.Operands[n-3]}},
			{Operator: "Tc", Operands: []ast.PdfObject{op.Operands[n-2]}},
			nextLine,
		}
	}
	return nil
}

// strippedByte reports whether the strip removes the code b: an ASCII
// control byte but TAB, LF and CR, the font's encoding gives no glyph for.
func strippedByte(b byte, defined map[byte]bool) bool {
	return b < 0x20 && b != '\t' && b != '\n' && b != '\r' && !defined[b]
}

// stripControlString returns a copy of s with ASCII control bytes
// removed. The returned count reports how many bytes were stripped.
func stripControlString(s ast.PdfString, defined map[byte]bool) (ast.PdfString, int) {
	src := []byte(s)
	dst := make([]byte, 0, len(src))
	removed := 0
	for _, b := range src {
		if strippedByte(b, defined) {
			removed++
			continue
		}
		dst = append(dst, b)
	}
	if removed == 0 {
		return s, 0
	}
	return ast.PdfString(dst), removed
}

// stripControlTJArray cleans every PdfString inside a TJ operand array.
func stripControlTJArray(arr ast.PdfArray, defined map[byte]bool) (ast.PdfArray, int) {
	out := make(ast.PdfArray, 0, len(arr))
	removed := 0
	for _, elem := range arr {
		if s, ok := elem.(ast.PdfString); ok {
			cleaned, n := stripControlString(s, defined)
			removed += n
			if len(cleaned) == 0 {
				continue
			}
			out = append(out, cleaned)
			continue
		}
		out = append(out, elem)
	}
	return out, removed
}

// isEmptyTJArray reports whether arr holds no text content after
// cleaning: only spacing adjustments (numbers) remain.
func isEmptyTJArray(arr ast.PdfArray) bool {
	for _, elem := range arr {
		if _, ok := elem.(ast.PdfString); ok {
			return false
		}
	}
	return true
}

// movesTextPosition reports whether obj, a TJ operand the repair left holding
// numbers alone, moves the text position: whether one of its numbers is not
// zero (ISO 32000-1, 9.4.3, Table 109).
func movesTextPosition(obj ast.PdfObject) bool {
	arr, ok := obj.(ast.PdfArray)
	if !ok {
		return false
	}
	for _, elem := range arr {
		if document.Float(elem) != 0 {
			return true
		}
	}
	return false
}

// textFontInfo records what a page's font says about the codes drawn with it.
type textFontInfo struct {
	// composite marks a font addressing glyphs with multi-byte codes, whose
	// strings must not be edited a byte at a time.
	composite bool
	// defined holds the single-byte codes the font's /Encoding gives a glyph
	// for, which are therefore not .notdef references.
	defined map[byte]bool
	// advance holds the horizontal displacement, in text space units per
	// unit of font size, of each control code whose width is not 0 (see
	// [controlAdvances]).
	advance map[byte]float64
	// offLine marks a Type 3 font whose glyphs move the text position off
	// the text line, which no TJ number can follow.
	offLine bool
}

// controlAdvances returns the horizontal displacement, in text space units per
// unit of font size, of each code below 0x20 the simple font dictionary fd
// gives a width that is not 0, and whether its glyphs move off the text line.
//
// A width is the /Widths entry of a code from /FirstChar to /LastChar and the
// font descriptor's /MissingWidth, by default 0, for any other (ISO 32000-1,
// 9.2.4 and 9.6.2.1, Table 111). It is in thousandths of a unit of text space,
// but for a Type 3 font, whose widths are in glyph space and which the
// /FontMatrix maps to text space (9.6.5, Table 112): a matrix with a second
// element that is not 0 moves each glyph off the line.
func controlAdvances(ctx context.Context, doc *document.Document, fd ast.PdfDict) (map[byte]float64, bool) {
	scale := 0.001
	if subtype, _ := document.ResolveName(ctx, doc, fd["Subtype"]); subtype == "Type3" {
		// Best-effort: a Type 3 font with no readable /FontMatrix has no
		// displacement to keep.
		matrix, _ := document.ResolveArray(ctx, doc, fd["FontMatrix"])
		if len(matrix) < 2 {
			return nil, true
		}
		if resolvedFloat(ctx, doc, matrix[1]) != 0 {
			return nil, true
		}
		scale = resolvedFloat(ctx, doc, matrix[0])
	}
	// Best-effort: an unreadable /FirstChar, /Widths or /FontDescriptor
	// leaves the widths at the /MissingWidth default of 0.
	first, _ := document.ResolveInt(ctx, doc, fd["FirstChar"])
	widths, _ := document.ResolveArray(ctx, doc, fd["Widths"])
	missing := 0.0
	if desc, _ := document.ResolveDict(ctx, doc, fd["FontDescriptor"]); desc != nil {
		missing = resolvedFloat(ctx, doc, desc["MissingWidth"])
	}
	var advance map[byte]float64
	for code := range 0x20 {
		width := missing
		if idx := int64(code) - int64(first); idx >= 0 && idx < int64(len(widths)) {
			width = resolvedFloat(ctx, doc, widths[idx])
		}
		if width*scale == 0 {
			continue
		}
		if advance == nil {
			advance = make(map[byte]float64)
		}
		advance[byte(code)] = width * scale
	}
	return advance, false
}

// resolvedFloat returns the number obj holds or refers to, 0 when it is none.
func resolvedFloat(ctx context.Context, doc *document.Document, obj ast.PdfObject) float64 {
	resolved, err := document.ResolveObject(ctx, doc, obj)
	if err != nil {
		return 0
	}
	return document.Float(resolved)
}

// pageTextFonts describes every font of fonts, the /Font dictionary of the
// /Resources a page resolves its names against ([pageResourceCategory]),
// reusing entries already described for another page through cache, keyed by
// object number.
func pageTextFonts(ctx context.Context, doc *document.Document, fonts ast.PdfDict, cache map[int]textFontInfo) map[string]textFontInfo {
	if fonts == nil {
		return nil
	}
	infos := make(map[string]textFontInfo, len(fonts))
	for name, entry := range fonts {
		var objNum int
		if ref, ok := entry.(ast.PdfIndirectRef); ok {
			objNum = ref.ObjectNum
			if cached, ok := cache[objNum]; ok {
				infos[string(name)] = cached
				continue
			}
		}
		fd, err := document.ResolveDict(ctx, doc, entry)
		if err != nil || fd == nil {
			// An unresolvable font could be composite; treat it as such
			// so a byte-level edit is never applied blind.
			infos[string(name)] = textFontInfo{composite: true}
			if objNum > 0 {
				cache[objNum] = textFontInfo{composite: true}
			}
			continue
		}
		if subtype, _ := document.ResolveName(ctx, doc, fd["Subtype"]); subtype == "Type0" {
			infos[string(name)] = textFontInfo{composite: true}
			if objNum > 0 {
				cache[objNum] = textFontInfo{composite: true}
			}
			continue
		}
		info := textFontInfo{}
		info.advance, info.offLine = controlAdvances(ctx, doc, fd)
		if font, err := typography.ResolveFont(ctx, doc, fd); err == nil && font != nil {
			for code := range font.Encoding {
				if code < 0x20 {
					if info.defined == nil {
						info.defined = make(map[byte]bool)
					}
					info.defined[code] = true
				}
			}
		} else {
			doc.Logger.Debug("resolving font encoding for control-byte strip", slog.Any("err", err))
		}
		infos[string(name)] = info
		if objNum > 0 {
			cache[objNum] = info
		}
	}
	return infos
}
