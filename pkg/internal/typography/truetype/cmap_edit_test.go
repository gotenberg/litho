package truetype

import (
	"context"
	"errors"
	"slices"
	"testing"
)

var (
	windowsUnicode = CmapSubtable{PlatformID: 3, EncodingID: 1}
	windowsSymbol  = CmapSubtable{PlatformID: 3, EncodingID: 0}
	unicodeBMP     = CmapSubtable{PlatformID: 0, EncodingID: 3}
	macRoman       = CmapSubtable{PlatformID: 1, EncodingID: 0}
)

// macintoshLayout rewrites [subsetSource], whose cmap holds (3,0) and (3,1),
// into the layout of a program made for the Macintosh: a (0,3) Unicode
// subtable and a (1,0) one, and no Windows subtable.
func macintoshLayout(t *testing.T) []byte {
	t.Helper()
	out, err := RewriteCmap(context.Background(), subsetSource(), []CmapAlias{
		{Target: macRoman, From: windowsUnicode},
		{Target: unicodeBMP, From: windowsUnicode},
	})
	if err != nil {
		t.Fatalf("RewriteCmap: %v", err)
	}
	return out
}

func TestRewriteCmap_RecordsAndMappings(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	src := subsetSource()
	mac := macintoshLayout(t)
	subs, err := CmapSubtables(mac)
	if err != nil {
		t.Fatalf("CmapSubtables: %v", err)
	}
	if want := []CmapSubtable{unicodeBMP, macRoman}; !slices.Equal(subs, want) {
		t.Fatalf("records %v, want %v in platform order", subs, want)
	}
	before, err := CharCodeToGID(ctx, src)
	if err != nil {
		t.Fatalf("CharCodeToGID: %v", err)
	}
	after, err := CharCodeToGID(ctx, mac)
	if err != nil {
		t.Fatalf("CharCodeToGID: %v", err)
	}
	for code, gid := range before {
		if after[code] != gid {
			t.Errorf("code %#x: glyph %d, want %d", code, after[code], gid)
		}
	}
	widths, err := GlyphWidths(ctx, mac)
	if err != nil {
		t.Fatalf("GlyphWidths: %v", err)
	}
	srcWidths, err := GlyphWidths(ctx, src)
	if err != nil {
		t.Fatalf("GlyphWidths: %v", err)
	}
	for gid, w := range srcWidths {
		if widths[gid] != w {
			t.Errorf("glyph %d advances %g, want %g", gid, widths[gid], w)
		}
	}
	if sum := tableChecksum(mac); sum != checkSumMagic {
		t.Errorf("whole-font checksum %#x, want %#x", sum, uint32(checkSumMagic))
	}
	if _, err := ReadInfo(mac); err != nil {
		t.Errorf("ReadInfo: %v", err)
	}

	// A (3,1) subtable aliasing the Unicode one maps what it maps.
	back, err := RewriteCmap(ctx, mac, []CmapAlias{
		{Target: unicodeBMP, From: unicodeBMP},
		{Target: windowsUnicode, From: unicodeBMP},
		{Target: macRoman, From: macRoman},
	})
	if err != nil {
		t.Fatalf("RewriteCmap: %v", err)
	}
	if subs, _ := CmapSubtables(back); !slices.Equal(subs, []CmapSubtable{unicodeBMP, macRoman, windowsUnicode}) {
		t.Errorf("records %v", subs)
	}
	sym, err := SymbolCharCodeToGID(ctx, back)
	if err != nil || sym['A'] != before['A'] {
		t.Errorf("(1,0) code 'A' reaches glyph %d (%v), want %d", sym['A'], err, before['A'])
	}
}

func TestRewriteCmap_Refusals(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	src := subsetSource()
	if _, err := RewriteCmap(ctx, src, []CmapAlias{{Target: macRoman, From: unicodeBMP}}); !errors.Is(err, ErrTableNotFound) {
		t.Errorf("a missing source record: %v, want ErrTableNotFound", err)
	}
	if _, err := RewriteCmap(ctx, src, []CmapAlias{{Target: macRoman, From: windowsSymbol}, {Target: macRoman, From: windowsUnicode}}); !errors.Is(err, ErrInvalidFont) {
		t.Errorf("a target given twice: %v, want ErrInvalidFont", err)
	}
	if _, err := RewriteCmap(ctx, []byte("not a font"), nil); !errors.Is(err, ErrInvalidFont) {
		t.Errorf("not a font: %v, want ErrInvalidFont", err)
	}
	if _, err := RewriteCmap(ctx, src[:len(src)/2], []CmapAlias{{Target: macRoman, From: windowsUnicode}}); err == nil {
		t.Error("a truncated program was rewritten")
	}
}
