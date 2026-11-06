package html

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/intconv"
)

// lastPage is the end of an open range ("5-", "-"): past any page count,
// as Chromium's printing::PageRange::kMaxPage + 1 is.
const lastPage = math.MaxUint32

// pageRange is a range of 1-based page numbers, from and to included.
type pageRange struct {
	from, to uint64
}

// parsePageRanges parses [Options.PageRanges] with the grammar of
// Chromium's printToPDF pageRanges (print_to_pdf::TextPageRangesToPageRanges,
// components/printing/browser/print_to_pdf/pdf_print_utils.cc): parts
// separated by commas, each trimmed of ASCII whitespace, an empty part
// skipped; a part is a page "N", a range "N-M" whose two numbers may have
// whitespace around the dash, an open range "N-" to the last page, "-M"
// from the first, or "-" for every page. A number is an unsigned 32-bit
// decimal with an optional plus sign and nothing else (base::StringToUint).
// A part that is none of these, and a range that starts at zero or after
// its end, fail with [ErrInvalidOptions]. No part at all selects every
// page: the result is empty.
//
// Its work is linear in len(spec).
func parsePageRanges(spec string) ([]pageRange, error) {
	var out []pageRange
	for part := range strings.SplitSeq(spec, ",") {
		part = strings.Trim(part, asciiSpace)
		if part == "" {
			continue
		}
		var r pageRange
		var ok bool
		switch {
		case !strings.Contains(part, "-"):
			r.from, ok = parseUint32(part)
			r.to = r.from
		case part == "-":
			r, ok = pageRange{from: 1, to: lastPage}, true
		case strings.HasPrefix(part, "-"):
			r.from = 1
			r.to, ok = parseUint32(part[1:])
		case strings.HasSuffix(part, "-"):
			r.from, ok = parseUint32(part[:len(part)-1])
			r.to = lastPage
		default:
			var ends []string
			for e := range strings.SplitSeq(part, "-") {
				if e = strings.Trim(e, asciiSpace); e != "" {
					ends = append(ends, e)
				}
			}
			if len(ends) == 2 {
				var okTo bool
				r.from, ok = parseUint32(ends[0])
				r.to, okTo = parseUint32(ends[1])
				ok = ok && okTo
			}
		}
		if !ok {
			return nil, fmt.Errorf("%w: page range %q is not a page, a range or an open range", ErrInvalidOptions, part)
		}
		if r.from < 1 || r.from > r.to {
			return nil, fmt.Errorf("%w: page range %q starts at zero or after its end", ErrInvalidOptions, part)
		}
		out = append(out, r)
	}
	return out, nil
}

// asciiSpace is the whitespace base::TRIM_WHITESPACE trims from an 8-bit
// string: the ASCII whitespace of Chromium's base::kWhitespaceASCII.
const asciiSpace = " \t\n\v\f\r"

// parseUint32 parses s as base::StringToUint does: an optional plus sign
// then decimal digits, nothing else, at most 2^32 - 1.
func parseUint32(s string) (uint64, bool) {
	s = strings.TrimPrefix(s, "+")
	if s == "" {
		return 0, false
	}
	var v uint64
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		v = v*10 + uint64(c-'0')
		if v > math.MaxUint32 {
			return 0, false
		}
	}
	return v, true
}

// selectPages returns the 0-based indexes of the pages of a document of n
// pages that ranges select, in ascending order, each once: Chromium sorts
// and merges the ranges (printing::PageRange::Normalize) and walks them
// up to the last page (printing::PageNumber). No ranges select every
// page. A selection that keeps no page fails with [ErrInvalidOptions], as
// printToPDF fails with "Page range exceeds page count".
func selectPages(ranges []pageRange, n int) ([]int, error) {
	if len(ranges) == 0 {
		out := make([]int, n)
		for i := range out {
			out[i] = i
		}
		return out, nil
	}
	sorted := slices.Clone(ranges)
	slices.SortFunc(sorted, func(a, b pageRange) int {
		return cmp.Or(cmp.Compare(a.from, b.from), cmp.Compare(a.to, b.to))
	})
	var out []int
	next := 1
	for _, r := range sorted {
		from := max(intconv.Clamp(r.from), next)
		to := min(intconv.Clamp(r.to), n)
		for p := from; p <= to; p++ {
			out = append(out, p-1)
		}
		next = max(next, to+1)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: the page ranges select none of the document's %d pages", ErrInvalidOptions, n)
	}
	return out, nil
}
