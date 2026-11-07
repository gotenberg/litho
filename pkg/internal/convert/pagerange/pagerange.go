// Package pagerange parses the page selection of a conversion and applies
// it to a laid-out document. Every converter takes the one grammar, that
// of Chromium's printToPDF pageRanges
// (print_to_pdf::TextPageRangesToPageRanges,
// components/printing/browser/print_to_pdf/pdf_print_utils.cc), so a
// selection means the same for an HTML, a DOCX or an ODT source.
package pagerange

import (
	"cmp"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/intconv"
)

// ErrInvalid reports a selection that does not parse or keeps no page.
// The converters wrap it in their own invalid-options error.
var ErrInvalid = errors.New("pagerange: invalid page ranges")

// LastPage is the end of an open range ("5-", "-"): past any page count,
// as Chromium's printing::PageRange::kMaxPage + 1 is.
const LastPage = math.MaxUint32

// MaxRanges bounds the ranges of one selection. Anti-DoS guard, not
// spec-mandated: [Select] walks each range up to the page count, and a
// selection lists a handful of them.
const MaxRanges = 1000

// Range is a range of 1-based page numbers, From and To included.
type Range struct {
	From, To uint64
}

// Parse parses spec with Chromium's grammar: parts separated by commas,
// each trimmed of ASCII whitespace, an empty part skipped; a part is a
// page "N", a range "N-M" whose two numbers may have whitespace around the
// dash, an open range "N-" to the last page, "-M" from the first, or "-"
// for every page. A number is an unsigned 32-bit decimal with an optional
// plus sign and nothing else (base::StringToUint). A part that is none of
// these, a range that starts at zero or after its end, and more than
// [MaxRanges] parts fail with [ErrInvalid]. No part at all selects every
// page: the result is empty.
//
// Its work is linear in len(spec).
func Parse(spec string) ([]Range, error) {
	var out []Range
	for part := range strings.SplitSeq(spec, ",") {
		part = strings.Trim(part, asciiSpace)
		if part == "" {
			continue
		}
		if len(out) == MaxRanges {
			return nil, fmt.Errorf("%w: more than %d page ranges", ErrInvalid, MaxRanges)
		}
		var r Range
		var ok bool
		switch {
		case !strings.Contains(part, "-"):
			r.From, ok = parseUint32(part)
			r.To = r.From
		case part == "-":
			r, ok = Range{From: 1, To: LastPage}, true
		case strings.HasPrefix(part, "-"):
			r.From = 1
			r.To, ok = parseUint32(part[1:])
		case strings.HasSuffix(part, "-"):
			r.From, ok = parseUint32(part[:len(part)-1])
			r.To = LastPage
		default:
			var ends []string
			for e := range strings.SplitSeq(part, "-") {
				if e = strings.Trim(e, asciiSpace); e != "" {
					ends = append(ends, e)
				}
			}
			if len(ends) == 2 {
				var okTo bool
				r.From, ok = parseUint32(ends[0])
				r.To, okTo = parseUint32(ends[1])
				ok = ok && okTo
			}
		}
		if !ok {
			return nil, fmt.Errorf("%w: page range %q is not a page, a range or an open range", ErrInvalid, part)
		}
		if r.From < 1 || r.From > r.To {
			return nil, fmt.Errorf("%w: page range %q starts at zero or after its end", ErrInvalid, part)
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

// Select returns the 0-based indexes of the pages of a document of n
// pages that ranges select, in ascending order, each once: Chromium sorts
// and merges the ranges (printing::PageRange::Normalize) and walks them
// up to the last page (printing::PageNumber). No ranges select every
// page. A selection that keeps no page fails with [ErrInvalid], as
// printToPDF fails with "Page range exceeds page count".
func Select(ranges []Range, n int) ([]int, error) {
	if len(ranges) == 0 {
		out := make([]int, n)
		for i := range out {
			out[i] = i
		}
		return out, nil
	}
	sorted := slices.Clone(ranges)
	slices.SortFunc(sorted, func(a, b Range) int {
		return cmp.Or(cmp.Compare(a.From, b.From), cmp.Compare(a.To, b.To))
	})
	var out []int
	next := 1
	for _, r := range sorted {
		from := max(intconv.Clamp(r.From), next)
		to := min(intconv.Clamp(r.To), n)
		for p := from; p <= to; p++ {
			out = append(out, p-1)
		}
		next = max(next, to+1)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: the page ranges select none of the document's %d pages", ErrInvalid, n)
	}
	return out, nil
}
