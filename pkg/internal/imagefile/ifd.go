package imagefile

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// The TIFF tags this package reads (TIFF 6.0, section 8 and Appendix A;
// EXIF 2.32, 4.6.4, Table 4, which reuses them in IFD0).
const (
	tagImageWidth       = 256
	tagImageLength      = 257
	tagBitsPerSample    = 258
	tagCompression      = 259
	tagPhotometric      = 262
	tagImageDescription = 270
	tagStripOffsets     = 273
	tagOrientation      = 274
	tagSamplesPerPixel  = 277
	tagRowsPerStrip     = 278
	tagStripByteCounts  = 279
	tagXResolution      = 282
	tagYResolution      = 283
	tagResolutionUnit   = 296
	tagTileWidth        = 322
	tagTileLength       = 323
	tagInkSet           = 332
	tagICCProfile       = 34675
)

// The field types of an IFD entry (TIFF 6.0, section 2) and their sizes.
const (
	typeByte      = 1
	typeASCII     = 2
	typeShort     = 3
	typeLong      = 4
	typeRational  = 5
	typeUndefined = 7
)

// typeSize returns the bytes of one value of field type t, 0 for a type
// this package does not read.
func typeSize(t uint16) int64 {
	switch t {
	case typeByte, typeASCII, typeUndefined:
		return 1
	case typeShort:
		return 2
	case typeLong:
		return 4
	case typeRational:
		return 8
	}
	return 0
}

// errIFD reports a TIFF structure whose header, IFD or entries do not lie
// within its bytes.
var errIFD = errors.New("malformed TIFF structure")

// tiffData is a TIFF structure (TIFF 6.0, section 2): a TIFF file, or the
// EXIF segment of another format (EXIF 2.32, 4.5.2).
type tiffData struct {
	data []byte
	bo   binary.ByteOrder
	// first is the offset of IFD0.
	first int64
}

// readTIFFHeader reads the header of the TIFF structure data.
func readTIFFHeader(data []byte) (*tiffData, error) {
	if len(data) < 8 {
		return nil, errIFD
	}
	var bo binary.ByteOrder
	switch string(data[:4]) {
	case "II*\x00":
		bo = binary.LittleEndian
	case "MM\x00*":
		bo = binary.BigEndian
	default:
		return nil, errIFD
	}
	return &tiffData{data: data, bo: bo, first: int64(bo.Uint32(data[4:]))}, nil
}

// ifdEntry is one entry of an IFD, its values read where they lie.
type ifdEntry struct {
	tag, typ uint16
	count    int64
	// values holds the entry's count values, at most what fits the
	// structure's bytes.
	values []byte
}

// ifd reads the IFD at off: its entries and the offset of the next IFD, 0
// for none. An entry whose values lie outside the structure, or of a type
// this package does not read, is left out; an IFD that does not lie in
// the structure fails with errIFD.
func (t *tiffData) ifd(off int64) ([]ifdEntry, int64, error) {
	n := int64(len(t.data))
	if off < 8 || off+2 > n {
		return nil, 0, fmt.Errorf("%w: IFD at %d of %d bytes", errIFD, off, n)
	}
	count := int64(t.bo.Uint16(t.data[off:]))
	end := off + 2 + 12*count
	if end+4 > n {
		return nil, 0, fmt.Errorf("%w: IFD of %d entries at %d of %d bytes", errIFD, count, off, n)
	}
	entries := make([]ifdEntry, 0, count)
	for i := range count {
		e := t.data[off+2+12*i:]
		typ := t.bo.Uint16(e[2:])
		size := typeSize(typ)
		if size == 0 {
			continue
		}
		c := int64(t.bo.Uint32(e[4:]))
		total := c * size // at most 2^32 * 8: no overflow on int64
		var values []byte
		if total <= 4 {
			values = e[8 : 8+total]
		} else {
			vo := int64(t.bo.Uint32(e[8:]))
			if vo+total > n {
				continue
			}
			values = t.data[vo : vo+total]
		}
		entries = append(entries, ifdEntry{tag: t.bo.Uint16(e), typ: typ, count: c, values: values})
	}
	return entries, int64(t.bo.Uint32(t.data[end:])), nil
}

// find returns the entry of tag in entries.
func find(entries []ifdEntry, tag uint16) (ifdEntry, bool) {
	for _, e := range entries {
		if e.tag == tag {
			return e, true
		}
	}
	return ifdEntry{}, false
}

// uint returns the first value of a BYTE, SHORT or LONG entry.
func (t *tiffData) uint(e ifdEntry) (uint32, bool) {
	if e.count < 1 {
		return 0, false
	}
	switch e.typ {
	case typeByte:
		return uint32(e.values[0]), true
	case typeShort:
		return uint32(t.bo.Uint16(e.values)), true
	case typeLong:
		return t.bo.Uint32(e.values), true
	}
	return 0, false
}

// rational returns the first value of a RATIONAL entry, false for another
// type or a zero denominator.
func (t *tiffData) rational(e ifdEntry) (float64, bool) {
	if e.typ != typeRational || e.count < 1 {
		return 0, false
	}
	num, den := t.bo.Uint32(e.values), t.bo.Uint32(e.values[4:])
	if den == 0 {
		return 0, false
	}
	return float64(num) / float64(den), true
}

// maxDescriptionBytes caps the text read as a picture's title or
// description. Anti-DoS guard, not spec-mandated: the title of a
// document's Info dictionary and a figure's /Alt are read by people, and
// a caption runs to a few hundred characters.
const maxDescriptionBytes = 4096

// text returns an ASCII entry's value up to its first NUL, white space
// trimmed, "" for another type or text that is not UTF-8. EXIF 2.32,
// 4.6.2 makes ASCII 7-bit; a producer that writes UTF-8 there is read as
// written.
func (t *tiffData) text(e ifdEntry) string {
	if e.typ != typeASCII {
		return ""
	}
	v := e.values
	if i := indexByte(v, 0); i >= 0 {
		v = v[:i]
	}
	return cleanText(v)
}

// indexByte returns the index of the first c in b, -1 for none.
func indexByte(b []byte, c byte) int {
	for i, x := range b {
		if x == c {
			return i
		}
	}
	return -1
}

// cleanText returns b as a single line of text: white space runs collapsed
// to one space, trimmed, cut at [maxDescriptionBytes] on a character
// boundary, "" for bytes that are not UTF-8.
func cleanText(b []byte) string {
	if len(b) > maxDescriptionBytes {
		b = b[:maxDescriptionBytes]
		for len(b) > 0 && !utf8.Valid(b) {
			b = b[:len(b)-1]
		}
	}
	if !utf8.Valid(b) {
		return ""
	}
	return strings.Join(strings.Fields(string(b)), " ")
}

// EXIF is what the IFD0 of an EXIF segment, or of a TIFF file, states of
// its picture (EXIF 2.32, 4.6.4, Table 4; TIFF 6.0, section 8).
type EXIF struct {
	// Orientation is the Orientation tag, 1 to 8, 1 when it is absent or
	// out of range. The value means, for the stored rows and columns: 1
	// as stored, 2 mirrored left to right, 3 turned a half, 4 mirrored top
	// to bottom, 5 transposed, 6 turned a quarter clockwise, 7 transversed,
	// 8 turned a quarter counter-clockwise.
	Orientation int
	// XDPI and YDPI are the XResolution and YResolution in pixels an
	// inch, by the ResolutionUnit (2, the default, an inch; 3 a
	// centimetre); both 0 when the file states none, states unit 1 (no
	// absolute unit) or states a value [Resolution] refuses.
	XDPI, YDPI float64
	// Title is the ImageDescription tag, which EXIF 2.32, 4.6.5 defines as
	// "the title of the image", "" for none.
	Title string
}

// readIFD0 reads what IFD0 of the TIFF structure t states.
func (t *tiffData) readIFD0(entries []ifdEntry) EXIF {
	x := EXIF{Orientation: 1}
	if e, ok := find(entries, tagOrientation); ok {
		if v, ok := t.uint(e); ok && v >= 1 && v <= 8 {
			x.Orientation = int(v) // fits int: 1 to 8
		}
	}
	unit := uint32(2)
	if e, ok := find(entries, tagResolutionUnit); ok {
		if v, ok := t.uint(e); ok {
			unit = v
		}
	}
	xe, okx := find(entries, tagXResolution)
	ye, oky := find(entries, tagYResolution)
	if okx && oky && (unit == 2 || unit == 3) {
		xr, ok1 := t.rational(xe)
		yr, ok2 := t.rational(ye)
		if ok1 && ok2 {
			if unit == 3 {
				xr, yr = xr*2.54, yr*2.54
			}
			x.XDPI, x.YDPI = Resolution(xr, yr)
		}
	}
	if e, ok := find(entries, tagImageDescription); ok {
		x.Title = t.text(e)
	}
	return x
}

// ReadEXIF reads IFD0 of the TIFF structure an EXIF segment holds (EXIF
// 2.32, 4.5.2). A structure that does not read gives the zero
// orientation's default, 1, and nothing else.
func ReadEXIF(data []byte) EXIF {
	t, err := readTIFFHeader(data)
	if err != nil {
		return EXIF{Orientation: 1}
	}
	entries, _, err := t.ifd(t.first)
	if err != nil {
		return EXIF{Orientation: 1}
	}
	return t.readIFD0(entries)
}

// The resolutions a file may state, in pixels an inch. A value outside
// them states nonsense: a page an inch wide holding a million pixels or a
// pixel an inch wide. litho then reads the picture at 96 pixels an inch.
const (
	minDPI = 1
	maxDPI = 100_000
)

// DefaultDPI is the resolution of a picture whose file states none, or
// states one [Resolution] refuses: 96 pixels an inch, a CSS pixel (CSS
// Values 4, 6.2).
const DefaultDPI = 96

// Resolution returns x and y, a resolution in pixels an inch, when both
// lie between 1 and 100,000, and 0, 0 otherwise.
func Resolution(x, y float64) (float64, float64) {
	if !(x >= minDPI && x <= maxDPI && y >= minDPI && y <= maxDPI) {
		return 0, 0
	}
	return x, y
}
