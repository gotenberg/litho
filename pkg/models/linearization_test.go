package models

import "testing"

func TestLinearizationParams_JSONRoundTrip(t *testing.T) {
	assertRoundTrip(t, LinearizationParams{
		FileLength:          10000,
		HintStreamOffset:    100,
		HintStreamLength:    500,
		FirstPageObjNum:     2,
		NumPages:            5,
		FirstPageEndOffset:  2000,
		FirstPageXRefOffset: 9500,
	})
}

func TestPageOffsetEntry_JSONRoundTrip(t *testing.T) {
	assertRoundTrip(t, PageOffsetEntry{
		ObjCount:            5,
		PageLength:          1000,
		ContentStreamOffset: 500,
	})
}
