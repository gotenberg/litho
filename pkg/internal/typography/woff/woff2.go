package woff

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/andybalholm/brotli"
)

// woff2HeaderSize is the size of the WOFF 2.0 header (WOFF 2.0, 4).
const woff2HeaderSize = 48

// knownTags are the tags a WOFF 2.0 table directory entry names by index,
// in the order of WOFF 2.0, 5.1, Table 2 (Known Table Tags); index 63 means
// the tag follows the flags.
var knownTags = [63]string{
	"cmap", "head", "hhea", "hmtx", "maxp", "name", "OS/2", "post",
	"cvt ", "fpgm", "glyf", "loca", "prep", "CFF ", "VORG", "EBDT",
	"EBLC", "gasp", "hdmx", "kern", "LTSH", "PCLT", "VDMX", "vhea",
	"vmtx", "BASE", "GDEF", "GPOS", "GSUB", "EBSC", "JSTF", "MATH",
	"CBDT", "CBLC", "COLR", "CPAL", "SVG ", "sbix", "acnt", "avar",
	"bdat", "bloc", "bsln", "cvar", "fdsc", "feat", "fmtx", "fvar",
	"gvar", "hsty", "just", "lcar", "mort", "morx", "opbd", "prop",
	"trak", "Zapf", "Silf", "Glat", "Gloc", "Feat", "Sill",
}

// Tags the transforms touch.
const (
	tagGlyf = 0x676C7966
	tagLoca = 0x6C6F6361
	tagHmtx = 0x686D7478
	tagHhea = 0x68686561
	tagMaxp = 0x6D617870
)

// flavorCollection is the flavor of a WOFF 2.0 collection: "ttcf".
const flavorCollection = 0x74746366

// woff2Entry is one WOFF 2.0 table directory entry (WOFF 2.0, 5.2).
type woff2Entry struct {
	tag uint32
	// transformed reports a table stored in its transformed form.
	transformed bool
	origLength  uint32
	// length is the bytes the entry takes in the decompressed stream:
	// its transformLength when transformed, its origLength otherwise.
	length uint32
	// offset is its position in the decompressed stream.
	offset int64
}

// reader reads big-endian fields from a byte slice, recording the first
// read past its end.
type reader struct {
	b   []byte
	pos int
	err error
}

// fail records err unless an error is already recorded.
func (r *reader) fail(err error) {
	if r.err == nil {
		r.err = err
	}
}

// bytes returns the next n bytes, nil past the end.
func (r *reader) bytes(n int) []byte {
	if r.err != nil {
		return nil
	}
	if n < 0 || n > len(r.b)-r.pos {
		r.fail(invalid("a read of %d bytes past the end of a %d-byte stream", n, len(r.b)))
		return nil
	}
	out := r.b[r.pos : r.pos+n]
	r.pos += n
	return out
}

func (r *reader) u8() uint8 {
	if b := r.bytes(1); b != nil {
		return b[0]
	}
	return 0
}

func (r *reader) u16() uint16 {
	if b := r.bytes(2); b != nil {
		return binary.BigEndian.Uint16(b)
	}
	return 0
}

func (r *reader) u32() uint32 {
	if b := r.bytes(4); b != nil {
		return binary.BigEndian.Uint32(b)
	}
	return 0
}

// base128 reads a UIntBase128 (WOFF 2.0, 6.1.1): at most five bytes, seven
// bits each, no leading zero byte, and no value past 2^32 - 1.
func (r *reader) base128() uint32 {
	var v uint32
	for i := range 5 {
		b := r.u8()
		if r.err != nil {
			return 0
		}
		if i == 0 && b == 0x80 {
			r.fail(invalid("UIntBase128 with a leading zero byte"))
			return 0
		}
		if v&0xFE000000 != 0 {
			r.fail(invalid("UIntBase128 past 32 bits"))
			return 0
		}
		v = v<<7 | uint32(b&0x7F)
		if b&0x80 == 0 {
			return v
		}
	}
	r.fail(invalid("UIntBase128 longer than five bytes"))
	return 0
}

// u255 reads a 255UInt16 (WOFF 2.0, 6.1.1).
func (r *reader) u255() uint16 {
	const (
		oneMoreByteCode1 = 255
		oneMoreByteCode2 = 254
		wordCode         = 253
		lowestUCode      = 253
	)
	switch code := r.u8(); code {
	case wordCode:
		return r.u16()
	case oneMoreByteCode1:
		return uint16(r.u8()) + lowestUCode
	case oneMoreByteCode2:
		return uint16(r.u8()) + lowestUCode*2
	default:
		return uint16(code)
	}
}

// decodeWOFF2 reconstructs the program of a WOFF 2.0 file (WOFF 2.0, 4 to
// 5): the table directory, the optional collection directory, then one
// Brotli stream holding every table in directory order.
func decodeWOFF2(data []byte) ([]byte, error) {
	if len(data) < woff2HeaderSize {
		return nil, invalid("a WOFF2 header of %d bytes, %d expected", len(data), woff2HeaderSize)
	}
	r := &reader{b: data, pos: 4}
	flavor := r.u32()
	length := r.u32()
	numTables := int(r.u16())
	if r.u16() != 0 {
		return nil, invalid("reserved header field set")
	}
	r.u32() // totalSfntSize: an estimate the decoder does not rely on.
	compressedSize := r.u32()
	r.pos = woff2HeaderSize
	if uint64(length) != uint64(len(data)) {
		return nil, invalid("header length %d for a file of %d bytes", length, len(data))
	}
	if numTables == 0 {
		return nil, invalid("no table")
	}
	if numTables > maxTables {
		return nil, fmt.Errorf("%w: %d tables, at most %d", ErrTooLarge, numTables, maxTables)
	}
	entries, err := readWOFF2Directory(r, numTables)
	if err != nil {
		return nil, err
	}
	pick := make([]int, numTables)
	for i := range pick {
		pick[i] = i
	}
	if flavor == flavorCollection {
		if flavor, pick, err = readCollectionFirstFont(r, numTables); err != nil {
			return nil, err
		}
	}
	if int64(r.pos)+int64(compressedSize) > int64(len(data)) {
		return nil, invalid("a compressed stream of %d bytes past the end of the file", compressedSize)
	}
	var streamLen int64
	for i := range entries {
		entries[i].offset = streamLen
		streamLen += int64(entries[i].length)
		if streamLen > MaxSfntBytes {
			return nil, fmt.Errorf("%w: a decompressed stream of more than %d bytes", ErrTooLarge, MaxSfntBytes)
		}
	}
	stream, err := unbrotli(data[r.pos:r.pos+int(compressedSize)], int(streamLen)) // fits int: compressedSize within the file, streamLen at most MaxSfntBytes
	if err != nil {
		return nil, err
	}
	return rebuildWOFF2(flavor, entries, pick, stream)
}

// readWOFF2Directory reads numTables entries of the table directory (WOFF
// 2.0, 5.2). The transform version of glyf and loca is 0 for the
// transformed form and 3 for the original one; that of every other table
// is 0 for the original form, and 1 marks a transformed hmtx. Any other
// version is refused.
func readWOFF2Directory(r *reader, numTables int) ([]woff2Entry, error) {
	entries := make([]woff2Entry, numTables)
	for i := range entries {
		flags := r.u8()
		var tag uint32
		if idx := flags & 0x3F; idx == 63 {
			tag = r.u32()
		} else {
			tag = binary.BigEndian.Uint32([]byte(knownTags[idx]))
		}
		version := flags >> 6
		e := woff2Entry{tag: tag, origLength: r.base128()}
		switch tag {
		case tagGlyf, tagLoca:
			e.transformed = version == 0
			if version != 0 && version != 3 {
				return nil, invalid("table %s with transform version %d", tagString(tag), version)
			}
		case tagHmtx:
			e.transformed = version == 1
			if version > 1 {
				return nil, invalid("table %s with transform version %d", tagString(tag), version)
			}
		default:
			if version != 0 {
				return nil, invalid("table %s with transform version %d", tagString(tag), version)
			}
		}
		e.length = e.origLength
		if e.transformed {
			e.length = r.base128()
			if tag == tagLoca && e.length != 0 {
				return nil, invalid("a transformed loca table of %d bytes, 0 expected", e.length)
			}
		}
		if r.err != nil {
			return nil, r.err
		}
		entries[i] = e
	}
	return entries, nil
}

// readCollectionFirstFont reads the collection directory (WOFF 2.0, 5.3)
// and returns the flavor of its first font and the indices of the table
// directory entries that font uses. The other fonts are skipped.
func readCollectionFirstFont(r *reader, numTables int) (uint32, []int, error) {
	version := r.u32()
	if version != 0x00010000 && version != 0x00020000 {
		return 0, nil, invalid("collection version %#x", version)
	}
	numFonts := int(r.u255())
	if numFonts == 0 {
		return 0, nil, invalid("a collection of no font")
	}
	var flavor uint32
	var first []int
	for f := range numFonts {
		n := int(r.u255())
		fl := r.u32()
		if r.err != nil {
			return 0, nil, r.err
		}
		if n > numTables {
			return 0, nil, invalid("collection font %d lists %d of %d tables", f, n, numTables)
		}
		idx := make([]int, n)
		for k := range idx {
			idx[k] = int(r.u255())
			if idx[k] >= numTables {
				return 0, nil, invalid("collection font %d names table %d of %d", f, idx[k], numTables)
			}
		}
		if f == 0 {
			flavor, first = fl, idx
		}
	}
	if r.err != nil {
		return 0, nil, r.err
	}
	if len(first) == 0 {
		return 0, nil, invalid("the first font of the collection has no table")
	}
	return flavor, first, nil
}

// unbrotli decompresses src, which must hold exactly n bytes (WOFF 2.0,
// 5.4: the stream's length is the sum of the table lengths).
func unbrotli(src []byte, n int) ([]byte, error) {
	out, err := readExactly(brotli.NewReader(bytes.NewReader(src)), n)
	if err != nil {
		return nil, invalid("Brotli stream: %v", err)
	}
	return out, nil
}

// rebuildWOFF2 returns the program of the entries pick names, whose bytes
// lie in stream. A transformed glyf table is rebuilt with its loca table,
// then a transformed hmtx from the glyphs' bounding boxes (WOFF 2.0, 5.1
// and 5.4). A transformed glyf or loca needs the other transformed too, and
// a transformed hmtx needs a transformed glyf.
func rebuildWOFF2(flavor uint32, entries []woff2Entry, pick []int, stream []byte) ([]byte, error) {
	byTag := make(map[uint32]woff2Entry, len(pick))
	for _, i := range pick {
		e := entries[i]
		if _, dup := byTag[e.tag]; dup {
			return nil, invalid("table %s listed twice", tagString(e.tag))
		}
		byTag[e.tag] = e
	}
	bytesOf := func(e woff2Entry) []byte {
		return stream[e.offset : e.offset+int64(e.length)]
	}
	glyf, hasGlyf := byTag[tagGlyf]
	loca, hasLoca := byTag[tagLoca]
	if hasGlyf != hasLoca || (hasGlyf && glyf.transformed != loca.transformed) {
		return nil, invalid("glyf and loca must both be present and transformed alike")
	}
	hmtx, hasHmtx := byTag[tagHmtx]
	if hasHmtx && hmtx.transformed && !glyf.transformed {
		return nil, invalid("a transformed hmtx table without a transformed glyf table")
	}

	tables := make([]table, 0, len(pick))
	var rebuilt *glyfResult
	if hasGlyf && glyf.transformed {
		var err error
		if rebuilt, err = reconstructGlyf(bytesOf(glyf)); err != nil {
			return nil, err
		}
		if int64(len(rebuilt.glyf)) > MaxSfntBytes {
			return nil, fmt.Errorf("%w: a glyf table of more than %d bytes", ErrTooLarge, MaxSfntBytes)
		}
	}
	for _, i := range pick {
		e := entries[i]
		var t table
		t.tag = e.tag
		switch {
		case e.tag == tagGlyf && rebuilt != nil:
			t.data = rebuilt.glyf
		case e.tag == tagLoca && rebuilt != nil:
			t.data = rebuilt.loca
		case e.tag == tagHmtx && e.transformed:
			hhea, okH := byTag[tagHhea]
			if !okH {
				return nil, invalid("a transformed hmtx table without an hhea table")
			}
			h := bytesOf(hhea)
			if len(h) < 36 {
				return nil, invalid("an hhea table of %d bytes", len(h))
			}
			data, err := reconstructHmtx(bytesOf(e), int(binary.BigEndian.Uint16(h[34:])), rebuilt.xMin)
			if err != nil {
				return nil, err
			}
			t.data = data
		case e.tag == tagHead:
			head := append([]byte(nil), bytesOf(e)...)
			if rebuilt != nil {
				if len(head) < 54 {
					return nil, invalid("a head table of %d bytes", len(head))
				}
				// The loca format the glyf transform chose is the one
				// head must declare (WOFF 2.0, 5.1).
				binary.BigEndian.PutUint16(head[50:], rebuilt.indexFormat)
			}
			t.data = head
		default:
			t.data = append([]byte(nil), bytesOf(e)...)
		}
		// The glyf encoding may differ from the original's, so its length
		// may too; the loca and hmtx lengths follow from the glyph count.
		if (e.tag == tagLoca || e.tag == tagHmtx) && e.transformed && int64(len(t.data)) != int64(e.origLength) {
			return nil, invalid("table %s rebuilt to %d bytes, %d declared", tagString(e.tag), len(t.data), e.origLength)
		}
		t.checksum = checksum(t.data)
		tables = append(tables, t)
	}
	if hasGlyf && rebuilt != nil {
		if maxp, ok := byTag[tagMaxp]; ok {
			m := bytesOf(maxp)
			if len(m) >= 6 && int(binary.BigEndian.Uint16(m[4:])) != rebuilt.numGlyphs {
				return nil, invalid("glyf rebuilt %d glyphs, maxp counts %d", rebuilt.numGlyphs, binary.BigEndian.Uint16(m[4:]))
			}
		}
	}
	return assemble(flavor, tables)
}
