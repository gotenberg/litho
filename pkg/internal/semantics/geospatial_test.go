package semantics

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

func TestComputeGeoBounds(t *testing.T) {
	tps := []models.GeoTiePoint{
		{Lat: 40.0, Lon: -74.0},
		{Lat: 41.0, Lon: -73.0},
		{Lat: 39.5, Lon: -74.5},
	}

	bounds := computeGeoBounds(tps)

	if bounds[0] != -74.5 {
		t.Errorf("minLon: got %g, want -74.5", bounds[0])
	}
	if bounds[1] != 39.5 {
		t.Errorf("minLat: got %g, want 39.5", bounds[1])
	}
	if bounds[2] != -73.0 {
		t.Errorf("maxLon: got %g, want -73.0", bounds[2])
	}
	if bounds[3] != 41.0 {
		t.Errorf("maxLat: got %g, want 41.0", bounds[3])
	}
}

func TestComputeGeoBounds_SinglePoint(t *testing.T) {
	tps := []models.GeoTiePoint{
		{Lat: 51.5, Lon: -0.12},
	}

	bounds := computeGeoBounds(tps)

	if bounds[0] != -0.12 || bounds[2] != -0.12 {
		t.Errorf("single point lon bounds: got [%g, %g]", bounds[0], bounds[2])
	}
	if bounds[1] != 51.5 || bounds[3] != 51.5 {
		t.Errorf("single point lat bounds: got [%g, %g]", bounds[1], bounds[3])
	}
}

func TestExtractEPSGFromWKT(t *testing.T) {
	tests := []struct {
		name string
		wkt  string
		want string
	}{
		{
			name: "standard WKT with EPSG",
			wkt:  `GEOGCS["GCS_WGS_1984",DATUM["D_WGS_1984"],AUTHORITY["EPSG","4326"]]`,
			want: "4326",
		},
		{
			name: "lowercase epsg",
			wkt:  `GEOGCS["GCS_WGS_1984",AUTHORITY["epsg","32632"]]`,
			want: "32632",
		},
		{
			name: "no AUTHORITY",
			wkt:  `GEOGCS["GCS_WGS_1984",DATUM["D_WGS_1984"]]`,
			want: "",
		},
		{
			name: "empty string",
			wkt:  "",
			want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := extractEPSGFromWKT(tc.wkt)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestGeoDataToGeoJSON_Basic(t *testing.T) {
	geo := &models.GeoData{
		CoordinateSystem: "GEOGCS",
		EPSG:             "4326",
		TiePoints: []models.GeoTiePoint{
			{PDFX: 0, PDFY: 0, Lat: 40.0, Lon: -74.0},
			{PDFX: 100, PDFY: 100, Lat: 41.0, Lon: -73.0},
		},
		Bounds: [4]float64{-74.0, 40.0, -73.0, 41.0},
	}

	result := GeoDataToGeoJSON(geo)

	// Validate it's valid JSON.
	var parsed map[string]any
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, result)
	}

	if parsed["type"] != "FeatureCollection" {
		t.Errorf("type: got %v, want FeatureCollection", parsed["type"])
	}

	features, ok := parsed["features"].([]any)
	if !ok {
		t.Fatal("features is not an array")
	}

	// 2 tie points + 1 bbox = 3 features.
	if len(features) != 3 {
		t.Errorf("expected 3 features, got %d", len(features))
	}
}

func TestGeoDataToGeoJSON_Empty(t *testing.T) {
	geo := &models.GeoData{}

	result := GeoDataToGeoJSON(geo)

	var parsed map[string]any
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	features, ok := parsed["features"].([]any)
	if !ok {
		t.Fatal("features is not an array")
	}
	if len(features) != 0 {
		t.Errorf("expected 0 features, got %d", len(features))
	}
}

func TestGeoDataToGeoJSON_Coordinates(t *testing.T) {
	geo := &models.GeoData{
		TiePoints: []models.GeoTiePoint{
			{Lat: 48.8566, Lon: 2.3522, PDFX: 50, PDFY: 75},
		},
		Bounds: [4]float64{2.3522, 48.8566, 2.3522, 48.8566},
	}

	result := GeoDataToGeoJSON(geo)

	var parsed map[string]any
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	features := parsed["features"].([]any)
	point := features[0].(map[string]any)
	geom := point["geometry"].(map[string]any)
	coords := geom["coordinates"].([]any)

	// GeoJSON uses [lon, lat] order.
	lon := coords[0].(float64)
	lat := coords[1].(float64)

	if math.Abs(lon-2.3522) > 0.0001 {
		t.Errorf("lon: got %g, want 2.3522", lon)
	}
	if math.Abs(lat-48.8566) > 0.0001 {
		t.Errorf("lat: got %g, want 48.8566", lat)
	}

	props := point["properties"].(map[string]any)
	if props["pdfX"].(float64) != 50 {
		t.Errorf("pdfX: got %v, want 50", props["pdfX"])
	}
}
