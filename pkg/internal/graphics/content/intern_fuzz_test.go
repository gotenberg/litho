package content

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// internReadings are the readings whose parses [FuzzInterningReadsTheSame]
// compares with and without interning.
var internReadings = [][]Option{nil, {SplitGluedOperators()}, {ForRepair()}, {WholeStream()}, {InlineImageData()}}

// readInterned returns, printed, what one parse of data under o returns: its
// operations, the integer overflows and the format violations the parser
// records, placed, and the error. intern chooses whether the parse shares the
// operands it reads again.
func readInterned(ctx context.Context, data []byte, o readOptions, intern bool) string {
	p := newParser(data, o, intern).WithFormatChecking()
	placer := &violationPlacer{p: p}
	ops, err := parseOperations(ctx, p, o, data, nil, placer)
	var b strings.Builder
	fmt.Fprintf(&b, "%#v\n%#v\n%v\n", ops, p.IntegerOverflows, err)
	for _, v := range placer.finish() {
		fmt.Fprintf(&b, "%v %q %#v %v %q\n", v.Err, v.Operator, v.Operand, v.Last, v.Key)
	}
	return b.String()
}

// FuzzInterningReadsTheSame asserts that a parse that shares the texts and the
// objects of the operands it reads again reads what a parse that does not
// reads, under every reading, the format violations and integer overflows the
// parser records included.
func FuzzInterningReadsTheSame(f *testing.F) {
	for _, s := range seedStreams {
		f.Add([]byte(s))
	}
	f.Add([]byte("1.5e2 1.5e2 w 99999999999999999999 99999999999999999999 w /F1 1 Tf /F1 1 Tf 0.5 0.5 g"))
	f.Add([]byte("/F1 1 Tf /F1"))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxFuzzInput {
			return
		}
		ctx := context.Background()
		for _, opts := range internReadings {
			o := optionsOf(ctx, opts)
			plain := readInterned(ctx, data, o, false)
			interned := readInterned(ctx, data, o, true)
			if plain != interned {
				t.Fatalf("options %d: interned parse reads\n%s\nwhere a plain one reads\n%s", len(opts), interned, plain)
			}
		}
	})
}

// TestInterningRecordsEachOccurrence locks that a number whose reading records
// a format violation or an integer overflow records it at each occurrence when
// the parse shares the operands it reads again, and that a repeated operand
// reads as the same value.
func TestInterningRecordsEachOccurrence(t *testing.T) {
	data := []byte("1.5e2 1.5e2 w 99999999999999999999 99999999999999999999 w /F1 1 Tf /F1 1 Tf")
	ctx := context.Background()
	o := optionsOf(ctx, []Option{ForRepair()})
	if plain, interned := readInterned(ctx, data, o, false), readInterned(ctx, data, o, true); plain != interned {
		t.Fatalf("interned parse reads\n%s\nwhere a plain one reads\n%s", interned, plain)
	}

	p := newParser(data, o, true).WithFormatChecking()
	ops, err := parseOperations(ctx, p, o, data, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(p.FormatViolations); got != 2 {
		t.Errorf("recorded %d exponent violations, want 2", got)
	}
	if got := len(p.IntegerOverflows); got != 2 {
		t.Errorf("recorded %d integer overflows, want 2", got)
	}
	if len(ops) != 4 || ops[2].Operands[0] != ops[3].Operands[0] {
		t.Errorf("repeated font name reads %#v", ops)
	}
}
