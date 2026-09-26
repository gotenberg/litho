package cmap

import (
	"context"
	"testing"
)

// TestPredefinedUnicode pins that a bundled CID-to-Unicode CMap yields its
// bfchar mappings apart from its bfrange ones. Adobe-Korea1-UCS2 maps CID 60
// hexadecimal to U+20A9 through bfchar and CID 41 to U+0060 through the bfrange
// <0001> <005F> <0020>. The CMap is Adobe's resource, the file veraPDF 1.30
// bundles, so its 6968 single mappings match veraPDF's code for code: <205A>
// maps to U+FFFD, which the PDF.js binary copy stores as <CDCDCDCD>.
func TestPredefinedUnicode(t *testing.T) {
	t.Parallel()

	chars, all := PredefinedUnicode("Adobe-Korea1-UCS2")
	if got := chars["\x00\x60"]; got != "₩" {
		t.Errorf("bfchar CID 60 maps to %q, want U+20A9", got)
	}
	if got := chars["\x20\x5A"]; got != "�" {
		t.Errorf("bfchar CID 205A maps to %q, want U+FFFD", got)
	}
	if got, ok := chars["\x00\x41"]; ok {
		t.Errorf("bfchar holds CID 41 (%q), which only a bfrange maps", got)
	}
	if got := all["\x00\x41"]; got != "`" {
		t.Errorf("CID 41 maps to %q, want U+0060", got)
	}
	if len(chars) != 6968 || len(all) != 18076 {
		t.Errorf("got %d single and %d mappings, want 6968 and 18076", len(chars), len(all))
	}
	for _, name := range []string{"No-Such-CMap", "", "../bcmaps/Adobe-Korea1-UCS2"} {
		if c, a := PredefinedUnicode(name); c != nil || a != nil {
			t.Errorf("PredefinedUnicode(%q) = %d, %d mappings, want none", name, len(c), len(a))
		}
	}
}

// TestParseRecordsUseCMap pins that the parser records the name a usecmap
// operator gives and leaves the program's own mappings as they are.
func TestParseRecordsUseCMap(t *testing.T) {
	t.Parallel()

	c, err := Parse(context.Background(), []byte("begincmap\n/Adobe-Korea1-UCS2 usecmap\n"+
		"1 begincodespacerange <00> <FF> endcodespacerange\n1 beginbfchar <41> <0041> endbfchar\nendcmap\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := c.UseCMap(); got != "Adobe-Korea1-UCS2" {
		t.Errorf("UseCMap() = %q, want Adobe-Korea1-UCS2", got)
	}
	if len(c.Mapping) != 1 {
		t.Errorf("got %d mappings, want 1", len(c.Mapping))
	}
	plain, err := Parse(context.Background(), []byte("1 beginbfchar <41> <0041> endbfchar\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := plain.UseCMap(); got != "" {
		t.Errorf("UseCMap() = %q for a program naming no base", got)
	}
}

// TestWithBase pins how a base CMap's mappings join a program's: a single
// mapping replaces one the program defines before its usecmap operator and
// yields to one defined after it, or to none when the base is merged after
// the program has run, and a base code takes the program's code width.
func TestWithBase(t *testing.T) {
	t.Parallel()

	c, err := Parse(context.Background(), []byte("1 begincodespacerange <00> <FF> endcodespacerange\n"+
		"1 beginbfchar <60> <0000> endbfchar\n/Base usecmap\n1 beginbfchar <61> <0000> endbfchar\n"))
	if err != nil {
		t.Fatal(err)
	}
	chars := map[string]string{"\x00\x60": "a", "\x00\x61": "b", "\x00\x62": "c"}
	got := c.WithBase(chars, map[string]string{"\x00\x63": "d"}, false)
	for code, want := range map[string]string{"\x60": "a", "\x61": "\x00", "\x62": "c", "\x63": "d"} {
		if got.Mapping[code] != want {
			t.Errorf("code %X maps to %q, want %q", code, got.Mapping[code], want)
		}
	}
	if len(got.Mapping) != 4 {
		t.Errorf("got %d mappings, want 4: %q", len(got.Mapping), got.Mapping)
	}
	if after := c.WithBase(chars, nil, true); after.Mapping["\x61"] != "b" {
		t.Errorf("merged after the program, code 61 maps to %q, want %q", after.Mapping["\x61"], "b")
	}
	if len(c.Mapping) != 2 {
		t.Errorf("WithBase changed the CMap it was called on: %q", c.Mapping)
	}
}
