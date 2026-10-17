package filter

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// tailLimit is the decoded budget the tail tests measure under.
const tailLimit = 1 << 20

func storedTail(t *testing.T, filter ast.PdfObject, data []byte) []byte {
	t.Helper()
	dict := ast.PdfDict{}
	if filter != nil {
		dict["Filter"] = filter
	}
	tail, err := StoredTail(context.Background(), ast.NewStream(dict, data), tailLimit)
	if err != nil {
		t.Fatalf("StoredTail: %v", err)
	}
	return tail
}

// TestStoredTail_FindsTheBytesPastEachFiltersEnd pins the tail of every filter
// whose decoder says where its data ends: what follows the end is returned,
// white space dropped, and data that ends where its encoding ends has none.
func TestStoredTail_FindsTheBytesPastEachFiltersEnd(t *testing.T) {
	t.Parallel()
	secret := []byte("SECRET")
	cases := []struct {
		name   string
		filter ast.PdfObject
		data   []byte
		want   []byte
	}{
		{"flate whole", ast.PdfName("FlateDecode"), zlibCompress([]byte("hello world")), nil},
		{"flate after checksum", ast.PdfName("FlateDecode"), append(zlibCompress([]byte("hello world")), secret...), secret},
		{"flate end-of-line counted in Length", ast.PdfName("FlateDecode"), append(zlibCompress([]byte("hello")), '\r', '\n'), nil},
		{"raw deflate", ast.PdfName("Fl"), append(rawDeflate(t, []byte("hello world")), secret...), secret},
		{"flate damaged header", ast.PdfName("FlateDecode"), []byte("SECRET not zlib"), []byte("SECRET not zlib")},
		{"lzw after EOD", ast.PdfName("LZWDecode"), append(lzwCompress([]byte("hello world")), secret...), secret},
		{"hex after EOD", ast.PdfName("ASCIIHexDecode"), []byte("68656c6c6f> SECRET"), secret},
		{"hex damaged", ast.PdfName("AHx"), []byte("6865 SECRET"), secret},
		{"hex whole", ast.PdfName("AHx"), []byte("68656c6c6f>\n"), nil},
		{"a85 after EOD", ast.PdfName("ASCII85Decode"), []byte("87cURD]i,\"Ebo80~> SECRET"), secret},
		{"a85 whole", ast.PdfName("A85"), []byte("87cURD]i,\"Ebo80~>"), nil},
		{"runlength after EOD", ast.PdfName("RunLengthDecode"), append([]byte{4, 'h', 'e', 'l', 'l', 'o', 128}, secret...), secret},
		{"runlength cut short", ast.PdfName("RL"), []byte{4, 'h', 'e'}, nil},
		{"chain measures the first filter", ast.PdfArray{ast.PdfName("ASCIIHexDecode"), ast.PdfName("FlateDecode")}, []byte("00> SECRET"), secret},
		{"no filter", nil, []byte("SECRET"), nil},
		{"codec reports none", ast.PdfName("DCTDecode"), []byte("SECRET"), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := storedTail(t, tc.filter, tc.data); !bytes.Equal(got, tc.want) {
				t.Errorf("tail = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestStoredTail_StopsAtTheDecodedBudget pins the anti-DoS guard: data that
// decodes past the limit reports no tail rather than decoding on.
func TestStoredTail_StopsAtTheDecodedBudget(t *testing.T) {
	t.Parallel()
	data := append(zlibCompress(make([]byte, 4096)), []byte("SECRET")...)
	tail, err := StoredTail(context.Background(), ast.NewStream(ast.PdfDict{"Filter": ast.PdfName("FlateDecode")}, data), 1024)
	if err != nil {
		t.Fatalf("StoredTail: %v", err)
	}
	if tail != nil {
		t.Errorf("tail = %q past the budget, want none", tail)
	}
}

// TestStoredTail_HonoursCancellation pins that a done context ends the
// measurement with its error rather than an answer.
func TestStoredTail_HonoursCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	data := append(zlibCompress([]byte("hello")), []byte("SECRET")...)
	if _, err := StoredTail(ctx, ast.NewStream(ast.PdfDict{"Filter": ast.PdfName("FlateDecode")}, data), tailLimit); err == nil {
		t.Error("a cancelled measurement returned no error")
	}
}
