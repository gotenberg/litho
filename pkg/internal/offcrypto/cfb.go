package offcrypto

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"strings"
	"unicode/utf16"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
)

// cfbSignature opens every compound file (MS-CFB, 2.2, Header Signature).
var cfbSignature = []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}

// The special sector numbers of MS-CFB, 2.1.
const (
	cfbMaxRegSect    = 0xFFFFFFFA
	cfbEndOfChain    = 0xFFFFFFFE
	cfbFreeSect      = 0xFFFFFFFF
	cfbNoStream      = 0xFFFFFFFF
	cfbHeaderBytes   = 512
	cfbDirEntryBytes = 128
	// cfbHeaderDIFAT is the number of DIFAT entries the header holds
	// (MS-CFB, 2.2).
	cfbHeaderDIFAT = 109
)

// The object types of a directory entry (MS-CFB, 2.6.1).
const (
	cfbTypeStream = 2
	cfbTypeRoot   = 5
)

// maxCFBDirEntries caps the directory entries read at 65,536. An encrypted
// Office document holds about ten (the root, EncryptionInfo,
// EncryptedPackage, the DataSpaces storage and its four streams); a binary
// Word or Excel file a few dozen. Anti-DoS guard, not spec-mandated: MS-CFB,
// 2.6 bounds the directory by the sector count alone.
const maxCFBDirEntries = 1 << 16

// cfbFile is a compound file read into its allocation tables and directory.
// Streams are copied out on demand.
type cfbFile struct {
	data       []byte
	major      uint16
	sectorSize int
	miniSize   int
	miniCutoff uint64
	nSectors   int
	fat        []uint32
	miniFAT    []uint32
	dir        []cfbDirEntry
	miniStream []byte
}

// cfbDirEntry is the part of a directory entry (MS-CFB, 2.6) the reader
// uses.
type cfbDirEntry struct {
	name               string
	objType            byte
	left, right, child uint32
	start              uint32
	size               uint64
}

func cfbMalformed(format string, args ...any) error {
	return fmt.Errorf("%w: compound file: %s", office.ErrMalformed, fmt.Sprintf(format, args...))
}

// readCFB reads the header, the FAT through the DIFAT, the directory, the
// mini FAT and the mini stream of a compound file (MS-CFB, 2.2 to 2.6).
// Every sector number is checked against the file's sector count and every
// chain against cycles, so a hostile file fails with office.ErrMalformed.
func readCFB(ctx context.Context, data []byte) (*cfbFile, error) {
	if len(data) < cfbHeaderBytes || !bytes.HasPrefix(data, cfbSignature) {
		return nil, cfbMalformed("no compound file header")
	}
	le := binary.LittleEndian
	f := &cfbFile{data: data, major: le.Uint16(data[0x1A:])}
	// MS-CFB, 2.2: Byte Order is 0xFFFE; the sector shift is 9 in version 3
	// and 12 in version 4; the mini sector shift is 6.
	if le.Uint16(data[0x1C:]) != 0xFFFE {
		return nil, cfbMalformed("byte order mark %#x", le.Uint16(data[0x1C:]))
	}
	shift, miniShift := le.Uint16(data[0x1E:]), le.Uint16(data[0x20:])
	switch {
	case f.major == 3 && shift == 9, f.major == 4 && shift == 12:
	default:
		return nil, cfbMalformed("version %d with sector shift %d", f.major, shift)
	}
	if miniShift != 6 {
		return nil, cfbMalformed("mini sector shift %d", miniShift)
	}
	f.sectorSize = 1 << shift
	f.miniSize = 1 << miniShift
	// Sector 0 follows the header sector. MS-CFB, 2.2 asks for a file of
	// whole sectors; a short last sector is read as zero-padded, the way
	// LibreOffice's and msoffcrypto-tool's readers do.
	if len(data) <= f.sectorSize {
		return nil, cfbMalformed("no sector after the header")
	}
	f.nSectors = (len(data) - f.sectorSize + f.sectorSize - 1) / f.sectorSize
	nFAT := le.Uint32(data[0x2C:])
	firstDir := le.Uint32(data[0x30:])
	f.miniCutoff = uint64(le.Uint32(data[0x38:]))
	firstMiniFAT, nMiniFAT := le.Uint32(data[0x3C:]), le.Uint32(data[0x40:])
	firstDIFAT, nDIFAT := le.Uint32(data[0x44:]), le.Uint32(data[0x48:])
	// MS-CFB, 2.2: Mini Stream Cutoff Size MUST be 4096.
	if f.miniCutoff != 4096 {
		return nil, cfbMalformed("mini stream cutoff %d", f.miniCutoff)
	}
	if int64(nFAT) > int64(f.nSectors) || int64(nDIFAT) > int64(f.nSectors) || int64(nMiniFAT) > int64(f.nSectors) {
		return nil, cfbMalformed("table sector counts past the %d sectors of the file", f.nSectors)
	}
	if err := f.readFAT(ctx, int(nFAT), firstDIFAT, int(nDIFAT)); err != nil { // fits int: at most nSectors, checked above
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := f.readDirectory(firstDir); err != nil {
		return nil, err
	}
	if nMiniFAT > 0 && firstMiniFAT != cfbEndOfChain {
		raw, err := f.chainBytes(firstMiniFAT, f.fat, f.nSectors, f.sectorSize, f.sectorBytes)
		if err != nil {
			return nil, fmt.Errorf("reading the mini FAT: %w", err)
		}
		f.miniFAT = make([]uint32, len(raw)/4)
		for i := range f.miniFAT {
			f.miniFAT[i] = le.Uint32(raw[4*i:])
		}
	}
	root := f.dir[0]
	if root.size > 0 && root.start != cfbEndOfChain {
		ms, err := f.chainBytes(root.start, f.fat, f.nSectors, f.sectorSize, f.sectorBytes)
		if err != nil {
			return nil, fmt.Errorf("reading the mini stream: %w", err)
		}
		if root.size > uint64(len(ms)) {
			return nil, cfbMalformed("mini stream of %d bytes in a chain of %d", root.size, len(ms))
		}
		f.miniStream = ms[:root.size]
	}
	return f, nil
}

// sectorBytes returns sector n, zero-padded when the file ends inside it.
func (f *cfbFile) sectorBytes(n uint32) ([]byte, error) {
	if n > cfbMaxRegSect || int64(n) >= int64(f.nSectors) {
		return nil, cfbMalformed("sector %d past the %d sectors of the file", n, f.nSectors)
	}
	off := (int(n) + 1) * f.sectorSize // fits int: n < nSectors, which len(data) bounds
	end := off + f.sectorSize
	if end <= len(f.data) {
		return f.data[off:end], nil
	}
	s := make([]byte, f.sectorSize)
	copy(s, f.data[off:])
	return s, nil
}

// miniSectorBytes returns mini sector n of the mini stream (MS-CFB, 2.4).
func (f *cfbFile) miniSectorBytes(n uint32) ([]byte, error) {
	off := int64(n) * int64(f.miniSize)
	if off+int64(f.miniSize) > int64(len(f.miniStream)) {
		return nil, cfbMalformed("mini sector %d past the mini stream", n)
	}
	return f.miniStream[off : off+int64(f.miniSize)], nil
}

// readFAT collects the FAT sector numbers from the header's DIFAT and the
// DIFAT chain (MS-CFB, 2.5), then reads the FAT (MS-CFB, 2.3).
func (f *cfbFile) readFAT(ctx context.Context, nFAT int, firstDIFAT uint32, nDIFAT int) error {
	le := binary.LittleEndian
	locs := make([]uint32, 0, min(nFAT, cfbHeaderDIFAT))
	for i := 0; i < cfbHeaderDIFAT && len(locs) < nFAT; i++ {
		locs = append(locs, le.Uint32(f.data[0x4C+4*i:]))
	}
	perDIFAT := f.sectorSize/4 - 1
	next := firstDIFAT
	for i := 0; len(locs) < nFAT; i++ {
		// A DIFAT chain is linked through each sector's last entry; one
		// longer than its header count is a cycle or a lie.
		if i >= nDIFAT || next == cfbEndOfChain || next == cfbFreeSect {
			return cfbMalformed("DIFAT lists %d of %d FAT sectors", len(locs), nFAT)
		}
		s, err := f.sectorBytes(next)
		if err != nil {
			return fmt.Errorf("reading the DIFAT: %w", err)
		}
		for j := 0; j < perDIFAT && len(locs) < nFAT; j++ {
			locs = append(locs, le.Uint32(s[4*j:]))
		}
		next = le.Uint32(s[4*perDIFAT:])
	}
	perFAT := f.sectorSize / 4
	f.fat = make([]uint32, 0, nFAT*perFAT)
	for i, loc := range locs {
		if i%256 == 255 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		s, err := f.sectorBytes(loc)
		if err != nil {
			return fmt.Errorf("reading FAT sector %d: %w", i, err)
		}
		for j := range perFAT {
			f.fat = append(f.fat, le.Uint32(s[4*j:]))
		}
	}
	return nil
}

// chainBytes concatenates the sectors of the chain that starts at start in
// table, each of size bytes read through read. A chain that leaves the
// table, or holds more links than there are units to address (limit) and
// so loops, fails (MS-CFB, 2.3: a chain ends with ENDOFCHAIN). The result
// is at most limit times size bytes.
func (f *cfbFile) chainBytes(start uint32, table []uint32, limit, size int, read func(uint32) ([]byte, error)) ([]byte, error) {
	var out []byte
	n := start
	limit = min(limit, len(table))
	for steps := 0; n != cfbEndOfChain; steps++ {
		if steps >= limit {
			return nil, cfbMalformed("sector chain from %d loops", start)
		}
		if uint64(n) >= uint64(len(table)) {
			return nil, cfbMalformed("sector %d outside its allocation table", n)
		}
		s, err := read(n)
		if err != nil {
			return nil, err
		}
		if out == nil {
			out = make([]byte, 0, size*4)
		}
		out = append(out, s...)
		n = table[n]
	}
	return out, nil
}

// readDirectory reads the directory chain (MS-CFB, 2.6) and checks that
// entry 0 is the root storage.
func (f *cfbFile) readDirectory(first uint32) error {
	raw, err := f.chainBytes(first, f.fat, f.nSectors, f.sectorSize, f.sectorBytes)
	if err != nil {
		return fmt.Errorf("reading the directory: %w", err)
	}
	n := min(len(raw)/cfbDirEntryBytes, maxCFBDirEntries)
	if n == 0 {
		return cfbMalformed("empty directory")
	}
	le := binary.LittleEndian
	f.dir = make([]cfbDirEntry, n)
	for i := range f.dir {
		e := raw[i*cfbDirEntryBytes : (i+1)*cfbDirEntryBytes]
		// MS-CFB, 2.6.1: the name is UTF-16LE, its length in bytes counts
		// the terminating null and is at most 64.
		nameLen := int(le.Uint16(e[0x40:]))
		if nameLen > 64 || nameLen%2 != 0 {
			nameLen = 0
		}
		units := make([]uint16, 0, nameLen/2)
		for j := 0; j+1 < nameLen; j += 2 {
			if u := le.Uint16(e[j:]); u != 0 {
				units = append(units, u)
			}
		}
		size := le.Uint64(e[0x78:])
		// MS-CFB, 2.6.3: a version 3 reader ignores the high 32 bits of the
		// stream size, which old writers leave uninitialised.
		if f.major == 3 {
			size &= 0xFFFFFFFF
		}
		f.dir[i] = cfbDirEntry{
			name:    string(utf16.Decode(units)),
			objType: e[0x42],
			left:    le.Uint32(e[0x44:]),
			right:   le.Uint32(e[0x48:]),
			child:   le.Uint32(e[0x4C:]),
			start:   le.Uint32(e[0x74:]),
			size:    size,
		}
	}
	if f.dir[0].objType != cfbTypeRoot {
		return cfbMalformed("entry 0 is not the root storage")
	}
	return nil
}

// rootChild finds the entry named name among the root storage's children:
// the red-black tree under the root's child link (MS-CFB, 2.6.4), walked
// with every entry visited at most once. Names compare case-insensitively,
// as MS-CFB, 2.6.4 orders them.
func (f *cfbFile) rootChild(name string) (cfbDirEntry, bool) {
	seen := make([]bool, len(f.dir))
	stack := []uint32{f.dir[0].child}
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if id == cfbNoStream || uint64(id) >= uint64(len(f.dir)) || seen[id] {
			continue
		}
		seen[id] = true
		e := f.dir[id]
		if strings.EqualFold(e.name, name) {
			return e, true
		}
		stack = append(stack, e.left, e.right)
	}
	return cfbDirEntry{}, false
}

// stream returns the bytes of the root-level stream named name, and false
// when there is none. A stream shorter than the mini stream cutoff lives in
// the mini stream (MS-CFB, 2.4).
func (f *cfbFile) stream(name string) ([]byte, bool, error) {
	e, ok := f.rootChild(name)
	if !ok || e.objType != cfbTypeStream {
		return nil, false, nil
	}
	if e.size == 0 {
		return []byte{}, true, nil
	}
	if e.size > uint64(len(f.data)) {
		return nil, true, cfbMalformed("stream %s of %d bytes in a file of %d", name, e.size, len(f.data))
	}
	var raw []byte
	var err error
	if e.size < f.miniCutoff {
		raw, err = f.chainBytes(e.start, f.miniFAT, len(f.miniStream)/f.miniSize, f.miniSize, f.miniSectorBytes)
	} else {
		raw, err = f.chainBytes(e.start, f.fat, f.nSectors, f.sectorSize, f.sectorBytes)
	}
	if err != nil {
		return nil, true, fmt.Errorf("reading stream %s: %w", name, err)
	}
	if e.size > uint64(len(raw)) {
		return nil, true, cfbMalformed("stream %s of %d bytes in a chain of %d", name, e.size, len(raw))
	}
	return raw[:e.size], true, nil
}
