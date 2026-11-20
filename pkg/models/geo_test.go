package models

import (
	"encoding/json"
	"testing"
)

func TestGeoTiePoint_JSONRoundTrip(t *testing.T) {
	assertRoundTrip(t, GeoTiePoint{PDFX: 100, PDFY: 200, Lat: 48.8566, Lon: 2.3522})
}

func TestGeoData_JSONRoundTrip(t *testing.T) {
	assertRoundTrip(t, GeoData{
		CoordinateSystem: "WGS84",
		EPSG:             "EPSG:4326",
		TiePoints: []GeoTiePoint{
			{PDFX: 0, PDFY: 0, Lat: 48.85, Lon: 2.35},
			{PDFX: 100, PDFY: 100, Lat: 48.86, Lon: 2.36},
		},
		Bounds: [4]float64{0, 0, 100, 100},
	})
}

func TestGeoData_ToGeoJSON(t *testing.T) {
	g := GeoData{
		CoordinateSystem: "WGS84",
		EPSG:             "EPSG:4326",
		TiePoints: []GeoTiePoint{
			{PDFX: 0, PDFY: 0, Lat: 48.85, Lon: 2.35},
		},
		Bounds: [4]float64{0, 0, 612, 792},
	}
	got := g.ToGeoJSON()

	// ToGeoJSON builds JSON with fmt.Sprintf, so re-parsing is the only
	// way to verify it is actually well-formed.
	var parsed struct {
		Type     string `json:"type"`
		Features []any  `json:"features"`
	}
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("ToGeoJSON produced invalid JSON: %v\n%s", err, got)
	}
	if parsed.Type != "FeatureCollection" {
		t.Errorf("type: got %q, want FeatureCollection", parsed.Type)
	}
	if len(parsed.Features) != 2 {
		t.Errorf("features: got %d, want 2 (1 tie point + bbox)", len(parsed.Features))
	}
}

func TestGeoData_ToGeoJSON_NoTiePoints(t *testing.T) {
	g := GeoData{CoordinateSystem: "WGS84"}
	got := g.ToGeoJSON()
	var parsed struct {
		Type     string `json:"type"`
		Features []any  `json:"features"`
	}
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("ToGeoJSON produced invalid JSON: %v\n%s", err, got)
	}
	if len(parsed.Features) != 0 {
		t.Errorf("features: got %d, want 0 when no tie points", len(parsed.Features))
	}
}

func TestScaleAxis_JSONRoundTrip(t *testing.T) {
	assertRoundTrip(t, ScaleAxis{Unit: "m", ConversionFactor: 0.0254, Decimals: 2})
}

func TestMeasurement_JSONRoundTrip(t *testing.T) {
	assertRoundTrip(t, Measurement{
		Source:   "page",
		Name:     "Plan scale",
		BBox:     [4]float64{0, 0, 612, 792},
		ScaleX:   ScaleAxis{Unit: "m", ConversionFactor: 0.01, Decimals: 2},
		ScaleY:   ScaleAxis{Unit: "m", ConversionFactor: 0.01, Decimals: 2},
		AreaUnit: "m^2",
	})
}
