package dictbreak

import (
	"slices"
	"testing"
)

// testTrie packs words as a plain trie, in the layout [Dictionary] reads,
// labels counted from base: one state a prefix, no state shared.
func testTrie(t *testing.T, base rune, words ...string) *Dictionary {
	t.Helper()
	type state struct {
		final  bool
		labels []byte
		next   []*state
	}
	root := &state{}
	for _, w := range words {
		s := root
		for _, r := range w {
			l := r - base
			if l <= 0 || l > maxLabel {
				t.Fatalf("letter %U outside the dictionary", r)
			}
			k := slices.Index(s.labels, byte(l))
			if k < 0 {
				k = len(s.labels)
				for k > 0 && s.labels[k-1] > byte(l) {
					k--
				}
				s.labels = slices.Insert(s.labels, k, byte(l))
				s.next = slices.Insert(s.next, k, &state{})
			}
			s = s.next[k]
		}
		s.final = true
	}
	first := map[*state]int{}
	var order []*state
	count := 0
	var place func(s *state)
	place = func(s *state) {
		if len(s.labels) == 0 {
			return
		}
		first[s] = count
		count += len(s.labels)
		order = append(order, s)
		for _, c := range s.next {
			place(c)
		}
	}
	place(root)
	var out []byte
	for _, s := range order {
		for i, l := range s.labels {
			c := s.next[i]
			b0, b1, tgt := l, byte(0), first[c]
			if i == len(s.labels)-1 {
				b0 |= 0x80
			}
			if c.final {
				b1 = 0x80
			}
			out = append(out, b0, b1|byte(tgt>>16&0x7f), byte(tgt>>8), byte(tgt))
		}
	}
	return &Dictionary{Base: base, Edges: string(out)}
}

func TestDictionary_Prefixes(t *testing.T) {
	t.Parallel()
	d := testTrie(t, 'a'-1, "ab", "abc", "abcde", "b")
	var lens [4]int
	for _, tc := range []struct {
		text  string
		want  []int
		reach int
	}{
		{"abcdx", []int{2, 3}, 4},
		{"abcdef", []int{2, 3, 5}, 5},
		{"a", nil, 1},
		{"bc", []int{1}, 1},
		{"c", nil, 0},
		{"A", nil, 0},
		{"", nil, 0},
	} {
		n, reach := d.Prefixes([]rune(tc.text), lens[:])
		if got := lens[:n]; !slices.Equal(got, tc.want) || reach != tc.reach {
			t.Errorf("Prefixes(%q) = %v, reach %d, want %v, reach %d", tc.text, got, reach, tc.want, tc.reach)
		}
	}
	// lens bounds the words written; reach is still the walk's.
	n, reach := d.Prefixes([]rune("abcde"), lens[:1])
	if n != 1 || lens[0] != 2 || reach != 5 {
		t.Errorf("Prefixes into one slot = %d words, first %d, reach %d, want 1, 2, 5", n, lens[0], reach)
	}
	var none *Dictionary
	if n, reach := none.Prefixes([]rune("ab"), lens[:]); n != 0 || reach != 0 {
		t.Errorf("a nil dictionary found %d words, reach %d", n, reach)
	}
}
