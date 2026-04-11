package document

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestGetFloat covers the object-to-float coercion used across the
// codebase. It deliberately tests the public [GetFloat] behavior rather
// than any internal branching.
func TestGetFloat(t *testing.T) {
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
			got := GetFloat(tc.obj)
			if got != tc.want {
				t.Errorf("GetFloat(%v) = %v, want %v", tc.obj, got, tc.want)
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
