package typography

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/models"
)

func TestParseCIDVerticalWidths_Individual(t *testing.T) {
	// c1 [w1_1 vx_1 vy_1 w1_2 vx_2 vy_2]
	w2 := ast.PdfArray{
		ast.PdfInt(100),
		ast.PdfArray{
			ast.PdfInt(-1000), ast.PdfInt(500), ast.PdfInt(880),
			ast.PdfInt(-900), ast.PdfInt(450), ast.PdfInt(860),
		},
	}
	metrics := parseCIDVerticalWidths(w2)
	if len(metrics) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(metrics))
	}
	m0 := metrics[100]
	if m0[0] != -1000 || m0[1] != 500 || m0[2] != 880 {
		t.Errorf("CID 100: got %v, want [-1000 500 880]", m0)
	}
	m1 := metrics[101]
	if m1[0] != -900 || m1[1] != 450 || m1[2] != 860 {
		t.Errorf("CID 101: got %v, want [-900 450 860]", m1)
	}
}

func TestParseCIDVerticalWidths_Range(t *testing.T) {
	// c1 c2 w1 vx vy
	w2 := ast.PdfArray{
		ast.PdfInt(200), ast.PdfInt(202),
		ast.PdfInt(-1000), ast.PdfInt(500), ast.PdfInt(880),
	}
	metrics := parseCIDVerticalWidths(w2)
	if len(metrics) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(metrics))
	}
	for cid := 200; cid <= 202; cid++ {
		m, ok := metrics[cid]
		if !ok {
			t.Errorf("missing CID %d", cid)
			continue
		}
		if m[0] != -1000 || m[1] != 500 || m[2] != 880 {
			t.Errorf("CID %d: got %v, want [-1000 500 880]", cid, m)
		}
	}
}

func TestGetVerticalMetrics_Explicit(t *testing.T) {
	font := &Font{
		WMode:        1,
		DW2:          [2]float64{880, -1000},
		CIDWidths:    map[int]float64{42: 600},
		MissingWidth: 1000,
		CIDVerticalMetrics: map[int][3]float64{
			42: {-900, 300, 850},
		},
	}
	w1, vX, vY := font.GetVerticalMetrics(42)
	if w1 != -900 || vX != 300 || vY != 850 {
		t.Errorf("got w1=%v vX=%v vY=%v, want -900, 300, 850", w1, vX, vY)
	}
}

func TestGetVerticalMetrics_Default(t *testing.T) {
	font := &Font{
		WMode:        1,
		DW2:          [2]float64{880, -1000},
		CIDWidths:    map[int]float64{42: 600},
		MissingWidth: 1000,
	}
	w1, vX, vY := font.GetVerticalMetrics(42)
	// Default: w1 = DW2[1] = -1000, vX = half horizontal width = 300, vY = DW2[0] = 880
	if w1 != -1000 {
		t.Errorf("w1: got %v, want -1000", w1)
	}
	if vX != 300 {
		t.Errorf("vX: got %v, want 300 (half of 600)", vX)
	}
	if vY != 880 {
		t.Errorf("vY: got %v, want 880", vY)
	}
}

func TestReconstructText_VerticalCJK(t *testing.T) {
	// Simulate two vertical columns of CJK text.
	// Column 1 (rightmost, X=100): 漢 字 (top-to-bottom)
	// Column 2 (X=80): 文 字 (top-to-bottom)
	// Y spacing: 6pt with fontSize 12, so gap = 6-6=0, no space inserted.
	// Expected reading order: 漢字\n文字
	elements := []models.TextElement{
		{Text: "漢", X: 100, Y: 200, FontSize: 12, Vertical: true},
		{Text: "字", X: 100, Y: 194, FontSize: 12, Vertical: true},
		{Text: "文", X: 80, Y: 200, FontSize: 12, Vertical: true},
		{Text: "字", X: 80, Y: 194, FontSize: 12, Vertical: true},
	}

	got := ReconstructText(elements, nil)
	want := "漢字\n文字"
	if got != want {
		t.Errorf("vertical CJK: got %q, want %q", got, want)
	}
}

func TestReconstructText_VerticalSingleColumn(t *testing.T) {
	// Single vertical column: 日 本 語
	// Y spacing: 6pt with fontSize 12 → no space inserted.
	elements := []models.TextElement{
		{Text: "日", X: 50, Y: 300, FontSize: 12, Vertical: true},
		{Text: "本", X: 50, Y: 294, FontSize: 12, Vertical: true},
		{Text: "語", X: 50, Y: 288, FontSize: 12, Vertical: true},
	}

	got := ReconstructText(elements, nil)
	want := "日本語"
	if got != want {
		t.Errorf("vertical single column: got %q, want %q", got, want)
	}
}
