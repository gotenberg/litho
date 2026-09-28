package fontbuild

import (
	"bytes"
	"encoding/binary"
)

// Collection writes faces, each an sfnt program such as [TrueType.Build]
// returns, as a TrueType Collection (ISO/IEC 14496-22: Font Collections, TTC
// header version 1.0). A table whose bytes two faces share is stored once, as
// collections store the tables their faces share.
func Collection(faces ...[]byte) []byte {
	type table struct {
		tag      string
		data     []byte
		checksum uint32
	}
	type face struct {
		version uint32
		tables  []table
	}
	parsed := make([]face, len(faces))
	dirBytes := 0
	for i, f := range faces {
		n := int(binary.BigEndian.Uint16(f[4:6]))
		parsed[i].version = binary.BigEndian.Uint32(f[0:4])
		for k := range n {
			rec := f[12+16*k:]
			off := binary.BigEndian.Uint32(rec[8:12])
			length := binary.BigEndian.Uint32(rec[12:16])
			parsed[i].tables = append(parsed[i].tables, table{
				tag: string(rec[0:4]), data: f[off : off+length], checksum: binary.BigEndian.Uint32(rec[4:8]),
			})
		}
		dirBytes += 12 + 16*n
	}
	var body bytes.Buffer
	base := 12 + 4*len(faces) + dirBytes
	stored := map[string]int{}
	offsetOf := func(data []byte) int {
		if at, ok := stored[string(data)]; ok {
			return at
		}
		at := base + body.Len()
		stored[string(data)] = at
		body.Write(data)
		for body.Len()%4 != 0 {
			body.WriteByte(0)
		}
		return at
	}
	var out bytes.Buffer
	out.WriteString("ttcf")
	be32(&out, 0x00010000)
	be32(&out, uint32(len(faces))) //nolint:gosec // G115: test collections hold few faces.
	dirAt := 12 + 4*len(faces)
	for _, f := range parsed {
		be32(&out, uint32(dirAt)) //nolint:gosec // G115: test collections stay small.
		dirAt += 12 + 16*len(f.tables)
	}
	for _, f := range parsed {
		be32(&out, f.version)
		be16(&out, uint16(len(f.tables))) //nolint:gosec // G115: few tables.
		out.Write(make([]byte, 6))
		for _, t := range f.tables {
			out.WriteString(t.tag)
			be32(&out, t.checksum)
			be32(&out, uint32(offsetOf(t.data))) //nolint:gosec // G115: test collections stay small.
			be32(&out, uint32(len(t.data)))      //nolint:gosec // G115: test collections stay small.
		}
	}
	out.Write(body.Bytes())
	return out.Bytes()
}
