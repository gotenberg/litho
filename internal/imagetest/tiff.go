package imagetest

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"image"
	"image/color"
	"slices"
)

// The TIFF compressions [TIFF] writes (TIFF 6.0, sections 3, 9 and 13;
// TIFF Technical Note 2).
const (
	TIFFNone     = 1
	TIFFLZW      = 5
	TIFFDeflate  = 8
	TIFFPackBits = 32773
)

// TIFFPage is one IFD of a TIFF file.
type TIFFPage struct {
	// Image is the page: a [*image.Gray] (8 bits, or 1 when Bilevel), a
	// [*image.Gray16], a [*image.Paletted] (8 bits), an [*image.NRGBA]
	// (RGB, with its alpha unassociated when Alpha) or an
	// [*image.NRGBA64] (16-bit RGB).
	Image   image.Image
	Bilevel bool
	Alpha   bool
	// Compression is one of the constants above.
	Compression int
	// RowsPerStrip cuts the page into strips; 0 writes one strip.
	RowsPerStrip int
	// DPI, Orientation, ICC and Description write those tags when set.
	DPI         int
	Orientation int
	ICC         []byte
	Description string
	// Reduced marks a reduced-resolution image (NewSubfileType 1).
	Reduced bool
}

// TIFF returns a little-endian TIFF file (TIFF 6.0) of the pages, in
// order, each IFD followed by its strips. next, when set, gives the offset
// the last IFD links to instead of 0: a test makes the chain loop with it.
func TIFF(pages []TIFFPage, next func(ifds []int) int) []byte {
	out := []byte("II*\x00\x00\x00\x00\x00")
	ifds := make([]int, 0, len(pages))
	links := make([]int, 0, len(pages))
	for _, p := range pages {
		strips := tiffStrips(p)
		type entry struct {
			tag, typ uint16
			count    uint32
			value    []byte
		}
		le := binary.LittleEndian
		short := func(v ...int) []byte {
			var b []byte
			for _, x := range v {
				b = le.AppendUint16(b, uint16(x)) //nolint:gosec // G115: a test tag
			}
			return b
		}
		long := func(v ...int) []byte {
			var b []byte
			for _, x := range v {
				b = le.AppendUint32(b, uint32(x)) //nolint:gosec // G115: a test tag
			}
			return b
		}
		b := p.Image.Bounds()
		var entries []entry
		add := func(tag, typ uint16, count int, value []byte) {
			entries = append(entries, entry{tag, typ, uint32(count), value}) //nolint:gosec // G115: a test count
		}
		if p.Reduced {
			add(254, 4, 1, long(1))
		}
		add(256, 4, 1, long(b.Dx()))
		add(257, 4, 1, long(b.Dy()))
		bps, spp, photometric := 8, 1, 1
		var palette []byte
		switch img := p.Image.(type) {
		case *image.Gray:
			if p.Bilevel {
				bps = 1
			}
		case *image.Gray16:
			bps = 16
		case *image.Paletted:
			photometric = 3
			r, g, bl := make([]int, 0, 3*256), make([]int, 0, 256), make([]int, 0, 256)
			for i := range 256 {
				c := color.NRGBA64{}
				if i < len(img.Palette) {
					c = color.NRGBA64Model.Convert(img.Palette[i]).(color.NRGBA64) //nolint:forcetypeassert // NRGBA64Model returns color.NRGBA64
				}
				r, g, bl = append(r, int(c.R)), append(g, int(c.G)), append(bl, int(c.B))
			}
			palette = short(append(append(r, g...), bl...)...)
		case *image.NRGBA64:
			bps, spp, photometric = 16, 3, 2
		default:
			spp, photometric = 3, 2
			if p.Alpha {
				spp = 4
			}
		}
		bpsValues := make([]int, spp)
		for i := range bpsValues {
			bpsValues[i] = bps
		}
		add(258, 3, spp, short(bpsValues...))
		add(259, 3, 1, short(p.Compression))
		add(262, 3, 1, short(photometric))
		if p.Description != "" {
			add(270, 2, len(p.Description)+1, append([]byte(p.Description), 0))
		}
		stripOffsetsAt := len(entries)
		add(273, 4, len(strips), make([]byte, 4*len(strips)))
		if p.Orientation != 0 {
			add(274, 3, 1, short(p.Orientation))
		}
		add(277, 3, 1, short(spp))
		rows := p.RowsPerStrip
		if rows == 0 {
			rows = b.Dy()
		}
		add(278, 4, 1, long(rows))
		var counts []int
		for _, s := range strips {
			counts = append(counts, len(s))
		}
		add(279, 4, len(strips), long(counts...))
		if p.DPI != 0 {
			r := long(p.DPI, 1)
			add(282, 5, 1, r)
			add(283, 5, 1, r)
			add(296, 3, 1, short(2))
		}
		if palette != nil {
			add(320, 3, 3*256, palette)
		}
		if p.Alpha && spp == 4 {
			add(338, 3, 1, short(2))
		}
		if p.ICC != nil {
			add(34675, 7, len(p.ICC), p.ICC)
		}
		slices.SortStableFunc(entries, func(a, b entry) int { return int(a.tag) - int(b.tag) })
		for i, e := range entries {
			if e.tag == 273 {
				stripOffsetsAt = i
			}
		}
		if len(out)%2 == 1 {
			out = append(out, 0)
		}
		ifd := len(out)
		ifds = append(ifds, ifd)
		size := 2 + 12*len(entries) + 4
		extra := ifd + size
		var tail []byte
		block := le.AppendUint16(nil, uint16(len(entries))) //nolint:gosec // G115: a few entries
		valueAt := make([]int, len(entries))
		for i, e := range entries {
			block = le.AppendUint16(block, e.tag)
			block = le.AppendUint16(block, e.typ)
			block = le.AppendUint32(block, e.count)
			if len(e.value) <= 4 {
				v := append(slices.Clone(e.value), 0, 0, 0, 0)
				valueAt[i] = len(out) + len(block)
				block = append(block, v[:4]...)
				continue
			}
			valueAt[i] = extra + len(tail)
			block = le.AppendUint32(block, uint32(extra+len(tail))) //nolint:gosec // G115: a test file
			tail = append(tail, e.value...)
			if len(tail)%2 == 1 {
				tail = append(tail, 0)
			}
		}
		links = append(links, len(out)+len(block))
		block = append(block, 0, 0, 0, 0)
		out = append(append(out, block...), tail...)
		offsets := make([]int, len(strips))
		for i, s := range strips {
			offsets[i] = len(out)
			out = append(out, s...)
		}
		at := valueAt[stripOffsetsAt]
		if len(strips) == 1 {
			le.PutUint32(out[at:], uint32(offsets[0])) //nolint:gosec // G115: a test file
		} else {
			for i, o := range offsets {
				le.PutUint32(out[at+4*i:], uint32(o)) //nolint:gosec // G115: as above
			}
		}
	}
	binary.LittleEndian.PutUint32(out[4:], uint32(ifds[0])) //nolint:gosec // G115: as above
	for i := range len(links) - 1 {
		binary.LittleEndian.PutUint32(out[links[i]:], uint32(ifds[i+1])) //nolint:gosec // G115: as above
	}
	if next != nil {
		binary.LittleEndian.PutUint32(out[links[len(links)-1]:], uint32(next(ifds))) //nolint:gosec // G115: as above
	}
	return out
}

// tiffStrips returns the compressed strips of a page.
func tiffStrips(p TIFFPage) [][]byte {
	b := p.Image.Bounds()
	rows := p.RowsPerStrip
	if rows == 0 {
		rows = b.Dy()
	}
	var strips [][]byte
	for y0 := 0; y0 < b.Dy(); y0 += rows {
		var raw []byte
		for y := y0; y < min(y0+rows, b.Dy()); y++ {
			raw = append(raw, tiffRow(p, b.Min.Y+y)...)
		}
		strips = append(strips, compress(raw, p.Compression))
	}
	return strips
}

// tiffRow returns the samples of row y of a page.
func tiffRow(p TIFFPage, y int) []byte {
	b := p.Image.Bounds()
	var row []byte
	switch img := p.Image.(type) {
	case *image.Gray:
		if p.Bilevel {
			row = make([]byte, (b.Dx()+7)/8)
			for x := range b.Dx() {
				if img.GrayAt(b.Min.X+x, y).Y >= 0x80 {
					row[x/8] |= 0x80 >> (x % 8)
				}
			}
			return row
		}
		return slices.Clone(img.Pix[img.PixOffset(b.Min.X, y):img.PixOffset(b.Max.X, y)])
	case *image.Gray16:
		for x := range b.Dx() {
			row = binary.LittleEndian.AppendUint16(row, img.Gray16At(b.Min.X+x, y).Y)
		}
	case *image.Paletted:
		return slices.Clone(img.Pix[img.PixOffset(b.Min.X, y):img.PixOffset(b.Max.X, y)])
	case *image.NRGBA64:
		for x := range b.Dx() {
			c := img.NRGBA64At(b.Min.X+x, y)
			row = binary.LittleEndian.AppendUint16(row, c.R)
			row = binary.LittleEndian.AppendUint16(row, c.G)
			row = binary.LittleEndian.AppendUint16(row, c.B)
		}
	default:
		for x := range b.Dx() {
			c := color.NRGBAModel.Convert(p.Image.At(b.Min.X+x, y)).(color.NRGBA) //nolint:forcetypeassert // NRGBAModel returns color.NRGBA
			row = append(row, c.R, c.G, c.B)
			if p.Alpha {
				row = append(row, c.A)
			}
		}
	}
	return row
}

// compress compresses a strip.
func compress(raw []byte, method int) []byte {
	switch method {
	case TIFFDeflate:
		var z bytes.Buffer
		w := zlib.NewWriter(&z)
		_, _ = w.Write(raw) // writes to a buffer fail on no path
		_ = w.Close()       // as above
		return z.Bytes()
	case TIFFPackBits:
		return packBits(raw)
	case TIFFLZW:
		return tiffLZW(raw)
	}
	return raw
}

// packBits codes raw as PackBits (TIFF 6.0, section 9): runs of two or
// more equal bytes as repeats, the rest as literals.
func packBits(raw []byte) []byte {
	var out []byte
	for i := 0; i < len(raw); {
		run := 1
		for i+run < len(raw) && run < 128 && raw[i+run] == raw[i] {
			run++
		}
		if run >= 2 {
			out = append(out, byte(257-run), raw[i])
			i += run
			continue
		}
		j := i
		for j < len(raw) && j-i < 128 && (j+1 >= len(raw) || raw[j+1] != raw[j]) {
			j++
		}
		if j == i {
			j = i + 1
		}
		out = append(out, byte(j-i-1)) //nolint:gosec // G115: a literal run of 1 to 128
		out = append(out, raw[i:j]...)
		i = j
	}
	return out
}

// tiffLZW codes raw as TIFF LZW (TIFF 6.0, section 13): codes of 9 to 12
// bits, most significant bit first, the code width growing one code early,
// a Clear code first and when the table is full, EndOfInformation last.
func tiffLZW(raw []byte) []byte {
	const clearCode, eoi = 256, 257
	var out []byte
	var acc uint32
	var nbits uint
	width := uint(9)
	put := func(code int) {
		acc = acc<<width | uint32(code) //nolint:gosec // G115: a 12-bit code
		nbits += width
		for nbits >= 8 {
			out = append(out, byte(acc>>(nbits-8))) //nolint:gosec // G115: the eight bits just completed
			nbits -= 8
		}
	}
	table := map[string]int{}
	next := 258
	reset := func() {
		clear(table)
		next = 258
		width = 9
	}
	put(clearCode)
	reset()
	var cur []byte
	for _, c := range raw {
		cand := append(slices.Clone(cur), c)
		if len(cur) == 0 {
			cur = cand
			continue
		}
		if _, ok := table[string(cand)]; ok {
			cur = cand
			continue
		}
		put(lzwCode(table, cur))
		table[string(cand)] = next
		next++
		// The width grows when the next code to assign would not fit,
		// one code early (TIFF 6.0, section 13).
		if next+1 > 1<<width && width < 12 {
			width++
		}
		if next >= 4094 {
			put(clearCode)
			reset()
		}
		cur = []byte{c}
	}
	if len(cur) > 0 {
		put(lzwCode(table, cur))
		next++
		if next+1 > 1<<width && width < 12 {
			width++
		}
	}
	put(eoi)
	if nbits > 0 {
		out = append(out, byte(acc<<(8-nbits))) //nolint:gosec // G115: the bits left, fewer than eight
	}
	return out
}

// lzwCode returns the code of s: its byte for one byte, its table entry
// otherwise.
func lzwCode(table map[string]int, s []byte) int {
	if len(s) == 1 {
		return int(s[0])
	}
	return table[string(s)]
}
