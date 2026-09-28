package cff

import (
	"context"
	"errors"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
)

func testCFF() []byte {
	return fontbuild.CFF{
		FontName: "TestSerif-Roman",
		Glyphs: []fontbuild.Glyph{
			{Name: "space", Advance: 250},
			{Name: "A", Advance: 722},
			{Name: "quoteright", Advance: 333},
		},
		NotdefAdvance: 500,
	}.Build()
}

func TestReadInfo(t *testing.T) {
	t.Parallel()
	data := testCFF()
	info, err := ReadInfo(data)
	if err != nil {
		t.Fatalf("ReadInfo: %v", err)
	}
	if info.FontName != "TestSerif-Roman" || info.CID || info.Glyphs != 4 {
		t.Errorf("info = %+v", info)
	}
	if info.BBox != [4]float64{0, 0, 1000, 800} {
		t.Errorf("BBox = %v", info.BBox)
	}
	names, err := GlyphNames(context.Background(), data)
	if err != nil || names[2] != "A" || names[3] != "quoteright" {
		t.Errorf("GlyphNames = %v, %v", names, err)
	}
	widths, err := GlyphWidths(context.Background(), data)
	if err != nil || widths[2] != 722 || widths[0] != 500 {
		t.Errorf("GlyphWidths = %v, %v", widths, err)
	}
	if enc := ExtractEncoding(data); enc['A'] != "A" || enc[0x27] != "quoteright" {
		t.Errorf("ExtractEncoding = %v", enc)
	}
}

func TestReadInfoRejects(t *testing.T) {
	t.Parallel()
	data := testCFF()
	for name, input := range map[string][]byte{
		"empty":     nil,
		"not cff":   []byte("OTTO"),
		"truncated": data[:12],
	} {
		if _, err := ReadInfo(input); !errors.Is(err, ErrInvalidCFF) && !errors.Is(err, ErrTruncated) {
			t.Errorf("%s: ReadInfo error = %v, want ErrInvalidCFF or ErrTruncated", name, err)
		}
	}
}
