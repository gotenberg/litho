package layout

import "math"

// prettyLines is how many of a paragraph's last lines text-wrap: pretty
// breaks again: 4, as Chromium does.
//
// # Corpus evidence
//
//   - probe, html: of 190 paragraphs of 8 to 60 words Chromium 156 prints
//     under text-wrap: pretty, the 31 whose greedy last line holds one
//     word come out as the rule of [engine.prettyEnds] gives them with 4
//     lines broken again, 16 of 27 of them with 3 and 21 of 27 with 5;
//     the 159 others break as under wrap.
const prettyLines = 4

// maxPrettyBreaks caps the break opportunities text-wrap: pretty weighs
// in the last lines of one paragraph: 256. Anti-DoS guard, not
// spec-mandated: the search costs the square of their number, and four
// lines of real text hold a few dozen.
const maxPrettyBreaks = 256

// prettyState is the block whose last lines text-wrap: pretty breaks
// again while it is laid out: the lines its greedy layout made, recorded
// while rec, then the ends chosen for some of them.
type prettyState struct {
	root  *box
	rec   bool
	lines []prettyLine
	ends  map[prettyKey]int
}

// prettyLine is one line the greedy layout of a pretty block made: its
// paragraph and advances, where it starts and ends, the room it was
// broken in and the indent it started at.
type prettyLine struct {
	p             *ifcPara
	w             []float64
	start, end    int
	avail, indent float64
}

// prettyKey names a line by its paragraph and its start.
type prettyKey struct {
	p     *ifcPara
	start int
}

// note records the line of p from start to end broken in avail after
// indent, replacing the record of a line from the same start, which a
// line broken again in another band makes.
func (s *prettyState) note(p *ifcPara, w []float64, start, end int, avail, indent float64) {
	if n := len(s.lines); n > 0 && s.lines[n-1].p == p && s.lines[n-1].start == start {
		s.lines = s.lines[:n-1]
	}
	s.lines = append(s.lines, prettyLine{p: p, w: w, start: start, end: end, avail: avail, indent: indent})
}

// prettyEnds chooses where the last lines of each paragraph of the pretty
// block end (CSS Text 4, 6.2, text-wrap-style: pretty, which leaves the
// algorithm to the user agent), as Chromium prints them: a paragraph of
// two lines or more whose greedy last line holds one word has its last
// [prettyLines] lines broken again, the lines before them as they were,
// into as many lines, the last holding two words or more, with the least
// sum of the squares of the room the others leave. Any other paragraph
// keeps its greedy breaks. The result maps the start of each line broken
// again, its last aside, to its end.
func (e *engine) prettyEnds() map[prettyKey]int {
	var out map[prettyKey]int
	lines := e.pretty.lines
	for i := 0; i < len(lines); {
		j := i
		for j < len(lines) && lines[j].p == lines[i].p {
			j++
		}
		if ends := prettyParagraph(lines[i:j]); len(ends) > 0 {
			if out == nil {
				out = map[prettyKey]int{}
			}
			for k, v := range ends {
				out[k] = v
			}
		}
		i = j
	}
	return out
}

// prettyParagraph returns the ends [engine.prettyEnds] chooses for the
// lines of one paragraph, nil to keep them.
func prettyParagraph(lines []prettyLine) map[prettyKey]int {
	n := len(lines)
	if n < 2 {
		return nil
	}
	last := lines[n-1]
	p := last.p
	size := p.p.Len()
	if last.end != size || words(p, last.start, size) > 1 {
		return nil
	}
	k := min(prettyLines, n)
	first := lines[n-k]
	breaks := []int{first.start}
	for i := first.start + 1; i < size; i++ {
		if p.allowed(i) && !hangsTo(p, i, size) {
			breaks = append(breaks, i)
			if len(breaks) > maxPrettyBreaks {
				return nil
			}
		}
	}
	breaks = append(breaks, size)
	m := len(breaks)
	// cost[j][b] is the least cost of lines j.. of the k, line j starting
	// at breaks[b]; next[j][b] the index its end is at.
	cost := make([][]float64, k+1)
	next := make([][]int, k)
	for j := range cost {
		cost[j] = make([]float64, m)
		for b := range cost[j] {
			cost[j][b] = math.Inf(1)
		}
	}
	cost[k][m-1] = 0
	for j := k - 1; j >= 0; j-- {
		next[j] = make([]int, m)
		ln := lines[n-k+j]
		indent := 0.0
		if j == 0 {
			indent = first.indent
		}
		for b := 0; b < m-1; b++ {
			for c := b + 1; c < m; c++ {
				if math.IsInf(cost[j+1][c], 1) {
					continue
				}
				wd := indent + lineAdvance(p, first.w, breaks[b], breaks[c])
				if wd > ln.avail+eps {
					break
				}
				lc := 0.0
				switch {
				case j < k-1:
					slack := ln.avail - wd
					lc = float64(slack * slack)
				case c != m-1 || words(p, breaks[b], size) < 2:
					continue
				}
				if total := lc + cost[j+1][c]; total < cost[j][b] {
					cost[j][b], next[j][b] = total, c
				}
			}
		}
	}
	if math.IsInf(cost[0][0], 1) {
		return nil
	}
	out := map[prettyKey]int{}
	b := 0
	for j := range k - 1 {
		c := next[j][b]
		out[prettyKey{p, breaks[b]}] = breaks[c]
		b = c
	}
	return out
}

// words counts the words of p from start to end: one more than the break
// opportunities inside it before its hanging end.
func words(p *ifcPara, start, end int) int {
	n := 1
	for i := start + 1; i < end; i++ {
		if p.allowed(i) && !hangsTo(p, i, end) {
			n++
		}
	}
	return n
}

// hangsTo reports whether every character of p from i to end hangs.
func hangsTo(p *ifcPara, i, end int) bool {
	for ; i < end; i++ {
		if !p.hangs(i) {
			return false
		}
	}
	return true
}

// lineAdvance returns the advance of the characters of p from start to
// end, w their advances, its hanging end left out.
func lineAdvance(p *ifcPara, w []float64, start, end int) float64 {
	for end > start && p.hangs(end-1) {
		end--
	}
	x := 0.0
	for i := start; i < end; i++ {
		x += w[i]
	}
	return x
}
