package truetype

import (
	"cmp"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"unicode/utf16"
)

// Outline names the glyph outline format an sfnt program carries.
type Outline int

const (
	// OutlineNone marks a program holding neither a glyf nor a CFF table.
	OutlineNone Outline = iota
	// OutlineTrueType marks quadratic outlines in the glyf table.
	OutlineTrueType
	// OutlineCFF marks cubic outlines in the CFF table (an OpenType font
	// with PostScript outlines).
	OutlineCFF
)

// Embedding permission bits of the OS/2 fsType field (OpenType specification,
// OS/2 table, fsType).
const (
	// FSTypeRestricted is the Restricted License embedding bit: the font
	// must not be embedded.
	FSTypeRestricted = 0x0002
	// FSTypePreviewPrint is the Preview & Print embedding bit.
	FSTypePreviewPrint = 0x0004
	// FSTypeEditable is the Editable embedding bit.
	FSTypeEditable = 0x0008
	// FSTypeBitmapOnly is the Bitmap embedding only bit: only the bitmaps
	// the font carries may be embedded, never its outlines.
	FSTypeBitmapOnly = 0x0200
)

// Name table identifiers (OpenType specification, name table, Name IDs).
const (
	nameIDFamily            = 1
	nameIDSubfamily         = 2
	nameIDFullName          = 4
	nameIDPostScript        = 6
	nameIDTypographicFamily = 16
	nameIDTypographicSub    = 17
)

// maxNameRecordLength bounds the bytes one name record may claim. Anti-DoS
// guard, not spec-mandated: the name table stores a string's length in a
// uint16, and the names read here are PostScript names (63 characters at most,
// Adobe Technical Note #5902) and family names, so a record past this length
// is not one of them.
const maxNameRecordLength = 1024

// Info is what the header tables of an sfnt font program (TrueType or
// OpenType) state about it: its names, its metrics, its embedding permissions
// and the outline format it carries.
type Info struct {
	// PostScriptName is name ID 6.
	PostScriptName string
	// Family is the typographic family (name ID 16), or the family (name ID
	// 1) when the program states none.
	Family string
	// Subfamily is the typographic subfamily (name ID 17), or the subfamily
	// (name ID 2) when the program states none.
	Subfamily string
	// FullName is name ID 4.
	FullName string
	// UnitsPerEm is the head table's unitsPerEm.
	UnitsPerEm uint16
	// BBox is the head table's xMin, yMin, xMax and yMax, in font units.
	BBox [4]int16
	// MacStyle is the head table's macStyle (bit 0 bold, bit 1 italic).
	MacStyle uint16
	// Ascender and Descender are the OS/2 table's sTypoAscender and
	// sTypoDescender, or the hhea table's ascender and descender when the
	// program carries no OS/2 table.
	Ascender, Descender int16
	// CapHeight is the OS/2 table's sCapHeight (version 2 and later), and 0
	// when the program does not state it.
	CapHeight int16
	// WeightClass is the OS/2 table's usWeightClass, 0 without the table.
	WeightClass uint16
	// FSType is the OS/2 table's fsType, the font's embedding permissions.
	FSType uint16
	// HasOS2 reports whether the program carries an OS/2 table.
	HasOS2 bool
	// ItalicAngle is the post table's italicAngle, in degrees.
	ItalicAngle float64
	// FixedPitch reports the post table's isFixedPitch.
	FixedPitch bool
	// Outline is the outline format the program carries.
	Outline Outline
}

// EmbeddingRestricted reports whether the program's fsType forbids embedding
// its outlines. Per the OpenType specification (OS/2 table, fsType) the
// permission bits are exclusive, and a font setting several grants the least
// restrictive, so the Restricted License bit bars embedding only when neither
// the Preview & Print nor the Editable bit is set. The Bitmap embedding only
// bit bars embedding the outlines whatever the other bits say.
func (i Info) EmbeddingRestricted() bool {
	if i.FSType&FSTypeBitmapOnly != 0 {
		return true
	}
	return i.FSType&FSTypeRestricted != 0 && i.FSType&(FSTypePreviewPrint|FSTypeEditable) == 0
}

// IsSfnt reports whether data starts with an sfnt version tag: TrueType
// (0x00010000 or "true") or OpenType with CFF outlines ("OTTO").
func IsSfnt(data []byte) bool {
	if len(data) < 4 {
		return false
	}
	switch string(data[:4]) {
	case "\x00\x01\x00\x00", "true", "OTTO":
		return true
	}
	return false
}

// ReadInfo reads the head, hhea, OS/2, post and name tables of an sfnt font
// program. The head table is required (it holds the unitsPerEm every advance
// width is scaled by); the others are read when present.
func ReadInfo(data []byte) (Info, error) {
	if !IsSfnt(data) {
		return Info{}, fmt.Errorf("%w: not an sfnt program", ErrInvalidFont)
	}
	records, err := ParseTableDirectory(data)
	if err != nil {
		return Info{}, err
	}
	var info Info

	head, err := TableData(data, records, "head")
	if err != nil {
		return Info{}, err
	}
	// The head table is 54 bytes (OpenType specification, head table).
	if len(head) < 54 {
		return Info{}, fmt.Errorf("%w: head table too short", ErrInvalidFont)
	}
	info.UnitsPerEm = binary.BigEndian.Uint16(head[18:20])
	for k := range 4 {
		info.BBox[k] = int16(binary.BigEndian.Uint16(head[36+2*k:])) //nolint:gosec // G115: the head bounding box is stored as int16.
	}
	info.MacStyle = binary.BigEndian.Uint16(head[44:46])

	if hhea, err := TableData(data, records, "hhea"); err == nil && len(hhea) >= 8 {
		info.Ascender = int16(binary.BigEndian.Uint16(hhea[4:6]))  //nolint:gosec // G115: hhea ascender is stored as int16.
		info.Descender = int16(binary.BigEndian.Uint16(hhea[6:8])) //nolint:gosec // G115: hhea descender is stored as int16.
	}
	if os2, err := TableData(data, records, "OS/2"); err == nil && len(os2) >= 10 {
		info.HasOS2 = true
		info.WeightClass = binary.BigEndian.Uint16(os2[4:6])
		info.FSType = binary.BigEndian.Uint16(os2[8:10])
		// sTypoAscender and sTypoDescender sit at 68 and 70 in every version.
		if len(os2) >= 72 {
			info.Ascender = int16(binary.BigEndian.Uint16(os2[68:70]))  //nolint:gosec // G115: sTypoAscender is stored as int16.
			info.Descender = int16(binary.BigEndian.Uint16(os2[70:72])) //nolint:gosec // G115: sTypoDescender is stored as int16.
		}
		// sCapHeight arrived with version 2, at offset 88.
		if version := binary.BigEndian.Uint16(os2[0:2]); version >= 2 && len(os2) >= 90 {
			info.CapHeight = int16(binary.BigEndian.Uint16(os2[88:90])) //nolint:gosec // G115: sCapHeight is stored as int16.
		}
	}
	if post, err := TableData(data, records, "post"); err == nil && len(post) >= 16 {
		// italicAngle is a 16.16 Fixed.
		info.ItalicAngle = float64(int32(binary.BigEndian.Uint32(post[4:8]))) / 65536 //nolint:gosec // G115: italicAngle is a signed 16.16 Fixed.
		info.FixedPitch = binary.BigEndian.Uint32(post[12:16]) != 0
	}

	switch {
	case hasTable(records, "glyf"):
		info.Outline = OutlineTrueType
	case hasTable(records, "CFF "):
		info.Outline = OutlineCFF
	}

	if name, err := TableData(data, records, "name"); err == nil {
		names := readNames(name)
		info.PostScriptName = names[nameIDPostScript]
		info.Family = cmp.Or(names[nameIDTypographicFamily], names[nameIDFamily])
		info.Subfamily = cmp.Or(names[nameIDTypographicSub], names[nameIDSubfamily])
		info.FullName = names[nameIDFullName]
	} else if !errors.Is(err, ErrTableNotFound) {
		return Info{}, err
	}
	return info, nil
}

// hasTable reports whether the directory lists tag.
func hasTable(records []TableRecord, tag string) bool {
	for _, r := range records {
		if r.Tag == tag {
			return true
		}
	}
	return false
}

// readNames returns the strings of the name table's family, subfamily, full
// and PostScript name records, keyed by name ID. A Windows Unicode record
// (platform 3, encoding 0 or 1) in US English is preferred, then any Windows
// Unicode record, then a Macintosh Roman one (platform 1, encoding 0), read as
// ASCII (OpenType specification, name table).
func readNames(table []byte) map[uint16]string {
	out := make(map[uint16]string)
	if len(table) < 6 {
		return out
	}
	count := int(binary.BigEndian.Uint16(table[2:4]))
	storage := int(binary.BigEndian.Uint16(table[4:6]))
	// A record is 12 bytes, so the table's length bounds how many it holds.
	count = min(count, (len(table)-6)/12)
	rank := make(map[uint16]int)
	for i := range count {
		rec := table[6+12*i : 18+12*i]
		platform := binary.BigEndian.Uint16(rec[0:2])
		encoding := binary.BigEndian.Uint16(rec[2:4])
		language := binary.BigEndian.Uint16(rec[4:6])
		nameID := binary.BigEndian.Uint16(rec[6:8])
		length := int(binary.BigEndian.Uint16(rec[8:10]))
		offset := int(binary.BigEndian.Uint16(rec[10:12]))
		switch nameID {
		case nameIDFamily, nameIDSubfamily, nameIDFullName, nameIDPostScript, nameIDTypographicFamily, nameIDTypographicSub:
		default:
			continue
		}
		start := storage + offset
		if length == 0 || length > maxNameRecordLength || start+length > len(table) {
			continue
		}
		raw := table[start : start+length]
		var r int
		var s string
		switch {
		case platform == 3 && (encoding == 0 || encoding == 1):
			r = 2
			if language == 0x0409 {
				r = 3
			}
			s = decodeUTF16BE(raw)
		case platform == 0:
			r = 2
			s = decodeUTF16BE(raw)
		case platform == 1 && encoding == 0:
			r = 1
			s = asciiOnly(raw)
		default:
			continue
		}
		s = strings.TrimSpace(s)
		if s == "" || r <= rank[nameID] {
			continue
		}
		rank[nameID] = r
		out[nameID] = s
	}
	return out
}

// decodeUTF16BE decodes big-endian UTF-16, dropping a trailing odd byte.
func decodeUTF16BE(b []byte) string {
	units := make([]uint16, len(b)/2)
	for i := range units {
		units[i] = binary.BigEndian.Uint16(b[2*i:])
	}
	return string(utf16.Decode(units))
}

// asciiOnly keeps the printable ASCII bytes of a Macintosh Roman string, which
// is all a PostScript or family name holds.
func asciiOnly(b []byte) string {
	var sb strings.Builder
	for _, c := range b {
		if c >= 0x20 && c < 0x7F {
			sb.WriteByte(c)
		}
	}
	return sb.String()
}
