package docx

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
)

// maxRangeParts caps the parts of a page range specification. Anti-DoS
// guard, not spec-mandated: a selection lists a handful of ranges.
const maxRangeParts = 1000

// selectPages returns the 0-based indexes of the pages of a document of n
// pages that spec selects (office.Options.PageRanges: "1-5, 8, 11-13",
// one-based, an open end "8-" running to the last page), ascending and
// each once. An empty spec selects every page. A part that does not
// parse, a range that starts at zero or after its end, and a selection
// that keeps no page fail with office.ErrInvalidOptions; a range past the
// last page is cut at it.
func selectPages(spec string, n int) ([]int, error) {
	keep := make([]bool, n)
	parts := strings.Split(spec, ",")
	if len(parts) > maxRangeParts {
		return nil, fmt.Errorf("%w: more than %d page ranges", office.ErrInvalidOptions, maxRangeParts)
	}
	selected := false
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		selected = true
		from, to, err := parseRange(part, n)
		if err != nil {
			return nil, err
		}
		for p := from; p <= min(to, n); p++ {
			keep[p-1] = true
		}
	}
	var out []int
	for i, k := range keep {
		if k || !selected {
			out = append(out, i)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: the page ranges %q select none of the document's %d pages", office.ErrInvalidOptions, spec, n)
	}
	return out, nil
}

// parseRange parses one part of a page range specification.
func parseRange(part string, n int) (int, int, error) {
	num := func(s string) (int, bool) {
		v, err := strconv.ParseUint(strings.TrimSpace(s), 10, 31)
		return int(v), err == nil // fits int: at most 2^31-1 by ParseUint's bit size
	}
	a, b, isRange := strings.Cut(part, "-")
	from, ok := num(a)
	to := from
	if isRange {
		if strings.TrimSpace(b) == "" {
			to = max(n, from)
		} else {
			var okTo bool
			to, okTo = num(b)
			ok = ok && okTo
		}
	}
	if !ok || from < 1 || from > to {
		return 0, 0, fmt.Errorf("%w: page range %q is not a page or a range of pages", office.ErrInvalidOptions, part)
	}
	return from, to, nil
}
