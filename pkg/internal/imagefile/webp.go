package imagefile

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"

	"golang.org/x/image/webp"
)

// webpChunk is one chunk of a WebP file's RIFF container (RFC 9649, 2.4).
type webpChunk struct {
	fourCC string
	body   []byte
}

// errWebPChunks reports a RIFF container whose chunks do not lie in it.
var errWebPChunks = errors.New("chunks run past the file")

// webpChunks splits the RIFF payload of a WebP file into its chunks, each
// padded to an even length (RFC 9649, 2.3).
func webpChunks(data []byte) ([]webpChunk, error) {
	if len(data) < 12 {
		return nil, errWebPChunks
	}
	end := min(int64(binary.LittleEndian.Uint32(data[4:]))+8, int64(len(data)))
	var chunks []webpChunk
	for i := int64(12); i+8 <= end; {
		size := int64(binary.LittleEndian.Uint32(data[i+4:]))
		if i+8+size > end {
			return nil, errWebPChunks
		}
		chunks = append(chunks, webpChunk{fourCC: string(data[i : i+4]), body: data[i+8 : i+8+size]})
		i += 8 + size + size&1
	}
	return chunks, nil
}

// frameSize returns the size a VP8 or VP8L bitstream states in its header
// (RFC 6386, 9.1: 14 bits each past the start code; RFC 9649, 3.2: 14
// bits each past the signature byte, less one).
func frameSize(c webpChunk) (int, int, error) {
	switch c.fourCC {
	case "VP8 ":
		b := c.body
		if len(b) < 10 || b[3] != 0x9d || b[4] != 0x01 || b[5] != 0x2a {
			return 0, 0, errors.New("a VP8 frame without its start code")
		}
		return int(binary.LittleEndian.Uint16(b[6:]) & 0x3fff), int(binary.LittleEndian.Uint16(b[8:]) & 0x3fff), nil
	case "VP8L":
		b := c.body
		if len(b) < 5 || b[0] != 0x2f {
			return 0, 0, errors.New("a VP8L bitstream without its signature")
		}
		v := binary.LittleEndian.Uint32(b[1:])
		return int(v&0x3fff) + 1, int(v>>14&0x3fff) + 1, nil // fits int: 14 bits each, plus one
	}
	return 0, 0, fmt.Errorf("a %q chunk where a bitstream belongs", c.fourCC)
}

// readWebP reads a WebP file (RFC 9649): a still picture, or the first
// frame of an animation composed on its canvas as a viewer shows it, the
// rest of the canvas transparent, as browsers leave it whatever the
// background colour of the ANIM chunk, which the format makes a hint
// (RFC 9649, 2.7.1.1). Every size a header states, the canvas's and the
// bitstream's, is checked against the pixel cap before decoding. Its
// colours are read in its ICCP profile when it carries one, else as sRGB,
// written as DeviceRGB; its EXIF chunk gives its orientation and title. A
// WebP file states no resolution.
func readWebP(ctx context.Context, data []byte, o *Options, fn func(*Picture) error) (Info, error) {
	chunks, err := webpChunks(data)
	if err != nil {
		return Info{}, malformed("WebP", err)
	}
	if len(chunks) == 0 {
		return Info{}, malformed("WebP", errors.New("no chunk"))
	}
	var exif EXIF
	exif.Orientation = 1
	var iccData []byte
	animated := false
	cw, ch := 0, 0
	var still, first *webpChunk
	for i := range chunks {
		c := &chunks[i]
		switch c.fourCC {
		case "VP8X":
			if len(c.body) < 10 {
				return Info{}, malformed("WebP", errors.New("a short VP8X chunk"))
			}
			animated = c.body[0]&0x02 != 0
			cw = int(uint32(c.body[4])|uint32(c.body[5])<<8|uint32(c.body[6])<<16) + 1 // fits int: 24 bits, plus one
			ch = int(uint32(c.body[7])|uint32(c.body[8])<<8|uint32(c.body[9])<<16) + 1 // fits int: 24 bits, plus one
		case "ICCP":
			iccData = c.body
		case "EXIF":
			exif = ReadEXIF(bytes.TrimPrefix(c.body, []byte("Exif\x00\x00")))
		case "VP8 ", "VP8L":
			if still == nil {
				still = c
			}
		case "ANMF":
			if first == nil {
				first = c
			}
		}
	}
	icc, reason := readProfile(iccData, o.PDF14)
	o.warn(reason)
	var pic *Picture
	if animated {
		if first == nil {
			return Info{}, malformed("WebP", errors.New("an animation without a frame"))
		}
		pic, err = readWebPFrame(ctx, first.body, cw, ch, icc)
	} else {
		if still == nil {
			return Info{}, malformed("WebP", errors.New("no bitstream"))
		}
		w, h, ferr := frameSize(*still)
		if ferr != nil {
			return Info{}, malformed("WebP", ferr)
		}
		if err := checkSize(ctx, w, h); err != nil {
			return Info{}, err
		}
		if cw == 0 {
			cw, ch = w, h
		}
		if err := checkSize(ctx, cw, ch); err != nil {
			return Info{}, err
		}
		if cw != w || ch != h {
			return Info{}, malformed("WebP", fmt.Errorf("a canvas of %d by %d pixels holding a picture of %d by %d", cw, ch, w, h))
		}
		pic, err = decodeWebP(ctx, data, w, h, icc)
	}
	if err != nil {
		return Info{}, err
	}
	pic.Orientation, pic.Title = exif.Orientation, exif.Title
	return Info{Title: exif.Title}, fn(pic)
}

// decodeWebP decodes the still WebP file data of w by h pixels.
func decodeWebP(ctx context.Context, data []byte, w, h int, icc *profile) (*Picture, error) {
	src, err := decoded(ctx, w, h, color.NRGBAModel, func() (image.Image, error) {
		img, err := webp.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, malformed("WebP", err)
		}
		return img, nil
	}, encodeOptions{icc: icc})
	if err != nil {
		return nil, err
	}
	return &Picture{Source: src, Width: w, Height: h}, nil
}

// readWebPFrame decodes the frame an ANMF chunk holds (RFC 9649, 2.7.1.2)
// and composes it at its offset on a transparent canvas of cw by ch
// pixels.
func readWebPFrame(ctx context.Context, body []byte, cw, ch int, icc *profile) (*Picture, error) {
	if cw == 0 || len(body) < 16 {
		return nil, malformed("WebP", errors.New("an animation frame without its header"))
	}
	if err := checkSize(ctx, cw, ch); err != nil {
		return nil, err
	}
	u24 := func(b []byte) int { return int(uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16) } // fits int: 24 bits
	x, y := 2*u24(body), 2*u24(body[3:])
	fw, fh := u24(body[6:])+1, u24(body[9:])+1
	if x+fw > cw || y+fh > ch {
		return nil, malformed("WebP", fmt.Errorf("a frame of %d by %d pixels at %d, %d past a canvas of %d by %d", fw, fh, x, y, cw, ch))
	}
	frame := append([]byte("RIFF\x00\x00\x00\x00WEBP"), body[16:]...)
	sub, err := webpChunks(frameWithSize(frame))
	if err != nil {
		return nil, malformed("WebP frame", err)
	}
	var alph, bits *webpChunk
	for i := range sub {
		switch sub[i].fourCC {
		case "ALPH":
			if alph == nil {
				alph = &sub[i]
			}
		case "VP8 ", "VP8L":
			if bits == nil {
				bits = &sub[i]
			}
		}
	}
	if bits == nil {
		return nil, malformed("WebP", errors.New("an animation frame without a bitstream"))
	}
	bw, bh, err := frameSize(*bits)
	if err != nil {
		return nil, malformed("WebP", err)
	}
	if bw != fw || bh != fh {
		return nil, malformed("WebP", fmt.Errorf("a frame of %d by %d pixels holding a bitstream of %d by %d", fw, fh, bw, bh))
	}
	// The frame as a still file of its own: its alpha chunk, when it has
	// one, under a VP8X header that announces it (RFC 9649, 2.7.1).
	var still bytes.Buffer
	still.WriteString("RIFF\x00\x00\x00\x00WEBP")
	if alph != nil {
		vp8x := []byte{0x10, 0, 0, 0, byte(fw - 1), byte((fw - 1) >> 8), byte((fw - 1) >> 16), byte(fh - 1), byte((fh - 1) >> 8), byte((fh - 1) >> 16)} //nolint:gosec // G115: the three bytes of a frame size, at most 2^24 by RFC 9649, 2.7.1.2
		writeChunk(&still, "VP8X", vp8x)
		writeChunk(&still, "ALPH", alph.body)
	}
	writeChunk(&still, bits.fourCC, bits.body)
	stillData := frameWithSize(still.Bytes())
	src, err := decoded(ctx, cw, ch, color.NRGBAModel, func() (image.Image, error) {
		img, err := webp.Decode(bytes.NewReader(stillData))
		if err != nil {
			return nil, malformed("WebP frame", err)
		}
		if x == 0 && y == 0 && fw == cw && fh == ch {
			return img, nil
		}
		canvas := image.NewNRGBA(image.Rect(0, 0, cw, ch))
		draw.Draw(canvas, image.Rect(x, y, x+fw, y+fh), img, image.Point{}, draw.Src)
		return canvas, nil
	}, encodeOptions{icc: icc})
	if err != nil {
		return nil, err
	}
	return &Picture{Source: src, Width: cw, Height: ch}, nil
}

// writeChunk writes a RIFF chunk, padded to an even length.
func writeChunk(b *bytes.Buffer, fourCC string, body []byte) {
	b.WriteString(fourCC)
	var size [4]byte
	binary.LittleEndian.PutUint32(size[:], uint32(len(body))) //nolint:gosec // G115: a chunk body cut from a file of at most the input cap
	b.Write(size[:])
	b.Write(body)
	if len(body)%2 == 1 {
		b.WriteByte(0)
	}
}

// frameWithSize sets the RIFF size of a container built in memory.
func frameWithSize(data []byte) []byte {
	binary.LittleEndian.PutUint32(data[4:], uint32(len(data)-8)) //nolint:gosec // G115: a container of at most the input cap
	return data
}
