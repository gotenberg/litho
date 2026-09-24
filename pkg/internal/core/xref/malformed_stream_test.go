package xref

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// malformedStream builds a cross-reference stream from the dictionary values
// under test. /Size stays 1 throughout: these cases are about /W and /Index,
// and varying a third entry would blur which guard answered.
func malformedStream(w0, w1, w2 int, index ast.PdfArray, body []byte) ast.PdfStream {
	dict := ast.PdfDict{
		"W":    ast.PdfArray{ast.PdfInt(w0), ast.PdfInt(w1), ast.PdfInt(w2)},
		"Size": ast.PdfInt(1),
	}
	if index != nil {
		dict["Index"] = index
	}
	return ast.NewStream(dict, body)
}

// TestParseXRefStream_MalformedDictionaryIsRefusedNotPanicked pins two
// crafted-input panics found by FuzzParseXRefStream.
//
// Both were reachable from a document: Document.parseXRefChain hands
// ParseXRefStream a stream whose dictionary came straight from the file, so
// every value below is attacker controlled.
//
//   - An /Index of odd length. The subsection walk steps i += 2 and reads
//     indexArray[i+1], which indexed past the end. ISO 32000-1, Table 17
//     defines /Index as pairs, so an odd count is malformed by definition.
//   - A negative /W field width. The entry loop slices entryData by w0, w1 and
//     w2, so each negative width broke a different slice expression
//     (entryData[0:w0], entryData[w0:w0+w1], entryData[w0+w1:w0+w1+w2]). The
//     existing "stream data too short" check bounds only the upper end.
//
// pkg/pdf.Open installs a recover, so these surfaced as ErrInternalPanic rather
// than a crash. That boundary is a safety net, not a licence: CLAUDE.md says
// internal code never panics.
//
// The CONTROL cases carry the test. Guards that refused everything would
// satisfy every rejection above while breaking every real document, and only a
// stream that still parses proves otherwise.
func TestParseXRefStream_MalformedDictionaryIsRefusedNotPanicked(t *testing.T) {
	t.Parallel()

	// One type-1 entry: type 1, offset 0x0A, generation 0, under /W [1 2 1].
	body := []byte{0x01, 0x00, 0x0A, 0x00}
	evenIndex := ast.PdfArray{ast.PdfInt(0), ast.PdfInt(1)}

	refused := []struct {
		name   string
		stream ast.PdfStream
	}{
		{"/Index of odd length", malformedStream(1, 2, 1, ast.PdfArray{ast.PdfInt(0)}, body)},
		{"negative /W[0]", malformedStream(-1, 2, 1, evenIndex, body)},
		{"negative /W[1]", malformedStream(1, -8, 1, evenIndex, body)},
		{"negative /W[2]", malformedStream(1, 2, -4, evenIndex, body)},
		{"/W[0] past the int64 a field decodes into", malformedStream(1<<20, 2, 1, evenIndex, body)},
		{"every /W width oversized", malformedStream(1<<20, 1<<20, 1<<20, evenIndex, body)},
		// The case maxXRefFieldWidth actually exists for, and the only one of
		// these that pins it. Merely LARGE widths are caught downstream by the
		// "stream data too short" check, so they pass with or without the
		// ceiling; three widths near MaxInt/2 sum to a NEGATIVE entrySize, which
		// that check cannot catch (it bounds the upper end only) and which
		// reaches data[offset : offset+entrySize] as a negative slice bound.
		// Measured: without the ceiling this panics with
		// "slice bounds out of range [:-4611686018427387907]".
		{"/W widths that overflow entrySize", malformedStream(math.MaxInt/2, math.MaxInt/2, math.MaxInt/2, evenIndex, body)},
	}
	for _, tt := range refused {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panicked instead of refusing: %v", r)
				}
			}()
			_, err := ParseXRefStream(context.Background(), tt.stream, make(map[int]XRefEntry), nil)
			if !errors.Is(err, ErrMalformedXRef) {
				t.Errorf("err = %v, want errors.Is(err, ErrMalformedXRef)", err)
			}
		})
	}

	accepted := []struct {
		name   string
		stream ast.PdfStream
	}{
		{"well-formed /W and /Index", malformedStream(1, 2, 1, evenIndex, body)},
		{"no /Index, defaulted from /Size", malformedStream(1, 2, 1, nil, body)},
		// entrySize 0 is legal: the entry loop is bounded by the /Index count,
		// and every field falls back to its default.
		{"zero widths", malformedStream(0, 0, 0, evenIndex, body)},
	}
	for _, tt := range accepted {
		t.Run("accepts "+tt.name, func(t *testing.T) {
			entries := make(map[int]XRefEntry)
			if _, err := ParseXRefStream(context.Background(), tt.stream, entries, nil); err != nil {
				t.Fatalf("refused a valid stream: %v", err)
			}
		})
	}
}
