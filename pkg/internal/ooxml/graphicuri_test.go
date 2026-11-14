package ooxml

import "testing"

// TestGraphicURI_MapsTheStrictNamespaces locks that the uri of a
// graphicData element reads in its Transitional form whether the document
// is Strict or Transitional, so that a Strict chart, diagram or picture is
// recognised.
func TestGraphicURI_MapsTheStrictNamespaces(t *testing.T) {
	tests := []struct{ uri, want string }{
		{"http://purl.oclc.org/ooxml/drawingml/chart", "http://schemas.openxmlformats.org/drawingml/2006/chart"},
		{"http://purl.oclc.org/ooxml/drawingml/picture", "http://schemas.openxmlformats.org/drawingml/2006/picture"},
		{"http://schemas.openxmlformats.org/drawingml/2006/diagram", "http://schemas.openxmlformats.org/drawingml/2006/diagram"},
		{"http://schemas.microsoft.com/office/drawing/2014/chartex", "http://schemas.microsoft.com/office/drawing/2014/chartex"},
	}
	for _, tc := range tests {
		doc := `<a:graphicData xmlns:a="http://purl.oclc.org/ooxml/drawingml/main" uri="` + tc.uri + `"/>`
		el, err := Parse(t.Context(), []byte(doc), nil)
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		if got := GraphicURI(el); got != tc.want {
			t.Errorf("GraphicURI(%q) = %q, want %q", tc.uri, got, tc.want)
		}
	}
	if got := GraphicURI(nil); got != "" {
		t.Errorf("GraphicURI(nil) = %q", got)
	}
}
