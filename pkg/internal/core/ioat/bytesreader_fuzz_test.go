package ioat

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

// FuzzBytesReaderMatchesReader pins that a Reader over bytes in memory reads
// as a Reader over a [bytes.Reader] of the same bytes: ops drives both
// through the same reads, unreads, seeks and limits, and every result must
// agree.
func FuzzBytesReaderMatchesReader(f *testing.F) {
	f.Add([]byte("1 0 obj << /Type /Page /Kids [2 0 R] >> endobj"), int64(2), []byte{0, 0, 1, 2, 9, 3, 7, 4, 0, 5, 6, 0})
	f.Add([]byte("1 0 obj << /Ty"), int64(0), []byte{0, 3, 12, 0, 0, 4, 0})
	f.Fuzz(func(t *testing.T, data []byte, offset int64, ops []byte) {
		want := NewReader(bytes.NewReader(data), int64(len(data)), offset)
		got := NewBytesReader(data, offset)
		for i := 0; i+1 < len(ops); i += 2 {
			arg := int64(ops[i+1])
			switch ops[i] % 7 {
			case 0:
				wb, werr := want.ReadByte()
				gb, gerr := got.ReadByte()
				if wb != gb || (werr == nil) != (gerr == nil) {
					t.Fatalf("op %d ReadByte = %d, %v; want %d, %v", i, gb, gerr, wb, werr)
				}
			case 1:
				werr := want.UnreadByte()
				gerr := got.UnreadByte()
				if (werr == nil) != (gerr == nil) {
					t.Fatalf("op %d UnreadByte = %v; want %v", i, gerr, werr)
				}
			case 2:
				wp := make([]byte, arg)
				gp := make([]byte, arg)
				wn, werr := io.ReadFull(want, wp)
				gn, gerr := io.ReadFull(got, gp)
				if wn != gn || !bytes.Equal(wp[:wn], gp[:gn]) || !errors.Is(gerr, werr) && (werr == nil) != (gerr == nil) {
					t.Fatalf("op %d ReadFull(%d) = %d, %v; want %d, %v", i, arg, gn, gerr, wn, werr)
				}
			case 3:
				whence := int(arg % 3)
				off := arg - 128
				wp, werr := want.Seek(off, whence)
				gp, gerr := got.Seek(off, whence)
				if wp != gp || (werr == nil) != (gerr == nil) {
					t.Fatalf("op %d Seek(%d, %d) = %d, %v; want %d, %v", i, off, whence, gp, gerr, wp, werr)
				}
			case 4:
				want.SetLimit(arg - 1)
				got.SetLimit(arg - 1)
			case 5:
				want.ClearLimit()
				got.ClearLimit()
			case 6:
				wa, ga := make([]byte, 4), make([]byte, 4)
				wn, _ := want.ReaderAt().ReadAt(wa, arg)
				gn, _ := got.ReaderAt().ReadAt(ga, arg)
				if wn != gn || !bytes.Equal(wa, ga) {
					t.Fatalf("op %d ReaderAt().ReadAt(%d) = %q; want %q", i, arg, ga[:gn], wa[:wn])
				}
			}
			if want.Position() != got.Position() {
				t.Fatalf("op %d: Position = %d, want %d", i, got.Position(), want.Position())
			}
		}
	})
}
