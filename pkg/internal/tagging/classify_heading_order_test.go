package tagging

import "testing"

// sized builds blocks carrying only the font sizes, in reading order, which is
// all headingLevels reads.
func sized(sizes ...float64) []textBlock {
	blocks := make([]textBlock, len(sizes))
	for i, s := range sizes {
		blocks[i] = textBlock{fontSize: s}
	}
	return blocks
}

// levelsOf returns the heading level per block, dropping the body blocks so a
// case reads as the heading sequence a validator would walk.
func levelsOf(t *testing.T, body float64, sizes ...float64) []int {
	t.Helper()
	var out []int
	for _, lvl := range headingLevels(sized(sizes...), body) {
		if lvl > 0 {
			out = append(out, lvl)
		}
	}
	return out
}

func equal(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestHeadingLevelsFollowReadingOrder pins ISO 14289-1, 7.4.2: the first
// heading is H1 and no heading jumps more than one level past the deepest seen.
//
// Levels used to come from ranking the distinct sizes, which answers a
// different question: the first heading a page contains is rarely its largest,
// so a page whose opening heading was the sixth-largest tagged to H6 and failed
// the rule immediately.
//
// # Corpus evidence
//
//   - govdocs thread_000_000337.pdf: did exactly that.
//   - All corpora: auto-tagging introduced 7.4.2 test 1 into 35 files that did
//     not have it.
func TestHeadingLevelsFollowReadingOrder(t *testing.T) {
	t.Parallel()

	const body = 10

	cases := []struct {
		name  string
		sizes []float64
		want  []int
	}{
		{
			// The defect: the smallest heading comes first in reading order.
			name:  "a small heading first still starts at H1",
			sizes: []float64{12, 30, 20},
			want:  []int{1, 1, 2},
		},
		{
			name:  "descending sizes nest one level at a time",
			sizes: []float64{30, 20, 15, 12},
			want:  []int{1, 2, 3, 4},
		},
		{
			name:  "a repeated size stays at its level",
			sizes: []float64{30, 20, 20, 20},
			want:  []int{1, 2, 2, 2},
		},
		{
			name:  "a larger heading re-opens a shallower level",
			sizes: []float64{30, 20, 15, 30, 20},
			want:  []int{1, 2, 3, 1, 2},
		},
		{
			// Body text between headings must not disturb the sequence.
			name:  "body text is skipped",
			sizes: []float64{30, 10, 20, 10, 15},
			want:  []int{1, 2, 3},
		},
		{
			name:  "a single heading is H1",
			sizes: []float64{14},
			want:  []int{1},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := levelsOf(t, body, tc.sizes...)
			if !equal(got, tc.want) {
				t.Fatalf("levels = %v, want %v", got, tc.want)
			}
			// Whatever the sizes, the emitted sequence must satisfy the clause.
			maxSeen := 0
			for i, lvl := range got {
				if lvl > maxSeen+1 {
					t.Errorf("heading %d jumps to H%d past H%d", i, lvl, maxSeen)
				}
				if lvl > maxSeen {
					maxSeen = lvl
				}
			}
			if len(got) > 0 && got[0] != 1 {
				t.Errorf("first heading is H%d, must be H1", got[0])
			}
		})
	}
}

// TestHeadingLevelsNoBodySize pins that a page with no usable body size claims
// no headings at all, rather than tagging everything H1.
func TestHeadingLevelsNoBodySize(t *testing.T) {
	t.Parallel()
	for _, lvl := range headingLevels(sized(30, 20, 10), 0) {
		if lvl != 0 {
			t.Fatalf("no body size must yield no headings, got level %d", lvl)
		}
	}
}

// TestHeadingLevelsDeepNestingStaysValid pins that a page with more distinct
// sizes than H6 still emits a sequence the clause accepts: the cap repeats a
// level, which is allowed, rather than skipping one, which is not.
func TestHeadingLevelsDeepNestingStaysValid(t *testing.T) {
	t.Parallel()

	sizes := []float64{40, 36, 32, 28, 24, 20, 18, 16}
	levels := headingLevels(sized(sizes...), 10)

	maxSeen := 0
	for i, lvl := range levels {
		if lvl == 0 {
			t.Fatalf("block %d is not a heading", i)
		}
		role := headingRole(lvl)
		if role == "" {
			t.Fatalf("block %d has no role", i)
		}
		if lvl > maxSeen+1 {
			t.Errorf("heading %d jumps to H%d past H%d", i, lvl, maxSeen)
		}
		if lvl > maxSeen {
			maxSeen = lvl
		}
	}
	if levels[0] != 1 {
		t.Errorf("first heading is H%d, must be H1", levels[0])
	}
}
