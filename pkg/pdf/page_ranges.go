package pdf

import (
	"fmt"
	"strconv"
	"strings"
)

// ParsePageRanges parses a page range specification into 1-based page
// numbers for a document of pageCount pages.
//
// The specification is a comma-separated list of parts. A part is a page, a
// range, or a keyword:
//
//	"3"         → [3]
//	"2-4"       → [2, 3, 4]
//	"3-"        → [3, 4, ..., pageCount]
//	"-3"        → [1, 2, 3]
//	"even"      → [2, 4, 6, ...]
//	"odd"       → [1, 3, 5, ...]
//	"last"      → [pageCount]
//
// A "!" in front of a part excludes its pages:
//
//	"!1"        → every page but the first
//	"odd,!1"    → the odd pages after the first
//	"!last"     → every page but the last
//	"1-3,!2"    → [1, 3]
//
// Keywords are case-insensitive, and whitespace around parts, after "!" and
// around the dash is ignored.
//
// The result lists the pages of the included parts in the order the
// specification lists them, each page once, at the position where the
// specification first names it: "3,1" returns [3, 1], "1,1" returns [1] and
// "3,1-4" returns [3, 1, 2, 4]. A specification without an included part
// starts from every page, 1 to pageCount. The pages of every excluded part
// are then removed, wherever the exclusion appears in the list: "!2,3,1"
// returns [3, 1]. Excluding a page the inclusions do not name changes
// nothing. The result holds at most pageCount pages, however long the
// specification.
//
// A specification that selects no page is an error, never an empty slice:
// [Writer.RotatePages] and the stamps treat an empty page list as every page,
// so an empty result would invert the request. "even" on a one-page
// document, "!1" on a one-page document and "1-3,!1-3" all select nothing.
//
// A keyword cannot be a range endpoint: "1-last" is an error, write "1-"
// instead. "-" alone, "!" alone, "!!1" and the "n" exclusion prefix of other
// tools are not supported.
//
// Every failure wraps [ErrInvalidPageRange]: an empty or malformed
// specification or part, a range whose start exceeds its end, a page outside
// [1, pageCount] in an included or an excluded part, or an empty selection.
// Every part is validated before any is expanded, so a malformed or
// out-of-range part costs nothing to reject, wherever it appears. Expanding
// visits each page a bounded number of times, so the cost grows with the
// length of the specification plus pageCount, not with their product.
//
// ParsePageRanges is safe for concurrent use.
func ParsePageRanges(spec string, pageCount int) ([]int, error) {
	var parts []pagePart
	err := scanPageRanges(spec, pageCount, func(part pagePart) {
		parts = append(parts, part)
	})
	if err != nil {
		return nil, err
	}

	excluded := newPageSet(pageCount)
	hasIncluded := false
	for _, part := range parts {
		if part.exclude {
			excluded.add(part, nil)
		} else {
			hasIncluded = true
		}
	}

	selected := newPageSet(pageCount)
	for p := 1; p <= pageCount; p++ {
		if excluded.has(p) {
			selected.mark(p)
		}
	}

	var pages []int
	collect := func(p int) { pages = append(pages, p) }
	if !hasIncluded {
		selected.add(pagePart{first: 1, last: pageCount, step: 1}, collect)
	}
	for _, part := range parts {
		if !part.exclude {
			selected.add(part, collect)
		}
	}

	if len(pages) == 0 {
		return nil, fmt.Errorf("%w: %q selects no pages in a %d-page document", ErrInvalidPageRange, spec, pageCount)
	}
	return pages, nil
}

// ValidatePageRanges reports whether spec is a valid page range
// specification for a document of pageCount pages, without expanding it: it
// returns the error [ParsePageRanges] returns for a malformed or out-of-range
// part, at a cost that grows with the length of spec alone. A specification
// whose parts are valid but select no page, such as "1-3,!1-3", passes:
// telling it takes the pages, which [ParsePageRanges] resolves.
//
// ValidatePageRanges is safe for concurrent use.
func ValidatePageRanges(spec string, pageCount int) error {
	return scanPageRanges(spec, pageCount, nil)
}

// scanPageRanges validates every part of spec against pageCount, calling
// visit, when not nil, with each one in order. It expands no part.
func scanPageRanges(spec string, pageCount int, visit func(pagePart)) error {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return fmt.Errorf("%w: empty specification", ErrInvalidPageRange)
	}

	for text := range strings.SplitSeq(spec, ",") {
		text = strings.TrimSpace(text)
		exclude := strings.HasPrefix(text, "!")
		if exclude {
			text = strings.TrimSpace(text[1:])
		}
		if text == "" {
			return fmt.Errorf("%w: empty part in %q", ErrInvalidPageRange, spec)
		}

		first, last, step, err := parsePagePart(text, pageCount)
		if err != nil {
			return err
		}
		if visit != nil {
			visit(pagePart{first: first, last: last, step: step, exclude: exclude})
		}
	}

	return nil
}

// pagePart is one validated part of a page range specification: the pages
// first, first+step, ..., up to last, included or excluded.
type pagePart struct {
	first, last, step int
	exclude           bool
}

// pageSet is a set of the pages 1 to pageCount that enumerates the pages of
// a part it does not hold yet without walking the ones it does. next[p] leads
// to the first page at or after p that the set does not hold, path
// compression keeping each walk short; the step-2 keywords, "odd" and
// "even", walk their pages directly, at most once each.
type pageSet struct {
	held      []bool
	next      []int
	odd, even bool
}

func newPageSet(pageCount int) *pageSet {
	s := &pageSet{
		held: make([]bool, pageCount+2),
		next: make([]int, pageCount+2),
	}
	for p := range s.next {
		s.next[p] = p
	}
	return s
}

func (s *pageSet) has(p int) bool {
	return s.held[p]
}

// mark adds page p to the set.
func (s *pageSet) mark(p int) {
	s.held[p] = true
	s.next[p] = p + 1
}

// free returns the first page at or after p that the set does not hold.
func (s *pageSet) free(p int) int {
	root := p
	for s.next[root] != root {
		root = s.next[root]
	}
	for s.next[p] != root {
		s.next[p], p = root, s.next[p]
	}
	return root
}

// add adds the pages of part to the set, calling visit, when not nil, with
// each page the set did not hold yet, in ascending order.
func (s *pageSet) add(part pagePart, visit func(int)) {
	if part.step != 1 {
		seen := &s.even
		if part.first%2 == 1 {
			seen = &s.odd
		}
		if *seen {
			return
		}
		*seen = true
		for p := part.first; p <= part.last; p += part.step {
			if !s.held[p] {
				s.mark(p)
				if visit != nil {
					visit(p)
				}
			}
		}
		return
	}
	for p := s.free(part.first); p <= part.last; p = s.free(p + 1) {
		s.mark(p)
		if visit != nil {
			visit(p)
		}
	}
}

// parsePagePart parses one part of a page range specification, its "!"
// already removed, into the pages first, first+step, ..., up to last. An
// empty selection has first > last.
//
// Bounds are checked before the part is expanded: expanding first lets
// "1-10000000" allocate one entry per page it spans on a five-page document
// before reporting the overflow. Anti-DoS guard, not spec-mandated. The bound
// is pageCount itself, and endpoints are checked in the order they are
// written so the first offender reported is an endpoint.
func parsePagePart(part string, pageCount int) (first, last, step int, err error) {
	inRange := func(p int) error {
		if p < 1 || p > pageCount {
			return fmt.Errorf("%w: page %d out of range [1, %d]", ErrInvalidPageRange, p, pageCount)
		}
		return nil
	}
	number := func(s string) (int, error) {
		n, err := strconv.Atoi(s)
		if err != nil {
			return 0, fmt.Errorf("%w: invalid page number %q", ErrInvalidPageRange, s)
		}
		return n, nil
	}

	switch strings.ToLower(part) {
	case "even":
		return 2, pageCount, 2, nil
	case "odd":
		return 1, pageCount, 2, nil
	case "last":
		if pageCount < 1 {
			return 0, 0, 0, fmt.Errorf("%w: document has no pages", ErrInvalidPageRange)
		}
		return pageCount, pageCount, 1, nil
	}

	startText, endText, isRange := strings.Cut(part, "-")
	if !isRange {
		n, err := number(part)
		if err != nil {
			return 0, 0, 0, err
		}
		if err := inRange(n); err != nil {
			return 0, 0, 0, err
		}
		return n, n, 1, nil
	}

	startText, endText = strings.TrimSpace(startText), strings.TrimSpace(endText)
	switch {
	case startText == "" && endText == "":
		return 0, 0, 0, fmt.Errorf("%w: range %q names no endpoint", ErrInvalidPageRange, part)
	case endText == "":
		start, err := number(startText)
		if err != nil {
			return 0, 0, 0, err
		}
		if err := inRange(start); err != nil {
			return 0, 0, 0, err
		}
		return start, pageCount, 1, nil
	case startText == "":
		end, err := number(endText)
		if err != nil {
			return 0, 0, 0, err
		}
		if err := inRange(end); err != nil {
			return 0, 0, 0, err
		}
		return 1, end, 1, nil
	}

	start, err := number(startText)
	if err != nil {
		return 0, 0, 0, err
	}
	end, err := number(endText)
	if err != nil {
		return 0, 0, 0, err
	}
	if start > end {
		return 0, 0, 0, fmt.Errorf("%w: range %d-%d starts after it ends", ErrInvalidPageRange, start, end)
	}
	if err := inRange(start); err != nil {
		return 0, 0, 0, err
	}
	if err := inRange(end); err != nil {
		return 0, 0, 0, err
	}
	return start, end, 1, nil
}
