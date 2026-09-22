package geospatial

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// geoMeasure builds a /Measure dictionary with /Subtype /GEO, a coordinate
// system and paired tie points.
//
// GPTS holds latitude/longitude pairs and LPTS the PDF-space x/y pairs that
// match them by index, per ISO 32000-1, table 267.
func geoMeasure(gcs ast.PdfDict) ast.PdfDict {
	return ast.PdfDict{
		"Subtype": ast.PdfName("GEO"),
		"GCS":     gcs,
		"GPTS":    ast.PdfArray{ast.PdfInt(10), ast.PdfInt(20), ast.PdfInt(30), ast.PdfInt(40)},
		"LPTS":    ast.PdfArray{ast.PdfInt(1), ast.PdfInt(2), ast.PdfInt(3), ast.PdfInt(4)},
	}
}

func viewportPage(measure ast.PdfDict) *document.Page {
	return &document.Page{
		Dict: ast.PdfDict{
			"Type": ast.PdfName("Page"),
			"VP": ast.PdfArray{
				ast.PdfDict{
					"Type":    ast.PdfName("Viewport"),
					"BBox":    ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(612), ast.PdfInt(792)},
					"Measure": measure,
				},
			},
		},
	}
}

// TestExtractGeoData_ViewportGEO covers the whole document-driven path, which
// no test reached: measured at 0.0% for ExtractGeoData, parseMeasureDict,
// parseGCS and parseGPTS, while the package's pure helpers (computeGeoBounds,
// extractEPSGFromWKT, GeoDataToGeoJSON) were fully covered. The corpus cannot
// close this: it carries no geospatial data at all, and tests must not depend
// on it, so a constructed page is the only route.
func TestExtractGeoData_ViewportGEO(t *testing.T) {
	page := viewportPage(geoMeasure(ast.PdfDict{
		"Type": ast.PdfName("GEOGCS"),
		"EPSG": ast.PdfInt(4326),
	}))

	got, err := ExtractGeoData(context.Background(), nil, page)
	if err != nil {
		t.Fatalf("ExtractGeoData: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d GeoData, want 1", len(got))
	}
	geo := got[0]

	if geo.CoordinateSystem != "GEOGCS" {
		t.Errorf("CoordinateSystem = %q, want %q", geo.CoordinateSystem, "GEOGCS")
	}
	if geo.EPSG != "4326" {
		t.Errorf("EPSG = %q, want %q: an integer /EPSG is formatted as a decimal", geo.EPSG, "4326")
	}

	// GPTS is [lat lon lat lon] and LPTS [x y x y], paired by index.
	if len(geo.TiePoints) != 2 {
		t.Fatalf("got %d tie points, want 2", len(geo.TiePoints))
	}
	for i, want := range []struct{ lat, lon, x, y float64 }{
		{10, 20, 1, 2},
		{30, 40, 3, 4},
	} {
		tp := geo.TiePoints[i]
		if tp.Lat != want.lat || tp.Lon != want.lon {
			t.Errorf("tie point %d = (lat %g, lon %g), want (%g, %g)", i, tp.Lat, tp.Lon, want.lat, want.lon)
		}
		if tp.PDFX != want.x || tp.PDFY != want.y {
			t.Errorf("tie point %d PDF = (%g, %g), want (%g, %g)", i, tp.PDFX, tp.PDFY, want.x, want.y)
		}
	}

	// computeGeoBounds returns [minLon, minLat, maxLon, maxLat], which is
	// longitude-first and therefore easy to transpose.
	if want := [4]float64{20, 10, 40, 30}; geo.Bounds != want {
		t.Errorf("Bounds = %v, want %v", geo.Bounds, want)
	}
}

// TestExtractGeoData_EPSGFromWKT pins the fallback: /EPSG wins, and only when
// it is absent is the WKT string mined for an authority code.
func TestExtractGeoData_EPSGFromWKT(t *testing.T) {
	page := viewportPage(geoMeasure(ast.PdfDict{
		"Type": ast.PdfName("GEOGCS"),
		"WKT":  ast.PdfString(`GEOGCS["WGS 84",AUTHORITY["EPSG","4326"]]`),
	}))

	got, err := ExtractGeoData(context.Background(), nil, page)
	if err != nil {
		t.Fatalf("ExtractGeoData: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d GeoData, want 1", len(got))
	}
	if got[0].EPSG != "4326" {
		t.Errorf("EPSG = %q, want %q from the WKT AUTHORITY clause", got[0].EPSG, "4326")
	}
}

// TestExtractGeoData_XObjectMeasure covers the second source: a /Measure on an
// image XObject rather than on a page viewport.
func TestExtractGeoData_XObjectMeasure(t *testing.T) {
	page := &document.Page{
		Dict: ast.PdfDict{"Type": ast.PdfName("Page")},
		Resources: ast.PdfDict{
			"XObject": ast.PdfDict{
				"Im0": ast.PdfStream{
					Dict: ast.PdfDict{
						"Type":    ast.PdfName("XObject"),
						"Subtype": ast.PdfName("Image"),
						"Measure": geoMeasure(ast.PdfDict{"Type": ast.PdfName("PROJCS")}),
					},
				},
			},
		},
	}

	got, err := ExtractGeoData(context.Background(), nil, page)
	if err != nil {
		t.Fatalf("ExtractGeoData: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d GeoData, want 1", len(got))
	}
	if got[0].CoordinateSystem != "PROJCS" {
		t.Errorf("CoordinateSystem = %q, want %q", got[0].CoordinateSystem, "PROJCS")
	}
}

// TestExtractGeoData_NotGeo is the control. parseMeasureDict returns no data
// for a /Measure that is not /GEO and for one stating no /Subtype at all, so a
// test asserting only the positive case would pass on a build that returned
// every measurement it found.
func TestExtractGeoData_NotGeo(t *testing.T) {
	for _, tc := range []struct {
		name    string
		measure ast.PdfDict
	}{
		{"rectilinear subtype", ast.PdfDict{
			"Subtype": ast.PdfName("RL"),
			"GPTS":    ast.PdfArray{ast.PdfInt(10), ast.PdfInt(20)},
		}},
		{"no subtype", ast.PdfDict{
			"GPTS": ast.PdfArray{ast.PdfInt(10), ast.PdfInt(20)},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ExtractGeoData(context.Background(), nil, viewportPage(tc.measure))
			if err != nil {
				t.Fatalf("ExtractGeoData: %v", err)
			}
			if got != nil {
				t.Errorf("got %v, want nil for a non-GEO measurement", got)
			}
		})
	}
}

// TestExtractGeoData_NoSources pins that a page declaring neither a viewport
// nor an XObject yields nil rather than an empty non-nil slice, which is what
// [pdf.Reader.ExtractGeoData] documents as "no geospatial data".
func TestExtractGeoData_NoSources(t *testing.T) {
	page := &document.Page{Dict: ast.PdfDict{"Type": ast.PdfName("Page")}}

	got, err := ExtractGeoData(context.Background(), nil, page)
	if err != nil {
		t.Fatalf("ExtractGeoData: %v", err)
	}
	if got != nil {
		t.Errorf("got %v, want nil", got)
	}
}
