package writer

import (
	"bytes"
	"compress/zlib"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

func TestWriteXRefTable_Basic(t *testing.T) {
	records := []XRefRecord{
		{ObjNum: 0, Offset: 0, Generation: 65535, Free: true},
		{ObjNum: 1, Offset: 9, Generation: 0},
		{ObjNum: 2, Offset: 58, Generation: 0},
	}
	trailer := ast.PdfDict{
		"Size": ast.PdfInt(3),
		"Root": ast.PdfIndirectRef{ObjectNum: 1, GenNum: 0},
	}

	var buf bytes.Buffer
	_, _, err := WriteXRefTable(context.Background(), &buf, records, trailer)
	if err != nil {
		t.Fatalf("WriteXRefTable: %v", err)
	}

	out := buf.String()

	// Check structure.
	if !strings.HasPrefix(out, "xref\n") {
		t.Error("missing 'xref' header")
	}
	if !strings.Contains(out, "0 3\n") {
		t.Error("missing subsection header '0 3'")
	}
	if !strings.Contains(out, "0000000000 65535 f\r\n") {
		t.Error("missing free entry for object 0")
	}
	if !strings.Contains(out, "0000000009 00000 n\r\n") {
		t.Error("missing entry for object 1")
	}
	if !strings.Contains(out, "trailer\n") {
		t.Error("missing 'trailer' keyword")
	}
	if !strings.Contains(out, "/Root") {
		t.Error("missing /Root in trailer")
	}
}

func TestWriteXRefTable_NonConsecutive(t *testing.T) {
	// Objects 1 and 5, should produce two subsections.
	records := []XRefRecord{
		{ObjNum: 1, Offset: 10, Generation: 0},
		{ObjNum: 5, Offset: 200, Generation: 0},
	}
	trailer := ast.PdfDict{"Size": ast.PdfInt(6)}

	var buf bytes.Buffer
	_, _, err := WriteXRefTable(context.Background(), &buf, records, trailer)
	if err != nil {
		t.Fatalf("WriteXRefTable: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "1 1\n") {
		t.Error("missing subsection '1 1'")
	}
	if !strings.Contains(out, "5 1\n") {
		t.Error("missing subsection '5 1'")
	}
}

func TestWriteXRefStream_Basic(t *testing.T) {
	records := []XRefRecord{
		{ObjNum: 0, Offset: 0, Generation: 65535, Free: true},
		{ObjNum: 1, Offset: 9, Generation: 0},
		{ObjNum: 2, Offset: 58, Generation: 0},
	}
	trailer := ast.PdfDict{
		"Root": ast.PdfIndirectRef{ObjectNum: 1, GenNum: 0},
	}
	allocator := NewObjAllocator(2) // NextObjNum == 3, the xref stream's own number

	const selfOffset = 200
	var buf bytes.Buffer
	_, err := WriteXRefStream(context.Background(), &buf, records, trailer, allocator, selfOffset)
	if err != nil {
		t.Fatalf("WriteXRefStream: %v", err)
	}

	out := buf.String()
	// The xref stream is written as an indirect object.
	if !strings.Contains(out, "obj\n") {
		t.Error("missing indirect object header")
	}
	if !strings.Contains(out, "/Type /XRef") {
		t.Error("missing /Type /XRef")
	}
	if !strings.Contains(out, "/W") {
		t.Error("missing /W array")
	}
	if !strings.Contains(out, "/Filter /FlateDecode") {
		t.Error("missing /Filter")
	}
	if !strings.Contains(out, "stream\r\n") {
		t.Error("missing stream keyword")
	}
	if !strings.Contains(out, "/Root") {
		t.Error("missing /Root in xref stream dict")
	}
	// The xref stream must carry an entry for its own object (number 3),
	// so /Size accounts for it and /Index spans 0..3 (ISO 32000-1
	// §7.5.8.2). Without the self-entry these would read /Size 3 / [0 3].
	if !strings.Contains(out, "/Size 4") {
		t.Errorf("expected /Size 4 (3 records + self), got:\n%s", out)
	}
	if !strings.Contains(out, "/Index [0 4]") {
		t.Errorf("expected /Index [0 4] covering the xref stream object, got:\n%s", out)
	}

	// Decode the stream and confirm the self object (3) has a type-1
	// entry at selfOffset.
	entries := decodeXRefStreamEntries(t, out)
	self, ok := entries[3]
	if !ok {
		t.Fatal("decoded xref stream has no entry for its own object (3)")
	}
	if self.typ != 1 {
		t.Errorf("self entry type = %d, want 1 (in-use)", self.typ)
	}
	if self.offset != selfOffset {
		t.Errorf("self entry offset = %d, want %d", self.offset, selfOffset)
	}
}

type decodedXRefEntry struct {
	typ    int64
	offset int64
}

// decodeXRefStreamEntries inflates the FlateDecode stream payload from a
// serialized xref stream and parses its W=[1 w1 w2] records, keyed by
// object number derived from the /Index subsections.
func decodeXRefStreamEntries(t *testing.T, out string) map[int]decodedXRefEntry {
	t.Helper()
	start := strings.Index(out, "stream\r\n")
	if start < 0 {
		t.Fatal("no stream marker")
	}
	start += len("stream\r\n")
	end := strings.Index(out[start:], "\nendstream")
	if end < 0 {
		t.Fatal("no endstream marker")
	}
	zr, err := zlib.NewReader(strings.NewReader(out[start : start+end]))
	if err != nil {
		t.Fatalf("zlib reader: %v", err)
	}
	raw, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("inflate: %v", err)
	}

	w1, w2 := parseWidths(t, out)
	idx := parseIndex(t, out)
	entrySize := 1 + w1 + w2
	if len(raw)%entrySize != 0 {
		t.Fatalf("decoded length %d not a multiple of entry size %d", len(raw), entrySize)
	}

	readBE := func(b []byte) int64 {
		var v int64
		for _, c := range b {
			v = v<<8 | int64(c)
		}
		return v
	}

	entries := make(map[int]decodedXRefEntry)
	pos := 0
	rec := 0
	for i := 0; i+1 < len(idx); i += 2 {
		objNum, count := idx[i], idx[i+1]
		for c := range count {
			b := raw[pos : pos+entrySize]
			entries[objNum+c] = decodedXRefEntry{
				typ:    readBE(b[:1]),
				offset: readBE(b[1 : 1+w1]),
			}
			pos += entrySize
			rec++
		}
	}
	return entries
}

func parseWidths(t *testing.T, out string) (int, int) {
	t.Helper()
	i := strings.Index(out, "/W [")
	if i < 0 {
		t.Fatal("no /W array")
	}
	var a, b, c int
	if _, err := fmt.Sscanf(out[i:], "/W [%d %d %d]", &a, &b, &c); err != nil {
		t.Fatalf("parsing /W: %v", err)
	}
	return b, c
}

func parseIndex(t *testing.T, out string) []int {
	t.Helper()
	i := strings.Index(out, "/Index [")
	if i < 0 {
		t.Fatal("no /Index array")
	}
	j := strings.Index(out[i:], "]")
	fields := strings.Fields(out[i+len("/Index [") : i+j])
	nums := make([]int, 0, len(fields))
	for _, f := range fields {
		var n int
		if _, err := fmt.Sscanf(f, "%d", &n); err != nil {
			t.Fatalf("parsing /Index field %q: %v", f, err)
		}
		nums = append(nums, n)
	}
	return nums
}

func TestWriteXRefStream_DropsInheritedDecodeParms(t *testing.T) {
	// An incremental update inherits the source xref stream's dict as the
	// trailer, which can carry /DecodeParms (a PNG predictor + /Columns). The
	// writer emits raw, non-predictor entry data, so it must not propagate
	// /DecodeParms: a strict parser would otherwise un-predict the raw rows
	// into garbage and reject the stream.
	records := []XRefRecord{{ObjNum: 1, Offset: 9, Generation: 0}}
	trailer := ast.PdfDict{
		"Root": ast.PdfIndirectRef{ObjectNum: 1, GenNum: 0},
		"DecodeParms": ast.PdfDict{
			"Predictor": ast.PdfInt(12),
			"Columns":   ast.PdfInt(3),
		},
	}
	var buf bytes.Buffer
	if _, err := WriteXRefStream(context.Background(), &buf, records, trailer, NewObjAllocator(1), 100); err != nil {
		t.Fatalf("WriteXRefStream: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "/DecodeParms") || strings.Contains(out, "/Predictor") {
		t.Errorf("inherited /DecodeParms must be dropped (writer emits non-predictor data):\n%s", out)
	}
	// The raw (no-predictor) stream still decodes to a valid entry.
	if _, ok := decodeXRefStreamEntries(t, out)[1]; !ok {
		t.Errorf("entry for object 1 not decodable:\n%s", out)
	}
}

func TestBytesNeeded(t *testing.T) {
	tests := []struct {
		val  int64
		want int
	}{
		{0, 1},
		{1, 1},
		{255, 1},
		{256, 2},
		{65535, 2},
		{65536, 3},
		{16777215, 3},
		{16777216, 4},
	}
	for _, tc := range tests {
		got := bytesNeeded(tc.val)
		if got != tc.want {
			t.Errorf("bytesNeeded(%d): got %d, want %d", tc.val, got, tc.want)
		}
	}
}

func TestAppendBigEndian(t *testing.T) {
	// 258 = 0x0102 in 2 bytes big-endian.
	got := appendBigEndian(nil, 258, 2)
	if len(got) != 2 || got[0] != 0x01 || got[1] != 0x02 {
		t.Errorf("appendBigEndian(258, 2): got %v", got)
	}

	// 0 in 1 byte.
	got = appendBigEndian(nil, 0, 1)
	if len(got) != 1 || got[0] != 0x00 {
		t.Errorf("appendBigEndian(0, 1): got %v", got)
	}
}
