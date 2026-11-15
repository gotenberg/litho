package imagefile

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/binary"
	"image"
	"image/png"
	"io"
	"unicode/utf8"

	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// pngMeta is what the ancillary chunks of a PNG state (PNG 3rd edition,
// 11.3).
type pngMeta struct {
	width, height int
	xdpi, ydpi    float64
	orientation   int
	icc           []byte
	title, desc   string
}

// readPNGMeta walks the chunks of the PNG data (PNG 3rd edition, 5.3) to
// its IEND or its end. A chunk whose length runs past the data ends the
// walk.
func readPNGMeta(data []byte) pngMeta {
	m := pngMeta{orientation: 1}
	n := int64(len(data))
	for i := int64(8); i+12 <= n; {
		length := int64(binary.BigEndian.Uint32(data[i:]))
		typ := string(data[i+4 : i+8])
		end := i + 12 + length
		if end > n {
			break
		}
		body := data[i+8 : i+8+length]
		switch typ {
		case "IHDR":
			if len(body) >= 8 {
				m.width = int(int32(binary.BigEndian.Uint32(body)))      //nolint:gosec // G115: a 31-bit field (11.2.1); a larger value reads negative and is refused
				m.height = int(int32(binary.BigEndian.Uint32(body[4:]))) //nolint:gosec // G115: as above
			}
		case "pHYs":
			// 11.3.4.3: pixels a unit along x and y; unit 1 is the metre,
			// unit 0 states the aspect ratio alone.
			if len(body) == 9 && body[8] == 1 {
				x, y := float64(binary.BigEndian.Uint32(body)), float64(binary.BigEndian.Uint32(body[4:]))
				m.xdpi, m.ydpi = Resolution(x*0.0254, y*0.0254)
			}
		case "iCCP":
			// 11.3.2.3: a profile name, a NUL, the compression method 0
			// and the zlib stream of the profile.
			if k := bytes.IndexByte(body, 0); k >= 0 && k+2 <= len(body) && body[k+1] == 0 {
				m.icc = inflateCapped(body[k+2:], maxICCInflate)
			}
		case "eXIf":
			x := ReadEXIF(body)
			m.orientation = x.Orientation
		case "tEXt", "zTXt", "iTXt":
			key, text := pngText(typ, body)
			switch key {
			case "Title":
				if m.title == "" {
					m.title = text
				}
			case "Description":
				if m.desc == "" {
					m.desc = text
				}
			}
		case "IEND":
			return m
		}
		i = end
	}
	return m
}

// maxICCInflate is the most an iCCP chunk's profile inflates to before it
// is refused: the profile cap and a byte, so a longer one reads as too
// long.
const maxICCInflate = int64(typeset.MaxICCBytes) + 1

// inflateCapped inflates the zlib stream z to at most limit bytes, nil
// when it does not inflate or reaches the limit.
func inflateCapped(z []byte, limit int64) []byte {
	r, err := zlib.NewReader(bytes.NewReader(z))
	if err != nil {
		return nil
	}
	defer r.Close() // Close reports nothing a read did not
	out, err := io.ReadAll(io.LimitReader(r, limit))
	if err != nil || int64(len(out)) >= limit {
		return nil
	}
	return out
}

// pngText returns the keyword and the text of a tEXt, zTXt or iTXt chunk
// (PNG 3rd edition, 11.3.3): Latin-1 text for the first two, UTF-8 for
// the last, compressed in zTXt and in an iTXt whose flag says so. The
// text is cut to [maxDescriptionBytes].
func pngText(typ string, body []byte) (string, string) {
	k := bytes.IndexByte(body, 0)
	if k < 1 || k > 79 {
		return "", ""
	}
	key, rest := string(body[:k]), body[k+1:]
	switch typ {
	case "tEXt":
		return key, cleanText(latin1(rest))
	case "zTXt":
		if len(rest) < 1 || rest[0] != 0 {
			return "", ""
		}
		return key, cleanText(latin1(inflateCapped(rest[1:], maxDescriptionBytes*4)))
	}
	// iTXt: compression flag, method, language tag, NUL, translated
	// keyword, NUL, text.
	if len(rest) < 2 {
		return "", ""
	}
	compressed, method := rest[0], rest[1]
	rest = rest[2:]
	for range 2 {
		z := bytes.IndexByte(rest, 0)
		if z < 0 {
			return "", ""
		}
		rest = rest[z+1:]
	}
	if compressed == 1 {
		if method != 0 {
			return "", ""
		}
		rest = inflateCapped(rest, maxDescriptionBytes*4)
	}
	return key, cleanText(rest)
}

// latin1 returns the ISO 8859-1 text b in UTF-8.
func latin1(b []byte) []byte {
	out := make([]byte, 0, len(b))
	for _, c := range b {
		out = utf8.AppendRune(out, rune(c))
	}
	return out
}

// readPNGFile reads the one picture of a PNG file.
func readPNGFile(ctx context.Context, data []byte, o *Options, fn func(*Picture) error) (Info, error) {
	m := readPNGMeta(data)
	pic, err := readPNG(ctx, data, m, o)
	if err != nil {
		return Info{}, err
	}
	pic.Description = m.desc
	return Info{Title: m.title}, fn(pic)
}

// readPNG decodes a PNG picture whose chunks state m. Without an iCCP
// chunk, its colours are sRGB (PNG 3rd edition, 4.3 and 11.3.2.5) and
// are written as DeviceRGB or DeviceGray; gAMA and cHRM are not applied.
func readPNG(ctx context.Context, data []byte, m pngMeta, o *Options) (*Picture, error) {
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, malformed("PNG header", err)
	}
	if err := checkSize(ctx, cfg.Width, cfg.Height); err != nil {
		return nil, err
	}
	icc, reason := readProfile(m.icc, o.PDF14)
	o.warn(reason)
	src, err := decoded(ctx, cfg.Width, cfg.Height, cfg.ColorModel, func() (image.Image, error) {
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, malformed("PNG", err)
		}
		return img, nil
	}, encodeOptions{no16: o.PDF14, icc: icc})
	if err != nil {
		return nil, err
	}
	return &Picture{Source: src, Width: cfg.Width, Height: cfg.Height, XDPI: m.xdpi, YDPI: m.ydpi, Orientation: m.orientation}, nil
}
