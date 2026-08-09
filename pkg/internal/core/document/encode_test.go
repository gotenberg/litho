package document

import (
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestEncodePdfString_RoundTrips pins that every encoded text string decodes
// back to what went in.
//
// A PDF text string is PDFDocEncoding or UTF-16BE with a BOM and nothing else
// (ISO 32000-1, 7.9.2.2). litho used to write a Go string's raw UTF-8 bytes,
// which a conforming reader decodes as PDFDocEncoding: "RIL’s" came out as
// "RILâ€™s" everywhere except litho, whose decoder tolerates stray UTF-8.
func TestEncodePdfString_RoundTrips(t *testing.T) {
	cases := []string{
		"",
		"Plain ASCII",
		"Café – naïve ’quote’",
		"CENTRAL INDIA CBM\x0bMajor Achievements",
		"tab\there",
		"line\nbreak",
		"中文標題",
		"emoji \U0001F600 title",
		"mixed Ω and ü and ˘",
	}
	for _, want := range cases {
		got := DecodePdfString(EncodePdfString(want))
		if got != want {
			t.Errorf("round trip of %q gave %q", want, got)
		}
	}
}

// TestEncodePdfString_PrefersPDFDocEncoding pins that a representable string
// stays in the compact single-byte form rather than doubling in size.
func TestEncodePdfString_PrefersPDFDocEncoding(t *testing.T) {
	got := []byte(EncodePdfString("Café – naïve ’quote’"))
	if len(got) >= 2 && got[0] == utf16BEBom0 && got[1] == utf16BEBom1 {
		t.Fatalf("encoded as UTF-16BE though every rune is representable: %q", got)
	}
	// Table D.2 assigns these bytes: en dash and right single quote are not
	// their Latin-1 code points.
	for _, want := range []byte{0xE9 /* é */, 0x85 /* – */, 0x90 /* ’ */} {
		found := slices.Contains(got, want)
		if !found {
			t.Errorf("byte %#x missing from %q", want, got)
		}
	}
}

// TestEncodePdfString_FallsBackForUndefinedBytes pins that a rune PDFDocEncoding
// leaves undefined takes the UTF-16BE route instead of being written as that
// undefined byte.
//
// U+000B is undefined in Table D.2. Writing it as 0x0B produced a byte litho's
// own decoder drops, so a merged bookmark title lost the break and read
// "CBMMajor Achievements".
func TestEncodePdfString_FallsBackForUndefinedBytes(t *testing.T) {
	got := []byte(EncodePdfString("CBM\x0bMajor"))
	if len(got) < 2 || got[0] != utf16BEBom0 || got[1] != utf16BEBom1 {
		t.Fatalf("U+000B did not force UTF-16BE: %q", got)
	}
	if DecodePdfString(ast.PdfString(got)) != "CBM\x0bMajor" {
		t.Errorf("round trip lost the vertical tab: %q", DecodePdfString(ast.PdfString(got)))
	}
}

// TestEncodePdfDocString_ReportsUnrepresentable pins the byte-oriented variant
// used for strings that may not take the UTF-16BE form.
//
// A file specification's /F is not a text string (ISO 32000-1, 7.11.2), so it
// cannot fall back the way a title can: the caller keeps its own bytes and the
// Unicode spelling travels in /UF.
func TestEncodePdfDocString_ReportsUnrepresentable(t *testing.T) {
	got, ok := EncodePdfDocString("café ’90.txt")
	if !ok {
		t.Fatal("a Latin-1 and Table D.2 spellable name was reported unrepresentable")
	}
	if DecodePdfString(got) != "café ’90.txt" {
		t.Errorf("round trip gave %q", DecodePdfString(got))
	}
	if slices.Contains([]byte(got), utf16BEBom0) {
		t.Errorf("PDFDocEncoding output carries a BOM byte: %q", got)
	}

	if _, ok := EncodePdfDocString("中文"); ok {
		t.Error("a CJK name was reported representable in PDFDocEncoding")
	}
	if _, ok := EncodePdfDocString("tab\x0bhere"); ok {
		t.Error("U+000B is undefined in Table D.2 but was reported representable")
	}
}
