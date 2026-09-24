package content

import (
	"context"
	"testing"
)

// maxFuzzInput bounds a fuzz case so the corpus stays in the size range real
// content streams occupy; the parser's own limits are exercised by the unit
// tests, not by feeding it a megabyte of noise.
const maxFuzzInput = 1 << 20

// seedStreams are the shapes a content stream takes: operands before operator,
// nested constructs, inline images, and the malformed spellings damaged files
// carry. Truncated variants matter as much as valid ones, since a stream cut
// short mid-token is exactly what a corrupt /Length produces.
var seedStreams = []string{
	"BT /F1 12 Tf (hello) Tj ET\n",
	"q 1 0 0 1 10 10 cm /Im0 Do Q\n",
	"0.5 0.5 0.5 rg 10 10 100 100 re f\n",
	"BT [(a) -250 (b)] TJ ET\n",
	"BI /W 2 /H 2 /CS /RGB /BPC 8 ID \x00\x01\x02\x03\x04\x05\x06\x07\x08\x09\x0a\x0b EI\n",
	"/OC /MC0 BDC BT (tagged) Tj ET EMC\n",
	"BT /F1 12 Tf (unterminated",
	"q q q q q q q q\n",
	"<</Type/Foo>> BDC EMC\n",
	"1 0 0 1 0 0 cm",
	"",
}

// FuzzParseStreamData feeds arbitrary bytes to the content stream parser and
// asserts crash-freedom.
//
// A content stream is attacker-controlled: its bytes come straight out of the
// PDF, after whatever filter chain the file chose. That makes this the most
// exposed parser after the lexer, and [ParseStreamData] is the entry point every
// text, image and graphics-state walk goes through.
func FuzzParseStreamData(f *testing.F) {
	for _, s := range seedStreams {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxFuzzInput {
			return
		}
		_, _ = ParseStreamData(context.Background(), data)
	})
}

// FuzzParseStreamDataWithOffsets covers the variant that also reports each
// operation's byte range, which redaction and content editing rely on to splice
// a stream back together.
func FuzzParseStreamDataWithOffsets(f *testing.F) {
	for _, s := range seedStreams {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxFuzzInput {
			return
		}
		ops, ranges, err := ParseStreamDataWithOffsets(context.Background(), data)
		if err != nil {
			return
		}
		// Every operation must carry a range, or a caller splicing by index
		// reads past the slice.
		if len(ranges) != len(ops) {
			t.Fatalf("got %d operations but %d byte ranges", len(ops), len(ranges))
		}
		for i, r := range ranges {
			if r.Start < 0 || r.End > len(data) || r.Start > r.End {
				t.Fatalf("operation %d has range [%d,%d) outside a %d-byte stream",
					i, r.Start, r.End, len(data))
			}
		}
	})
}

// FuzzParseStreamDataFormatChecked covers the format-checking variant, which
// runs against damaged files by design: it reports syntax violations rather
// than refusing the stream, so it sees the malformed input the others reject.
func FuzzParseStreamDataFormatChecked(f *testing.F) {
	for _, s := range seedStreams {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxFuzzInput {
			return
		}
		_, _ = ParseStreamDataFormatChecked(context.Background(), data)
	})
}

// FuzzParseStreamDataWithOverflows covers the fourth variant, which was the one
// ParseStreamData* entry point with no target of its own.
//
// It is not a wrapper: alongside the operations it returns the value of every
// integer literal the parser had to read as a real because it overflows 64 bits
// ([parser.Parser.IntegerOverflows]), so it carries a code path the other three
// never enter. Compliance reaches it on attacker-controlled content from three
// places (check.go, file_limits.go, content_state.go), and an overflowing
// literal is exactly what a crafted stream supplies.
func FuzzParseStreamDataWithOverflows(f *testing.F) {
	for _, s := range seedStreams {
		f.Add([]byte(s))
	}
	// Literals past 2^63, which is the condition the overflow slice exists to
	// report: a plain seed corpus never produces one.
	f.Add([]byte("99999999999999999999 0 0 1 0 0 cm\n"))
	f.Add([]byte("BT 18446744073709551616 Tz (x) Tj ET\n"))
	f.Add([]byte("-99999999999999999999 w\n"))

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxFuzzInput {
			return
		}
		_, _, _ = ParseStreamDataWithOverflows(context.Background(), data)
	})
}

// Do not assert that overflow values imply operations. It reads like an
// invariant and is false: the slice reports every overflowing literal the
// parser READ, and a trailing operand run with no operator produces no
// operation at all, the same way repairMalformedNumbers drops a trailing run
// that "invokes nothing". Fuzzing found it in under a second with the bare
// literal 09227000000000000000, and `99999999999999999999` reproduces it
// deterministically: ops=0, overflows=1. Adding `0 0 1 0 0 cm` after it gives
// ops=1, overflows=1. The parser is right; the tempting assertion is not.
