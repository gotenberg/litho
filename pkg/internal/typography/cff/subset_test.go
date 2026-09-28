package cff

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
)

// subsetGlyphs are the glyphs of the synthetic programs: A, B, C, acute and
// the seac glyph Aacute built from A (Standard code 65) and acute (194).
func subsetGlyphs() []fontbuild.Glyph {
	return []fontbuild.Glyph{
		{Name: "C", Advance: 722},
		{Name: "A", Advance: 667},
		{Name: "B", Advance: 611},
		{Name: "acute", Advance: 333},
		{Name: "Aacute", Advance: 667, Seac: &[2]byte{65, 194}},
	}
}

// checkKept asserts that every glyph newGID keeps has the name and advance it
// had in src.
func checkKept(t *testing.T, src, out []byte, newGID map[uint16]uint16) {
	t.Helper()
	ctx := context.Background()
	srcNames, err := GlyphNames(ctx, src)
	if err != nil {
		t.Fatalf("GlyphNames(src): %v", err)
	}
	outNames, err := GlyphNames(ctx, out)
	if err != nil {
		t.Fatalf("GlyphNames(out): %v", err)
	}
	srcWidths, err := GlyphWidths(ctx, src)
	if err != nil {
		t.Fatalf("GlyphWidths(src): %v", err)
	}
	outWidths, err := GlyphWidths(ctx, out)
	if err != nil {
		t.Fatalf("GlyphWidths(out): %v", err)
	}
	if len(outWidths) != len(newGID) {
		t.Errorf("%d glyphs in the subset, %d kept", len(outWidths), len(newGID))
	}
	for o, n := range newGID {
		if o != 0 && srcNames[o] != outNames[n] {
			t.Errorf("glyph %d named %q, now %d named %q", o, srcNames[o], n, outNames[n])
		}
		if srcWidths[o] != outWidths[n] {
			t.Errorf("glyph %d (%s) advances %g, now %g", o, srcNames[o], srcWidths[o], outWidths[n])
		}
	}
	info, err := ReadInfo(out)
	if err != nil {
		t.Fatalf("ReadInfo(out): %v", err)
	}
	srcInfo, _ := ReadInfo(src)
	if info.FontName != srcInfo.FontName || info.BBox != srcInfo.BBox {
		t.Errorf("info %+v, source %+v", info, srcInfo)
	}
}

func TestSubset_StandardEncodingWithSubrs(t *testing.T) {
	t.Parallel()
	src := fontbuild.CFF{FontName: "TestSerif", Glyphs: subsetGlyphs(), Subrs: true}.Build()
	out, newGID, err := Subset(context.Background(), src, []uint16{3}) // B
	if err != nil {
		t.Fatalf("Subset: %v", err)
	}
	if len(newGID) != 2 || newGID[3] != 1 {
		t.Errorf("newGID %v", newGID)
	}
	checkKept(t, src, out, newGID)
	if enc := ExtractEncoding(out); enc['B'] != "B" || len(enc) != 1 {
		t.Errorf("built-in encoding %v, want the Standard encoding's B alone", enc)
	}
}

func TestSubset_KeepsSeacComponents(t *testing.T) {
	t.Parallel()
	src := fontbuild.CFF{FontName: "TestSerif", Glyphs: subsetGlyphs(), Subrs: true}.Build()
	out, newGID, err := Subset(context.Background(), src, []uint16{5}) // Aacute
	if err != nil {
		t.Fatalf("Subset: %v", err)
	}
	for _, gid := range []uint16{2, 4, 5} {
		if _, ok := newGID[gid]; !ok {
			t.Errorf("glyph %d dropped: %v", gid, newGID)
		}
	}
	if _, ok := newGID[1]; ok {
		t.Errorf("C kept: %v", newGID)
	}
	checkKept(t, src, out, newGID)
}

func TestSubset_RebuildsACustomEncoding(t *testing.T) {
	t.Parallel()
	src := fontbuild.CFF{
		FontName: "TestSerif",
		Glyphs:   subsetGlyphs(),
		Encoding: map[byte]string{0x43: "C", 0x41: "A", 0x42: "B", 0x61: "A", 0x27: "acute"},
	}.Build()
	if enc := ExtractEncoding(src); enc[0x61] != "A" || enc[0x42] != "B" {
		t.Fatalf("source encoding %v", enc)
	}
	// B, A and Aacute, whose seac brings acute.
	out, newGID, err := Subset(context.Background(), src, []uint16{3, 2, 5})
	if err != nil {
		t.Fatalf("Subset: %v", err)
	}
	checkKept(t, src, out, newGID)
	enc := ExtractEncoding(out)
	want := map[byte]string{0x41: "A", 0x61: "A", 0x42: "B", 0x27: "acute"}
	if len(enc) != len(want) {
		t.Errorf("encoding %v, want %v", enc, want)
	}
	for code, name := range want {
		if enc[code] != name {
			t.Errorf("code %#x -> %q, want %q", code, enc[code], name)
		}
	}
}

func TestSubset_RealPrograms(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"frutiger_subr_width.cff"} {
		src, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		info, err := ReadInfo(src)
		if err != nil {
			t.Fatalf("%s: ReadInfo: %v", name, err)
		}
		keep := []uint16{1, uint16(info.Glyphs / 2), uint16(info.Glyphs - 1)} //nolint:gosec // G115: a Card16 glyph count.
		out, newGID, err := Subset(context.Background(), src, keep)
		if err != nil {
			t.Fatalf("%s: Subset: %v", name, err)
		}
		if len(out) >= len(src) {
			t.Errorf("%s: subset of %d bytes, source %d", name, len(out), len(src))
		}
		checkKept(t, src, out, newGID)
		// The subroutines the three glyphs do not call are emptied, and the
		// INDEXes keep their lengths.
		before, err := readSubsetSource(src)
		if err != nil {
			t.Fatalf("%s: reading the source: %v", name, err)
		}
		after, err := readSubsetSource(out)
		if err != nil {
			t.Fatalf("%s: reading the subset: %v", name, err)
		}
		if len(after.localSubrs) != len(before.localSubrs) || len(after.globalSubrs) != len(before.globalSubrs) {
			t.Errorf("%s: subroutine INDEXes of %d and %d entries, source %d and %d", name,
				len(after.localSubrs), len(after.globalSubrs), len(before.localSubrs), len(before.globalSubrs))
		}
		emptied := 0
		for _, body := range append(after.localSubrs, after.globalSubrs...) {
			if len(body) == 1 && body[0] == csReturn {
				emptied++
			}
		}
		if emptied == 0 {
			t.Errorf("%s: no subroutine emptied", name)
		}
	}
}

func TestSubset_Refuses(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cid, err := os.ReadFile(filepath.Join("testdata", "cid_fdarray_matrix.cff"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := Subset(ctx, cid, []uint16{1}); !errors.Is(err, ErrSubsetUnsupported) {
		t.Errorf("CID-keyed program: %v", err)
	}
	src := fontbuild.CFF{FontName: "TestSerif", Glyphs: subsetGlyphs()}.Build()
	if _, _, err := Subset(ctx, src, []uint16{6}); !errors.Is(err, ErrInvalidCFF) {
		t.Errorf("glyph past the program: %v", err)
	}
	if _, _, err := Subset(ctx, src[:len(src)-8], []uint16{1}); err == nil {
		t.Errorf("a truncated program was subset")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err := Subset(cancelled, src, []uint16{1}); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled: %v", err)
	}
}

func TestDictEntries_RoundTrips(t *testing.T) {
	t.Parallel()
	// 100 -50 1000 -1000 32000 70000 0.5 BlueValues(6), then an escape op.
	dict := []byte{239, 0x59, 0xFA, 0x7C, 0xFE, 0x7C, 28, 0x7D, 0x00, 29, 0, 1, 0x11, 0x70, 30, 0x0A, 0x5F, 6, 139, 12, 7}
	entries, err := dictEntries(dict)
	if err != nil {
		t.Fatalf("dictEntries: %v", err)
	}
	if len(entries) != 2 || entries[0].op != 6 || entries[1].op != 1207 {
		t.Fatalf("entries %+v", entries)
	}
	want := []float64{100, -50, 1000, -1000, 32000, 70000, 0.5}
	for i, v := range want {
		if entries[0].values[i] != v {
			t.Errorf("operand %d = %g, want %g", i, entries[0].values[i], v)
		}
	}
	var rebuilt []byte
	for _, e := range entries {
		rebuilt = append(rebuilt, e.raw...)
		rebuilt = append(rebuilt, e.opBytes()...)
	}
	if string(rebuilt) != string(dict) {
		t.Errorf("rebuilt %v, want %v", rebuilt, dict)
	}
	for _, bad := range [][]byte{{28, 1}, {29, 0}, {30, 0x12}, {139}, {22}, {12}} {
		if _, err := dictEntries(bad); err == nil {
			t.Errorf("dictEntries(%v) accepted", bad)
		}
	}
}
