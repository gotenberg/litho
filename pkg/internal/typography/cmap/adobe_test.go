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

func TestOrderingFromEncodingName(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		want string
	}{
		// Japan1 shorthand names and prefix families.
		{"H", "Japan1"},
		{"V", "Japan1"},
		{"Hiragana", "Japan1"},
		{"Katakana", "Japan1"},
		{"Roman", "Japan1"},
		{"Hankaku", "Japan1"},
		{"WP-Symbol", "Japan1"},
		{"78-EUC-H", "Japan1"},
		{"78ms-RKSJ-V", "Japan1"},
		{"83pv-RKSJ-H", "Japan1"},
		{"90ms-RKSJ-H", "Japan1"},
		{"Add-H", "Japan1"},
		{"EUC-V", "Japan1"},
		{"Ext-RKSJ-V", "Japan1"},
		{"NWP-H", "Japan1"},
		{"RKSJ-H", "Japan1"},
		{"UniJIS-UTF16-H", "Japan1"},
		{"UniJIS-UTF16-V", "Japan1"},
		{"UniJIS2004-UTF32-V", "Japan1"},
		{"UniJISX0213-UTF32-H", "Japan1"},
		// GB1 family.
		{"GB-EUC-H", "GB1"},
		{"GBK-EUC-H", "GB1"},
		{"GBK2K-V", "GB1"},
		{"UniGB-UCS2-V", "GB1"},
		{"UniGB-UTF16-H", "GB1"},
		// CNS1 family.
		{"B5-H", "CNS1"},
		{"B5pc-V", "CNS1"},
		{"CNS-EUC-H", "CNS1"},
		{"CNS1-V", "CNS1"},
		{"ETen-B5-V", "CNS1"},
		{"ETenms-B5-H", "CNS1"},
		{"ETHK-B5-H", "CNS1"},
		{"HKdla-B5-H", "CNS1"},
		{"HKscs-B5-V", "CNS1"},
		{"HKm314-B5-H", "CNS1"},
		{"UniCNS-UTF16-H", "CNS1"},
		// Korea1 family.
		{"KSC-EUC-H", "Korea1"},
		{"KSC-Johab-V", "Korea1"},
		{"KSCms-UHC-V", "Korea1"},
		{"UniKS-UTF16-V", "Korea1"},
		// Unrecognised.
		{"", ""},
		{"Bogus", ""},
		{"MyCustomCMap", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := orderingFromEncodingName(tc.name); got != tc.want {
				t.Errorf("orderingFromEncodingName(%q) = %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

// TestLoadPredefinedCMap_NewlyCovered is a regression guard for
// encodings that loaded nil before the classifier refactor but should
// now compose a valid CID→Unicode CMap.
func TestLoadPredefinedCMap_NewlyCovered(t *testing.T) {
	t.Parallel()
	encodings := []string{
		"UniJIS-UTF16-H",
		"UniJIS-UTF16-V",
		"UniGB-UTF16-H",
		"UniCNS-UTF16-H",
		"UniKS-UTF16-H",
		"ETen-B5-H",
		"HKscs-B5-H",
		"B5-H",
		"KSCms-UHC-H",
	}
	for _, enc := range encodings {
		t.Run(enc, func(t *testing.T) {
			t.Parallel()
			cm := LoadPredefinedCMap(enc)
			if cm == nil {
				t.Fatalf("LoadPredefinedCMap(%q) returned nil", enc)
			}
		})
	}
}
