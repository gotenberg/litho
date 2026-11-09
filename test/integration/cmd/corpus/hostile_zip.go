package main

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"encoding/binary"
	"hash/crc32"
	"io"
)

// deflated is one entry's content compressed as a zip stores it, with the
// size and CRC-32 of what it inflates to.
type deflated struct {
	data []byte
	size uint64
	crc  uint32
}

// deflate compresses what write writes, streamed, so a payload of hundreds
// of megabytes is never held whole. The fastest level packs a run of one
// byte as tightly as the best one, over ten times faster.
func deflate(write func(w io.Writer) error) (deflated, error) {
	var buf bytes.Buffer
	fw, err := flate.NewWriter(&buf, flate.BestSpeed)
	if err != nil {
		return deflated{}, err
	}
	h := crc32.NewIEEE()
	c := &countWriter{}
	if err := write(io.MultiWriter(fw, h, c)); err != nil {
		return deflated{}, err
	}
	if err := fw.Close(); err != nil {
		return deflated{}, err
	}
	return deflated{data: buf.Bytes(), size: c.n, crc: h.Sum32()}, nil
}

// deflateBytes compresses data (see [deflate]).
func deflateBytes(data []byte) (deflated, error) {
	return deflate(func(w io.Writer) error {
		_, err := w.Write(data)
		return err
	})
}

// countWriter counts the bytes written through it.
type countWriter struct{ n uint64 }

func (c *countWriter) Write(p []byte) (int, error) {
	c.n += uint64(len(p))
	return len(p), nil
}

// repeatTo writes n bytes of b's repetition to w.
func repeatTo(w io.Writer, b byte, n int64) error {
	chunk := bytes.Repeat([]byte{b}, 1<<20)
	for n > 0 {
		k := min(n, int64(len(chunk)))
		if _, err := w.Write(chunk[:k]); err != nil {
			return err
		}
		n -= k
	}
	return nil
}

// rawEntry is one entry [rawZip] writes as given: its stored bytes, method,
// flags and the sizes and CRC-32 its headers declare, true or not.
type rawEntry struct {
	name   string
	method uint16
	flags  uint16
	d      deflated
	// declared, when set, is the uncompressed size the headers state
	// instead of the true one.
	declared uint64
}

// rawZip writes entries with archive/zip's raw mode, after prefix: the
// headers say what each entry declares, not what it holds.
func rawZip(prefix []byte, entries []rawEntry) ([]byte, error) {
	var buf bytes.Buffer
	buf.Write(prefix)
	zw := zip.NewWriter(&buf)
	zw.SetOffset(int64(len(prefix)))
	for _, e := range entries {
		size := e.d.size
		if e.declared != 0 {
			size = e.declared
		}
		w, err := zw.CreateRaw(&zip.FileHeader{
			Name: e.name, Method: e.method, Flags: e.flags, Modified: zipEpoch,
			CRC32: e.d.crc, CompressedSize64: uint64(len(e.d.data)), UncompressedSize64: size,
		})
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(e.d.data); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// rawPackage returns the entries of parts deflated, in package order.
func rawPackage(parts map[string][]byte) ([]rawEntry, error) {
	var out []rawEntry
	for _, name := range packageOrder(parts) {
		d, err := deflateBytes(parts[name])
		if err != nil {
			return nil, err
		}
		out = append(out, rawEntry{name: name, method: zip.Deflate, d: d})
	}
	return out, nil
}

// overlapZip writes parts, then one local entry holding shared, which the
// central directory names once for each of names: every one of them
// inflates the same stored bytes (Fifield, "A better zip bomb", 2019).
// archive/zip cannot write a central entry without its own local one.
func overlapZip(parts map[string][]byte, shared deflated, names []string) ([]byte, error) {
	type central struct {
		name   string
		d      deflated
		offset uint32
	}
	var (
		buf  bytes.Buffer
		dir  []central
		le   = binary.LittleEndian
		u16  = func(v uint16) { _ = binary.Write(&buf, le, v) } //nolint:errcheck // bytes.Buffer writes never fail
		u32  = func(v uint32) { _ = binary.Write(&buf, le, v) } //nolint:errcheck // bytes.Buffer writes never fail
		dosD = uint16(1<<5 | 1)                                 // 1980-01-01
	)
	local := func(name string, d deflated) uint32 {
		off := uint32(buf.Len()) //nolint:gosec // a generated archive well under 4 GiB
		u32(0x04034b50)
		u16(20)
		u16(0)
		u16(zip.Deflate)
		u16(0)
		u16(dosD)
		u32(d.crc)
		u32(uint32(len(d.data))) //nolint:gosec // a generated entry well under 4 GiB
		u32(uint32(d.size))      //nolint:gosec // a generated payload well under 4 GiB
		u16(uint16(len(name)))   //nolint:gosec // a short generated name
		u16(0)
		buf.WriteString(name)
		buf.Write(d.data)
		return off
	}
	for _, name := range packageOrder(parts) {
		d, err := deflateBytes(parts[name])
		if err != nil {
			return nil, err
		}
		dir = append(dir, central{name, d, local(name, d)})
	}
	off := local(names[0], shared)
	for _, name := range names {
		dir = append(dir, central{name, shared, off})
	}
	start := uint32(buf.Len()) //nolint:gosec // a generated archive well under 4 GiB
	for _, c := range dir {
		u32(0x02014b50)
		u16(20)
		u16(20)
		u16(0)
		u16(zip.Deflate)
		u16(0)
		u16(dosD)
		u32(c.d.crc)
		u32(uint32(len(c.d.data))) //nolint:gosec // a generated entry well under 4 GiB
		u32(uint32(c.d.size))      //nolint:gosec // a generated payload well under 4 GiB
		u16(uint16(len(c.name)))   //nolint:gosec // a short generated name
		u16(0)
		u16(0)
		u16(0)
		u16(0)
		u32(0)
		u32(c.offset)
		buf.WriteString(c.name)
	}
	size := uint32(buf.Len()) - start //nolint:gosec // a generated archive well under 4 GiB
	u32(0x06054b50)
	u16(0)
	u16(0)
	u16(uint16(len(dir))) //nolint:gosec // fewer than 65,535 generated entries
	u16(uint16(len(dir))) //nolint:gosec // fewer than 65,535 generated entries
	u32(size)
	u32(start)
	u16(0)
	return buf.Bytes(), nil
}
