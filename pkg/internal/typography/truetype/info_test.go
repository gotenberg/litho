package truetype

import (
	"context"
	"errors"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
)

func testProgram(fsType uint16) []byte {
	return fontbuild.TrueType{
		PostScriptName: "TestSans-Bold",
		Family:         "Test Sans",
		Subfamily:      "Bold",
		UnitsPerEm:     2048,
		FSType:         fsType,
		Glyphs: []fontbuild.Glyph{
			{Name: "space", Unicode: ' ', Advance: 569, Empty: true},
			{Name: "A", Unicode: 'A', Advance: 1479},
		},
		NotdefAdvance: 1536,
	}.Build()
}

func TestReadInfo(t *testing.T) {
	t.Parallel()
	info, err := ReadInfo(testProgram(0))
	if err != nil {
		t.Fatalf("ReadInfo: %v", err)
	}
	if info.PostScriptName != "TestSans-Bold" || info.Family != "Test Sans" || info.Subfamily != "Bold" {
		t.Errorf("names = %q %q %q", info.PostScriptName, info.Family, info.Subfamily)
	}
	if info.UnitsPerEm != 2048 || info.Outline != OutlineTrueType || !info.HasOS2 {
		t.Errorf("upem %d outline %d os2 %v", info.UnitsPerEm, info.Outline, info.HasOS2)
	}
	if info.CapHeight == 0 || info.Ascender <= 0 || info.Descender >= 0 {
		t.Errorf("metrics: cap %d asc %d desc %d", info.CapHeight, info.Ascender, info.Descender)
	}
	widths, err := GlyphWidths(context.Background(), testProgram(0))
	if err != nil || widths[2] != 1479 {
		t.Errorf("GlyphWidths = %v, %v", widths, err)
	}
	gids, err := CharCodeToGID(context.Background(), testProgram(0))
	if err != nil || gids['A'] != 2 {
		t.Errorf("CharCodeToGID = %v, %v", gids, err)
	}
}

func TestInfoEmbeddingRestricted(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		fsType uint16
		want   bool
	}{
		{0, false},
		{FSTypeRestricted, true},
		{FSTypeRestricted | FSTypePreviewPrint, false},
		{FSTypeEditable, false},
		{FSTypeBitmapOnly, true},
		{FSTypeEditable | FSTypeBitmapOnly, true},
	} {
		info, err := ReadInfo(testProgram(tc.fsType))
		if err != nil {
			t.Fatalf("ReadInfo: %v", err)
		}
		if got := info.EmbeddingRestricted(); got != tc.want {
			t.Errorf("fsType %#04x: EmbeddingRestricted = %v, want %v", tc.fsType, got, tc.want)
		}
	}
}

func TestReadInfoRejects(t *testing.T) {
	t.Parallel()
	data := testProgram(0)
	for name, input := range map[string][]byte{
		"empty":     nil,
		"not sfnt":  []byte("%!PS-AdobeFont-1.0"),
		"truncated": data[:40],
	} {
		if _, err := ReadInfo(input); !errors.Is(err, ErrInvalidFont) && !errors.Is(err, ErrTableNotFound) {
			t.Errorf("%s: ReadInfo error = %v, want ErrInvalidFont or ErrTableNotFound", name, err)
		}
	}
}
