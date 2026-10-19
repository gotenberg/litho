package cmap

import (
	"bytes"
	"context"
	"maps"
	"slices"
	"testing"
)

// TestEncodingProgram_CopiesEveryTable118CMap pins that every predefined CMap
// of ISO 32000-1, 9.7.5.2, Table 118 litho bundles yields a program, and that
// the program decodes every code as the named CMap does: ISO 19005-1, 6.3.3.3
// requires each of them embedded, and a validator never checks that the
// embedded stream maps what the name did.
func TestEncodingProgram_CopiesEveryTable118CMap(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	for _, name := range slices.Sorted(maps.Keys(predefinedCMapCIDSystemInfo)) {
		if name == "Identity-H" || name == "Identity-V" {
			continue
		}
		want := LoadConformanceCIDMap(name)
		if want == nil {
			continue
		}
		prog, ok, err := EncodingProgram(ctx, name)
		if err != nil || !ok {
			t.Errorf("%s: ok=%v err=%v, want a program", name, ok, err)
			continue
		}
		got := ParseConformanceCIDMapOver(ctx, prog.Program, Base{})
		if got == nil {
			t.Errorf("%s: the program does not parse", name)
			continue
		}
		wantMap, _ := want.cidMappings()
		gotMap, _ := got.cidMappings()
		if !maps.Equal(gotMap, wantMap) {
			t.Errorf("%s: the program maps %d codes, the named CMap %d", name, len(gotMap), len(wantMap))
		}
		if !slices.Equal(got.notdefs, want.notdefs) {
			t.Errorf("%s: the program's notdef mappings differ from the named CMap's", name)
		}
		if prog.WMode != want.wmode || programWMode(prog.Program) != want.wmode {
			t.Errorf("%s: WMode %d (program %d), want %d", name, prog.WMode, programWMode(prog.Program), want.wmode)
		}
		if bytes.Contains(prog.Program, []byte("usecmap")) {
			t.Errorf("%s: the program still names a base", name)
		}
		if reg, ord, _, _ := PredefinedCMapCIDSystemInfo(name, false); prog.Registry != reg || prog.Ordering != ord {
			t.Errorf("%s: CIDSystemInfo %s-%s, want %s-%s", name, prog.Registry, prog.Ordering, reg, ord)
		}
	}
}

// TestEncodingProgram_KeepsMixedWidthCodes pins the codespace handling on
// 90ms-RKSJ-H, which mixes one- and two-byte codes: each code is written at
// the width its codespace gives it, so a one-byte code still decodes alone
// and a two-byte code still decodes whole.
func TestEncodingProgram_KeepsMixedWidthCodes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	prog, ok, err := EncodingProgram(ctx, "90ms-RKSJ-H")
	if err != nil || !ok {
		t.Fatalf("EncodingProgram(90ms-RKSJ-H): ok=%v err=%v", ok, err)
	}
	got := ParseConformanceCIDMapOver(ctx, prog.Program, Base{})
	want := LoadConformanceCIDMap("90ms-RKSJ-H")
	if got == nil || want == nil {
		t.Fatal("cannot read the program or the bundled CMap")
	}
	for _, text := range [][]byte{{0x41}, {0x81, 0x40}, {0x41, 0x88, 0x9F, 0x20}} {
		if g, w := got.Decode(text), want.Decode(text); !slices.Equal(g, w) {
			t.Errorf("% X decodes as %v, the named CMap as %v", text, g, w)
		}
	}
}

// TestEncodingProgram_CopiesVeraPDFsFileByteForByte pins the precedence: a
// name veraPDF 1.30.2 bundles a revised copy of gets that copy unchanged.
func TestEncodingProgram_CopiesVeraPDFsFileByteForByte(t *testing.T) {
	t.Parallel()

	prog, ok, err := EncodingProgram(context.Background(), "UniGB-UTF16-H")
	if err != nil || !ok {
		t.Fatalf("EncodingProgram(UniGB-UTF16-H): ok=%v err=%v", ok, err)
	}
	want, ok, err := ConformanceProgram("UniGB-UTF16-H")
	if err != nil || !ok {
		t.Fatalf("ConformanceProgram: ok=%v err=%v", ok, err)
	}
	if !bytes.Equal(prog.Program, want.Program) {
		t.Errorf("the program is %d bytes, not veraPDF's %d", len(prog.Program), len(want.Program))
	}
}

// TestEncodingProgram_Declines pins the refusals: the Identity CMaps, which
// ISO 19005-1, 6.3.3.3 exempts, and names litho holds no data for.
func TestEncodingProgram_Declines(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"Identity-H", "Identity-V", "Adobe-Korea1-2", "NotACMapName", ""} {
		prog, ok, err := EncodingProgram(context.Background(), name)
		if err != nil || ok || prog.Program != nil {
			t.Errorf("%q: ok=%v err=%v, %d bytes, want a refusal", name, ok, err, len(prog.Program))
		}
	}
}
