package models

import (
	"fmt"
	"strings"
)

// GeoTiePoint represents a single tie point mapping PDF coordinates to
// GPS coordinates (latitude/longitude).
// Concurrent reads of a value are safe; a write needs synchronization.
type GeoTiePoint struct {
	// PDFX is the x coordinate of the point in the page's /LPTS array, or 0
	// when the array has no entry for it.
	PDFX float64 `json:"pdfX"`
	// PDFY is the y coordinate of the point in the page's /LPTS array, or 0
	// when the array has no entry for it.
	PDFY float64 `json:"pdfY"`
	// Lat is the latitude of the point, from the /GPTS array.
	Lat float64 `json:"lat"`
	// Lon is the longitude of the point, from the /GPTS array.
	Lon float64 `json:"lon"`
}

// GeoData holds geospatial metadata extracted from a PDF page or image
// that contains a /Measure dictionary with /Subtype /GEO.
// Concurrent reads of a value are safe; a write needs synchronization.
type GeoData struct {
	// CoordinateSystem is the /Type of the /GCS dictionary, such as GEOGCS or
	// PROJCS.
	CoordinateSystem string `json:"coordinateSystem"`
	// EPSG is the EPSG code of the coordinate system, from the /GCS /EPSG
	// entry or its /WKT text, or empty when neither gives one.
	EPSG string `json:"epsg,omitempty"`
	// TiePoints are the points of the /GPTS array, in file order.
	TiePoints []GeoTiePoint `json:"tiePoints"`
	// Bounds is the box [minLon minLat maxLon maxLat] around TiePoints. It is
	// all zeros when there are none.
	Bounds [4]float64 `json:"bounds"`
}

// ToGeoJSON converts the geospatial data to a GeoJSON FeatureCollection
// string containing tie points as Point features and the bounding box as
// a Polygon feature.
func (g *GeoData) ToGeoJSON() string {
	var features []string

	for i, tp := range g.TiePoints {
		feature := fmt.Sprintf(
			`{"type":"Feature","id":%d,"geometry":{"type":"Point","coordinates":[%g,%g]},"properties":{"pdfX":%g,"pdfY":%g}}`,
			i, tp.Lon, tp.Lat, tp.PDFX, tp.PDFY,
		)
		features = append(features, feature)
	}

	if len(g.TiePoints) > 0 {
		b := g.Bounds
		bbox := fmt.Sprintf(
			`{"type":"Feature","id":"bbox","geometry":{"type":"Polygon","coordinates":[[`+
				`[%g,%g],[%g,%g],[%g,%g],[%g,%g],[%g,%g]`+
				`]]},"properties":{"type":"bounds","crs":"%s","epsg":"%s"}}`,
			b[0], b[1], b[2], b[1], b[2], b[3], b[0], b[3], b[0], b[1],
			g.CoordinateSystem, g.EPSG,
		)
		features = append(features, bbox)
	}

	return fmt.Sprintf(`{"type":"FeatureCollection","features":[%s]}`, strings.Join(features, ","))
}

// ScaleAxis describes the unit and conversion factor for one axis of a
// rectilinear measurement scale extracted from a /Measure /RL dictionary.
// Concurrent reads of a value are safe; a write needs synchronization.
type ScaleAxis struct {
	// Unit is the unit label, the /U entry of the number format.
	Unit string `json:"unit"`
	// ConversionFactor is the /C entry of the number format, the factor that
	// converts a value into Unit.
	ConversionFactor float64 `json:"conversionFactor"`
	// Decimals is the /D entry of the number format, the number of decimal
	// places to show. It is 0 when the entry is absent.
	Decimals int `json:"decimals"`
}

// Measurement holds the 2D measurement scale extracted from a page Viewport
// (/VP) or a Line/PolyLine annotation that carries a /Measure dictionary with
// /Subtype /RL (Rectilinear). Developers can use the scale axes to convert
// raw PDF point coordinates into real-world physical measurements.
// Concurrent reads of a value are safe; a write needs synchronization.
type Measurement struct {
	// Source says where the scale comes from: "Viewport" for a page viewport,
	// or the annotation subtype, "Line" or "PolyLine".
	Source string `json:"source"`
	// Name is the viewport's /Name or the annotation's /NM, or empty when it
	// has none.
	Name string `json:"name,omitempty"`
	// BBox is the viewport's /BBox or the annotation's /Rect, in default user
	// space.
	BBox [4]float64 `json:"bbox"`
	// ScaleX is the scale along the x axis.
	ScaleX ScaleAxis `json:"scaleX"`
	// ScaleY is the scale along the y axis. It equals ScaleX when the
	// dictionary has no /Y entry.
	ScaleY ScaleAxis `json:"scaleY"`
	// AreaUnit is the unit label for areas, or empty when the dictionary
	// gives none.
	AreaUnit string `json:"areaUnit,omitempty"`
}
