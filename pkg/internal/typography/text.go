package typography

import (
	"fmt"
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
	for i := len(machine.MarkedContent) - 1; i >= 0; i-- {
		if machine.MarkedContent[i].MCID != -1 {
			mcid = machine.MarkedContent[i].MCID
			break
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

		// 3. Clipping check
		if x < state.ClippingPath.MinX || x > state.ClippingPath.MaxX || y < state.ClippingPath.MinY || y > state.ClippingPath.MaxY {
			isHidden = true
		}

		*elements = append(*elements, models.TextElement{
			Text:      char,
			X:         x,
			Y:         y,
			FontSize:  fontSize,
			MCID:      mcid,
			Hidden:    isHidden,
			Invisible: isInvisible,
			Vertical:  font.WMode == 1,
		})

		// Advance text matrix using exact glyph metrics.
		// Char code: big-endian interpretation of the n consumed bytes (1 for simple fonts, 2 for CIDFonts).
		charCode := int(data[0])
		if n == 2 && len(data) >= 2 {
			charCode = int(data[0])<<8 | int(data[1])
		}

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

// ReconstructText takes spatial text elements and reassembles them into a string
// by guessing spaces and newlines based on coordinates.
// If beads are provided, it respects the article threads to solve multi-column issues.
func ReconstructText(elements []models.TextElement, beads []semantics.Bead) string {
	if len(elements) == 0 {
		return ""
	}

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

			// Check if element is within bead's bounding box
			if el.X >= minX && el.X <= maxX && el.Y >= minY && el.Y <= maxY {
				assignedBead = bIdx
				break
			}
		}
		ewb[i] = elementWithBead{TextElement: el, BeadIndex: assignedBead}
	}

	if isVerticalPage {
		// Vertical CJK: right-to-left columns, top-to-bottom within each column.
		sort.SliceStable(ewb, func(i, j int) bool {
			if len(beads) > 0 {
				if ewb[i].BeadIndex != ewb[j].BeadIndex {
					if ewb[i].BeadIndex == -1 {
						return false
					}
					if ewb[j].BeadIndex == -1 {
						return true
					}
					return ewb[i].BeadIndex < ewb[j].BeadIndex
				}
			}
			// Group elements by columns (same X band = same column).
			if ewb[i].X > ewb[j].X+2.0 {
				return true // i is to the right → comes first in CJK vertical
			} else if ewb[i].X < ewb[j].X-2.0 {
				return false
			}
			// Within same column: top-to-bottom (Y descending).
			return ewb[i].Y > ewb[j].Y
		})
	} else {
		// Horizontal: top-to-bottom (Y descending), then left-to-right (X ascending).
		sort.SliceStable(ewb, func(i, j int) bool {
			if len(beads) > 0 {
				if ewb[i].BeadIndex != ewb[j].BeadIndex {
					if ewb[i].BeadIndex == -1 {
						return false
					}
					if ewb[j].BeadIndex == -1 {
						return true
					}
					return ewb[i].BeadIndex < ewb[j].BeadIndex
				}
			}
			// Group elements by lines roughly
			if ewb[i].Y > ewb[j].Y+2.0 {
				return true // i is above j
			} else if ewb[i].Y < ewb[j].Y-2.0 {
				return false // i is below j
			}
			return ewb[i].X < ewb[j].X
		})
	}

	// Group sorted elements into lines/columns for processing.
	type lineGroup struct {
		elements []elementWithBead
	}
	var lines []lineGroup
	currentLine := lineGroup{elements: []elementWithBead{ewb[0]}}

	for i := 1; i < len(ewb); i++ {
		prev := ewb[i-1]
		curr := ewb[i]

		isNewLine := false
		if len(beads) > 0 && prev.BeadIndex != curr.BeadIndex {
			isNewLine = true
		} else if isVerticalPage {
			// New column when X position changes significantly.
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

	// Apply bidi reordering per line and build the output.
	var sb strings.Builder
	for lineIdx, line := range lines {
		// Collect character strings for bidi reordering.
		chars := make([]string, len(line.elements))
		for i, el := range line.elements {
			chars[i] = el.Text
		}
		reordered := reorderBidiLine(chars)

		if lineIdx > 0 {
			prevLine := lines[lineIdx-1]
			lastPrev := prevLine.elements[len(prevLine.elements)-1]
			firstCurr := line.elements[0]
			if len(beads) > 0 && lastPrev.BeadIndex != firstCurr.BeadIndex {
				sb.WriteString("\n\n")
			} else {
				sb.WriteString("\n")
			}
		}

		// Write the reordered characters with inferred spaces.
		for i, ch := range reordered {
			if i > 0 {
				prev := line.elements[i-1]
				curr := line.elements[i]
				if isVerticalPage {
					// Vertical: detect gaps between characters in the Y direction.
					expectedY := prev.Y - 0.5*prev.FontSize
					gap := expectedY - curr.Y
					if gap > 0.2*curr.FontSize {
						sb.WriteString(" ")
					}
				} else {
					expectedX := prev.X + 0.5*prev.FontSize
					gap := curr.X - expectedX
					if gap > 0.2*curr.FontSize {
						sb.WriteString(" ")
					}
				}
			}
			sb.WriteString(ch)
		}
	}

	return stripSoftHyphens(sb.String())
}

// extractTextRecursive recursively extracts text from operations, supporting Form XObjects.
func extractTextRecursive(doc *document.Document, machine *graphics.Machine, resources ast.PdfDict, ops []content.Operation, elements *[]models.TextElement) error {
	var currentFont *Font

	// Pre-populate active OCs from resources /Properties
	if propsObj, ok := resources["Properties"]; ok {
		propsRes, _ := document.ResolveObject(doc, propsObj)
		if propsDict, ok := propsRes.(ast.PdfDict); ok {
			for k, v := range propsDict {
				vRes, _ := document.ResolveObject(doc, v)
				if dict, ok := vRes.(ast.PdfDict); ok {
					if subtypeObj, ok := dict["Type"]; ok {
						subtypeRes, _ := document.ResolveObject(doc, subtypeObj)
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
				for i := len(machine.MarkedContent) - 1; i >= 0; i-- {
					if machine.MarkedContent[i].MCID != -1 {
						mcid = machine.MarkedContent[i].MCID
						break
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

					*elements = append(*elements, models.TextElement{
						Text:     string(r),
						X:        x,
						Y:        y,
						FontSize: fontSize,
						MCID:     mcid,
					})

					advance := 0.5 * fontSize
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
				resResolved, err := document.ResolveObject(doc, resObj)
				if err != nil {
					return fmt.Errorf("failed to load font /%s: %w", fontName, ErrFontNotFound)
				}
				if resDictRoot, ok := resResolved.(ast.PdfDict); ok {
					if fontRefObj, ok := resDictRoot[fontName]; ok {
						fontDictResolved, err := document.ResolveObject(doc, fontRefObj)
						if err != nil {
							return fmt.Errorf("failed to load font /%s: %w", fontName, ErrFontNotFound)
						}
						if fontDict, ok := fontDictResolved.(ast.PdfDict); ok {
							font, err := GetFont(doc, fontDict)
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
						xobjResolved, _ := document.ResolveObject(doc, xobjObj)
						if xobjDict, ok := xobjResolved.(ast.PdfDict); ok {
							if targetObj, ok := xobjDict[name]; ok {
								targetResolved, _ := document.ResolveObject(doc, targetObj)
								if stream, ok := targetResolved.(ast.PdfStream); ok {
									if subtypeObj, ok := stream.Dict["Subtype"]; ok {
										subtypeResolved, _ := document.ResolveObject(doc, subtypeObj)
										if subtypeName, ok := subtypeResolved.(ast.PdfName); ok && subtypeName == "Form" {
											// Parse the Form XObject stream
											formOps, err := content.ParsePageContent(doc, stream)
											if err == nil {
												// Determine resources
												formRes := resources // inherit by default
												if resObj, ok := stream.Dict["Resources"]; ok {
													resResolved, _ := document.ResolveObject(doc, resObj)
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
													matrixResolved, err := document.ResolveObject(doc, matrixObj)
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

												// Recurse
												if err := extractTextRecursive(doc, machine, formRes, formOps, elements); err != nil {
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
	StripInvisible bool // If true, completely omits text matched by invisibility heuristics
	StripHidden    bool // If true, completely omits text outside the clipping path
	StripArtifacts bool // If true, discards text inside /Artifact marked content (headers, footers, page numbers)
}

// ExtractElements extracts the text from a page's content operations as raw spatial elements.
func ExtractElements(doc *document.Document, machine *graphics.Machine, page *document.Page, ops []content.Operation) ([]models.TextElement, error) {
	var elements []models.TextElement
	if err := extractTextRecursive(doc, machine, page.Resources, ops, &elements); err != nil {
		return nil, err
	}
	return elements, nil
}

// ExtractText extracts the text from a page's content operations and reconstructs it.
func ExtractText(doc *document.Document, machine *graphics.Machine, page *document.Page, ops []content.Operation, config ExtractionConfig) (string, error) {
	machine.StripArtifacts = config.StripArtifacts

	elements, err := ExtractElements(doc, machine, page, ops)
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
		filtered = append(filtered, el)
	}

	var pageBeads []semantics.Bead
	allThreads, err := semantics.GetThreads(doc)
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
