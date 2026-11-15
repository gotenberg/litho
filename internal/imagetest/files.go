package imagetest

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"slices"
)

// EXIFOptions is what [EXIF] writes in IFD0.
type EXIFOptions struct {
	// Orientation is the Orientation tag, 0 for none.
	Orientation int
	// DPI is the XResolution and YResolution in pixels an inch, 0 for
	// none.
	DPI int
	// Title is the ImageDescription tag, "" for none.
	Title string
}

// EXIF returns the TIFF structure of an EXIF segment holding o in IFD0
// (EXIF 2.32, 4.5.2 and 4.6.4), big-endian.
func EXIF(o EXIFOptions) []byte {
	type entry struct {
		tag, typ uint16
		count    uint32
		value    []byte
	}
	var entries []entry
	if o.Title != "" {
		entries = append(entries, entry{270, 2, uint32(len(o.Title) + 1), append([]byte(o.Title), 0)}) //nolint:gosec // G115: a test title
	}
	if o.Orientation != 0 {
		entries = append(entries, entry{274, 3, 1, []byte{0, byte(o.Orientation), 0, 0}}) //nolint:gosec // G115: an orientation of 1 to 8
	}
	if o.DPI != 0 {
		r := binary.BigEndian.AppendUint32(binary.BigEndian.AppendUint32(nil, uint32(o.DPI)), 1) //nolint:gosec // G115: a test resolution
		entries = append(entries, entry{282, 5, 1, r}, entry{283, 5, 1, r}, entry{296, 3, 1, []byte{0, 2, 0, 0}})
	}
	slices.SortFunc(entries, func(a, b entry) int { return int(a.tag) - int(b.tag) })
	out := []byte("MM\x00*\x00\x00\x00\x08")
	out = binary.BigEndian.AppendUint16(out, uint16(len(entries))) //nolint:gosec // G115: a few entries
	extra := 8 + 2 + 12*len(entries) + 4
	var tail []byte
	for _, e := range entries {
		out = binary.BigEndian.AppendUint16(out, e.tag)
		out = binary.BigEndian.AppendUint16(out, e.typ)
		out = binary.BigEndian.AppendUint32(out, e.count)
		if len(e.value) <= 4 {
			v := append(slices.Clone(e.value), 0, 0, 0, 0)
			out = append(out, v[:4]...)
			continue
		}
		out = binary.BigEndian.AppendUint32(out, uint32(extra+len(tail))) //nolint:gosec // G115: a small structure
		tail = append(tail, e.value...)
		if len(tail)%2 == 1 {
			tail = append(tail, 0)
		}
	}
	out = append(out, 0, 0, 0, 0)
	return append(out, tail...)
}

// PNGChunk is a chunk to insert into a PNG file.
type PNGChunk struct {
	Type string
	Data []byte
}

// WithPNGChunks returns the PNG file data with chunks inserted after its
// IHDR chunk, each with its CRC (PNG 3rd edition, 5.3).
func WithPNGChunks(data []byte, chunks ...PNGChunk) []byte {
	ihdrEnd := 8 + 12 + int(binary.BigEndian.Uint32(data[8:]))
	out := slices.Clone(data[:ihdrEnd])
	for _, c := range chunks {
		out = binary.BigEndian.AppendUint32(out, uint32(len(c.Data))) //nolint:gosec // G115: a test chunk
		body := append([]byte(c.Type), c.Data...)
		out = append(out, body...)
		out = binary.BigEndian.AppendUint32(out, crc32.ChecksumIEEE(body))
	}
	return append(out, data[ihdrEnd:]...)
}

// PHYs returns the body of a pHYs chunk of dpi pixels an inch, as pixels
// a metre rounded (PNG 3rd edition, 11.3.4.3).
func PHYs(dpi int) []byte {
	ppm := uint32((dpi*10000 + 127) / 254) //nolint:gosec // G115: a test resolution
	return append(binary.BigEndian.AppendUint32(binary.BigEndian.AppendUint32(nil, ppm), ppm), 1)
}

// ICCP returns the body of an iCCP chunk holding profile (PNG 3rd
// edition, 11.3.2.3).
func ICCP(profile []byte) []byte {
	var z bytes.Buffer
	w := zlib.NewWriter(&z)
	_, _ = w.Write(profile) // writes to a buffer fail on no path
	_ = w.Close()           // as above
	return append([]byte("profile\x00\x00"), z.Bytes()...)
}

// ICCProfile returns an ICC profile (ICC.1:2010) of the colour space
// space, "GRAY", "RGB " or "CMYK", of major version major: a display
// profile of sRGB's primaries and D50 white with a gamma of 2.2 for gray
// and RGB, a bare output profile for CMYK, holding the tags a reader of
// its class needs. It is valid enough to be embedded and to be refused
// for its version; it is not meant to match sRGB.
func ICCProfile(space string, major byte) []byte {
	xyz := func(x, y, z float64) []byte {
		b := []byte("XYZ \x00\x00\x00\x00")
		for _, v := range []float64{x, y, z} {
			b = binary.BigEndian.AppendUint32(b, uint32(int32(v*65536+0.5))) //nolint:gosec // G115: s15Fixed16 of a small value
		}
		return b
	}
	curv := append([]byte("curv\x00\x00\x00\x00\x00\x00\x00\x01"), 0x02, 0x33, 0, 0) // gamma 2.2 as u8Fixed8
	desc := append(binary.BigEndian.AppendUint32([]byte("desc\x00\x00\x00\x00"), 5), "test\x00"...)
	desc = append(desc, make([]byte, 4+4+2+1+67)...)
	type tag struct {
		sig  string
		data []byte
	}
	tags := []tag{{"desc", desc}, {"wtpt", xyz(0.9642, 1, 0.8249)}}
	class := "mntr"
	switch space {
	case "GRAY":
		tags = append(tags, tag{"kTRC", curv})
	case "RGB ":
		tags = append(tags,
			tag{"rXYZ", xyz(0.4361, 0.2225, 0.0139)}, tag{"gXYZ", xyz(0.3851, 0.7169, 0.0971)}, tag{"bXYZ", xyz(0.1431, 0.0606, 0.7141)},
			tag{"rTRC", curv}, tag{"gTRC", curv}, tag{"bTRC", curv})
	default:
		class = "prtr"
	}
	head := make([]byte, 128, 128+4+12*len(tags))
	head[8] = major
	copy(head[12:], class)
	copy(head[16:], space)
	copy(head[20:], "XYZ ")
	copy(head[36:], "acsp")
	copy(head[68:], xyz(0.9642, 1, 0.8249)[8:])
	table := binary.BigEndian.AppendUint32(nil, uint32(len(tags))) //nolint:gosec // G115: a few tags
	off := 128 + 4 + 12*len(tags)
	var body []byte
	for _, t := range tags {
		for len(body)%4 != 0 {
			body = append(body, 0)
		}
		table = append(table, t.sig...)
		table = binary.BigEndian.AppendUint32(table, uint32(off+len(body))) //nolint:gosec // G115: a small profile
		table = binary.BigEndian.AppendUint32(table, uint32(len(t.data)))   //nolint:gosec // G115: as above
		body = append(body, t.data...)
	}
	out := append(append(head, table...), body...)
	binary.BigEndian.PutUint32(out, uint32(len(out))) //nolint:gosec // G115: as above
	return out
}

// BMP returns img as a BMP file of the given bits a pixel: 8 (its palette, a
// [*image.Paletted] of at most 256 colours), 24 or 32, the last with a
// BITMAPV4HEADER whose alpha the picture's (Microsoft, BITMAPV4HEADER),
// bottom-up, with the resolution dpi pixels an inch.
func BMP(img image.Image, bits, dpi int) []byte {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	header := 40
	if bits == 32 {
		header = 108
	}
	var palette []byte
	if bits == 8 {
		for _, c := range img.(*image.Paletted).Palette { //nolint:forcetypeassert // an 8-bit BMP is written from a paletted picture
			n := color.NRGBAModel.Convert(c).(color.NRGBA) //nolint:forcetypeassert // NRGBAModel returns color.NRGBA
			palette = append(palette, n.B, n.G, n.R, 0)
		}
	}
	row := (w*bits + 31) / 32 * 4
	off := 14 + header + len(palette)
	out := []byte("BM")
	out = binary.LittleEndian.AppendUint32(out, uint32(off+row*h)) //nolint:gosec // G115: a test file
	out = append(out, 0, 0, 0, 0)
	out = binary.LittleEndian.AppendUint32(out, uint32(off)) //nolint:gosec // G115: as above
	hdr := make([]byte, header)
	binary.LittleEndian.PutUint32(hdr, uint32(header)) //nolint:gosec // G115: 40 or 108
	binary.LittleEndian.PutUint32(hdr[4:], uint32(w))  //nolint:gosec // G115: a test size
	binary.LittleEndian.PutUint32(hdr[8:], uint32(h))  //nolint:gosec // G115: as above
	binary.LittleEndian.PutUint16(hdr[12:], 1)
	binary.LittleEndian.PutUint16(hdr[14:], uint16(bits))                //nolint:gosec // G115: 8, 24 or 32
	binary.LittleEndian.PutUint32(hdr[20:], uint32(row*h))               //nolint:gosec // G115: a test size
	binary.LittleEndian.PutUint32(hdr[24:], uint32((dpi*10000+127)/254)) //nolint:gosec // G115: a test resolution
	binary.LittleEndian.PutUint32(hdr[28:], uint32((dpi*10000+127)/254)) //nolint:gosec // G115: as above
	binary.LittleEndian.PutUint32(hdr[32:], uint32(len(palette)/4))      //nolint:gosec // G115: at most 256
	out = append(append(out, hdr...), palette...)
	line := make([]byte, row)
	for y := h - 1; y >= 0; y-- {
		clear(line)
		for x := range w {
			switch bits {
			case 8:
				line[x] = img.(*image.Paletted).ColorIndexAt(b.Min.X+x, b.Min.Y+y) //nolint:forcetypeassert // as above
			default:
				c := color.NRGBAModel.Convert(img.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA) //nolint:forcetypeassert // NRGBAModel returns color.NRGBA
				k := x * bits / 8
				line[k], line[k+1], line[k+2] = c.B, c.G, c.R
				if bits == 32 {
					line[k+3] = c.A
				}
			}
		}
		out = append(out, line...)
	}
	return out
}

// ICOImage is one image of an icon file: a PNG file or a DIB.
type ICOImage struct {
	// Width and Height are the sizes the directory states, 256 written as
	// 0.
	Width, Height, Bits int
	Data                []byte
}

// ICO returns an icon file holding images, in order (the Windows icon
// directory: a header, a 16-byte entry an image, then their data).
func ICO(images ...ICOImage) []byte {
	out := []byte{0, 0, 1, 0}
	out = binary.LittleEndian.AppendUint16(out, uint16(len(images))) //nolint:gosec // G115: a few images
	off := 6 + 16*len(images)
	for _, im := range images {
		out = append(out, byte(im.Width), byte(im.Height), 0, 0) //nolint:gosec // G115: the directory holds a byte, 256 written as 0
		out = binary.LittleEndian.AppendUint16(out, 1)
		out = binary.LittleEndian.AppendUint16(out, uint16(im.Bits))      //nolint:gosec // G115: a bit count
		out = binary.LittleEndian.AppendUint32(out, uint32(len(im.Data))) //nolint:gosec // G115: a test image
		out = binary.LittleEndian.AppendUint32(out, uint32(off))          //nolint:gosec // G115: a test file
		off += len(im.Data)
	}
	for _, im := range images {
		out = append(out, im.Data...)
	}
	return out
}

// DIB returns img as the DIB of an icon entry (a BMP without its file
// header): a BITMAPINFOHEADER whose height counts the colour bitmap and
// the AND mask, a 32-bit or 24-bit colour bitmap from the bottom, then the
// AND mask, 1 where the picture is transparent.
func DIB(img image.Image, bits int) []byte {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	hdr := make([]byte, 40)
	binary.LittleEndian.PutUint32(hdr, 40)
	binary.LittleEndian.PutUint32(hdr[4:], uint32(w))   //nolint:gosec // G115: an icon's size
	binary.LittleEndian.PutUint32(hdr[8:], uint32(2*h)) //nolint:gosec // G115: as above
	binary.LittleEndian.PutUint16(hdr[12:], 1)
	binary.LittleEndian.PutUint16(hdr[14:], uint16(bits)) //nolint:gosec // G115: 24 or 32
	row := (w*bits + 31) / 32 * 4
	mrow := (w + 31) / 32 * 4
	out := hdr
	for y := h - 1; y >= 0; y-- {
		line := make([]byte, row)
		for x := range w {
			c := color.NRGBAModel.Convert(img.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA) //nolint:forcetypeassert // NRGBAModel returns color.NRGBA
			k := x * bits / 8
			line[k], line[k+1], line[k+2] = c.B, c.G, c.R
			if bits == 32 {
				line[k+3] = c.A
			}
		}
		out = append(out, line...)
	}
	for y := h - 1; y >= 0; y-- {
		line := make([]byte, mrow)
		for x := range w {
			if color.NRGBAModel.Convert(img.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA).A < 0x80 { //nolint:forcetypeassert // NRGBAModel returns color.NRGBA
				line[x/8] |= 0x80 >> (x % 8)
			}
		}
		out = append(out, line...)
	}
	return out
}

// AnimatedGIF returns a GIF of two frames on a 24 by 16 screen: a 12 by 8
// frame at 6, 4 over a transparent screen, then a full frame of another
// colour.
func AnimatedGIF() []byte {
	pal := color.Palette{color.NRGBA{200, 30, 30, 255}, color.NRGBA{30, 30, 200, 255}, color.NRGBA{}}
	first := image.NewPaletted(image.Rect(6, 4, 18, 12), pal)
	for i := range first.Pix {
		first.Pix[i] = uint8(i % 2)
	}
	second := image.NewPaletted(image.Rect(0, 0, 24, 16), pal)
	for i := range second.Pix {
		second.Pix[i] = 1
	}
	var b bytes.Buffer
	_ = gif.EncodeAll(&b, &gif.GIF{ // a bytes.Buffer takes every write
		Image: []*image.Paletted{first, second}, Delay: []int{50, 50},
		Config: image.Config{Width: 24, Height: 16, ColorModel: pal},
	})
	return b.Bytes()
}
