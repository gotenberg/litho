package pdf

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"strings"
	"testing"

	"golang.org/x/image/webp"

	"github.com/gotenberg/litho/internal/imagetest"
	"github.com/gotenberg/litho/pkg/internal/imagefile"
	"github.com/gotenberg/litho/pkg/models"
)

// TestFromImage_DrawsEveryFormatsPixels converts a picture of each format
// imagetest writes and compares its page, rendered at the picture's own
// resolution, with the picture as its own decoder reads it: equal for the
// lossless formats, a JPEG within its decoding.
func TestFromImage_DrawsEveryFormatsPixels(t *testing.T) {
	t.Parallel()
	pic := testPicture(35, 21)
	translucent := testPicture(19, 13)
	for i := 3; i < len(translucent.Pix); i += 4 {
		translucent.Pix[i] = uint8(i * 3 % 256)
	}
	gray := image.NewGray(image.Rect(0, 0, 33, 17))
	for i := range gray.Pix {
		gray.Pix[i] = uint8(i * 13 % 256)
	}
	icon := testPicture(32, 32)
	for y := range 32 {
		for x := range 32 {
			if (x-16)*(x-16)+(y-16)*(y-16) > 200 {
				icon.SetNRGBA(x, y, color.NRGBA{})
			}
		}
	}
	lossy := imagetest.RIFFWebP(imagetest.Chunk{FourCC: "VP8 ", Body: imagetest.VP8(pic)})
	lossyDecoded, err := webp.Decode(bytes.NewReader(lossy))
	if err != nil {
		t.Fatal(err)
	}
	progressive := imagetest.JPEG(gray, imagetest.JPEGOptions{Components: 1, Progressive: true})
	progressiveDecoded, err := jpeg.Decode(bytes.NewReader(progressive))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		data    []byte
		want    image.Image
		dpi     float64
		tol     int
		version string
	}{
		{"webp lossless", imagetest.RIFFWebP(imagetest.Chunk{FourCC: "VP8L", Body: imagetest.VP8L(pic)}), pic, 96, 1, ""},
		{"webp alpha", imagetest.RIFFWebP(imagetest.Chunk{FourCC: "VP8L", Body: imagetest.VP8L(translucent)}), translucent, 96, 1, ""},
		{"webp lossy", lossy, lossyDecoded, 96, 1, ""},
		{"webp animated", imagetest.AnimatedWebP(pic, gray), pic, 96, 1, ""},
		{"bmp 32 bits", imagetest.BMP(translucent, 32, 96), translucent, 96, 1, ""},
		{"ico", imagetest.ICO(imagetest.ICOImage{Width: 16, Height: 16, Bits: 32, Data: imagetest.DIB(testPicture(16, 16), 32)}, imagetest.ICOImage{Width: 32, Height: 32, Bits: 32, Data: imagetest.DIB(icon, 32)}), icon, 96, 1, ""},
		{"tiff lzw", imagetest.TIFF([]imagetest.TIFFPage{{Image: pic, Compression: imagetest.TIFFLZW, DPI: 96}}, nil), pic, 96, 1, ""},
		{"tiff packbits alpha", imagetest.TIFF([]imagetest.TIFFPage{{Image: translucent, Alpha: true, Compression: imagetest.TIFFPackBits, DPI: 96}}, nil), translucent, 96, 1, ""},
		{"jpeg 2000", imagetest.JPX(pic, imagetest.JPXOptions{}), pic, 96, 1, ""},
		{"jpeg 2000 gray", imagetest.JPX(gray, imagetest.JPXOptions{}), gray, 96, 1, ""},
		{"jpeg 2000 at PDF 1.4", imagetest.JPX(pic, imagetest.JPXOptions{}), pic, 96, 1, "1.4"},
		{"jpeg 2000 codestream", imagetest.JPX(pic, imagetest.JPXOptions{Codestream: true}), pic, 96, 1, ""},
		{"jpeg progressive", progressive, progressiveDecoded, 96, 3, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var opts []ImageOption
			if tc.version != "" {
				opts = append(opts, WithConvertVersion(tc.version))
			}
			r, _, _ := convertImageFile(t, tc.data, opts...)
			samePixels(t, renderedPage(t, r, 1, tc.dpi), overWhite(tc.want), tc.tol)
		})
	}
}

// TestFromImage_KeepsTheFileOrTheDepthTheVersionHolds checks what each
// version writes: a JPEG 2000 file as it is from PDF 1.5, decoded under
// PDF 1.4; a 16-bit picture at 16 bits from PDF 1.5, at 8 under 1.4; an
// Adobe CMYK JPEG as it is, its samples read inverted back.
func TestFromImage_KeepsTheFileOrTheDepthTheVersionHolds(t *testing.T) {
	t.Parallel()
	jp2 := imagetest.JPX(testPicture(12, 9), imagetest.JPXOptions{})
	deep := image.NewNRGBA64(image.Rect(0, 0, 9, 7))
	for i := range 63 {
		deep.SetNRGBA64(i%9, i/9, color.NRGBA64{uint16(i * 1031), uint16(i * 977), 7, 0xFFFF})
	}
	cmyk := imagetest.JPEG(testPicture(16, 8), imagetest.JPEGOptions{Components: 4})
	for _, tc := range []struct {
		name, version string
		data          []byte
		holds, lacks  []string
	}{
		{"jp2 at 1.7", "", jp2, []string{"/JPXDecode"}, nil},
		{"jp2 at 1.4", "1.4", jp2, nil, []string{"/JPXDecode"}},
		{"16 bit at 1.7", "", encodePNG(t, deep), []string{"/BitsPerComponent 16"}, nil},
		{"16 bit at 1.4", "1.4", encodePNG(t, deep), nil, []string{"/BitsPerComponent 16"}},
		{"cmyk jpeg", "", cmyk, []string{"/DeviceCMYK", "/Decode [1 0 1 0 1 0 1 0]"}, nil},
	} {
		var opts []ImageOption
		if tc.version != "" {
			opts = append(opts, WithConvertVersion(tc.version))
		}
		_, data, _ := convertImageFile(t, tc.data, opts...)
		for _, s := range tc.holds {
			if !bytes.Contains(data, []byte(s)) {
				t.Errorf("%s: the PDF lacks %s", tc.name, s)
			}
		}
		for _, s := range tc.lacks {
			if bytes.Contains(data, []byte(s)) {
				t.Errorf("%s: the PDF holds %s", tc.name, s)
			}
		}
		if strings.HasSuffix(tc.name, "jpeg") && !bytes.Contains(data, tc.data) {
			t.Errorf("%s: the PDF does not hold the JPEG's bytes", tc.name)
		}
	}
}

// TestFromImage_TurnsByTheEXIFOrientation checks each of the eight EXIF
// orientations: the page draws the picture turned as the orientation
// says, its samples untouched in the file.
func TestFromImage_TurnsByTheEXIFOrientation(t *testing.T) {
	t.Parallel()
	pic := image.NewGray(image.Rect(0, 0, 24, 16))
	for y := range 16 {
		for x := range 24 {
			// Flat 8 by 8 blocks, which a JPEG holds within a level or two.
			pic.SetGray(x, y, color.Gray{uint8(40*(x/8) + 100*(y/8))})
		}
	}
	for o := 1; o <= 8; o++ {
		data := imagetest.JPEG(pic, imagetest.JPEGOptions{Components: 1, EXIF: imagetest.EXIF(imagetest.EXIFOptions{Orientation: o})})
		decoded, err := jpeg.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		want, err := imagefile.OrientImage(context.Background(), decoded, o)
		if err != nil {
			t.Fatal(err)
		}
		r, out, _ := convertImageFile(t, data)
		if !bytes.Contains(out, data) {
			t.Errorf("orientation %d: the JPEG is not written as it is", o)
		}
		got := renderedPage(t, r, 1, 96)
		t.Run(string(rune('0'+o)), func(t *testing.T) { samePixels(t, got, want, 3) })
	}
}

// pageSize returns the size of page n of r in points, from its rendering
// at 144 dpi.
func pageSize(t *testing.T, r *Reader, n int) (float64, float64) {
	t.Helper()
	b := renderedPage(t, r, n, 144).Bounds()
	return float64(b.Dx()) / 2, float64(b.Dy()) / 2
}

// near reports two sizes within the half point a 144 dpi rendering
// rounds to.
func near(a, b float64) bool { return math.Abs(a-b) <= 0.5 }

// TestFromImage_SizesThePageByTheResolution checks the page rule: the
// picture at the resolution its file states, 96 pixels an inch without
// one, the paper with the picture fitted to it, the page turned.
func TestFromImage_SizesThePageByTheResolution(t *testing.T) {
	t.Parallel()
	pic := testPicture(300, 150)
	plain := encodePNG(t, pic)
	at300 := imagetest.WithPNGChunks(plain, imagetest.PNGChunk{Type: "pHYs", Data: imagetest.PHYs(300)})
	turned := imagetest.JPEG(pic, imagetest.JPEGOptions{Components: 3, JFIFDPI: 150, EXIF: imagetest.EXIF(imagetest.EXIFOptions{Orientation: 6})})
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 400 200"><rect width="400" height="200" fill="teal"/></svg>`)
	svgSized := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="2in" height="1in" viewBox="0 0 400 200"><rect width="400" height="200"/></svg>`)
	for _, tc := range []struct {
		name string
		data []byte
		opts []ImageOption
		w, h float64
	}{
		{"no resolution", plain, nil, 225, 112.5},
		{"300 pixels an inch", at300, nil, 72, 36},
		{"turned, 150 pixels an inch", turned, nil, 72, 144},
		{"default paper changes nothing", plain, []ImageOption{WithDefaultPaper(models.PaperA4)}, 225, 112.5},
		{"paper", plain, []ImageOption{WithPaper(models.PaperA4)}, models.PaperA4.Width, models.PaperA4.Height},
		{"portrait", plain, []ImageOption{WithOrientation(models.OrientationPortrait)}, 112.5, 225},
		{"svg view box", svg, nil, 300, 150},
		{"svg size", svgSized, nil, 144, 72},
	} {
		r, _, _ := convertImageFile(t, tc.data, tc.opts...)
		if w, h := pageSize(t, r, 1); !near(w, tc.w) || !near(h, tc.h) {
			t.Errorf("%s: page %v by %v, want %v by %v", tc.name, w, h, tc.w, tc.h)
		}
	}
}

// TestFromImage_FitsThePictureToThePaper checks that the picture is scaled
// down to fit the paper, never up, and centred.
func TestFromImage_FitsThePictureToThePaper(t *testing.T) {
	t.Parallel()
	red := image.NewNRGBA(image.Rect(0, 0, 40, 20))
	for i := range red.Pix {
		red.Pix[i] = []byte{255, 0, 0, 255}[i%4]
	}
	ctx := context.Background()
	for _, tc := range []struct {
		name  string
		paper models.Paper
		// box is the picture's rectangle on the page, in points.
		box image.Rectangle
	}{
		{"never up", models.Paper{Width: 100, Height: 100}, image.Rect(35, 42, 65, 57)},
		{"down to fit", models.Paper{Width: 15, Height: 30}, image.Rect(0, 11, 15, 18)},
	} {
		r, _, _ := convertImageFile(t, encodePNG(t, red), WithPaper(tc.paper))
		img, err := r.RenderPage(ctx, 1, WithRenderDPI(72))
		if err != nil {
			t.Fatal(err)
		}
		in, out := tc.box.Inset(1), tc.box.Inset(-1)
		for y := range img.Bounds().Dy() {
			for x := range img.Bounds().Dx() {
				p := image.Pt(x, y)
				cr, _, _, _ := img.At(x, y).RGBA()
				g := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA).G
				switch {
				case p.In(in) && (cr>>8 < 250 || g > 5):
					t.Fatalf("%s: %v inside the picture is %v", tc.name, p, img.At(x, y))
				case !p.In(out) && g < 250:
					t.Fatalf("%s: %v outside the picture is %v", tc.name, p, img.At(x, y))
				}
			}
		}
	}
}

// TestFromImage_KeepsAPageWithinThePDFLimits checks the page of a picture
// at its natural size: past the 14,400 points of ISO 32000-1, Annex C.2,
// an informative limit, it is written whole, with no warning; past 32,767
// points under PDF 1.4, whose real numbers stop there, it is cut and the
// picture scaled down, and under 3 points grown to 3, each with a warning.
func TestFromImage_KeepsAPageWithinThePDFLimits(t *testing.T) {
	t.Parallel()
	// 1,000 pixels at 3 pixels an inch, stated as 118 pixels a metre: 24,021
	// points by the 20 pixels' 480.
	wide := imagetest.WithPNGChunks(encodePNG(t, testPicture(1000, 20)), imagetest.PNGChunk{Type: "pHYs", Data: imagetest.PHYs(3)})
	r, _, report := convertImageFile(t, wide)
	if pg, _ := r.Page(t.Context(), 1); pg.Width < 24000 || pg.Height < 480 || len(report.Warnings) != 0 {
		t.Errorf("wide: page %v by %v, warnings %+v; want its natural size", pg.Width, pg.Height, report.Warnings)
	}
	// At 2 pixels an inch, 36,000 points: cut to 32,767 under PDF 1.4.
	wider := imagetest.WithPNGChunks(encodePNG(t, testPicture(1000, 20)), imagetest.PNGChunk{Type: "pHYs", Data: imagetest.PHYs(2)})
	r, _, report = convertImageFile(t, wider, WithConvertVersion("1.4"))
	if pg, _ := r.Page(t.Context(), 1); !near(pg.Width, 32767) || len(report.Warnings) != 1 {
		t.Errorf("wider at 1.4: page %v wide, warnings %+v; want 32,767 and a warning", pg.Width, report.Warnings)
	}
	tiny := encodePNG(t, testPicture(1, 1))
	r, _, report = convertImageFile(t, tiny)
	if w, h := pageSize(t, r, 1); !near(w, 3) || !near(h, 3) || len(report.Warnings) != 1 {
		t.Errorf("tiny: page %v by %v, warnings %+v", w, h, report.Warnings)
	}
}

// TestFromImage_PagesATIFFFile checks that each page of a TIFF file is a
// page, drawn as its samples, and that WithPageRanges selects among them.
func TestFromImage_PagesATIFFFile(t *testing.T) {
	t.Parallel()
	pages := []image.Image{testPicture(20, 10), testPicture(7, 13), testPicture(15, 15)}
	var tp []imagetest.TIFFPage
	for i, p := range pages {
		tp = append(tp, imagetest.TIFFPage{Image: p, Compression: []int{imagetest.TIFFNone, imagetest.TIFFLZW, imagetest.TIFFDeflate}[i], DPI: 96})
	}
	data := imagetest.TIFF(tp, nil)
	r, _, report := convertImageFile(t, data)
	if report.Pages != 3 {
		t.Fatalf("%d pages", report.Pages)
	}
	for i, p := range pages {
		samePixels(t, renderedPage(t, r, i+1, 96), p, 1)
	}
	r, _, report = convertImageFile(t, data, WithPageRanges("3,1"))
	if report.Pages != 2 {
		t.Fatalf("selected %d pages", report.Pages)
	}
	samePixels(t, renderedPage(t, r, 1, 96), pages[2], 1)
}

// TestFromImage_TagsAFigureWithTheFormatsDescription checks the structure:
// a Figure a page, described by a PNG Description chunk or an SVG title,
// the document titled by a PNG Title chunk or an EXIF ImageDescription,
// and that a picture without a description fails PDF/UA remediation
// unless WithFigureAltText describes it.
func TestFromImage_TagsAFigureWithTheFormatsDescription(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	described := imagetest.WithPNGChunks(encodePNG(t, testPicture(8, 8)),
		imagetest.PNGChunk{Type: "tEXt", Data: []byte("Title\x00Squares")},
		imagetest.PNGChunk{Type: "tEXt", Data: []byte("Description\x00A grid of squares")})
	_, data, _ := convertImageFile(t, described)
	if !bytes.Contains(data, []byte("/S /Figure")) || !bytes.Contains(data, []byte("A grid of squares")) || !bytes.Contains(data, []byte("Squares")) {
		t.Error("the PNG's description and title are not the figure's and the document's")
	}
	titled := imagetest.JPEG(testPicture(8, 8), imagetest.JPEGOptions{Components: 3, EXIF: imagetest.EXIF(imagetest.EXIFOptions{Title: "Harbour"})})
	r, _, _ := convertImageFile(t, titled)
	if meta, err := r.Metadata(ctx); err != nil || meta["Title"] != "Harbour" {
		t.Errorf("EXIF title: %v, %v", meta, err)
	}
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="40" height="20"><title>Two bars</title><rect width="20" height="20"/></svg>`)
	if _, data, _ := convertImageFile(t, svg); !bytes.Contains(data, []byte("Two bars")) {
		t.Error("the SVG title is not the figure's description")
	}
	plain := encodePNG(t, testPicture(8, 8))
	_, report, err := convertAndRemediate(ctx, FromImage(bytes.NewReader(plain), WithConvertFontDirs(convertFontDir(t))), "1.7", "", models.PDFUA1, "en", "Picture")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Residual) == 0 {
		t.Error("a figure without a description passes PDF/UA")
	}
	var out bytes.Buffer
	_, err = NewPipeline().Convert(FromImage(bytes.NewReader(plain), WithConvertFontDirs(convertFontDir(t)))).Apply(func(ctx context.Context, w *Writer) error {
		w.SetMetadata(models.Metadata{Title: "Picture"})
		rep, err := w.RemediatePDFUA(ctx, models.PDFUA1, WithLanguage("en"), WithFigureAltText("A test picture"))
		if err == nil && len(rep.Residual) > 0 {
			t.Errorf("described at remediation: residual %v", rep.Residual)
		}
		return err
	}).WriteTo(ctx, &out)
	if err != nil {
		t.Fatal(err)
	}
}

// TestFromImage_RunsInAPipeline converts an image, stamps it in a later
// stage, and merges three images of three formats into one file.
func TestFromImage_RunsInAPipeline(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fonts := WithConvertFontDirs(convertFontDir(t))
	white := image.NewGray(image.Rect(0, 0, 200, 100))
	for i := range white.Pix {
		white.Pix[i] = 0xFF
	}
	var out bytes.Buffer
	_, err := NewPipeline().Convert(FromImage(bytes.NewReader(encodePNG(t, white)), fonts)).Apply(func(_ context.Context, w *Writer) error {
		return w.StampText(TextStamp{Text: "STAMPED", X: 10, Y: 10, FontSize: 20})
	}).WriteTo(ctx, &out)
	if err != nil {
		t.Fatal(err)
	}
	r, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatal(err)
	}
	// The stamp draws dark glyphs over the white picture, near its
	// bottom-left corner.
	page := renderedPage(t, r, 1, 72)
	dark := 0
	for y := page.Bounds().Dy() - 30; y < page.Bounds().Dy(); y++ {
		for x := range 100 {
			if c := color.GrayModel.Convert(page.At(x, y)).(color.Gray); c.Y < 128 {
				dark++
			}
		}
	}
	if dark == 0 {
		t.Error("the stamp stage drew nothing on the converted page")
	}
	sources := [][]byte{
		encodePNG(t, testPicture(30, 20)),
		imagetest.JPEG(testPicture(20, 30), imagetest.JPEGOptions{Components: 3}),
		imagetest.TIFF([]imagetest.TIFFPage{{Image: testPicture(10, 10), Compression: imagetest.TIFFNone}, {Image: testPicture(12, 12), Compression: imagetest.TIFFNone}}, nil),
	}
	var pipelines []*Pipeline
	for _, s := range sources {
		pipelines = append(pipelines, NewPipeline().Convert(FromImage(bytes.NewReader(s), fonts)))
	}
	out.Reset()
	if _, err := NewPipeline().MergePipelines(pipelines).WriteTo(ctx, &out); err != nil {
		t.Fatal(err)
	}
	r, err = Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if n, err := r.PageCount(ctx); err != nil || n != 4 {
		t.Errorf("merged %d pages, %v", n, err)
	}
}

// TestFromImage_RefusesBombsAndBrokenFiles checks how each hostile file
// ends: a pixel bomb with ErrConvertInputTooLarge before it is decoded, a
// truncated file with ErrConvertMalformed, a TIFF chain that loops and an
// icon entry outside the file as their readable part.
func TestFromImage_RefusesBombsAndBrokenFiles(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		data []byte
		want error
	}{
		{"png bomb", imagetest.PNGBomb(), ErrConvertInputTooLarge},
		{"jpeg bomb", imagetest.JPEGBomb(), ErrConvertInputTooLarge},
		{"gif bomb", imagetest.GIFBomb(), ErrConvertInputTooLarge},
		{"webp bomb", imagetest.WebPBomb(), ErrConvertInputTooLarge},
		{"bmp bomb", imagetest.BMPBomb(), ErrConvertInputTooLarge},
		{"tiff bomb", imagetest.TIFFBomb(), ErrConvertInputTooLarge},
		{"ico bomb", imagetest.ICOBomb(), ErrConvertInputTooLarge},
		{"jpeg 2000 bomb", imagetest.JPXBomb(), ErrConvertInputTooLarge},
		{"truncated png", encodePNG(t, testPicture(40, 40))[:100], ErrConvertMalformed},
		{"looping tiff", imagetest.LoopingTIFF(), nil},
		{"icon outside", imagetest.ICOOutside(), ErrConvertMalformed},
	} {
		_, err := FromImage(bytes.NewReader(tc.data), WithConvertFontDirs(convertFontDir(t))).WriteTo(ctx, &bytes.Buffer{})
		if tc.want == nil && err != nil || tc.want != nil && !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, err, tc.want)
		}
	}
}

// imageSameBytes are the conversions TestFromImage_SameBytesOnEveryMachine
// pins, one a format, each of a file imagetest writes.
var imageSameBytes = []struct {
	name string
	data func() []byte
	want string
}{
	{"png", func() []byte { return encodePNGBytes(testPicture(31, 17)) }, "390c313bff301c6d99acdedb46268153f65c2fb7b1612d042cbe30870e766d55"},
	{"jpeg", func() []byte {
		return imagetest.JPEG(testPicture(31, 17), imagetest.JPEGOptions{Components: 3, EXIF: imagetest.EXIF(imagetest.EXIFOptions{Orientation: 6})})
	}, "344a6e4cad9faf30eba9d7170dddce1e91ea186f0ea69ada8fc5f50335e75c50"},
	{"gif", func() []byte { return imagetest.AnimatedGIF() }, "f3385cbb063b8ffd10e0a6f9394a36e5523237a9479c040746df36ca6920bb9e"},
	{"webp", func() []byte {
		return imagetest.RIFFWebP(imagetest.Chunk{FourCC: "VP8 ", Body: imagetest.VP8(testPicture(31, 17))})
	}, "0cca5fc32137ebae80817d84695a1f9f909d0a34e92d368116b71790646eaa1d"},
	{"bmp", func() []byte { return imagetest.BMP(testPicture(31, 17), 32, 300) }, "f6d4b56262d7d7171d2b1a6d45b7c3b46c26865a8bb44cc3ac800891d8435987"},
	{"tiff", func() []byte {
		return imagetest.TIFF([]imagetest.TIFFPage{{Image: testPicture(31, 17), Compression: imagetest.TIFFLZW}, {Image: testPicture(9, 9), Compression: imagetest.TIFFDeflate}}, nil)
	}, "95162b8d5a8222749a50ac82eaf0605d1a446c32a0fae1613ecb8020689e8259"},
	{"jpeg 2000", func() []byte { return imagetest.JPX(testPicture(31, 17), imagetest.JPXOptions{}) }, "3cf5e1409540b2a598458a901ec7b59cb3473cfa1f84b0a8ec767d29a211b0f3"},
	{"ico", func() []byte {
		return imagetest.ICO(imagetest.ICOImage{Width: 16, Height: 16, Bits: 32, Data: imagetest.DIB(testPicture(16, 16), 32)})
	}, "49844b2266a093af58ffcb88036d11c45446373375d5584729105930d1326cc7"},
	{"svg", func() []byte {
		return []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="120" height="60"><rect x="5" y="5" width="50" height="30" fill="#c33"/><text x="10" y="55" font-family="Liberation Sans" font-size="12">Pinned</text></svg>`)
	}, "afccf3dc0944c7eff343c39a2601a96de1d6ea3ec956c41026807f1dd7204700"},
}

func encodePNGBytes(img image.Image) []byte {
	var b bytes.Buffer
	_ = png.Encode(&b, img) // a bytes.Buffer takes every write
	return b.Bytes()
}

// TestFromImage_SameBytesOnEveryMachine pins the SHA-256 of one conversion
// of each format: every machine writes these bytes.
func TestFromImage_SameBytesOnEveryMachine(t *testing.T) {
	t.Parallel()
	for _, tc := range imageSameBytes {
		_, data, _ := convertImageFile(t, tc.data())
		_, again, _ := convertImageFile(t, tc.data())
		if !bytes.Equal(data, again) {
			t.Errorf("%s: two conversions write different bytes", tc.name)
		}
		sum := sha256.Sum256(data)
		if got := hex.EncodeToString(sum[:]); got != tc.want {
			t.Errorf("%s: SHA-256 %s, pinned %s", tc.name, got, tc.want)
		}
	}
}
