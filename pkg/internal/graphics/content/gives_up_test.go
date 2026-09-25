package content

import (
	"context"
	"testing"
)

// TestParseGivesUpWhereVeraPDFDoes pins that a parse without an option gives
// no operation at all for a stream holding an array or a dictionary veraPDF
// cannot read ([parser.Parser.WithVeraPDFContainers]), the operators before it
// included, and nothing from the variants that report more than operations,
// while [WholeStream], [ForRepair], [SplitGluedOperators] and a context from
// [WithWholeStreams] read the stream.
func TestParseGivesUpWhereVeraPDFDoes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	for _, src := range []string{
		"q Q [1 yy] TJ q Q",
		"q Q /P <</A 1 xx>> BDC EMC",
		"q Q BI /W 1 /H 1 /Foo 1.5e1 ID \x80 EI q Q",
		"<414> Tj 99999999999999999999 w [1e2] 0 d",
	} {
		if got := operatorsOf(t, src); len(got) != 0 {
			t.Errorf("%q parses as %q, want nothing", src, got)
		}
		ops, overflows, violations, err := ParseStreamDataForCompliance(ctx, []byte(src))
		if err != nil || ops != nil || overflows != nil || violations != nil {
			t.Errorf("%q: ParseStreamDataForCompliance gives %v, %v, %v and %v, want nothing", src, ops, overflows, violations, err)
		}
		if ops, v := ParseStreamDataFormatChecked(ctx, []byte(src)); ops != nil || v != nil {
			t.Errorf("%q: ParseStreamDataFormatChecked gives %v and %v, want nothing", src, ops, v)
		}
		if ops, ranges, err := ParseStreamDataWithOffsets(ctx, []byte(src)); err != nil || ops != nil || ranges != nil {
			t.Errorf("%q: ParseStreamDataWithOffsets gives %v, %v and %v, want nothing", src, ops, ranges, err)
		}
		for _, opt := range []Option{WholeStream(), ForRepair(), SplitGluedOperators()} {
			if got := operatorsOf(t, src, opt); len(got) == 0 {
				t.Errorf("%q parses whole as nothing", src)
			}
		}
		if got, err := ParseStreamData(WithWholeStreams(ctx), []byte(src)); err != nil || len(got) == 0 {
			t.Errorf("%q parses under WithWholeStreams as %v, %v, want the whole stream", src, got, err)
		}
	}
	for _, src := range []string{"q Q /P <</A xx>> BDC EMC", "[(a) - (b)] TJ", "/P <</A 1 2>> BDC EMC"} {
		if got := operatorsOf(t, src); len(got) == 0 {
			t.Errorf("%q parses as nothing, veraPDF reads it", src)
		}
	}
}
