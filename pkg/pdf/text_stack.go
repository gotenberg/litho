package pdf

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/typography"
)

// runStack is one piece of text a page draws several times over itself, a
// fake bold, a drop shadow, an outline pass, judged as one: glyphs holds the
// glyphs of every copy in content order, and members the runs, by index into
// the runs [textScene.stackRuns] read, that draw it, in content order.
type runStack struct {
	glyphs  []int
	members []int
}

// stackRuns gathers runs into the pieces of text they draw: two runs at
// least [stackMinShare] of the shorter's characters of which draw characters
// of the other again over themselves, by the rule default text extraction
// drops repeated glyphs by ([typography.OverdrawCopies]: the same character
// at the same size, under a quarter em away, its neighbours shifted alike),
// join one stack, and every other run stands alone. same reports two runs that may join, nil for any.
//
// The copies of one piece of text show as one: a copy drawn a fraction of a
// point off another lies over it (ISO 32000-1, 8.2: later marks paint over
// earlier ones), so each copy judged alone is judged against the others,
// light text against its own light copy, and the copy under the others
// shows only through them. Judged as one, a stack is drawn without all its
// copies, and what lies under the whole stack is its background.
func (s *textScene) stackRuns(ctx context.Context, runs [][]int, same func(a, b int) bool) ([]runStack, error) {
	copyOf, err := typography.OverdrawCopies(ctx, s.elements)
	if err != nil {
		return nil, fmt.Errorf("matching copied glyphs: %w", err)
	}
	s.copies = copyOf
	runOf := make([]int, len(s.elements))
	for i := range runOf {
		runOf[i] = -1
	}
	for k, run := range runs {
		for _, i := range run {
			runOf[i] = k
		}
	}
	parent := make([]int, len(runs))
	for k := range parent {
		parent[k] = k
	}
	find := func(k int) int {
		for parent[k] != k {
			parent[k] = parent[parent[k]]
			k = parent[k]
		}
		return k
	}
	links := make(map[[2]int]int)
	for j, i := range copyOf {
		if i < 0 {
			continue
		}
		a, b := runOf[i], runOf[j]
		if a < 0 || b < 0 || a == b || (same != nil && !same(a, b)) {
			continue
		}
		links[[2]int{min(a, b), max(a, b)}]++
	}
	pairs := make([][2]int, 0, len(links))
	for pair, n := range links {
		if float64(n) >= stackMinShare*float64(min(inked(s, runs[pair[0]]), inked(s, runs[pair[1]]))) {
			pairs = append(pairs, pair)
		}
	}
	// Sorted, so the stacks never depend on the order a map is read in.
	slices.SortFunc(pairs, func(x, y [2]int) int {
		if x[0] != y[0] {
			return x[0] - y[0]
		}
		return x[1] - y[1]
	})
	for _, pair := range pairs {
		// The lower root stays, so a stack is named by its first run.
		ra, rb := find(pair[0]), find(pair[1])
		if ra > rb {
			ra, rb = rb, ra
		}
		parent[rb] = ra
	}
	var stacks []runStack
	at := make(map[int]int)
	for k := range runs {
		root := find(k)
		n, ok := at[root]
		if !ok {
			n = len(stacks)
			at[root] = n
			stacks = append(stacks, runStack{})
		}
		stacks[n].members = append(stacks[n].members, k)
		stacks[n].glyphs = append(stacks[n].glyphs, runs[k]...)
	}
	for n := range stacks {
		if len(stacks[n].members) > 1 {
			slices.Sort(stacks[n].glyphs)
		}
	}
	return stacks, nil
}

// stackMinShare is the share of the characters of the shorter of two runs
// that must draw characters of the other again for the two to be one piece
// of text drawn twice ([textScene.stackRuns]). Not spec-mandated. A copy for
// an effect draws every character again, so the share is whole but where a
// copy is cut into show-text chains at other places than its original; two
// texts set at one place share a prefix now and then, as a report's English
// and Chinese running heads share their year, and judging those as one would
// judge each by the other's colour and name it by the other's words.
//
// # Corpus evidence
//
//   - pdfjs issue19319.pdf page 26: the English and Chinese running heads
//     share their four first glyphs, "2023", 4 of the 14 characters of the
//     shorter.
const stackMinShare = 0.5

// inked counts the glyphs of run that are not white space.
func inked(s *textScene, run []int) int {
	n := 0
	for _, i := range run {
		if strings.TrimSpace(s.elements[i].Text) != "" {
			n++
		}
	}
	return n
}

// stackGlyphs returns the glyphs of each stack, the runs to judge.
func stackGlyphs(stacks []runStack) [][]int {
	out := make([][]int, len(stacks))
	for n, st := range stacks {
		out[n] = st.glyphs
	}
	return out
}
