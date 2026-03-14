package parser

import (
	"bytes"
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
	obj, err := p.ParseObject()
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

func TestParseArray_MissingCloseBracket(t *testing.T) {
	// Array with no closing ].
	p := newParser("[1 2 3")
	obj, err := p.ParseObject()
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
	obj, err := p.ParseObject()
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
	obj, err := p.ParseObject()
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
	obj, err := p.ParseObject()
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
	obj, err := p.ParseObject()
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
	obj, err := p.ParseObject()
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

func TestParseNestedDict_MissingInnerClose(t *testing.T) {
	// Inner dict missing >> but outer has >>.
	// << /Resources << /Font << /F1 1 0 R >> >>
	// Here inner is fine, but test: << /Resources << /Font 1 0 R >>
	// which would close outer dict, leaving Resources with partial inner.
	p := newParser("<< /A << /B /C >>")
	obj, err := p.ParseObject()
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
	obj, err := p.ParseObject()
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

func TestParseArray_HexStringWithoutSpace(t *testing.T) {
	// Hex string immediately after a number: "3<ABCD>" should parse as 3 then hex string.
	p := newParser("[3<ABCD>]")
	obj, err := p.ParseObject()
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
