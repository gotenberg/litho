package models

import "testing"

func TestPage_JSONRoundTrip(t *testing.T) {
	assertRoundTrip(t, Page{
		PageNumber:  5,
		Width:       612,
		Height:      792,
		Rotate:      90,
		UserUnit:    1.5,
		IsLandscape: true,
		DPM:         map[string]string{"Producer": "litho"},
		MediaBox:    [4]float64{0, 0, 612, 792},
		CropBox:     [4]float64{10, 10, 602, 782},
		BleedBox:    [4]float64{5, 5, 607, 787},
		TrimBox:     [4]float64{20, 20, 592, 772},
		ArtBox:      [4]float64{30, 30, 582, 762},
	})
}
