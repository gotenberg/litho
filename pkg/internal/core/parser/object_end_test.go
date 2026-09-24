package parser

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/ioat"
	"github.com/gotenberg/litho/pkg/internal/core/lexer"
)

// parseObjectEndingAt parses the indirect object at the start of src with
// format checking, bounded at the first occurrence of next when bounded is
// set. It returns the object and the format violations the parse recorded.
func parseObjectEndingAt(t *testing.T, src, next string, bounded bool) (ast.PdfObject, []error) {
	t.Helper()
	end := strings.Index(src, next)
	if end < 0 {
		t.Fatalf("%q does not hold %q", src, next)
	}
	lex := lexer.NewLexer(ioat.NewReader(bytes.NewReader([]byte(src)), int64(len(src)), 0))
	p := NewParser(lex).WithFormatChecking()
	if bounded {
		p.WithObjectEnd(int64(end))
	}
	obj, err := p.ParseObject(context.Background())
	if err != nil {
		t.Fatalf("ParseObject: %v", err)
	}
	return obj, p.FormatViolations
}

// hasHexStringViolation reports whether violations hold either ISO 19005-2,
// 6.1.6 hex string finding.
func hasHexStringViolation(violations []error) bool {
	for _, v := range violations {
		if errors.Is(v, ErrHexStringInvalidChar) || errors.Is(v, ErrHexStringOddDigits) {
			return true
		}
	}
	return false
}

// TestWithObjectEndStopsRunawayLiteralString pins that a literal string still
// open at the next object ends there, in a value position and in a key
// position alike. Its closing parenthesis comes inside object 2, and reading
// on to it parsed object 2's hex string as a value of object 1, with the ISO
// 19005-2, 6.1.6 finding that goes with it.
//
// # Corpus evidence
//
//   - pdfjs issue11678.pdf: binary data in object 21's dictionary opens a
//     literal string that closes 184 KB later, and the 97 hex string findings
//     read past it were reported against the file.
func TestWithObjectEndStopsRunawayLiteralString(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
	}{
		{
			"value position",
			"1 0 obj\n<< /Type /XObject /Width 628 /Junk (\x80\x01binary\nendobj\n" +
				"2 0 obj\n<< /A (x)) /B <zz> >>\nendobj\n",
		},
		{
			"key position",
			"1 0 obj\n<< /Type /XObject /Width 628 (\x80\x01binary\nendobj\n" +
				"2 0 obj\n<< /A (x)) /B <zz> >>\nendobj\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// The unbounded parse is the control: it must read object 2's
			// entry, or the bounded one below proves nothing.
			obj, violations := parseObjectEndingAt(t, tc.src, "2 0 obj", false)
			if dict, ok := obj.(ast.PdfDict); !ok || dict["B"] == nil || !hasHexStringViolation(violations) {
				t.Fatalf("unbounded parse = %v with %v, want object 2's /B and its hex string finding", obj, violations)
			}

			obj, violations = parseObjectEndingAt(t, tc.src, "2 0 obj", true)
			dict, ok := obj.(ast.PdfDict)
			if !ok {
				t.Fatalf("bounded parse = %T, want ast.PdfDict", obj)
			}
			if dict["Type"] != ast.PdfName("XObject") || dict["Width"] != ast.PdfInt(628) {
				t.Errorf("bounded parse lost the pairs before the string: %v", dict)
			}
			for _, key := range []ast.PdfName{"Junk", "A", "B"} {
				if v, found := dict[key]; found {
					t.Errorf("bounded parse kept /%s = %v from past the object's end", key, v)
				}
			}
			if hasHexStringViolation(violations) {
				t.Errorf("bounded parse reported a hex string of the next object: %v", violations)
			}
		})
	}
}

// TestWithObjectEndStopsRunawayDictionary pins that a dictionary missing both
// its closing delimiter and endobj ends at the next object rather than parsing
// that object as one of its keys.
func TestWithObjectEndStopsRunawayDictionary(t *testing.T) {
	src := "1 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica\n" +
		"2 0 obj\n<< /Type /Page /Bad <zz> >>\nendobj\n"

	_, violations := parseObjectEndingAt(t, src, "2 0 obj", false)
	if !hasHexStringViolation(violations) {
		t.Fatalf("unbounded parse reported %v, want object 2's hex string finding", violations)
	}

	obj, violations := parseObjectEndingAt(t, src, "2 0 obj", true)
	dict, ok := obj.(ast.PdfDict)
	if !ok {
		t.Fatalf("bounded parse = %T, want ast.PdfDict", obj)
	}
	want := ast.PdfDict{"Type": ast.PdfName("Font"), "Subtype": ast.PdfName("Type1"), "BaseFont": ast.PdfName("Helvetica")}
	if len(dict) != len(want) {
		t.Errorf("bounded parse = %v, want %v", dict, want)
	}
	for k, v := range want {
		if dict[k] != v {
			t.Errorf("bounded parse /%s = %v, want %v", k, dict[k], v)
		}
	}
	if hasHexStringViolation(violations) {
		t.Errorf("bounded parse reported a hex string of the next object: %v", violations)
	}
}

// TestWithObjectEndKeepsStringHoldingObjectKeywords pins that the bound comes
// from where the next object starts, not from the bytes: ISO 32000-1, 7.3.4.2
// lets a literal string hold "1 0 obj" and "endobj", even at the start of a
// line, as long as its parentheses balance.
func TestWithObjectEndKeepsStringHoldingObjectKeywords(t *testing.T) {
	title := "see \\(1 0 obj\\) and\n1 0 obj\nendobj (nested) here"
	src := "3 0 obj\n<< /Title (" + title + ") /Author (x) >>\nendobj\n" +
		"4 0 obj\n<< >>\nendobj\n"

	obj, _ := parseObjectEndingAt(t, src, "4 0 obj", true)
	dict, ok := obj.(ast.PdfDict)
	if !ok {
		t.Fatalf("ParseObject = %T, want ast.PdfDict", obj)
	}
	want := "see (1 0 obj) and\n1 0 obj\nendobj (nested) here"
	if got := dict["Title"]; got != ast.PdfString(want) {
		t.Errorf("/Title = %q, want %q", got, want)
	}
	if got := dict["Author"]; got != ast.PdfString("x") {
		t.Errorf("/Author = %v, want (x)", got)
	}
}

// TestWithObjectEndLeavesStreamDataWhole pins that a stream's data is read by
// its /Length, past the end of the object: an object header the bound came
// from may sit inside the data, as it does in an uncompressed embedded file.
func TestWithObjectEndLeavesStreamDataWhole(t *testing.T) {
	data := "%PDF-1.4\n2 0 obj\n<< >>\nendobj\n%%EOF\n"
	src := "1 0 obj\n<< /Length " + strconv.Itoa(len(data)) + " >>\nstream\n" + data + "\nendstream\nendobj\n"

	obj, _ := parseObjectEndingAt(t, src, "2 0 obj", true)
	stream, ok := obj.(ast.PdfStream)
	if !ok {
		t.Fatalf("ParseObject = %T, want ast.PdfStream", obj)
	}
	got, err := stream.Bytes(context.Background())
	if err != nil {
		t.Fatalf("reading stream data: %v", err)
	}
	if string(got) != data {
		t.Errorf("stream data = %q, want %q", got, data)
	}
}
