package xref

import (
	"context"
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// A cross-reference stream is read straight out of the file, so both its
// dictionary and its body are attacker controlled. ParseXRefStream documents
// ErrMalformedXRef as the answer for every malformed shape, which makes
// "never panics" the whole contract: a crash here happens before a caller can
// decide anything.
//
// No fuzz file in this repository mentioned xref before this one. FuzzParser
// builds its own lexer and never constructs an xref, FuzzHeaderVersion drives
// only the header reader, and compliance's FuzzFileStructureBytes drives
// checkHeaderBytes/findStreamDataStart/streamLengthMatches. The earlier
// coverage sweep missed this because it enumerated EXPORTED []byte-first
// entry points, and ParseXRefStream takes an ast.PdfStream.
//
// The dictionary is what drives every guard, so the fuzzer derives /W, /Size
// and /Index from the input rather than pinning a sane dict: entrySize is
// w0+w1+w2, the entry loop slices entryData by those widths, and the /Index
// walk reads indexArray[i+1] two at a time. Fuzzing only the body would leave
// all of that at a fixed value.

// fuzzXRefStream builds the stream ParseXRefStream is asked to read, taking
// every dictionary value from the fuzzed input.
func fuzzXRefStream(w0, w1, w2, size int, index []int, body []byte) ast.PdfStream {
	dict := ast.PdfDict{
		"W":    ast.PdfArray{ast.PdfInt(w0), ast.PdfInt(w1), ast.PdfInt(w2)},
		"Size": ast.PdfInt(size),
	}
	if index != nil {
		arr := make(ast.PdfArray, 0, len(index))
		for _, v := range index {
			arr = append(arr, ast.PdfInt(v))
		}
		dict["Index"] = arr
	}
	return ast.NewStream(dict, body)
}

// FuzzParseXRefStream drives the PDF 1.5+ compressed cross-reference reader.
//
// Non-panic only. ErrMalformedXRef is the documented outcome for a malformed
// stream, so a rejection is the function working; asserting anything about the
// entries it populates would encode a promise the code does not make.
func FuzzParseXRefStream(f *testing.F) {
	// A well-formed 1-entry table: type 1, offset 0x0A, generation 0.
	f.Add(1, 2, 1, 1, true, 0, 1, []byte{0x01, 0x00, 0x0A, 0x00})
	// Same widths, body too short for the declared count.
	f.Add(1, 2, 1, 4, true, 0, 4, []byte{0x01, 0x00})
	// No /Index, so it defaults to [0 Size].
	f.Add(1, 2, 1, 2, false, 0, 0, []byte{0x01, 0x00, 0x0A, 0x00, 0x01, 0x00, 0x14, 0x00})
	// An /Index with an ODD length: the walk reads indexArray[i+1].
	f.Add(1, 2, 1, 1, true, 3, 0, []byte{0x01, 0x00, 0x0A, 0x00})
	// Negative widths: entryData is sliced by these.
	f.Add(-1, 2, 1, 1, true, 0, 1, []byte{0x01, 0x00, 0x0A, 0x00})
	f.Add(1, -8, 1, 1, true, 0, 1, []byte{0x01, 0x00, 0x0A, 0x00})
	// Widths summing past any plausible body.
	f.Add(1<<20, 1<<20, 1<<20, 1, true, 0, 1, []byte{0x01})
	// Widths that OVERFLOW entrySize to a negative: the "stream data too short"
	// check bounds the upper end only, so a negative sum sails past it into a
	// negative slice bound. Merely large widths do not reach that path.
	f.Add(math.MaxInt/2, math.MaxInt/2, math.MaxInt/2, 1, true, 0, 1, []byte{0x01, 0x00, 0x0A, 0x00})
	// Zero widths: entrySize 0, so the loop never advances offset.
	f.Add(0, 0, 0, 1<<16, true, 0, 1<<16, []byte{})
	// Empty everything.
	f.Add(0, 0, 0, 0, false, 0, 0, []byte{})

	f.Fuzz(func(_ *testing.T, w0, w1, w2, size int, withIndex bool, start, count int, body []byte) {
		// Keep the declared table small: this target probes SHAPE handling,
		// not how long a loop bounded by len(body) takes to refuse.
		const lim = 1 << 12
		if size > lim || size < -lim || count > lim || count < -lim ||
			start > lim || start < -lim || len(body) > lim {
			return
		}

		var index []int
		if withIndex {
			index = []int{start, count}
		}
		stream := fuzzXRefStream(w0, w1, w2, size, index, body)
		_, _ = ParseXRefStream(context.Background(), stream, make(map[int]XRefEntry), nil)

		// An odd-length /Index is the shape the two-at-a-time walk has to
		// survive, and it cannot be expressed through the pair above.
		if withIndex {
			odd := fuzzXRefStream(w0, w1, w2, size, []int{start}, body)
			_, _ = ParseXRefStream(context.Background(), odd, make(map[int]XRefEntry), nil)
		}
	})
}
