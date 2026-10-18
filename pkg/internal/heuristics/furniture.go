package heuristics

import (
	"sort"
	"strconv"
	"strings"
)

// Edge names the page margin a line of text sits in.
type Edge int

// The margins [MarginEdge] tells apart.
const (
	// EdgeNone is a line in neither margin band, or one whose position is not
	// compared.
	EdgeNone Edge = iota
	// EdgeTop is a line in the top margin band, where running headers sit.
	EdgeTop
	// EdgeBottom is a line in the bottom margin band, where running footers
	// and page numbers sit.
	EdgeBottom
)

// marginBandRatio is the share of the page height, at the top and at the
// bottom, that [MarginEdge] reads as the margin band. A tuning constant, not
// spec-mandated: a one-inch margin on US Letter is 9% of its height, and
// running heads sit inside the margin, so 10% holds them without reaching the
// first or last body line of a normally set page. The bookmark heading filter
// reads the same share of the text's extent.
const marginBandRatio = 0.10

// MarginEdge returns the margin band a line sits in and its distance from that
// page edge, given the lowest and highest glyph origins of the line and the
// bottom and top of the page's visible box, all in default user space (ISO
// 32000-1, 8.3.2.3; Y grows upward). The whole line must sit in the band: a
// block reaching out of it is body text that only starts near an edge. A line
// outside the box shows nothing and sits in no band.
func MarginEdge(minY, maxY, boxBottom, boxTop float64) (Edge, float64) {
	h := boxTop - boxBottom
	if h <= 0 || minY > maxY || minY < boxBottom || maxY > boxTop {
		return EdgeNone, 0
	}
	band := h * marginBandRatio
	switch {
	case minY >= boxTop-band:
		return EdgeTop, boxTop - maxY
	case maxY <= boxBottom+band:
		return EdgeBottom, minY - boxBottom
	}
	return EdgeNone, 0
}

// Line is one line of text on one page, as [RunningLines] reads it.
type Line struct {
	// Page identifies the page the line is on. Any numbering works, as long
	// as it is the same for every line of one page.
	Page int
	// Text is the line's text, in any form that compares equal across pages
	// when the line repeats.
	Text string
	// Edge is the margin band the line sits in (see [MarginEdge]).
	Edge Edge
	// Offset is the line's distance from that edge, in points.
	Offset float64
}

// RunningOptions sets how strictly [RunningLines] reads a repeat.
type RunningOptions struct {
	// MinPages is the number of distinct pages a text must appear on to
	// repeat. Values below 2 read as 2: a text on one page repeats nothing.
	MinPages int
	// PositionTolerance, when positive, compares positions too: only lines
	// in a margin band count, and a repeat must sit in the same band within
	// this many points of the same distance from the edge. Zero compares the
	// text alone, wherever it sits.
	PositionTolerance float64
	// FoldCase compares the text ignoring ASCII case.
	FoldCase bool
	// FoldDigits compares every run of ASCII digits as equal, so "Page 3" and
	// "Page 14" repeat.
	FoldDigits bool
}

// RunningLines reports, for each line, whether it is running page furniture:
// a header, a footer or a page number that repeats on at least
// opts.MinPages distinct pages. Running heads and page numbers are pagination
// artifacts (ISO 32000-1, 14.8.2.2.2), not content, so a structure pass marks
// them as such (ISO 14289-1, 7.8) and an outline pass leaves them out.
//
// With a position tolerance, the repeats of one text form one cluster: the
// lines within the tolerance of the distance from the edge that gathers the
// most pages. A text that repeats at scattered positions is a quotation, not
// a running head, and stays content.
func RunningLines(lines []Line, opts RunningOptions) []bool {
	running := make([]bool, len(lines))
	minPages := max(opts.MinPages, 2)
	positional := opts.PositionTolerance > 0

	type groupKey struct {
		edge Edge
		text string
	}
	groups := make(map[groupKey][]int)
	var order []groupKey
	for i, l := range lines {
		if positional && l.Edge == EdgeNone {
			continue
		}
		text := foldRunningText(l.Text, opts)
		if text == "" {
			continue
		}
		k := groupKey{text: text}
		if positional {
			k.edge = l.Edge
		}
		if _, seen := groups[k]; !seen {
			order = append(order, k)
		}
		groups[k] = append(groups[k], i)
	}

	for _, k := range order {
		idx := groups[k]
		if !positional {
			if distinctPages(lines, idx) >= minPages {
				for _, i := range idx {
					running[i] = true
				}
			}
			continue
		}
		lo, hi := densestCluster(lines, idx, opts.PositionTolerance)
		if hi-lo > 0 && distinctPages(lines, idx[lo:hi]) >= minPages {
			for _, i := range idx[lo:hi] {
				running[i] = true
			}
		}
	}
	return running
}

// densestCluster sorts idx by offset in place and returns the run [lo, hi)
// of it, centred on one line's offset and spanning tol on either side, that
// covers the most distinct pages. The window edges only ever move forward, so
// the scan is linear after the sort, whatever the number of repeats.
func densestCluster(lines []Line, idx []int, tol float64) (int, int) {
	sort.SliceStable(idx, func(a, b int) bool { return lines[idx[a]].Offset < lines[idx[b]].Offset })
	pages := make(map[int]int)
	bestLo, bestHi, best := 0, 0, 0
	lo, hi := 0, 0
	for c := range idx {
		center := lines[idx[c]].Offset
		for hi < len(idx) && lines[idx[hi]].Offset <= center+tol {
			pages[lines[idx[hi]].Page]++
			hi++
		}
		for lines[idx[lo]].Offset < center-tol {
			p := lines[idx[lo]].Page
			if pages[p]--; pages[p] == 0 {
				delete(pages, p)
			}
			lo++
		}
		if len(pages) > best {
			bestLo, bestHi, best = lo, hi, len(pages)
		}
	}
	return bestLo, bestHi
}

// distinctPages counts the pages the lines at idx sit on.
func distinctPages(lines []Line, idx []int) int {
	seen := make(map[int]struct{}, len(idx))
	for _, i := range idx {
		seen[lines[i].Page] = struct{}{}
	}
	return len(seen)
}

// foldRunningText returns the form of s that [RunningLines] compares: trimmed,
// and folded as opts asks.
func foldRunningText(s string, opts RunningOptions) string {
	s = strings.TrimSpace(s)
	if opts.FoldCase {
		s = strings.ToLower(s)
	}
	if !opts.FoldDigits {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	inDigits := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= '0' && c <= '9' {
			if !inDigits {
				b.WriteByte('#')
			}
			inDigits = true
			continue
		}
		inDigits = false
		b.WriteByte(c)
	}
	return b.String()
}

// IsPageNumber reports whether text reads as the bare page number n and
// nothing else: "7", "- 7 -", "Page 7", "page 7 of 12" or "7 / 12", ignoring
// spaces. Only ASCII digits count, so a number drawn in a font whose codes
// are not ASCII reads as no page number, the safe side for a caller that
// would otherwise take content for furniture.
func IsPageNumber(text string, n int) bool {
	if n < 1 {
		return false
	}
	s := strings.ToLower(strings.Join(strings.Fields(text), ""))
	s = strings.TrimPrefix(s, "page")
	s = strings.Trim(s, "-")
	if total, ok := cutTotal(s); ok {
		s = total
	}
	return s == strconv.Itoa(n)
}

// cutTotal returns the number before an " of M" or " / M" total, when s ends
// in one.
func cutTotal(s string) (string, bool) {
	for _, sep := range []string{"of", "/"} {
		before, after, found := strings.Cut(s, sep)
		if !found || after == "" {
			continue
		}
		if allDigits(after) {
			return before, true
		}
	}
	return "", false
}

// allDigits reports whether s is one or more ASCII digits.
func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
