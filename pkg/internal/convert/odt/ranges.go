package odt

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
)

// maxRanges bounds the ranges of a page selection: 1,000. Anti-DoS
// guard, not spec-mandated: a selection lists a handful.
const maxRanges = 1000

// pageRange is one range of a selection, 1-based and inclusive.
type pageRange struct{ from, to int }

// parsePageRanges parses office.Options.PageRanges, "1-5, 8, 11-13": nil
// for an empty selection, office.ErrInvalidOptions for a malformed one or
// a range whose start exceeds its end.
func parsePageRanges(s string) ([]pageRange, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	var out []pageRange
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if len(out) >= maxRanges {
			return nil, fmt.Errorf("%w: more than %d page ranges", office.ErrInvalidOptions, maxRanges)
		}
		from, to, isRange := strings.Cut(part, "-")
		a, err := strconv.Atoi(strings.TrimSpace(from))
		if err != nil || a < 1 {
			return nil, fmt.Errorf("%w: page range %q", office.ErrInvalidOptions, part)
		}
		bnd := a
		if isRange {
			if bnd, err = strconv.Atoi(strings.TrimSpace(to)); err != nil || bnd < 1 {
				return nil, fmt.Errorf("%w: page range %q", office.ErrInvalidOptions, part)
			}
		}
		if a > bnd {
			return nil, fmt.Errorf("%w: page range %q starts past its end", office.ErrInvalidOptions, part)
		}
		out = append(out, pageRange{a, bnd})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: page ranges %q select nothing", office.ErrInvalidOptions, s)
	}
	return out, nil
}

// selectPages returns the 0-based indexes of the pages of a document of
// total pages the ranges keep, in document order, each once: every page
// for no ranges, office.ErrInvalidOptions when they keep none.
func selectPages(ranges []pageRange, total int) ([]int, error) {
	if ranges == nil {
		out := make([]int, total)
		for i := range out {
			out[i] = i
		}
		return out, nil
	}
	keep := make([]bool, total)
	for _, r := range ranges {
		for p := r.from; p <= min(r.to, total); p++ {
			keep[p-1] = true
		}
	}
	var out []int
	for i, k := range keep {
		if k {
			out = append(out, i)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: the page ranges keep none of %d pages", office.ErrInvalidOptions, total)
	}
	return out, nil
}
