package tagging

import (
	"fmt"
	"sort"
)

// markSpan is a resolved region ready to inject. A structure span carries an
// MCID and a role tag and is wrapped in a BDC/EMC sequence linked to a
// structure element; an artifact span (artifact == true) is wrapped in an
// /Artifact BMC/EMC sequence so painting content outside the logical structure
// is still marked, satisfying the "all content marked or artifact" rule.
type markSpan struct {
	start, end int
	mcid       int
	tag        Role
	artifact   bool
}

// injectMarks wraps each span in content with a marked-content sequence:
//
//	/<tag> <</MCID n>> BDC
//	... original bytes ...
//	EMC
//
// It copies the bytes between and around spans verbatim, so the original
// drawing operators are never rewritten, only bracketed. Spans must be
// non-overlapping and within [0, len(content)]; injectMarks returns an
// error otherwise. The input order of spans does not matter; output is
// assembled in offset order.
func injectMarks(content []byte, spans []markSpan) ([]byte, error) {
	if len(spans) == 0 {
		return content, nil
	}

	sorted := make([]markSpan, len(spans))
	copy(sorted, spans)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].start < sorted[j].start })

	prevEnd := 0
	for _, s := range sorted {
		if s.start < 0 || s.start > s.end || s.end > len(content) {
			return nil, fmt.Errorf("mark span [%d,%d) out of range for %d-byte content", s.start, s.end, len(content))
		}
		if s.start < prevEnd {
			return nil, fmt.Errorf("mark span [%d,%d) overlaps a preceding span ending at %d", s.start, s.end, prevEnd)
		}
		prevEnd = s.end
	}

	// Pre-size: original bytes plus a bounded wrapper per span.
	out := make([]byte, 0, len(content)+len(sorted)*48) //nolint:mnd // wrapper is ~30-40 bytes; 48 is a safe estimate
	cursor := 0
	for _, s := range sorted {
		out = append(out, content[cursor:s.start]...)
		if s.artifact {
			out = append(out, "\n/Artifact BMC\n"...)
		} else {
			out = append(out, fmt.Sprintf("\n/%s <</MCID %d>> BDC\n", s.tag, s.mcid)...)
		}
		out = append(out, content[s.start:s.end]...)
		out = append(out, "\nEMC\n"...)
		cursor = s.end
	}
	out = append(out, content[cursor:]...)
	return out, nil
}
