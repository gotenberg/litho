package content

import (
	"context"
	"reflect"
	"strings"
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
	// Operators written with no white space between them, which
	// SplitGluedOperators splits, and a truncated one.
	"BT /F1 24 Tf 50 500 Td (a) Tj ETBT /F1 1e2 Tf 50 600 Td (b) Tj ET\n",
	"qQ BTET ETXX /F1 12 TfT* T*Tf q1 0 0 1 0 0cm /P <<>> BDC EMC\n",
	"BT (a) TjET",
	// Numbers veraPDF ends before a letter or saturates, which ForRepair keeps
	// whole, and a truncated one.
	"0 0 m .020.594 10 l 12m - 5 w --5 w 1e2 w 55.8492#7136 0 Td 5-3 m\n",
	"0 0 m 1.5.",
	// Operators veraPDF reads on through a delimiter, and a truncated one.
	"q Q%comment\nBT (a) T)Tj ET q Q>> /P <</A xx>> BDC EMC [xx)] 0 d q Q{}\n",
	"q Q%c",
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
		_, _ = ParseStreamData(context.Background(), data, SplitGluedOperators())
		_, _ = ParseStreamData(context.Background(), data, ForRepair())
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
		for _, opts := range [][]Option{nil, {SplitGluedOperators()}, {ForRepair()}} {
			ops, ranges, err := ParseStreamDataWithOffsets(context.Background(), data, opts...)
			if err != nil {
				continue
			}
			// Every operation must carry a range, or a caller splicing by
			// index reads past the slice.
			if len(ranges) != len(ops) {
				t.Fatalf("got %d operations but %d byte ranges", len(ops), len(ranges))
			}
			for i, r := range ranges {
				if r.Start < 0 || r.End > len(data) || r.Start > r.End {
					t.Fatalf("operation %d has range [%d,%d) outside a %d-byte stream",
						i, r.Start, r.End, len(data))
				}
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
// ([parser.Parser.IntegerOverflows]), so it carries a code path the first three
// never enter. Compliance reaches it on attacker-controlled content from two
// places (check.go, file_limits.go), and an overflowing literal is exactly what
// a crafted stream supplies.
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

// FuzzParseStreamDataForCompliance covers the variant compliance reaches from
// content_state.go and graphics.go on the form XObject, pattern, Type3 glyph and
// appearance streams of a page, and asserts what its callers rely on: format
// checking records violations and changes nothing else, so the operations, the
// overflows and the outcome match [ParseStreamDataWithOverflows] on the same
// bytes; and placing the violations loses or repeats none, so they match
// [ParseStreamDataFormatChecked] one for one, and only a violation with an
// operand can hold the last one.
func FuzzParseStreamDataForCompliance(f *testing.F) {
	for _, s := range seedStreams {
		f.Add([]byte(s))
	}
	f.Add([]byte("99999999999999999999 0 0 1 0 0 cm\n"))
	f.Add([]byte("1.5e1 w [2.5e1 3] 0 d 1e1 w <4g8> Tj\n"))
	f.Add([]byte("1.5E-1 w <48"))
	f.Add([]byte("<414> BI /W 1 /H 1 /F <4G> ID \x80 EI <434> zz <4"))

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxFuzzInput {
			return
		}
		ops, overflows, violations, err := ParseStreamDataForCompliance(context.Background(), data)
		wantOps, wantOverflows, wantErr := ParseStreamDataWithOverflows(context.Background(), data)
		if (err == nil) != (wantErr == nil) {
			t.Fatalf("error = %v, want %v", err, wantErr)
		}
		if !reflect.DeepEqual(ops, wantOps) || !reflect.DeepEqual(overflows, wantOverflows) {
			t.Fatalf("format checking changed the parse: %d operations and %v overflows, want %d and %v",
				len(ops), overflows, len(wantOps), wantOverflows)
		}
		_, wantViolations := ParseStreamDataFormatChecked(context.Background(), data)
		if len(violations) != len(wantViolations) {
			t.Fatalf("placed %d violations, the parser recorded %d", len(violations), len(wantViolations))
		}
		for i, v := range violations {
			if v.Err.Error() != wantViolations[i].Error() {
				t.Fatalf("violation %d is %q, want %q", i, v.Err, wantViolations[i])
			}
			if v.Last && v.Operand == nil {
				t.Fatalf("violation %d is placed on the last operand of %q, and on no operand", i, v.Operator)
			}
		}
	})
}

// FuzzGluedOperators covers the split of one keyword into operators, which the
// unknown-operator repair runs on keywords read from attacker-controlled
// content, and asserts the split loses and adds nothing: the operators it
// returns spell the keyword.
func FuzzGluedOperators(f *testing.F) {
	for _, s := range []string{"ETBT", "qQ", "BTET", "ETXX", "TfT*", "q1", "falsef", "BDX", "ETB", ""} {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, keyword string) {
		if len(keyword) > maxFuzzInput {
			return
		}
		parts := GluedOperators(keyword)
		if parts == nil {
			return
		}
		if joined := strings.Join(parts, ""); joined != keyword {
			t.Fatalf("GluedOperators(%q) = %q, which spells %q", keyword, parts, joined)
		}
		for _, p := range parts {
			if !IsOperator(p) {
				t.Fatalf("GluedOperators(%q) returned %q, no operator", keyword, p)
			}
		}
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
