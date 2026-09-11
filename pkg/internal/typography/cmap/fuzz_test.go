package cmap

import (
	"context"
	"maps"
	"testing"
)

// FuzzParse feeds arbitrary bytes to the CMap parser and asserts
// crash-freedom. The parser reads attacker-controlled ToUnicode and
// embedded CMap streams.
func FuzzParse(f *testing.F) {
	f.Add([]byte("begincmap\nendcmap\n"))
	f.Add([]byte("/CIDInit /ProcSet findresource begin\n"))
	f.Add([]byte("1 begincodespacerange <00> <ff> endcodespacerange\n"))
	f.Add([]byte("2 beginbfchar <0041> <0041> <0042> <0042> endbfchar\n"))
	f.Add([]byte("1 beginbfrange <0000> <005e> <0020> endbfrange\n"))

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		_, _ = Parse(context.Background(), data)
	})
}

// FuzzParseCID feeds arbitrary bytes to the CID CMap parser.
func FuzzParseCID(f *testing.F) {
	f.Add([]byte("begincmap\nendcmap\n"))
	f.Add([]byte("1 begincidrange <0000> <00ff> 0 endcidrange\n"))
	f.Add([]byte("1 begincidchar <0041> 65 endcidchar\n"))

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		_, _ = ParseCID(context.Background(), data)
	})
}

// FuzzConformBfRanges feeds arbitrary bytes to the bfrange rewriter and
// asserts crash-freedom and its contract: a reported rewrite maps what the
// input maps and leaves no range a strict reader drops.
func FuzzConformBfRanges(f *testing.F) {
	f.Add([]byte("1 beginbfrange <01ED> <0206> <0061> endbfrange\n"))
	f.Add([]byte("1 beginbfrange <01FE> <0201> [<0041> <0042> <0043> <0044>] endbfrange\n"))
	f.Add([]byte("1 beginbfrange <01ED> <02"))

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		ctx := context.Background()
		out, changed, err := ConformBfRanges(ctx, data)
		if err != nil || !changed {
			return
		}
		want, err := Parse(ctx, data)
		if err != nil {
			t.Fatalf("input parsed inside ConformBfRanges but not here: %v", err)
		}
		got, err := Parse(ctx, out)
		if err != nil {
			t.Fatalf("rewrite does not parse: %v", err)
		}
		if !maps.Equal(got.Mapping, want.Mapping) || !maps.Equal(got.StrictMapping(), got.Mapping) {
			t.Fatalf("rewrite broke its contract:\n%s", out)
		}
	})
}
