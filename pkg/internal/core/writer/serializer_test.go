package writer

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/ioat"
	"github.com/gotenberg/litho/pkg/internal/core/lexer"
	"github.com/gotenberg/litho/pkg/internal/core/parser"
)

// roundTrip serializes obj, parses the result back, and returns the parsed object.
func roundTrip(t *testing.T, obj ast.PdfObject) ast.PdfObject {
	t.Helper()
	var buf bytes.Buffer
	_, err := Serialize(context.Background(), &buf, obj)
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}
	r := ioat.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()), 0)
	lex := lexer.NewLexer(r)
	p := parser.NewParser(lex)
	parsed, err := p.ParseObject(context.Background())
	if err != nil {
		t.Fatalf("ParseObject(%q): %v", buf.String(), err)
	}
	return parsed
}

func TestSerialize_Null(t *testing.T) {
	var buf bytes.Buffer
	_, err := Serialize(context.Background(), &buf, ast.PdfNull{})
	if err != nil {
		t.Fatal(err)
	}
	if buf.String() != "null" {
		t.Errorf("got %q, want %q", buf.String(), "null")
	}
}

func TestSerialize_NilObject(t *testing.T) {
	var buf bytes.Buffer
	_, err := Serialize(context.Background(), &buf, nil)
	if err != nil {
		t.Fatal(err)
	}
	if buf.String() != "null" {
		t.Errorf("got %q, want %q", buf.String(), "null")
	}
}

func TestSerialize_Bool(t *testing.T) {
	got := roundTrip(t, ast.PdfBool(true))
	if v, ok := got.(ast.PdfBool); !ok || !bool(v) {
		t.Errorf("round-trip true: got %v", got)
	}
	got = roundTrip(t, ast.PdfBool(false))
	if v, ok := got.(ast.PdfBool); !ok || bool(v) {
		t.Errorf("round-trip false: got %v", got)
	}
}

func TestSerialize_Int(t *testing.T) {
	for _, val := range []int64{0, 42, -7, 1234567890} {
		got := roundTrip(t, ast.PdfInt(val))
		if v, ok := got.(ast.PdfInt); !ok || int64(v) != val {
			t.Errorf("round-trip %d: got %v", val, got)
		}
	}
}

func TestSerialize_Real(t *testing.T) {
	got := roundTrip(t, ast.PdfReal(3.14))
	if v, ok := got.(ast.PdfReal); !ok {
		t.Errorf("round-trip 3.14: got %T %v", got, got)
	} else if float64(v) < 3.139 || float64(v) > 3.141 {
		t.Errorf("round-trip 3.14: got %f", float64(v))
	}
}

func TestSerialize_Real_Integer(t *testing.T) {
	// A real that is actually an integer value should serialize cleanly.
	var buf bytes.Buffer
	_, err := Serialize(context.Background(), &buf, ast.PdfReal(72.0))
	if err != nil {
		t.Fatal(err)
	}
	if buf.String() != "72" {
		t.Errorf("got %q, want %q", buf.String(), "72")
	}
}

func TestSerialize_Name(t *testing.T) {
	got := roundTrip(t, ast.PdfName("Type"))
	if v, ok := got.(ast.PdfName); !ok || string(v) != "Type" {
		t.Errorf("round-trip /Type: got %v", got)
	}
}

func TestSerialize_Name_Escaping(t *testing.T) {
	// Name with a space requires hex escaping.
	var buf bytes.Buffer
	_, err := Serialize(context.Background(), &buf, ast.PdfName("A B"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "#20") {
		t.Errorf("expected hex escape for space, got %q", buf.String())
	}
	got := roundTrip(t, ast.PdfName("A B"))
	if v, ok := got.(ast.PdfName); !ok || string(v) != "A B" {
		t.Errorf("round-trip name with space: got %v", got)
	}
}

func TestSerialize_String_Literal(t *testing.T) {
	got := roundTrip(t, ast.PdfString("Hello World"))
	if v, ok := got.(ast.PdfString); !ok || string(v) != "Hello World" {
		t.Errorf("round-trip string: got %v", got)
	}
}

func TestSerialize_String_Escapes(t *testing.T) {
	got := roundTrip(t, ast.PdfString("a(b)c\\d"))
	if v, ok := got.(ast.PdfString); !ok || string(v) != "a(b)c\\d" {
		t.Errorf("round-trip escaped string: got %q", string(v))
	}
}

func TestSerialize_String_Binary(t *testing.T) {
	// Binary content should use hex form.
	data := ast.PdfString(string([]byte{0x00, 0x01, 0xFF}))
	var buf bytes.Buffer
	_, err := Serialize(context.Background(), &buf, data)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(buf.String(), "<") {
		t.Errorf("expected hex form for binary, got %q", buf.String())
	}
}

func TestSerialize_Array(t *testing.T) {
	arr := ast.PdfArray{ast.PdfInt(1), ast.PdfName("Type"), ast.PdfBool(true)}
	got := roundTrip(t, arr)
	if v, ok := got.(ast.PdfArray); !ok || len(v) != 3 {
		t.Errorf("round-trip array: got %v", got)
	}
}

func TestSerialize_Dict(t *testing.T) {
	d := ast.PdfDict{
		"Type": ast.PdfName("Page"),
		"Size": ast.PdfInt(42),
	}
	got := roundTrip(t, d)
	if v, ok := got.(ast.PdfDict); !ok {
		t.Errorf("round-trip dict: got %T", got)
	} else {
		if name, ok := v["Type"].(ast.PdfName); !ok || string(name) != "Page" {
			t.Errorf("dict /Type: got %v", v["Type"])
		}
	}
}

func TestSerialize_IndirectRef(t *testing.T) {
	var buf bytes.Buffer
	_, err := Serialize(context.Background(), &buf, ast.PdfIndirectRef{ObjectNum: 5, GenNum: 0})
	if err != nil {
		t.Fatal(err)
	}
	if buf.String() != "5 0 R" {
		t.Errorf("got %q, want %q", buf.String(), "5 0 R")
	}
}

func TestSerialize_Stream(t *testing.T) {
	data := []byte("BT /F1 12 Tf (Hello) Tj ET")
	dict := ast.PdfDict{
		"Filter": ast.PdfName("FlateDecode"),
	}
	stream := ast.NewStream(dict, data)

	var buf bytes.Buffer
	_, err := Serialize(context.Background(), &buf, stream)
	if err != nil {
		t.Fatal(err)
	}

	// Verify the output contains the stream markers and raw data.
	output := buf.String()
	if !strings.Contains(output, "stream\r\n") {
		t.Error("missing 'stream' keyword")
	}
	if !strings.Contains(output, "\r\nendstream") {
		t.Error("missing 'endstream' keyword")
	}
	if !strings.Contains(output, string(data)) {
		t.Error("stream data not preserved")
	}
	// Verify /Length was updated.
	if !strings.Contains(output, "/Length 26") {
		t.Errorf("expected /Length 26, got: %s", output)
	}
}

func TestSerializeIndirect(t *testing.T) {
	var buf bytes.Buffer
	_, err := SerializeIndirect(context.Background(), &buf, 5, 0, ast.PdfInt(42))
	if err != nil {
		t.Fatal(err)
	}
	want := "5 0 obj\n42\nendobj\n"
	if buf.String() != want {
		t.Errorf("got %q, want %q", buf.String(), want)
	}
}

func TestSerialize_DictKeySorted(t *testing.T) {
	d := ast.PdfDict{
		"Z": ast.PdfInt(1),
		"A": ast.PdfInt(2),
		"M": ast.PdfInt(3),
	}
	var buf bytes.Buffer
	_, err := Serialize(context.Background(), &buf, d)
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	aIdx := strings.Index(out, "/A")
	mIdx := strings.Index(out, "/M")
	zIdx := strings.Index(out, "/Z")
	if aIdx > mIdx || mIdx > zIdx {
		t.Errorf("keys not sorted: %s", out)
	}
}

func TestFormatReal(t *testing.T) {
	tests := []struct {
		input float64
		want  string
	}{
		{0, "0"},
		{1, "1"},
		{72, "72"},
		{3.14, "3.14"},
		{0.5, "0.5"},
		{-2.5, "-2.5"},
		{100.000, "100"},
		// Values that fit the 6-decimal grid are unchanged.
		{0.000001, "0.000001"},
		// Nonzero magnitudes smaller than the grid must survive rather
		// than collapse to "0" and lose data on round-trip.
		{0.0000005, "0.0000005"},
		{0.0000001, "0.0000001"},
		{-0.0000001, "-0.0000001"},
		{1.5e-8, "0.000000015"},
	}
	for _, tc := range tests {
		got := formatReal(tc.input)
		if got != tc.want {
			t.Errorf("formatReal(%v): got %q, want %q", tc.input, got, tc.want)
		}
	}
}
