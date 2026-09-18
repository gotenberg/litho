package mutations

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// scanFixture builds a document holding the same word three ways: a literal
// string, a UTF-16BE string with a BOM, and a Flate-compressed stream. Each is
// a separate reason a naive scan would miss a surviving copy.
func scanFixture(t *testing.T) *document.Document {
	t.Helper()
	ctx := context.Background()

	body := "BT /F1 12 Tf 100 700 Td (KEEP) Tj ET"
	// "TOPSECRET" as UTF-16BE with the byte-order mark PDF uses for text
	// strings (ISO 32000-1, 7.9.2.2). A scan comparing raw bytes sees no
	// recognisable word here at all.
	utf16Hex := "FEFF0054004F0050005300450043005200450054"

	streamPlain := []byte("hidden copy: STREAMSECRET here")
	streamData, err := flateCompress(streamPlain)
	if err != nil {
		t.Fatalf("flateCompress: %v", err)
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n" +
		"5 0 obj\n<< /Font << /F1 4 0 R >> >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 5 0 R /Contents 6 0 R >>\nendobj\n" +
		"6 0 obj\n<< /Length " + strconv.Itoa(len(body)) + " >>\nstream\n" + body + "\nendstream\nendobj\n" +
		"7 0 obj\n<< /Title (LITERALSECRET) /Subject <" + utf16Hex + "> >>\nendobj\n" +
		"8 0 obj\n<< /Filter /FlateDecode /Length " + strconv.Itoa(len(streamData)) + " >>\nstream\n")
	buf.Write(streamData)
	buf.WriteString("\nendstream\nendobj\n" +
		"trailer\n<< /Size 9 /Root 1 0 R /Info 7 0 R >>\n%%EOF\n")

	data := buf.Bytes()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	return doc
}

func TestScanForTokens_FindsALiteralString(t *testing.T) {
	// Positive control. If this fails with no hits, the sweep enumerated
	// nothing and every "clean" result below would be meaningless.
	doc := scanFixture(t)

	res, err := ScanForTokens(context.Background(), doc, []string{"LITERALSECRET"})
	if err != nil {
		t.Fatalf("ScanForTokens: %v", err)
	}
	if len(res.Hits) == 0 {
		t.Fatalf("no hits: the scan found no surviving copy (result %+v)", res)
	}
	for _, hit := range res.Hits {
		if hit.Token != "LITERALSECRET" {
			t.Errorf("hit = %+v, want the searched token", hit)
		}
	}
}

func TestScanForTokens_FindsAUTF16String(t *testing.T) {
	// The bytes of this string spell nothing: every character is preceded by a
	// zero byte and the whole is led by a BOM. Only decoding finds it, which is
	// why the scan cannot be a byte search.
	doc := scanFixture(t)

	res, err := ScanForTokens(context.Background(), doc, []string{"TOPSECRET"})
	if err != nil {
		t.Fatalf("ScanForTokens: %v", err)
	}
	if len(res.Hits) == 0 {
		t.Errorf("a UTF-16 copy of the token was not found: %+v", res)
	}
}

func TestScanForTokens_FindsATokenInsideACompressedStream(t *testing.T) {
	// The stored bytes are Flate-compressed, so the word is absent from them.
	// A scan that read raw stream bytes would call this document clean.
	doc := scanFixture(t)

	res, err := ScanForTokens(context.Background(), doc, []string{"STREAMSECRET"})
	if err != nil {
		t.Fatalf("ScanForTokens: %v", err)
	}
	found := false
	for _, hit := range res.Hits {
		if hit.Token == "STREAMSECRET" && hit.Where == "stream" {
			found = true
		}
	}
	if !found {
		t.Errorf("the compressed copy was not found: %+v", res)
	}
}

func TestScanForTokens_AbsentTokenIsNotReported(t *testing.T) {
	doc := scanFixture(t)

	res, err := ScanForTokens(context.Background(), doc, []string{"NOTINTHISDOCUMENT"})
	if err != nil {
		t.Fatalf("ScanForTokens: %v", err)
	}
	if len(res.Hits) != 0 {
		t.Errorf("Hits = %+v, want none", res.Hits)
	}
}

func TestScanForTokens_RejectsAnEmptyToken(t *testing.T) {
	// An empty token is contained in every string, so answering would report
	// the whole document as leaking and mean nothing.
	doc := scanFixture(t)

	_, err := ScanForTokens(context.Background(), doc, []string{"LITERALSECRET", ""})
	if !errors.Is(err, ErrScanTokenEmpty) {
		t.Errorf("err = %v, want ErrScanTokenEmpty", err)
	}
}

func TestScanForTokens_NoTokensIsNotAScan(t *testing.T) {
	doc := scanFixture(t)

	res, err := ScanForTokens(context.Background(), doc, nil)
	if err != nil {
		t.Fatalf("ScanForTokens: %v", err)
	}
	if !res.Clean() {
		t.Errorf("result = %+v, want a clean empty result", res)
	}
}

func TestScanForTokens_HitsAreOrdered(t *testing.T) {
	// The sweep ranges a map of xref entries, so without the sort the order
	// would differ run to run and a caller's report would be unstable.
	doc := scanFixture(t)

	res, err := ScanForTokens(context.Background(), doc,
		[]string{"LITERALSECRET", "TOPSECRET", "STREAMSECRET"})
	if err != nil {
		t.Fatalf("ScanForTokens: %v", err)
	}
	for i := 1; i < len(res.Hits); i++ {
		a, b := res.Hits[i-1], res.Hits[i]
		if a.ObjectNum > b.ObjectNum ||
			(a.ObjectNum == b.ObjectNum && a.Token > b.Token) {
			t.Errorf("hits out of order at %d: %+v then %+v", i, a, b)
		}
	}
}
