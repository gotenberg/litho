package imagefile

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"testing"

	"github.com/gotenberg/litho/internal/imagetest"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// readAll reads every picture of data with o.
func readAll(t *testing.T, data []byte, o Options) ([]*Picture, Info, error) {
	t.Helper()
	ctx, _, end := limits.WithBudget(context.Background(), limits.Limits{}.WithDefaults())
	defer end()
	var pics []*Picture
	info, err := Read(ctx, data, Sniff(data), o, func(p *Picture) error {
		pics = append(pics, p)
		return nil
	})
	return pics, info, err
}

// readOne reads the one picture of data.
func readOne(t *testing.T, data []byte, o Options) *Picture {
	t.Helper()
	pics, _, err := readAll(t, data, o)
	if err != nil {
		t.Fatal(err)
	}
	if len(pics) != 1 {
		t.Fatalf("%d pictures", len(pics))
	}
	return pics[0]
}

func pngOf(t *testing.T, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func ramp(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetNRGBA(x, y, color.NRGBA{uint8(x * 9), uint8(y * 7), uint8(x ^ y), 255})
		}
	}
	return img
}

// TestRead_KeepsTheSmallestLosslessLayout checks the layout each picture
// is written in: indexed at the bits its palette needs, gray at the bits
// its levels need, 16 bits kept unless the PDF version has none.
func TestRead_KeepsTheSmallestLosslessLayout(t *testing.T) {
	t.Parallel()
	four := image.NewPaletted(image.Rect(0, 0, 9, 5), color.Palette{color.Black, color.White, color.NRGBA{255, 0, 0, 255}})
	for i := range four.Pix {
		four.Pix[i] = uint8(i % 3)
	}
	bilevel := image.NewGray(image.Rect(0, 0, 9, 5))
	for i := range bilevel.Pix {
		bilevel.Pix[i] = uint8(255 * (i % 2))
	}
	levels := image.NewGray(image.Rect(0, 0, 9, 5))
	for i := range levels.Pix {
		levels.Pix[i] = uint8(0x55 * (i % 4))
	}
	deep := image.NewGray16(image.Rect(0, 0, 9, 5))
	for i := range 45 {
		deep.SetGray16(i%9, i/9, color.Gray16{Y: uint16(i * 1001)})
	}
	widened := image.NewGray16(image.Rect(0, 0, 9, 5))
	for i := range 45 {
		widened.SetGray16(i%9, i/9, color.Gray16{Y: uint16(i) * 0x0101})
	}
	for _, tc := range []struct {
		name       string
		data       []byte
		pdf14      bool
		bits, comp int
		palette    bool
	}{
		{"palette of 3", pngOf(t, four), false, 2, 1, true},
		{"bilevel", pngOf(t, bilevel), false, 1, 1, false},
		{"four levels", pngOf(t, levels), false, 2, 1, false},
		{"16 bit", pngOf(t, deep), false, 16, 1, false},
		{"16 bit at PDF 1.4", pngOf(t, deep), true, 8, 1, false},
		{"8-bit levels widened", pngOf(t, widened), false, 8, 1, false},
		{"rgb", pngOf(t, ramp(9, 5)), false, 8, 3, false},
	} {
		p := readOne(t, tc.data, Options{PDF14: tc.pdf14})
		l := p.Source.Layout
		if l == nil || l.BitsPerComponent != tc.bits || l.Components != tc.comp || (l.Palette != nil) != tc.palette {
			t.Errorf("%s: layout %+v", tc.name, l)
		}
	}
}

// TestRead_TellsResolutionOrientationAndText checks what each format's
// file states reaches the picture.
func TestRead_TellsResolutionOrientationAndText(t *testing.T) {
	t.Parallel()
	exif := imagetest.EXIF(imagetest.EXIFOptions{Orientation: 6, DPI: 200, Title: "A title"})
	png := imagetest.WithPNGChunks(pngOf(t, ramp(8, 6)),
		imagetest.PNGChunk{Type: "pHYs", Data: imagetest.PHYs(300)},
		imagetest.PNGChunk{Type: "eXIf", Data: exif},
		imagetest.PNGChunk{Type: "tEXt", Data: []byte("Title\x00Caf\xe9")},
		imagetest.PNGChunk{Type: "iTXt", Data: []byte("Description\x00\x00\x00en\x00\x00A ramp of colours")})
	pics, info, err := readAll(t, png, Options{})
	if err != nil {
		t.Fatal(err)
	}
	p := pics[0]
	if p.XDPI < 299.9 || p.XDPI > 300.1 || p.Orientation != 6 || p.Description != "A ramp of colours" || info.Title != "Café" {
		t.Errorf("png: %+v, title %q", p, info.Title)
	}
	jfif := imagetest.JPEG(ramp(8, 6), imagetest.JPEGOptions{Components: 3, JFIFDPI: 150, EXIF: exif})
	pics, info, err = readAll(t, jfif, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if p := pics[0]; p.XDPI != 150 || p.Orientation != 6 || info.Title != "A title" {
		t.Errorf("jpeg with JFIF: %+v, title %q", p, info.Title)
	}
	if p := readOne(t, imagetest.JPEG(ramp(8, 6), imagetest.JPEGOptions{Components: 1, EXIF: exif}), Options{}); p.XDPI != 200 {
		t.Errorf("jpeg with EXIF alone: %+v", p)
	}
	nonsense := imagetest.WithPNGChunks(pngOf(t, ramp(8, 6)), imagetest.PNGChunk{Type: "pHYs", Data: []byte{0, 0, 0, 1, 0, 0, 0, 1, 1}})
	if p := readOne(t, nonsense, Options{}); p.XDPI != 0 {
		t.Errorf("a pHYs of 1 pixel a metre reads as %v pixels an inch", p.XDPI)
	}
	if p := readOne(t, imagetest.BMP(ramp(8, 6), 24, 72), Options{}); p.XDPI < 71.9 || p.XDPI > 72.1 {
		t.Errorf("bmp: %+v", p)
	}
	tiff := imagetest.TIFF([]imagetest.TIFFPage{{Image: ramp(8, 6), Compression: imagetest.TIFFNone, DPI: 600, Orientation: 3, Description: "Scan"}}, nil)
	pics, info, err = readAll(t, tiff, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if p := pics[0]; p.XDPI != 600 || p.Orientation != 3 || info.Title != "Scan" {
		t.Errorf("tiff: %+v, title %q", p, info.Title)
	}
}

// TestRead_ComposesTheFirstFrame checks that an animated GIF and WebP give
// their first frame alone, at its place on the canvas, the rest
// transparent.
func TestRead_ComposesTheFirstFrame(t *testing.T) {
	t.Parallel()
	pal := color.Palette{color.NRGBA{255, 0, 0, 255}, color.NRGBA{0, 0, 255, 255}}
	frame := image.NewPaletted(image.Rect(2, 1, 6, 4), pal)
	second := image.NewPaletted(image.Rect(0, 0, 8, 5), pal)
	for i := range second.Pix {
		second.Pix[i] = 1
	}
	var b bytes.Buffer
	if err := gif.EncodeAll(&b, &gif.GIF{Image: []*image.Paletted{frame, second}, Delay: []int{10, 10}, Config: image.Config{Width: 8, Height: 5, ColorModel: pal}}); err != nil {
		t.Fatal(err)
	}
	p := readOne(t, b.Bytes(), Options{})
	if p.Width != 8 || p.Height != 5 || p.Source.Layout.Palette == nil || p.Source.Layout.AlphaBits == 0 {
		t.Errorf("gif: %+v, layout %+v", p, p.Source.Layout)
	}
	red, blue := ramp(6, 4), ramp(6, 4)
	for i := 0; i < len(red.Pix); i += 4 {
		copy(red.Pix[i:], []byte{255, 0, 0, 255})
		copy(blue.Pix[i:], []byte{0, 0, 255, 255})
	}
	webp := imagetest.RIFFWebP(
		imagetest.Chunk{FourCC: "VP8X", Body: imagetest.VP8X(0x02|0x10, 10, 8)},
		imagetest.Chunk{FourCC: "ANIM", Body: make([]byte, 6)},
		imagetest.Chunk{FourCC: "ANMF", Body: imagetest.ANMF(2, 4, 6, 4, 100, imagetest.Chunk{FourCC: "VP8L", Body: imagetest.VP8L(red)})},
		imagetest.Chunk{FourCC: "ANMF", Body: imagetest.ANMF(0, 0, 6, 4, 100, imagetest.Chunk{FourCC: "VP8L", Body: imagetest.VP8L(blue)})})
	p = readOne(t, webp, Options{})
	if p.Width != 10 || p.Height != 8 || p.Source.Layout.AlphaBits != 8 {
		t.Errorf("webp: %+v, layout %+v", p, p.Source.Layout)
	}
}

// TestRead_ReadsEveryTIFFPage checks the pages of a TIFF file: each in
// order, a reduced-resolution image left out, a chain that loops ended.
func TestRead_ReadsEveryTIFFPage(t *testing.T) {
	t.Parallel()
	pages := []imagetest.TIFFPage{
		{Image: ramp(8, 6), Compression: imagetest.TIFFLZW},
		{Image: ramp(4, 3), Compression: imagetest.TIFFNone, Reduced: true},
		{Image: ramp(5, 7), Compression: imagetest.TIFFPackBits},
		{Image: ramp(9, 2), Compression: imagetest.TIFFDeflate},
	}
	pics, _, err := readAll(t, imagetest.TIFF(pages, nil), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(pics) != 3 || pics[0].Width != 8 || pics[1].Width != 5 || pics[2].Width != 9 {
		t.Fatalf("%d pages", len(pics))
	}
	loop := imagetest.TIFF(pages[:2], func(ifds []int) int { return ifds[0] })
	pics, _, err = readAll(t, loop, Options{})
	if err != nil || len(pics) != 1 {
		t.Errorf("a looping chain: %d pages, %v", len(pics), err)
	}
	if _, _, err := readAll(t, imagetest.TIFF(pages, nil), Options{MaxPictures: 2}); !errors.Is(err, limits.ErrTooManyPages) {
		t.Errorf("past the page cap: %v", err)
	}
}

// TestRead_TakesTheLargestIcon checks that an icon file gives its largest
// image, of the most bits among the largest, whatever its kind.
func TestRead_TakesTheLargestIcon(t *testing.T) {
	t.Parallel()
	small := pngOf(t, ramp(16, 16))
	large := ramp(48, 48)
	large.SetNRGBA(0, 0, color.NRGBA{})
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"png entry", imagetest.ICO(imagetest.ICOImage{Width: 16, Height: 16, Bits: 32, Data: small}, imagetest.ICOImage{Width: 48, Height: 48, Bits: 32, Data: pngOf(t, large)})},
		{"32-bit DIB", imagetest.ICO(imagetest.ICOImage{Width: 16, Height: 16, Bits: 32, Data: small}, imagetest.ICOImage{Width: 48, Height: 48, Bits: 32, Data: imagetest.DIB(large, 32)})},
		{"24-bit DIB and its mask", imagetest.ICO(imagetest.ICOImage{Width: 48, Height: 48, Bits: 24, Data: imagetest.DIB(large, 24)}, imagetest.ICOImage{Width: 16, Height: 16, Bits: 32, Data: small})},
	} {
		p := readOne(t, tc.data, Options{})
		if p.Width != 48 || p.Height != 48 || p.Source.Layout.AlphaBits == 0 {
			t.Errorf("%s: %dx%d, layout %+v", tc.name, p.Width, p.Height, p.Source.Layout)
		}
	}
	outside := imagetest.ICO(imagetest.ICOImage{Width: 16, Height: 16, Bits: 32, Data: small})
	binary.LittleEndian.PutUint32(outside[18:], 1<<30)
	if _, _, err := readAll(t, outside, Options{}); !errors.Is(err, ErrMalformed) {
		t.Errorf("an entry outside the file: %v", err)
	}
}

// TestRead_WritesJPEG2000AsItIsFromPDF15 checks that a JP2 file is kept
// from PDF 1.5 and decoded under PDF 1.4, and that a bare codestream is
// decoded.
func TestRead_WritesJPEG2000AsItIsFromPDF15(t *testing.T) {
	t.Parallel()
	jp2 := imagetest.JPX(ramp(20, 10), imagetest.JPXOptions{})
	if p := readOne(t, jp2, Options{}); !p.Source.Layout.JPX || !bytes.Equal(p.Source.JPEG, jp2) {
		t.Errorf("PDF 1.5: layout %+v", p.Source.Layout)
	}
	if p := readOne(t, jp2, Options{PDF14: true}); p.Source.Layout.JPX || p.Source.IsJPEG() {
		t.Errorf("PDF 1.4: layout %+v", p.Source.Layout)
	}
	j2k := imagetest.JPX(ramp(20, 10), imagetest.JPXOptions{Codestream: true})
	if p := readOne(t, j2k, Options{}); p.Source.Layout.JPX {
		t.Errorf("a bare codestream is kept: %+v", p.Source.Layout)
	}
}

// TestRead_KeepsTheICCProfile checks that a profile of the picture's space
// is kept, and that a version 4 one is left out under PDF 1.4.
func TestRead_KeepsTheICCProfile(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		major byte
		pdf14 bool
		kept  bool
	}{{"v2", 2, false, true}, {"v4", 4, false, true}, {"v4 at PDF 1.4", 4, true, false}, {"v2 at PDF 1.4", 2, true, true}} {
		icc := imagetest.ICCProfile("RGB ", tc.major)
		data := imagetest.WithPNGChunks(pngOf(t, ramp(4, 4)), imagetest.PNGChunk{Type: "iCCP", Data: imagetest.ICCP(icc)})
		var warned []string
		p := readOne(t, data, Options{PDF14: tc.pdf14, Warn: func(msg string, _ bool) { warned = append(warned, msg) }})
		if kept := bytes.Equal(p.Source.Layout.ICC, icc); kept != tc.kept || kept == (len(warned) > 0) {
			t.Errorf("%s: kept %t, warnings %q", tc.name, kept, warned)
		}
	}
	cmyk := imagetest.ICCProfile("CMYK", 2)
	jpeg := imagetest.JPEG(ramp(8, 8), imagetest.JPEGOptions{Components: 4, ICC: cmyk})
	p := readOne(t, jpeg, Options{})
	if !bytes.Equal(p.Source.Layout.ICC, cmyk) || !p.Source.Layout.Invert || !bytes.Equal(p.Source.JPEG, jpeg) {
		t.Errorf("CMYK JPEG: layout %+v", p.Source.Layout)
	}
}

// TestRead_RefusesPixelBombs checks that a header promising more pixels
// than the cap is refused before a byte of it is decoded, in each format.
func TestRead_RefusesPixelBombs(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"png", imagetest.PNGBomb()},
		{"jpeg", imagetest.JPEGBomb()},
		{"gif", imagetest.GIFBomb()},
		{"webp", imagetest.WebPBomb()},
		{"bmp", imagetest.BMPBomb()},
		{"tiff", imagetest.TIFFBomb()},
		{"ico", imagetest.ICOBomb()},
		{"jpeg 2000", imagetest.JPXBomb()},
	} {
		_, _, err := readAll(t, tc.data, Options{})
		if !errors.Is(err, limits.ErrPictureTooLarge) && !errors.Is(err, limits.ErrInputTooLarge) {
			t.Errorf("%s: %v, want a pixel cap refusal", tc.name, err)
		}
	}
}

// TestRead_RefusesTruncatedFiles checks that every format's file cut in
// half fails as malformed, never as anything else.
func TestRead_RefusesTruncatedFiles(t *testing.T) {
	t.Parallel()
	for name, data := range seeds() {
		half := data[:len(data)/2]
		if Sniff(half) == Unknown {
			continue
		}
		_, _, err := readAll(t, half, Options{})
		if err != nil && !errors.Is(err, ErrMalformed) && !errors.Is(err, ErrUnsupported) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// seeds returns one valid file of each format.
func seeds() map[string][]byte {
	pic := ramp(20, 12)
	var p, g bytes.Buffer
	_ = png.Encode(&p, pic) // a bytes.Buffer takes every write
	pal := image.NewPaletted(pic.Bounds(), color.Palette{color.Black, color.White})
	_ = gif.Encode(&g, pal, nil) // as above
	return map[string][]byte{
		"png":  p.Bytes(),
		"jpeg": imagetest.JPEG(pic, imagetest.JPEGOptions{Components: 3, Progressive: true}),
		"gif":  g.Bytes(),
		"webp": imagetest.RIFFWebP(imagetest.Chunk{FourCC: "VP8L", Body: imagetest.VP8L(pic)}),
		"bmp":  imagetest.BMP(pic, 24, 96),
		"tiff": imagetest.TIFF([]imagetest.TIFFPage{{Image: pic, Compression: imagetest.TIFFLZW}}, nil),
		"jp2":  imagetest.JPX(pic, imagetest.JPXOptions{}),
		"ico":  imagetest.ICO(imagetest.ICOImage{Width: 20, Height: 12, Bits: 32, Data: imagetest.DIB(pic, 32)}),
	}
}

// FuzzRead reads attacker-controlled bytes of every format: no panic, no
// error but the ones Read documents.
func FuzzRead(f *testing.F) {
	for _, data := range seeds() {
		f.Add(data)
		f.Add(data[:len(data)/2])
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		ctx, _, end := limits.WithBudget(context.Background(), limits.Limits{MaxImagePixels: 1 << 16, MaxConvertMemoryBytes: 64 << 20}.WithDefaults())
		defer end()
		_, err := Read(ctx, data, Sniff(data), Options{MaxPictures: 4}, func(*Picture) error { return nil })
		if err != nil && !errors.Is(err, ErrMalformed) && !errors.Is(err, ErrUnsupported) && !errors.Is(err, limits.ErrPictureTooLarge) &&
			!errors.Is(err, limits.ErrInputTooLarge) && !errors.Is(err, limits.ErrTooManyPages) && !errors.Is(err, typeset.ErrInvalidImage) {
			t.Fatalf("undocumented error: %v", err)
		}
	})
}
