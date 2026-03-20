package cmap

import "testing"

func TestAdobeCIDToUnicode_Japan1(t *testing.T) {
	m := AdobeCIDToUnicode("Japan1")
	if m == nil {
		t.Fatal("Japan1 map is nil")
	}
	if len(m) < 1000 {
		t.Errorf("Japan1 map too small: %d entries", len(m))
	}
	// CID 1 should map to U+0020 (space) per Adobe-Japan1 spec.
	if m[1] != 0x0020 {
		t.Errorf("CID 1: got U+%04X, want U+0020", m[1])
	}
}

func TestAdobeCIDToUnicode_AllCollections(t *testing.T) {
	for _, ordering := range []string{"Japan1", "GB1", "CNS1", "Korea1"} {
		m := AdobeCIDToUnicode(ordering)
		if m == nil {
			t.Errorf("%s: map is nil", ordering)
			continue
		}
		if len(m) < 100 {
			t.Errorf("%s: only %d entries", ordering, len(m))
		}
	}
}

func TestAdobeCIDToUnicode_Unknown(t *testing.T) {
	if m := AdobeCIDToUnicode("Bogus"); m != nil {
		t.Errorf("expected nil for unknown ordering, got %d entries", len(m))
	}
}
