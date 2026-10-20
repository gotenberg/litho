package pdf

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
)

// plantedPDF builds a one-page document that draws body, with a copy of the
// redacted word planted where the plant puts it.
//
// Objects 1 to 6 are the catalog, the page tree, the page, the font, the
// resources and the content stream; a plant adds its own objects from 7 on and
// may add entries to the catalog, the page and the trailer.
func plantedPDF(body string, p plant) []byte {
	objs := "%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R " + p.catalog + " >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n" +
		"5 0 obj\n<< /Font << /F1 4 0 R >> >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " +
		"/Resources 5 0 R /Contents 6 0 R " + p.page + " >>\nendobj\n" +
		"6 0 obj\n<< /Length " + strconv.Itoa(len(body)) + " >>\nstream\n" + body + "\nendstream\nendobj\n" +
		p.objects
	return []byte(objs + "trailer\n<< /Size 20 /Root 1 0 R " + p.trailer + " >>\n%%EOF\n")
}

// plant names one home for a copy of the redacted word.
type plant struct {
	name    string
	catalog string
	page    string
	trailer string
	objects string
}

// plantStream writes one uncompressed stream object.
func plantStream(num int, dict, data string) string {
	return strconv.Itoa(num) + " 0 obj\n<< " + dict + " /Length " + strconv.Itoa(len(data)) +
		" >>\nstream\n" + data + "\nendstream\nendobj\n"
}

// plantsOf returns a copy of word in each home the redaction must clear or
// refuse over: an annotation value, an outline title, /Info, the XMP packet,
// private catalog data, a form field value and an embedded file.
func plantsOf(word string) []plant {
	return []plant{
		{
			name: "annotation contents",
			page: "/Annots [7 0 R]",
			objects: "7 0 obj\n<< /Type /Annot /Subtype /Text /Rect [400 100 420 120] /Contents (" +
				word + ") >>\nendobj\n",
		},
		{
			name:    "outline title",
			catalog: "/Outlines 8 0 R",
			objects: "8 0 obj\n<< /Type /Outlines /First 9 0 R /Last 9 0 R /Count 1 >>\nendobj\n" +
				"9 0 obj\n<< /Title (" + word + ") /Parent 8 0 R /Dest [3 0 R /Fit] >>\nendobj\n",
		},
		{
			name:    "info title",
			trailer: "/Info 10 0 R",
			objects: "10 0 obj\n<< /Title (" + word + ") >>\nendobj\n",
		},
		{
			name:    "xmp metadata",
			catalog: "/Metadata 11 0 R",
			objects: plantStream(11, "/Type /Metadata /Subtype /XML",
				`<x:xmpmeta xmlns:x="adobe:ns:meta/"><dc:title>`+word+`</dc:title></x:xmpmeta>`),
		},
		{
			name:    "catalog piece info",
			catalog: "/PieceInfo 12 0 R",
			objects: "12 0 obj\n<< /SomeApp << /Private (" + word + ") >> >>\nendobj\n",
		},
		{
			name:    "form field value",
			catalog: "/AcroForm << /Fields [13 0 R] >>",
			page:    "/Annots [13 0 R]",
			objects: "13 0 obj\n<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /V (" + word +
				") /Rect [400 100 500 120] /P 3 0 R >>\nendobj\n",
		},
		{
			name:    "embedded file",
			catalog: "/Names << /EmbeddedFiles << /Names [(notes.txt) 14 0 R] >> >>",
			objects: "14 0 obj\n<< /Type /Filespec /F (notes.txt) /EF << /F 15 0 R >> >>\nendobj\n" +
				plantStream(15, "/Type /EmbeddedFile", "draft: "+word+" again"),
		},
	}
}

// assertPlantCaught fails unless the redaction either refused or wrote a file
// holding no copy of word.
func assertPlantCaught(t *testing.T, src []byte, word string) {
	t.Helper()
	ctx := context.Background()

	if n := bytes.Count(src, []byte(word)); n < 1 {
		t.Fatalf("fixture holds no planted copy of %q", word)
	}
	r, err := Open(ctx, bytes.NewReader(src), int64(len(src)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	w := NewWriter(r)
	if _, err := w.Redact(ctx, []RedactRegion{coversSecret}); err != nil {
		if errors.Is(err, ErrRedactionIncomplete) || errors.Is(err, ErrRedactionRefused) {
			return
		}
		t.Fatalf("Redact: %v", err)
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		if errors.Is(err, ErrRedactionIncomplete) {
			return
		}
		t.Fatalf("WriteTo: %v", err)
	}
	if bytes.Contains(out.Bytes(), []byte(word)) {
		t.Errorf("Redact succeeded and the planted %q survives in the output", word)
	}
	if text := allPagesText(t, out.Bytes()); strings.Contains(text, word) {
		t.Errorf("Redact succeeded and %q survives in the page text %q", word, text)
	}
}

// TestWriter_Redact_CatchesEveryPlantedCopy pins the fail-closed property over
// every home of a copy, for a word drawn by one operator and for the same word
// drawn one glyph per operator, as a producer that positions each glyph draws
// it. Whatever the scan looks for, a removed word copied into an annotation,
// an outline, the metadata, private data, a form field or an attachment is
// either cleared or refused over, never written.
func TestWriter_Redact_CatchesEveryPlantedCopy(t *testing.T) {
	t.Parallel()

	bodies := map[string]string{
		"one operator": "BT /F1 12 Tf 100 700 Td (SECRET) Tj ET BT /F1 12 Tf 100 600 Td (KEEP) Tj ET",
		"one operator per glyph": "BT /F1 12 Tf 100 700 Td (S) Tj (E) Tj (C) Tj (R) Tj (E) Tj (T) Tj ET " +
			"BT /F1 12 Tf 100 600 Td (KEEP) Tj ET",
	}
	for drawn, body := range bodies {
		for _, p := range plantsOf("SECRET") {
			t.Run(drawn+"/"+p.name, func(t *testing.T) {
				t.Parallel()
				assertPlantCaught(t, plantedPDF(body, p), "SECRET")
			})
		}
	}
}
