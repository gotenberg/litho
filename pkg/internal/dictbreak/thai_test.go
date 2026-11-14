package dictbreak

import (
	"slices"
	"testing"
)

// TestDivideThai locks the choices of the Thai engine (ICU User Guide,
// Boundary Analysis, Dictionary-Based BreakIterator) on word lists of a
// few words, each case naming the rule it shows.
func TestDivideThai(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		words []string
		text  string
		want  []int
	}{
		{
			// กขค is followed by no word; กข by คง.
			name:  "the longest word a second word follows",
			words: []string{"กข", "กขค", "คง"},
			text:  "กขคง",
			want:  []int{2},
		},
		{
			// กขค then งจ leaves ฉ; กข then คง then จฉ reads three words.
			name:  "a word a second and a third follow, at once",
			words: []string{"กข", "กขค", "คง", "งจ", "จฉ"},
			text:  "กขคงจฉ",
			want:  []int{2, 4},
		},
		{
			name:  "the longest word when none is followed",
			words: []string{"กข", "กขค"},
			text:  "กขคง",
			want:  []int{3},
		},
		{
			name:  "a tone mark stays with the word before it",
			words: []string{"กขค"},
			text:  "กขค่กขค",
			want:  []int{4},
		},
		{
			name:  "letters no word starts make a word of their own after a long word",
			words: []string{"กขค", "จฉช"},
			text:  "กขคงงจฉช",
			want:  []int{3, 5},
		},
		{
			name:  "letters no word starts join a short word before them",
			words: []string{"กข", "จฉช"},
			text:  "กขงงจฉช",
			want:  []int{4},
		},
		{
			name:  "a repetition mark ends the word before it",
			words: []string{"กขค"},
			text:  "กขคๆกขค",
			want:  []int{4},
		},
		{
			name:  "an abbreviation mark ends the word before it",
			words: []string{"กขค"},
			text:  "กขคฯกขค",
			want:  []int{4},
		},
		{
			name:  "a run shorter than two words of two letters",
			words: []string{"กข", "ค"},
			text:  "กขค",
			want:  nil,
		},
		{
			name:  "no word at all",
			words: []string{"จฉ"},
			text:  "กขคง",
			want:  nil,
		},
	} {
		d := testTrie(t, 0x0e00, tc.words...)
		got := divideThai(d, []rune(tc.text), nil)
		if !slices.Equal(got, tc.want) {
			t.Errorf("%s: divideThai(%q) = %v, want %v", tc.name, tc.text, got, tc.want)
		}
	}
}

func TestDivideThai_AppendsAfterEnds(t *testing.T) {
	t.Parallel()
	d := testTrie(t, 0x0e00, "กข", "คง")
	got := divideThai(d, []rune("กขคง"), []int{7})
	if want := []int{7, 2}; !slices.Equal(got, want) {
		t.Errorf("divideThai appended to [7] = %v, want %v", got, want)
	}
	if got := divideThai(nil, []rune("กขคง"), nil); got != nil {
		t.Errorf("divideThai without a word list = %v, want none", got)
	}
}

func TestIsThai(t *testing.T) {
	t.Parallel()
	for r, want := range map[rune]bool{
		'ก': true, 'ฯ': true, 'ั': true, 'ฺ': true,
		'฿': false, 'เ': true, 'ๆ': true, '๎': true,
		'๏': false, '๐': false, '๛': false, 'a': false, ' ': false,
	} {
		if got := IsThai(r); got != want {
			t.Errorf("IsThai(%U) = %t, want %t", r, got, want)
		}
	}
}
