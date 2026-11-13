package writer

import (
	"bytes"
	"compress/zlib"
	"io"
	"runtime"
	"testing"
)

// zlibBytes compresses data with a new zlib writer at the default level.
func zlibBytes(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestDeflater_WritesTheBytesOfANewWriter(t *testing.T) {
	t.Parallel()
	large := make([]byte, 1<<20)
	for i := range large {
		large[i] = byte(i * i >> 7)
	}
	inputs := [][]byte{
		[]byte("BT /F1 12 Tf 72 712 Td (Hello) Tj ET\n"),
		large,
		nil,
		bytes.Repeat([]byte("0 0 m 10 10 l S\n"), 4096),
		[]byte("q 1 0 0 1 0 0 cm Q\n"),
		large[:70000],
	}
	var d Deflater
	for i, in := range inputs {
		got, err := d.Deflate(in)
		if err != nil {
			t.Fatalf("stream %d: %v", i, err)
		}
		if want := zlibBytes(t, in); !bytes.Equal(got, want) {
			t.Errorf("stream %d: %d bytes differ from a new writer's %d", i, len(got), len(want))
		}
		if cap(got) != len(got) {
			t.Errorf("stream %d: capacity %d past length %d", i, cap(got), len(got))
		}
	}
	if held, least := d.Held(), int64(DeflaterState); held < least {
		t.Errorf("Held() = %d, want at least %d", held, least)
	}
}

func TestDeflater_ZeroValueHoldsNothing(t *testing.T) {
	t.Parallel()
	var d Deflater
	if got := d.Held(); got != 0 {
		t.Errorf("Held() = %d before any stream, want 0", got)
	}
}

// TestDeflaterStateCoversACompressor measures what a zlib writer at the
// default level allocates up to its first output, the state a [Deflater]
// keeps, and fails when it passes [DeflaterState]. It runs alone, not in
// parallel, so that the counter reads its allocations only.
//
// # Corpus evidence
//
//   - probe, zlib: a writer and its first one-byte write allocate 748,272
//     bytes in 15 objects with Go 1.27.
func TestDeflaterStateCoversACompressor(t *testing.T) {
	const builds = 8
	writers := make([]*zlib.Writer, builds)
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	for i := range writers {
		writers[i] = zlib.NewWriter(io.Discard)
		if _, err := writers[i].Write([]byte{0}); err != nil {
			t.Fatal(err)
		}
	}
	runtime.ReadMemStats(&after)
	runtime.KeepAlive(writers)
	per := (after.TotalAlloc - before.TotalAlloc) / builds
	if per > DeflaterState {
		t.Errorf("a zlib writer allocates %d bytes, past DeflaterState %d", per, DeflaterState)
	}
	t.Logf("a zlib writer allocates %d bytes", per)
}
