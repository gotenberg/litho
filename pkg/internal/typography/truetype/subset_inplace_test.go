package truetype

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"maps"
	"testing"
)

// glyphLengths returns the outline length of every glyph of a program.
func glyphLengths(t *testing.T, data []byte) []int {
	t.Helper()
	records, err := ParseTableDirectory(data)
	if err != nil {
		t.Fatalf("ParseTableDirectory: %v", err)
	}
	head, _ := TableData(data, records, "head")
	loca, _ := TableData(data, records, "loca")
	glyf, _ := TableData(data, records, "glyf")
	n, err := NumGlyphs(data)
	if err != nil {
		t.Fatalf("NumGlyphs: %v", err)
	}
	offsets, err := glyphOffsets(loca, len(glyf), n, binary.BigEndian.Uint16(head[50:52]) != 0)
	if err != nil {
		t.Fatalf("glyphOffsets: %v", err)
	}
	out := make([]int, n)
	for gid := range n {
		out[gid] = offsets[gid+1] - offsets[gid]
	}
	return out
}

func TestSubsetInPlace_KeepsGlyphIDs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	src := subsetSource()
	out, err := SubsetInPlace(ctx, src, []uint16{4, 6}) // B and Aacute
	if err != nil {
		t.Fatalf("SubsetInPlace: %v", err)
	}
	if len(out) >= len(src) {
		t.Errorf("subset of %d bytes, source %d", len(out), len(src))
	}
	before, after := glyphLengths(t, src), glyphLengths(t, out)
	if len(after) != len(before) {
		t.Fatalf("%d glyphs, source %d", len(after), len(before))
	}
	// Aacute (6) draws A (2) and acute (5), which stay with it.
	keptGIDs := map[int]bool{0: true, 2: true, 4: true, 5: true, 6: true}
	for gid := range before {
		switch {
		case keptGIDs[gid] && after[gid] != before[gid]:
			t.Errorf("kept glyph %d is %d bytes, source %d", gid, after[gid], before[gid])
		case !keptGIDs[gid] && after[gid] != 0:
			t.Errorf("glyph %d keeps %d bytes", gid, after[gid])
		}
	}
	for _, read := range []func(context.Context, []byte) (map[uint16]uint16, error){CharCodeToGID, SymbolCharCodeToGID} {
		want, err := read(ctx, src)
		if err != nil {
			t.Fatalf("reading the source cmap: %v", err)
		}
		got, err := read(ctx, out)
		if err != nil || !maps.Equal(got, want) {
			t.Errorf("cmap %v, %v, want the source's %v", got, err, want)
		}
	}
	wantWidths, _ := GlyphWidths(ctx, src)
	if got, err := GlyphWidths(ctx, out); err != nil || !maps.Equal(got, wantWidths) {
		t.Errorf("widths %v, %v, want %v", got, err, wantWidths)
	}
	wantNames, _ := PostGlyphNames(ctx, src)
	if got, err := PostGlyphNames(ctx, out); err != nil || !maps.Equal(got, wantNames) {
		t.Errorf("post names %v, %v, want %v", got, err, wantNames)
	}
	if sum := tableChecksum(out); sum != checkSumMagic {
		t.Errorf("font checksum %#x, want %#x", sum, uint32(checkSumMagic))
	}
}

func TestSubsetInPlace_KeepingEveryGlyphChangesNoOutline(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	src := subsetSource()
	all := make([]uint16, 0, 9)
	for gid := range uint16(9) {
		all = append(all, gid)
	}
	out, err := SubsetInPlace(ctx, src, all)
	if err != nil {
		t.Fatalf("SubsetInPlace: %v", err)
	}
	records, _ := ParseTableDirectory(src)
	outRecords, _ := ParseTableDirectory(out)
	for _, tag := range []string{"glyf", "loca", "hmtx", "cmap", "post"} {
		want, _ := TableData(src, records, tag)
		got, err := TableData(out, outRecords, tag)
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("table %q differs from the source", tag)
		}
	}
	again, err := SubsetInPlace(ctx, out, all)
	if err != nil || !bytes.Equal(again, out) {
		t.Errorf("a second pass changed the program: %v", err)
	}
}

func TestSubsetInPlace_Refuses(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	src := subsetSource()
	if _, err := SubsetInPlace(ctx, src, []uint16{9}); !errors.Is(err, ErrInvalidFont) {
		t.Errorf("glyph past numGlyphs: %v", err)
	}
	otto := append([]byte("OTTO"), src[4:]...)
	if _, err := SubsetInPlace(ctx, otto, nil); !errors.Is(err, ErrInvalidFont) {
		t.Errorf("OTTO program: %v", err)
	}
	if _, err := SubsetInPlace(ctx, src[:len(src)/2], nil); err == nil {
		t.Error("a truncated program subset")
	}
}
