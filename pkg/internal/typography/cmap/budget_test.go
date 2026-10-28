package cmap

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
)

// rangeBomb returns a CMap program of n blocks, each holding one range entry
// of 65536 two-byte codes: about 60 bytes per block that expand to the
// largest range [Parse] and [ParseCID] accept.
func rangeBomb(n int, cid bool) []byte {
	var b strings.Builder
	b.WriteString("1 begincodespacerange <0000> <FFFF> endcodespacerange\n")
	for i := range n {
		if cid {
			fmt.Fprintf(&b, "1 begincidrange <0000> <FFFF> %d endcidrange\n", i)
		} else {
			fmt.Fprintf(&b, "1 beginbfrange <0000> <FFFF> <%04X> endbfrange\n", i)
		}
	}
	return []byte(b.String())
}

// codeSpaceBomb returns a CMap program declaring n one-byte codespace ranges.
func codeSpaceBomb(n int) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "%d begincodespacerange\n", n)
	for i := range n {
		fmt.Fprintf(&b, "<%02X> <%02X>\n", i%256, i%256)
	}
	b.WriteString("endcodespacerange\n")
	return []byte(b.String())
}

// bfrangeArrayBomb returns a bfrange entry whose source range spans 65536
// codes and whose array destination holds a few thousand values, crossing
// every first-byte boundary.
func bfrangeArrayBomb() []byte {
	var b strings.Builder
	b.WriteString("1 begincodespacerange <0000> <FFFF> endcodespacerange\n1 beginbfrange <0000> <FFFF> [")
	for range 4 * ctxCheckInterval {
		b.WriteString("<0041>")
	}
	b.WriteString("] endbfrange\n")
	return []byte(b.String())
}

// useCMapChain returns a CMap program running more usecmap operators than
// [maxUseCMapOps], each naming a predefined CMap whose own chain the bundled
// data follows.
func useCMapChain() []byte {
	var b strings.Builder
	b.WriteString("1 begincodespacerange <0000> <FFFF> endcodespacerange\n")
	for range 4 * maxUseCMapOps {
		b.WriteString("/UniJIS-UCS2-HW-V usecmap\n/Identity-H usecmap\n")
	}
	b.WriteString("1 begincidchar <0041> 7 endcidchar\n")
	return []byte(b.String())
}

// TestUseCMapChainSeedIsBounded pins that the fuzz seed running many usecmap
// operators keeps only [maxUseCMapOps] of them and marks the rest unread.
func TestUseCMapChainSeedIsBounded(t *testing.T) {
	t.Parallel()

	c, err := ParseCID(context.Background(), useCMapChain())
	if err != nil {
		t.Fatalf("ParseCID: %v", err)
	}
	if len(c.useCMaps) != maxUseCMapOps || !c.incomplete {
		t.Errorf("useCMaps = %d, incomplete = %v; want %d, true", len(c.useCMaps), c.incomplete, maxUseCMapOps)
	}
	if m := ParseEmbeddedCIDMap(context.Background(), useCMapChain()); m == nil || len(m.bases) != maxUseCMapOps {
		t.Errorf("ParseEmbeddedCIDMap resolved %v, want %d bases", m, maxUseCMapOps)
	}
}

func TestParseRefusesRangesPastBudget(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	// Four full ranges sit exactly on the budget; a fifth passes it.
	if _, err := Parse(ctx, rangeBomb(maxCMapRangeCodes>>16, false)); err != nil {
		t.Fatalf("Parse of ranges within the budget: %v", err)
	}
	if _, err := Parse(ctx, rangeBomb(maxCMapRangeCodes>>16+1, false)); !errors.Is(err, ErrCMapTooLarge) {
		t.Fatalf("Parse of ranges past the budget: err = %v, want ErrCMapTooLarge", err)
	}
	if _, err := ParseCID(ctx, rangeBomb(maxCMapRangeCodes>>16, true)); err != nil {
		t.Fatalf("ParseCID of ranges within the budget: %v", err)
	}
	if _, err := ParseCID(ctx, rangeBomb(maxCMapRangeCodes>>16+1, true)); !errors.Is(err, ErrCMapTooLarge) {
		t.Fatalf("ParseCID of ranges past the budget: err = %v, want ErrCMapTooLarge", err)
	}
	if m := ParseEmbeddedCIDMap(ctx, rangeBomb(maxCMapRangeCodes>>16+1, true)); m != nil {
		t.Fatal("ParseEmbeddedCIDMap of ranges past the budget returned a map, want nil")
	}
}

// TestParseBudgetBoundsWork proves the bound with a small budget: the parse
// stops at the code that passes it, however many ranges follow.
func TestParseBudgetBoundsWork(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	const budget = 100
	src := []byte("1 begincodespacerange <0000> <FFFF> endcodespacerange\n" +
		"2 beginbfrange <0000> <003F> <0041> <0100> <01FF> <0041> endbfrange\n")
	b := &parseBudget{maxRangeCodes: budget, maxCodeSpaces: maxCodeSpaceRanges}
	if _, err := parse(ctx, src, b); !errors.Is(err, ErrCMapTooLarge) {
		t.Fatalf("parse: err = %v, want ErrCMapTooLarge", err)
	}
	if b.rangeCodes != budget+1 {
		t.Errorf("parse expanded %d codes, want it to stop at %d", b.rangeCodes, budget+1)
	}

	cid := []byte("2 begincidrange <0000> <003F> 1 <0100> <01FF> 1 endcidrange\n")
	b = &parseBudget{maxRangeCodes: budget, maxCodeSpaces: maxCodeSpaceRanges}
	if _, err := parseCID(ctx, cid, b); !errors.Is(err, ErrCMapTooLarge) {
		t.Fatalf("parseCID: err = %v, want ErrCMapTooLarge", err)
	}
	if b.rangeCodes != budget+1 {
		t.Errorf("parseCID expanded %d codes, want it to stop at %d", b.rangeCodes, budget+1)
	}

	b = &parseBudget{maxRangeCodes: budget, maxCodeSpaces: 2}
	if _, err := parse(ctx, codeSpaceBomb(3), b); !errors.Is(err, ErrCMapTooLarge) {
		t.Fatalf("parse of three codespace ranges under a budget of two: err = %v, want ErrCMapTooLarge", err)
	}
}

func TestParseRefusesCodeSpacesPastBudget(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	if c, err := Parse(ctx, codeSpaceBomb(maxCodeSpaceRanges)); err != nil || len(c.CodeSpaceRanges) != maxCodeSpaceRanges {
		t.Fatalf("Parse of %d codespace ranges: err = %v", maxCodeSpaceRanges, err)
	}
	if _, err := Parse(ctx, codeSpaceBomb(maxCodeSpaceRanges+1)); !errors.Is(err, ErrCMapTooLarge) {
		t.Fatalf("Parse of %d codespace ranges: err = %v, want ErrCMapTooLarge", maxCodeSpaceRanges+1, err)
	}
}

// cancelAfterTwoChecks is a context whose first two Err calls report it live
// and every later one cancelled. The parse loop checks once per top-level
// token, the count and the begin keyword, so the parse enters a block before
// it is cancelled.
type cancelAfterTwoChecks struct {
	context.Context
	calls atomic.Int32
}

func (c *cancelAfterTwoChecks) Err() error {
	if c.calls.Add(1) > 2 {
		return context.Canceled
	}
	return nil
}

// TestParseChecksContextInsideBlocks pins that a cancelled context stops a
// parse at the first checkpoint inside one long block, rather than after the
// block ends.
func TestParseChecksContextInsideBlocks(t *testing.T) {
	t.Parallel()

	const entries = 4 * ctxCheckInterval
	var tu, cid strings.Builder
	fmt.Fprintf(&tu, "%d beginbfchar\n", entries)
	fmt.Fprintf(&cid, "%d begincidchar\n", entries)
	for i := range entries {
		fmt.Fprintf(&tu, "<%04X> <0041>\n", i)
		fmt.Fprintf(&cid, "<%04X> 1\n", i)
	}
	tu.WriteString("endbfchar\n")
	cid.WriteString("endcidchar\n")

	b := newParseBudget()
	if _, err := parse(&cancelAfterTwoChecks{Context: context.Background()}, []byte(tu.String()), b); !errors.Is(err, context.Canceled) {
		t.Fatalf("parse: err = %v, want context.Canceled", err)
	}
	if b.steps != ctxCheckInterval {
		t.Errorf("parse stopped after %d entries, want %d", b.steps, ctxCheckInterval)
	}
	b = newParseBudget()
	if _, err := parseCID(&cancelAfterTwoChecks{Context: context.Background()}, []byte(cid.String()), b); !errors.Is(err, context.Canceled) {
		t.Fatalf("parseCID: err = %v, want context.Canceled", err)
	}
	if b.steps != ctxCheckInterval {
		t.Errorf("parseCID stopped after %d entries, want %d", b.steps, ctxCheckInterval)
	}
}

// TestParseCIDRejectsOutOfRangeValues pins that an entry whose code is wider
// than four bytes or whose CID is negative or overflows maps nothing, while
// the entries beside it still map.
func TestParseCIDRejectsOutOfRangeValues(t *testing.T) {
	t.Parallel()

	src := []byte("4 begincidchar\n<0041> 7\n<0102030405> 8\n<0042> -1\n<0043> 9\nendcidchar\n" +
		"3 begincidrange\n<0100> <0101> 9223372036854775807\n<000000000200> <000000000201> 1\n<0300> <0301> 70000\nendcidrange\n" +
		"2 beginnotdefrange\n<0400> <04FF> -3\n<0500> <05FF> 2\nendnotdefrange\n")
	c, err := ParseCID(context.Background(), src)
	if err != nil {
		t.Fatalf("ParseCID: %v", err)
	}
	want := map[int64]int{0x41: 7, 0x43: 9, 0x300: 70000, 0x301: 70001}
	if len(c.Mapping) != len(want) {
		t.Fatalf("Mapping = %v, want %v", c.Mapping, want)
	}
	for code, cid := range want {
		if c.Mapping[code] != cid {
			t.Errorf("Mapping[%#x] = %d, want %d", code, c.Mapping[code], cid)
		}
	}
	if len(c.notdefs) != 1 || c.notdefs[0] != (notdefSpan{lo: 0x500, hi: 0x5FF, cid: 2}) {
		t.Errorf("notdefs = %v, want the one with a non-negative CID", c.notdefs)
	}
}

// TestConformBfRangesArrayStopsAtArrayEnd pins that a crossing bfrange whose
// array destination is shorter than its source range is rewritten through
// the array's end only, the codes [Parse] maps.
func TestConformBfRangesArrayStopsAtArrayEnd(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	src := []byte("1 begincodespacerange <0000> <FFFF> endcodespacerange\n" +
		"1 beginbfrange <00FE> <FFFE> [<0041> <0042> <0043>] endbfrange\n")
	out, changed, err := ConformBfRanges(ctx, src)
	if err != nil || !changed {
		t.Fatalf("ConformBfRanges: changed = %v, err = %v", changed, err)
	}
	want, err := Parse(ctx, src)
	if err != nil {
		t.Fatalf("Parse(src): %v", err)
	}
	got, err := Parse(ctx, out)
	if err != nil {
		t.Fatalf("Parse(out): %v", err)
	}
	if len(got.Mapping) != 3 || len(got.Mapping) != len(want.Mapping) {
		t.Errorf("rewrite maps %d codes, input %d, want 3", len(got.Mapping), len(want.Mapping))
	}
}
