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
// specification lists them, and keeps duplicates: "3,1" returns [3, 1] and
// "1,1" returns [1, 1]. A specification without an included part starts from
// every page, 1 to pageCount. The pages of every excluded part are then
// removed, wherever the exclusion appears in the list: "!2,3,1" returns
// [3, 1]. Excluding a page the inclusions do not name changes nothing. The
// page operations of [Writer] treat a page list as a set, while [Split]
// honors the order and the repeats.
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
// [1, pageCount] in an included or an excluded part, or an empty selection. A
// range is bounded against pageCount before it is expanded, so a huge range
// costs nothing to reject.
//
// ParsePageRanges is safe for concurrent use.
func ParsePageRanges(spec string, pageCount int) ([]int, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, fmt.Errorf("%w: empty specification", ErrInvalidPageRange)
	}

	var (
		included    []int
		hasIncluded bool
		excluded    []bool
	)
	for part := range strings.SplitSeq(spec, ",") {
		part = strings.TrimSpace(part)
		exclude := strings.HasPrefix(part, "!")
		if exclude {
			part = strings.TrimSpace(part[1:])
		}
		if part == "" {
			return nil, fmt.Errorf("%w: empty part in %q", ErrInvalidPageRange, spec)
		}

		first, last, step, err := parsePagePart(part, pageCount)
		if err != nil {
			return nil, err
		}

		if !exclude {
			hasIncluded = true
			for i := first; i <= last; i += step {
				included = append(included, i)
			}
			continue
		}
		if excluded == nil {
			excluded = make([]bool, pageCount+1)
		}
		for i := first; i <= last; i += step {
			excluded[i] = true
		}
	}

	if !hasIncluded {
		for i := 1; i <= pageCount; i++ {
			included = append(included, i)
		}
	}

	pages := included
	if excluded != nil {
		pages = included[:0]
		for _, p := range included {
			if !excluded[p] {
				pages = append(pages, p)
			}
		}
	}

	if len(pages) == 0 {
		return nil, fmt.Errorf("%w: %q selects no pages in a %d-page document", ErrInvalidPageRange, spec, pageCount)
	}
	return pages, nil
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
