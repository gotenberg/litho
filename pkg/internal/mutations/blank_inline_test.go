package mutations

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/hex"
	"errors"
	"image"
	"image/jpeg"
	"io"
	"math/rand/v2"
	"strconv"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// inlineFixture builds a one-page document whose content is body, with a
// Helvetica font as /F1, the colour spaces resources names under /ColorSpace,
// and a form XObject /Fm1 whose content is form.
func inlineFixture(t *testing.T, body, colorSpaces, form string) (*document.Document, *document.Page) {
	t.Helper()
	ctx := context.Background()

	data := []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n" +
		"5 0 obj\n<< /Font << /F1 4 0 R >> /XObject << /Fm1 7 0 R >> /ColorSpace << " + colorSpaces + " >> >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 5 0 R /Contents 6 0 R >>\nendobj\n" +
		"6 0 obj\n<< /Length " + strconv.Itoa(len(body)) + " >>\nstream\n" + body + "\nendstream\nendobj\n" +
		"7 0 obj\n<< /Type /XObject /Subtype /Form /BBox [0 0 612 792] /Resources 5 0 R /Length " +
		strconv.Itoa(len(form)) + " >>\nstream\n" + form + "\nendstream\nendobj\n" +
		"trailer\n<< /Size 8 /Root 1 0 R >>\n%%EOF\n")

	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil || len(pages) == 0 {
		t.Fatalf("ResolvePages: %v (%d pages)", err, len(pages))
	}
	return doc, pages[0]
}

// placeInline paints an inline image of the given dictionary entries and data
// at (100,600), 40 wide and 20 high, then draws "KEEP" well clear of it.
func placeInline(entries string, data []byte) string {
	return "q 40 0 0 20 100 600 cm BI " + entries + " ID " + string(data) + "\nEI Q " +
		"BT /F1 12 Tf 100 300 Td (KEEP) Tj ET"
}

// leftHalf covers the left half of the image placeInline paints.
var leftHalf = [][4]float64{{90, 590, 120, 630}}

// blankedInline is an inline image as the redaction wrote it.
type blankedInline struct {
	dict    ast.PdfDict
	samples []byte
}

// redactedInline returns the one inline image the page's rewritten content
// paints, with its data decoded, and checks that the rest of the page
// survived.
func redactedInline(t *testing.T, alloc *writer.ObjAllocator, page *document.Page) blankedInline {
	t.Helper()
	ctx := context.Background()

	got := newPageContents(t, alloc, page.Ref.ObjectNum)
	if !bytes.Contains(got, []byte("(KEEP) Tj")) {
		t.Errorf("text outside the region was lost: %q", got)
	}
	ops, ranges, err := content.ParseStreamDataWithOffsets(ctx, got, content.SplitGluedOperators())
	if err != nil {
		t.Fatalf("parsing the rewritten content: %v", err)
	}
	var found []blankedInline
	for i, op := range ops {
		if op.Operator != "BI" {
			continue
		}
		dict, _ := op.Operands[0].(ast.PdfDict)
		loc, err := locateInlineImage(ctx, got, ranges[i], dict, content.NewInlineImageEnds(got))
		if err != nil {
			t.Fatalf("locating the rewritten inline image: %v", err)
		}
		raw := bytes.TrimRight(loc.data, " \n")
		if f, ok := dict["F"].(ast.PdfArray); ok && len(f) == 2 && f[0] == ast.PdfName("AHx") {
			hexData := bytes.TrimSuffix(raw, []byte(">"))
			decoded := make([]byte, hex.DecodedLen(len(hexData)))
			if _, err := hex.Decode(decoded, hexData); err != nil {
				t.Fatalf("hex: %v", err)
			}
			raw = decoded
		} else if dict["F"] != ast.PdfName("Fl") {
			t.Fatalf("/F = %v, want /Fl or [/AHx /Fl]", dict["F"])
		}
		r, err := zlib.NewReader(bytes.NewReader(raw))
		if err != nil {
			t.Fatalf("zlib: %v", err)
		}
		samples, err := io.ReadAll(r)
		if err != nil {
			t.Fatalf("inflating: %v", err)
		}
		found = append(found, blankedInline{dict: dict, samples: samples})
	}
	if len(found) != 1 {
		t.Fatalf("the rewritten content paints %d inline images, want 1: %q", len(found), got)
	}
	if _, ok := found[0].dict["D"]; ok {
		t.Errorf("the replacement carries /D, which the samples already fold in")
	}
	return found[0]
}

func redactInline(t *testing.T, doc *document.Document, page *document.Page) (*writer.ObjAllocator, RedactReport) {
	t.Helper()
	alloc := writer.NewObjAllocator(20)
	report, err := RedactPageRegions(context.Background(), alloc, doc, page, leftHalf)
	if err != nil {
		t.Fatalf("RedactPageRegions: %v", err)
	}
	if len(report.BlankedImages) != 1 {
		t.Errorf("BlankedImages = %q, want one inline image", report.BlankedImages)
	}
	return alloc, report
}

func TestRedactPageRegions_BlanksAnUncompressedInlineImage(t *testing.T) {
	// Four columns by two rows of DeviceRGB, each sample its own colour.
	data := []byte{
		10, 20, 30, 40, 50, 60, 70, 80, 90, 100, 110, 120,
		11, 21, 31, 41, 51, 61, 71, 81, 91, 101, 111, 121,
	}
	doc, page := inlineFixture(t, placeInline("/W 4 /H 2 /CS /RGB /BPC 8", data), "", "")
	alloc, _ := redactInline(t, doc, page)

	got := redactedInline(t, alloc, page)
	if got.dict["CS"] != ast.PdfName("RGB") || got.dict["BPC"] != ast.PdfInt(8) {
		t.Errorf("dictionary = %v, want /CS /RGB at /BPC 8", got.dict)
	}
	// The region holds the centres of columns 0 and 1 (x 105 and 115) only.
	want := []byte{
		0, 0, 0, 0, 0, 0, 70, 80, 90, 100, 110, 120,
		0, 0, 0, 0, 0, 0, 71, 81, 91, 101, 111, 121,
	}
	if !bytes.Equal(got.samples, want) {
		t.Errorf("samples = %v, want %v", got.samples, want)
	}
}

func TestRedactPageRegions_BlanksAFlateInlineImage(t *testing.T) {
	gray := []byte{200, 201, 202, 203, 204, 205, 206, 207}
	compressed, err := flateCompress(gray)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(compressed, []byte("EI")) {
		t.Fatal("fixture data holds EI")
	}
	doc, page := inlineFixture(t, placeInline("/W 4 /H 2 /CS /G /BPC 8 /F /Fl", compressed), "", "")
	alloc, _ := redactInline(t, doc, page)

	got := redactedInline(t, alloc, page)
	want := []byte{0, 0, 202, 203, 0, 0, 206, 207}
	if !bytes.Equal(got.samples, want) {
		t.Errorf("samples = %v, want %v", got.samples, want)
	}
}

func TestRedactPageRegions_BlanksADCTInlineImage(t *testing.T) {
	src := image.NewGray(image.Rect(0, 0, 16, 8))
	for i := range src.Pix {
		src.Pix[i] = 180
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, src, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(buf.Bytes(), []byte("EI")) {
		t.Fatal("fixture data holds EI")
	}
	doc, page := inlineFixture(t, placeInline("/W 16 /H 8 /CS /G /BPC 8 /F /DCT", buf.Bytes()), "", "")
	alloc, _ := redactInline(t, doc, page)

	got := redactedInline(t, alloc, page)
	if len(got.samples) != 16*8 {
		t.Fatalf("got %d samples, want %d", len(got.samples), 16*8)
	}
	// Columns 0 to 7 have their centres at x 101.25 to 118.75, inside the
	// region; 8 to 15 lie outside and keep the flat grey.
	for y := range 8 {
		for x := range 16 {
			v := got.samples[y*16+x]
			switch {
			case x < 8 && v != 0:
				t.Errorf("sample (%d,%d) = %d inside the region, want 0", x, y, v)
			case x >= 8 && (v < 178 || v > 182):
				t.Errorf("sample (%d,%d) = %d outside the region, want about 180", x, y, v)
			}
		}
	}
}

func TestRedactPageRegions_BlanksADCTInlineImageAfterACRLF(t *testing.T) {
	src := image.NewGray(image.Rect(0, 0, 16, 8))
	for i := range src.Pix {
		src.Pix[i] = 180
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, src, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatal(err)
	}
	// ID then CR LF: the LF is a second white-space byte before the SOI.
	body := "q 40 0 0 20 100 600 cm BI /W 16 /H 8 /CS /G /BPC 8 /F /DCT ID\r\n" + buf.String() + "\nEI Q " +
		"BT /F1 12 Tf 100 300 Td (KEEP) Tj ET"
	doc, page := inlineFixture(t, body, "", "")
	alloc, _ := redactInline(t, doc, page)

	got := redactedInline(t, alloc, page)
	if len(got.samples) != 16*8 || got.samples[0] != 0 || got.samples[15] < 178 {
		t.Errorf("samples = %v, want the left half black and the right grey", got.samples)
	}
}

func TestRedactPageRegions_BlanksAnInlineStencilMask(t *testing.T) {
	// Eight columns by two rows, every sample painting under /D [1 0]: a 1 bit
	// paints, so 0xFF paints all eight.
	doc, page := inlineFixture(t, placeInline("/IM true /W 8 /H 2 /BPC 1 /D [1 0]", []byte{0xFF, 0xFF}), "", "")
	alloc, _ := redactInline(t, doc, page)

	got := redactedInline(t, alloc, page)
	if got.dict["IM"] != ast.PdfBool(true) || got.dict["BPC"] != ast.PdfInt(1) {
		t.Errorf("dictionary = %v, want a stencil mask at one bit", got.dict)
	}
	if _, ok := got.dict["CS"]; ok {
		t.Errorf("the stencil mask names a colour space: %v", got.dict)
	}
	// Columns 0 to 3 (centres x 102.5 to 117.5) are covered and turn
	// unpainted, a 1 bit under the default /Decode; 4 to 7 keep painting, 0.
	want := []byte{0xF0, 0xF0}
	if !bytes.Equal(got.samples, want) {
		t.Errorf("samples = %08b, want %08b", got.samples, want)
	}
}

func TestRedactPageRegions_BlanksAOneBitInlinePicture(t *testing.T) {
	// One-bit DeviceGray: 1 is white. Row 0 white, row 1 alternating.
	doc, page := inlineFixture(t, placeInline("/W 8 /H 2 /CS /DeviceGray /BPC 1", []byte{0xFF, 0xAA}), "", "")
	alloc, _ := redactInline(t, doc, page)

	got := redactedInline(t, alloc, page)
	if got.dict["CS"] != ast.PdfName("DeviceGray") || got.dict["BPC"] != ast.PdfInt(8) {
		t.Errorf("dictionary = %v, want /CS /DeviceGray at /BPC 8", got.dict)
	}
	want := []byte{
		0, 0, 0, 0, 255, 255, 255, 255,
		0, 0, 0, 0, 255, 0, 255, 0,
	}
	if !bytes.Equal(got.samples, want) {
		t.Errorf("samples = %v, want %v", got.samples, want)
	}
}

func TestRedactPageRegions_BlanksAnIndexedInlineImageThroughAResource(t *testing.T) {
	// Index 0 is red and 1 green, through a /ColorSpace resource.
	doc, page := inlineFixture(t, placeInline("/W 4 /H 1 /CS /CS0 /BPC 8", []byte{0, 1, 1, 0}),
		"/CS0 [/Indexed /DeviceRGB 1 <FF000000FF00>]", "")
	alloc, _ := redactInline(t, doc, page)

	got := redactedInline(t, alloc, page)
	if got.dict["CS"] != ast.PdfName("DeviceRGB") {
		t.Errorf("/CS = %v, want the indexed base, DeviceRGB", got.dict["CS"])
	}
	want := []byte{0, 0, 0, 0, 0, 0, 0, 255, 0, 255, 0, 0}
	if !bytes.Equal(got.samples, want) {
		t.Errorf("samples = %v, want %v", got.samples, want)
	}
}

func TestRedactPageRegions_BlanksAnInlineIndexedArray(t *testing.T) {
	// Two bits a sample: indices 0 1 2 3, white, red, green, blue. Index 3 is
	// past hival 2 and reads as the base's zero, as the decode reads it.
	doc, page := inlineFixture(t, placeInline("/W 4 /H 1 /CS [/I /RGB 2 <FFFFFFFF000000FF00>] /BPC 2", []byte{0x1B}), "", "")
	alloc, _ := redactInline(t, doc, page)

	got := redactedInline(t, alloc, page)
	if got.dict["CS"] != ast.PdfName("DeviceRGB") {
		t.Errorf("/CS = %v, want DeviceRGB", got.dict["CS"])
	}
	want := []byte{0, 0, 0, 0, 0, 0, 0, 255, 0, 0, 0, 0}
	if !bytes.Equal(got.samples, want) {
		t.Errorf("samples = %v, want %v", got.samples, want)
	}
}

func TestRedactPageRegions_InlineImageRefusals(t *testing.T) {
	tests := []struct {
		name, body, colorSpaces, form string
		want                          error
	}{
		{
			name: "drawn by a form XObject",
			body: "/Fm1 Do",
			form: placeInline("/W 1 /H 1 /CS /G /BPC 8", []byte{7}),
			want: ErrRegionInFormXObject,
		},
		{
			name: "a key Table 93 does not list",
			body: placeInline("/W 1 /H 1 /CS /G /BPC 8 /Foo 1", []byte{7}),
			want: ErrInlineImageInRegion,
		},
		{
			name: "a key written both ways",
			body: placeInline("/W 1 /Width 1 /H 1 /CS /G /BPC 8", []byte{7}),
			want: ErrInlineImageInRegion,
		},
		{
			name:        "a separation no inline image can name once rewritten",
			body:        placeInline("/W 1 /H 1 /CS /CS0 /BPC 8", []byte{7}),
			colorSpaces: "/CS0 [/Separation /Spot /DeviceGray {1 exch sub}]",
			want:        ErrInlineImageInRegion,
		},
		{
			name: "a filter litho cannot decode",
			body: placeInline("/W 1 /H 1 /CS /G /BPC 8 /F /Nope", []byte{7}),
			want: ErrImageNotDecodable,
		},
		{
			name: "a colour space the resources lack",
			body: placeInline("/W 1 /H 1 /CS /Missing /BPC 8", []byte{7}),
			want: ErrImageNotDecodable,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			doc, page := inlineFixture(t, tc.body, tc.colorSpaces, tc.form)
			alloc := writer.NewObjAllocator(20)
			_, err := RedactPageRegions(context.Background(), alloc, doc, page, [][4]float64{{0, 0, 612, 792}})
			if !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
			if n := len(alloc.Pending()); n != 0 {
				t.Errorf("registered %d objects while refusing", n)
			}
		})
	}
}

func TestInlineImageText_HexWrapsDataHoldingEI(t *testing.T) {
	ctx := context.Background()
	// Search a fixed sequence for samples whose compressed form holds "EI".
	rng := rand.New(rand.NewPCG(1, 2))
	var samples []byte
	for range 10000 {
		candidate := make([]byte, 64)
		for i := range candidate {
			candidate[i] = byte(rng.IntN(256))
		}
		compressed, err := flateCompress(candidate)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(compressed, []byte("EI")) {
			samples = candidate
			break
		}
	}
	if samples == nil {
		t.Fatal("no candidate compressed to data holding EI")
	}

	text, err := inlinePictureText(ctx, ast.PdfDict{}, ast.PdfDict{}, 64, 1, ast.PdfName("G"), samples)
	if err != nil {
		t.Fatal(err)
	}
	ops, err := content.ParseStreamData(ctx, append(text, " Q"...), content.SplitGluedOperators())
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 2 || ops[0].Operator != "BI" || ops[1].Operator != "Q" {
		t.Fatalf("the text reads as %d operations, want BI then Q: %q", len(ops), text)
	}
	if f, _ := ops[0].Operands[0].(ast.PdfDict)["F"].(ast.PdfArray); len(f) != 2 || f[0] != ast.PdfName("AHx") {
		t.Errorf("/F = %v, want [/AHx /Fl]", ops[0].Operands[0])
	}
}
