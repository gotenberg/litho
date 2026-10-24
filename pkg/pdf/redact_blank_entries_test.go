package pdf

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// blankEntriesImage is the image dictionary entries the tests plant on
// [imageDoc]'s image: those a blanked image keeps, then those it drops.
const blankEntriesImage = "/OC 30 0 R /Interpolate true /Intent /Perceptual /StructParent 0 /Name /Im1 " +
	"/Measure << /Type /Measure /Subtype /GEO >> " +
	"/Metadata 31 0 R /OPI << /Version 2.0 >> /ID <0011> /Alternates [] " +
	"/PtData << /Type /PtData /Subtype /Cloud >> /AF [] "

// blankEntriesObjects are the objects [blankEntriesImage] names: an
// optional content group and an XMP stream.
const blankEntriesObjects = "30 0 obj\n<< /Type /OCG /Name (Layer) >>\nendobj\n" +
	"31 0 obj\n<< /Type /Metadata /Subtype /XML /Length 5 >>\nstream\n<x/>\n\nendstream\nendobj\n"

var (
	keptImageEntries    = []ast.PdfName{"OC", "Interpolate", "Intent", "StructParent", "Name", "Measure"}
	droppedImageEntries = []ast.PdfName{"Metadata", "OPI", "ID", "Alternates", "PtData", "AF"}
)

// blankEntriesDoc returns [imageDoc] over contents, its image carrying
// [blankEntriesImage] and written in the colour space and depth space
// states, its optional content group off when hidden is set.
func blankEntriesDoc(contents []string, space string, hidden bool) []byte {
	catalog := "/OCProperties << /OCGs [30 0 R] /D << >> >> "
	if hidden {
		catalog = "/OCProperties << /OCGs [30 0 R] /D << /OFF [30 0 R] >> >> "
	}
	src := imageDoc(contents, catalog, "", blankEntriesObjects)
	return bytes.Replace(src, []byte("/ColorSpace /DeviceGray /BitsPerComponent 8 "),
		[]byte(space+" "+blankEntriesImage), 1)
}

// pageXObject returns the dictionary of the XObject the page's resources
// name name in data.
func pageXObject(t *testing.T, data []byte, page int, name ast.PdfName) ast.PdfDict {
	t.Helper()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	pages, err := r.resolvePages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	xobjects, err := document.ResolveDict(ctx, r.doc, pages[page-1].Resources["XObject"])
	if err != nil {
		t.Fatal(err)
	}
	stream, err := document.ResolveStream(ctx, r.doc, xobjects[name])
	if err != nil {
		t.Fatalf("XObject %s: %v", name, err)
	}
	return stream.Dict
}

// TestWriterRedactBlankedImageKeepsWhereItPaints pins the entries a blanked
// image keeps: those saying where and how it paints (ISO 32000-1, Table 89:
// /OC, /Interpolate, /Intent, /Name; ISO 32000-2, /Measure) and the parent
// tree key its structure element names it by (14.7.4.4), on every path that
// writes a replacement: a picture kept in its own space, one written in
// sRGB, and a stencil mask. Those describing what it showed go.
func TestWriterRedactBlankedImageKeepsWhereItPaints(t *testing.T) {
	t.Parallel()
	gray := make([]string, 256)
	for i := range gray {
		gray[i] = string("0123456789ABCDEF"[i>>4]) + string("0123456789ABCDEF"[i&15])
	}
	for _, tc := range []struct{ name, space string }{
		{"own space", "/ColorSpace /DeviceGray /BitsPerComponent 8"},
		{"sRGB", "/ColorSpace [/Indexed /DeviceGray 255 <" + strings.Join(gray, "") + ">] /BitsPerComponent 8"},
		{"stencil", "/ImageMask true /BitsPerComponent 1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, err := redactRegions(t, blankEntriesDoc([]string{placeLeft}, tc.space, false), RedactRegion{Page: 1, Rect: topLeft})
			if err != nil {
				t.Fatalf("Redact: %v", err)
			}
			dict := pageXObject(t, out, 1, "Im1")
			_, srgb := dict["ColorSpace"].(ast.PdfArray)
			if took := map[string]bool{
				"own space": dict["ColorSpace"] == ast.PdfName("DeviceGray"),
				"sRGB":      srgb,
				"stencil":   dict["ImageMask"] == ast.PdfBool(true),
			}[tc.name]; !took {
				t.Fatalf("ColorSpace %v, ImageMask %v: the case did not take its path", dict["ColorSpace"], dict["ImageMask"])
			}
			for _, key := range keptImageEntries {
				if _, ok := dict[key]; !ok {
					t.Errorf("the blanked image lost %s", key)
				}
			}
			for _, key := range droppedImageEntries {
				if _, ok := dict[key]; ok {
					t.Errorf("the blanked image kept %s", key)
				}
			}
		})
	}
}

// TestWriterRedactBlankedCopyCarriesNoStructParent pins the copy a shared
// image is blanked in for one placement: it keeps /OC and the rendering
// entries, and carries no /StructParent, since the parent tree entry's
// element names the image itself, which still paints the other placement
// (ISO 32000-1, 14.7.4.4). The image keeps every entry.
func TestWriterRedactBlankedCopyCarriesNoStructParent(t *testing.T) {
	t.Parallel()
	out, err := redactRegions(t, blankEntriesDoc([]string{placeLeft + " " + placeRight}, "/ColorSpace /DeviceGray /BitsPerComponent 8", false),
		RedactRegion{Page: 1, Rect: topLeft})
	if err != nil {
		t.Fatalf("Redact: %v", err)
	}
	copied := pageXObject(t, out, 1, "LithoRedacted1")
	for _, key := range keptImageEntries {
		if _, ok := copied[key]; ok != (key != "StructParent") {
			t.Errorf("the blanked copy carries %s: %v", key, ok)
		}
	}
	source := pageXObject(t, out, 1, "Im1")
	for _, key := range append(keptImageEntries, droppedImageEntries...) {
		if _, ok := source[key]; !ok {
			t.Errorf("the image the other placement paints lost %s", key)
		}
	}
}

// TestWriterRedactHiddenImageStaysHidden pins that an image in an optional
// content group that is off stays hidden once blanked: the replacement
// dropped /OC, and the blank painted black where the page showed paper
// (ISO 32000-1, 8.11.2).
func TestWriterRedactHiddenImageStaysHidden(t *testing.T) {
	t.Parallel()
	src := blankEntriesDoc([]string{placeLeft}, "/ColorSpace /DeviceGray /BitsPerComponent 8", true)
	if got := renderedGray(t, src, 1, copyLeftTop); !nearGray(got, 0xff) {
		t.Fatalf("hidden image renders %d before the redaction, want paper", got)
	}
	out, err := redactRegions(t, src, RedactRegion{Page: 1, Rect: topLeft})
	if err != nil {
		t.Fatalf("Redact: %v", err)
	}
	if got := renderedGray(t, out, 1, copyLeftTop); !nearGray(got, 0xff) {
		t.Errorf("blanked hidden image renders %d, want paper", got)
	}
}

// TestWriterRedactLossyBlankingKeepsTheEntries pins the entries of a
// photograph [WithRedactionJPEGQuality] redraws as a form: the form at its
// object number takes /OC and /StructParent (ISO 32000-1, Table 95), and
// every image the form draws takes /Interpolate and /Intent (Table 89) and
// no /OC or /StructParent of its own.
func TestWriterRedactLossyBlankingKeepsTheEntries(t *testing.T) {
	t.Parallel()
	in := jpegPhotoPDF(t, " /OC 6 0 R /Interpolate true /Intent /Saturation /StructParent 0", "<< /Type /OCG /Name (Layer) >>")
	out, report := redactBytes(t, in, lossyBlankRegion, WithRedactionJPEGQuality(75))
	if report.LossyBlankedImages != 1 {
		t.Fatalf("LossyBlankedImages = %d, want 1", report.LossyBlankedImages)
	}
	form := pageXObject(t, out, 1, "Im")
	if form["Subtype"] != ast.PdfName("Form") {
		t.Fatalf("Subtype = %v, want the form", form["Subtype"])
	}
	for _, key := range []ast.PdfName{"OC", "StructParent"} {
		if _, ok := form[key]; !ok {
			t.Errorf("the form lost %s", key)
		}
	}

	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(out), int64(len(out)))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	images := 0
	var walk func(dict ast.PdfDict, depth int)
	walk = func(dict ast.PdfDict, depth int) {
		res, err := document.ResolveDict(ctx, r.doc, dict["Resources"])
		if err != nil || depth > 2 {
			t.Fatalf("resources at depth %d: %v", depth, err)
		}
		xobjects, err := document.ResolveDict(ctx, r.doc, res["XObject"])
		if err != nil {
			t.Fatal(err)
		}
		for _, ref := range xobjects {
			s, err := document.ResolveStream(ctx, r.doc, ref)
			if err != nil {
				t.Fatal(err)
			}
			if s.Dict["Subtype"] == ast.PdfName("Form") {
				walk(s.Dict, depth+1)
				continue
			}
			images++
			if s.Dict["Interpolate"] != ast.PdfBool(true) || s.Dict["Intent"] != ast.PdfName("Saturation") {
				t.Errorf("tile /Interpolate %v /Intent %v, want the photograph's", s.Dict["Interpolate"], s.Dict["Intent"])
			}
			for _, key := range []ast.PdfName{"OC", "StructParent"} {
				if _, ok := s.Dict[key]; ok {
					t.Errorf("a tile carries %s", key)
				}
			}
		}
	}
	walk(form, 0)
	if images < 2 {
		t.Fatalf("the form draws %d images, want the underlay and its tiles", images)
	}
}
