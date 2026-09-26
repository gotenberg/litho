package document

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestFloat covers the object-to-float coercion used across the
// codebase. It deliberately tests the public [Float] behavior rather
// than any internal branching.
func TestFloat(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		obj  ast.PdfObject
		want float64
	}{
		{"int", ast.PdfInt(42), 42},
		{"real", ast.PdfReal(3.14), 3.14},
		{"nil", nil, 0},
		{"name", ast.PdfName("foo"), 0},
		{"string", ast.PdfString("bar"), 0},
		{"bool", ast.PdfBool(true), 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Float(tc.obj)
			if got != tc.want {
				t.Errorf("Float(%v) = %v, want %v", tc.obj, got, tc.want)
			}
		})
	}
}

// TestDecodePdfString covers the common string-decoding paths: plain
// ASCII, UTF-16 BE with BOM, and empty input.
func TestDecodePdfString(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   ast.PdfString
		want string
	}{
		{"ascii", ast.PdfString("hello"), "hello"},
		{"empty", ast.PdfString(""), ""},
		{"utf16be-bom", ast.PdfString("\xfe\xff\x00H\x00i"), "Hi"},
		{"utf16le-bom", ast.PdfString("\xff\xfeH\x00i\x00"), "Hi"},
		{"utf8-bom", ast.PdfString("\xef\xbb\xbfHi"), "Hi"},
		{"utf16be-double-bom", ast.PdfString("\xfe\xff\xfe\xff\x00H\x00i"), "Hi"},
		{"utf8-leading-fEFF", ast.PdfString("\xef\xbb\xbf\xe8\xa1\xa8"), "表"},
		// Double UTF-8 BOM: the producer wrapped a U+FEFF rune (which
		// is itself 3 bytes in UTF-8) inside a UTF-8 BOM marker. The
		// outer BOM is consumed as an encoding marker; the inner
		// U+FEFF must also drop so the caller does not see a stray
		// zero-width no-break space at the front of the string.
		{"utf8-double-bom", ast.PdfString("\xef\xbb\xbf\xef\xbb\xbfHi"), "Hi"},
		// UTF-8 BOM declared but the payload contains invalid UTF-8
		// (lone continuation byte). Replace with U+FFFD instead of
		// returning a Go string that fails utf8.ValidString, which
		// would silently corrupt JSON encoders and rune iteration
		// downstream.
		{"utf8-bom-invalid-payload", ast.PdfString("\xef\xbb\xbf\xc2hello"), "�hello"},
		// PDFDocEncoding diacritic range (ISO 32000-1 Table D.2). Bytes
		// 0x18-0x1F are spacing modifier letters, not control codes.
		// A producer that uses 0x18 in a bookmark title means U+02D8
		// BREVE, not U+0018 CANCEL.
		{"pdfdoc-breve", ast.PdfString("a\x18b\xff"), "a˘bÿ"},
		{"pdfdoc-tilde", ast.PdfString("a\x1fb\xff"), "a˜bÿ"},
		// With no high byte to force the slow path, the fast path returned
		// the raw control character.
		{"pdfdoc-breve-pure-ascii", ast.PdfString("a\x18b"), "a˘b"},
		// Undefined PDFDocEncoding bytes (0x00-0x08, 0x0B, 0x0E-0x17,
		// 0x7F) have no Unicode mapping. Dropping them keeps corrupted
		// producer output (0x05 leaking between words) from surfacing
		// as a literal control char in user-facing strings.
		{"pdfdoc-drop-0x05-pure-ascii", ast.PdfString("Blend\x05Down"), "BlendDown"},
		// The trailing 0xFF forces the PDFDocEncoding branch even when
		// the rest is ASCII, separately covering both fast and slow paths.
		{"pdfdoc-drop-0x05", ast.PdfString("Blend\x05Down\xff"), "BlendDownÿ"},
		{"pdfdoc-drop-0x7F", ast.PdfString("ab\x7fcd\xff"), "abcdÿ"},
		// The four defined low controls survive: HT (0x09), LF (0x0A),
		// FF (0x0C), CR (0x0D). Multi-line outline titles depend on
		// these breaks not being stripped.
		{"pdfdoc-keep-controls", ast.PdfString("a\tb\nc\fd\re\xff"), "a\tb\nc\fd\reÿ"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := DecodePdfString(tc.in)
			if got != tc.want {
				t.Errorf("DecodePdfString(%q) = %q, want %q", string(tc.in), got, tc.want)
			}
		})
	}
}

// TestResolve_DirectObject covers the case where the input is not an
// indirect reference. Every resolver helper should return the input
// unchanged (after the appropriate type check).
func TestResolve_DirectObject(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	// Use a nil *Document: direct values should not touch it.
	var doc *Document

	// ResolveObject: direct value passes through.
	got, err := ResolveObject(ctx, doc, ast.PdfInt(5))
	if err != nil {
		t.Errorf("ResolveObject(int): %v", err)
	}
	if got != ast.PdfInt(5) {
		t.Errorf("ResolveObject(int) = %v, want PdfInt(5)", got)
	}

	// ResolveInt: direct int passes through.
	gotInt, err := ResolveInt(ctx, doc, ast.PdfInt(5))
	if err != nil {
		t.Errorf("ResolveInt: %v", err)
	}
	if gotInt != 5 {
		t.Errorf("ResolveInt = %v, want 5", gotInt)
	}

	// ResolveBool: direct bool passes through.
	gotBool, err := ResolveBool(ctx, doc, ast.PdfBool(true))
	if err != nil {
		t.Errorf("ResolveBool: %v", err)
	}
	if !bool(gotBool) {
		t.Error("ResolveBool: got false, want true")
	}

	// ResolveName: direct name passes through.
	gotName, err := ResolveName(ctx, doc, ast.PdfName("Foo"))
	if err != nil {
		t.Errorf("ResolveName: %v", err)
	}
	if gotName != "Foo" {
		t.Errorf("ResolveName = %q, want Foo", gotName)
	}

	// ResolveDict: direct dict passes through.
	dict := ast.PdfDict{"Key": ast.PdfInt(1)}
	gotDict, err := ResolveDict(ctx, doc, dict)
	if err != nil {
		t.Errorf("ResolveDict: %v", err)
	}
	if len(gotDict) != 1 {
		t.Errorf("ResolveDict: got %d entries, want 1", len(gotDict))
	}

	// ResolveArray: direct array passes through.
	arr := ast.PdfArray{ast.PdfInt(1), ast.PdfInt(2)}
	gotArr, err := ResolveArray(ctx, doc, arr)
	if err != nil {
		t.Errorf("ResolveArray: %v", err)
	}
	if len(gotArr) != 2 {
		t.Errorf("ResolveArray: got %d entries, want 2", len(gotArr))
	}
}

// TestResolve_WrongTypeErrors asserts that typed resolvers reject
// objects of the wrong concrete type rather than coercing silently.
func TestResolve_WrongTypeErrors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var doc *Document

	if _, err := ResolveInt(ctx, doc, ast.PdfName("foo")); err == nil {
		t.Error("ResolveInt(name): expected error")
	}
	if _, err := ResolveBool(ctx, doc, ast.PdfInt(1)); err == nil {
		t.Error("ResolveBool(int): expected error")
	}
	if _, err := ResolveName(ctx, doc, ast.PdfInt(1)); err == nil {
		t.Error("ResolveName(int): expected error")
	}
	if _, err := ResolveDict(ctx, doc, ast.PdfInt(1)); err == nil {
		t.Error("ResolveDict(int): expected error")
	}
	if _, err := ResolveArray(ctx, doc, ast.PdfInt(1)); err == nil {
		t.Error("ResolveArray(int): expected error")
	}
}
