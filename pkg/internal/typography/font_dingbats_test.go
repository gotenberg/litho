package typography

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// dingbatsFont builds a simple font dictionary naming baseFont and nothing
// else, so the built-in encoding is the only source of the mapping.
func dingbatsFont(baseFont string) ast.PdfDict {
	return ast.PdfDict{
		"Type":     ast.PdfName("Font"),
		"Subtype":  ast.PdfName("Type1"),
		"BaseFont": ast.PdfName(baseFont),
	}
}

// TestResolveFont_ZapfDingbatsBuiltinEncoding covers the dingbats arm of the
// default-encoding switch, which no test in the repository reached.
//
// Measured with -coverpkg across the whole module: BuildSymbolEncoding read
// 83.3% and BuildZapfDingbatsEncoding 0.0%. The two sit on ADJACENT arms of the
// same switch, twice (font.go:343/345 and font.go:663/665), so the Symbol case
// was exercised while the dingbats case never was. A per-package profile hides
// this entirely, reporting both as 0.0%, because the callers live in
// typography while the builders live in typography/encodings.
//
// Per ISO 32000-1 §9.6.6.1 Symbol and ZapfDingbats use their own built-in
// encoding whatever their subtype, so a font naming ZapfDingbats and declaring
// no /Encoding must map its codes through the dingbats table rather than the
// Latin-1 default.
func TestResolveFont_ZapfDingbatsBuiltinEncoding(t *testing.T) {
	font, err := ResolveFont(context.Background(),
		&document.Document{Logger: document.NopLogger()}, dingbatsFont("ZapfDingbats"))
	if err != nil {
		t.Fatalf("ResolveFont: %v", err)
	}

	for _, c := range []struct {
		code byte
		want rune
		note string
	}{
		{0x21, 0x2701, "a1, the first dingbat"},
		// a19 and a20 are the two ticks, and they are ADJACENT codes. An
		// off-by-one anywhere in the table or the lookup silently returns the
		// other one, which is a difference a reader cannot see in rendered
		// output. Asserting both makes that confusion fail loudly.
		{0x33, 0x2713, "a19, the light check mark"},
		{0x34, 0x2714, "a20, the heavy check mark"},
		{0x61, 0x2741, "a60, mid-table"},
		{0xA1, 0x2761, "a101, the high range"},
	} {
		got, ok := font.Encoding[c.code]
		if !ok {
			t.Errorf("byte 0x%02X unmapped, want U+%04X (%s)", c.code, c.want, c.note)
			continue
		}
		if got != c.want {
			t.Errorf("byte 0x%02X = U+%04X, want U+%04X (%s)", c.code, got, c.want, c.note)
		}
	}
}

// TestResolveFont_ZapfDingbatsSubsetPrefix pins that a subset-tagged BaseFont
// still selects the built-in encoding.
//
// The detection lowercases the name and asks whether it CONTAINS "dingbat", so
// the six-letter subset prefix a producer prepends does not have to be stripped
// first. Real files carry this spelling: the compliance suite's fixtures use
// MHHIBN+ZapfDingbats.
func TestResolveFont_ZapfDingbatsSubsetPrefix(t *testing.T) {
	font, err := ResolveFont(context.Background(),
		&document.Document{Logger: document.NopLogger()}, dingbatsFont("MHHIBN+ZapfDingbats"))
	if err != nil {
		t.Fatalf("ResolveFont: %v", err)
	}
	if got, ok := font.Encoding[0x34]; !ok || got != 0x2714 {
		t.Errorf("byte 0x34 = U+%04X (present %t), want U+2714: a subset prefix must not defeat the match",
			got, ok)
	}
}

// TestResolveFont_NonDingbatsKeepsLatinDefault is the control. Without it the
// tests above would pass on a build that handed every font the dingbats table,
// since they only ever assert that the mapping IS present.
func TestResolveFont_NonDingbatsKeepsLatinDefault(t *testing.T) {
	font, err := ResolveFont(context.Background(),
		&document.Document{Logger: document.NopLogger()}, dingbatsFont("Helvetica"))
	if err != nil {
		t.Fatalf("ResolveFont: %v", err)
	}
	// 0x34 is the digit four in any Latin text encoding, and the heavy check
	// mark in the dingbats table.
	if got, ok := font.Encoding[0x34]; !ok || got != '4' {
		t.Errorf("byte 0x34 = U+%04X (present %t), want U+0034 '4': a Latin font must not take the dingbats arm",
			got, ok)
	}
}
