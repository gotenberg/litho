package cff

import (
	"bytes"
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
)

// checkInPlace asserts that out keeps every glyph ID of src, with its name or
// CID, its encoding codes and Font DICT, that the glyphs kept carry the
// charstrings src gave them, that every other glyph is its width operand and
// endchar, and that every glyph keeps its advance.
func checkInPlace(t *testing.T, src, out []byte, kept map[int]bool) {
	t.Helper()
	ctx := context.Background()
	before, err := readInPlaceSource(src)
	if err != nil {
		t.Fatalf("reading the source: %v", err)
	}
	after, err := readInPlaceSource(out)
	if err != nil {
		t.Fatalf("reading the subset: %v", err)
	}
	if len(after.charStrings) != len(before.charStrings) {
		t.Fatalf("%d glyphs, source %d", len(after.charStrings), len(before.charStrings))
	}
	for gid, cs := range after.charStrings {
		switch {
		case kept[gid] && !bytes.Equal(cs, before.charStrings[gid]):
			t.Errorf("kept glyph %d changed its charstring", gid)
		case !kept[gid] && (len(cs) > 6 || cs[len(cs)-1] != csEndchar):
			// The width operand takes five bytes at most (Adobe Technical
			// Note #5177, 3.2), and endchar one.
			t.Errorf("glyph %d keeps a charstring of %d bytes", gid, len(cs))
		}
	}
	for _, widths := range []func(context.Context, []byte) (map[uint16]float64, error){GlyphWidths, CIDWidths} {
		want, err := widths(ctx, src)
		if err != nil {
			t.Fatalf("widths of the source: %v", err)
		}
		if got, err := widths(ctx, out); err != nil || !maps.Equal(got, want) {
			t.Errorf("widths %v (%v), source %v", got, err, want)
		}
	}
	if !slices.Equal(after.charset, before.charset) {
		t.Errorf("charset %v, source %v", after.charset, before.charset)
	}
	if !bytes.Equal(after.fdSelect, before.fdSelect) || len(after.fds) != len(before.fds) {
		t.Errorf("FDSelect or FDArray changed")
	}
	srcNames, _ := GlyphNames(ctx, src)
	outNames, _ := GlyphNames(ctx, out)
	if !maps.Equal(outNames, srcNames) {
		t.Errorf("glyph names %v, source %v", outNames, srcNames)
	}
	if got, want := ExtractEncoding(out), ExtractEncoding(src); !maps.Equal(got, want) {
		t.Errorf("encoding %v, source %v", got, want)
	}
	srcCIDs, _ := ValidCIDs(src)
	outCIDs, _ := ValidCIDs(out)
	if !maps.Equal(outCIDs, srcCIDs) {
		t.Errorf("%d CIDs, source %d", len(outCIDs), len(srcCIDs))
	}
	info, err := ReadInfo(out)
	if err != nil {
		t.Fatalf("ReadInfo(out): %v", err)
	}
	srcInfo, _ := ReadInfo(src)
	if info.FontName != srcInfo.FontName || info.BBox != srcInfo.BBox {
		t.Errorf("info %+v, source %+v", info, srcInfo)
	}
	again, err := SubsetInPlace(ctx, out, keys(kept))
	if err != nil || !bytes.Equal(again, out) {
		t.Errorf("a second pass changed the program: %v", err)
	}
}

func keys(kept map[int]bool) []uint16 {
	out := make([]uint16, 0, len(kept))
	for gid := range kept {
		out = append(out, uint16(gid)) //nolint:gosec // G115: test glyph IDs are small.
	}
	return out
}

func TestSubsetInPlace_NameKeyed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  []byte
	}{
		{"standard encoding with subrs", fontbuild.CFF{FontName: "TestSerif", Glyphs: subsetGlyphs(), Subrs: true}.Build()},
		{"custom encoding", fontbuild.CFF{
			FontName: "TestSerif",
			Glyphs:   subsetGlyphs(),
			Encoding: map[byte]string{0x41: "A", 0x42: "B", 0x61: "A", 0x43: "C"},
		}.Build()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Aacute (5) draws A (2) and acute (4) through seac.
			out, err := SubsetInPlace(context.Background(), tc.src, []uint16{3, 5})
			if err != nil {
				t.Fatalf("SubsetInPlace: %v", err)
			}
			checkInPlace(t, tc.src, out, map[int]bool{0: true, 2: true, 3: true, 4: true, 5: true})
		})
	}
}

func TestSubsetInPlace_RealPrograms(t *testing.T) {
	t.Parallel()
	for _, file := range []string{"frutiger_subr_width.cff", "g5.cff", "cid_fdarray_matrix.cff"} {
		t.Run(file, func(t *testing.T) {
			t.Parallel()
			src, err := os.ReadFile(filepath.Join("testdata", file))
			if err != nil {
				t.Fatalf("reading %s: %v", file, err)
			}
			ctx := context.Background()
			out, err := SubsetInPlace(ctx, src, []uint16{1, 3})
			if err != nil {
				t.Fatalf("SubsetInPlace: %v", err)
			}
			if len(out) >= len(src) {
				t.Errorf("subset of %d bytes, source %d", len(out), len(src))
			}
			parsed, err := readInPlaceSource(src)
			if err != nil {
				t.Fatalf("reading the source: %v", err)
			}
			kept := map[int]bool{0: true, 1: true, 3: true}
			if parsed.fdSelect == nil {
				named, err := readSubsetSource(src)
				if err != nil {
					t.Fatalf("readSubsetSource: %v", err)
				}
				flags, _, err := named.keptGlyphs(ctx, []uint16{1, 3})
				if err != nil {
					t.Fatalf("keptGlyphs: %v", err)
				}
				for gid, k := range flags {
					if k {
						kept[gid] = true
					}
				}
			}
			checkInPlace(t, src, out, kept)

			all := make([]uint16, len(parsed.charStrings))
			for gid := range all {
				all[gid] = uint16(gid) //nolint:gosec // G115: a CharStrings INDEX is Card16-counted.
			}
			whole, err := SubsetInPlace(ctx, src, all)
			if err != nil {
				t.Fatalf("SubsetInPlace of every glyph: %v", err)
			}
			wantWidths, _ := GlyphWidths(ctx, src)
			if got, _ := GlyphWidths(ctx, whole); !maps.Equal(got, wantWidths) {
				t.Errorf("widths changed keeping every glyph")
			}
			wantCIDWidths, _ := CIDWidths(ctx, src)
			if got, _ := CIDWidths(ctx, whole); !maps.Equal(got, wantCIDWidths) {
				t.Errorf("CID widths changed keeping every glyph")
			}
		})
	}
}

func TestSubsetInPlace_Refuses(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	src := fontbuild.CFF{FontName: "TestSerif", Glyphs: subsetGlyphs()}.Build()
	if _, err := SubsetInPlace(ctx, src, []uint16{6}); !errors.Is(err, ErrInvalidCFF) {
		t.Errorf("glyph past the CharStrings: %v", err)
	}
	if _, err := SubsetInPlace(ctx, src[:len(src)/2], nil); err == nil {
		t.Error("a truncated program subset")
	}
	if _, err := SubsetInPlace(ctx, []byte{1, 0, 4, 1, 0, 0}, nil); err == nil {
		t.Error("a program without fonts subset")
	}
}
