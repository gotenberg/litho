package mutations

import (
	"bytes"
	"errors"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

func TestExciseSpans_RemovesOneSpan(t *testing.T) {
	data := []byte("BT /F1 12 Tf (secret) Tj ET")
	// Drop the show-text operator, keeping the text object around it. The span
	// starts at the space before "(", matching the ranges
	// [content.ParseStreamDataWithOffsets] reports: operands plus the operator
	// keyword, including any leading whitespace.
	got, err := ExciseSpans(data, []content.ByteRange{{Start: 12, End: 24}})
	if err != nil {
		t.Fatalf("ExciseSpans: %v", err)
	}

	want := []byte("BT /F1 12 Tf ET")
	if !bytes.Equal(got, want) {
		t.Errorf("ExciseSpans() = %q, want %q", got, want)
	}
}

func TestExciseSpans_UnorderedSpansAssembleInOffsetOrder(t *testing.T) {
	data := []byte("0123456789")
	got, err := ExciseSpans(data, []content.ByteRange{
		{Start: 7, End: 9},
		{Start: 1, End: 3},
	})
	if err != nil {
		t.Fatalf("ExciseSpans: %v", err)
	}

	// "12" and "78" leave; the spans were given out of order.
	want := []byte("034569")
	if !bytes.Equal(got, want) {
		t.Errorf("ExciseSpans() = %q, want %q", got, want)
	}
}

func TestExciseSpans_NoSpansReturnsDataUnchanged(t *testing.T) {
	data := []byte("q 1 0 0 1 0 0 cm Q")
	got, err := ExciseSpans(data, nil)
	if err != nil {
		t.Fatalf("ExciseSpans: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("ExciseSpans() = %q, want the data unchanged", got)
	}
}

func TestExciseSpans_EdgeSpans(t *testing.T) {
	data := []byte("abcdef")

	cases := []struct {
		name string
		span content.ByteRange
		want string
	}{
		{"leading", content.ByteRange{Start: 0, End: 2}, "cdef"},
		{"trailing", content.ByteRange{Start: 4, End: 6}, "abcd"},
		{"whole", content.ByteRange{Start: 0, End: 6}, ""},
		{"empty span", content.ByteRange{Start: 3, End: 3}, "abcdef"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ExciseSpans(data, []content.ByteRange{tc.span})
			if err != nil {
				t.Fatalf("ExciseSpans: %v", err)
			}
			if string(got) != tc.want {
				t.Errorf("ExciseSpans() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestExciseSpans_AdjacentSpansAreNotOverlapping(t *testing.T) {
	data := []byte("abcdef")
	// [1,3) and [3,5) touch but share no byte.
	got, err := ExciseSpans(data, []content.ByteRange{
		{Start: 1, End: 3},
		{Start: 3, End: 5},
	})
	if err != nil {
		t.Fatalf("ExciseSpans: %v", err)
	}
	if string(got) != "af" {
		t.Errorf("ExciseSpans() = %q, want %q", got, "af")
	}
}

func TestExciseSpans_RejectsOutOfRange(t *testing.T) {
	data := []byte("abcdef")

	cases := []struct {
		name string
		span content.ByteRange
	}{
		{"past the end", content.ByteRange{Start: 4, End: 99}},
		{"negative start", content.ByteRange{Start: -1, End: 2}},
		{"end before start", content.ByteRange{Start: 4, End: 2}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ExciseSpans(data, []content.ByteRange{tc.span})
			if !errors.Is(err, ErrSpanOutOfRange) {
				t.Errorf("err = %v, want ErrSpanOutOfRange", err)
			}
		})
	}
}

func TestExciseSpans_RejectsOverlap(t *testing.T) {
	data := []byte("abcdef")
	_, err := ExciseSpans(data, []content.ByteRange{
		{Start: 1, End: 4},
		{Start: 3, End: 5},
	})
	if !errors.Is(err, ErrSpansOverlap) {
		t.Errorf("err = %v, want ErrSpansOverlap", err)
	}
}

func TestExciseSpans_LeavesCallerSlicesAlone(t *testing.T) {
	data := []byte("abcdef")
	dataCopy := bytes.Clone(data)
	spans := []content.ByteRange{
		{Start: 4, End: 6},
		{Start: 0, End: 2},
	}
	spansCopy := make([]content.ByteRange, len(spans))
	copy(spansCopy, spans)

	if _, err := ExciseSpans(data, spans); err != nil {
		t.Fatalf("ExciseSpans: %v", err)
	}

	if !bytes.Equal(data, dataCopy) {
		t.Errorf("data mutated: %q, want %q", data, dataCopy)
	}
	for i := range spans {
		if spans[i] != spansCopy[i] {
			t.Errorf("spans reordered at %d: %+v, want %+v", i, spans[i], spansCopy[i])
		}
	}
}
