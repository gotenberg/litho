package varfont

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf16"
)

// Name IDs the instance's PostScript name is built from (OpenType
// specification, 'name' table, Name IDs).
const (
	nameFamily        = 1
	namePostScript    = 6
	nameTypoFamily    = 16
	nameVariationsPre = 25
)

// 'name' table layout (OpenType specification, 'name' table): the header
// and one name record.
const (
	nameHeaderSize = 6
	nameRecordSize = 12
)

// maxPostScriptName is the longest PostScript name an instance gets: 63
// characters, the limit Adobe Technical Note #5902 keeps generated names
// within.
const maxPostScriptName = 63

// PostScriptName returns the PostScript name of the instance at the user
// coordinates user of a font whose design space is axes and whose 'name'
// table is name, after Adobe Technical Note #5902 (Generating PostScript
// names for fonts using OpenType font variations): the PostScript name of
// the named instance at those coordinates when the font gives it one, else
// a family prefix (name ID 25, else 16, else 1, keeping its ASCII letters
// and digits) followed by "-" and the named instance's subfamily name, or
// by "_" and each axis's value and tag in 'fvar' order for an unnamed
// position. A name longer than 63 characters is cut to the prefix and a
// hash of the whole.
func PostScriptName(axes *Axes, name []byte, user []float64) string {
	at := make([]float64, len(axes.Axes))
	for i, ax := range axes.Axes {
		at[i] = ax.Default
		if i < len(user) {
			at[i] = axes.Clamp(i, user[i])
		}
	}
	prefix := ""
	for _, id := range []uint16{nameVariationsPre, nameTypoFamily, nameFamily} {
		if prefix = alnum(nameString(name, id)); prefix != "" {
			break
		}
	}
	if prefix == "" {
		prefix = "Font"
	}
	for _, ni := range axes.Instances {
		if !sameCoords(ni.Coords, at) {
			continue
		}
		if ni.PostScriptNameID != noPostScriptName {
			if s := psChars(nameString(name, ni.PostScriptNameID)); s != "" {
				return limitName(prefix, s)
			}
		}
		if s := alnum(nameString(name, ni.SubfamilyNameID)); s != "" {
			return limitName(prefix, prefix+"-"+s)
		}
	}
	var b strings.Builder
	b.WriteString(prefix)
	for i, ax := range axes.Axes {
		b.WriteString("_")
		// Adding 0 turns a negative zero into 0.
		b.WriteString(strconv.FormatFloat(math.Round(at[i]*1000)/1000+0, 'f', -1, 64))
		b.WriteString(alnum(ax.Tag))
	}
	return limitName(prefix, b.String())
}

// sameCoords reports whether a and b hold the same coordinates.
func sameCoords(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// limitName returns s, or prefix and a hash of s when s is longer than
// [maxPostScriptName].
func limitName(prefix, s string) string {
	if len(s) <= maxPostScriptName {
		return s
	}
	sum := sha256.Sum256([]byte(s))
	h := strings.ToUpper(hex.EncodeToString(sum[:8]))
	return prefix[:min(len(prefix), maxPostScriptName-len(h)-1)] + "-" + h
}

// alnum keeps the ASCII letters and digits of s.
func alnum(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x80 && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return r
		}
		return -1
	}, s)
}

// psChars keeps the printable ASCII characters of s a PostScript name may
// hold: no space and none of [](){}<>/%.
func psChars(s string) string {
	return strings.Map(func(r rune) rune {
		if r <= ' ' || r > '~' || strings.ContainsRune("[](){}<>/%", r) {
			return -1
		}
		return r
	}, s)
}

// nameRecord is one record of a 'name' table.
type nameRecord struct {
	platform, encoding, language, id uint16
	value                            []byte
}

// parseName reads the records of a 'name' table of format 0 or 1, and the
// language-tag strings of format 1 as records of platform 0xFFFF.
func parseName(name []byte) (format uint16, records []nameRecord, err error) {
	if len(name) < nameHeaderSize {
		return 0, nil, fmt.Errorf("%w: name header", ErrInvalid)
	}
	format = binary.BigEndian.Uint16(name)
	count := int(binary.BigEndian.Uint16(name[2:]))
	storage := int(binary.BigEndian.Uint16(name[4:]))
	if format > 1 || nameHeaderSize+count*nameRecordSize > len(name) || storage > len(name) {
		return 0, nil, fmt.Errorf("%w: name records past the table", ErrInvalid)
	}
	str := func(off, n int) ([]byte, error) {
		if storage+off+n > len(name) {
			return nil, fmt.Errorf("%w: name string past the table", ErrInvalid)
		}
		return name[storage+off : storage+off+n], nil
	}
	records = make([]nameRecord, 0, count)
	for i := range count {
		r := name[nameHeaderSize+i*nameRecordSize:]
		v, err := str(int(binary.BigEndian.Uint16(r[10:])), int(binary.BigEndian.Uint16(r[8:])))
		if err != nil {
			return 0, nil, err
		}
		records = append(records, nameRecord{
			platform: binary.BigEndian.Uint16(r), encoding: binary.BigEndian.Uint16(r[2:]),
			language: binary.BigEndian.Uint16(r[4:]), id: binary.BigEndian.Uint16(r[6:]), value: v,
		})
	}
	if format == 1 {
		at := nameHeaderSize + count*nameRecordSize
		if at+2 > len(name) {
			return 0, nil, fmt.Errorf("%w: name language tags past the table", ErrInvalid)
		}
		tags := int(binary.BigEndian.Uint16(name[at:]))
		if at+2+4*tags > len(name) {
			return 0, nil, fmt.Errorf("%w: name language tags past the table", ErrInvalid)
		}
		for i := range tags {
			r := name[at+2+4*i:]
			v, err := str(int(binary.BigEndian.Uint16(r[2:])), int(binary.BigEndian.Uint16(r)))
			if err != nil {
				return 0, nil, err
			}
			records = append(records, nameRecord{platform: 0xFFFF, value: v})
		}
	}
	return format, records, nil
}

// nameString returns name ID id of a 'name' table: the Windows English
// (United States) string, else the first Windows, Unicode or Macintosh
// Roman one; "" when the table has none or does not read.
func nameString(name []byte, id uint16) string {
	_, records, err := parseName(name)
	if err != nil {
		return ""
	}
	best, rank := "", 0
	for _, r := range records {
		if r.id != id {
			continue
		}
		var s string
		var k int
		switch {
		case r.platform == 3 && (r.encoding == 1 || r.encoding == 10):
			s, k = utf16BE(r.value), 2
			if r.language == 0x0409 {
				k = 3
			}
		case r.platform == 0:
			s, k = utf16BE(r.value), 1
		case r.platform == 1 && r.encoding == 0:
			s, k = string(r.value), 1
		default:
			continue
		}
		if k > rank {
			best, rank = s, k
		}
	}
	return best
}

// utf16BE decodes big-endian UTF-16.
func utf16BE(b []byte) string {
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = binary.BigEndian.Uint16(b[2*i:])
	}
	return string(utf16.Decode(u))
}

// renamePostScript returns the 'name' table name with every PostScript
// name record (name ID 6) holding ps, an ASCII string, in its record's
// encoding: UTF-16BE on the Unicode and Windows platforms, bytes on the
// Macintosh one. Other records keep their strings.
func renamePostScript(name []byte, ps string) ([]byte, error) {
	format, records, err := parseName(name)
	if err != nil {
		return nil, err
	}
	var storage []byte
	var head, langs []byte
	nameCount := 0
	for _, r := range records {
		v := r.value
		if r.id == namePostScript && r.platform != 0xFFFF {
			if r.platform == 1 {
				v = []byte(ps)
			} else {
				v = make([]byte, 0, 2*len(ps))
				for _, c := range ps {
					v = binary.BigEndian.AppendUint16(v, uint16(c)) //nolint:gosec // ps is ASCII
				}
			}
		}
		if len(storage)+len(v) > math.MaxUint16 {
			return nil, fmt.Errorf("%w: name strings past 64 KiB", ErrInvalid)
		}
		if r.platform == 0xFFFF {
			langs = binary.BigEndian.AppendUint16(langs, uint16(len(v)))       //nolint:gosec // bounded by the check above
			langs = binary.BigEndian.AppendUint16(langs, uint16(len(storage))) //nolint:gosec // as above
		} else {
			nameCount++
			for _, f := range []uint16{r.platform, r.encoding, r.language, r.id, uint16(len(v)), uint16(len(storage))} { //nolint:gosec // as above
				head = binary.BigEndian.AppendUint16(head, f)
			}
		}
		storage = append(storage, v...)
	}
	size := nameHeaderSize + len(head)
	if format == 1 {
		size += 2 + len(langs)
	}
	if size > math.MaxUint16 {
		return nil, fmt.Errorf("%w: name records past 64 KiB", ErrInvalid)
	}
	out := binary.BigEndian.AppendUint16(nil, format)
	out = binary.BigEndian.AppendUint16(out, uint16(nameCount)) //nolint:gosec // as many as the source's uint16 count
	out = binary.BigEndian.AppendUint16(out, uint16(size))      //nolint:gosec // bounded by the check above
	out = append(out, head...)
	if format == 1 {
		out = binary.BigEndian.AppendUint16(out, uint16(len(langs)/4)) //nolint:gosec // as many as the source's uint16 count
		out = append(out, langs...)
	}
	return append(out, storage...), nil
}
