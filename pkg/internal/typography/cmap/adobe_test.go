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

// TestLoadPredefinedCMap_UnifiedIdeograph is a regression guard: the
// CID→Unicode side used to invert the Uni*-UCS2-H CMaps, keeping the lowest
// code point that selects a CID, so <4E00> came back as U+2F00 KANGXI RADICAL
// ONE through UniGB-UCS2-H and UniJIS-UCS2-H. The collection's UCS2 CMap
// (ISO 32000-1, 9.10.2) names the ideograph, and a code selecting the same
// CID through the radical maps to it too, as PDF.js reads it.
func TestLoadPredefinedCMap_UnifiedIdeograph(t *testing.T) {
	t.Parallel()
	for _, enc := range []string{"UniGB-UCS2-H", "UniGB-UCS2-V", "UniGB-UTF16-H", "UniCNS-UCS2-H", "UniJIS-UCS2-H", "UniKS-UCS2-H"} {
		t.Run(enc, func(t *testing.T) {
			t.Parallel()
			cm := LoadPredefinedCMap(enc)
			if cm == nil {
				t.Fatalf("LoadPredefinedCMap(%q) returned nil", enc)
			}
			if _, got, ok := cm.Map([]byte{0x4E, 0x00}); !ok || got != "一" {
				t.Errorf("<4E00> maps to %q (%v), want U+4E00", got, ok)
			}
		})
	}
	if _, got, _ := LoadPredefinedCMap("UniGB-UCS2-H").Map([]byte{0x2F, 0x00}); got != "一" {
		t.Errorf("UniGB-UCS2-H <2F00> maps to %q, want U+4E00, the character of the CID it selects", got)
	}
}

// TestAdobeCIDToUnicode_Sequences checks the two kinds of CID the rune map
// leaves out and the string map keeps: one mapped to a sequence, Adobe-Japan1
// CID 11136 to "89", and CID 0, mapped to U+FFFD.
func TestAdobeCIDToUnicode_Sequences(t *testing.T) {
	t.Parallel()
	if got := adobeCIDToUnicodeStrings("Japan1")[11136]; got != "89" {
		t.Errorf("Japan1 CID 11136: got %q, want \"89\"", got)
	}
	if r, ok := AdobeCIDToUnicode("Japan1")[11136]; ok {
		t.Errorf("Japan1 CID 11136 in the rune map as U+%04X, want absent", r)
	}
	if got := adobeCIDToUnicodeStrings("GB1")[0]; got != "�" {
		t.Errorf("GB1 CID 0: got %q, want U+FFFD", got)
	}
	if r, ok := AdobeCIDToUnicode("GB1")[0]; ok {
		t.Errorf("GB1 CID 0 in the rune map as U+%04X, want absent", r)
	}
}
