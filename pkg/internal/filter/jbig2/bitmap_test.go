package jbig2

import "testing"

func TestBitmap_GetSetPixel(t *testing.T) {
	bm := NewBitmap(16, 2)

	// All pixels start as 0 (white).
	if bm.GetPixel(0, 0) != 0 {
		t.Error("expected pixel (0,0) = 0")
	}

	// Set pixel and verify.
	bm.SetPixel(5, 0, 1)
	if bm.GetPixel(5, 0) != 1 {
		t.Error("expected pixel (5,0) = 1 after set")
	}
	if bm.GetPixel(4, 0) != 0 {
		t.Error("expected pixel (4,0) = 0 (not modified)")
	}

	// Clear pixel.
	bm.SetPixel(5, 0, 0)
	if bm.GetPixel(5, 0) != 0 {
		t.Error("expected pixel (5,0) = 0 after clear")
	}
}

func TestBitmap_RowStride(t *testing.T) {
	tests := []struct {
		width      int
		wantStride int
	}{
		{1, 1}, {7, 1}, {8, 1}, {9, 2}, {16, 2}, {17, 3},
	}
	for _, tt := range tests {
		bm := NewBitmap(tt.width, 1)
		if bm.RowStride() != tt.wantStride {
			t.Errorf("width=%d: got stride %d, want %d", tt.width, bm.RowStride(), tt.wantStride)
		}
	}
}

func TestBitmap_Fill(t *testing.T) {
	bm := NewBitmap(8, 1)
	bm.Fill(0xff)
	for x := range 8 {
		if bm.GetPixel(x, 0) != 1 {
			t.Errorf("expected pixel (%d,0) = 1 after fill", x)
		}
	}
}

func TestBlit_Simple(t *testing.T) {
	dst := NewBitmap(8, 2)
	src := NewBitmap(4, 1)
	src.SetPixel(0, 0, 1)
	src.SetPixel(1, 0, 1)

	Blit(src, dst, 2, 0, CombinationOR)

	if dst.GetPixel(2, 0) != 1 {
		t.Error("expected pixel (2,0) = 1")
	}
	if dst.GetPixel(3, 0) != 1 {
		t.Error("expected pixel (3,0) = 1")
	}
	if dst.GetPixel(0, 0) != 0 {
		t.Error("expected pixel (0,0) = 0")
	}
}

func TestExtract(t *testing.T) {
	src := NewBitmap(16, 2)
	src.SetPixel(4, 0, 1)
	src.SetPixel(5, 0, 1)

	sub := Extract(src, 3, 0, 4, 1)
	if sub.Width() != 4 || sub.Height() != 1 {
		t.Fatalf("extract: got %dx%d, want 4x1", sub.Width(), sub.Height())
	}
	if sub.GetPixel(1, 0) != 1 {
		t.Error("expected extracted pixel (1,0) = 1")
	}
	if sub.GetPixel(2, 0) != 1 {
		t.Error("expected extracted pixel (2,0) = 1")
	}
	if sub.GetPixel(0, 0) != 0 {
		t.Error("expected extracted pixel (0,0) = 0")
	}
}

func TestCombineBytes(t *testing.T) {
	tests := []struct {
		old, new byte
		op       CombinationOperator
		want     byte
	}{
		{0xAA, 0x55, CombinationOR, 0xFF},
		{0xAA, 0x55, CombinationAND, 0x00},
		{0xAA, 0x55, CombinationXOR, 0xFF},
		{0xAA, 0x55, CombinationXNOR, 0x00},
		{0xAA, 0x55, CombinationReplace, 0x55},
	}
	for _, tt := range tests {
		got := CombineBytes(tt.old, tt.new, tt.op)
		if got != tt.want {
			t.Errorf("CombineBytes(0x%02x, 0x%02x, %d) = 0x%02x, want 0x%02x",
				tt.old, tt.new, tt.op, got, tt.want)
		}
	}
}
