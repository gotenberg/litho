package pdf

import (
	"fmt"
	"strconv"
	"strings"
)

// ParsePageRanges parses a page range specification into 1-based page
// numbers for a document of pageCount pages.
//
// The specification is a comma-separated list of parts, or one keyword:
//
//	"1"         → [1]
//	"1-3"       → [1, 2, 3]
//	"1,3,5"     → [1, 3, 5]
//	"1-3,7-9"   → [1, 2, 3, 7, 8, 9]
//	"even"      → [2, 4, 6, ...]
//	"odd"       → [1, 3, 5, ...]
//	"last"      → [pageCount]
//
// Keywords are case-insensitive, and whitespace around parts is ignored.
//
// The result keeps the order the specification lists pages in, and keeps
// duplicates: "3,1" returns [3, 1] and "1,1" returns [1, 1]. The page
// operations of [Writer] treat a page list as a set, while [Split] honors the
// order and the repeats.
//
// A specification that selects no page is an error, never an empty slice:
// [Writer.RotatePages] and the stamps treat an empty page list as every page,
// so an empty result would invert the request. Only "even" and "odd" can
// select nothing, on documents with fewer than two pages or none.
//
// Every failure wraps [ErrInvalidPageRange]: an empty or malformed
// specification, a range whose start exceeds its end, a page outside
// [1, pageCount], or an empty selection. A range is bounded against pageCount
// before it is expanded, so a huge range costs nothing to reject.
//
// ParsePageRanges is safe for concurrent use.
func ParsePageRanges(spec string, pageCount int) ([]int, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, fmt.Errorf("%w: empty specification", ErrInvalidPageRange)
	}

	selected := func(pages []int) ([]int, error) {
		if len(pages) == 0 {
			return nil, fmt.Errorf("%w: %q selects no pages in a %d-page document", ErrInvalidPageRange, spec, pageCount)
		}
		return pages, nil
	}

	switch strings.ToLower(spec) {
	case "even":
		var pages []int
		for i := 2; i <= pageCount; i += 2 {
			pages = append(pages, i)
		}
		return selected(pages)
	case "odd":
		var pages []int
		for i := 1; i <= pageCount; i += 2 {
			pages = append(pages, i)
		}
		return selected(pages)
	case "last":
		if pageCount < 1 {
			return nil, fmt.Errorf("%w: document has no pages", ErrInvalidPageRange)
		}
		return []int{pageCount}, nil
	}

	// Bounds are checked per part, before the range is expanded: expanding
	// first lets "1-10000000" allocate one entry per page it spans on a
	// five-page document before reporting the overflow. Anti-DoS guard, not
	// spec-mandated. The bound is pageCount itself, and endpoints are checked
	// before their interior so the first offender reported is an endpoint.
	inRange := func(p int) error {
		if p < 1 || p > pageCount {
			return fmt.Errorf("%w: page %d out of range [1, %d]", ErrInvalidPageRange, p, pageCount)
		}
		return nil
	}

	var pages []int
	for part := range strings.SplitSeq(spec, ",") {
		part = strings.TrimSpace(part)
		if strings.Contains(part, "-") {
			bounds := strings.SplitN(part, "-", 2)
			start, err := strconv.Atoi(strings.TrimSpace(bounds[0]))
			if err != nil {
				return nil, fmt.Errorf("%w: invalid page number %q", ErrInvalidPageRange, bounds[0])
			}
			end, err := strconv.Atoi(strings.TrimSpace(bounds[1]))
			if err != nil {
				return nil, fmt.Errorf("%w: invalid page number %q", ErrInvalidPageRange, bounds[1])
			}
			if start > end {
				return nil, fmt.Errorf("%w: range %d-%d starts after it ends", ErrInvalidPageRange, start, end)
			}
			if err := inRange(start); err != nil {
				return nil, err
			}
			if err := inRange(end); err != nil {
				return nil, err
			}
			for i := start; i <= end; i++ {
				pages = append(pages, i)
			}
		} else {
			n, err := strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("%w: invalid page number %q", ErrInvalidPageRange, part)
			}
			if err := inRange(n); err != nil {
				return nil, err
			}
			pages = append(pages, n)
		}
	}

	return pages, nil
}
