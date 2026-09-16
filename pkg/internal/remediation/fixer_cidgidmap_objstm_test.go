package remediation

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/xref"
)

// objStmCIDFontPDF builds a PDF whose CIDFontType2 lives inside an object
// stream, so the font's cross-reference entry is type 2 (ISO 32000-1, 7.5.7
// and Table 18) instead of a byte offset. Real producers store font
// dictionaries this way: every pdfjs corpus file carrying the CIDToGIDMap
// violation keeps its CIDFont compressed.
//
// The object stream and the cross-reference stream are both unfiltered, which
// keeps the fixture readable and exercises the same xref entry types a
// Flate-compressed producer emits.
func objStmCIDFontPDF(fontBody string) []byte {
	var buf bytes.Buffer
	offsets := make(map[int]int64)
	writeObj := func(num int, body string) {
		offsets[num] = int64(buf.Len())
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", num, body)
	}

	buf.WriteString("%PDF-1.5\n")
	writeObj(1, "<< /Type /Catalog /Pages 2 0 R >>")
	writeObj(2, "<< /Type /Pages /Kids [3 0 R] /Count 1 >>")
	writeObj(3, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>")

	// Object 5 is stored inside object stream 4.
	header := "5 0\n"
	payload := header + fontBody
	writeObj(4, fmt.Sprintf(
		"<< /Type /ObjStm /N 1 /First %d /Length %d >>\nstream\n%s\nendstream",
		len(header), len(payload), payload))

	xrefOffset := int64(buf.Len())

	// Cross-reference stream entries, /W [1 4 2] over objects 0 through 6.
	var entries []byte
	put := func(entryType byte, field2 uint32, field3 uint16) {
		entries = append(entries, entryType,
			byte(field2>>24), byte(field2>>16), byte(field2>>8), byte(field2),
			byte(field3>>8), byte(field3))
	}
	put(xref.XRefTypeFree, 0, 65535)
	put(xref.XRefTypeNormal, uint32(offsets[1]), 0)
	put(xref.XRefTypeNormal, uint32(offsets[2]), 0)
	put(xref.XRefTypeNormal, uint32(offsets[3]), 0)
	put(xref.XRefTypeNormal, uint32(offsets[4]), 0)
	put(xref.XRefTypeCompressed, 4, 0)
	put(xref.XRefTypeNormal, uint32(xrefOffset), 0)

	fmt.Fprintf(&buf,
		"6 0 obj\n<< /Type /XRef /Size 7 /W [1 4 2] /Root 1 0 R /Length %d >>\nstream\n%s\nendstream\nendobj\n",
		len(entries), entries)
	fmt.Fprintf(&buf, "startxref\n%d\n%%%%EOF\n", xrefOffset)

	return buf.Bytes()
}

func TestFixCIDToGIDMap_FontInsideObjectStream(t *testing.T) {
	pdf := objStmCIDFontPDF("<< /Type /Font /Subtype /CIDFontType2 /BaseFont /ABCDEF+Sans >>")
	doc, alloc := newTestDoc(t, pdf)

	// The fixture only means anything if the reader really reports a
	// compressed entry. A rebuilt cross-reference would report type 1 and the
	// assertions below would pass without exercising the compressed path.
	if got := doc.Index.Entries[5].Type; got != xref.XRefTypeCompressed {
		t.Fatalf("fixture: object 5 has xref type %d, want %d (in an object stream)",
			got, xref.XRefTypeCompressed)
	}

	descs, err := fixCIDToGIDMap(context.Background(), alloc, doc, Options{}, nil, "PDF/A-2")
	if err != nil {
		t.Fatalf("fixCIDToGIDMap: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("a CIDFontType2 inside an object stream must be repaired, got %v", descs)
	}
	if got := pendingDict(t, alloc, 5)["CIDToGIDMap"]; got != ast.PdfName("Identity") {
		t.Errorf("/CIDToGIDMap: got %v, want /Identity", got)
	}
}
