package filter

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/limits"
)

// FuzzSalvageReport feeds arbitrary FlateDecode and LZWDecode data to the
// chain, the refusing decoder and the streaming reader. None may panic, and
// the refusing decoder must refuse exactly the streams the report does not
// call clean, handing on the default bytes for the others.
func FuzzSalvageReport(f *testing.F) {
	plain := []byte("salvage me, salvage me, salvage me")
	whole := zlibCompress(plain)
	badSum := bytes.Clone(whole)
	badSum[len(badSum)-1] ^= 0xFF
	f.Add(whole)
	f.Add(whole[:len(whole)/2])
	f.Add(badSum)
	f.Add(whole[:len(whole)-4])
	f.Add(lzwCompress(plain))
	f.Add([]byte{0x80, 0x0b, 0x60})

	strict := NewDecoder(nil, limits.Default()).RefuseSalvaged()
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<16 {
			return
		}
		ctx := context.Background()
		for _, name := range []ast.PdfName{"FlateDecode", "LZWDecode"} {
			stream := ast.NewStream(ast.PdfDict{"Filter": name}, data)
			got, report, err := (*Decoder)(nil).DecodeStreamReport(ctx, stream)
			refused, serr := strict.DecodeStream(ctx, stream)
			switch {
			case err != nil:
				if serr == nil {
					t.Fatalf("%s: the refusing decoder decoded a stream that fails: %v", name, err)
				}
			case report.Clean():
				if serr != nil || !bytes.Equal(refused, got) {
					t.Fatalf("%s: clean stream refused or changed: %v", name, serr)
				}
			case !errors.Is(serr, ErrSalvagedStream):
				t.Fatalf("%s: report %+v not refused: %v", name, report, serr)
			}
			if rc, err := NewStreamReader(ctx, stream); err == nil {
				_, _ = io.Copy(io.Discard, rc) // Read errors are part of the contract under test: only a panic fails.
				_ = rc.Close()                 // Close errors likewise.
			}
		}
	})
}
