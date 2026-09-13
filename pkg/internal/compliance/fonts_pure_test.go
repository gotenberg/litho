package compliance

import "testing"

func TestIsPredefinedCMap(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		want bool
	}{
		{"Identity-H", true},
		{"Identity-V", true},
		{"Adobe-GB1-H", true},
		{"Adobe-CNS1-H", true},
		{"Adobe-Japan1-H", true},
		{"UniGB-UCS2", true},
		{"UniJIS-UCS2", true},
		{"UniJIS2004-UTF16", true},
		{"UniJIS-UTF8", true},
		// CIDSystemInfo ordering identifiers should be rejected.
		{"Adobe-Korea1-2", false},
		{"Adobe-Japan1-6", false},
		{"Adobe-GB1-5", false},
		// Completely unknown names.
		{"Custom-CMap", false},
		{"", false},
		{"Identity", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isPredefinedCMap(tc.name); got != tc.want {
				t.Errorf("isPredefinedCMap(%q) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

func TestIsSubsetFont(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		basefont string
		want     bool
	}{
		{"valid subset", "ABCDEF+Arial", true},
		{"lowercase tag rejected", "abcdef+Arial", false},
		{"no plus sign", "ABCDEFArial", false},
		{"too short", "ABC+X", false},
		{"plus at wrong position", "ABCDE+FArial", false},
		{"digit in tag", "ABCD1F+Arial", false},
		{"empty", "", false},
		{"just tag and plus", "ABCDEF+", false}, // len < 8
		{"seven uppercase before plus", "ABCDEFG+Arial", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isSubsetFont(tc.basefont); got != tc.want {
				t.Errorf("isSubsetFont(%q) = %v, want %v", tc.basefont, got, tc.want)
			}
		})
	}
}

func TestExtractCMapStreamWMode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		data string
		want int
	}{
		{"wmode 0", "/WMode 0 def", 0},
		{"wmode 1", "/WMode 1 def", 1},
		{"with extra whitespace", "/WMode   1 def", 1},
		{"with newline", "/WMode\n0\ndef", 0},
		{"not found", "/CMapType 1 def", -1},
		{"empty", "", -1},
		{"no digit after", "/WMode abc", -1},
		{"in larger context", "begin\n/WMode 1 def\nend", 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ExtractCMapStreamWMode([]byte(tc.data)); got != tc.want {
				t.Errorf("ExtractCMapStreamWMode(%q) = %d, want %d", tc.data, got, tc.want)
			}
		})
	}
}

func TestExtractUseCMap(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		data string
		want string
	}{
		{"identity-H", "/Identity-H usecmap", "Identity-H"},
		{"in larger context", "begin\n/Adobe-Japan1-H usecmap\nend", "Adobe-Japan1-H"},
		{"with extra spaces", "/SomeCMap   usecmap", "SomeCMap"},
		{"not found", "/CMapType 1 def", ""},
		{"empty", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := extractUseCMap([]byte(tc.data)); got != tc.want {
				t.Errorf("extractUseCMap(%q) = %q, want %q", tc.data, got, tc.want)
			}
		})
	}
}

func TestIsPUAHex(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		token string
		want  bool
	}{
		{"BMP PUA start", "<E000>", true},
		{"BMP PUA end", "<F8FF>", true},
		{"BMP PUA middle", "<E500>", true},
		{"below BMP PUA", "<DFFF>", false},
		{"above BMP PUA", "<F900>", false},
		{"supplementary PUA-A start", "<F0000>", true},
		{"supplementary PUA-A end", "<FFFFF>", true},
		{"ASCII code point", "<0041>", false},
		{"no angle brackets", "E000", false},
		{"empty string", "", false},
		{"empty brackets", "<>", false},
		{"lowercase hex rejected", "<e000>", false},
		{"invalid hex char", "<GHIJ>", false},
		{"surrogate pair PUA-B", "<DBC0DD6D>", true},    // U+10016D
		{"surrogate pair PUA-A", "<DB80DC08>", true},    // U+F0008
		{"surrogate pair non-PUA", "<D835DC00>", false}, // U+1D400 (math bold A)
		{"PUA-B direct", "<100000>", true},
		{"PUA-B end", "<10FFFD>", true},
		{"above PUA-B", "<10FFFE>", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isPUAHex(tc.token); got != tc.want {
				t.Errorf("isPUAHex(%q) = %v, want %v", tc.token, got, tc.want)
			}
		})
	}
}

func TestContainsPUAMapping(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		cmapData string
		want     bool
	}{
		{"has PUA mapping", "1 beginbfchar\n<03> <E000>\nendbfchar", true},
		{"no PUA mapping", "1 beginbfchar\n<03> <0041>\nendbfchar", false},
		{"empty", "", false},
		{"no bfchar section", "/CMapType 1 def", false},
		{"PUA in supplementary range", "1 beginbfchar\n<03> <F0000>\nendbfchar", true},
		{"multiple mappings one PUA", "2 beginbfchar\n<01> <0041>\n<02> <E100>\nendbfchar", true},
		// Packed entries with the end marker on the entry line, and the PUA
		// destination past the second field, expressed as a surrogate pair.
		{"packed line surrogate PUA-B", "6 beginbfchar\n<01> <0061> <02> <0062> <03> <DBC0DD6D> <04> <0064> endbfchar", true},
		{"packed line no PUA", "2 beginbfchar\n<01> <0061> <02> <0062> endbfchar", false},
		{"bfrange PUA dst", "1 beginbfrange\n<01> <01> <E000>\nendbfrange", true},
		{"bfrange non-PUA", "1 beginbfrange\n<01> <03> <0041>\nendbfrange", false},
		{"source in PUA range is not a mapping", "1 beginbfchar\n<E000> <0041>\nendbfchar", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := containsPUAMapping(tc.cmapData); got != tc.want {
				t.Errorf("containsPUAMapping(...) = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestPuaSourceCodes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		cmapData string
		want     []uint32 // source codes expected in the PUA set
	}{
		{"bfchar PUA dst", "2 beginbfchar\n<01> <0061>\n<02> <E04A>\nendbfchar", []uint32{0x02}},
		{"bfchar no PUA", "1 beginbfchar\n<01> <0061>\nendbfchar", nil},
		{"bfrange whole range PUA", "1 beginbfrange\n<01> <03> <E000>\nendbfrange", []uint32{0x01, 0x02, 0x03}},
		{"bfrange partial PUA at boundary", "1 beginbfrange\n<01> <03> <F8FE>\nendbfrange", []uint32{0x01, 0x02}},
		{"bfrange non-PUA", "1 beginbfrange\n<01> <03> <0041>\nendbfrange", nil},
		// ISO 32000-1, 9.10.3: an array destination names each code's value in
		// turn, so only the codes whose own entry is a PUA value count.
		{"bfrange array destination", "1 beginbfrange\n<01> <03> [<E000> <0041> <E002>]\nendbfrange", []uint32{0x01, 0x03}},
		{"bfrange array then plain range", "2 beginbfrange\n<01> <02> [<0041> <E001>]\n<05> <05> <E005>\nendbfrange", []uint32{0x02, 0x05}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := puaSourceCodes(tc.cmapData)
			if len(got) != len(tc.want) {
				t.Fatalf("puaSourceCodes(...) = %v, want codes %v", got, tc.want)
			}
			for _, c := range tc.want {
				if !got[c] {
					t.Errorf("puaSourceCodes(...) missing code %#x; got %v", c, got)
				}
			}
		})
	}
}
