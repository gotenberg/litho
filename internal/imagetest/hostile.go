package imagetest

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"hash/crc32"
	"image"
)

// The pixel bombs: a file of a few hundred bytes whose header promises far
// more pixels than any cap admits. A reader that allocates what the header
// says before it checks the size runs out of memory.

// pngChunk returns a PNG chunk of type typ with its CRC.
func pngChunk(typ string, body []byte) []byte {
	out := binary.BigEndian.AppendUint32(nil, uint32(len(body))) //nolint:gosec // G115: a small chunk
	tb := append([]byte(typ), body...)
	out = append(out, tb...)
	return binary.BigEndian.AppendUint32(out, crc32.ChecksumIEEE(tb))
}

// PNGBomb returns a PNG of 100,000 by 100,000 gray pixels whose IDAT holds
// one row.
func PNGBomb() []byte {
	ihdr := binary.BigEndian.AppendUint32(binary.BigEndian.AppendUint32(nil, 100_000), 100_000)
	ihdr = append(ihdr, 8, 0, 0, 0, 0)
	var z bytes.Buffer
	w := zlib.NewWriter(&z)
	_, _ = w.Write(make([]byte, 100_001)) // writes to a buffer fail on no path
	_ = w.Close()                         // as above
	out := []byte("\x89PNG\r\n\x1a\n")
	out = append(out, pngChunk("IHDR", ihdr)...)
	out = append(out, pngChunk("IDAT", z.Bytes())...)
	return append(out, pngChunk("IEND", nil)...)
}

// JPEGBomb returns a JPEG whose frame is 65,535 by 65,535 pixels and
// whose one scan ends at once.
func JPEGBomb() []byte {
	var out bytes.Buffer
	out.Write([]byte{0xFF, 0xD8})
	dqt := make([]byte, 65)
	for i := 1; i < len(dqt); i++ {
		dqt[i] = 1
	}
	segment(&out, 0xDB, dqt)
	segment(&out, 0xC0, []byte{8, 0xFF, 0xFF, 0xFF, 0xFF, 1, 1, 0x11, 0})
	dht := append([]byte{0x00}, dcBits[:]...)
	dht = append(dht, dcValues...)
	dht = append(dht, 0x10)
	dht = append(dht, acBits[:]...)
	dht = append(dht, acValues...)
	segment(&out, 0xC4, dht)
	segment(&out, 0xDA, []byte{1, 1, 0x00, 0, 63, 0})
	out.Write([]byte{0x00, 0xFF, 0xD9})
	return out.Bytes()
}

// GIFBomb returns a GIF whose logical screen is 65,535 by 65,535 pixels,
// holding one image of 1 by 1.
func GIFBomb() []byte {
	out := []byte("GIF89a\xff\xff\xff\xff\x80\x00\x00")
	out = append(out, 0, 0, 0, 255, 255, 255)
	out = append(out, ',', 0, 0, 0, 0, 1, 0, 1, 0, 0)
	return append(out, 2, 2, 0x4C, 0x01, 0, ';')
}

// WebPBomb returns a lossless WebP whose bitstream is 16,384 by 16,384
// pixels, past the default cap of 100 million.
func WebPBomb() []byte {
	return RIFFWebP(Chunk{"VP8L", []byte{0x2f, 0xff, 0xff, 0xff, 0x0f, 0, 0, 0}})
}

// BMPBomb returns a 24-bit BMP of 100,000 by 100,000 pixels and no pixel
// data.
func BMPBomb() []byte {
	small := BMP(image.NewNRGBA(image.Rect(0, 0, 1, 1)), 24, 96)
	binary.LittleEndian.PutUint32(small[18:], 100_000)
	binary.LittleEndian.PutUint32(small[22:], 100_000)
	return small
}

// TIFFBomb returns a TIFF whose one page is 100,000 by 100,000 pixels.
func TIFFBomb() []byte {
	data := TIFF([]TIFFPage{{Image: image.NewGray(image.Rect(0, 0, 1, 1)), Compression: TIFFNone}}, nil)
	ifd := binary.LittleEndian.Uint32(data[4:])
	n := int(binary.LittleEndian.Uint16(data[ifd:]))
	for i := range n {
		e := data[int(ifd)+2+12*i:]
		switch binary.LittleEndian.Uint16(e) {
		case 256, 257:
			binary.LittleEndian.PutUint32(e[8:], 100_000)
		}
	}
	return data
}

// ICOBomb returns an icon file whose one image is [PNGBomb].
func ICOBomb() []byte {
	return ICO(ICOImage{Width: 0, Height: 0, Bits: 32, Data: PNGBomb()})
}

// JPXBomb returns a bare JPEG 2000 codestream whose image and tile are
// 100,000 by 100,000 pixels.
func JPXBomb() []byte {
	data := JPX(image.NewGray(image.Rect(0, 0, 1, 1)), JPXOptions{Codestream: true})
	// SOC, then SIZ: its marker, Lsiz and Rsiz before Xsiz, Ysiz, XOsiz,
	// YOsiz, XTsiz and YTsiz (ITU-T T.800, A.5.1).
	for _, at := range []int{8, 12, 24, 28} {
		binary.BigEndian.PutUint32(data[at:], 100_000)
	}
	return data
}

// LoopingTIFF returns a TIFF of two pages whose last IFD links back to
// its first.
func LoopingTIFF() []byte {
	page := func(c uint8) TIFFPage {
		img := image.NewGray(image.Rect(0, 0, 16, 16))
		for i := range img.Pix {
			img.Pix[i] = c
		}
		return TIFFPage{Image: img, Compression: TIFFNone}
	}
	return TIFF([]TIFFPage{page(40), page(200)}, func(ifds []int) int { return ifds[0] })
}

// ICOOutside returns an icon file whose one image's offset points past the
// file's end.
func ICOOutside() []byte {
	img := image.NewNRGBA(image.Rect(0, 0, 16, 16))
	for i := range img.Pix {
		img.Pix[i] = 0xFF
	}
	data := ICO(ICOImage{Width: 16, Height: 16, Bits: 32, Data: DIB(img, 32)})
	binary.LittleEndian.PutUint32(data[18:], 1<<30)
	return data
}
