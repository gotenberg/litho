package pdf

import (
	"bytes"
	"strings"
	"testing"
)

// countedWrites counts the writes it receives and keeps their bytes.
type countedWrites struct {
	bytes.Buffer
	writes int
}

func (w *countedWrites) Write(p []byte) (int, error) {
	w.writes++
	return w.Buffer.Write(p)
}

// fewWrites fails the test when dst received its bytes in more writes than
// buffers of [writeBufferSize] hold them, with a few to spare for a write
// larger than the buffer and the last flush.
func fewWrites(t *testing.T, what string, dst *countedWrites) {
	t.Helper()
	if dst.Len() == 0 {
		t.Fatalf("%s wrote nothing", what)
	}
	if most := dst.Len()/writeBufferSize + 4; dst.writes > most {
		t.Errorf("%s: %d bytes reached the destination in %d writes, want at most %d", what, dst.Len(), dst.writes, most)
	}
}

// TestWrites_ReachTheDestinationBuffered checks that a conversion, a merge
// and a split hand their file to the caller's writer in buffers, not one
// write a token: on a file each write is a system call.
func TestWrites_ReachTheDestinationBuffered(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	fonts := WithConvertFontDirs(convertFontDir(t))

	var converted countedWrites
	src := strings.Repeat("A line of the document, long enough to fill a page with others like it.\n", 4000)
	if _, err := FromText(strings.NewReader(src), fonts).WriteTo(ctx, &converted); err != nil {
		t.Fatal(err)
	}
	fewWrites(t, "a conversion", &converted)

	open := func() *Reader {
		t.Helper()
		r, err := Open(ctx, bytes.NewReader(converted.Bytes()), int64(converted.Len()))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := r.Close(); err != nil {
				t.Error(err)
			}
		})
		return r
	}

	var merged countedWrites
	if err := Merge(ctx, []*Reader{open(), open()}, &merged); err != nil {
		t.Fatal(err)
	}
	fewWrites(t, "a merge", &merged)

	var split countedWrites
	if err := Split(ctx, open(), []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, &split); err != nil {
		t.Fatal(err)
	}
	fewWrites(t, "a split", &split)
}
