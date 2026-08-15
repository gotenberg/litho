package document

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/crypto"
)

// aesCryptFilterPDF builds an AES-128 document that opens on the empty user
// password. When indirect is set, the /Encrypt dictionary names its /CF crypt
// filter dictionary through an indirect reference instead of holding it
// directly, which ISO 32000-1, 7.3.10 permits for any object.
func aesCryptFilterPDF(t *testing.T, indirect bool) []byte {
	t.Helper()

	docID := bytes.Repeat([]byte{0xCD}, 16)
	enc, encDict, err := crypto.NewEncryptor(crypto.EncryptConfig{
		Algorithm:   crypto.AES_128,
		Permissions: -1852,
	}, docID)
	if err != nil {
		t.Fatalf("NewEncryptor: %v", err)
	}
	cipher, err := enc.EncryptFunc(4, 0)([]byte(aesMarker))
	if err != nil {
		t.Fatalf("EncryptFunc: %v", err)
	}

	var encBody, cryptFilter bytes.Buffer
	encBody.WriteString("<<")
	for _, key := range []ast.PdfName{"Filter", "V", "R", "Length", "P", "O", "U", "StmF", "StrF", "CF"} {
		value, ok := encDict[key]
		if !ok {
			continue
		}
		switch {
		case key == "CF" && indirect:
			fmt.Fprintf(&cryptFilter, "%v", value)
			encBody.WriteString(" /CF 6 0 R")
		case key == "O" || key == "U":
			fmt.Fprintf(&encBody, " %v (%s)", key, escapePdfLiteral([]byte(value.(ast.PdfString))))
		default:
			fmt.Fprintf(&encBody, " %v %v", key, value)
		}
	}
	encBody.WriteString(" >>")

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Author (" + escapePdfLiteral(cipher) + ") >>",
		encBody.String(),
	}
	if indirect {
		objs = append(objs, cryptFilter.String())
	}

	var b bytes.Buffer
	b.WriteString("%PDF-1.6\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	start := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R /Info 4 0 R /Encrypt 5 0 R /ID [<%X> <%X>] >>\nstartxref\n%d\n%%%%EOF\n",
		len(objs)+1, docID, docID, start)
	return b.Bytes()
}

// aesMarker is the plaintext the fixture encrypts, chosen so a wrong cipher
// produces visibly different bytes rather than a subtle difference.
const aesMarker = "AES marker"

// TestIndirectCryptFilterStillDecryptsAsAES pins that an AES document whose
// /CF is written indirectly is decrypted with AES.
//
// [crypto.NewHandler] receives a dictionary and no way to resolve anything, so
// an indirect /CF left it unable to see a crypt filter at all. It fell back to
// the V2 default and ran RC4 over an AESV2 file (ISO 32000-1, Table 25), and
// because /O and /U do not depend on the cipher, authentication still succeeded:
// litho reported an authenticated document and handed back every string and
// stream as ciphertext. A rewrite would then bake that garbage in.
func TestIndirectCryptFilterStillDecryptsAsAES(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	for _, tc := range []struct {
		name     string
		indirect bool
	}{
		{"direct /CF", false},
		{"indirect /CF", true},
	} {
		data := aesCryptFilterPDF(t, tc.indirect)
		doc, err := NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
		if err != nil {
			t.Fatalf("%s: NewDocument: %v", tc.name, err)
		}
		if doc.Crypto == nil {
			t.Fatalf("%s: no crypto handler", tc.name)
		}
		if doc.Crypto.CFM != "AESV2" {
			t.Errorf("%s: CFM = %q, want AESV2 (V2 means the file is being decrypted with RC4)", tc.name, doc.Crypto.CFM)
		}

		info, err := ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 4})
		if err != nil {
			t.Fatalf("%s: resolving /Info: %v", tc.name, err)
		}
		got, ok := info["Author"].(ast.PdfString)
		if !ok {
			t.Fatalf("%s: /Author is %T, want ast.PdfString", tc.name, info["Author"])
		}
		if string(got) != aesMarker {
			t.Errorf("%s: /Author = %q, want %q", tc.name, got, aesMarker)
		}
	}
}
