package mutations

import (
	"fmt"
	"sort"

	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// ExciseSpans removes the given byte spans from content stream data and
// returns the result, copying every byte outside them verbatim.
//
// Removal is a splice, not a parse and re-emit. litho has no
// operator-to-bytes serializer, and re-emitting the operators it keeps would
// perturb content no caller asked it to touch. Pair it with
// [content.ParseStreamDataWithOffsets], whose ranges run parallel to the
// operations it returns and are contiguous and monotonic, so a run of
// operations spans [first.Start, last.End].
//
// Spans may arrive in any order; the result is assembled in offset order and
// the caller's slice is left as it was. A span outside the data, or one
// overlapping another, is reported rather than clamped or merged: this backs
// content removal, where a caller has to be able to say exactly what left the
// file, and a span adjusted on its behalf removes bytes it never chose. Given
// no spans the data is returned unchanged.
func ExciseSpans(data []byte, spans []content.ByteRange) ([]byte, error) {
	if len(spans) == 0 {
		return data, nil
	}

	sorted := make([]content.ByteRange, len(spans))
	copy(sorted, spans)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Start < sorted[j].Start })

	prevEnd := 0
	for _, s := range sorted {
		if s.Start < 0 || s.Start > s.End || s.End > len(data) {
			return nil, fmt.Errorf("%w: [%d,%d) in %d bytes", ErrSpanOutOfRange, s.Start, s.End, len(data))
		}
		if s.Start < prevEnd {
			return nil, fmt.Errorf("%w: [%d,%d) starts before %d", ErrSpansOverlap, s.Start, s.End, prevEnd)
		}
		prevEnd = s.End
	}

	// Excision only ever shrinks the stream, so the input length bounds the
	// output and one allocation serves.
	out := make([]byte, 0, len(data))
	cursor := 0
	for _, s := range sorted {
		out = append(out, data[cursor:s.Start]...)
		cursor = s.End
	}
	return append(out, data[cursor:]...), nil
}
