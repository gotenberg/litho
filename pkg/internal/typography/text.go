package typography

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/semantics"
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

		// 3. Clipping check — use a glyph bounding box (origin + advance width
		// × font size height) rather than just the origin point so that glyphs
		// partially outside the clip region are still detected.
		glyphH := fontSize
		glyphW := pageWidth
		if glyphW == 0 {
			glyphW = 0.5 * fontSize // fallback for unknown width
		}
		glyphMinX := x
		glyphMaxX := x + glyphW
		glyphMinY := y - 0.2*glyphH // descender estimate
		glyphMaxY := y + 0.8*glyphH // ascender estimate
		clip := state.ClippingPath
		if glyphMaxX < clip.MinX || glyphMinX > clip.MaxX || glyphMaxY < clip.MinY || glyphMinY > clip.MaxY {
			isHidden = true
		}

		*elements = append(*elements, models.TextElement{
			Text:          char,
			X:             x,
			Y:             y,
			Width:         pageWidth,
			FontSize:      fontSize,
			MCID:          mcid,
			Hidden:        isHidden,
			Invisible:     isInvisible,
			Vertical:      font.WMode == 1,
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

// ReconstructText takes spatial text elements and reassembles them into a string.
// When no article thread beads are provided, it preserves content stream order
// (matching PDF.js behavior). When beads are provided, it sorts elements by
// bead index to respect explicit multi-column reading order.
func ReconstructText(elements []models.TextElement, beads []semantics.Bead) string {
	if len(elements) == 0 {
		return ""
	}

	// When beads are present, use bead-aware spatial sorting.
	if len(beads) > 0 {
		return reconstructWithBeads(elements, beads)
	}

	// Content stream order: group elements into lines, apply bidi reordering
	// per line, then join with newlines and inferred spaces.
	type line struct {
		elems []models.TextElement
	}
	var lines []line
	cur := line{elems: []models.TextElement{elements[0]}}

	for i := 1; i < len(elements); i++ {
		prev := elements[i-1]
		el := elements[i]
		newLine := false
		if el.Vertical {
			if math.Abs(prev.X-el.X) > 2.0 {
				newLine = true
			}
		} else {
			if math.Abs(prev.Y-el.Y) > 2.0 {
				newLine = true
			}
		}
		if newLine {
			lines = append(lines, cur)
			cur = line{elems: []models.TextElement{el}}
		} else {
			cur.elems = append(cur.elems, el)
		}
	}
	lines = append(lines, cur)

	var sb strings.Builder
	for lineIdx, ln := range lines {
		if lineIdx > 0 {
			sb.WriteString("\n")
		}

		// Bidi reorder within the line.
		chars := make([]string, len(ln.elems))
		for i, el := range ln.elems {
			chars[i] = el.Text
		}
		reordered := reorderBidiLine(chars)

		for i, ch := range reordered {
			if i > 0 {
				prev := ln.elems[i-1]
				curr := ln.elems[i]
				if curr.Vertical {
					if needsSpace(prev.Y-curr.Y, prev.Width, curr.FontSize) {
						sb.WriteString(" ")
					}
				} else {
					if needsSpace(curr.X-prev.X, prev.Width, curr.FontSize) {
						sb.WriteString(" ")
					}
				}
			}
			sb.WriteString(ch)
		}
	}

	return normalizeSoftHyphens(sb.String())
}

// needsSpace decides whether to insert a space between two consecutive glyphs.
// delta is the distance between the two glyphs along the reading direction
// (curr.X - prev.X for horizontal, prev.Y - curr.Y for vertical).
// prevWidth is the advance width of the previous glyph (0 if unknown).
func needsSpace(delta, prevWidth, fontSize float64) bool {
	if prevWidth > 0 {
		// Use actual glyph width: space exists when the gap after the glyph
		// exceeds a fraction of the font size. A typical inter-word space is
		// ~0.25 em; TJ kerning adjustments rarely exceed 0.1 em in practice.
		// Use 0.15 em as the threshold to catch word gaps while absorbing kerning.
		gap := delta - prevWidth
		return gap > 0.15*fontSize
	}
	// Fallback when Width is not set: use a heuristic.
	// Expected advance is ~0.5*fontSize; a gap well beyond that is a space.
	// Use a higher threshold (0.3) to avoid false positives from kerning.
	expectedAdvance := 0.5 * fontSize
	gap := delta - expectedAdvance
	return gap > 0.3*fontSize
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

// reconstructWithBeads handles the bead-aware spatial sorting path for PDFs
// that have article threads defining explicit reading order.
func reconstructWithBeads(elements []models.TextElement, beads []semantics.Bead) string {
	type elementWithBead struct {
		models.TextElement
		BeadIndex int
	}

	// Detect whether the page contains vertical text.
	verticalCount := 0
	for _, el := range elements {
		if el.Vertical {
			verticalCount++
		}
	}
	isVerticalPage := verticalCount > len(elements)/2

	ewb := make([]elementWithBead, len(elements))
	for i, el := range elements {
		assignedBead := -1
		for bIdx, bead := range beads {
			minX := bead.Rect[0]
			maxX := bead.Rect[2]
			if minX > maxX {
				minX, maxX = maxX, minX
			}
			minY := bead.Rect[1]
			maxY := bead.Rect[3]
			if minY > maxY {
				minY, maxY = maxY, minY
			}

			if el.X >= minX && el.X <= maxX && el.Y >= minY && el.Y <= maxY {
				assignedBead = bIdx
				break
			}
		}
		ewb[i] = elementWithBead{TextElement: el, BeadIndex: assignedBead}
	}

	if isVerticalPage {
		sort.SliceStable(ewb, func(i, j int) bool {
			if ewb[i].BeadIndex != ewb[j].BeadIndex {
				if ewb[i].BeadIndex == -1 {
					return false
				}
				if ewb[j].BeadIndex == -1 {
					return true
				}
				return ewb[i].BeadIndex < ewb[j].BeadIndex
			}
			if ewb[i].X > ewb[j].X+2.0 {
				return true
			} else if ewb[i].X < ewb[j].X-2.0 {
				return false
			}
			return ewb[i].Y > ewb[j].Y
		})
	} else {
		sort.SliceStable(ewb, func(i, j int) bool {
			if ewb[i].BeadIndex != ewb[j].BeadIndex {
				if ewb[i].BeadIndex == -1 {
					return false
				}
				if ewb[j].BeadIndex == -1 {
					return true
				}
				return ewb[i].BeadIndex < ewb[j].BeadIndex
			}
			if ewb[i].Y > ewb[j].Y+2.0 {
				return true
			} else if ewb[i].Y < ewb[j].Y-2.0 {
				return false
			}
			return ewb[i].X < ewb[j].X
		})
	}

	// Group sorted elements into lines.
	type lineGroup struct {
		elements []elementWithBead
	}
	var lines []lineGroup
	currentLine := lineGroup{elements: []elementWithBead{ewb[0]}}

	for i := 1; i < len(ewb); i++ {
		prev := ewb[i-1]
		curr := ewb[i]

		isNewLine := false
		if prev.BeadIndex != curr.BeadIndex {
			isNewLine = true
		} else if isVerticalPage {
			dx := prev.X - curr.X
			if dx > 0.5*curr.FontSize || dx < -0.5*curr.FontSize {
				isNewLine = true
			}
		} else {
			dy := prev.Y - curr.Y
			if dy > 0.5*curr.FontSize || dy < -0.5*curr.FontSize {
				isNewLine = true
			}
		}

		if isNewLine {
			lines = append(lines, currentLine)
			currentLine = lineGroup{elements: []elementWithBead{curr}}
		} else {
			currentLine.elements = append(currentLine.elements, curr)
		}
	}
	lines = append(lines, currentLine)

	var sb strings.Builder
	for lineIdx, line := range lines {
		chars := make([]string, len(line.elements))
		for i, el := range line.elements {
			chars[i] = el.Text
		}
		reordered := reorderBidiLine(chars)

		if lineIdx > 0 {
			prevLine := lines[lineIdx-1]
			lastPrev := prevLine.elements[len(prevLine.elements)-1]
			firstCurr := line.elements[0]
			if lastPrev.BeadIndex != firstCurr.BeadIndex {
				sb.WriteString("\n\n")
			} else {
				sb.WriteString("\n")
			}
		}

		for i, ch := range reordered {
			if i > 0 {
				prev := line.elements[i-1]
				curr := line.elements[i]
				if isVerticalPage {
					if needsSpace(prev.Y-curr.Y, prev.Width, curr.FontSize) {
						sb.WriteString(" ")
					}
				} else {
					if needsSpace(curr.X-prev.X, prev.Width, curr.FontSize) {
						sb.WriteString(" ")
					}
				}
			}
			sb.WriteString(ch)
		}
	}

	return normalizeSoftHyphens(sb.String())
}

// maxFormXObjectDepth is the maximum recursion depth for nested Form XObjects.
// This prevents stack overflows and excessive processing time from circular
// references or deeply nested XObjects in malformed PDFs.
const maxFormXObjectDepth = 10

// extractTextRecursive recursively extracts text from operations, supporting Form XObjects.
// The visited set tracks indirect reference object numbers to break circular references,
// and depth guards against non-circular but excessively deep nesting.
func extractTextRecursive(ctx context.Context, doc *document.Document, machine *graphics.Machine, resources ast.PdfDict, ops []content.Operation, elements *[]models.TextElement, visited map[int]bool, depth int) error {
	if depth > maxFormXObjectDepth {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	var currentFont *Font

	// Make resources available to the machine for pattern resolution.
	machine.Ctx = ctx
	machine.Doc = doc
	machine.Resources = resources

	// Pre-populate active OCs from resources /Properties
	if propsObj, ok := resources["Properties"]; ok {
		propsRes, _ := document.ResolveObject(ctx, doc, propsObj)
		if propsDict, ok := propsRes.(ast.PdfDict); ok {
			for k, v := range propsDict {
				vRes, _ := document.ResolveObject(ctx, doc, v)
				if dict, ok := vRes.(ast.PdfDict); ok {
					if subtypeObj, ok := dict["Type"]; ok {
						subtypeRes, _ := document.ResolveObject(ctx, doc, subtypeObj)
						if subtypeName, ok := subtypeRes.(ast.PdfName); ok && subtypeName == "OCG" {
							// Determine visibility using Off -> On -> BaseState priority.
							visible := true
							if ref, ok := v.(ast.PdfIndirectRef); ok {
								if doc.OCConfig != nil {
									if doc.OCConfig.Off[ref.ObjectNum] {
										visible = false
									} else if doc.OCConfig.On[ref.ObjectNum] {
										visible = true
									} else {
										// Not in either explicit list: fall back to BaseState.
										visible = doc.OCConfig.BaseState != "OFF"
									}
								}
							}
							machine.ActiveOCs[string(k)] = visible
						}
					}
				}
			}
		}
	}

	for _, op := range ops {
		// Errors from Execute are non-fatal: unknown/unsupported operators return errors
		// but must not abort extraction of the remaining content stream.
		_ = machine.Execute([]content.Operation{op}) //nolint:errcheck

		state := machine.Current()

		// If Marked Content just provided an ActualText cheat-code, render it directly.
		if machine.PendingActualText != "" {
			if !state.SkipExtraction {
				fontSize := state.Text.Tfs
				if fontSize == 0 {
					fontSize = 12
				}

				mcid := -1
				var mcLang2, mcExpansion2 string
				for i := len(machine.MarkedContent) - 1; i >= 0; i-- {
					mc := &machine.MarkedContent[i]
					if mcid == -1 && mc.MCID != -1 {
						mcid = mc.MCID
					}
					if mcLang2 == "" && mc.Language != "" {
						mcLang2 = mc.Language
					}
					if mcExpansion2 == "" && mc.ExpansionText != "" {
						mcExpansion2 = mc.ExpansionText
					}
				}

				// Handle UTF-16 BE encoding that might be present in ActualText
				actualText := document.DecodePdfString(ast.PdfString(machine.PendingActualText))

				for _, r := range actualText {
					tm := state.Text.Tm
					ctm := state.CTM
					tx := tm[4]
					ty := tm[5]
					x := tx*ctm[0] + ty*ctm[2] + ctm[4]
					y := tx*ctm[1] + ty*ctm[3] + ctm[5]

					advance := 0.5 * fontSize
					dx := advance * tm[0] * ctm[0]

					*elements = append(*elements, models.TextElement{
						Text:          string(r),
						X:             x,
						Y:             y,
						Width:         math.Abs(dx),
						FontSize:      fontSize,
						MCID:          mcid,
						Language:      mcLang2,
						ExpansionText: mcExpansion2,
					})

					state.Text.Tm[4] += advance * state.Text.Tm[0]
					state.Text.Tm[5] += advance * state.Text.Tm[1]
				}
			}
			machine.PendingActualText = ""
		}

		if state.SkipExtraction {
			continue // Skip all drawing/text operators if this layer is hidden
		}

		switch op.Operator {
		case "Tf": // Set text font
			if len(op.Operands) < 1 {
				continue
			}
			fontName, ok := op.Operands[0].(ast.PdfName)
			if !ok {
				continue
			}

			resolved := false
			if resObj, ok := resources["Font"]; ok {
				resResolved, err := document.ResolveObject(ctx, doc, resObj)
				if err != nil {
					return fmt.Errorf("failed to load font /%s: %w", fontName, ErrFontNotFound)
				}
				if resDictRoot, ok := resResolved.(ast.PdfDict); ok {
					if fontRefObj, ok := resDictRoot[fontName]; ok {
						fontDictResolved, err := document.ResolveObject(ctx, doc, fontRefObj)
						if err != nil {
							return fmt.Errorf("failed to load font /%s: %w", fontName, ErrFontNotFound)
						}
						if fontDict, ok := fontDictResolved.(ast.PdfDict); ok {
							font, err := GetFont(ctx, doc, fontDict)
							if err != nil {
								return fmt.Errorf("failed to load font /%s: %w", fontName, ErrFontNotFound)
							}
							currentFont = font
							resolved = true
						}
					}
				}
			}
			if !resolved {
				return fmt.Errorf("failed to load font /%s: %w", fontName, ErrFontNotFound)
			}

		case "Tj": // Show text
			if machine.IgnoreTextOps {
				continue
			}
			if len(op.Operands) == 1 && currentFont != nil {
				if str, ok := op.Operands[0].(ast.PdfString); ok {
					processText(str, currentFont, machine, elements)
				}
			}

		case "'": // Move to next line and show text
			if machine.IgnoreTextOps {
				continue
			}
			if len(op.Operands) == 1 && currentFont != nil {
				if str, ok := op.Operands[0].(ast.PdfString); ok {
					processText(str, currentFont, machine, elements)
				}
			}

		case "\"": // Set word/char spacing, move to next line, and show text
			if machine.IgnoreTextOps {
				continue
			}
			if len(op.Operands) == 3 && currentFont != nil {
				if str, ok := op.Operands[2].(ast.PdfString); ok {
					processText(str, currentFont, machine, elements)
				}
			}

		case "TJ": // Show text with kerning
			if machine.IgnoreTextOps {
				continue
			}
			if len(op.Operands) == 1 && currentFont != nil {
				if arr, ok := op.Operands[0].(ast.PdfArray); ok {
					for _, item := range arr {
						if str, ok := item.(ast.PdfString); ok {
							processText(str, currentFont, machine, elements)
						} else {
							var numVal float64
							switch num := item.(type) {
							case ast.PdfInt:
								numVal = float64(num)
							case ast.PdfReal:
								numVal = float64(num)
							default:
								continue
							}
							state := machine.Current()
							fontSize := state.Text.Tfs
							if fontSize == 0 {
								fontSize = 12
							}
							adjustment := -(numVal / 1000.0) * fontSize
							if currentFont.WMode == 1 {
								// Vertical mode: displace along Y axis.
								state.Text.Tm[4] += adjustment * state.Text.Tm[2]
								state.Text.Tm[5] += adjustment * state.Text.Tm[3]
							} else {
								// Horizontal mode: displace along X axis.
								state.Text.Tm[4] += adjustment * state.Text.Tm[0]
								state.Text.Tm[5] += adjustment * state.Text.Tm[1]
							}
						}
					}
				}
			}

		case "Do": // Draw Object (Form XObject)
			if len(op.Operands) == 1 {
				if name, ok := op.Operands[0].(ast.PdfName); ok {
					if xobjObj, ok := resources["XObject"]; ok {
						xobjResolved, _ := document.ResolveObject(ctx, doc, xobjObj)
						if xobjDict, ok := xobjResolved.(ast.PdfDict); ok {
							if targetObj, ok := xobjDict[name]; ok {
								// Track visited Form XObjects by indirect reference to break circular references.
								objNum := 0
								if ref, ok := targetObj.(ast.PdfIndirectRef); ok {
									objNum = ref.ObjectNum
									if visited[objNum] {
										continue
									}
								}

								targetResolved, _ := document.ResolveObject(ctx, doc, targetObj)
								if stream, ok := targetResolved.(ast.PdfStream); ok {
									if subtypeObj, ok := stream.Dict["Subtype"]; ok {
										subtypeResolved, _ := document.ResolveObject(ctx, doc, subtypeObj)
										if subtypeName, ok := subtypeResolved.(ast.PdfName); ok && subtypeName == "Form" {
											// Parse the Form XObject stream
											formOps, err := content.ParsePageContent(ctx, doc, stream)
											if err == nil {
												// Determine resources
												formRes := resources // inherit by default
												if resObj, ok := stream.Dict["Resources"]; ok {
													resResolved, _ := document.ResolveObject(ctx, doc, resObj)
													if resDict, ok := resResolved.(ast.PdfDict); ok {
														formRes = resDict
													}
												}

												// Graphics state save/restore and matrix transform: these trivial
												// operators cannot fail in practice, and this function does not
												// return errors, so suppression is justified.
												_ = machine.Execute([]content.Operation{{Operator: "q"}}) //nolint:errcheck

												// If there is a Matrix in the Form XObject, apply it
												if matrixObj, ok := stream.Dict["Matrix"]; ok {
													matrixResolved, err := document.ResolveObject(ctx, doc, matrixObj)
													if err != nil {
														return err
													}
													if matrixArr, ok := matrixResolved.(ast.PdfArray); ok && len(matrixArr) == 6 {
														_ = machine.Execute([]content.Operation{{ //nolint:errcheck
															Operator: "cm",
															Operands: matrixArr,
														}})
													}
												}

												// Mark as visited before recursing; unmark after to allow
												// the same XObject to appear in sibling contexts.
												if objNum != 0 {
													visited[objNum] = true
												}
												err = extractTextRecursive(ctx, doc, machine, formRes, formOps, elements, visited, depth+1)
												if objNum != 0 {
													delete(visited, objNum)
												}
												if err != nil {
													return err
												}

												_ = machine.Execute([]content.Operation{{Operator: "Q"}}) //nolint:errcheck
											}
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}
	return nil
}

// ExtractionConfig holds options for text extraction behavior
type ExtractionConfig struct {
	StripInvisible bool // If true, completely omits text matched by invisibility heuristics (Tr=3, white-on-white)
	StripHidden    bool // If true, completely omits text outside the graphics state clipping path (W/W*)
	StripClipped   bool // If true, completely omits text outside the page CropBox
	StripArtifacts bool // If true, discards text inside /Artifact marked content (headers, footers, page numbers)
}

// ExtractElements extracts the text from a page's content operations as raw spatial elements.
func ExtractElements(ctx context.Context, doc *document.Document, machine *graphics.Machine, page *document.Page, ops []content.Operation) ([]models.TextElement, error) {
	var elements []models.TextElement
	if err := extractTextRecursive(ctx, doc, machine, page.Resources, ops, &elements, make(map[int]bool), 0); err != nil {
		return nil, err
	}

	// Flag elements that fall outside the CropBox (the visible region).
	cropBox := boxRectFromArray(page.CropBox)
	if cropBox != [4]float64{} {
		// Normalize: ensure min < max.
		minX, minY, maxX, maxY := cropBox[0], cropBox[1], cropBox[2], cropBox[3]
		if minX > maxX {
			minX, maxX = maxX, minX
		}
		if minY > maxY {
			minY, maxY = maxY, minY
		}
		for i := range elements {
			if elements[i].X < minX || elements[i].X > maxX ||
				elements[i].Y < minY || elements[i].Y > maxY {
				elements[i].IsClipped = true
			}
		}
	}

	return elements, nil
}

// boxRectFromArray converts a PdfArray [llx, lly, urx, ury] to [4]float64.
func boxRectFromArray(arr ast.PdfArray) [4]float64 {
	if len(arr) < 4 {
		return [4]float64{}
	}
	return [4]float64{pdfFloat(arr[0]), pdfFloat(arr[1]), pdfFloat(arr[2]), pdfFloat(arr[3])}
}

// pdfFloat extracts a float64 from a PdfInt or PdfReal.
func pdfFloat(obj ast.PdfObject) float64 {
	switch v := obj.(type) {
	case ast.PdfReal:
		return float64(v)
	case ast.PdfInt:
		return float64(v)
	}
	return 0
}

// ExtractText extracts the text from a page's content operations and reconstructs it.
func ExtractText(ctx context.Context, doc *document.Document, machine *graphics.Machine, page *document.Page, ops []content.Operation, config ExtractionConfig) (string, error) {
	machine.StripArtifacts = config.StripArtifacts

	elements, err := ExtractElements(ctx, doc, machine, page, ops)
	if err != nil {
		return "", err
	}

	// Filter elements based on config
	var filtered []models.TextElement
	for _, el := range elements {
		if config.StripInvisible && el.Invisible {
			continue
		}
		if config.StripHidden && el.Hidden {
			continue
		}
		if config.StripClipped && el.IsClipped {
			continue
		}
		filtered = append(filtered, el)
	}

	var pageBeads []semantics.Bead
	allThreads, err := semantics.GetThreads(ctx, doc)
	if err != nil {
		return "", fmt.Errorf("failed to get article threads: %w", err)
	}
	for _, thread := range allThreads {
		for _, bead := range thread.Beads {
			if bead.PageRef.ObjectNum == page.Ref.ObjectNum && bead.PageRef.GenNum == page.Ref.GenNum {
				pageBeads = append(pageBeads, bead)
			}
		}
	}

	return ReconstructText(filtered, pageBeads), nil
}
