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
	edits := make([]SpanEdit, len(spans))
	for i, s := range spans {
		edits[i] = SpanEdit{Span: s}
	}
	return EditSpans(data, edits)
}

// SpanEdit replaces the bytes of Span with Replacement. An empty Replacement
// removes them.
type SpanEdit struct {
	Span        content.ByteRange
	Replacement []byte
}

// EditSpans is [ExciseSpans] where a span can also be replaced rather than
// removed: removing an operator can change what the operators after it do
// (a show-text operator carries the advance that positions the next one), and
// replacing it with an operator that keeps that effect is what leaves the rest
// of the page where it was. The same rules hold: edits may arrive in any
// order, and a span outside the data or overlapping another is reported.
func EditSpans(data []byte, edits []SpanEdit) ([]byte, error) {
	if len(edits) == 0 {
		return data, nil
	}

	sorted := make([]SpanEdit, len(edits))
	copy(sorted, edits)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Span.Start < sorted[j].Span.Start })

	prevEnd := 0
	grow := 0
	for _, e := range sorted {
		s := e.Span
		if s.Start < 0 || s.Start > s.End || s.End > len(data) {
			return nil, fmt.Errorf("%w: [%d,%d) in %d bytes", ErrSpanOutOfRange, s.Start, s.End, len(data))
		}
		if s.Start < prevEnd {
			return nil, fmt.Errorf("%w: [%d,%d) starts before %d", ErrSpansOverlap, s.Start, s.End, prevEnd)
		}
		prevEnd = s.End
		grow += len(e.Replacement)
	}

	out := make([]byte, 0, len(data)+grow)
	cursor := 0
	for _, e := range sorted {
		out = append(out, data[cursor:e.Span.Start]...)
		out = append(out, e.Replacement...)
		cursor = e.Span.End
	}
	return append(out, data[cursor:]...), nil
}
