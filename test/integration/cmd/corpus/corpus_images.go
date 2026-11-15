package main

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"os"
	"path/filepath"

	"github.com/gotenberg/litho/internal/imagetest"
)

// imageCorpus is the corpus of image files `corpus build` generates from
// code, each format's variants and hostile files, which pdf.FromImage
// converts. Its files are never committed: the build writes the same bytes
// each time. No reference engine converts an image file to PDF as a
// viewer shows it, so the corpus runs the targets and robustness cells
// alone.
const imageCorpus = "litho/image"

// imageFile is one file of the image corpus: its name, the file's
// extension and its bytes, and an options.json when it is not "".
type imageFile struct {
	name, ext string
	data      func() []byte
	options   string
	// hostile marks a file the targets cell leaves out: it probes how a
	// conversion ends, which robustness.json says.
	hostile bool
}

// imagePhoto returns a w by h picture of smooth gradients, a lighter disc
// whose lift fades to nothing at its rim, different for each seed: what a
// photograph's samples look like to a codec, made by code. No two
// neighbouring pixels differ by much, so two renderers that resample the
// picture differently still draw it alike.
func imagePhoto(w, h, seed int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	radius := h / 3
	for y := range h {
		for x := range w {
			r := fold(x*255/max(w-1, 1) + seed*37)
			g := fold(y*255/max(h-1, 1) + seed*71)
			b := fold((x+y)*255/max(w+h-2, 1) + seed*13)
			if dx, dy := x-w/3, y-h/2; dx*dx+dy*dy < radius*radius {
				lift := 60 * (radius*radius - dx*dx - dy*dy) / (radius * radius)
				r, g, b = min(255, r+lift), min(255, g+lift), min(255, b+lift)
			}
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(r), G: uint8(g), B: uint8(b), A: 255}) //nolint:gosec // G115: levels of 0 to 255
		}
	}
	return img
}

// fold returns v, zero or more, folded into a level that climbs to 255
// and back down, so that a gradient past 255 turns instead of wrapping.
func fold(v int) int {
	v %= 510
	if v > 255 {
		return 510 - v
	}
	return v
}

// translucent returns img with an alpha that falls from opaque at the top
// to clear at the bottom.
func translucent(img *image.NRGBA) *image.NRGBA {
	h := img.Bounds().Dy()
	for y := range h {
		for x := range img.Bounds().Dx() {
			img.Pix[img.PixOffset(x, y)+3] = uint8(255 - y*255/max(h-1, 1)) //nolint:gosec // G115: a level of 0 to 255
		}
	}
	return img
}

// withTIFFResolution returns the TIFF file data, written by
// imagetest.TIFF, its first page's XResolution and YResolution set to
// num/den. A bilevel page is drawn at 72.01 pixels an inch, as
// png-1bit's pHYs of 2,835 pixels a metre draws it: the pinned pdftoppm
// smooths the edges of a 1-bit image it draws at one pixel a device pixel
// exactly, which litho draws sharp, and the targets cell's renderer check
// would then judge the two renderers apart.
func withTIFFResolution(data []byte, num, den uint32) []byte {
	le := binary.LittleEndian
	ifd := le.Uint32(data[4:])
	n := uint32(le.Uint16(data[ifd:]))
	for i := range n {
		e := data[ifd+2+12*i:]
		if tag := le.Uint16(e); tag == 282 || tag == 283 {
			at := le.Uint32(e[8:])
			le.PutUint32(data[at:], num)
			le.PutUint32(data[at+4:], den)
		}
	}
	return data
}

// animatedGIF returns a GIF of two frames on a 240 by 160 screen: a
// 120 by 80 frame at 60, 40 in light colours a few levels apart over a
// clear screen, then a full frame of other colours. A viewer shows the
// first composed on the clear screen, which a page shows white.
func animatedGIF() []byte {
	pal := make(color.Palette, 0, 17)
	for i := range 16 {
		pal = append(pal, color.NRGBA{uint8(200 + i*3), uint8(240 - i*2), 220, 255}) //nolint:gosec // G115: levels of 200 to 245
	}
	pal = append(pal, color.NRGBA{})
	first := image.NewPaletted(image.Rect(60, 40, 180, 120), pal)
	for y := 40; y < 120; y++ {
		for x := 60; x < 180; x++ {
			first.SetColorIndex(x, y, uint8((x-60)*15/119)) //nolint:gosec // G115: an index of 0 to 15
		}
	}
	second := image.NewPaletted(image.Rect(0, 0, 240, 160), pal)
	for i := range second.Pix {
		second.Pix[i] = 15
	}
	var b bytes.Buffer
	_ = gif.EncodeAll(&b, &gif.GIF{ // a bytes.Buffer takes every write
		Image: []*image.Paletted{first, second}, Delay: []int{50, 50},
		Config: image.Config{Width: 240, Height: 160, ColorModel: pal},
	})
	return b.Bytes()
}

// pngBytes encodes img as PNG.
func pngBytes(img image.Image) []byte {
	var b bytes.Buffer
	_ = png.Encode(&b, img) // a bytes.Buffer takes every write
	return b.Bytes()
}

// imageFiles lists the files of the image corpus, in a fixed order.
func imageFiles() []imageFile {
	photo := func() *image.NRGBA { return imagePhoto(480, 320, 1) }
	gray := func() *image.Gray {
		g := image.NewGray(image.Rect(0, 0, 400, 300))
		p := imagePhoto(400, 300, 2)
		for i := range g.Pix {
			g.Pix[i] = uint8((int(p.Pix[4*i]) + int(p.Pix[4*i+1])) / 2) //nolint:gosec // G115: the mean of two levels
		}
		return g
	}
	indexed := func(colors int) *image.Paletted {
		// A ramp of colours a few levels apart, laid in blocks whose
		// neighbours are next in the ramp: no edge a renderer resamples
		// apart.
		pal := make(color.Palette, 0, colors)
		for i := range colors {
			pal = append(pal, color.NRGBA{uint8(60 + i*120/max(colors-1, 1)), uint8(200 - i*80/max(colors-1, 1)), 140, 255}) //nolint:gosec // G115: levels of 60 to 200
		}
		p := image.NewPaletted(image.Rect(0, 0, 300, 200), pal)
		for y := range 200 {
			for x := range 300 {
				k := (x/20 + y/20) % (2 * (colors - 1))
				if k >= colors {
					k = 2*(colors-1) - k
				}
				p.SetColorIndex(x, y, uint8(k)) //nolint:gosec // G115: an index below the palette of at most 16 colours
			}
		}
		return p
	}
	deep := func() *image.NRGBA64 {
		img := image.NewNRGBA64(image.Rect(0, 0, 320, 200))
		for y := range 200 {
			for x := range 320 {
				img.SetNRGBA64(x, y, color.NRGBA64{uint16(x * 65535 / 319), uint16(y * 65535 / 199), uint16((x+y)*65000/518 + 300), 0xFFFF})
			}
		}
		return img
	}
	bilevel := func() *image.Gray {
		g := image.NewGray(image.Rect(0, 0, 800, 500))
		for y := range 500 {
			for x := range 800 {
				if (x/16+y/16)%2 == 0 || (x-400)*(x-400)+(y-250)*(y-250) < 120*120 {
					g.Pix[y*800+x] = 0xFF
				}
			}
		}
		return g
	}
	hostile := imageHostile()
	files := make([]imageFile, 0, 40+len(hostile))
	files = append(files, []imageFile{
		{name: "png-rgb", ext: "png", data: func() []byte {
			return imagetest.WithPNGChunks(pngBytes(photo()), imagetest.PNGChunk{Type: "pHYs", Data: imagetest.PHYs(150)})
		}},
		{name: "png-alpha", ext: "png", data: func() []byte { return pngBytes(translucent(photo())) }},
		{name: "png-16bit", ext: "png", data: func() []byte { return pngBytes(deep()) }},
		{name: "png-gray", ext: "png", data: func() []byte { return pngBytes(gray()) }},
		{name: "png-indexed", ext: "png", data: func() []byte { return pngBytes(indexed(4)) }},
		{name: "png-indexed-alpha", ext: "png", data: func() []byte {
			p := indexed(6)
			p.Palette[2] = color.NRGBA{0, 0, 0, 0}
			p.Palette[3] = color.NRGBA{200, 50, 50, 128}
			return pngBytes(p)
		}},
		// A bilevel picture is drawn at 72 pixels an inch, one pixel a
		// point, which no renderer resamples.
		{name: "png-1bit", ext: "png", data: func() []byte {
			return imagetest.WithPNGChunks(pngBytes(bilevel()), imagetest.PNGChunk{Type: "pHYs", Data: imagetest.PHYs(72)})
		}},
		{name: "png-icc", ext: "png", data: func() []byte {
			return imagetest.WithPNGChunks(pngBytes(photo()), imagetest.PNGChunk{Type: "iCCP", Data: imagetest.ICCP(imagetest.ICCProfile("RGB ", 2))})
		}},
		{name: "png-described", ext: "png", data: func() []byte {
			return imagetest.WithPNGChunks(pngBytes(photo()),
				imagetest.PNGChunk{Type: "tEXt", Data: []byte("Title\x00Gradients and a disc")},
				imagetest.PNGChunk{Type: "iTXt", Data: []byte("Description\x00\x00\x00en\x00\x00A gradient of red and green under a pale disc")},
				imagetest.PNGChunk{Type: "eXIf", Data: imagetest.EXIF(imagetest.EXIFOptions{Orientation: 6})})
		}},
		{
			name: "png-paper-a4-landscape", ext: "png", data: func() []byte { return pngBytes(photo()) },
			options: `{"paperWidth": 8.27, "paperHeight": 11.69, "landscape": true}` + "\n",
		},
		{name: "jpeg-baseline", ext: "jpg", data: func() []byte {
			return imagetest.JPEG(photo(), imagetest.JPEGOptions{Components: 3, JFIFDPI: 72})
		}},
		{name: "jpeg-gray", ext: "jpg", data: func() []byte { return imagetest.JPEG(gray(), imagetest.JPEGOptions{Components: 1}) }},
		{name: "jpeg-cmyk", ext: "jpg", data: func() []byte {
			return imagetest.JPEG(photo(), imagetest.JPEGOptions{Components: 4, ICC: imagetest.ICCProfile("CMYK", 2)})
		}},
		{name: "jpeg-progressive", ext: "jpg", data: func() []byte {
			return imagetest.JPEG(photo(), imagetest.JPEGOptions{Components: 3, Progressive: true, EXIF: imagetest.EXIF(imagetest.EXIFOptions{DPI: 300, Title: "A progressive JPEG"})})
		}},
		{name: "jpeg-progressive-cmyk", ext: "jpg", data: func() []byte {
			return imagetest.JPEG(photo(), imagetest.JPEGOptions{Components: 4, Progressive: true, EXIF: imagetest.EXIF(imagetest.EXIFOptions{Orientation: 6})})
		}},
		{name: "jpeg-progressive-gray", ext: "jpg", data: func() []byte {
			return imagetest.JPEG(gray(), imagetest.JPEGOptions{Components: 1, Progressive: true, EXIF: imagetest.EXIF(imagetest.EXIFOptions{Orientation: 3})})
		}},
		{name: "gif-static", ext: "gif", data: func() []byte {
			var b bytes.Buffer
			_ = gif.Encode(&b, indexed(16), nil) // a bytes.Buffer takes every write
			return b.Bytes()
		}},
		{name: "gif-animated", ext: "gif", data: animatedGIF},
		{name: "webp-lossless", ext: "webp", data: func() []byte {
			return imagetest.RIFFWebP(imagetest.Chunk{FourCC: "VP8L", Body: imagetest.VP8L(photo())})
		}},
		{name: "webp-alpha", ext: "webp", data: func() []byte {
			return imagetest.RIFFWebP(imagetest.Chunk{FourCC: "VP8L", Body: imagetest.VP8L(translucent(imagePhoto(240, 160, 3)))})
		}},
		{name: "webp-lossy", ext: "webp", data: func() []byte {
			return imagetest.RIFFWebP(imagetest.Chunk{FourCC: "VP8 ", Body: imagetest.VP8(photo())})
		}},
		{name: "webp-animated", ext: "webp", data: func() []byte {
			return imagetest.AnimatedWebP(imagePhoto(200, 120, 4), imagePhoto(200, 120, 5))
		}},
		{name: "webp-icc-exif", ext: "webp", data: func() []byte {
			p := imagePhoto(200, 120, 6)
			return imagetest.RIFFWebP(
				imagetest.Chunk{FourCC: "VP8X", Body: imagetest.VP8X(0x20|0x08, 200, 120)},
				imagetest.Chunk{FourCC: "ICCP", Body: imagetest.ICCProfile("RGB ", 4)},
				imagetest.Chunk{FourCC: "VP8L", Body: imagetest.VP8L(p)},
				imagetest.Chunk{FourCC: "EXIF", Body: imagetest.EXIF(imagetest.EXIFOptions{Orientation: 8, Title: "A WebP with metadata"})})
		}},
		{name: "bmp-8bit", ext: "bmp", data: func() []byte { return imagetest.BMP(indexed(16), 8, 96) }},
		{name: "bmp-24bit", ext: "bmp", data: func() []byte { return imagetest.BMP(photo(), 24, 200) }},
		{name: "bmp-32bit-alpha", ext: "bmp", data: func() []byte { return imagetest.BMP(translucent(photo()), 32, 96) }},
		{name: "tiff-multipage", ext: "tif", data: func() []byte {
			return imagetest.TIFF([]imagetest.TIFFPage{
				{Image: photo(), Compression: imagetest.TIFFNone, DPI: 150, Description: "Four pages"},
				{Image: imagePhoto(160, 100, 7), Compression: imagetest.TIFFNone, Reduced: true},
				{Image: imagePhoto(300, 400, 8), Compression: imagetest.TIFFLZW, DPI: 150, RowsPerStrip: 16},
				{Image: gray(), Compression: imagetest.TIFFPackBits, DPI: 200},
				{Image: indexed(8), Compression: imagetest.TIFFDeflate, DPI: 72, Orientation: 8},
			}, nil)
		}},
		{name: "tiff-bilevel", ext: "tif", data: func() []byte {
			data := imagetest.TIFF([]imagetest.TIFFPage{{Image: bilevel(), Bilevel: true, Compression: imagetest.TIFFPackBits, DPI: 72}}, nil)
			return withTIFFResolution(data, 7201, 100)
		}},
		{name: "tiff-16bit-icc", ext: "tif", data: func() []byte {
			return imagetest.TIFF([]imagetest.TIFFPage{{Image: deep(), Compression: imagetest.TIFFDeflate, ICC: imagetest.ICCProfile("RGB ", 4)}}, nil)
		}},
		{name: "tiff-alpha", ext: "tif", data: func() []byte {
			return imagetest.TIFF([]imagetest.TIFFPage{{Image: translucent(photo()), Alpha: true, Compression: imagetest.TIFFLZW}}, nil)
		}},
		{name: "jp2-rgb", ext: "jp2", data: func() []byte { return imagetest.JPX(photo(), imagetest.JPXOptions{}) }},
		{name: "jp2-gray", ext: "jp2", data: func() []byte { return imagetest.JPX(gray(), imagetest.JPXOptions{}) }},
		{name: "j2k-codestream", ext: "j2k", data: func() []byte {
			return imagetest.JPX(imagePhoto(200, 140, 9), imagetest.JPXOptions{Codestream: true})
		}},
		{name: "ico-sizes", ext: "ico", data: func() []byte {
			icon := func(n int) *image.NRGBA {
				p := imagePhoto(n, n, n)
				for y := range n {
					for x := range n {
						if (x-n/2)*(x-n/2)+(y-n/2)*(y-n/2) > n*n/5 {
							p.SetNRGBA(x, y, color.NRGBA{})
						}
					}
				}
				return p
			}
			return imagetest.ICO(
				imagetest.ICOImage{Width: 16, Height: 16, Bits: 32, Data: pngBytes(icon(16))},
				imagetest.ICOImage{Width: 48, Height: 48, Bits: 24, Data: imagetest.DIB(icon(48), 24)},
				imagetest.ICOImage{Width: 32, Height: 32, Bits: 32, Data: imagetest.DIB(icon(32), 32)},
				imagetest.ICOImage{Width: 0, Height: 0, Bits: 32, Data: imagetest.DIB(icon(256), 32)},
			)
		}},
		{name: "svg-text", ext: "svg", data: func() []byte {
			return []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="400" height="240" viewBox="0 0 400 240">
  <title>A labelled bar chart</title>
  <desc>Three bars of 40, 70 and 55 units with their labels.</desc>
  <rect width="400" height="240" fill="#f4f1ea"/>
  <rect x="40" y="140" width="80" height="80" fill="#3a6ea5"/>
  <rect x="160" y="80" width="80" height="140" fill="#c0504d"/>
  <rect x="280" y="110" width="80" height="110" fill="#9bbb59"/>
  <text x="200" y="40" font-family="Liberation Sans" font-size="22" text-anchor="middle">Units sold</text>
  <text x="80" y="234" font-family="Liberation Serif" font-size="12" text-anchor="middle">Spring</text>
  <text x="200" y="234" font-family="Liberation Serif" font-size="12" text-anchor="middle">Summer</text>
  <text x="320" y="234" font-family="Liberation Serif" font-size="12" text-anchor="middle">Autumn</text>
</svg>
`)
		}},
		{name: "svg-viewbox", ext: "svg", data: func() []byte {
			return []byte(`<?xml version="1.0" encoding="UTF-8"?>
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 300 300">
  <circle cx="150" cy="150" r="120" fill="none" stroke="#222" stroke-width="12"/>
  <path d="M90 160 L135 205 L215 105" fill="none" stroke="#2a8" stroke-width="18" stroke-linecap="round"/>
</svg>
`)
		}},
	}...)
	for _, h := range hostile {
		h.hostile = true
		files = append(files, h)
	}
	return files
}

// imageHostile lists the hostile files of the image corpus: each format's
// file cut in half, a pixel bomb in each format, a TIFF whose chain of
// pages loops, an icon whose image lies outside it, the formats litho
// does not read, and decompression bombs inside a TIFF strip and a PNG.
func imageHostile() []imageFile {
	cut := func(name, ext string, data func() []byte) imageFile {
		return imageFile{name: "truncated-" + name, ext: ext, data: func() []byte {
			d := data()
			return d[:len(d)/2]
		}}
	}
	photo := func() *image.NRGBA { return imagePhoto(200, 120, 10) }
	return []imageFile{
		cut("png", "png", func() []byte { return pngBytes(photo()) }),
		cut("jpeg", "jpg", func() []byte { return imagetest.JPEG(photo(), imagetest.JPEGOptions{Components: 3, Progressive: true}) }),
		cut("gif", "gif", imagetest.AnimatedGIF),
		cut("webp", "webp", func() []byte {
			return imagetest.RIFFWebP(imagetest.Chunk{FourCC: "VP8L", Body: imagetest.VP8L(photo())})
		}),
		cut("bmp", "bmp", func() []byte { return imagetest.BMP(photo(), 24, 96) }),
		cut("tiff", "tif", func() []byte {
			return imagetest.TIFF([]imagetest.TIFFPage{{Image: photo(), Compression: imagetest.TIFFLZW}}, nil)
		}),
		cut("jp2", "jp2", func() []byte { return imagetest.JPX(photo(), imagetest.JPXOptions{}) }),
		cut("ico", "ico", func() []byte {
			return imagetest.ICO(imagetest.ICOImage{Width: 32, Height: 32, Bits: 32, Data: imagetest.DIB(imagePhoto(32, 32, 11), 32)})
		}),
		{name: "bomb-png", ext: "png", data: imagetest.PNGBomb},
		{name: "bomb-jpeg", ext: "jpg", data: imagetest.JPEGBomb},
		{name: "bomb-gif", ext: "gif", data: imagetest.GIFBomb},
		{name: "bomb-webp", ext: "webp", data: imagetest.WebPBomb},
		{name: "bomb-bmp", ext: "bmp", data: imagetest.BMPBomb},
		{name: "bomb-tiff", ext: "tif", data: imagetest.TIFFBomb},
		{name: "bomb-jp2", ext: "j2k", data: imagetest.JPXBomb},
		{name: "bomb-ico", ext: "ico", data: imagetest.ICOBomb},
		{name: "bomb-svg", ext: "svg", data: func() []byte {
			return []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="100000000" height="100000000"><rect width="10" height="10"/></svg>`)
		}},
		{name: "tiff-loop", ext: "tif", data: imagetest.LoopingTIFF},
		{name: "ico-outside", ext: "ico", data: imagetest.ICOOutside},
		{name: "unsupported-heif", ext: "png", data: func() []byte { return []byte("\x00\x00\x00\x18ftypheic\x00\x00\x00\x00mif1heic") }},
		{name: "unsupported-avif", ext: "png", data: func() []byte { return []byte("\x00\x00\x00\x18ftypavif\x00\x00\x00\x00mif1avif") }},
		{name: "unsupported-jxl", ext: "png", data: func() []byte { return []byte("\xff\x0a\xfa\x7f\x01\x00\x00\x00") }},
		{name: "not-an-image", ext: "png", data: func() []byte { return []byte("This is a text file named as a PNG.\n") }},
		{name: "deflate-bomb-tiff-strip", ext: "tif", data: func() []byte { return stripBombTIFF(64 << 20) }},
		{name: "deflate-bomb-png", ext: "png", data: func() []byte { return idatBombPNG(64 << 20) }},
	}
}

// zlibZeros returns n zero bytes zlib-compressed at the best ratio.
func zlibZeros(n int) []byte {
	var z bytes.Buffer
	w, _ := zlib.NewWriterLevel(&z, zlib.BestCompression) // a known level: no error
	zero := make([]byte, 1<<16)
	for written := 0; written < n; written += len(zero) {
		_, _ = w.Write(zero) // writes to a buffer fail on no path
	}
	_ = w.Close() // as above
	return z.Bytes()
}

// stripBombTIFF returns a 100 by 100 gray TIFF whose one Deflate strip
// claims 2^31 - 1 rows and inflates to n zero bytes.
func stripBombTIFF(n int) []byte {
	z := zlibZeros(n)
	le := binary.LittleEndian
	entries := [][3]uint32{{256, 4, 100}, {257, 4, 100}, {258, 3, 8}, {259, 3, 8}, {262, 3, 1}, {273, 4, 0}, {277, 3, 1}, {278, 4, 1<<31 - 1}, {279, 4, uint32(len(z))}} //nolint:gosec // G115: a small strip
	data := le.AppendUint32([]byte("II*\x00"), 8)
	data = le.AppendUint16(data, uint16(len(entries))) //nolint:gosec // G115: nine entries
	strip := 8 + 2 + 12*len(entries) + 4
	for _, e := range entries {
		v := e[2]
		if e[0] == 273 {
			v = uint32(strip) //nolint:gosec // G115: a small offset
		}
		data = le.AppendUint16(data, uint16(e[0])) //nolint:gosec // G115: a tag
		data = le.AppendUint16(data, uint16(e[1])) //nolint:gosec // G115: a type
		data = le.AppendUint32(data, 1)
		if e[1] == 3 {
			v &= 0xFFFF
		}
		data = le.AppendUint32(data, v)
	}
	data = append(data, 0, 0, 0, 0)
	return append(data, z...)
}

// idatBombPNG returns a 100 by 100 gray PNG whose IDAT inflates to n zero
// bytes, far past the 10,100 its rows hold.
func idatBombPNG(n int) []byte {
	small := pngBytes(image.NewGray(image.Rect(0, 0, 100, 100)))
	// The signature and IHDR, then the bomb's IDAT, then IEND.
	head := small[:8+25]
	z := zlibZeros(n)
	chunk := func(typ string, body []byte) []byte {
		var b bytes.Buffer
		_ = binary.Write(&b, binary.BigEndian, uint32(len(body))) //nolint:gosec // G115: a small chunk
		b.WriteString(typ)
		b.Write(body)
		_ = binary.Write(&b, binary.BigEndian, crc32.ChecksumIEEE(append([]byte(typ), body...)))
		return b.Bytes()
	}
	out := append(bytes.Clone(head), chunk("IDAT", z)...)
	return append(out, chunk("IEND", nil)...)
}

// buildImages writes every file of the image corpus into <out>/<name>/,
// with its options.json, replacing what an earlier build wrote there.
func buildImages(m *Manifest) error {
	out := buildOut(m)
	if err := os.RemoveAll(out); err != nil {
		return err
	}
	files := imageFiles()
	for _, f := range files {
		dir := filepath.Join(out, f.name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, f.name+"."+f.ext), f.data(), 0o644); err != nil { //nolint:gosec // testdata is world-readable
			return fmt.Errorf("%s: %w", f.name, err)
		}
		if f.options != "" {
			if err := os.WriteFile(filepath.Join(dir, "options.json"), []byte(f.options), 0o644); err != nil { //nolint:gosec // as above
				return err
			}
		}
	}
	fmt.Printf("%s: %d image files built into %s\n", m.Name, len(files), out)
	return nil
}

// imageFixtures lists the fixtures of the image corpus, each the directory
// its file was built into, refusing one not built yet.
func imageFixtures(m *Manifest) ([]Fixture, error) {
	out := buildOut(m)
	files := imageFiles()
	fixtures := make([]Fixture, 0, len(files))
	for _, f := range files {
		dir := filepath.Join(out, f.name)
		if _, err := os.Stat(filepath.Join(dir, f.name+"."+f.ext)); err != nil {
			return nil, fmt.Errorf("%w: %s is missing (run make corpus-build CORPUS=%s)", errInputsUnfetched, dir, m.Name)
		}
		fixtures = append(fixtures, Fixture{ID: f.name, PDFPath: dir})
	}
	return fixtures, nil
}

// imageHostileNames returns the names of the image corpus's hostile files.
func imageHostileNames() map[string]bool {
	out := map[string]bool{}
	for _, f := range imageHostile() {
		out[f.name] = true
	}
	return out
}
