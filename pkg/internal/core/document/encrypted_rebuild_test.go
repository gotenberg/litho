package document

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/crypto"
)

// encryptedRebuiltPDF builds an RC4-128 document that opens on the empty user
// password and whose startxref points nowhere, so the reader falls back to the
// raw-scan rebuild. Every string is encrypted with the same handler litho will
// derive, so a resolution that skips decryption returns visible ciphertext.
func encryptedRebuiltPDF(t *testing.T, author string) []byte {
	t.Helper()

	docID := bytes.Repeat([]byte{0xAB}, 16)
	enc, encDict, err := crypto.NewEncryptor(crypto.EncryptConfig{
		Algorithm:   crypto.RC4_128,
		Permissions: -1852,
	}, docID)
	if err != nil {
		t.Fatalf("NewEncryptor: %v", err)
	}

	// Object 4 holds the /Author string, encrypted under its own object key
	// (ISO 32000-1, Algorithm 1).
	cipher, err := enc.EncryptFunc(4, 0)([]byte(author))
	if err != nil {
		t.Fatalf("EncryptFunc: %v", err)
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	obj := func(num int, body string) {
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", num, body)
	}
	obj(1, "<< /Type /Catalog /Pages 2 0 R >>")
	obj(2, "<< /Type /Pages /Count 1 /Kids [3 0 R] >>")
	obj(3, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>")
	obj(4, "<< /Author ("+escapePdfLiteral(cipher)+") >>")

	var encBody bytes.Buffer
	encBody.WriteString("<<")
	for _, k := range []ast.PdfName{"Filter", "V", "R", "Length", "P", "O", "U"} {
		v, ok := encDict[k]
		if !ok {
			continue
		}
		// k is an ast.PdfName, which already renders its leading slash.
		if s, isStr := v.(ast.PdfString); isStr {
			fmt.Fprintf(&encBody, " %v (%s)", k, escapePdfLiteral([]byte(s)))
			continue
		}
		fmt.Fprintf(&encBody, " %v %v", k, v)
	}
	encBody.WriteString(" >>")
	obj(5, encBody.String())

	// A startxref pointing at offset 9 lands inside the header, so the chain
	// parse fails and the reader rebuilds by scanning for object headers. That
	// rebuild resolves every object it finds, which is what used to cache them
	// as ciphertext before the crypto handler existed.
	fmt.Fprintf(&buf, "trailer\n<< /Size 6 /Root 1 0 R /Info 4 0 R /Encrypt 5 0 R /ID [<%X> <%X>] >>\nstartxref\n9\n%%%%EOF\n",
		docID, docID)
	return buf.Bytes()
}

// escapePdfLiteral renders bytes as the body of a PDF literal string, escaping
// only what ISO 32000-1, 7.3.4.2 requires.
func escapePdfLiteral(b []byte) string {
	var out bytes.Buffer
	for _, c := range b {
		switch c {
		case '(', ')', '\\':
			out.WriteByte('\\')
			out.WriteByte(c)
		case '\r':
			out.WriteString(`\r`)
		default:
			out.WriteByte(c)
		}
	}
	return out.String()
}

// TestEncryptedDocumentRecoveredByRebuildStillDecrypts pins that objects parsed
// before the crypto handler exists are not served from the cache afterwards.
//
// A raw-scan rebuild resolves every object it recovered, looking for object
// streams, and that runs before the /Encrypt dictionary is read. The resolutions
// cached ciphertext, and nothing downstream could tell: a later resolve was a
// cache hit that returned the encrypted bytes as the object's value.
// [Document.Authenticate] already cleared the caches for this reason, but a
// document that opens on the empty user password authenticates inside
// [crypto.NewHandler] and never calls it.
//
// # Corpus evidence
//
//   - pdfjs issue5909_original.pdf: its final startxref points into a
//     superseded revision, and all 43 objects were cached as ciphertext.
//     `litho metadata` printed the encrypted bytes as the /Author and
//     `litho text` found nothing in a document poppler reads in full.
func TestEncryptedDocumentRecoveredByRebuildStillDecrypts(t *testing.T) {
	t.Parallel()

	const author = "Bloomberg nX Reporting platform"
	data := encryptedRebuiltPDF(t, author)

	ctx := context.Background()
	doc, err := NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	if !doc.Rebuilt {
		t.Fatal("fixture is wrong: the reader must fall back to the raw-scan rebuild")
	}
	if !doc.IsEncrypted || !doc.Authenticated {
		t.Fatalf("fixture is wrong: encrypted=%v authenticated=%v, want both true", doc.IsEncrypted, doc.Authenticated)
	}

	info, err := ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 4})
	if err != nil {
		t.Fatalf("resolving /Info: %v", err)
	}
	got, ok := info["Author"].(ast.PdfString)
	if !ok {
		t.Fatalf("/Author is %T, want ast.PdfString", info["Author"])
	}
	if string(got) != author {
		t.Errorf("/Author = %q, want %q (the object was served from a cache filled before decryption)", got, author)
	}
}
