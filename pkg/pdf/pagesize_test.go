package pdf

import (
	"bytes"
	"context"
	"math"
	"testing"
)

// letterPDF builds a minimal PDF with a US Letter page (612x792 pt).
func letterPDF() []byte {
	return []byte(
		"%PDF-1.4\n" +
			"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>\nendobj\n" +
			"xref\n0 4\n0000000000 65535 f \n0000000009 00000 n \n0000000058 00000 n \n0000000115 00000 n \n" +
			"trailer\n<< /Size 4 /Root 1 0 R >>\nstartxref\n199\n%%EOF\n")
}

// rotatedPDF builds a minimal PDF with a US Letter page rotated 90 degrees.
func rotatedPDF() []byte {
	return []byte(
		"%PDF-1.4\n" +
			"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Rotate 90 >>\nendobj\n" +
			"xref\n0 4\n0000000000 65535 f \n0000000009 00000 n \n0000000058 00000 n \n0000000115 00000 n \n" +
			"trailer\n<< /Size 4 /Root 1 0 R >>\nstartxref\n211\n%%EOF\n")
}

// landscapePDF builds a minimal PDF with a landscape page (792x612 pt).
func landscapePDF() []byte {
	return []byte(
		"%PDF-1.4\n" +
			"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 792 612] >>\nendobj\n" +
			"xref\n0 4\n0000000000 65535 f \n0000000009 00000 n \n0000000058 00000 n \n0000000115 00000 n \n" +
			"trailer\n<< /Size 4 /Root 1 0 R >>\nstartxref\n199\n%%EOF\n")
}

// cropBoxPDF builds a PDF with a CropBox smaller than the MediaBox.
func cropBoxPDF() []byte {
	return []byte(
		"%PDF-1.4\n" +
			"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /CropBox [0 0 360 720] >>\nendobj\n" +
			"xref\n0 4\n0000000000 65535 f \n0000000009 00000 n \n0000000058 00000 n \n0000000115 00000 n \n" +
			"trailer\n<< /Size 4 /Root 1 0 R >>\nstartxref\n223\n%%EOF\n")
}

func almostEqual(a, b, epsilon float64) bool {
	return math.Abs(a-b) < epsilon
}

func TestGetPage_Portrait(t *testing.T) {
	data := letterPDF()
	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	p, err := r.GetPage(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetPage: %v", err)
	}

	// US Letter: 612x792 pt
	if !almostEqual(p.Width, 612, 0.01) {
		t.Errorf("width: got %f, want 612", p.Width)
	}
	if !almostEqual(p.Height, 792, 0.01) {
		t.Errorf("height: got %f, want 792", p.Height)
	}
	if p.IsLandscape {
		t.Error("expected portrait, got landscape")
	}
	if p.Rotate != 0 {
		t.Errorf("rotate: got %d, want 0", p.Rotate)
	}
}

func TestGetPage_Landscape(t *testing.T) {
	data := landscapePDF()
	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	p, err := r.GetPage(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetPage: %v", err)
	}

	if !p.IsLandscape {
		t.Error("expected landscape, got portrait")
	}
	if !almostEqual(p.Width, 792, 0.01) {
		t.Errorf("width: got %f, want 792", p.Width)
	}
	if !almostEqual(p.Height, 612, 0.01) {
		t.Errorf("height: got %f, want 612", p.Height)
	}
}

func TestGetPage_Rotated90(t *testing.T) {
	data := rotatedPDF()
	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	p, err := r.GetPage(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetPage: %v", err)
	}

	// Portrait MediaBox (612x792) with /Rotate 90 swaps to 792x612.
	if !p.IsLandscape {
		t.Error("expected landscape after 90° rotation, got portrait")
	}
	if !almostEqual(p.Width, 792, 0.01) {
		t.Errorf("width: got %f, want 792", p.Width)
	}
	if !almostEqual(p.Height, 612, 0.01) {
		t.Errorf("height: got %f, want 612", p.Height)
	}
	if p.Rotate != 90 {
		t.Errorf("rotate: got %d, want 90", p.Rotate)
	}
}

func TestGetPage_CropBox(t *testing.T) {
	data := cropBoxPDF()
	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	p, err := r.GetPage(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetPage: %v", err)
	}

	// CropBox is 360x720 pt.
	if !almostEqual(p.Width, 360, 0.01) {
		t.Errorf("width: got %f, want 360", p.Width)
	}
	if !almostEqual(p.Height, 720, 0.01) {
		t.Errorf("height: got %f, want 720", p.Height)
	}
}

// userUnitPDF builds a PDF with /UserUnit 2.0 and a half-size MediaBox.
// Effective dimensions: 306*2 x 396*2 = 612x792 pt (US Letter).
func userUnitPDF() []byte {
	return []byte(
		"%PDF-1.6\n" +
			"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 306 396] /UserUnit 2.0 >>\nendobj\n" +
			"xref\n0 4\n0000000000 65535 f \n0000000009 00000 n \n0000000058 00000 n \n0000000115 00000 n \n" +
			"trailer\n<< /Size 4 /Root 1 0 R >>\nstartxref\n215\n%%EOF\n")
}

func TestGetPage_UserUnit(t *testing.T) {
	data := userUnitPDF()
	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	p, err := r.GetPage(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetPage: %v", err)
	}

	// 306 * 2.0 = 612, 396 * 2.0 = 792
	if !almostEqual(p.Width, 612, 0.01) {
		t.Errorf("width: got %f, want 612", p.Width)
	}
	if !almostEqual(p.Height, 792, 0.01) {
		t.Errorf("height: got %f, want 792", p.Height)
	}
	if !almostEqual(p.UserUnit, 2.0, 0.001) {
		t.Errorf("userUnit: got %f, want 2.0", p.UserUnit)
	}
	if p.IsLandscape {
		t.Error("expected portrait, got landscape")
	}
}

func TestGetPage_InvalidPage(t *testing.T) {
	data := letterPDF()
	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	_, err = r.GetPage(context.Background(), 99)
	if err == nil {
		t.Error("expected error for invalid page number")
	}
}
