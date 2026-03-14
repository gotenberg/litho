package semantics

import (
	"fmt"
	"math"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// ExtractGeoData scans a page dictionary and its Image XObjects for /Measure
// dictionaries with /Subtype /GEO. Returns nil if no geospatial data is found.
func ExtractGeoData(doc *document.Document, page *document.Page) ([]models.GeoData, error) {
	var results []models.GeoData

	// Check the page-level /VP (Viewport) array for geospatial viewports.
	if vpObj, ok := page.Dict["VP"]; ok {
		vpResolved, err := resolveObject(doc, vpObj)
		if err == nil {
			if vpArr, ok := vpResolved.(ast.PdfArray); ok {
				for _, vpEntry := range vpArr {
					vpRes, err := resolveObject(doc, vpEntry)
					if err != nil {
						continue
					}
					vpDict, ok := vpRes.(ast.PdfDict)
					if !ok {
						continue
					}
					if measureObj, ok := vpDict["Measure"]; ok {
						geo, err := parseMeasureDict(doc, measureObj)
						if err == nil && geo != nil {
							results = append(results, *geo)
						}
					}
				}
			}
		}
	}

	// Check Image XObjects for /Measure dictionaries.
	if page.Resources != nil {
		if xobjObj, ok := page.Resources["XObject"]; ok {
			xobjResolved, err := resolveObject(doc, xobjObj)
			if err == nil {
				if xobjDict, ok := xobjResolved.(ast.PdfDict); ok {
					for _, obj := range xobjDict {
						resolved, err := resolveObject(doc, obj)
						if err != nil {
							continue
						}
						stream, ok := resolved.(ast.PdfStream)
						if !ok {
							continue
						}
						if measureObj, ok := stream.Dict["Measure"]; ok {
							geo, err := parseMeasureDict(doc, measureObj)
							if err == nil && geo != nil {
								results = append(results, *geo)
							}
						}
					}
				}
			}
		}
	}

	if len(results) == 0 {
		return nil, nil
	}
	return results, nil
}

// parseMeasureDict resolves and parses a /Measure dictionary, extracting
// geospatial data if /Subtype is /GEO.
func parseMeasureDict(doc *document.Document, measureObj ast.PdfObject) (*models.GeoData, error) {
	measureResolved, err := resolveObject(doc, measureObj)
	if err != nil {
		return nil, err
	}

	measureDict, ok := measureResolved.(ast.PdfDict)
	if !ok {
		return nil, fmt.Errorf("/Measure is not a dictionary")
	}

	// Check /Subtype is /GEO.
	if subtypeObj, ok := measureDict["Subtype"]; ok {
		subtypeRes, _ := resolveObject(doc, subtypeObj)
		if name, ok := subtypeRes.(ast.PdfName); ok {
			if name != "GEO" {
				return nil, nil
			}
		}
	} else {
		return nil, nil
	}

	geo := &models.GeoData{}

	// Extract /GCS (Geographic Coordinate System).
	if gcsObj, ok := measureDict["GCS"]; ok {
		gcsRes, err := resolveObject(doc, gcsObj)
		if err == nil {
			if gcsDict, ok := gcsRes.(ast.PdfDict); ok {
				parseGCS(doc, gcsDict, geo)
			}
		}
	}

	// Extract /GPTS (Geospatial Tie Points) — array of lat/lon pairs.
	// Format: [lat1 lon1 lat2 lon2 ... latN lonN]
	if gptsObj, ok := measureDict["GPTS"]; ok {
		gptsRes, err := resolveObject(doc, gptsObj)
		if err == nil {
			if gptsArr, ok := gptsRes.(ast.PdfArray); ok {
				parseGPTS(doc, measureDict, gptsArr, geo)
			}
		}
	}

	// Compute bounding box from tie points.
	if len(geo.TiePoints) > 0 {
		geo.Bounds = computeGeoBounds(geo.TiePoints)
	}

	return geo, nil
}

// parseGCS extracts coordinate system info from a /GCS dictionary.
func parseGCS(doc *document.Document, gcsDict ast.PdfDict, geo *models.GeoData) {
	// /Type — "GEOGCS", "PROJCS", etc.
	if typeObj, ok := gcsDict["Type"]; ok {
		typeRes, _ := resolveObject(doc, typeObj)
		if name, ok := typeRes.(ast.PdfName); ok {
			geo.CoordinateSystem = string(name)
		}
	}

	// /EPSG — EPSG code.
	if epsgObj, ok := gcsDict["EPSG"]; ok {
		epsgRes, _ := resolveObject(doc, epsgObj)
		switch v := epsgRes.(type) {
		case ast.PdfInt:
			geo.EPSG = fmt.Sprintf("%d", int(v))
		case ast.PdfString:
			geo.EPSG = string(v)
		}
	}

	// /WKT — Well-Known Text may contain EPSG info.
	if geo.EPSG == "" {
		if wktObj, ok := gcsDict["WKT"]; ok {
			wktRes, _ := resolveObject(doc, wktObj)
			if wktStr, ok := wktRes.(ast.PdfString); ok {
				geo.EPSG = extractEPSGFromWKT(string(wktStr))
			}
		}
	}
}

// parseGPTS extracts tie points from the /GPTS array and optionally pairs them
// with the /LPTS (PDF-space points) array.
func parseGPTS(doc *document.Document, measureDict ast.PdfDict, gptsArr ast.PdfArray, geo *models.GeoData) {
	// /LPTS (Local Points) — PDF user space coordinates matching GPTS.
	var lptsArr ast.PdfArray
	if lptsObj, ok := measureDict["LPTS"]; ok {
		lptsRes, err := resolveObject(doc, lptsObj)
		if err == nil {
			if arr, ok := lptsRes.(ast.PdfArray); ok {
				lptsArr = arr
			}
		}
	}

	for i := 0; i+1 < len(gptsArr); i += 2 {
		lat := getFloat(gptsArr[i])
		lon := getFloat(gptsArr[i+1])

		tp := models.GeoTiePoint{Lat: lat, Lon: lon}

		// Match with LPTS if available.
		if lptsArr != nil && i+1 < len(lptsArr) {
			tp.PDFX = getFloat(lptsArr[i])
			tp.PDFY = getFloat(lptsArr[i+1])
		}

		geo.TiePoints = append(geo.TiePoints, tp)
	}
}

// computeGeoBounds calculates the bounding box [minLon, minLat, maxLon, maxLat]
// from a set of tie points.
func computeGeoBounds(tps []models.GeoTiePoint) [4]float64 {
	minLon, minLat := math.MaxFloat64, math.MaxFloat64
	maxLon, maxLat := -math.MaxFloat64, -math.MaxFloat64

	for _, tp := range tps {
		if tp.Lon < minLon {
			minLon = tp.Lon
		}
		if tp.Lon > maxLon {
			maxLon = tp.Lon
		}
		if tp.Lat < minLat {
			minLat = tp.Lat
		}
		if tp.Lat > maxLat {
			maxLat = tp.Lat
		}
	}

	return [4]float64{minLon, minLat, maxLon, maxLat}
}

// extractEPSGFromWKT attempts to extract an EPSG code from a WKT string.
// WKT strings often end with AUTHORITY["EPSG","4326"].
func extractEPSGFromWKT(wkt string) string {
	idx := strings.LastIndex(wkt, `AUTHORITY["EPSG"`)
	if idx == -1 {
		idx = strings.LastIndex(wkt, `AUTHORITY["epsg"`)
	}
	if idx == -1 {
		return ""
	}

	// Find the code between the second set of quotes.
	sub := wkt[idx:]
	start := strings.Index(sub, `,"`)
	if start == -1 {
		return ""
	}
	sub = sub[start+2:]
	before, _, ok := strings.Cut(sub, `"`)
	if !ok {
		return ""
	}
	return before
}

// GeoDataToGeoJSON converts extracted geospatial data to a GeoJSON
// FeatureCollection string containing the tie points and bounding box.
func GeoDataToGeoJSON(geo *models.GeoData) string {
	var features []string

	// Add tie points as Point features.
	for i, tp := range geo.TiePoints {
		feature := fmt.Sprintf(
			`{"type":"Feature","id":%d,"geometry":{"type":"Point","coordinates":[%g,%g]},"properties":{"pdfX":%g,"pdfY":%g}}`,
			i, tp.Lon, tp.Lat, tp.PDFX, tp.PDFY,
		)
		features = append(features, feature)
	}

	// Add bounding box as a Polygon feature.
	if len(geo.TiePoints) > 0 {
		b := geo.Bounds
		bbox := fmt.Sprintf(
			`{"type":"Feature","id":"bbox","geometry":{"type":"Polygon","coordinates":[[`+
				`[%g,%g],[%g,%g],[%g,%g],[%g,%g],[%g,%g]`+
				`]]},"properties":{"type":"bounds","crs":"%s","epsg":"%s"}}`,
			b[0], b[1], b[2], b[1], b[2], b[3], b[0], b[3], b[0], b[1],
			geo.CoordinateSystem, geo.EPSG,
		)
		features = append(features, bbox)
	}

	return fmt.Sprintf(`{"type":"FeatureCollection","features":[%s]}`, strings.Join(features, ","))
}
