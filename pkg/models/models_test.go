package models

import (
	"context"
	"encoding/json"
	"io"
	"reflect"
	"strings"
	"testing"
)

// assertRoundTrip marshals in to JSON, unmarshals the result into a fresh
// instance of T, and asserts the result is [reflect.DeepEqual] to the input.
// The input must populate every exported, non-function field with a
// non-zero sentinel so a stray `omitempty` would be caught.
func assertRoundTrip[T any](t *testing.T, in T) {
	t.Helper()
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Errorf("round trip mismatch:\n want: %+v\n  got: %+v\n JSON: %s", in, out, data)
	}
}

// --- Per-type round-trip tests -------------------------------------------

func TestPage_JSONRoundTrip(t *testing.T) {
	assertRoundTrip(t, Page{
		PageNumber:  5,
		Width:       612,
		Height:      792,
		Rotate:      90,
		UserUnit:    1.5,
		IsLandscape: true,
		DPM:         map[string]string{"Producer": "litho"},
		MediaBox:    [4]float64{0, 0, 612, 792},
		CropBox:     [4]float64{10, 10, 602, 782},
		BleedBox:    [4]float64{5, 5, 607, 787},
		TrimBox:     [4]float64{20, 20, 592, 772},
		ArtBox:      [4]float64{30, 30, 582, 762},
	})
}

func TestTextElement_JSONRoundTrip(t *testing.T) {
	// SpaceWidth is intentionally zero here: it has json:"-" and is covered
	// by [TestTextElement_SpaceWidthNotSerialized].
	assertRoundTrip(t, TextElement{
		Text:          "Hello",
		X:             100,
		Y:             200,
		Width:         50.5,
		FontSize:      12,
		MCID:          3,
		Hidden:        true,
		Invisible:     true,
		Vertical:      true,
		Angle:         45.5,
		Redacted:      true,
		IsClipped:     true,
		PatternType:   "tiling",
		Language:      "en",
		ExpansionText: "hello world",
		MathML:        "<mrow></mrow>",
	})
}

func TestTextElement_SpaceWidthNotSerialized(t *testing.T) {
	in := TextElement{
		Text:       "Hi",
		FontSize:   12,
		SpaceWidth: 42.5,
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(strings.ToLower(string(data)), "spacewidth") {
		t.Errorf("SpaceWidth leaked into JSON: %s", data)
	}
	var out TextElement
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.SpaceWidth != 0 {
		t.Errorf("SpaceWidth: got %v, want 0 (not serialized)", out.SpaceWidth)
	}
	if out.Text != "Hi" || out.FontSize != 12 {
		t.Errorf("other fields lost: %+v", out)
	}
}

func TestBookmark_JSONRoundTrip(t *testing.T) {
	assertRoundTrip(t, Bookmark{
		Title:      "Chapter 1",
		PageNumber: 10,
		Children: []Bookmark{
			{
				Title:      "Section 1.1",
				PageNumber: 11,
				Children: []Bookmark{
					{Title: "Subsection", PageNumber: 12},
				},
			},
			{Title: "Section 1.2", PageNumber: 15},
		},
	})
}

func TestFormField_JSONRoundTrip(t *testing.T) {
	assertRoundTrip(t, FormField{Name: "fullname", Value: "Alice"})
}

func TestAnnotation_JSONRoundTrip(t *testing.T) {
	assertRoundTrip(t, Annotation{
		Subtype:    "Link",
		Rect:       [4]float64{0, 0, 100, 100},
		Contents:   "click me",
		URI:        "https://example.com",
		DestPage:   5,
		QuadPoints: [][8]float64{{0, 0, 100, 0, 0, 100, 100, 100}},
	})
}

// ImageMeta, EmbeddedFile, and Model3D carry a lazy Open closure that cannot
// round-trip through JSON. The dedicated tests below verify every other
// field survives and that Open is reset to nil on the decode side.

func TestImageMeta_JSONRoundTrip(t *testing.T) {
	in := ImageMeta{
		Filename:         "img.jpg",
		Width:            800,
		Height:           600,
		ColorSpace:       "DeviceRGB",
		BitsPerComponent: 8,
		Open: func(_ context.Context) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("data")), nil
		},
	}
	assertRoundTripExceptOpen(t, in, func(v *ImageMeta) { v.Open = nil })
}

func TestEmbeddedFile_JSONRoundTrip(t *testing.T) {
	in := EmbeddedFile{
		Name:     "attachment.txt",
		MIMEType: "text/plain",
		Open: func(_ context.Context) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("data")), nil
		},
	}
	assertRoundTripExceptOpen(t, in, func(v *EmbeddedFile) { v.Open = nil })
}

func TestModel3D_JSONRoundTrip(t *testing.T) {
	in := Model3D{
		Name:    "cube",
		Subtype: "U3D",
		Open: func(_ context.Context) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("u3d")), nil
		},
	}
	assertRoundTripExceptOpen(t, in, func(v *Model3D) { v.Open = nil })
}

// assertRoundTripExceptOpen is the variant of [assertRoundTrip] for types
// that carry an unexportable lazy-content closure. It strips the closure
// from a clone of the input before comparison.
func assertRoundTripExceptOpen[T any](t *testing.T, in T, zeroOpen func(*T)) {
	t.Helper()
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	expected := in
	zeroOpen(&expected)
	if !reflect.DeepEqual(expected, out) {
		t.Errorf("round trip mismatch (Open excluded):\n want: %+v\n  got: %+v\n JSON: %s",
			expected, out, data)
	}
}

func TestLine_JSONRoundTrip(t *testing.T) {
	assertRoundTrip(t, Line{X1: 10, Y1: 20, X2: 30, Y2: 40})
}

func TestTable_JSONRoundTrip(t *testing.T) {
	assertRoundTrip(t, Table{
		Rows: [][]string{{"a", "b"}, {"c", "d"}},
		BBox: [4]float64{0, 0, 100, 100},
	})
}

func TestSignature_JSONRoundTrip(t *testing.T) {
	assertRoundTrip(t, Signature{
		FieldName:    "Signature1",
		ByteRange:    [4]int64{0, 100, 200, 300},
		Contents:     "DEADBEEF",
		DocumentHash: "abc123",
		SigningTime:  "D:20260101000000+00'00",
		MDPPolicy:    1,
	})
}

func TestSignatureValidation_JSONRoundTrip(t *testing.T) {
	assertRoundTrip(t, SignatureValidation{
		FieldName:       "Signature1",
		HashValid:       true,
		CertChainValid:  true,
		SignerCN:        "Alice",
		Issuer:          "CA",
		NotBefore:       "2025-01-01",
		NotAfter:        "2027-01-01",
		SigningTime:     "2025-06-01",
		TimestampValid:  true,
		Error:           "none",
		HasDSS:          true,
		HasTimestamp:    true,
		TimestampTime:   "2025-06-01",
		TimestampIssuer: "TSA",
		LTVEnabled:      true,
		RevocationOK:    true,
		RevocationInfo:  "ok",
		MDPPolicy:       2,
		MDPValid:        true,
		MDPInfo:         "ok",
	})
}

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

func TestDSSInfo_JSONRoundTrip(t *testing.T) {
	assertRoundTrip(t, DSSInfo{CertCount: 3, CRLCount: 1, OCSPCount: 2, VRICount: 1})
}

func TestPronunciationLexicon_JSONRoundTrip(t *testing.T) {
	assertRoundTrip(t, PronunciationLexicon{XML: "<lexicon/>"})
}

func TestFormulaElement_JSONRoundTrip(t *testing.T) {
	assertRoundTrip(t, FormulaElement{
		MathML: "<mrow><mi>x</mi></mrow>",
		MCIDs:  []int{1, 2, 3},
	})
}

func TestLinearizationParams_JSONRoundTrip(t *testing.T) {
	assertRoundTrip(t, LinearizationParams{
		FileLength:          10000,
		HintStreamOffset:    100,
		HintStreamLength:    500,
		FirstPageObjNum:     2,
		NumPages:            5,
		FirstPageEndOffset:  2000,
		FirstPageXRefOffset: 9500,
	})
}

func TestPageOffsetEntry_JSONRoundTrip(t *testing.T) {
	assertRoundTrip(t, PageOffsetEntry{
		ObjCount:            5,
		PageLength:          1000,
		ContentStreamOffset: 500,
	})
}

// --- Profile constants ----------------------------------------------------

func TestPDFAProfile_JSONValue(t *testing.T) {
	tests := []struct {
		profile PDFAProfile
		want    string
	}{
		{PDFA1b, `"PDF/A-1b"`},
		{PDFA1a, `"PDF/A-1a"`},
		{PDFA2b, `"PDF/A-2b"`},
		{PDFA2u, `"PDF/A-2u"`},
		{PDFA2a, `"PDF/A-2a"`},
		{PDFA3b, `"PDF/A-3b"`},
		{PDFA3u, `"PDF/A-3u"`},
		{PDFA3a, `"PDF/A-3a"`},
		{PDFA4, `"PDF/A-4"`},
		{PDFA4e, `"PDF/A-4e"`},
		{PDFA4f, `"PDF/A-4f"`},
	}
	for _, tc := range tests {
		t.Run(string(tc.profile), func(t *testing.T) {
			data, err := json.Marshal(tc.profile)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(data) != tc.want {
				t.Errorf("got %s, want %s", data, tc.want)
			}
		})
	}
}

func TestPDFUAProfile_JSONValue(t *testing.T) {
	tests := []struct {
		profile PDFUAProfile
		want    string
	}{
		{PDFUA1, `"PDF/UA-1"`},
		{PDFUA2, `"PDF/UA-2"`},
	}
	for _, tc := range tests {
		t.Run(string(tc.profile), func(t *testing.T) {
			data, err := json.Marshal(tc.profile)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(data) != tc.want {
				t.Errorf("got %s, want %s", data, tc.want)
			}
		})
	}
}

// --- Empty-schema drift guards --------------------------------------------
//
// Each case asserts the JSON byte shape of a zero-value instance of the
// type. Any field rename, tag change, or `omitempty` regression will break
// one of these goldens.

func TestModels_EmptySchema(t *testing.T) {
	tests := []struct {
		name string
		v    any
		want string
	}{
		{
			"Page",
			Page{},
			`{"pageNumber":0,"width":0,"height":0,"rotate":0,"userUnit":0,"isLandscape":false,"mediaBox":[0,0,0,0],"cropBox":[0,0,0,0],"bleedBox":[0,0,0,0],"trimBox":[0,0,0,0],"artBox":[0,0,0,0]}`,
		},
		{
			"TextElement",
			TextElement{},
			`{"text":"","x":0,"y":0,"width":0,"fontSize":0,"mcid":0}`,
		},
		{
			"Bookmark",
			Bookmark{},
			`{"title":"","pageNumber":0}`,
		},
		{
			"FormField",
			FormField{},
			`{"name":"","value":""}`,
		},
		{
			"Annotation",
			Annotation{},
			`{"subtype":"","rect":[0,0,0,0]}`,
		},
		{
			"ImageMeta",
			ImageMeta{},
			`{"filename":"","width":0,"height":0,"colorSpace":"","bitsPerComponent":0}`,
		},
		{
			"Line",
			Line{},
			`{"x1":0,"y1":0,"x2":0,"y2":0}`,
		},
		{
			"Table",
			Table{},
			`{"rows":null,"bbox":[0,0,0,0]}`,
		},
		{
			"EmbeddedFile",
			EmbeddedFile{},
			`{"name":""}`,
		},
		{
			"Signature",
			Signature{},
			`{"fieldName":"","byteRange":[0,0,0,0],"contents":"","documentHash":""}`,
		},
		{
			"SignatureValidation",
			SignatureValidation{},
			`{"fieldName":"","hashValid":false,"certChainValid":false,"signerCN":"","issuer":"","notBefore":"","notAfter":"","timestampValid":false}`,
		},
		{
			"GeoTiePoint",
			GeoTiePoint{},
			`{"pdfX":0,"pdfY":0,"lat":0,"lon":0}`,
		},
		{
			"GeoData",
			GeoData{},
			`{"coordinateSystem":"","tiePoints":null,"bounds":[0,0,0,0]}`,
		},
		{
			"ScaleAxis",
			ScaleAxis{},
			`{"unit":"","conversionFactor":0,"decimals":0}`,
		},
		{
			"Measurement",
			Measurement{},
			`{"source":"","bbox":[0,0,0,0],"scaleX":{"unit":"","conversionFactor":0,"decimals":0},"scaleY":{"unit":"","conversionFactor":0,"decimals":0}}`,
		},
		{
			"Model3D",
			Model3D{},
			`{"name":"","subtype":""}`,
		},
		{
			"DSSInfo",
			DSSInfo{},
			`{"certCount":0,"crlCount":0,"ocspCount":0,"vriCount":0}`,
		},
		{
			"PronunciationLexicon",
			PronunciationLexicon{},
			`{"xml":""}`,
		},
		{
			"FormulaElement",
			FormulaElement{},
			`{"mathML":""}`,
		},
		{
			"LinearizationParams",
			LinearizationParams{},
			`{"fileLength":0,"hintStreamOffset":0,"hintStreamLength":0,"firstPageObjNum":0,"numPages":0,"firstPageEndOffset":0,"firstPageXRefOffset":0}`,
		},
		{
			"PageOffsetEntry",
			PageOffsetEntry{},
			`{"objCount":0,"pageLength":0,"contentStreamOffset":0}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.v)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(data) != tc.want {
				t.Errorf("schema drift:\n want: %s\n  got: %s", tc.want, data)
			}
		})
	}
}
