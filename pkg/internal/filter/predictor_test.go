package filter

import (
	"bytes"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

func TestApplyPNGPredictor_None(t *testing.T) {
	// 2 rows, 4 columns, 1 color, 8 BPC → 4 bytes per row + 1 filter byte = 5 per row.
	// Filter type 0 (None): output equals input (without filter bytes).
	input := []byte{
		0, 10, 20, 30, 40, // row 0: None
		0, 50, 60, 70, 80, // row 1: None
	}
	params := &predictorParams{Predictor: 10, Columns: 4, Colors: 1, BitsPerComponent: 8}

	result, err := applyPredictor(input, params)
	if err != nil {
		t.Fatalf("applyPredictor failed: %v", err)
	}

	expected := []byte{10, 20, 30, 40, 50, 60, 70, 80}
	if !bytes.Equal(result, expected) {
		t.Errorf("got %v, want %v", result, expected)
	}
}

func TestApplyPNGPredictor_Sub(t *testing.T) {
	// Filter type 1 (Sub): each byte = raw + left.
	// Row: [1, 10, 5, 3, 2] → Sub filter, bytesPerPixel=1.
	// Decoded: 10, 10+5=15, 15+3=18, 18+2=20.
	input := []byte{1, 10, 5, 3, 2}
	params := &predictorParams{Predictor: 11, Columns: 4, Colors: 1, BitsPerComponent: 8}

	result, err := applyPredictor(input, params)
	if err != nil {
		t.Fatalf("applyPredictor failed: %v", err)
	}

	expected := []byte{10, 15, 18, 20}
	if !bytes.Equal(result, expected) {
		t.Errorf("got %v, want %v", result, expected)
	}
}

func TestApplyPNGPredictor_Up(t *testing.T) {
	// Filter type 2 (Up): each byte = raw + above.
	// Row 0 (None): [0, 10, 20, 30] → 10, 20, 30
	// Row 1 (Up): [2, 5, 10, 15] → 5+10=15, 10+20=30, 15+30=45
	input := []byte{
		0, 10, 20, 30,
		2, 5, 10, 15,
	}
	params := &predictorParams{Predictor: 12, Columns: 3, Colors: 1, BitsPerComponent: 8}

	result, err := applyPredictor(input, params)
	if err != nil {
		t.Fatalf("applyPredictor failed: %v", err)
	}

	expected := []byte{10, 20, 30, 15, 30, 45}
	if !bytes.Equal(result, expected) {
		t.Errorf("got %v, want %v", result, expected)
	}
}

func TestApplyPNGPredictor_Average(t *testing.T) {
	// Filter type 3 (Average): each byte = raw + floor((left + above) / 2).
	// Row 0 (None): [0, 100, 200] → 100, 200
	// Row 1 (Average): [3, 10, 20]
	//   byte 0: 10 + floor((0 + 100) / 2) = 10 + 50 = 60
	//   byte 1: 20 + floor((60 + 200) / 2) = 20 + 130 = 150
	input := []byte{
		0, 100, 200,
		3, 10, 20,
	}
	params := &predictorParams{Predictor: 13, Columns: 2, Colors: 1, BitsPerComponent: 8}

	result, err := applyPredictor(input, params)
	if err != nil {
		t.Fatalf("applyPredictor failed: %v", err)
	}

	expected := []byte{100, 200, 60, 150}
	if !bytes.Equal(result, expected) {
		t.Errorf("got %v, want %v", result, expected)
	}
}

func TestApplyPNGPredictor_Paeth(t *testing.T) {
	// Filter type 4 (Paeth): each byte = raw + PaethPredictor(left, above, upper-left).
	// Row 0 (None): [0, 10, 20] → 10, 20
	// Row 1 (Paeth): [4, 5, 3]
	//   byte 0: left=0, up=10, upLeft=0 → Paeth(0,10,0) = 10 → 5+10 = 15
	//   byte 1: left=15, up=20, upLeft=10 → p=15+20-10=25, pa=10, pb=5, pc=15 → pb<=pc → b=20 → 3+20 = 23
	input := []byte{
		0, 10, 20,
		4, 5, 3,
	}
	params := &predictorParams{Predictor: 14, Columns: 2, Colors: 1, BitsPerComponent: 8}

	result, err := applyPredictor(input, params)
	if err != nil {
		t.Fatalf("applyPredictor failed: %v", err)
	}

	expected := []byte{10, 20, 15, 23}
	if !bytes.Equal(result, expected) {
		t.Errorf("got %v, want %v", result, expected)
	}
}

func TestApplyTIFFPredictor(t *testing.T) {
	// TIFF Predictor 2: horizontal differencing with 3 colors.
	// Row: [10, 20, 30, 5, 10, 15] → Colors=3, Columns=2
	// Decoded: [10, 20, 30, 10+5=15, 20+10=30, 30+15=45]
	input := []byte{10, 20, 30, 5, 10, 15}
	params := &predictorParams{Predictor: 2, Columns: 2, Colors: 3, BitsPerComponent: 8}

	result, err := applyPredictor(input, params)
	if err != nil {
		t.Fatalf("applyPredictor failed: %v", err)
	}

	expected := []byte{10, 20, 30, 15, 30, 45}
	if !bytes.Equal(result, expected) {
		t.Errorf("got %v, want %v", result, expected)
	}
}

func TestApplyPredictor_NilParams(t *testing.T) {
	input := []byte{1, 2, 3}
	result, err := applyPredictor(input, nil)
	if err != nil {
		t.Fatalf("applyPredictor failed: %v", err)
	}
	if !bytes.Equal(result, input) {
		t.Errorf("got %v, want %v", result, input)
	}
}

func TestExtractPredictorParams_Defaults(t *testing.T) {
	parms := ast.PdfDict{
		"Predictor": ast.PdfInt(12),
	}
	p := extractPredictorParams(parms)
	if p == nil {
		t.Fatal("expected non-nil params")
	}
	if p.Predictor != 12 {
		t.Errorf("Predictor = %d, want 12", p.Predictor)
	}
	if p.Columns != 1 {
		t.Errorf("Columns = %d, want 1", p.Columns)
	}
	if p.Colors != 1 {
		t.Errorf("Colors = %d, want 1", p.Colors)
	}
	if p.BitsPerComponent != 8 {
		t.Errorf("BitsPerComponent = %d, want 8", p.BitsPerComponent)
	}
}

func TestExtractPredictorParams_NoPredictor(t *testing.T) {
	parms := ast.PdfDict{
		"Columns": ast.PdfInt(10),
	}
	p := extractPredictorParams(parms)
	if p != nil {
		t.Errorf("expected nil params when predictor absent, got %+v", p)
	}
}

func TestExtractPredictorParams_Nil(t *testing.T) {
	p := extractPredictorParams(nil)
	if p != nil {
		t.Errorf("expected nil for nil dict, got %+v", p)
	}
}

func TestApplyPNGPredictor_MultiColor(t *testing.T) {
	// 3 colors, 8 BPC, 2 columns → bytesPerPixel=3, rowBytes=6.
	// Row (Sub): [1, R, G, B, dR, dG, dB]
	// Decoded: R, G, B, R+dR, G+dG, B+dB.
	input := []byte{1, 100, 150, 200, 10, 20, 30}
	params := &predictorParams{Predictor: 11, Columns: 2, Colors: 3, BitsPerComponent: 8}

	result, err := applyPredictor(input, params)
	if err != nil {
		t.Fatalf("applyPredictor failed: %v", err)
	}

	expected := []byte{100, 150, 200, 110, 170, 230}
	if !bytes.Equal(result, expected) {
		t.Errorf("got %v, want %v", result, expected)
	}
}

func TestPaethPredictor(t *testing.T) {
	tests := []struct {
		a, b, c byte
		want    byte
	}{
		{0, 0, 0, 0},
		{10, 0, 0, 10},       // pa=0 smallest → a
		{0, 10, 0, 10},       // pb=0 smallest → b
		{10, 20, 10, 20},     // p=20, pa=10, pb=0, pc=10 → b
		{100, 200, 150, 150}, // p=150, pa=50, pb=50, pc=0 → pc smallest → c
	}
	for _, tc := range tests {
		got := paethPredictor(tc.a, tc.b, tc.c)
		if got != tc.want {
			t.Errorf("paethPredictor(%d,%d,%d) = %d, want %d", tc.a, tc.b, tc.c, got, tc.want)
		}
	}
}
