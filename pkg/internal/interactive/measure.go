package interactive

import (
	"context"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// ExtractMeasurements scans a page for 2D rectilinear measurement scales.
// It checks the /VP (Viewport) array and Line/PolyLine annotations for
// /Measure dictionaries with /Subtype /RL.
func ExtractMeasurements(ctx context.Context, doc *document.Document, page *document.Page) ([]models.Measurement, error) {
	var results []models.Measurement

	// 1. Check page-level /VP (Viewport) array.
	if vpObj, ok := page.Dict["VP"]; ok {
		vpResolved, err := document.ResolveObject(ctx, doc, vpObj)
		if err == nil {
			if vpArr, ok := vpResolved.(ast.PdfArray); ok {
				for _, vpEntry := range vpArr {
					vpRes, err := document.ResolveObject(ctx, doc, vpEntry)
					if err != nil {
						continue
					}
					vpDict, ok := vpRes.(ast.PdfDict)
					if !ok {
						continue
					}

					measureObj, ok := vpDict["Measure"]
					if !ok {
						continue
					}

					m, err := parseRLMeasure(ctx, doc, measureObj)
					if err != nil || m == nil {
						continue
					}

					m.Source = "Viewport"

					// Extract viewport /Name.
					if nameObj, ok := vpDict["Name"]; ok {
						nameRes, _ := document.ResolveObject(ctx, doc, nameObj)
						if str, ok := nameRes.(ast.PdfString); ok {
							m.Name = document.DecodePdfString(str)
						}
					}

					// Extract viewport /BBox.
					if bboxObj, ok := vpDict["BBox"]; ok {
						bboxRes, _ := document.ResolveObject(ctx, doc, bboxObj)
						if bboxArr, ok := bboxRes.(ast.PdfArray); ok && len(bboxArr) >= 4 {
							for i := range 4 {
								m.BBox[i] = document.GetFloat(bboxArr[i])
							}
						}
					}

					results = append(results, *m)
				}
			}
		}
	}

	// 2. Check annotations for Line/PolyLine with /Measure.
	if annotsObj, ok := page.Dict["Annots"]; ok {
		annotsResolved, err := document.ResolveObject(ctx, doc, annotsObj)
		if err == nil {
			if annotsArr, ok := annotsResolved.(ast.PdfArray); ok {
				for _, annotRef := range annotsArr {
					annotRes, err := document.ResolveObject(ctx, doc, annotRef)
					if err != nil {
						continue
					}
					annotDict, ok := annotRes.(ast.PdfDict)
					if !ok {
						continue
					}

					// Only process Line and PolyLine subtypes.
					subtype := resolveNameStr(ctx, doc, annotDict, "Subtype")
					if subtype != "Line" && subtype != "PolyLine" {
						continue
					}

					measureObj, ok := annotDict["Measure"]
					if !ok {
						continue
					}

					m, err := parseRLMeasure(ctx, doc, measureObj)
					if err != nil || m == nil {
						continue
					}

					m.Source = subtype

					// Use annotation /NM as name.
					if nmObj, ok := annotDict["NM"]; ok {
						nmRes, _ := document.ResolveObject(ctx, doc, nmObj)
						if str, ok := nmRes.(ast.PdfString); ok {
							m.Name = document.DecodePdfString(str)
						}
					}

					// Use annotation /Rect as bounding box.
					if rectObj, ok := annotDict["Rect"]; ok {
						rectRes, _ := document.ResolveObject(ctx, doc, rectObj)
						if rectArr, ok := rectRes.(ast.PdfArray); ok && len(rectArr) >= 4 {
							for i := range 4 {
								m.BBox[i] = document.GetFloat(rectArr[i])
							}
						}
					}

					results = append(results, *m)
				}
			}
		}
	}

	if len(results) == 0 {
		return nil, nil
	}
	return results, nil
}

// parseRLMeasure parses a /Measure dictionary and extracts scale data if
// /Subtype is /RL (Rectilinear). Returns nil if the subtype is not /RL.
func parseRLMeasure(ctx context.Context, doc *document.Document, measureObj ast.PdfObject) (*models.Measurement, error) {
	measureRes, err := document.ResolveObject(ctx, doc, measureObj)
	if err != nil {
		return nil, err
	}
	measureDict, ok := measureRes.(ast.PdfDict)
	if !ok {
		return nil, nil
	}

	// Check /Subtype is /RL.
	subtype := resolveNameStr(ctx, doc, measureDict, "Subtype")
	if subtype != "RL" {
		return nil, nil
	}

	m := &models.Measurement{}

	// Extract /X axis scale.
	if xObj, ok := measureDict["X"]; ok {
		m.ScaleX = parseNumberFormatArray(ctx, doc, xObj)
	}

	// Extract /Y axis scale (if absent, Y defaults to same as X per spec).
	if yObj, ok := measureDict["Y"]; ok {
		m.ScaleY = parseNumberFormatArray(ctx, doc, yObj)
	} else {
		m.ScaleY = m.ScaleX
	}

	// Extract /A area unit.
	if aObj, ok := measureDict["A"]; ok {
		area := parseNumberFormatArray(ctx, doc, aObj)
		m.AreaUnit = area.Unit
	}

	return m, nil
}

// parseNumberFormatArray extracts the first number format dictionary from an
// array and returns the unit, conversion factor, and decimal count.
// PDF spec: the value is an array of Number Format dictionaries; we take
// the first entry which defines the primary display format.
func parseNumberFormatArray(ctx context.Context, doc *document.Document, obj ast.PdfObject) models.ScaleAxis {
	resolved, err := document.ResolveObject(ctx, doc, obj)
	if err != nil {
		return models.ScaleAxis{}
	}

	arr, ok := resolved.(ast.PdfArray)
	if !ok {
		return models.ScaleAxis{}
	}

	if len(arr) == 0 {
		return models.ScaleAxis{}
	}

	// Take the first number format dictionary.
	nfRes, err := document.ResolveObject(ctx, doc, arr[0])
	if err != nil {
		return models.ScaleAxis{}
	}
	nfDict, ok := nfRes.(ast.PdfDict)
	if !ok {
		return models.ScaleAxis{}
	}

	var axis models.ScaleAxis

	// /U — unit label string.
	if uObj, ok := nfDict["U"]; ok {
		uRes, _ := document.ResolveObject(ctx, doc, uObj)
		if str, ok := uRes.(ast.PdfString); ok {
			axis.Unit = document.DecodePdfString(str)
		}
	}

	// /C — conversion factor (float).
	if cObj, ok := nfDict["C"]; ok {
		axis.ConversionFactor = document.GetFloat(cObj)
	}

	// /D — decimal places (integer, defaults to 0).
	if dObj, ok := nfDict["D"]; ok {
		dRes, _ := document.ResolveObject(ctx, doc, dObj)
		if dInt, ok := dRes.(ast.PdfInt); ok {
			axis.Decimals = int(dInt)
		}
	}

	return axis
}

// resolveNameStr is a small helper that resolves a PdfName from a dictionary.
func resolveNameStr(ctx context.Context, doc *document.Document, dict ast.PdfDict, key ast.PdfName) string {
	obj, ok := dict[key]
	if !ok {
		return ""
	}
	res, err := document.ResolveObject(ctx, doc, obj)
	if err != nil {
		return ""
	}
	if name, ok := res.(ast.PdfName); ok {
		return string(name)
	}
	return ""
}
