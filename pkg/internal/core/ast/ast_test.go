package ast

import (
	"strings"
	"testing"
)

func TestPdfNull_String(t *testing.T) {
	n := PdfNull{}
	if n.String() != "null" {
		t.Errorf("PdfNull.String() = %q, want %q", n.String(), "null")
	}
}

func TestPdfBool_String(t *testing.T) {
	if PdfBool(true).String() != "true" {
		t.Error("PdfBool(true).String() != \"true\"")
	}
	if PdfBool(false).String() != "false" {
		t.Error("PdfBool(false).String() != \"false\"")
	}
}

func TestPdfInt_String(t *testing.T) {
	tests := []struct {
		val  PdfInt
		want string
	}{
		{0, "0"},
		{42, "42"},
		{-7, "-7"},
	}
	for _, tc := range tests {
		if got := tc.val.String(); got != tc.want {
			t.Errorf("PdfInt(%d).String() = %q, want %q", tc.val, got, tc.want)
		}
	}
}

func TestPdfReal_String(t *testing.T) {
	tests := []struct {
		val  PdfReal
		want string
	}{
		{0.0, "0"},
		{3.14, "3.14"},
		{-1.5, "-1.5"},
	}
	for _, tc := range tests {
		got := tc.val.String()
		if got != tc.want {
			t.Errorf("PdfReal(%v).String() = %q, want %q", tc.val, got, tc.want)
		}
	}
}

func TestPdfName_String(t *testing.T) {
	if got := PdfName("Type").String(); got != "/Type" {
		t.Errorf("PdfName(\"Type\").String() = %q, want %q", got, "/Type")
	}
}

func TestPdfString_String(t *testing.T) {
	if got := PdfString("Hello").String(); got != "(Hello)" {
		t.Errorf("PdfString(\"Hello\").String() = %q, want %q", got, "(Hello)")
	}
}

func TestPdfArray_String(t *testing.T) {
	a := PdfArray{PdfInt(1), PdfInt(2), PdfInt(3)}
	if got := a.String(); got != "[1 2 3]" {
		t.Errorf("PdfArray.String() = %q, want %q", got, "[1 2 3]")
	}
}

func TestPdfArray_StringWithNil(t *testing.T) {
	a := PdfArray{PdfInt(1), nil, PdfInt(3)}
	if got := a.String(); got != "[1 null 3]" {
		t.Errorf("PdfArray.String() with nil = %q, want %q", got, "[1 null 3]")
	}
}

func TestPdfArray_Empty(t *testing.T) {
	a := PdfArray{}
	if got := a.String(); got != "[]" {
		t.Errorf("empty PdfArray.String() = %q, want %q", got, "[]")
	}
}

func TestPdfDict_String(t *testing.T) {
	d := PdfDict{
		"Type": PdfName("Catalog"),
	}
	got := d.String()
	if !strings.Contains(got, "/Type /Catalog") {
		t.Errorf("PdfDict.String() = %q, expected to contain /Type /Catalog", got)
	}
	if !strings.HasPrefix(got, "<<") || !strings.HasSuffix(got, ">>") {
		t.Errorf("PdfDict.String() = %q, expected << >> delimiters", got)
	}
}

func TestPdfDict_NilValue(t *testing.T) {
	d := PdfDict{
		"Key": nil,
	}
	got := d.String()
	if !strings.Contains(got, "null") {
		t.Errorf("PdfDict with nil value.String() = %q, expected null", got)
	}
}

func TestPdfIndirectRef_String(t *testing.T) {
	r := PdfIndirectRef{ObjectNum: 10, GenNum: 0}
	if got := r.String(); got != "10 0 R" {
		t.Errorf("PdfIndirectRef.String() = %q, want %q", got, "10 0 R")
	}
}

func TestPdfStream_String(t *testing.T) {
	s := NewStream(PdfDict{"Length": PdfInt(5)}, []byte("hello"))
	got := s.String()
	if !strings.Contains(got, "stream") {
		t.Errorf("PdfStream.String() = %q, expected 'stream'", got)
	}
	if !strings.Contains(got, "endstream") {
		t.Errorf("PdfStream.String() = %q, expected 'endstream'", got)
	}
}

func TestPdfStream_NilDict(t *testing.T) {
	s := NewStream(nil, []byte{1, 2})
	got := s.String()
	if !strings.Contains(got, "<<>>") {
		t.Errorf("PdfStream with nil dict.String() = %q, expected <<>>", got)
	}
}

func TestIsPdfObject_Interface(t *testing.T) {
	// Verify all types satisfy PdfObject.
	var _ PdfObject = PdfNull{}
	var _ PdfObject = PdfBool(true)
	var _ PdfObject = PdfInt(0)
	var _ PdfObject = PdfReal(0)
	var _ PdfObject = PdfName("")
	var _ PdfObject = PdfString("")
	var _ PdfObject = PdfArray{}
	var _ PdfObject = PdfDict{}
	var _ PdfObject = PdfIndirectRef{}
	var _ PdfObject = NewStream(nil, nil)
}
