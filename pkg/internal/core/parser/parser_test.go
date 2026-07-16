package parser

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/ioat"
	"github.com/gotenberg/litho/pkg/internal/core/lexer"
)

func newParser(input string) *Parser {
	data := []byte(input)
	lex := lexer.NewLexer(ioat.NewReader(bytes.NewReader(data), int64(len(data)), 0))
	return NewParser(lex)
}

func TestParseDict_MissingCloseBracket(t *testing.T) {
	// Dictionary with no closing >>.
	p := newParser("<< /Type /Catalog /Pages 2 0 R")
	obj, err := p.ParseObject(context.Background())
	if err != nil {
		t.Fatalf("ParseObject failed: %v", err)
	}

	dict, ok := obj.(ast.PdfDict)
	if !ok {
		t.Fatalf("expected PdfDict, got %T", obj)
	}

	if dict["Type"] != ast.PdfName("Catalog") {
		t.Errorf("Type = %v, want Catalog", dict["Type"])
	}
	if _, ok := dict["Pages"]; !ok {
		t.Error("missing /Pages key")
	}
}

func TestParseObject_ScientificNotation(t *testing.T) {
	// A dot+exponent real ("1.5e3") parses (Go ParseFloat accepts it) and,
	// under format checking, records a violation. The writer re-emits a
	// plain decimal via formatReal, so the round-trip is compliant.
	r, ok := func() (ast.PdfReal, bool) {
		pf := newParser("1.5e3").WithFormatChecking()
		obj, err := pf.ParseObject(context.Background())
		if err != nil {
			t.Fatalf("ParseObject(1.5e3): %v", err)
		}
		if len(pf.FormatViolations) != 1 {
			t.Fatalf("scientific notation under checkFormat: got %d violations, want 1 (%v)", len(pf.FormatViolations), pf.FormatViolations)
		}
		rv, isReal := obj.(ast.PdfReal)
		return rv, isReal
	}()
	if !ok || float64(r) != 1500 {
		t.Fatalf("1.5e3 parsed to %v (%T), want PdfReal(1500)", r, r)
	}

	// A plain real records nothing.
	plain := newParser("1.5").WithFormatChecking()
	if _, err := plain.ParseObject(context.Background()); err != nil {
		t.Fatalf("ParseObject(1.5): %v", err)
	}
	if len(plain.FormatViolations) != 0 {
		t.Errorf("plain real recorded %d violations, want 0", len(plain.FormatViolations))
	}

	// A bare exponent integer ("1e2", no dot) remains a parse error, as
	// before, so text extraction is unaffected.
	if _, err := newParser("1e2").ParseObject(context.Background()); err == nil {
		t.Error("ParseObject(1e2): expected a parse error (no dot), got nil")
	}
}

func TestParseArray_MissingCloseBracket(t *testing.T) {
	// Array with no closing ].
	p := newParser("[1 2 3")
	obj, err := p.ParseObject(context.Background())
	if err != nil {
		t.Fatalf("ParseObject failed: %v", err)
	}

	arr, ok := obj.(ast.PdfArray)
	if !ok {
		t.Fatalf("expected PdfArray, got %T", obj)
	}

	if len(arr) != 3 {
		t.Errorf("got %d elements, want 3", len(arr))
	}
}

func TestParseDict_ClosedByEndobj(t *testing.T) {
	// Dictionary closed by endobj instead of >>.
	p := newParser("1 0 obj << /Type /Page endobj")
	obj, err := p.ParseObject(context.Background())
	if err != nil {
		t.Fatalf("ParseObject failed: %v", err)
	}

	dict, ok := obj.(ast.PdfDict)
	if !ok {
		t.Fatalf("expected PdfDict, got %T", obj)
	}

	if dict["Type"] != ast.PdfName("Page") {
		t.Errorf("Type = %v, want Page", dict["Type"])
	}
}

func TestParseArray_ClosedByEndobj(t *testing.T) {
	// Array closed by endobj instead of ].
	p := newParser("1 0 obj [10 20 30 endobj")
	obj, err := p.ParseObject(context.Background())
	if err != nil {
		t.Fatalf("ParseObject failed: %v", err)
	}

	arr, ok := obj.(ast.PdfArray)
	if !ok {
		t.Fatalf("expected PdfArray, got %T", obj)
	}

	if len(arr) != 3 {
		t.Errorf("got %d elements, want 3", len(arr))
	}
}

func TestParseDict_NullByteInName(t *testing.T) {
	// Null byte inside a name should be stripped.
	p := newParser("<< /Ty\x00pe /Ca\x00talog >>")
	obj, err := p.ParseObject(context.Background())
	if err != nil {
		t.Fatalf("ParseObject failed: %v", err)
	}

	dict, ok := obj.(ast.PdfDict)
	if !ok {
		t.Fatalf("expected PdfDict, got %T", obj)
	}

	if dict["Type"] != ast.PdfName("Catalog") {
		t.Errorf("Type = %v, want Catalog", dict["Type"])
	}
}

func TestParseArray_DictWithoutSpace(t *testing.T) {
	// Array element followed immediately by dict start.
	p := newParser("[1 2<< /K /V >>]")
	obj, err := p.ParseObject(context.Background())
	if err != nil {
		t.Fatalf("ParseObject failed: %v", err)
	}

	arr, ok := obj.(ast.PdfArray)
	if !ok {
		t.Fatalf("expected PdfArray, got %T", obj)
	}

	if len(arr) != 3 {
		t.Fatalf("got %d elements, want 3", len(arr))
	}

	// First two should be integers.
	if v, ok := arr[0].(ast.PdfInt); !ok || v != 1 {
		t.Errorf("arr[0] = %v, want PdfInt(1)", arr[0])
	}
	if v, ok := arr[1].(ast.PdfInt); !ok || v != 2 {
		t.Errorf("arr[1] = %v, want PdfInt(2)", arr[1])
	}

	// Third should be a dict.
	dict, ok := arr[2].(ast.PdfDict)
	if !ok {
		t.Fatalf("arr[2] = %T, want PdfDict", arr[2])
	}
	if dict["K"] != ast.PdfName("V") {
		t.Errorf("dict[K] = %v, want V", dict["K"])
	}
}

func TestParseIndirectObject_MissingEndobj(t *testing.T) {
	// Object with no endobj keyword.
	p := newParser("5 0 obj << /Type /Font >>")
	obj, err := p.ParseObject(context.Background())
	if err != nil {
		t.Fatalf("ParseObject failed: %v", err)
	}

	dict, ok := obj.(ast.PdfDict)
	if !ok {
		t.Fatalf("expected PdfDict, got %T", obj)
	}

	if dict["Type"] != ast.PdfName("Font") {
		t.Errorf("Type = %v, want Font", dict["Type"])
	}
}

func TestParseIndirectObject_EmptyValue(t *testing.T) {
	// An indirect object with no value between `obj` and `endobj` (or truncated
	// at EOF) is the null object per ISO 32000-1 §7.3.9. qpdf's empty-object.pdf
	// has `7 0 obj\nendobj`; litho used to reject it as "unexpected keyword:
	// endobj", breaking a full rewrite of any file containing one.
	for _, src := range []string{"7 0 obj\nendobj", "7 0 obj"} {
		p := newParser(src)
		obj, err := p.ParseObject(context.Background())
		if err != nil {
			t.Fatalf("ParseObject(%q): unexpected error %v", src, err)
		}
		if _, ok := obj.(ast.PdfNull); !ok {
			t.Errorf("ParseObject(%q) = %T, want ast.PdfNull", src, obj)
		}
	}
}

func TestParseNestedDict_MissingInnerClose(t *testing.T) {
	// Inner dict missing >> but outer has >>.
	// << /Resources << /Font << /F1 1 0 R >> >>
	// Here inner is fine, but test: << /Resources << /Font 1 0 R >>
	// which would close outer dict, leaving Resources with partial inner.
	p := newParser("<< /A << /B /C >>")
	obj, err := p.ParseObject(context.Background())
	if err != nil {
		t.Fatalf("ParseObject failed: %v", err)
	}

	dict, ok := obj.(ast.PdfDict)
	if !ok {
		t.Fatalf("expected PdfDict, got %T", obj)
	}

	// /A should contain a dict with /B = /C.
	inner, ok := dict["A"].(ast.PdfDict)
	if !ok {
		t.Fatalf("/A = %T, want PdfDict", dict["A"])
	}
	if inner["B"] != ast.PdfName("C") {
		t.Errorf("/A/B = %v, want C", inner["B"])
	}
}

func TestParseDict_NonNameKey_Skipped(t *testing.T) {
	// A dict with a non-name key (integer) should skip it and continue parsing.
	p := newParser("<< 42 /Type /Page >>")
	obj, err := p.ParseObject(context.Background())
	if err != nil {
		t.Fatalf("ParseObject failed: %v", err)
	}

	dict, ok := obj.(ast.PdfDict)
	if !ok {
		t.Fatalf("expected PdfDict, got %T", obj)
	}

	// 42 is skipped; /Type is consumed as the next key with /Page as value.
	if dict["Type"] != ast.PdfName("Page") {
		t.Errorf("Type = %v, want Page", dict["Type"])
	}
}

func TestParseHexString_TolerateNonHex(t *testing.T) {
	// veraPDF/Acrobat ignore stray non-hex bytes rather than failing the
	// decode; litho must not drop the value (or, in a content stream, the
	// enclosing operator). "<484!>" -> strip '!' -> "484" -> pad -> "4840".
	tests := []struct {
		in   string
		want string
	}{
		{"<484!>", "\x48\x40"},      // stray non-hex char ignored, then odd-padded
		{"<48 41>", "\x48\x41"},     // whitespace ignored (unchanged behaviour)
		{"<4>", "\x40"},             // odd length padded with trailing 0
		{"<48 41!>", "\x48\x41"},    // mix of whitespace and stray char
		{"<48414>", "\x48\x41\x40"}, // odd length after valid digits
	}
	for _, tc := range tests {
		p := newParser(tc.in)
		obj, err := p.ParseObject(context.Background())
		if err != nil {
			t.Errorf("ParseObject(%q) failed: %v", tc.in, err)
			continue
		}
		s, ok := obj.(ast.PdfString)
		if !ok {
			t.Errorf("ParseObject(%q) = %T, want PdfString", tc.in, obj)
			continue
		}
		if string(s) != tc.want {
			t.Errorf("ParseObject(%q) = %q, want %q", tc.in, string(s), tc.want)
		}
	}
}

func TestParseArray_HexStringWithoutSpace(t *testing.T) {
	// Hex string immediately after a number: "3<ABCD>" should parse as 3 then hex string.
	p := newParser("[3<ABCD>]")
	obj, err := p.ParseObject(context.Background())
	if err != nil {
		t.Fatalf("ParseObject failed: %v", err)
	}

	arr, ok := obj.(ast.PdfArray)
	if !ok {
		t.Fatalf("expected PdfArray, got %T", obj)
	}

	if len(arr) != 2 {
		t.Fatalf("got %d elements, want 2", len(arr))
	}

	if v, ok := arr[0].(ast.PdfInt); !ok || v != 3 {
		t.Errorf("arr[0] = %v, want PdfInt(3)", arr[0])
	}
	if _, ok := arr[1].(ast.PdfString); !ok {
		t.Errorf("arr[1] = %T, want PdfString", arr[1])
	}
}
