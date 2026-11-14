package main

import (
	"errors"
	"slices"
	"testing"
)

// words reads back every word the packed edges spell, in label order.
func words(t *testing.T, edges string, base rune) []string {
	t.Helper()
	var out []string
	var walk func(state int, prefix []rune)
	walk = func(state int, prefix []rune) {
		for off := state * edgeSize; ; off += edgeSize {
			if off+edgeSize > len(edges) {
				t.Fatalf("state at edge %d runs past the edges", state)
			}
			w := append(slices.Clone(prefix), base+rune(edges[off]&maxLabel))
			if edges[off+1]&0x80 != 0 {
				out = append(out, string(w))
			}
			if next := int(edges[off+1]&0x7f)<<16 | int(edges[off+2])<<8 | int(edges[off+3]); next != 0 {
				walk(next, w)
			}
			if edges[off]&0x80 != 0 {
				return
			}
		}
	}
	walk(0, nil)
	return out
}

func TestEncode_SpellsTheListAndSharesEndings(t *testing.T) {
	list := []string{"dc", "abc", "ab", "dc", "bc"}
	edges, err := encode(list, 'a'-1)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := words(t, edges, 'a'-1), []string{"ab", "abc", "bc", "dc"}; !slices.Equal(got, want) {
		t.Errorf("words %v, want %v", got, want)
	}
	// The start state has a, b and d; a leads to b, a final state with c;
	// b and d share the state that c ends: 3 + 1 + 1 + 1 edges, where a
	// trie of the list has 7.
	if n := len(edges) / edgeSize; n != 6 {
		t.Errorf("%d edges, want 6", n)
	}
}

func TestEncode_RefusesWordsOutsideItsLetters(t *testing.T) {
	for _, w := range []string{"", "aB", "aĀ"} {
		if _, err := encode([]string{"ab", w}, 'a'-1); !errors.Is(err, errBadWord) {
			t.Errorf("encode with %q: %v, want errBadWord", w, err)
		}
	}
}
