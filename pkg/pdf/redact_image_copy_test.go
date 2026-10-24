package pdf

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/mutations"
	"github.com/gotenberg/litho/pkg/models"
)

// Sample centres of the placements imageDoc's placeLeft and placeRight paint.
var (
	copyLeftTop    = [2]float64{112.5, 587.5}
	copyLeftBottom = [2]float64{112.5, 512.5}
	copyRight      = [2]float64{312.5, 587.5}
)

// TestWriter_Redact_ImageCopySurvivesEveryWriterStage pins that a placement
// blanked in a copy stays blank whatever else the Writer does, before or
// after the redaction.
//
// The copy is painted under a new resource name, and a stage that rebuilds a
// page from the source content stream names the image itself again, whose
// samples the redaction never touched. No scan for text sees that, so WriteTo
// checks the composed pages for it and applies the redaction again.
func TestWriter_Redact_ImageCopySurvivesEveryWriterStage(t *testing.T) {
	t.Parallel()

	redact := func(t *testing.T, w *Writer) {
		t.Helper()
		if _, err := w.Redact(context.Background(), []RedactRegion{{Page: 1, Rect: topLeft}}); err != nil {
			t.Fatalf("Redact: %v", err)
		}
	}
	autoTag := func(_ *testing.T, w *Writer) { w.AutoTag() }
	remediateUA := func(t *testing.T, w *Writer) {
		t.Helper()
		if _, err := w.RemediatePDFUA(context.Background(), models.PDFUA1); err != nil {
			t.Fatalf("RemediatePDFUA: %v", err)
		}
	}
	remediateA := func(t *testing.T, w *Writer) {
		t.Helper()
		if _, err := w.RemediatePDFA(context.Background(), models.PDFA2b); err != nil {
			t.Fatalf("RemediatePDFA: %v", err)
		}
	}

	for _, tc := range []struct {
		name  string
		steps []func(*testing.T, *Writer)
	}{
		{"Redact", []func(*testing.T, *Writer){redact}},
		{"Redact then AutoTag", []func(*testing.T, *Writer){redact, autoTag}},
		{"AutoTag then Redact", []func(*testing.T, *Writer){autoTag, redact}},
		{"Redact then RemediatePDFUA", []func(*testing.T, *Writer){redact, remediateUA}},
		{"AutoTag, Redact, then RemediatePDFUA", []func(*testing.T, *Writer){autoTag, redact, remediateUA}},
		{"Redact then RemediatePDFA 2b", []func(*testing.T, *Writer){redact, remediateA}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			// Text gives auto-tagging something to mark up.
			font := "31 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n"
			src := imageDoc([]string{"BT /F1 12 Tf 100 700 Td (KEEP) Tj ET " + placeLeft + " " + placeRight}, "", "", font)
			src = bytes.Replace(src, []byte("<< /XObject << /Im1 7 0 R >> >>"),
				[]byte("<< /XObject << /Im1 7 0 R >> /Font << /F1 31 0 R >> >>"), 1)
			r, err := Open(ctx, bytes.NewReader(src), int64(len(src)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			w := NewWriter(r)
			for _, step := range tc.steps {
				step(t, w)
			}
			var out bytes.Buffer
			if err := w.WriteTo(ctx, &out); err != nil {
				t.Fatalf("WriteTo: %v", err)
			}
			if got := renderedGray(t, out.Bytes(), 1, copyLeftTop); !nearGray(got, 0) {
				t.Errorf("covered placement = %d, want 0", got)
			}
			if got := renderedGray(t, out.Bytes(), 1, copyRight); !nearGray(got, 0x80) {
				t.Errorf("placement outside the region = %d, want 0x80", got)
			}
		})
	}
}

// TestWriter_WriteTo_ReappliesARedactionWhoseImageCopyWasUndone pins that
// WriteTo blanks a placement again when a later edit puts the page back as
// the source holds it, the image named where the copy was.
//
// No stage on the Writer does that today, which is why the edit is made here
// by hand: the check is what keeps a stage that rebuilds a page from the
// source, as auto-tagging once did, from restoring every covered sample.
func TestWriter_WriteTo_ReappliesARedactionWhoseImageCopyWasUndone(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	src := imageDoc([]string{placeLeft + " " + placeRight}, "", "", "")
	r, err := Open(ctx, bytes.NewReader(src), int64(len(src)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	w := NewWriter(r)
	if _, err := w.Redact(ctx, []RedactRegion{{Page: 1, Rect: topLeft}}); err != nil {
		t.Fatalf("Redact: %v", err)
	}
	pages, err := w.reader.resolvePages(ctx)
	if err != nil {
		t.Fatalf("resolvePages: %v", err)
	}
	// The source's page dictionary, its /Contents painting /Im1 at both
	// placements.
	w.rewriteOverrides[pages[0].Ref.ObjectNum] = pages[0].Dict

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if got := renderedGray(t, out.Bytes(), 1, copyLeftTop); !nearGray(got, 0) {
		t.Errorf("covered placement = %d, want 0", got)
	}
	if got := renderedGray(t, out.Bytes(), 1, copyRight); !nearGray(got, 0x80) {
		t.Errorf("placement outside the region = %d, want 0x80", got)
	}
}

// TestWriter_Redact_AgainOverAnImageCopy pins that a second Redact builds on
// a copy the first made: over the same placement it blanks the copy further,
// and over the image's other placement it blanks that one in a copy of its
// own.
func TestWriter_Redact_AgainOverAnImageCopy(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	src := imageDoc([]string{placeLeft + " " + placeRight}, "", "", "")
	r, err := Open(ctx, bytes.NewReader(src), int64(len(src)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	w := NewWriter(r)
	for _, rect := range [][4]float64{topLeft, bottomLeft} {
		if _, err := w.Redact(ctx, []RedactRegion{{Page: 1, Rect: rect}}); err != nil {
			t.Fatalf("Redact %v: %v", rect, err)
		}
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	for _, at := range [][2]float64{copyLeftTop, copyLeftBottom} {
		if got := renderedGray(t, out.Bytes(), 1, at); !nearGray(got, 0) {
			t.Errorf("left placement at %v = %d, want 0", at, got)
		}
	}
	if got := renderedGray(t, out.Bytes(), 1, copyRight); !nearGray(got, 0x80) {
		t.Errorf("right placement = %d, want 0x80", got)
	}
}

// TestWriter_Redact_ImageCopyKeepsTheSoftMask pins that a copy carries a soft
// mask of its own, opaque where the copy was blanked, and leaves the image's
// soft mask to its other placements.
func TestWriter_Redact_ImageCopyKeepsTheSoftMask(t *testing.T) {
	t.Parallel()

	alpha := string(bytes.Repeat([]byte{0x40}, 16))
	mask := "30 0 obj\n<< /Type /XObject /Subtype /Image /Width 4 /Height 4 /ColorSpace /DeviceGray " +
		"/BitsPerComponent 8 /Length 16 >>\nstream\n" + alpha + "\nendstream\nendobj\n"
	src := imageDoc([]string{placeLeft + " " + placeRight}, "", "", mask)
	src = bytes.Replace(src, []byte("/Subtype /Image /Width 4 /Height 4 /ColorSpace /DeviceGray /BitsPerComponent 8 /Length 16 >>\nstream\n"+
		string(bytes.Repeat([]byte{0x80}, 16))),
		[]byte("/Subtype /Image /Width 4 /Height 4 /ColorSpace /DeviceGray /BitsPerComponent 8 /SMask 30 0 R /Length 16 >>\nstream\n"+
			string(bytes.Repeat([]byte{0x80}, 16))), 1)

	out, report, err := redactWith(t, src, []RedactRegion{{Page: 1, Rect: topLeft}})
	if err != nil {
		t.Fatalf("Redact: %v", err)
	}
	if len(report.ImagesBlankedInCopy[1]) != 1 {
		t.Fatalf("ImagesBlankedInCopy = %v, want /Im1 on page 1", report.ImagesBlankedInCopy)
	}
	// Opaque black where the copy is painted: the mask's 25% coverage would
	// let the white page through.
	if got := renderedGray(t, out, 1, copyLeftTop); !nearGray(got, 0) {
		t.Errorf("covered placement = %d, want 0", got)
	}
	// 0x80 at a quarter coverage over white, as before the redaction.
	want := uint8((0x80*0x40 + 0xff*(0xff-0x40)) / 0xff)
	if got := renderedGray(t, out, 1, copyRight); !nearGray(got, want) {
		t.Errorf("placement outside the region = %d, want %d", got, want)
	}
}

// formImageDoc builds imageDoc's document with a form, object 9, that the
// pages name /Fm1 and that draws its own /Im1, object 8, a 4x4 DeviceGray
// image of 0x40 samples, over its 100pt square. Each entry of contents is one
// page's content stream.
func formImageDoc(contents []string) []byte {
	formSamples := string(bytes.Repeat([]byte{0x40}, 16))
	form := "q 100 0 0 100 0 0 cm /Im1 Do Q"
	extra := "8 0 obj\n<< /Type /XObject /Subtype /Image /Width 4 /Height 4 /ColorSpace /DeviceGray " +
		"/BitsPerComponent 8 /Length 16 >>\nstream\n" + formSamples + "\nendstream\nendobj\n" +
		"9 0 obj\n<< /Type /XObject /Subtype /Form /BBox [0 0 100 100] /Resources << /XObject << /Im1 8 0 R >> >> " +
		"/Length " + strconv.Itoa(len(form)) + " >>\nstream\n" + form + "\nendstream\nendobj\n"
	src := imageDoc(contents, "", "", extra)
	return bytes.Replace(src, []byte("/XObject << /Im1 7 0 R >>"), []byte("/XObject << /Im1 7 0 R /Fm1 9 0 R >>"), 1)
}

// placeFormLeft draws /Fm1 where placeLeft draws /Im1.
const placeFormLeft = "q 1 0 0 1 100 500 cm /Fm1 Do Q"

// TestWriter_Redact_BlanksAnImageAFormDraws pins that a region over an image
// a form XObject draws once blanks that image, the one the form names in its
// own resources, and leaves the page's image of the same name alone.
//
// Read in the page's resources, the form's /Im1 named the page's image,
// painted outside the region: that one lost its samples, the form's kept
// every one of them, and the redaction reported success. Blanked in place,
// the form's image also failed the shared-image check, which did not follow a
// form's resources, and refused.
func TestWriter_Redact_BlanksAnImageAFormDraws(t *testing.T) {
	t.Parallel()

	src := formImageDoc([]string{placeFormLeft + " " + placeRight})
	out, report, err := redactWith(t, src, []RedactRegion{{Page: 1, Rect: wholeLeft}})
	if err != nil {
		t.Fatalf("Redact: %v", err)
	}
	if len(report.ImagesBlankedInCopy) != 0 {
		t.Errorf("ImagesBlankedInCopy = %v, want none: the form's image is painted once", report.ImagesBlankedInCopy)
	}
	if got := renderedGray(t, out, 1, copyLeftTop); !nearGray(got, 0) {
		t.Errorf("the form's image = %d, want 0", got)
	}
	if got := renderedGray(t, out, 1, copyRight); !nearGray(got, 0x80) {
		t.Errorf("the page's own image, outside the region = %d, want 0x80", got)
	}
	if got := imageSamples(t, out, 1); !allSamples(got, 0x80) {
		t.Errorf("the page's own /Im1 = %v, want all 0x80", got)
	}
}

// TestWriter_Redact_StrictRefusesASharedImageAFormDraws pins that an image a
// form draws on two pages, under a region on one of them, refuses under
// WithStrictRedaction: the same samples are painted where no region covers
// them. The default blanks it in a copy of the form (see
// TestWriter_Redact_BlanksASharedFormImageInAFormCopy).
func TestWriter_Redact_StrictRefusesASharedImageAFormDraws(t *testing.T) {
	t.Parallel()

	src := formImageDoc([]string{placeFormLeft, placeFormLeft})
	out, _, err := redactWith(t, src, []RedactRegion{{Page: 1, Rect: wholeLeft}}, WithStrictRedaction())
	if !errors.Is(err, ErrRedactionRefused) || !errors.Is(err, mutations.ErrImageShared) {
		t.Fatalf("Redact = %v, want ErrRedactionRefused wrapping ErrImageShared", err)
	}
	for page := 1; page <= 2; page++ {
		if got := renderedGray(t, out, page, copyLeftTop); !nearGray(got, 0x40) {
			t.Errorf("page %d after a refusal = %d, want 0x40", page, got)
		}
	}
}

// TestWriter_Redact_ImageCopyIsDeterministic pins that a redaction blanking
// in a copy writes the same bytes on every run: the copy's resource name and
// object number come from the page and the allocator, never from map order.
func TestWriter_Redact_ImageCopyIsDeterministic(t *testing.T) {
	t.Parallel()

	src := imageDoc([]string{placeLeft + " " + placeRight, placeLeft}, "", "", "")
	regions := []RedactRegion{{Page: 1, Rect: topLeft}, {Page: 1, Rect: wholeRight}, {Page: 2, Rect: bottomLeft}}
	first, _, err := redactWith(t, src, regions)
	if err != nil {
		t.Fatalf("Redact: %v", err)
	}
	// The pixel check writes nothing, and the first run passed it: the
	// repeats skip it, so the test costs one check rather than five.
	for range 4 {
		again, _, err := redactWith(t, src, regions, WithoutRedactionPixelCheck())
		if err != nil {
			t.Fatalf("Redact: %v", err)
		}
		if !bytes.Equal(first, again) {
			t.Fatal("two runs over the same input wrote different bytes")
		}
	}
}

// TestWriter_Redact_ImageCopyWithTheOtherOptions pins that a shared image is
// blanked in a copy under the raster fallback and the pixel check as it is
// without them. The fallback runs because a form fills a square inside the
// region, and the pixel check reads the copy as the blanked image it is.
func TestWriter_Redact_ImageCopyWithTheOtherOptions(t *testing.T) {
	t.Parallel()

	fill := "0 0 40 40 re f"
	form := "30 0 obj\n<< /Type /XObject /Subtype /Form /BBox [0 0 40 40] /Length " + strconv.Itoa(len(fill)) +
		" >>\nstream\n" + fill + "\nendstream\nendobj\n"
	withForm := imageDoc([]string{placeLeft + " " + placeRight + " q 1 0 0 1 150 420 cm /Fm9 Do Q", placeLeft}, "", "", form)
	withForm = bytes.Replace(withForm, []byte("/XObject << /Im1 7 0 R >>"), []byte("/XObject << /Im1 7 0 R /Fm9 30 0 R >>"), 1)
	// The bottom half of the left placement and the form's square: a
	// placement the region covers whole is removed, not blanked in a copy.
	region := RedactRegion{Page: 1, Rect: [4]float64{90, 410, 210, 545}}

	for _, tc := range []struct {
		name   string
		src    []byte
		opt    RedactOption
		raster bool
	}{
		{"raster fallback", withForm, WithRedactionRasterFallback(), true},
		{"pixel check", imageDoc([]string{placeLeft + " " + placeRight, placeLeft}, "", "", ""), WithRedactionPixelCheck(), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, report, err := redactWith(t, tc.src, []RedactRegion{region}, tc.opt)
			if err != nil {
				t.Fatalf("Redact: %v", err)
			}
			if got := report.ImagesBlankedInCopy[1]; len(got) != 1 || got[0] != "Im1" {
				t.Errorf("ImagesBlankedInCopy = %v, want Im1 on page 1", report.ImagesBlankedInCopy)
			}
			if tc.raster && report.RasterisedUnits == 0 {
				t.Error("RasterisedUnits = 0, want the form rasterised")
			}
			if got := renderedGray(t, out, 1, copyLeftBottom); !nearGray(got, 0) {
				t.Errorf("covered placement = %d, want 0", got)
			}
			if got := renderedGray(t, out, 1, copyRight); !nearGray(got, 0x80) {
				t.Errorf("placement outside the region = %d, want 0x80", got)
			}
			if got := renderedGray(t, out, 2, copyLeftTop); !nearGray(got, 0x80) {
				t.Errorf("page 2's placement = %d, want 0x80", got)
			}
		})
	}
}
