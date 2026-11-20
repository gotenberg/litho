package models

import (
	"encoding/json"
	"testing"
)

// TestModels_EmptySchema asserts the JSON byte shape of a zero-value instance
// of each type. Any field rename, tag change, or `omitempty` regression will
// break one of these goldens.
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
