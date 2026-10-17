package image

import (
	"context"
	"encoding/binary"
	"fmt"
	stdimage "image"
	"math"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/icc"
)

// flatTable builds a lut16Type table over n inputs whose two-point grid holds
// the grey L* at every point.
func flatTable(n int, lStar float64) []byte {
	b := []byte("mft2\x00\x00\x00\x00")
	b = append(b, byte(n), 3, 2, 0)
	for i := range 9 {
		v := uint32(0)
		if i%4 == 0 {
			v = 1 << 16
		}
		b = binary.BigEndian.AppendUint32(b, v)
	}
	b = binary.BigEndian.AppendUint16(b, 2)
	b = binary.BigEndian.AppendUint16(b, 2)
	for range n {
		b = binary.BigEndian.AppendUint16(b, 0)
		b = binary.BigEndian.AppendUint16(b, 65535)
	}
	for range 1 << n {
		// Legacy 16-bit Lab: L* 100 is 0xFF00, a* and b* 0 is 0x8000.
		b = binary.BigEndian.AppendUint16(b, uint16(math.Round(lStar/100*65280)))
		b = binary.BigEndian.AppendUint16(b, 0x8000)
		b = binary.BigEndian.AppendUint16(b, 0x8000)
	}
	for range 3 {
		b = binary.BigEndian.AppendUint16(b, 0)
		b = binary.BigEndian.AppendUint16(b, 65535)
	}
	return b
}

// flatProfile builds an ICC profile of the data colour space whose tables
// paint every colour a grey: L* 50 under the perceptual intent, and lStar
// under relative colorimetric. Neither the built-in profile nor a device
// formula gives such a result, so a pixel shows which reading it had.
func flatProfile(space string, lStar float64) []byte {
	n := map[string]int{"GRAY": 1, "RGB ": 3, "CMYK": 4}[space]
	tags := [][2]any{{"A2B0", flatTable(n, 50)}, {"A2B1", flatTable(n, lStar)}}
	header := make([]byte, 128)
	header[8] = 2
	copy(header[12:], "mntr")
	copy(header[16:], space)
	copy(header[20:], "Lab ")
	copy(header[36:], "acsp")
	table := binary.BigEndian.AppendUint32(nil, uint32(len(tags)))
	offset := 128 + 4 + 12*len(tags)
	var body []byte
	for _, tag := range tags {
		data := tag[1].([]byte)
		table = append(table, tag[0].(string)...)
		table = binary.BigEndian.AppendUint32(table, uint32(offset+len(body)))
		table = binary.BigEndian.AppendUint32(table, uint32(len(data)))
		body = append(body, data...)
	}
	out := append(append(header, table...), body...)
	binary.BigEndian.PutUint32(out, uint32(len(out)))
	return out
}

// profileObj writes a profile as an ICC profile stream object of n
// components.
func profileObj(n int, profile []byte) string {
	return fmt.Sprintf("<< /N %d /Length %d >>\nstream\n%s\nendstream", n, len(profile), profile)
}

// managedPDF builds a one-page document showing a width by 1 image in the
// colour space cs, its catalog carrying catalogExtra, with extra objects
// numbered from 5.
func managedPDF(t *testing.T, catalogExtra, cs, imageExtra string, width int, samples []byte, extra ...string) (context.Context, *document.Document, *document.Page) {
	t.Helper()
	return contextPDF(t, pdfContext{catalog: catalogExtra}, cs, imageExtra, width, samples, extra...)
}

// pdfContext is what contextPDF writes into the catalog, the page dictionary
// and the page's resource dictionary around the image.
type pdfContext struct {
	catalog, page, resources string
}

// contextPDF builds a one-page document showing /Im0, a width by 1 image in
// the colour space cs, with pc's entries in the catalog, the page and its
// resources, and extra objects numbered from 5.
func contextPDF(t *testing.T, pc pdfContext, cs, imageExtra string, width int, samples []byte, extra ...string) (context.Context, *document.Document, *document.Page) {
	t.Helper()
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R " + pc.catalog + " >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " + pc.page +
			" /Resources << /XObject << /Im0 4 0 R >> " + pc.resources + " >> >>",
		fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width %d /Height 1"+
			" /ColorSpace %s /BitsPerComponent 8 %s /Length %d >>\nstream\n%s\nendstream",
			width, cs, imageExtra, len(samples), samples),
	}
	return imagePDF(t, append(objs, extra...))
}

// wantPixels decodes the image and checks each pixel against want to within
// one level, the rounding two colour engines may differ by.
func wantPixels(t *testing.T, ctx context.Context, doc *document.Document, page *document.Page, want ...[3]int) {
	t.Helper()
	img, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
	if err != nil {
		t.Fatalf("DecodeImage: %v", err)
	}
	for x, w := range want {
		r, g, b, _ := img.At(x, 0).RGBA()
		got := [3]int{int(r >> 8), int(g >> 8), int(b >> 8)}
		for c := range 3 {
			if d := got[c] - w[c]; d < -1 || d > 1 {
				t.Errorf("pixel %d = %v, want %v", x, got, w)
				break
			}
		}
	}
}

// The sRGB colours of the grey L* 50 and L* 25, and of the process inks and
// black through the built-in CMYK profile, as LittleCMS 2.17 gives them.
var (
	grey50  = [3]int{119, 119, 119}
	grey25  = [3]int{59, 59, 59}
	cyan    = [3]int{0, 174, 239}
	magenta = [3]int{236, 0, 140}
	yellow  = [3]int{255, 242, 0}
	black   = [3]int{35, 31, 32}
)

// TestDecodeImage_DeviceCMYKReadsTheBuiltinProfile pins that a document with
// no output intent has its DeviceCMYK read through the built-in press
// profile. The device formula (1-c)(1-k) paints cyan (0, 255, 255) and black
// (0, 0, 0), colours no press prints.
func TestDecodeImage_DeviceCMYKReadsTheBuiltinProfile(t *testing.T) {
	t.Parallel()

	ctx, doc, page := managedPDF(t, "", "/DeviceCMYK", "", 4,
		[]byte{255, 0, 0, 0, 0, 255, 0, 0, 0, 0, 255, 0, 0, 0, 0, 255})
	wantPixels(t, ctx, doc, page, cyan, magenta, yellow, black)
}

// TestDecodeImage_ICCBasedReadsItsProfile pins that an ICCBased image is read
// through its embedded profile (ISO 32000-1, 8.6.5.5), and that a profile
// litho cannot use falls back to the device space /N names.
func TestDecodeImage_ICCBasedReadsItsProfile(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		n       int
		profile []byte
		samples []byte
		want    [][3]int
	}{
		{"grey", 1, flatProfile("GRAY", 25), []byte{0, 255}, [][3]int{grey25, grey25}},
		{"RGB", 3, flatProfile("RGB ", 25), []byte{255, 0, 0, 0, 0, 255}, [][3]int{grey25, grey25}},
		{"CMYK", 4, flatProfile("CMYK", 25), []byte{255, 0, 0, 0, 0, 0, 0, 255}, [][3]int{grey25, grey25}},
		{
			"an unreadable profile reads as DeviceRGB", 3, []byte("not a profile"),
			[]byte{255, 0, 0, 10, 20, 30}, [][3]int{{255, 0, 0}, {10, 20, 30}},
		},
		{
			"a profile of another space reads as DeviceCMYK", 4, flatProfile("RGB ", 25),
			[]byte{255, 0, 0, 0, 0, 0, 0, 255}, [][3]int{cyan, black},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, doc, page := managedPDF(t, "", "[/ICCBased 5 0 R]", "", len(tc.want), tc.samples,
				profileObj(tc.n, tc.profile))
			wantPixels(t, ctx, doc, page, tc.want...)
		})
	}
}

// TestDecodeImage_DeviceCMYKReadsTheOutputIntent pins the order DeviceCMYK's
// profile is chosen in: the document's output intent (ISO 32000-1, 14.11.5),
// then the caller's profile, then the built-in one.
func TestDecodeImage_DeviceCMYKReadsTheOutputIntent(t *testing.T) {
	t.Parallel()

	samples := []byte{255, 0, 0, 0}
	outputIntent := "/OutputIntents [<< /Type /OutputIntent /S /GTS_PDFA1 /DestOutputProfile 5 0 R >>]"
	for _, tc := range []struct {
		name   string
		intent bool
		caller []byte
		want   [3]int
	}{
		{"the output intent", true, nil, grey25},
		{"the output intent over the caller's profile", true, flatProfile("CMYK", 50), grey25},
		{"the caller's profile", false, flatProfile("CMYK", 50), grey50},
		{"the built-in profile", false, nil, cyan},
		{"the built-in profile over a caller's RGB profile", false, flatProfile("RGB ", 50), cyan},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			catalog := ""
			if tc.intent {
				catalog = outputIntent
			}
			ctx, doc, page := managedPDF(t, catalog, "/DeviceCMYK", "", 1, samples,
				profileObj(4, flatProfile("CMYK", 25)))
			doc.CMYKProfile = tc.caller
			wantPixels(t, ctx, doc, page, tc.want)
		})
	}
}

// TestDecodeImage_RenderingIntent pins that an image's /Intent picks the
// profile's table (ISO 32000-1, 8.6.5.8), relative colorimetric when it
// states none.
func TestDecodeImage_RenderingIntent(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		intent string
		want   [3]int
	}{
		{"", grey25},
		{"/Intent /RelativeColorimetric", grey25},
		{"/Intent /Perceptual", grey50},
		{"/Intent /NoSuchIntent", grey25},
	} {
		ctx, doc, page := managedPDF(t, "", "[/ICCBased 5 0 R]", tc.intent, 1, []byte{10, 20, 30},
			profileObj(3, flatProfile("RGB ", 25)))
		wantPixels(t, ctx, doc, page, tc.want)
	}
}

// TestDecodeImage_ManagedPaletteAndTints pins that the colour table of an
// indexed image and the alternate space of a Separation image are read
// through their profiles too.
func TestDecodeImage_ManagedPaletteAndTints(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		cs      string
		samples []byte
		extra   []string
		want    [][3]int
	}{
		{
			"indexed over ICCBased", "[/Indexed [/ICCBased 5 0 R] 1 <FF000000 00000000>]",
			[]byte{0, 1}, []string{profileObj(4, flatProfile("CMYK", 25))}, [][3]int{grey25, grey25},
		},
		{
			"indexed over DeviceCMYK", "[/Indexed /DeviceCMYK 1 <FF000000 000000FF>]",
			[]byte{0, 1}, nil, [][3]int{cyan, black},
		},
		{
			"Separation over ICCBased", "[/Separation /Spot [/ICCBased 5 0 R] 6 0 R]",
			[]byte{0, 255}, []string{
				profileObj(4, flatProfile("CMYK", 25)),
				"<< /FunctionType 2 /Domain [0 1] /Range [0 1 0 1 0 1 0 1] /C0 [0 0 0 0] /C1 [1 0 0 0] /N 1 >>",
			}, [][3]int{grey25, grey25},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, doc, page := managedPDF(t, "", tc.cs, "", len(tc.want), tc.samples, tc.extra...)
			wantPixels(t, ctx, doc, page, tc.want...)
		})
	}
}

// TestMatteRGB_ReadsTheProfile pins that a soft mask's /Matte is converted as
// the image's samples are, so the unblending subtracts the colour the pixels
// were blended with.
func TestMatteRGB_ReadsTheProfile(t *testing.T) {
	t.Parallel()

	ctx, doc, _ := managedPDF(t, "", "/DeviceCMYK", "", 1, []byte{0, 0, 0, 0},
		profileObj(4, flatProfile("CMYK", 25)))
	for _, tc := range []struct {
		cs   ast.PdfObject
		want [3]int
	}{
		{ast.PdfName("DeviceCMYK"), cyan},
		{ast.PdfArray{ast.PdfName("ICCBased"), ast.PdfIndirectRef{ObjectNum: 5}}, grey25},
	} {
		r, g, b, ok := matteRGB(ctx, doc, ast.PdfDict{"ColorSpace": tc.cs}, colorContext{}, []float64{1, 0, 0, 0})
		got := [3]int{int(r), int(g), int(b)}
		for c := range 3 {
			if d := got[c] - tc.want[c]; !ok || d < -1 || d > 1 {
				t.Errorf("%v: matte = %v, %t, want %v", tc.cs, got, ok, tc.want)
				break
			}
		}
	}
}

// TestDecodeImage_DefaultColorSpaces pins that a default colour space in the
// painting resources stands in for a device space (ISO 32000-1, 8.6.5.6),
// as the image's own space, as the base of an Indexed space and as the
// alternate of a Separation space, and that a default of the wrong kind is
// ignored.
func TestDecodeImage_DefaultColorSpaces(t *testing.T) {
	t.Parallel()

	tint := "<< /FunctionType 2 /Domain [0 1] /Range [0 1 0 1 0 1 0 1] /C0 [0 0 0 0] /C1 [1 0 0 0] /N 1 >>"
	for _, tc := range []struct {
		name     string
		defaults string
		cs       string
		samples  []byte
		extra    []string
		want     [3]int
	}{
		{"DefaultCMYK", "/DefaultCMYK [/ICCBased 5 0 R]", "/DeviceCMYK", []byte{255, 0, 0, 0},
			[]string{profileObj(4, flatProfile("CMYK", 25))}, grey25},
		{"DefaultRGB", "/DefaultRGB [/ICCBased 5 0 R]", "/DeviceRGB", []byte{255, 0, 0},
			[]string{profileObj(3, flatProfile("RGB ", 25))}, grey25},
		{"DefaultGray", "/DefaultGray [/ICCBased 5 0 R]", "/DeviceGray", []byte{255},
			[]string{profileObj(1, flatProfile("GRAY", 25))}, grey25},
		{"an Indexed base", "/DefaultCMYK [/ICCBased 5 0 R]", "[/Indexed /DeviceCMYK 0 <FF000000>]", []byte{0},
			[]string{profileObj(4, flatProfile("CMYK", 25))}, grey25},
		{"a Separation alternate", "/DefaultCMYK [/ICCBased 5 0 R]", "[/Separation /Spot /DeviceCMYK 6 0 R]", []byte{255},
			[]string{profileObj(4, flatProfile("CMYK", 25)), tint}, grey25},
		{"a default of another component count", "/DefaultRGB [/ICCBased 5 0 R]", "/DeviceRGB", []byte{10, 20, 30},
			[]string{profileObj(4, flatProfile("CMYK", 25))}, [3]int{10, 20, 30}},
		{"a default that is not CIE-based", "/DefaultCMYK /DeviceRGB", "/DeviceCMYK", []byte{255, 0, 0, 0},
			nil, cyan},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pc := pdfContext{resources: "/ColorSpace << " + tc.defaults + " >>"}
			ctx, doc, page := contextPDF(t, pc, tc.cs, "", 1, tc.samples, tc.extra...)
			wantPixels(t, ctx, doc, page, tc.want)
		})
	}
}

// TestDecodeImage_DefaultsOfTheFormThatPaints pins that an image a form draws
// takes the default colour spaces of the form's own resources, not the
// page's: a form is a content stream of its own (ISO 32000-2, 8.6.5.7). The
// listing's Decode function reads the image the same way.
func TestDecodeImage_DefaultsOfTheFormThatPaints(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		formSpace string
		want      [3]int
	}{
		{"the page's default does not reach into the form", "", cyan},
		{"the form's own default applies", "/ColorSpace << /DefaultCMYK [/ICCBased 6 0 R] >>", grey25},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cyanSample := []byte{255, 0, 0, 0}
			objs := []string{
				"<< /Type /Catalog /Pages 2 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources <<" +
					" /XObject << /Fm0 4 0 R >> /ColorSpace << /DefaultCMYK [/ICCBased 6 0 R] >> >> >>",
				"<< /Type /XObject /Subtype /Form /BBox [0 0 1 1] /Resources << /XObject << /Im1 5 0 R >> " +
					tc.formSpace + " >> /Length 0 >>\nstream\n\nendstream",
				fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceCMYK"+
					" /BitsPerComponent 8 /Length 4 >>\nstream\n%s\nendstream", cyanSample),
				profileObj(4, flatProfile("CMYK", 25)),
			}
			ctx, doc, page := imagePDF(t, objs)
			img, err := DecodeImage(ctx, doc, page, ast.PdfName("Im1"))
			if err != nil {
				t.Fatalf("DecodeImage: %v", err)
			}
			listed := ExtractImagesRecursive(ctx, doc, page)
			if len(listed) != 1 || listed[0].Decode == nil {
				t.Fatalf("listed %d images, want 1 with a Decode function", len(listed))
			}
			fromListing, err := listed[0].Decode(ctx)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			for name, im := range map[string]stdimage.Image{"DecodeImage": img, "the listing": fromListing} {
				r, g, b, _ := im.At(0, 0).RGBA()
				got := [3]int{int(r >> 8), int(g >> 8), int(b >> 8)}
				for c := range 3 {
					if d := got[c] - tc.want[c]; d < -1 || d > 1 {
						t.Errorf("%s: %v, want %v", name, got, tc.want)
						break
					}
				}
			}
		})
	}
}

// TestDecodeImage_PageOutputIntents pins that a page's own output intents
// replace the document's for its DeviceCMYK (ISO 32000-2, 14.11.5).
func TestDecodeImage_PageOutputIntents(t *testing.T) {
	t.Parallel()

	intents := func(obj int) string {
		return fmt.Sprintf("/OutputIntents [<< /Type /OutputIntent /S /GTS_PDFX /DestOutputProfile %d 0 R >>]", obj)
	}
	profiles := []string{profileObj(4, flatProfile("CMYK", 50)), profileObj(4, flatProfile("CMYK", 25))}
	for _, tc := range []struct {
		name string
		pc   pdfContext
		want [3]int
	}{
		{"the page's", pdfContext{catalog: intents(5), page: intents(6)}, grey25},
		{"the document's", pdfContext{catalog: intents(5)}, grey50},
		{"the document's past a page intent with no CMYK profile", pdfContext{catalog: intents(5), page: "/OutputIntents []"}, grey50},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, doc, page := contextPDF(t, tc.pc, "/DeviceCMYK", "", 1, []byte{255, 0, 0, 0}, profiles...)
			wantPixels(t, ctx, doc, page, tc.want)
		})
	}
}

// TestDecodeImage_DeviceRGBAndGrayReadTheOutputIntent pins that DeviceRGB
// and DeviceGray are read through an output intent's destination profile of
// their own colour family (ISO 32000-1, 14.11.5; ISO 19005-2, 6.2.4.3), the
// page's before the document's, wherever the device space stands: as the
// image's space, the base of an Indexed space or the alternate of a
// Separation space. A profile of another family, and a default colour space
// (ISO 32000-1, 8.6.5.6), leave the output intent unused.
func TestDecodeImage_DeviceRGBAndGrayReadTheOutputIntent(t *testing.T) {
	t.Parallel()

	intents := func(objs ...int) string {
		s := "/OutputIntents ["
		for _, obj := range objs {
			s += fmt.Sprintf("<< /Type /OutputIntent /S /GTS_PDFA1 /DestOutputProfile %d 0 R >>", obj)
		}
		return s + "]"
	}
	rgb25, rgb50 := profileObj(3, flatProfile("RGB ", 25)), profileObj(3, flatProfile("RGB ", 50))
	gray25, cmyk25 := profileObj(1, flatProfile("GRAY", 25)), profileObj(4, flatProfile("CMYK", 25))
	tint := "<< /FunctionType 2 /Domain [0 1] /Range [0 1 0 1 0 1] /C0 [0 0 0] /C1 [1 0 0] /N 1 >>"
	for _, tc := range []struct {
		name    string
		pc      pdfContext
		cs      string
		samples []byte
		extra   []string
		want    [3]int
	}{
		{"DeviceRGB through an RGB profile", pdfContext{catalog: intents(5)}, "/DeviceRGB",
			[]byte{10, 20, 30}, []string{rgb25}, grey25},
		{"DeviceGray through a Gray profile", pdfContext{catalog: intents(5)}, "/DeviceGray",
			[]byte{10}, []string{gray25}, grey25},
		{"the first profile of the family", pdfContext{catalog: intents(5, 6)}, "/DeviceRGB",
			[]byte{10, 20, 30}, []string{cmyk25, rgb25}, grey25},
		{"the page's before the document's", pdfContext{catalog: intents(5), page: intents(6)}, "/DeviceRGB",
			[]byte{10, 20, 30}, []string{rgb50, rgb25}, grey25},
		{"an Indexed base", pdfContext{catalog: intents(5)}, "[/Indexed /DeviceRGB 0 <0A141E>]",
			[]byte{0}, []string{rgb25}, grey25},
		{"a Separation alternate", pdfContext{catalog: intents(5)}, "[/Separation /Spot /DeviceRGB 6 0 R]",
			[]byte{255}, []string{rgb25, tint}, grey25},
		{"an ICCBased space whose profile cannot be used", pdfContext{catalog: intents(5)}, "[/ICCBased 6 0 R]",
			[]byte{10, 20, 30}, []string{rgb25, "<< /N 3 /Length 3 >>\nstream\nbad\nendstream"}, grey25},
		{"DeviceRGB past a CMYK profile", pdfContext{catalog: intents(5)}, "/DeviceRGB",
			[]byte{10, 20, 30}, []string{cmyk25}, [3]int{10, 20, 30}},
		{"DeviceGray past an RGB profile", pdfContext{catalog: intents(5)}, "/DeviceGray",
			[]byte{10}, []string{rgb25}, [3]int{10, 10, 10}},
		{"a default colour space first", pdfContext{catalog: intents(5), resources: "/ColorSpace << /DefaultRGB [/ICCBased 6 0 R] >>"},
			"/DeviceRGB", []byte{10, 20, 30}, []string{rgb25, rgb50}, grey50},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, doc, page := contextPDF(t, tc.pc, tc.cs, "", 1, tc.samples, tc.extra...)
			wantPixels(t, ctx, doc, page, tc.want)
		})
	}
}

// TestColorTransform_SRGBOutputIntentReadsAsIs pins that an sRGB destination
// profile, which gives every colour back as it came, leaves DeviceRGB samples
// unconverted rather than rounded through the profile.
func TestColorTransform_SRGBOutputIntentReadsAsIs(t *testing.T) {
	t.Parallel()

	ctx, doc, page := managedPDF(t, "/OutputIntents [<< /Type /OutputIntent /S /GTS_PDFA1 /DestOutputProfile 5 0 R >>]",
		"/DeviceRGB", "", 1, []byte{0, 175, 255}, profileObj(3, icc.SRGBProfile()))
	cc := colorContext{resources: page.Resources, page: page.Dict}
	if tr := colorTransform(ctx, doc, ast.PdfName("DeviceRGB"), "DeviceRGB", 3, icc.RelativeColorimetric, cc); tr != nil {
		t.Errorf("an sRGB output intent gave a transform, want none")
	}
	img, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
	if err != nil {
		t.Fatalf("DecodeImage: %v", err)
	}
	if r, g, b, _ := img.At(0, 0).RGBA(); [3]uint32{r >> 8, g >> 8, b >> 8} != [3]uint32{0, 175, 255} {
		t.Errorf("pixel = %d %d %d, want 0 175 255", r>>8, g>>8, b>>8)
	}
}

// TestDecodeImage_CIEBasedSpacesIgnoreTheOutputIntent pins that a CIE-based
// space, which litho reads in the device space of its component count, keeps
// its own reading under an output intent of that family: the space is
// characterised by its own dictionary (ISO 32000-1, 8.6.5.2-8.6.5.4).
func TestDecodeImage_CIEBasedSpacesIgnoreTheOutputIntent(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		cs, profile string
		samples     []byte
	}{
		{"[/CalRGB << /WhitePoint [0.9505 1 1.089] /Gamma [2.2 2.2 2.2] >>]", profileObj(3, flatProfile("RGB ", 25)), []byte{10, 20, 30}},
		{"[/CalGray << /WhitePoint [0.9505 1 1.089] /Gamma 2.2 >>]", profileObj(1, flatProfile("GRAY", 25)), []byte{10}},
		{"[/Lab << /WhitePoint [0.9505 1 1.089] /Range [-100 100 -100 100] >>]", profileObj(3, flatProfile("RGB ", 25)), []byte{128, 100, 160}},
	} {
		pixel := func(catalog string) [3]uint32 {
			ctx, doc, page := managedPDF(t, catalog, tc.cs, "", 1, tc.samples, tc.profile)
			img, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
			if err != nil {
				t.Fatalf("%s: DecodeImage: %v", tc.cs, err)
			}
			r, g, b, _ := img.At(0, 0).RGBA()
			return [3]uint32{r >> 8, g >> 8, b >> 8}
		}
		without := pixel("")
		with := pixel("/OutputIntents [<< /Type /OutputIntent /S /GTS_PDFA1 /DestOutputProfile 5 0 R >>]")
		if with != without {
			t.Errorf("%s: %v under the output intent, %v without", tc.cs, with, without)
		}
	}
}

// TestDecodeImage_JPXEmbeddedProfile pins that a JPEG 2000 image with no
// /ColorSpace is read through the ICC profile of its colour specification
// (ISO 32000-1, 7.4.9), and that a /ColorSpace, when present, wins over it.
func TestDecodeImage_JPXEmbeddedProfile(t *testing.T) {
	t.Parallel()

	box := func(kind string, body ...[]byte) []byte {
		b := slices.Concat(body...)
		n := len(b) + 8
		return slices.Concat([]byte{byte(n >> 24), byte(n >> 16), byte(n >> 8), byte(n)}, []byte(kind), b)
	}
	// One grey channel, 1 by 9, specified by a restricted ICC profile
	// (method 2) that paints every level L* 25 grey.
	jp2 := slices.Concat(
		box("jP  ", []byte{0x0D, 0x0A, 0x87, 0x0A}),
		box("ftyp", []byte("jp2 \x00\x00\x00\x00jp2 ")),
		box("jp2h",
			box("ihdr", []byte{0, 0, 0, 9, 0, 0, 0, 1, 0, 1, 7, 7, 0, 0}),
			box("colr", []byte{2, 0, 0}, flatProfile("GRAY", 25))),
		box("jp2c", readTestJPX(t, "testdata/annex_j10.j2k")),
	)
	for _, tc := range []struct {
		entries string
		want    uint32
	}{
		{"", 59},
		{"/ColorSpace /DeviceGray", 101},
	} {
		ctx, doc, page := imagePDF(t, jpxPDF(1, 9, tc.entries, jp2))
		img, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
		if err != nil {
			t.Fatalf("%q: DecodeImage: %v", tc.entries, err)
		}
		if got := greyAt(img, 0, 0); got < tc.want-1 || got > tc.want+1 {
			t.Errorf("%q: grey %d, want %d", tc.entries, got, tc.want)
		}
	}
}
