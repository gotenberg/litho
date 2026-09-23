package jbig2

import (
	"context"
	"os"
	"testing"
)

// FuzzDecode feeds arbitrary page and global segment data to the decoder. The
// contract is crash-freedom: every input either decodes to a bitmap whose
// buffer matches its dimensions or returns an error.
func FuzzDecode(f *testing.F) {
	b, err := os.ReadFile("testdata/t88_annex_h1.jb2")
	if err != nil {
		f.Fatalf("reading seed: %v", err)
	}
	// Seeds reach the parser and both generic region codings: the Annex H.1
	// page 1 (MMR) and page 2 (arithmetic, TPGD) regions, each behind its
	// page information segment, whole and truncated, plus an encoder round
	// trip with moved AT pixels.
	var raw [][]byte
	for off := 13; off < len(b); {
		_, n, err := parseSegment(b[off:])
		if err != nil {
			f.Fatalf("seed segment: %v", err)
		}
		raw = append(raw, b[off:off+n])
		off += n
	}
	mmr := concat(raw[1], raw[4])
	arith := concat(raw[8], raw[11])
	f.Add(mmr, []byte(nil))
	f.Add(arith, []byte(nil))
	f.Add(arith[:len(arith)-9], []byte(nil))
	f.Add(mmr[:len(mmr)-20], raw[0])
	src := testImage(40, 24, 7)
	at := [][2]int{{4, -2}, {-5, -1}, {1, -3}, {-6, -3}}
	f.Add(genericStream(src.Width, src.Height, 0, true, at, encodeGeneric(src, 0, true, at)), []byte(nil))
	f.Add([]byte{}, []byte{})

	f.Fuzz(func(t *testing.T, data, globals []byte) {
		bm, err := Decode(context.Background(), data, globals)
		if err != nil {
			return
		}
		if len(bm.Data) != bm.Stride*bm.Height || bm.Stride != (bm.Width+7)/8 {
			t.Fatalf("%dx%d bitmap with stride %d holds %d bytes", bm.Width, bm.Height, bm.Stride, len(bm.Data))
		}
	})
}
