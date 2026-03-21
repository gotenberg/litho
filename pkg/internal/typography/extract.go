package typography

import (
	"context"
	"log/slog"
	"math"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/semantics"
	"github.com/gotenberg/litho/pkg/internal/typography/encodings"
	"github.com/gotenberg/litho/pkg/models"
)

// fallbackFont returns a minimal font that uses WinAnsiEncoding, matching
// a minimal Helvetica/WinAnsiEncoding font dictionary.
// This allows text extraction to continue when a font cannot be resolved,
// producing best-effort output rather than aborting the page.
func fallbackFont() *Font {
	return &Font{
		Subtype:  "Type1",
		Encoding: copyEncoding(encodings.WinAnsiEncoding),
	}
}

// maxFormXObjectDepth is the maximum recursion depth for nested Form XObjects.
// This prevents stack overflows and excessive processing time from circular
// references or deeply nested XObjects in malformed PDFs.
const maxFormXObjectDepth = 10

// collectAnnotAppearanceObjects returns the set of object numbers referenced by
// annotation appearance streams (/AP) on this page. These streams render visual
// overlays (stamps, widgets, markup) and are excluded from text extraction to
// avoid duplicating content that is not part of the page's semantic text flow.
func collectAnnotAppearanceObjects(ctx context.Context, doc *document.Document, pageDict ast.PdfDict) map[int]bool {
	result := make(map[int]bool)
	annotsObj, ok := pageDict["Annots"]
	if !ok {
		return result
	}
	annotsArr, err := document.ResolveArray(ctx, doc, annotsObj)
	if err != nil {
		return result
	}
	if annotsArr == nil {
		return result
	}
	for _, annotObj := range annotsArr {
		annotDict, err := document.ResolveDict(ctx, doc, annotObj)
		if err != nil {
			continue
		}
		if annotDict == nil {
			continue
		}
		apObj, ok := annotDict["AP"]
		if !ok {
			continue
		}
		apDict, err := document.ResolveDict(ctx, doc, apObj)
		if err != nil {
			continue
		}
		if apDict == nil {
			continue
		}
		// Collect object numbers from /N (normal), /R (rollover), /D (down) appearances.
		for _, key := range []ast.PdfName{"N", "R", "D"} {
			apEntry, ok := apDict[key]
			if !ok {
				continue
			}
			if ref, ok := apEntry.(ast.PdfIndirectRef); ok {
				result[ref.ObjectNum] = true
			}
			// An appearance can also be a dict of state-specific appearances.
			stateDict, err := document.ResolveDict(ctx, doc, apEntry)
			if err != nil {
				continue
			}
			for _, stateObj := range stateDict {
				if ref, ok := stateObj.(ast.PdfIndirectRef); ok {
					result[ref.ObjectNum] = true
				}
			}
		}
	}
	return result
}

// extractTextRecursive recursively extracts text from operations, supporting Form XObjects.
// The visited set tracks indirect reference object numbers to break circular references,
// and depth guards against non-circular but excessively deep nesting.
// annotAppearances contains object numbers of annotation appearance streams to skip.
func extractTextRecursive(ctx context.Context, doc *document.Document, machine *graphics.Machine, resources ast.PdfDict, ops []content.Operation, elements *[]models.TextElement, visited map[int]bool, depth int, annotAppearances map[int]bool) error {
	if depth > maxFormXObjectDepth {
		if doc != nil {
			doc.Logger.Debug("form xobject max depth reached",
				slog.Int("depth", depth),
			)
		}
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
		propsDict, err := document.ResolveDict(ctx, doc, propsObj)
		if err != nil {
			doc.Logger.Debug("resolving Properties dict", slog.Any("err", err))
		}
		for k, v := range propsDict {
			dict, err := document.ResolveDict(ctx, doc, v)
			if err != nil {
				doc.Logger.Debug("resolving Properties entry", slog.String("key", string(k)), slog.Any("err", err))
			}
			if dict != nil {
				if subtypeObj, ok := dict["Type"]; ok {
					subtypeName, err := document.ResolveName(ctx, doc, subtypeObj)
					if err != nil {
						doc.Logger.Debug("resolving OCG type name", slog.Any("err", err))
					}
					if subtypeName != "" && subtypeName == "OCG" {
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

	for opIdx, op := range ops {
		// Check for context cancellation every 100 operators to respect deadlines
		// without adding per-operator overhead.
		if opIdx%100 == 0 && opIdx > 0 { //nolint:mnd // amortized cancellation check — frequent enough to respect deadlines, rare enough to avoid overhead
			if err := ctx.Err(); err != nil {
				return err
			}
		}
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
				resDictRoot, err := document.ResolveDict(ctx, doc, resObj)
				if err != nil {
					doc.Logger.Debug("resolving Font resource dict", slog.Any("err", err))
				}
				if resDictRoot != nil {
					if fontRefObj, ok := resDictRoot[fontName]; ok {
						// Check font cache by indirect reference object number.
						if ref, ok := fontRefObj.(ast.PdfIndirectRef); ok && doc != nil {
							if cached, ok := doc.GetCachedFont(ref.ObjectNum); ok {
								if f, ok := cached.(*Font); ok {
									currentFont = f
									resolved = true
								}
							}
						}
						if !resolved {
							fontDict, err := document.ResolveDict(ctx, doc, fontRefObj)
							if err == nil {
								if fontDict != nil {
									font, err := GetFont(ctx, doc, fontDict)
									if err == nil {
										currentFont = font
										resolved = true
										// Store in font cache.
										if ref, ok := fontRefObj.(ast.PdfIndirectRef); ok && doc != nil {
											doc.SetCachedFont(ref.ObjectNum, font)
										}
									}
								}
							}
						}
					}
				}
			}
			if !resolved {
				// Fall back to a default font rather than aborting the
				// entire page. Text may be garbled but extraction continues,
				// to produce best-effort output rather than aborting the page.
				if doc != nil {
					doc.Logger.Debug("font not found, using fallback",
						slog.String("font", string(fontName)),
					)
				}
				currentFont = fallbackFont()
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
						xobjDict, err := document.ResolveDict(ctx, doc, xobjObj)
						if err != nil {
							doc.Logger.Debug("resolving XObject dict for Do", slog.Any("err", err))
						}
						if xobjDict != nil {
							if targetObj, ok := xobjDict[name]; ok {
								// Track visited Form XObjects by indirect reference to break circular references.
								objNum := 0
								if ref, ok := targetObj.(ast.PdfIndirectRef); ok {
									objNum = ref.ObjectNum
									if visited[objNum] {
										continue
									}
									// Skip annotation appearance streams — they are visual overlays, not page content.
									if annotAppearances[objNum] {
										continue
									}
								}

								stream, err := document.ResolveStream(ctx, doc, targetObj)
								if err != nil {
									doc.Logger.Debug("resolving Form XObject stream", slog.Any("err", err))
								}
								if stream.Dict != nil {
									if subtypeObj, ok := stream.Dict["Subtype"]; ok {
										subtypeName, err := document.ResolveName(ctx, doc, subtypeObj)
										if err != nil {
											doc.Logger.Debug("resolving XObject subtype", slog.Any("err", err))
										}
										if subtypeName == "Form" {
											// Parse the Form XObject stream
											formOps, err := content.ParsePageContent(ctx, doc, stream)
											if err == nil {
												// Determine resources
												formRes := resources // inherit by default
												if resObj, ok := stream.Dict["Resources"]; ok {
													resDict, err := document.ResolveDict(ctx, doc, resObj)
													if err != nil {
														doc.Logger.Debug("resolving Form XObject resources", slog.Any("err", err))
													}
													if resDict != nil {
														formRes = resDict
													}
												}

												// Graphics state save/restore and matrix transform: these trivial
												// operators cannot fail in practice, and this function does not
												// return errors, so suppression is justified.
												_ = machine.Execute([]content.Operation{{Operator: "q"}}) //nolint:errcheck

												// If there is a Matrix in the Form XObject, apply it
												if matrixObj, ok := stream.Dict["Matrix"]; ok {
													matrixArr, err := document.ResolveArray(ctx, doc, matrixObj)
													if err != nil {
														return err
													}
													if len(matrixArr) == 6 {
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
												err = extractTextRecursive(ctx, doc, machine, formRes, formOps, elements, visited, depth+1, annotAppearances)
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
	// Collect annotation appearance object numbers so we can skip them during
	// text extraction — annotation appearances are visual overlays, not page content.
	annotAppearances := collectAnnotAppearanceObjects(ctx, doc, page.Dict)

	// Pre-allocate with a capacity estimate: roughly 1 text element per 3 operators.
	estimatedCap := max(len(ops)/3,
		//nolint:mnd // minimum capacity to avoid early reallocations
		64)
	elements := make([]models.TextElement, 0, estimatedCap)
	if err := extractTextRecursive(ctx, doc, machine, page.Resources, ops, &elements, make(map[int]bool), 0, annotAppearances); err != nil {
		return nil, err
	}

	if doc != nil {
		doc.Logger.Debug("text elements extracted",
			slog.Int("count", len(elements)),
			slog.Int("operators", len(ops)),
			slog.Int("annot_appearances_skipped", len(annotAppearances)),
		)
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

	if doc != nil {
		doc.Logger.Debug("text elements filtered",
			slog.Int("before", len(elements)),
			slog.Int("after", len(filtered)),
			slog.Bool("strip_invisible", config.StripInvisible),
			slog.Bool("strip_hidden", config.StripHidden),
			slog.Bool("strip_clipped", config.StripClipped),
			slog.Bool("strip_artifacts", config.StripArtifacts),
		)
	}

	var pageBeads []semantics.Bead
	allThreads, _ := semantics.GetThreads(ctx, doc) // Non-fatal: proceed without thread reordering on error.
	for _, thread := range allThreads {
		for _, bead := range thread.Beads {
			if bead.PageRef.ObjectNum == page.Ref.ObjectNum && bead.PageRef.GenNum == page.Ref.GenNum {
				pageBeads = append(pageBeads, bead)
			}
		}
	}

	return ReconstructText(filtered, pageBeads), nil
}
