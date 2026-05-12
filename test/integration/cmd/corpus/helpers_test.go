package main

import "testing"

// TestStripUndefinedControls covers each branch of the undefined-byte
// table (per ISO 32000-1 Table D.2) and confirms the four defined low
// controls (HT, LF, FF, CR) survive the strip. The govdocs metadata
// canary depends on every variant landing the same value on both
// sides of the comparison.
func TestStripUndefinedControls(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"ascii", "Hello", "Hello"},
		{"trailing-null", "Hello\x00", "Hello"},
		{"leading-null-padding", "\x00\x00\x00Hello", "Hello"},
		{"interior-controls", "Title\x05foo\x07bar", "Titlefoobar"},
		// 0x03 (ETX) shows up as a Producer prefix on some govdocs fixtures.
		{"etx-prefix", "\x03 version 2.51-b", " version 2.51-b"},
		// 0x7F (DEL) is undefined too.
		{"del", "a\x7fb", "ab"},
		// Defined controls survive: HT (0x09), LF (0x0A), FF (0x0C), CR (0x0D).
		{"tab", "a\tb", "a\tb"},
		{"newline", "a\nb", "a\nb"},
		{"ff", "a\x0cb", "a\x0cb"},
		{"cr", "a\rb", "a\rb"},
		// 0x18-0x1F are PDFDocEncoding diacritics, NOT undefined. We do
		// not strip the U+0018-U+001F runes here even though litho's
		// PDFDocEncoding decoder maps the bytes to glyphs; if the
		// caller never decoded, the rune survives.
		{"keep-0x18", "x\x18y", "x\x18y"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := stripUndefinedControls(c.in); got != c.want {
				t.Errorf("stripUndefinedControls(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
