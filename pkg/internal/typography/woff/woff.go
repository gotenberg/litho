package woff

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"slices"
)

// MaxSfntBytes caps the program a decoder reconstructs: the sum of its
// tables, each padded to four bytes, plus its table directory. Anti-DoS
// guard, not spec-mandated: a compressed file of a few kilobytes may claim
// a program of gigabytes, and the cap is checked before any table is
// inflated. The largest web fonts in use, pan-CJK families, reconstruct to
// 20 to 30 MiB, and the cap matches the bound litho's font index applies to
// one font program.
const MaxSfntBytes = 64 << 20

// maxTables caps the tables of one font. Anti-DoS guard, not
// spec-mandated: the table count is a 16-bit field, real fonts carry 10 to
// 40 tables, and every table costs a directory entry and a decode.
const maxTables = 256

// maxGlyphPoints caps the points of one reconstructed simple glyph. The
// format bounds it: a TrueType glyph stores its last point index in a
// 16-bit endPtsOfContours entry (ISO/IEC 14496-22, glyf table), so a glyph
// holds at most 65536 points, and a transformed glyph claiming more is
// refused.
const maxGlyphPoints = 1 << 16

// Kind is the container format of a font file.
type Kind uint8

// The container formats [Sniff] tells apart.
const (
	// Other is anything else: an sfnt program, a collection, or bytes
	// that are no font.
	Other Kind = iota
	// WOFF is WOFF 1.0.
	WOFF
	// WOFF2 is WOFF 2.0.
	WOFF2
)

// Signatures of the two formats (WOFF 1.0, 3; WOFF 2.0, 4).
const (
	signatureWOFF  = 0x774F4646 // "wOFF"
	signatureWOFF2 = 0x774F4632 // "wOF2"
)

// Sniff returns the container format data starts with, by its signature.
func Sniff(data []byte) Kind {
	if len(data) < 4 {
		return Other
	}
	switch binary.BigEndian.Uint32(data) {
	case signatureWOFF:
		return WOFF
	case signatureWOFF2:
		return WOFF2
	}
	return Other
}

// Decode returns the sfnt program the WOFF 1.0 or WOFF 2.0 file data wraps,
// a new slice that does not alias data.
//
// The program holds every table of the file, in ascending tag order, each
// padded to four bytes, with the table directory's search fields and the
// table checksums of the original. A transformed WOFF 2.0 glyf table is
// rebuilt as a glyf and a loca table, a transformed hmtx table as an hmtx
// table; the glyph outlines and metrics are the original's, their byte
// encoding may differ. For a WOFF 2.0 collection, the program is its first
// font.
//
// Decode returns an error wrapping [ErrNotWOFF] for data of another format,
// [ErrInvalid] for a malformed file and [ErrTooLarge] for a file past
// [MaxSfntBytes] or the table bound.
func Decode(data []byte) ([]byte, error) {
	switch Sniff(data) {
	case WOFF:
		return decodeWOFF(data)
	case WOFF2:
		return decodeWOFF2(data)
	case Other:
	}
	return nil, ErrNotWOFF
}

// table is one table of a program being reconstructed.
type table struct {
	tag      uint32
	checksum uint32
	data     []byte
}

// invalid returns an error wrapping [ErrInvalid].
func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

// tagString renders a table tag for an error message.
func tagString(tag uint32) string {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], tag)
	return fmt.Sprintf("%q", b[:])
}

// paddedLen returns n rounded up to a multiple of four.
func paddedLen(n int64) int64 {
	return (n + 3) &^ 3
}

// sfntSize returns the size of a program of n tables whose lengths sum,
// padded, to body bytes.
func sfntSize(n int, body int64) int64 {
	return 12 + 16*int64(n) + body
}

// checksum returns the table checksum of data (ISO/IEC 14496-22, 5.2,
// Calculating checksums): the sum of its big-endian uint32 words, the last
// one padded with zeros.
func checksum(data []byte) uint32 {
	var sum uint32
	n := len(data) &^ 3
	for i := 0; i < n; i += 4 {
		sum += binary.BigEndian.Uint32(data[i:])
	}
	if rest := data[n:]; len(rest) > 0 {
		var w [4]byte
		copy(w[:], rest)
		sum += binary.BigEndian.Uint32(w[:])
	}
	return sum
}

// tagHead is the head table's tag.
const tagHead = 0x68656164

// assemble writes the sfnt program of flavor holding tables: the offset
// table, the directory in ascending tag order and the tables padded to
// four bytes (ISO/IEC 14496-22, 5.1, Table directory). A table listed twice
// is refused. When the head table is present, its checkSumAdjustment is
// recomputed over the program written.
func assemble(flavor uint32, tables []table) ([]byte, error) {
	slices.SortFunc(tables, func(a, b table) int {
		switch {
		case a.tag < b.tag:
			return -1
		case a.tag > b.tag:
			return 1
		}
		return 0
	})
	var body int64
	for i, t := range tables {
		if i > 0 && tables[i-1].tag == t.tag {
			return nil, invalid("table %s listed twice", tagString(t.tag))
		}
		body += paddedLen(int64(len(t.data)))
	}
	size := sfntSize(len(tables), body)
	if size > MaxSfntBytes {
		return nil, fmt.Errorf("%w: a program of %d bytes, at most %d", ErrTooLarge, size, MaxSfntBytes)
	}
	out := make([]byte, size)
	n := len(tables)
	entrySelector := 0
	for 1<<(entrySelector+1) <= n {
		entrySelector++
	}
	searchRange := 16 << entrySelector
	binary.BigEndian.PutUint32(out[0:], flavor)
	binary.BigEndian.PutUint16(out[4:], uint16(n))                 //nolint:gosec // n is at most maxTables
	binary.BigEndian.PutUint16(out[6:], uint16(searchRange))       //nolint:gosec // at most 16 times maxTables
	binary.BigEndian.PutUint16(out[8:], uint16(entrySelector))     //nolint:gosec // at most log2 of maxTables
	binary.BigEndian.PutUint16(out[10:], uint16(16*n-searchRange)) //nolint:gosec // at most 16 times maxTables
	off := sfntSize(n, 0)
	headOff := int64(-1)
	for i, t := range tables {
		rec := out[12+16*i:]
		binary.BigEndian.PutUint32(rec[0:], t.tag)
		binary.BigEndian.PutUint32(rec[4:], t.checksum)
		binary.BigEndian.PutUint32(rec[8:], uint32(off))          //nolint:gosec // off is below MaxSfntBytes
		binary.BigEndian.PutUint32(rec[12:], uint32(len(t.data))) //nolint:gosec // every table is below MaxSfntBytes
		copy(out[off:], t.data)
		if t.tag == tagHead && len(t.data) >= 12 {
			headOff = off
		}
		off += paddedLen(int64(len(t.data)))
	}
	if headOff >= 0 {
		// ISO/IEC 14496-22, head table: checkSumAdjustment is
		// 0xB1B0AFBA minus the checksum of the whole font, computed with
		// the field itself set to 0.
		binary.BigEndian.PutUint32(out[headOff+8:], 0)
		binary.BigEndian.PutUint32(out[headOff+8:], 0xB1B0AFBA-checksum(out))
	}
	return out, nil
}

// WOFF 1.0 layout (WOFF 1.0, 4 and 5): a 44-byte header, then one 20-byte
// entry per table.
const (
	woffHeaderSize = 44
	woffEntrySize  = 20
)

// decodeWOFF reconstructs the program of a WOFF 1.0 file (WOFF 1.0, 4 to
// 6): each table is stored as is when its compressed length equals its
// original length and as a zlib stream when it is shorter.
func decodeWOFF(data []byte) ([]byte, error) {
	if len(data) < woffHeaderSize {
		return nil, invalid("a WOFF header of %d bytes, %d expected", len(data), woffHeaderSize)
	}
	be := binary.BigEndian
	flavor := be.Uint32(data[4:])
	length := be.Uint32(data[8:])
	numTables := int(be.Uint16(data[12:]))
	if be.Uint16(data[14:]) != 0 {
		return nil, invalid("reserved header field set")
	}
	if uint64(length) != uint64(len(data)) {
		return nil, invalid("header length %d for a file of %d bytes", length, len(data))
	}
	if numTables == 0 {
		return nil, invalid("no table")
	}
	if numTables > maxTables {
		return nil, fmt.Errorf("%w: %d tables, at most %d", ErrTooLarge, numTables, maxTables)
	}
	dirEnd := woffHeaderSize + woffEntrySize*numTables
	if dirEnd > len(data) {
		return nil, invalid("a table directory of %d tables past the end of the file", numTables)
	}
	type entry struct {
		tag, offset, compLength, origLength, checksum uint32
	}
	entries := make([]entry, numTables)
	var body int64
	for i := range entries {
		rec := data[woffHeaderSize+woffEntrySize*i:]
		e := entry{tag: be.Uint32(rec), offset: be.Uint32(rec[4:]), compLength: be.Uint32(rec[8:]), origLength: be.Uint32(rec[12:]), checksum: be.Uint32(rec[16:])}
		if uint64(e.offset)+uint64(e.compLength) > uint64(len(data)) || int64(e.offset) < int64(dirEnd) {
			return nil, invalid("table %s outside the file", tagString(e.tag))
		}
		if e.compLength > e.origLength {
			return nil, invalid("table %s compressed to more than its length", tagString(e.tag))
		}
		body += paddedLen(int64(e.origLength))
		if sfntSize(numTables, body) > MaxSfntBytes {
			return nil, fmt.Errorf("%w: a program of more than %d bytes", ErrTooLarge, MaxSfntBytes)
		}
		entries[i] = e
	}
	tables := make([]table, numTables)
	for i, e := range entries {
		stored := data[e.offset : e.offset+e.compLength]
		t := table{tag: e.tag, checksum: e.checksum}
		if e.compLength == e.origLength {
			t.data = slices.Clone(stored)
		} else {
			inflated, err := inflate(stored, int(e.origLength)) // fits int: at most MaxSfntBytes, checked above
			if err != nil {
				return nil, invalid("table %s: %v", tagString(e.tag), err)
			}
			t.data = inflated
		}
		tables[i] = t
	}
	return assemble(flavor, tables)
}

// inflate decompresses the zlib stream src, which must hold exactly n bytes.
func inflate(src []byte, n int) ([]byte, error) {
	zr, err := zlib.NewReader(bytes.NewReader(src))
	if err != nil {
		return nil, err
	}
	defer zr.Close() //nolint:errcheck // reading from memory: Close reports nothing a read did not
	return readExactly(zr, n)
}

// readExactly reads r to its end and returns its bytes, which must number
// exactly n. The buffer grows with what r delivers, so a header claiming
// more than a stream holds costs no memory up front.
func readExactly(r io.Reader, n int) ([]byte, error) {
	var buf bytes.Buffer
	got, err := buf.ReadFrom(io.LimitReader(r, int64(n)+1))
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if got != int64(n) {
		return nil, fmt.Errorf("a stream of %d bytes, %d expected", got, n)
	}
	return buf.Bytes(), nil
}
