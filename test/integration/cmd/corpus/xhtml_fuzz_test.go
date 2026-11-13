package main

import (
	"bytes"
	"testing"
)

// FuzzStripStyleCDATA checks that no file makes the strip panic, that a
// refused file comes back unchanged, and that an accepted one loses its
// markers and nothing else.
func FuzzStripStyleCDATA(f *testing.F) {
	f.Add([]byte(`<html><head><style><![CDATA[ p { color: green } ]]></style></head><body><p>a</p></body></html>`))
	f.Add([]byte(`<html><head><style><![CDATA[ p { col`))
	f.Fuzz(func(t *testing.T, src []byte) {
		got, ok := stripStyleCDATA(src)
		if !ok {
			if !bytes.Equal(got, src) {
				t.Errorf("a refused file changed: %q to %q", src, got)
			}
			return
		}
		// Each section loses its two markers, 12 bytes, and keeps the rest
		// in order.
		removed := len(src) - len(got)
		if removed%(len(cdataOpen)+len(cdataClose)) != 0 || !subsequence(got, src) {
			t.Errorf("%q stripped to %q: more than its markers went", src, got)
		}
	})
}

// subsequence reports whether a's bytes appear in b in order.
func subsequence(a, b []byte) bool {
	i := 0
	for _, c := range b {
		if i < len(a) && a[i] == c {
			i++
		}
	}
	return i == len(a)
}
