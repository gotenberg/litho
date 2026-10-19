package content

import (
	"context"
	"testing"
)

func FuzzTerminatedEnd(f *testing.F) {
	f.Add([]byte("0 0 m 10 10 l S\n(closed) Tj\n"))
	f.Add([]byte("0 0 m 10 10 l S\n(dangling ( string"))
	f.Add([]byte("0 0 m 10 10 l S\n<0A1B"))
	f.Fuzz(func(t *testing.T, data []byte) {
		n := TerminatedEnd(context.Background(), data)
		if n < 0 || n > len(data) {
			t.Fatalf("TerminatedEnd = %d, outside [0, %d]", n, len(data))
		}
	})
}
