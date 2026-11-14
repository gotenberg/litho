package dictbreak

import (
	"slices"
	"strings"
	"testing"
)

// TestThaiWords_HoldTheList walks the generated automaton: every edge
// leads inside it, and it spells the 26,383 words of ICU's thaidict.txt.
func TestThaiWords_HoldTheList(t *testing.T) {
	t.Parallel()
	d := thaiWords
	if d == nil || len(d.Edges)%edgeSize != 0 {
		t.Fatal("the Thai word list is missing or torn")
	}
	words := 0
	var walk func(state, depth int)
	walk = func(state, depth int) {
		if depth > 64 {
			t.Fatal("a path longer than any word")
		}
		for off := state * edgeSize; ; off += edgeSize {
			if off+edgeSize > len(d.Edges) {
				t.Fatalf("state at edge %d runs past the edges", state)
			}
			if d.Edges[off+1]&0x80 != 0 {
				words++
			}
			if next := d.target(off); next != 0 {
				walk(next, depth+1)
			}
			if d.Edges[off]&0x80 != 0 {
				return
			}
		}
	}
	walk(0, 0)
	if words != 26383 {
		t.Errorf("the list spells %d words, want 26383", words)
	}
	var lens [maxCandidates]int
	for word, whole := range map[string]bool{"มนุษย์": true, "เกียรติศักดิ์": true, "มนุษ": false, "กก": true} {
		n, reach := d.Prefixes([]rune(word), lens[:])
		got := n > 0 && lens[n-1] == len([]rune(word))
		if got != whole || reach != len([]rune(word)) {
			t.Errorf("%q: a word %t, reach %d, want %t, %d", word, got, reach, whole, len([]rune(word)))
		}
	}
}

// TestThai_DividesAsChromium divides Thai sentences into the words
// Chromium breaks their lines between.
//
// # Corpus evidence
//
//   - litho/html th-line-break: Chromium of gotenberg/gotenberg:9.4.1 sets
//     the six Thai paragraphs on 25 lines, 19 of them ending inside a
//     paragraph; litho sets the same 25 lines ending at the same 19 places,
//     where it set 22 lines broken at spaces alone without the word list
//     (layout 0.234 to 0.952).
//   - probe, html: eight paragraphs of Thai with no space, 1,198
//     characters, in boxes 8, 13, 19 and 27 em wide: Chromium sets 39
//     lines, 31 ending inside a paragraph, and litho the same 39 at the
//     same 31. Breaking between every letter a word may end on and every
//     letter one may start on, with no word list, ends 0 of the 19 and 6 of
//     the 31 where Chromium does.
func TestThai_DividesAsChromium(t *testing.T) {
	t.Parallel()
	for _, words := range [][]string{
		{"มนุษย์", "ทั้ง", "หลาย", "เกิด", "มา", "มี", "อิสระ", "และ", "เสมอ", "ภาค", "กัน", "ใน", "เกียรติศักดิ์", "และ", "สิทธิ"},
		{"อยู่", "ภาย", "ใต้", "การ", "จำกัด", "อธิปไตย", "ใด"},
		{"ไม่", "ว่า", "ดิน", "แดน", "นี้", "จะ", "เป็น", "เอกราช"},
	} {
		var want []int
		at := 0
		for _, w := range words[:len(words)-1] {
			at += len([]rune(w))
			want = append(want, at)
		}
		text := strings.Join(words, "")
		if got := Thai([]rune(text), nil); !slices.Equal(got, want) {
			t.Errorf("Thai(%q) = %v, want %v", text, got, want)
		}
	}
}

func BenchmarkThai(b *testing.B) {
	// About a page of running Thai: 3,225 characters.
	text := []rune(strings.Repeat("ทุกคนต่างเสมอกันในกฎหมายและมีสิทธิที่จะได้รับความคุ้มครองของกฎหมายเท่าเทียมกันโดยปราศจากการเลือกปฏิบัติใดๆทุกคนมีสิทธิที่จะได้รับ", 25))
	ends := make([]int, 0, len(text))
	b.ReportAllocs()
	for b.Loop() {
		ends = Thai(text, ends[:0])
	}
}
