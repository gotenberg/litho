package document

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// buildPageBoxPDF constructs a minimal PDF with configurable page boxes.
// The page tree root can carry inherited MediaBox and CropBox, while the
// page node can carry any of the five boxes.
func buildPageBoxPDF(inheritedMediaBox, inheritedCropBox string, pageBoxes map[string]string) []byte {
	// Object 1: Catalog
	obj1 := "1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n"

	// Object 2: Pages (with inherited boxes)
	pagesExtras := ""
	if inheritedMediaBox != "" {
		pagesExtras += " /MediaBox " + inheritedMediaBox
	}
	if inheritedCropBox != "" {
		pagesExtras += " /CropBox " + inheritedCropBox
	}
	obj2 := fmt.Sprintf("2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1%s >>\nendobj\n", pagesExtras)

	// Object 3: Page (with per-page boxes)
	var pageExtras strings.Builder
	for key, val := range pageBoxes {
		pageExtras.WriteString(fmt.Sprintf(" /%s %s", key, val))
	}
	obj3 := fmt.Sprintf("3 0 obj\n<< /Type /Page /Parent 2 0 R%s >>\nendobj\n", pageExtras.String())

	body := obj1 + obj2 + obj3

	// Calculate offsets for xref
	off1 := 0
	off2 := len(obj1)
	off3 := off2 + len(obj2)

	xrefOffset := len(body)
	xref := fmt.Sprintf("xref\n0 4\n0000000000 65535 f \n%010d 00000 n \n%010d 00000 n \n%010d 00000 n \n", off1, off2, off3)
	trailer := fmt.Sprintf("trailer\n<< /Size 4 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xrefOffset)

	return []byte("%PDF-1.7\n" + body + xref + trailer)
}

func arrToFloats(arr ast.PdfArray) [4]float64 {
	if len(arr) < 4 {
		return [4]float64{}
	}
	var r [4]float64
	for i := range 4 {
		switch v := arr[i].(type) {
		case ast.PdfInt:
			r[i] = float64(v)
		case ast.PdfReal:
			r[i] = float64(v)
		}
	}
	return r
}

func TestPageBoxes_AllFiveExplicit(t *testing.T) {
	pdf := buildPageBoxPDF(
		"[0 0 612 792]", // MediaBox on Pages node (inherited)
		"",              // No inherited CropBox
		map[string]string{
			"CropBox":  "[10 10 602 782]",
			"BleedBox": "[5 5 607 787]",
			"TrimBox":  "[20 20 592 772]",
			"ArtBox":   "[30 30 582 762]",
		},
	)

	ctx := context.Background()
	doc, err := NewDocument(ctx, bytes.NewReader(pdf), int64(len(pdf)), nil)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatal(err)
	}
	pages, err := ResolvePages(ctx, doc, catalog)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 1 {
		t.Fatalf("expected 1 page, got %d", len(pages))
	}

	p := pages[0]
	assertBox(t, "MediaBox", arrToFloats(p.MediaBox), [4]float64{0, 0, 612, 792})
	assertBox(t, "CropBox", arrToFloats(p.CropBox), [4]float64{10, 10, 602, 782})
	assertBox(t, "BleedBox", arrToFloats(p.BleedBox), [4]float64{5, 5, 607, 787})
	assertBox(t, "TrimBox", arrToFloats(p.TrimBox), [4]float64{20, 20, 592, 772})
	assertBox(t, "ArtBox", arrToFloats(p.ArtBox), [4]float64{30, 30, 582, 762})
}

func TestPageBoxes_CropBoxDefaultsToMediaBox(t *testing.T) {
	pdf := buildPageBoxPDF(
		"[0 0 612 792]", // inherited MediaBox
		"",              // no CropBox anywhere
		nil,
	)

	ctx := context.Background()
	doc, err := NewDocument(ctx, bytes.NewReader(pdf), int64(len(pdf)), nil)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatal(err)
	}
	pages, err := ResolvePages(ctx, doc, catalog)
	if err != nil {
		t.Fatal(err)
	}
	p := pages[0]

	// CropBox should default to MediaBox
	assertBox(t, "CropBox", arrToFloats(p.CropBox), [4]float64{0, 0, 612, 792})
}

func TestPageBoxes_InheritedCropBox(t *testing.T) {
	pdf := buildPageBoxPDF(
		"[0 0 612 792]",   // inherited MediaBox
		"[10 10 602 782]", // inherited CropBox from Pages node
		nil,               // page has no boxes of its own
	)

	ctx := context.Background()
	doc, err := NewDocument(ctx, bytes.NewReader(pdf), int64(len(pdf)), nil)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatal(err)
	}
	pages, err := ResolvePages(ctx, doc, catalog)
	if err != nil {
		t.Fatal(err)
	}
	p := pages[0]

	assertBox(t, "CropBox", arrToFloats(p.CropBox), [4]float64{10, 10, 602, 782})
	// BleedBox, TrimBox, ArtBox should default to CropBox
	assertBox(t, "BleedBox", arrToFloats(p.BleedBox), [4]float64{10, 10, 602, 782})
	assertBox(t, "TrimBox", arrToFloats(p.TrimBox), [4]float64{10, 10, 602, 782})
	assertBox(t, "ArtBox", arrToFloats(p.ArtBox), [4]float64{10, 10, 602, 782})
}

func TestPageBoxes_PageOverridesCropBox(t *testing.T) {
	pdf := buildPageBoxPDF(
		"[0 0 612 792]",   // inherited MediaBox
		"[10 10 602 782]", // inherited CropBox
		map[string]string{
			"CropBox": "[50 50 562 742]", // page overrides
		},
	)

	ctx := context.Background()
	doc, err := NewDocument(ctx, bytes.NewReader(pdf), int64(len(pdf)), nil)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatal(err)
	}
	pages, err := ResolvePages(ctx, doc, catalog)
	if err != nil {
		t.Fatal(err)
	}
	p := pages[0]

	// Page-level CropBox overrides inherited one
	assertBox(t, "CropBox", arrToFloats(p.CropBox), [4]float64{50, 50, 562, 742})
	// Non-inheritable boxes default to the effective CropBox
	assertBox(t, "BleedBox", arrToFloats(p.BleedBox), [4]float64{50, 50, 562, 742})
}

func TestPageBoxes_NonInheritableDefaultToCropBox(t *testing.T) {
	// Only TrimBox is explicitly set; BleedBox and ArtBox should default to CropBox.
	pdf := buildPageBoxPDF(
		"[0 0 612 792]", // inherited MediaBox
		"",              // no inherited CropBox
		map[string]string{
			"CropBox": "[10 10 602 782]",
			"TrimBox": "[20 20 592 772]",
		},
	)

	ctx := context.Background()
	doc, err := NewDocument(ctx, bytes.NewReader(pdf), int64(len(pdf)), nil)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatal(err)
	}
	pages, err := ResolvePages(ctx, doc, catalog)
	if err != nil {
		t.Fatal(err)
	}
	p := pages[0]

	assertBox(t, "TrimBox", arrToFloats(p.TrimBox), [4]float64{20, 20, 592, 772})
	assertBox(t, "BleedBox", arrToFloats(p.BleedBox), [4]float64{10, 10, 602, 782})
	assertBox(t, "ArtBox", arrToFloats(p.ArtBox), [4]float64{10, 10, 602, 782})
}

// TestPageBoxes_ClampedToMediaBox pins that the bleed, trim and art boxes are
// reduced to their intersection with the media box and with nothing else (ISO
// 32000-1, 14.11.2.1), while the page dictionary and the Declared fields keep
// the entries as written.
func TestPageBoxes_ClampedToMediaBox(t *testing.T) {
	p := onlyPage(t, buildPageBoxPDF(
		"[0 0 612 792]",
		"",
		map[string]string{
			"CropBox":  "[10 10 602 782]",
			"BleedBox": "[-10 -10 622 802]",
			"TrimBox":  "[0 0 700 700]",
			"ArtBox":   "[5 5 607 787]",
		},
	))

	assertBox(t, "CropBox", arrToFloats(p.CropBox), [4]float64{10, 10, 602, 782})
	assertBox(t, "BleedBox", arrToFloats(p.BleedBox), [4]float64{0, 0, 612, 792})
	assertBox(t, "TrimBox", arrToFloats(p.TrimBox), [4]float64{0, 0, 612, 700})
	// Wider than the crop box, inside the media box: the crop box has no defined
	// relationship with the other boundaries, so nothing clips it.
	assertBox(t, "ArtBox", arrToFloats(p.ArtBox), [4]float64{5, 5, 607, 787})

	declared, ok := p.Dict["TrimBox"].(ast.PdfArray)
	if !ok {
		t.Fatalf("page dict /TrimBox is %T, want the declared array", p.Dict["TrimBox"])
	}
	assertBox(t, "declared TrimBox", arrToFloats(declared), [4]float64{0, 0, 700, 700})
	assertBox(t, "DeclaredTrimBox", arrToFloats(p.DeclaredTrimBox), [4]float64{0, 0, 700, 700})
	assertBox(t, "DeclaredBleedBox", arrToFloats(p.DeclaredBleedBox), [4]float64{-10, -10, 622, 802})
}

// TestPageBoxes_DefaultsFollowTheEffectiveCropBox pins that an absent bleed,
// trim or art box takes the crop box after the crop box itself is clamped, so
// a crop box larger than the page does not leak through the defaults.
func TestPageBoxes_DefaultsFollowTheEffectiveCropBox(t *testing.T) {
	p := onlyPage(t, buildPageBoxPDF(
		"[0 0 612 792]",
		"[-20 -20 700 900]",
		nil,
	))

	for name, box := range map[string]ast.PdfArray{
		"CropBox":  p.CropBox,
		"BleedBox": p.BleedBox,
		"TrimBox":  p.TrimBox,
		"ArtBox":   p.ArtBox,
	} {
		assertBox(t, name, arrToFloats(box), [4]float64{0, 0, 612, 792})
	}
}

// TestPageBoxes_DisjointBoxTakesTheCropBox pins that a boundary missing the
// media box entirely falls back to its Table 30 default, the crop box, rather
// than the media box or an empty rectangle.
func TestPageBoxes_DisjointBoxTakesTheCropBox(t *testing.T) {
	p := onlyPage(t, buildPageBoxPDF(
		"[0 0 612 792]",
		"",
		map[string]string{
			"CropBox":  "[10 10 602 782]",
			"BleedBox": "[900 900 1000 1000]",
			"TrimBox":  "[612 0 700 792]",
			"ArtBox":   "[0 0 0 0]",
		},
	))

	assertBox(t, "BleedBox", arrToFloats(p.BleedBox), [4]float64{10, 10, 602, 782})
	assertBox(t, "TrimBox", arrToFloats(p.TrimBox), [4]float64{10, 10, 602, 782})
	assertBox(t, "ArtBox", arrToFloats(p.ArtBox), [4]float64{10, 10, 602, 782})
}

func assertBox(t *testing.T, name string, got, want [4]float64) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got %v, want %v", name, got, want)
	}
}
