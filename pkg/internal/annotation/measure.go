package annotation

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
		vpArr, err := document.ResolveArray(ctx, doc, vpObj)
		if err == nil && vpArr != nil {
			for _, vpEntry := range vpArr {
				vpDict, err := document.ResolveDict(ctx, doc, vpEntry)
				if err != nil || vpDict == nil {
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
					str, err := document.ResolveString(ctx, doc, nameObj)
					if err == nil && str != "" {
						m.Name = document.DecodePdfString(str)
					}
				}

				// Extract viewport /BBox.
				if bboxObj, ok := vpDict["BBox"]; ok {
					bboxArr, err := document.ResolveArray(ctx, doc, bboxObj)
					if err == nil && bboxArr != nil && len(bboxArr) >= 4 {
						for i := range 4 {
							m.BBox[i] = document.GetFloat(bboxArr[i])
						}
					}
				}

				results = append(results, *m)
			}
		}
	}

	// 2. Check annotations for Line/PolyLine with /Measure.
	if annotsObj, ok := page.Dict["Annots"]; ok {
		annotsArr, err := document.ResolveArray(ctx, doc, annotsObj)
		if err == nil && annotsArr != nil {
			for _, annotRef := range annotsArr {
				annotDict, err := document.ResolveDict(ctx, doc, annotRef)
				if err != nil || annotDict == nil {
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
					str, err := document.ResolveString(ctx, doc, nmObj)
					if err == nil && str != "" {
						m.Name = document.DecodePdfString(str)
					}
				}

				// Use annotation /Rect as bounding box.
				if rectObj, ok := annotDict["Rect"]; ok {
					rectArr, err := document.ResolveArray(ctx, doc, rectObj)
					if err == nil && rectArr != nil && len(rectArr) >= 4 {
						for i := range 4 {
							m.BBox[i] = document.GetFloat(rectArr[i])
						}
					}
				}

				results = append(results, *m)
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
	measureDict, err := document.ResolveDict(ctx, doc, measureObj)
	if err != nil {
		return nil, err
	}
	if measureDict == nil {
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
	arr, err := document.ResolveArray(ctx, doc, obj)
	if err != nil || arr == nil {
		return models.ScaleAxis{}
	}

	if len(arr) == 0 {
		return models.ScaleAxis{}
	}

	// Take the first number format dictionary.
	nfDict, err := document.ResolveDict(ctx, doc, arr[0])
	if err != nil || nfDict == nil {
		return models.ScaleAxis{}
	}

	var axis models.ScaleAxis

	// /U — unit label string.
	if uObj, ok := nfDict["U"]; ok {
		str, err := document.ResolveString(ctx, doc, uObj)
		if err == nil && str != "" {
			axis.Unit = document.DecodePdfString(str)
		}
	}

	// /C — conversion factor (float).
	if cObj, ok := nfDict["C"]; ok {
		axis.ConversionFactor = document.GetFloat(cObj)
	}

	// /D — decimal places (integer, defaults to 0).
	if dObj, ok := nfDict["D"]; ok {
		dInt, err := document.ResolveInt(ctx, doc, dObj)
		if err == nil && dInt != 0 {
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
	name, err := document.ResolveName(ctx, doc, obj)
	if err != nil {
		return ""
	}
	return string(name)
}
