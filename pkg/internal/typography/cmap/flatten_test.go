package cmap

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// cmapProgram wraps codespace and cidrange sections in the CMap prologue a
// real encoding CMap carries, so the identity entries the rewrite must
// preserve are present to be checked.
func cmapProgram(name, body string) []byte {
	return []byte("%!PS-Adobe-3.0 Resource-CMap\n" +
		"/CIDInit /ProcSet findresource begin\n12 dict begin\nbegincmap\n" +
		"/CIDSystemInfo 3 dict dup begin /Registry (Adobe) def /Ordering (Japan1) def /Supplement 2 def end def\n" +
		"/CMapName /" + name + " def\n/CMapType 1 def\n/WMode 0 def\n" +
		body +
		"endcmap\nCMapName currentdict /CMap defineresource pop\nend\nend\n")
}

func rangeSection(lo, hi, cid int) string {
	return fmt.Sprintf("1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n"+
		"1 begincidrange\n<%04X> <%04X> %d\nendcidrange\n", lo, hi, cid)
}

// TestFlattenUseCMap_InlinesTheChain pins the whole contract: the merged
// program maps every code the chain did, the using CMap wins a disagreement,
// and the CMap's own identity survives.
func TestFlattenUseCMap_InlinesTheChain(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	// The base reaches further than the CMap above it, which is the only
	// reason the reference is load-bearing: codes 0x0100-0x01FF exist solely
	// because of the base.
	base := cmapProgram("BaseCMap", rangeSection(0x0000, 0x01FF, 0))
	using := cmapProgram("UsingCMap", rangeSection(0x0000, 0x000F, 900))

	out, changed, err := FlattenUseCMap(ctx, using, [][]byte{base})
	if err != nil {
		t.Fatalf("FlattenUseCMap: %v", err)
	}
	if !changed {
		t.Fatal("declined a chain it can settle")
	}

	got, err := ParseCID(ctx, out)
	if err != nil {
		t.Fatalf("re-parsing the rewrite: %v", err)
	}
	if len(got.Mapping) != 0x200 {
		t.Errorf("merged map holds %d codes, want 512", len(got.Mapping))
	}
	// The using CMap overrides the base on the codes it defines.
	for code, want := range map[int64]int{0x0000: 900, 0x000F: 915, 0x0010: 0x10, 0x01FF: 0x1FF} {
		if got.Mapping[code] != want {
			t.Errorf("code %#04x maps to %d, want %d", code, got.Mapping[code], want)
		}
	}
	if strings.Contains(string(out), "usecmap") {
		t.Error("a usecmap invocation survived the rewrite")
	}
	// Identity must survive: a renamed CMap would assert a collection the
	// glyphs do not come from.
	for _, keep := range []string{"/CMapName /UsingCMap", "/Ordering (Japan1)", "/WMode 0", "/CMapType 1"} {
		if !strings.Contains(string(out), keep) {
			t.Errorf("the rewrite dropped %q", keep)
		}
	}
}

// TestFlattenUseCMap_SplitsAtFirstByteBoundaries pins the shape Adobe's own
// CMaps use. A range crossing a first-byte boundary is what Adobe Technical
// Note #5411, 1.4.1 rules invalid and what a validator clamps, which would
// leave most of the merged mapping unreachable while the reference still
// looked repaired.
func TestFlattenUseCMap_SplitsAtFirstByteBoundaries(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	base := cmapProgram("BaseCMap", rangeSection(0x0000, 0x02FF, 0))
	using := cmapProgram("UsingCMap", rangeSection(0x0000, 0x000F, 0))

	out, changed, err := FlattenUseCMap(ctx, using, [][]byte{base})
	if err != nil || !changed {
		t.Fatalf("FlattenUseCMap: changed=%v err=%v", changed, err)
	}
	for _, want := range []string{"<0000> <00FF> 0", "<0100> <01FF> 256", "<0200> <02FF> 512"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("missing first-byte-aligned range %q\n--- got ---\n%s", want, out)
		}
	}
}

// TestFlattenUseCMap_Declines covers every arm that must leave the CMap as
// written. Without these the function would pass just as well if it inlined
// whatever it could and wrote the result regardless.
func TestFlattenUseCMap_Declines(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	good := cmapProgram("BaseCMap", rangeSection(0x0000, 0x00FF, 0))

	for _, tc := range []struct {
		name  string
		using []byte
		bases [][]byte
	}{
		{"no base to inline", cmapProgram("A", rangeSection(0, 0xFF, 0)), nil},
		{"empty using program", nil, [][]byte{good}},
		{
			"no codespace to anchor the rewrite",
			[]byte("begincmap\n1 begincidrange\n<00> <0F> 0\nendcidrange\nendcmap\n"),
			[][]byte{good},
		},
		{
			// A one-byte <41> and a two-byte <0041> are the same integer key,
			// so a mixed-width chain cannot be merged unambiguously.
			"chain mixes code widths",
			cmapProgram("A", "1 begincodespacerange\n<00> <FF>\nendcodespacerange\n"+
				"1 begincidrange\n<00> <0F> 0\nendcidrange\n"),
			[][]byte{good},
		},
		{
			"base maps nothing at all",
			cmapProgram("A", "1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n"),
			[][]byte{cmapProgram("B", "1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n")},
		},
	} {
		out, changed, err := FlattenUseCMap(ctx, tc.using, tc.bases)
		if err != nil {
			t.Errorf("%s: unexpected error %v", tc.name, err)
			continue
		}
		if changed {
			t.Errorf("%s: rewrote a CMap it should have left alone:\n%s", tc.name, out)
		}
		if string(out) != string(tc.using) {
			t.Errorf("%s: declined but did not return the input unchanged", tc.name)
		}
	}
}
