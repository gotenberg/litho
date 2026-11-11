package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func TestFaceCovered(t *testing.T) {
	for name, want := range map[string]bool{
		"Calibri":                  true,
		"Calibri-Bold":             true,
		"Calibri,BoldItalic":       true,
		"Calibri-Light":            false,
		"Calibri-LightItalic":      false,
		"CalibriLight":             false,
		"Cambria-Italic":           true,
		"CambriaMath":              false,
		"ArialMT":                  true,
		"Arial-BoldMT":             true,
		"Arial-Black":              false,
		"ArialNarrow":              false,
		"TimesNewRomanPSMT":        true,
		"TimesNewRomanPS-ItalicMT": true,
		"CourierNewPSMT":           true,
		"Helvetica,Bold":           true,
		"Times-Roman":              true,
		"SymbolMT":                 true,
		"Wingdings-Regular":        true,
		"Webdings":                 true,
		"LiberationSans-Bold":      true,
		"DejaVuSansMono":           true,
		"Carlito-Italic":           true,
		"NotoSansCJKjp-Regular":    true,
		"OpenSymbol":               true,
		"___WRD_EMBED_SUB_1434":    true,
		"Aptos":                    false,
		"Aptos-Narrow":             false,
		"MS Gothic":                false,
		"Tahoma":                   false,
		"SegoeUISymbol":            false,
	} {
		if got := faceCovered(name); got != want {
			t.Errorf("faceCovered(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestStripSubsetTagAndDecodeName(t *testing.T) {
	for in, want := range map[string]string{
		"BCDEEE+Calibri-Light": "Calibri-Light",
		"Calibri":              "Calibri",
		"bcdeee+Calibri":       "bcdeee+Calibri",
		"BCD+Calibri":          "BCD+Calibri",
	} {
		if got := stripSubsetTag(in); got != want {
			t.Errorf("stripSubsetTag(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{"MS#20Gothic": "MS Gothic", "A#2": "A#2", "Plain": "Plain", "#zz": "#zz"} {
		if got := decodeName(in); got != want {
			t.Errorf("decodeName(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestReferenceFonts reads the faces of a file whose font dictionaries sit
// in an object stream, one of them twice under two subset tags and one with
// a space its name escapes, and names the ones no fixture font stands in
// for.
func TestReferenceFonts(t *testing.T) {
	d := pdfgen.New()
	font := func(name string) *pdfgen.Dict {
		return d.Indirect(pdfgen.D("Type", pdfgen.N("Font"), "Subtype", pdfgen.N("TrueType"), "BaseFont", pdfgen.N(name)))
	}
	d.AddPage(pdfgen.D(
		"Type", pdfgen.N("Page"),
		"MediaBox", pdfgen.A(0, 0, 200, 200),
		"Contents", d.Stream([]byte("BT /F1 12 Tf 10 10 Td (a) Tj ET")),
		"Resources", pdfgen.D("Font", pdfgen.D(
			"F1", font("BCDEEE+Calibri-Light"),
			"F2", font("BCDFEE+Calibri"),
			"F3", font("MS Gothic"),
			"F4", font("BCDGEE+Calibri"),
		)),
	))
	out, err := d.Save(pdfgen.SaveOptions{GenerateObjectStreams: true, CompressStreams: true})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "ref.pdf")
	if err := os.WriteFile(path, out.Bytes, 0o600); err != nil {
		t.Fatal(err)
	}
	faces, err := referenceFonts(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Calibri", "Calibri-Light", "MS Gothic"}; !slices.Equal(faces, want) {
		t.Errorf("referenceFonts = %q, want %q", faces, want)
	}
	if got, want := missingReferenceFaces(faces), []string{"Calibri-Light", "MS Gothic"}; !slices.Equal(got, want) {
		t.Errorf("missingReferenceFaces = %q, want %q", got, want)
	}
}

// TestDiffCompare_LeavesMissingFontsUnscored checks that a fixture whose
// reference draws a face no fixture font stands in for is skipped, its
// scores kept, and counted in reference_fonts_missing.
func TestDiffCompare_LeavesMissingFontsUnscored(t *testing.T) {
	f := newDiffFeature()
	out := []byte(`{"outcome":"pass","scores":{"pages":1,"text_f1":0.9,"link_recall":1,"layout":0.8,"strict":0,"outline_recall":1},"reference_fonts_missing":["Aptos","Calibri-Light"]}`)
	v := f.Compare("doc", out)
	if !v.Skipped || v.Match || v.Metrics[metricReferenceFontsMissing] != 1 || v.Scores[scoreLayout] != 0.8 {
		t.Errorf("verdict %+v, want skipped with its scores and the metric", v)
	}
	if want := "unscored: the reference draws Aptos, Calibri-Light"; len(v.Reason) < len(want) || v.Reason[:len(want)] != want {
		t.Errorf("reason %q", v.Reason)
	}
	v = f.Compare("doc", []byte(`{"outcome":"pass","scores":{"pages":1,"text_f1":1,"link_recall":1,"layout":1,"strict":1,"outline_recall":1}}`))
	if v.Skipped || !v.Match {
		t.Errorf("verdict %+v, want a match", v)
	}
	if n, ok := v.Metrics[metricReferenceFontsMissing]; !ok || n != 0 {
		t.Errorf("reference_fonts_missing %d, %v, want 0", n, ok)
	}
}
