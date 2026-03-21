package geospatial

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// ExtractGeoData scans a page dictionary and its Image XObjects for /Measure
// dictionaries with /Subtype /GEO. Returns nil if no geospatial data is found.
func ExtractGeoData(ctx context.Context, doc *document.Document, page *document.Page) ([]models.GeoData, error) {
	var results []models.GeoData

	// Check the page-level /VP (Viewport) array for geospatial viewports.
	if vpObj, ok := page.Dict["VP"]; ok {
		vpArr, err := document.ResolveArray(ctx, doc, vpObj)
		if err == nil {
			for _, vpEntry := range vpArr {
				vpDict, err := document.ResolveDict(ctx, doc, vpEntry)
				if err != nil {
					continue
				}
				if vpDict == nil {
					continue
				}
				if measureObj, ok := vpDict["Measure"]; ok {
					geo, err := parseMeasureDict(ctx, doc, measureObj)
					if err == nil && geo != nil {
						results = append(results, *geo)
					}
				}
			}
		}
	}

	// Check Image XObjects for /Measure dictionaries.
	if page.Resources != nil {
		if xobjObj, ok := page.Resources["XObject"]; ok {
			xobjDict, err := document.ResolveDict(ctx, doc, xobjObj)
			if err == nil {
				for _, obj := range xobjDict {
					stream, err := document.ResolveStream(ctx, doc, obj)
					if err != nil {
						continue
					}
					if stream.Dict == nil {
						continue
					}
					if measureObj, ok := stream.Dict["Measure"]; ok {
						geo, err := parseMeasureDict(ctx, doc, measureObj)
						if err == nil && geo != nil {
							results = append(results, *geo)
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
func parseMeasureDict(ctx context.Context, doc *document.Document, measureObj ast.PdfObject) (*models.GeoData, error) {
	measureDict, err := document.ResolveDict(ctx, doc, measureObj)
	if err != nil {
		return nil, err
	}
	if measureDict == nil {
		return nil, fmt.Errorf("/Measure is not a dictionary")
	}

	// Check /Subtype is /GEO.
	if subtypeObj, ok := measureDict["Subtype"]; ok {
		name, err := document.ResolveName(ctx, doc, subtypeObj)
		if err != nil {
			doc.Logger.Debug("resolving Measure subtype", slog.Any("err", err))
		}
		if name != "" {
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
		gcsDict, err := document.ResolveDict(ctx, doc, gcsObj)
		if err == nil {
			if gcsDict != nil {
				parseGCS(ctx, doc, gcsDict, geo)
			}
		}
	}

	// Extract /GPTS (Geospatial Tie Points) — array of lat/lon pairs.
	// Format: [lat1 lon1 lat2 lon2 ... latN lonN]
	if gptsObj, ok := measureDict["GPTS"]; ok {
		gptsArr, err := document.ResolveArray(ctx, doc, gptsObj)
		if err == nil {
			if gptsArr != nil {
				parseGPTS(ctx, doc, measureDict, gptsArr, geo)
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
func parseGCS(ctx context.Context, doc *document.Document, gcsDict ast.PdfDict, geo *models.GeoData) {
	// /Type — "GEOGCS", "PROJCS", etc.
	if typeObj, ok := gcsDict["Type"]; ok {
		name, err := document.ResolveName(ctx, doc, typeObj)
		if err != nil {
			doc.Logger.Debug("resolving GCS type", slog.Any("err", err))
		}
		if name != "" {
			geo.CoordinateSystem = string(name)
		}
	}

	// /EPSG — EPSG code.
	if epsgObj, ok := gcsDict["EPSG"]; ok {
		epsgRes, err := document.ResolveObject(ctx, doc, epsgObj)
		if err != nil {
			doc.Logger.Debug("resolving GCS EPSG", slog.Any("err", err))
		}
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
			wktStr, err := document.ResolveString(ctx, doc, wktObj)
			if err != nil {
				doc.Logger.Debug("resolving GCS WKT", slog.Any("err", err))
			}
			if wktStr != "" {
				geo.EPSG = extractEPSGFromWKT(string(wktStr))
			}
		}
	}
}

// parseGPTS extracts tie points from the /GPTS array and optionally pairs them
// with the /LPTS (PDF-space points) array.
func parseGPTS(ctx context.Context, doc *document.Document, measureDict ast.PdfDict, gptsArr ast.PdfArray, geo *models.GeoData) {
	// /LPTS (Local Points) — PDF user space coordinates matching GPTS.
	var lptsArr ast.PdfArray
	if lptsObj, ok := measureDict["LPTS"]; ok {
		arr, err := document.ResolveArray(ctx, doc, lptsObj)
		if err == nil {
			if arr != nil {
				lptsArr = arr
			}
		}
	}

	for i := 0; i+1 < len(gptsArr); i += 2 {
		lat := document.GetFloat(gptsArr[i])
		lon := document.GetFloat(gptsArr[i+1])

		tp := models.GeoTiePoint{Lat: lat, Lon: lon}

		// Match with LPTS if available.
		if lptsArr != nil && i+1 < len(lptsArr) {
			tp.PDFX = document.GetFloat(lptsArr[i])
			tp.PDFY = document.GetFloat(lptsArr[i+1])
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
