package compliance

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

func TestContainsPUARunes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		s    string
		want bool
	}{
		{"empty", "", false},
		{"ascii", "Hello World", false},
		{"BMP PUA start", string(rune(0xE000)), true},
		{"BMP PUA end", string(rune(0xF8FF)), true},
		{"BMP PUA middle", string(rune(0xE500)), true},
		{"just below PUA", string(rune(0xDFFF)), false},
		{"just above PUA", string(rune(0xF900)), false},
		{"supplementary PUA-A start", string(rune(0xF0000)), true},
		{"supplementary PUA-A end", string(rune(0xFFFFF)), true},
		{"supplementary PUA-B start", string(rune(0x100000)), true},
		{"supplementary PUA-B end", string(rune(0x10FFFF)), true},
		{"mixed with PUA", "Hello" + string(rune(0xE000)) + "World", true},
		{"CJK character", "\u4e16", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := containsPUARunes(tc.s); got != tc.want {
				t.Errorf("containsPUARunes(%q) = %v, want %v", tc.s, got, tc.want)
			}
		})
	}
}

func TestContainsPUABytes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		data []byte
		want bool
	}{
		{"empty", nil, false},
		{"ASCII UTF-16BE", []byte{0x00, 0x41}, false},               // 'A'
		{"BMP PUA E000", []byte{0xE0, 0x00}, true},                  // U+E000
		{"BMP PUA F8FF", []byte{0xF8, 0xFF}, true},                  // U+F8FF
		{"just below PUA", []byte{0xDF, 0xFF}, false},               // U+DFFF
		{"just above PUA", []byte{0xF9, 0x00}, false},               // U+F900
		{"with BOM then PUA", []byte{0xFE, 0xFF, 0xE0, 0x00}, true}, // BOM + U+E000
		{"with BOM no PUA", []byte{0xFE, 0xFF, 0x00, 0x41}, false},  // BOM + 'A'
		{"odd length", []byte{0x00}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := containsPUABytes(tc.data); got != tc.want {
				t.Errorf("containsPUABytes(%v) = %v, want %v", tc.data, got, tc.want)
			}
		})
	}
}

func TestHeadingLevel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		tag  ast.PdfName
		want int
	}{
		{"H1", 1},
		{"H2", 2},
		{"H3", 3},
		{"H4", 4},
		{"H5", 5},
		{"H6", 6},
		{"H", 0},
		{"H7", 0},
		{"P", 0},
		{"", 0},
	}
	for _, tc := range tests {
		t.Run(string(tc.tag), func(t *testing.T) {
			t.Parallel()
			if got := headingLevel(tc.tag); got != tc.want {
				t.Errorf("headingLevel(%q) = %d, want %d", tc.tag, got, tc.want)
			}
		})
	}
}

func TestIsMathMLType(t *testing.T) {
	t.Parallel()
	tests := []struct {
		tag  ast.PdfName
		want bool
	}{
		{"math", true},
		{"Math", true},
		{"MATH", false},
		{"mi", false},
		{"P", false},
		{"", false},
	}
	for _, tc := range tests {
		t.Run(string(tc.tag), func(t *testing.T) {
			t.Parallel()
			if got := isMathMLType(tc.tag); got != tc.want {
				t.Errorf("isMathMLType(%q) = %v, want %v", tc.tag, got, tc.want)
			}
		})
	}
}

func TestIsStandardStructType(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		tag     ast.PdfName
		profile string
		want    bool
	}{
		{"Document is always standard", "Document", "PDF/A-1a", true},
		{"P is always standard", "P", "PDF/A-2b", true},
		{"Table is always standard", "Table", "PDF/UA-1", true},
		{"Title is PDF2 only - part 4", "Title", "PDF/A-4", true},
		{"Title is PDF2 only - UA2", "Title", "PDF/UA-2", true},
		{"Title is not standard for part 1", "Title", "PDF/A-1a", false},
		{"Title is not standard for UA1", "Title", "PDF/UA-1", false},
		{"Em is PDF2 only - part 4", "Em", "PDF/A-4", true},
		{"Em is not standard for part 2", "Em", "PDF/A-2a", false},
		{"nonexistent type", "FooBar", "PDF/A-4", false},
		{"empty name", "", "PDF/A-1a", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isStandardStructType(tc.tag, tc.profile); got != tc.want {
				t.Errorf("isStandardStructType(%q, %q) = %v, want %v", tc.tag, tc.profile, got, tc.want)
			}
		})
	}
}
