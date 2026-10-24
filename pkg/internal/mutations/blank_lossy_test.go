package mutations

import (
	"image"
	"regexp"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

func TestLossyTileNameHoldsNoThreeDigits(t *testing.T) {
	t.Parallel()
	if got := lossyTileName(0); got != "U" {
		t.Errorf("lossyTileName(0) = %q, want U", got)
	}
	run := regexp.MustCompile(`[0-9]{2}`)
	seen := make(map[ast.PdfName]bool)
	for i := 1; i < 5000; i++ {
		name := lossyTileName(i)
		if run.MatchString(string(name)) {
			t.Fatalf("lossyTileName(%d) = %q holds two digits in a row", i, name)
		}
		if seen[name] {
			t.Fatalf("lossyTileName(%d) = %q repeats", i, name)
		}
		seen[name] = true
	}
}

func TestJPEGColorSpace(t *testing.T) {
	t.Parallel()
	ref := ast.PdfIndirectRef{ObjectNum: 9}
	for _, tc := range []struct {
		cs   ast.PdfObject
		want bool
	}{
		{ast.PdfName("DeviceGray"), true},
		{ast.PdfName("DeviceRGB"), true},
		{ast.PdfName("DeviceCMYK"), true},
		{ast.PdfArray{ast.PdfName("ICCBased"), ref}, true},
		{ast.PdfArray{ast.PdfName("CalRGB"), ast.PdfDict{}}, true},
		{ast.PdfArray{ast.PdfName("Separation"), ast.PdfName("None"), ast.PdfName("DeviceGray"), ref}, false},
		{ast.PdfArray{ast.PdfName("Lab"), ast.PdfDict{}}, false},
		{ast.PdfArray{}, false},
		{nil, false},
	} {
		if got := jpegColorSpace(tc.cs); got != tc.want {
			t.Errorf("jpegColorSpace(%v) = %v, want %v", tc.cs, got, tc.want)
		}
	}
}

func TestMergeBlankedPhotographs(t *testing.T) {
	t.Parallel()
	a := map[int]BlankedPhotograph{7: {Reach: []image.Rectangle{image.Rect(0, 0, 2, 2)}, FormDepth: 1}}
	b := map[int]BlankedPhotograph{
		7: {Reach: []image.Rectangle{image.Rect(5, 5, 6, 6)}},
		8: {Reach: []image.Rectangle{image.Rect(1, 1, 3, 3)}, FormDepth: 2},
	}
	got := MergeBlankedPhotographs(MergeBlankedPhotographs(nil, a), b)
	if len(got) != 2 || len(got[7].Reach) != 2 || got[7].FormDepth != 1 || got[8].FormDepth != 2 {
		t.Errorf("MergeBlankedPhotographs = %+v", got)
	}
}
