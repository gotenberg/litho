package content

import (
	"context"
	"reflect"
	"testing"
)

// operatorsOf returns the operator of every operation data holds, parsed
// with opts.
func operatorsOf(t *testing.T, data string, opts ...Option) []string {
	t.Helper()
	ops, err := ParseStreamData(context.Background(), []byte(data), opts...)
	if err != nil {
		t.Fatalf("ParseStreamData(%q): %v", data, err)
	}
	out := make([]string, len(ops))
	for i, op := range ops {
		out[i] = op.Operator
	}
	return out
}

// TestParseEndsAtStrayArrayClose pins that a parse without an option ends at a
// "]" where an operand or an operator stands, as veraPDF's content stream
// parser does (PDFStreamParser.nextOperator, see [endsReading]), and that a
// "]" closing an array, or read as a dictionary's value, ends nothing.
// [WholeStream], [ForRepair] and [SplitGluedOperators] read on.
func TestParseEndsAtStrayArrayClose(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		src   string
		want  []string
		whole []string
	}{
		{"after an operator", "q Q ] xx", []string{"q", "Q"}, []string{"q", "Q", "xx"}},
		{"between operands", "1 2 ] 3 m 0 0 l", []string{}, []string{"m", "l"}},
		{"against an operator", "q xx] zz", []string{"q", "xx"}, []string{"q", "xx", "zz"}},
		{"at the start", "] q Q", []string{}, []string{"q", "Q"}},
		{"closing an array", "[(a) 1] TJ q", []string{"TJ", "q"}, []string{"TJ", "q"}},
		{"closing nested arrays", "[[1] 2] 0 d q", []string{"d", "q"}, []string{"d", "q"}},
		{"inside a dictionary", "/P <</A ]>> BDC EMC", []string{"BDC", "EMC"}, []string{"BDC", "EMC"}},
		{"after a closed array", "[1] 0 d ] q", []string{"d"}, []string{"d", "q"}},
	} {
		if got := operatorsOf(t, tc.src); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: %q parses as %q, want %q", tc.name, tc.src, got, tc.want)
		}
		for _, opt := range []Option{WholeStream(), ForRepair(), SplitGluedOperators()} {
			if got := operatorsOf(t, tc.src, opt); !reflect.DeepEqual(got, tc.whole) {
				t.Errorf("%s: %q parses whole as %q, want %q", tc.name, tc.src, got, tc.whole)
			}
		}
	}
}

// TestWholeStreamKeepsTheTokens pins that [WholeStream] changes only where the
// parse ends: it reads a stream holding no stray "]" exactly as a parse
// without an option does, operands and offsets included.
func TestWholeStreamKeepsTheTokens(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	for _, src := range []string{
		"q1 0 0 1 0 0 cm 12m Q%c\nBT /F1 1e2 Tf (a) )Tj) ET",
		"0 0 m .020.594 10 l - w [1 2] 0 d /P <</MCID 0>> BDC EMC",
	} {
		ops, ranges, err := ParseStreamDataWithOffsets(ctx, []byte(src))
		if err != nil {
			t.Fatalf("ParseStreamDataWithOffsets(%q): %v", src, err)
		}
		wops, wranges, err := ParseStreamDataWithOffsets(ctx, []byte(src), WholeStream())
		if err != nil {
			t.Fatalf("ParseStreamDataWithOffsets(%q, WholeStream()): %v", src, err)
		}
		if !reflect.DeepEqual(ops, wops) || !reflect.DeepEqual(ranges, wranges) {
			t.Errorf("%q: WholeStream reads %v at %v, want %v at %v", src, wops, wranges, ops, ranges)
		}
	}
}

// TestWithWholeStreamsReadsWhole pins that under a context from
// [WithWholeStreams] every parse reads a stream as [WholeStream] reads it, the
// variants that report more than operations included.
func TestWithWholeStreamsReadsWhole(t *testing.T) {
	t.Parallel()

	ctx := WithWholeStreams(context.Background())
	for _, src := range []string{"q Q ] xx", "1 2 ] 3 m 0 0 l", "<414> Tj ] 99999999999999999999 w"} {
		want, err := ParseStreamData(context.Background(), []byte(src), WholeStream())
		if err != nil {
			t.Fatalf("ParseStreamData(%q, WholeStream()): %v", src, err)
		}
		if got, err := ParseStreamData(ctx, []byte(src)); err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("%q reads as %v, %v, want %v", src, got, err, want)
		}
		if got, _, _, err := ParseStreamDataForCompliance(ctx, []byte(src)); err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("%q: ParseStreamDataForCompliance reads %v, %v, want %v", src, got, err, want)
		}
	}
}
