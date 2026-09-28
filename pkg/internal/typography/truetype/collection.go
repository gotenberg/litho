package truetype

import (
	"encoding/binary"
	"fmt"
	"io"
	"slices"
)

// collectionTag opens the header of a TrueType or OpenType Collection
// (ISO/IEC 14496-22, the OpenType specification: Font Collections,
// TTC header).
const collectionTag = "ttcf"

// maxCollectionFaces bounds the faces [Faces] reads from a collection.
// Anti-DoS guard, not spec-mandated: the TTC header counts its faces in a
// uint32, and the largest collections in use, the pan-CJK families, hold a few
// dozen faces, so a count past this bound is no collection a font directory
// holds.
const maxCollectionFaces = 1024

// maxSfntTables bounds the table directory [ReadInfoAt] and [ExtractFace]
// read for a face. Anti-DoS guard, not spec-mandated: the directory counts its
// tables in a uint16, and the OpenType specification registers fewer than a
// hundred table tags, so a face listing more is no font program.
const maxSfntTables = 512

// maxNameTableBytes bounds the bytes of the name table [ReadInfoAt] reads. It
// is what [readNames] can reach: the six-byte header, 65535 twelve-byte
// records, and a string whose storage offset, record offset and length are
// each at most 65535. Anti-DoS guard, not spec-mandated: a longer name table
// holds nothing [readNames] reads.
const maxNameTableBytes = 6 + 12*0xFFFF + 3*0xFFFF

// maxFaceBytes bounds the maxBytes [ExtractFace] accepts, so the offsets of
// the program it assembles fit the uint32 of the table directory. Anti-DoS
// guard, not spec-mandated.
const maxFaceBytes = 1 << 31

// IsCollection reports whether data starts with the tag of a TrueType or
// OpenType Collection header (ISO/IEC 14496-22: Font Collections, TTC
// header).
func IsCollection(data []byte) bool {
	return len(data) >= 4 && string(data[:4]) == collectionTag
}

// Faces returns the offset of the table directory of each face r holds: one
// per face of a collection, in the order its header lists them (ISO/IEC
// 14496-22: TTC header, numFonts then tableDirectoryOffsets), and 0 alone for
// a single sfnt program. size is the length of r.
func Faces(r io.ReaderAt, size int64) ([]int64, error) {
	var head [12]byte
	if size < int64(len(head)) {
		return nil, fmt.Errorf("%w: data too short for header", ErrInvalidFont)
	}
	if _, err := r.ReadAt(head[:], 0); err != nil {
		return nil, fmt.Errorf("reading the font header: %w", err)
	}
	if IsSfnt(head[:]) {
		return []int64{0}, nil
	}
	if !IsCollection(head[:]) {
		return nil, fmt.Errorf("%w: neither an sfnt program nor a collection", ErrInvalidFont)
	}
	// Versions 1.0 and 2.0 share the header up to the offsets; 2.0 appends
	// the DSIG fields after them, which this reader ignores.
	n := binary.BigEndian.Uint32(head[8:12])
	if n == 0 || n > maxCollectionFaces {
		return nil, fmt.Errorf("%w: collection of %d faces", ErrInvalidFont, n)
	}
	if int64(len(head))+4*int64(n) > size {
		return nil, fmt.Errorf("%w: collection header past the end of the data", ErrInvalidFont)
	}
	raw := make([]byte, 4*n)
	if _, err := r.ReadAt(raw, int64(len(head))); err != nil {
		return nil, fmt.Errorf("reading the collection header: %w", err)
	}
	faces := make([]int64, n)
	for i := range faces {
		faces[i] = int64(binary.BigEndian.Uint32(raw[4*i:]))
		if faces[i]+12 > size {
			return nil, fmt.Errorf("%w: face %d past the end of the data", ErrInvalidFont, i)
		}
	}
	return faces, nil
}

// ReadInfoAt reads the [Info] of the face whose table directory starts at dir
// in r, of length size, as [ReadInfo] reads that of a program: it reads the
// directory and the part of the head, hhea, OS/2, post and name tables
// [ReadInfo] uses, and none of the outlines.
func ReadInfoAt(r io.ReaderAt, size, dir int64) (Info, error) {
	_, records, err := readDirectoryAt(r, size, dir)
	if err != nil {
		return Info{}, err
	}
	return readInfo(records, func(tag string, need int) ([]byte, error) {
		for _, rec := range records {
			if rec.Tag != tag {
				continue
			}
			n := int64(rec.Length)
			if need > 0 {
				n = min(n, int64(need))
			}
			if tag == "name" {
				n = min(n, maxNameTableBytes)
			}
			buf := make([]byte, n)
			if _, err := r.ReadAt(buf, int64(rec.Offset)); err != nil {
				return nil, fmt.Errorf("reading table %q: %w", tag, err)
			}
			return buf, nil
		}
		return nil, fmt.Errorf("%w: %q", ErrTableNotFound, tag)
	})
}

// ExtractFace returns the face whose table directory starts at dir in r, of
// length size, as a program of its own: its tables, which a collection may
// share between faces, copied under a table directory of their own in tag
// order, with the head table's checkSumAdjustment computed anew (ISO/IEC
// 14496-22: table directory, head table). A DSIG table is dropped, since it
// signs the collection, not the program. A face whose tables sum past
// maxBytes is refused.
func ExtractFace(r io.ReaderAt, size, dir, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 || maxBytes > maxFaceBytes {
		return nil, fmt.Errorf("%w: face bound %d out of range", ErrInvalidFont, maxBytes)
	}
	version, records, err := readDirectoryAt(r, size, dir)
	if err != nil {
		return nil, err
	}
	tables := make(map[string][]byte, len(records))
	tags := make([]string, 0, len(records))
	var total int64
	for _, rec := range records {
		if rec.Tag == "DSIG" {
			continue
		}
		if _, dup := tables[rec.Tag]; dup {
			return nil, fmt.Errorf("%w: table %q listed twice", ErrInvalidFont, rec.Tag)
		}
		total += int64(rec.Length)
		if total > maxBytes {
			return nil, fmt.Errorf("%w: face of more than %d bytes", ErrInvalidFont, maxBytes)
		}
		buf := make([]byte, rec.Length)
		if _, err := r.ReadAt(buf, int64(rec.Offset)); err != nil {
			return nil, fmt.Errorf("reading table %q: %w", rec.Tag, err)
		}
		tables[rec.Tag] = buf
		tags = append(tags, rec.Tag)
	}
	if _, ok := tables["head"]; !ok {
		return nil, fmt.Errorf("%w: %q", ErrTableNotFound, "head")
	}
	if len(tables["head"]) < headInfoBytes {
		return nil, fmt.Errorf("%w: head table too short", ErrInvalidFont)
	}
	// checkSumAdjustment is computed over the program with the field zeroed
	// (ISO/IEC 14496-22: head table).
	binary.BigEndian.PutUint32(tables["head"][8:12], 0)
	slices.Sort(tags)
	return assembleTables(version, tags, tables), nil
}

// readDirectoryAt reads the sfnt version and the table directory starting at
// dir in r, of length size, and checks that every table it lists lies within
// size: a face whose directory points past the file is refused whole, since
// no program extracted from it would be complete.
func readDirectoryAt(r io.ReaderAt, size, dir int64) (uint32, []TableRecord, error) {
	var head [12]byte
	if dir < 0 || dir+int64(len(head)) > size {
		return 0, nil, fmt.Errorf("%w: table directory past the end of the data", ErrInvalidFont)
	}
	if _, err := r.ReadAt(head[:], dir); err != nil {
		return 0, nil, fmt.Errorf("reading the table directory: %w", err)
	}
	if !IsSfnt(head[:]) {
		return 0, nil, fmt.Errorf("%w: not an sfnt program", ErrInvalidFont)
	}
	n := int(binary.BigEndian.Uint16(head[4:6]))
	if n > maxSfntTables {
		return 0, nil, fmt.Errorf("%w: %d tables", ErrInvalidFont, n)
	}
	if dir+int64(len(head))+16*int64(n) > size {
		return 0, nil, fmt.Errorf("%w: data too short for table directory", ErrInvalidFont)
	}
	raw := make([]byte, 16*n)
	if _, err := r.ReadAt(raw, dir+int64(len(head))); err != nil {
		return 0, nil, fmt.Errorf("reading the table directory: %w", err)
	}
	records := make([]TableRecord, n)
	for i := range records {
		rec := raw[16*i:]
		records[i] = TableRecord{
			Tag:    string(rec[0:4]),
			Offset: binary.BigEndian.Uint32(rec[8:12]),
			Length: binary.BigEndian.Uint32(rec[12:16]),
		}
		if int64(records[i].Offset)+int64(records[i].Length) > size {
			return 0, nil, fmt.Errorf("%w: table %q extends beyond data", ErrInvalidFont, records[i].Tag)
		}
	}
	return binary.BigEndian.Uint32(head[0:4]), records, nil
}
