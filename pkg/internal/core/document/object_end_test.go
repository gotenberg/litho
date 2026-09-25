package document

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/parser"
)

// objectEndDoc opens a document whose object 3 opens a literal string that
// closes only inside object 4, after object 4's entry /A, so that reading on
// past object 3's end parses object 4's hex string <zz> as a value of object
// 3. Object 5 holds a string that balances but carries "1 0 obj" and "endobj"
// at the start of lines. The document is in compliance mode, so each parse
// records its format violations.
func objectEndDoc(t *testing.T) *Document {
	t.Helper()
	data := buildDestPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [] /Count 0 >>",
		"<< /Type /XObject /Subtype /Image /Width 4 /Junk (\x80\x01binary",
		"<< /A (x)) /B <zz> >>",
		"<< /Title (holds\n1 0 obj\nendobj\nas text) /Author (x) >>",
		"<< /Z 1 >>",
	)
	doc, err := NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	doc.SetComplianceMode(true)
	return doc
}

// hexStringViolations counts the ISO 19005-2, 6.1.6 hex string findings among
// violations.
func hexStringViolations(violations []error) int {
	n := 0
	for _, v := range violations {
		if errors.Is(v, parser.ErrHexStringInvalidChar) || errors.Is(v, parser.ErrHexStringOddDigits) {
			n++
		}
	}
	return n
}

// TestResolveStopsObjectAtNextObject pins that resolving an object whose
// literal string never closes before the next object stops there: the
// dictionary keeps the pairs before the string, and the next object's hex
// string is reported against that object, once, rather than against this one.
//
// # Corpus evidence
//
//   - pdfjs issue11678.pdf: object 21's string ran across 13 objects, and the
//     hex strings read there added 97 findings of ISO 19005-2, 6.1.6 t1 and t2
//     that veraPDF does not report.
func TestResolveStopsObjectAtNextObject(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc := objectEndDoc(t)

	obj, err := doc.Resolve(ctx, ast.PdfIndirectRef{ObjectNum: 3})
	if err != nil {
		t.Fatalf("Resolve(3): %v", err)
	}
	dict, ok := obj.(ast.PdfDict)
	if !ok {
		t.Fatalf("object 3 = %T, want ast.PdfDict", obj)
	}
	if dict["Subtype"] != ast.PdfName("Image") || dict["Width"] != ast.PdfInt(4) {
		t.Errorf("object 3 lost the pairs before its string: %v", dict)
	}
	if b, found := dict["B"]; found {
		t.Errorf("object 3 took object 4's /B = %v", b)
	}
	if n := hexStringViolations(doc.FormatViolations()); n != 0 {
		t.Errorf("object 3 reported %d hex string findings of object 4", n)
	}

	if _, err := doc.Resolve(ctx, ast.PdfIndirectRef{ObjectNum: 4}); err != nil {
		t.Fatalf("Resolve(4): %v", err)
	}
	if n := hexStringViolations(doc.FormatViolations()); n != 1 {
		t.Errorf("object 4 reported %d hex string findings, want 1 for <zz>", n)
	}
}

// TestResolveKeepsStringHoldingObjectKeywords pins that the bound comes from
// the cross-reference table: a string holding "1 0 obj" and "endobj" at the
// start of lines, which ISO 32000-1, 7.3.4.2 allows, is read whole.
func TestResolveKeepsStringHoldingObjectKeywords(t *testing.T) {
	t.Parallel()
	doc := objectEndDoc(t)

	obj, err := doc.Resolve(context.Background(), ast.PdfIndirectRef{ObjectNum: 5})
	if err != nil {
		t.Fatalf("Resolve(5): %v", err)
	}
	dict, ok := obj.(ast.PdfDict)
	if !ok {
		t.Fatalf("object 5 = %T, want ast.PdfDict", obj)
	}
	if want := ast.PdfString("holds\n1 0 obj\nendobj\nas text"); dict["Title"] != want {
		t.Errorf("/Title = %q, want %q", dict["Title"], want)
	}
	if dict["Author"] != ast.PdfString("x") {
		t.Errorf("/Author = %v, want (x)", dict["Author"])
	}
}

// TestObjectEndSkipsOffsetMissingItsHeader pins that a table offset whose
// bytes do not open the header of the object the table names bounds nothing:
// the parse runs on to the next offset that does.
func TestObjectEndSkipsOffsetMissingItsHeader(t *testing.T) {
	t.Parallel()
	doc := objectEndDoc(t)

	third, fourth := doc.xrefEntries[3].Offset, doc.xrefEntries[4].Offset
	fifth := doc.xrefEntries[5].Offset
	if got := doc.objectEnd(third); got != fourth {
		t.Fatalf("objectEnd(object 3) = %d, want object 4's offset %d", got, fourth)
	}

	// Point the declared start of object 4 one byte into its header.
	for i, s := range doc.declaredStarts {
		if s.num == 4 {
			doc.declaredStarts[i].offset++
		}
	}
	if got := doc.objectEnd(third); got != fifth {
		t.Errorf("objectEnd(object 3) = %d with object 4's offset astray, want object 5's offset %d", got, fifth)
	}
}
