package recolor

import (
	"context"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/icc"
	pdfimage "github.com/gotenberg/litho/pkg/internal/graphics/image"
)

// dimLiftedGreyProfile builds a version 2.1 grey display profile whose tone
// curve starts at a luminance of 1/8, so black point compensation (ISO 18619)
// moves its black, and whose media white is 60% of D50, so absolute
// colorimetric (ICC.1:2001-04, 4.6) reads its white as a grey.
func dimLiftedGreyProfile() []byte {
	be32 := binary.BigEndian.AppendUint32
	s15 := func(b []byte, v float64) []byte { return be32(b, uint32(int32(v*65536+0.5))) }
	wtpt := []byte("XYZ \x00\x00\x00\x00")
	for _, v := range []float64{0.9642 * 0.6, 0.6, 0.8249 * 0.6} {
		wtpt = s15(wtpt, v)
	}
	trc := []byte("curv\x00\x00\x00\x00")
	trc = be32(trc, 2)
	trc = binary.BigEndian.AppendUint16(trc, 0x2000)
	trc = binary.BigEndian.AppendUint16(trc, 0xFFFF)
	tags := []struct {
		sig  string
		data []byte
	}{{"wtpt", wtpt}, {"kTRC", trc}}
	table := be32(nil, uint32(len(tags)))
	var body []byte
	offset := 128 + 4 + 12*len(tags)
	for _, tag := range tags {
		table = append(table, tag.sig...)
		table = be32(table, uint32(offset+len(body)))
		table = be32(table, uint32(len(tag.data)))
		body = append(body, tag.data...)
		for len(body)%4 != 0 {
			body = append(body, 0)
		}
	}
	header := make([]byte, 128)
	binary.BigEndian.PutUint32(header[0:], uint32(128+len(table)+len(body)))
	binary.BigEndian.PutUint32(header[8:], 0x02100000)
	copy(header[12:], "mntrGRAYXYZ ")
	copy(header[36:], "acsp")
	ill := s15(nil, 0.9642)
	ill = s15(ill, 1)
	ill = s15(ill, 0.8249)
	copy(header[68:], ill)
	return append(append(header, table...), body...)
}

// jpxBox wraps body in a JP2 box of the given type (T.800, I.4).
func jpxBox(kind string, body ...[]byte) []byte {
	b := slices.Concat(body...)
	n := len(b) + 8
	return slices.Concat([]byte{byte(n >> 24), byte(n >> 16), byte(n >> 8), byte(n)}, []byte(kind), b)
}

// iccJ10 wraps the J.10 codestream of the filter package's test data, one
// 8-bit component 1 wide and 9 high, in a JP2 file whose Colour
// Specification box carries profile by the restricted ICC method (T.800,
// I.5.3.3, method 2).
func iccJ10(t *testing.T, profile []byte) []byte {
	t.Helper()
	cs, err := os.ReadFile("../filter/testdata/annex_j10.j2k")
	if err != nil {
		t.Fatal(err)
	}
	return slices.Concat(
		jpxBox("jP  ", []byte{0x0D, 0x0A, 0x87, 0x0A}),
		jpxBox("ftyp", []byte("jp2 \x00\x00\x00\x00jp2 ")),
		jpxBox("jp2h",
			jpxBox("ihdr", []byte{0, 0, 0, 9, 0, 0, 0, 1, 0, 1, 7, 7, 0, 0}),
			jpxBox("colr", []byte{2, 0, 0}, profile)),
		jpxBox("jp2c", cs),
	)
}

// TestRenderingIntentJPXColourCopies checks that a JPEG 2000 image taking its
// colour from its own data (ISO 32000-1, 7.4.9) reaches sRGB under the
// graphics state it is painted under, as the renderer decodes it: a copy
// under /RI /AbsoluteColorimetric decodes under that intent, and one under
// /UseBlackPtComp /OFF without compensation (ISO 32000-1, 8.6.5.8; ISO
// 32000-2, Table 57), while the painting under the option's variant decodes
// as the decoder does with no graphics state.
func TestRenderingIntentJPXColourCopies(t *testing.T) {
	data := iccJ10(t, dimLiftedGreyProfile())
	img := fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width 1 /Height 9 /Filter /JPXDecode /Length %d >>\nstream\n%s\nendstream", len(data), data)
	f := onePage("/Resources << /ExtGState << /Abs << /RI /AbsoluteColorimetric >> /Off << /UseBlackPtComp /OFF >> >> /XObject << /Im 5 0 R >> >>",
		"/Im Do q /Abs gs /Im Do Q /Off gs /Im Do", img)
	src := f.bytes()
	r := recolorBytes(t, src, rgbTarget, naive(Options{BlackPointCompensation: true}))
	if got, want := r.text(0), "/Im Do\nq\n/Abs gs\n/Im_AbsoluteColorimetric Do\nQ\n/Off gs\n/Im_BPCOFF Do"; got != want {
		t.Fatalf("content:\n%s\nwant:\n%s", got, want)
	}
	if r.report.Intent.Copies != 1 || r.report.BlackPoint.Copies != 1 {
		t.Errorf("copies: intent %d, black point %d; want 1 and 1", r.report.Intent.Copies, r.report.BlackPoint.Copies)
	}

	ctx := context.Background()
	doc, _ := open(t, src)
	source, err := document.ResolveStream(ctx, doc, ast.PdfIndirectRef{ObjectNum: 5})
	if err != nil {
		t.Fatal(err)
	}
	// expected decodes the source as the renderer does under a graphics
	// state, or with none for a nil intent, as sRGB bytes.
	expected := func(intent *icc.Intent) []byte {
		t.Helper()
		var im image.Image
		var err error
		if intent == nil {
			im, err = pdfimage.DecodePainted(ctx, doc, "Im", source, nil, nil)
		} else {
			im, err = pdfimage.DecodePaintedUnder(ctx, doc, "Im", source, nil, nil, *intent)
		}
		if err != nil {
			t.Fatalf("decoding the source: %v", err)
		}
		var out []byte
		for y := range 9 {
			c := color.NRGBAModel.Convert(im.At(0, y)).(color.NRGBA)
			out = append(out, c.R, c.G, c.B)
		}
		return out
	}
	abs, off := icc.AbsoluteColorimetric, icc.RelativeColorimetric.WithBPC(icc.BPCOff)
	xobjects := r.dict(r.dict(r.pages[0].Dict["Resources"])["XObject"])
	got := map[ast.PdfName][]byte{}
	for name, want := range map[ast.PdfName][]byte{
		"Im":                      expected(nil),
		"Im_AbsoluteColorimetric": expected(&abs),
		"Im_BPCOFF":               expected(&off),
	} {
		got[name] = decode(t, r, xobjects[name])
		if !slices.Equal(got[name], want) {
			t.Errorf("%s: samples %v, want %v", name, got[name], want)
		}
	}
	for _, name := range []ast.PdfName{"Im_AbsoluteColorimetric", "Im_BPCOFF"} {
		if slices.Equal(got[name], got["Im"]) {
			t.Errorf("%s: samples %v, the same as the option's: the state did not reach the decoder", name, got[name])
		}
	}
}

// TestRenderingIntentJPXColourOption checks that the option's rendering
// intent reaches the decoder of a JPEG 2000 image taking its colour from its
// own data where the content sets none, as the graphics state's intent it
// stands in for (ISO 32000-1, 8.6.5.8): painted under no state, the image
// decodes under the option's /AbsoluteColorimetric; after an ri
// /RelativeColorimetric, its copy decodes as with no graphics state; and an
// image stating its own /Intent keeps it (8.9.5, Table 89).
func TestRenderingIntentJPXColourOption(t *testing.T) {
	data := iccJ10(t, dimLiftedGreyProfile())
	img := func(extra string) string {
		return fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width 1 /Height 9 /Filter /JPXDecode%s /Length %d >>\nstream\n%s\nendstream", extra, len(data), data)
	}
	f := onePage("/Resources << /XObject << /Im 5 0 R /Own 6 0 R >> >>",
		"/Im Do /Own Do q /RelativeColorimetric ri /Im Do Q", img(""), img(" /Intent /RelativeColorimetric"))
	src := f.bytes()
	r := recolorBytes(t, src, rgbTarget, naive(Options{Intent: icc.AbsoluteColorimetric, BlackPointCompensation: true}))
	if got, want := r.text(0), "/Im Do\n/Own Do\nq\n/RelativeColorimetric ri\n/Im_RelativeColorimetric Do\nQ"; got != want {
		t.Fatalf("content:\n%s\nwant:\n%s", got, want)
	}

	ctx := context.Background()
	doc, _ := open(t, src)
	// expected decodes object num as the renderer does under a graphics
	// state, or with none for a nil intent, as sRGB bytes.
	expected := func(num int, intent *icc.Intent) []byte {
		t.Helper()
		source, err := document.ResolveStream(ctx, doc, ast.PdfIndirectRef{ObjectNum: num})
		if err != nil {
			t.Fatal(err)
		}
		var im image.Image
		if intent == nil {
			im, err = pdfimage.DecodePainted(ctx, doc, "Im", source, nil, nil)
		} else {
			im, err = pdfimage.DecodePaintedUnder(ctx, doc, "Im", source, nil, nil, *intent)
		}
		if err != nil {
			t.Fatalf("decoding the source: %v", err)
		}
		var out []byte
		for y := range 9 {
			c := color.NRGBAModel.Convert(im.At(0, y)).(color.NRGBA)
			out = append(out, c.R, c.G, c.B)
		}
		return out
	}
	abs := icc.AbsoluteColorimetric
	if slices.Equal(expected(5, &abs), expected(5, nil)) {
		t.Fatal("the profile decodes alike under both intents: the test cannot discriminate")
	}
	xobjects := r.dict(r.dict(r.pages[0].Dict["Resources"])["XObject"])
	for name, want := range map[ast.PdfName][]byte{
		"Im":                      expected(5, &abs),
		"Own":                     expected(6, nil),
		"Im_RelativeColorimetric": expected(5, nil),
	} {
		if got := decode(t, r, xobjects[name]); !slices.Equal(got, want) {
			t.Errorf("%s: samples %v, want %v", name, got, want)
		}
	}
}

// TestRenderingIntentJPXColourInline checks the same of an inline image
// whose JPEG 2000 data gives its colour: painted after a gs setting
// /RI /AbsoluteColorimetric, it converts otherwise than with no state.
func TestRenderingIntentJPXColourInline(t *testing.T) {
	data := string(iccJ10(t, dimLiftedGreyProfile()))
	inline := "BI /W 1 /H 9 /F /JPXDecode ID " + data + " EI"
	res := "/Resources << /ExtGState << /Abs << /RI /AbsoluteColorimetric >> >> >>"
	under := recolor(t, onePage(res, "/Abs gs "+inline), rgbTarget, naive(Options{}))
	plain := recolor(t, onePage(res, inline), rgbTarget, naive(Options{}))
	// The content as written, from BI on: the converted dictionary and
	// samples, which the parsed operations leave out.
	written := func(r result) string {
		t.Helper()
		text := string(decode(t, r, r.pages[0].Dict["Contents"]))
		return text[strings.Index(text, "BI"):]
	}
	got, rel := written(under), written(plain)
	if under.report.Images.Converted+plain.report.Images.Converted != 2 || strings.Contains(rel, "JPXDecode") {
		t.Fatalf("inline images converted: %d and %d; with no state: %q", under.report.Images.Converted, plain.report.Images.Converted, rel)
	}
	if got == rel {
		t.Errorf("inline image under /Abs gs converts as with no state:\n%q", got)
	}
	if under.report.Intent.InlineImages != 1 {
		t.Errorf("Intent.InlineImages = %d, want 1", under.report.Intent.InlineImages)
	}
}
