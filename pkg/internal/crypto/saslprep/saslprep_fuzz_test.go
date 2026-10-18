package saslprep

import (
	"testing"
	"unicode/utf8"
)

// FuzzPrepare asserts the preparation never panics, returns valid UTF-8
// holding no prohibited character, and that [Stored] only ever narrows
// [Query]: what it accepts, Query accepts with the same result.
func FuzzPrepare(f *testing.F) {
	f.Add("I­X")
	f.Add("Ⅸ")
	f.Add("ا1ب")
	f.Add("ا1")
	f.Add("a\xffb")
	f.Add("pw\U0001F600")
	f.Fuzz(func(t *testing.T, s string) {
		q, qErr := Query(s)
		if qErr == nil {
			if !utf8.ValidString(q) {
				t.Fatalf("Query(%q) = %q, not valid UTF-8", s, q)
			}
			for _, r := range q {
				if in(prohibited, r) {
					t.Fatalf("Query(%q) = %q holds prohibited U+%04X", s, q, r)
				}
			}
		} else if q != "" {
			t.Fatalf("Query(%q) returned %q with error %v", s, q, qErr)
		}
		st, sErr := Stored(s)
		if sErr == nil && (qErr != nil || st != q) {
			t.Fatalf("Stored(%q) = %q, nil but Query = %q, %v", s, st, q, qErr)
		}
	})
}
