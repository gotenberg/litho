package filter

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// FuzzStoredTail feeds arbitrary stored data to every filter StoredTail
// measures, which must not panic and must return a suffix of the data.
func FuzzStoredTail(f *testing.F) {
	f.Add([]byte("68656c6c6f> SECRET"), uint8(0))
	f.Add([]byte("6865"), uint8(0)) // Truncated: no EOD marker.
	f.Add([]byte{4, 'h', 'e', 'l', 'l', 'o', 128, 'x'}, uint8(3))
	f.Add([]byte{4, 'h'}, uint8(3)) // Truncated literal run.

	names := []ast.PdfName{"ASCIIHexDecode", "ASCII85Decode", "FlateDecode", "RunLengthDecode", "LZWDecode"}
	f.Fuzz(func(t *testing.T, data []byte, which uint8) {
		if len(data) > maxASCIIFuzzInput {
			return
		}
		name := names[int(which)%len(names)]
		stream := ast.NewStream(ast.PdfDict{"Filter": name}, data)
		tail, err := StoredTail(context.Background(), stream, tailLimit)
		if err != nil {
			t.Fatalf("StoredTail(%s, %q): %v", name, data, err)
		}
		if len(tail) > 0 && !bytes.Contains(data, tail) {
			t.Fatalf("StoredTail(%s, %q) = %q, which the data does not hold", name, data, tail)
		}
	})
}
