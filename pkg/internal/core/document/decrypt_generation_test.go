package document

import (
	"bytes"
	"context"
	"crypto/md5" //nolint:gosec // ISO 32000-1, 7.6.3.3 defines the standard security handler key in terms of MD5.
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// passwordPad is the 32-byte padding string of ISO 32000-1, Table 21.
var passwordPad = []byte{
	0x28, 0xBF, 0x4E, 0x5E, 0x4E, 0x75, 0x8A, 0x41, 0x64, 0x00, 0x4E, 0x56,
	0xFF, 0xFA, 0x01, 0x08, 0x2E, 0x2E, 0x00, 0xB6, 0xD0, 0x68, 0x3E, 0x80,
	0x2F, 0x0C, 0xA9, 0xFE, 0x64, 0x53, 0x69, 0x7A,
}

func rc4Cipher(key, data []byte) []byte {
	var s [256]byte
	for i := range s {
		s[i] = byte(i)
	}
	j := 0
	for i := range 256 {
		j = (j + int(s[i]) + int(key[i%len(key)])) % 256
		s[i], s[j] = s[j], s[i]
	}
	out := make([]byte, len(data))
	i, j := 0, 0
	for k, c := range data {
		i = (i + 1) % 256
		j = (j + int(s[i])) % 256
		s[i], s[j] = s[j], s[i]
		out[k] = c ^ s[(int(s[i])+int(s[j]))%256]
	}
	return out
}

// rc4ObjectKey derives the per-object RC4 key of ISO 32000-1, 7.6.2: the file
// key followed by the low three bytes of the object number and the low two of
// the generation.
func rc4ObjectKey(fileKey []byte, objNum, genNum int) []byte {
	h := md5.New() //nolint:gosec // See the import note.
	h.Write(fileKey)
	h.Write([]byte{byte(objNum), byte(objNum >> 8), byte(objNum >> 16)})
	h.Write([]byte{byte(genNum), byte(genNum >> 8)})
	sum := h.Sum(nil)
	n := min(len(fileKey)+5, 16)
	return sum[:n]
}

// TestDecryptionUsesTheObjectsGeneration pins that the per-object cipher keys on
// the generation the cross-reference gives an object, not the one a caller put
// in the reference.
//
// ISO 32000-1, 7.6.2 mixes object number and generation into the key, while
// litho resolves by object number alone (see [Document.ObjectGeneration]). A
// caller building ast.PdfIndirectRef{ObjectNum: n} with no generation still
// reaches the object, and before this it decrypted with the wrong key and cached
// the result for everyone after it. pdfjs issue16464.pdf carries eleven
// generation-1 objects: the compliance cache-warming pass resolved them at
// generation 0 and `remediate` wrote four of page 1's five content streams out
// as noise.
func TestDecryptionUsesTheObjectsGeneration(t *testing.T) {
	t.Parallel()

	const (
		streamNum  = 7
		streamGen  = 1
		plaintext  = " q "
		docIDBytes = "0123456789abcdef"
	)

	// A genuine /V 1, /R 2 setup with empty owner and user passwords, so the
	// handler litho builds authenticates and derives the same file key this
	// test encrypts with. ISO 32000-1, Algorithm 3 makes /O the padded user
	// password enciphered under a key from the padded owner password, and
	// Algorithm 4 makes /U the padding enciphered under the file key.
	ownerDigest := md5.Sum(passwordPad) //nolint:gosec // See the import note.
	owner := rc4Cipher(ownerDigest[:5], passwordPad)

	h := md5.New() //nolint:gosec // See the import note.
	h.Write(passwordPad)
	h.Write(owner)
	h.Write([]byte{0xFC, 0xFF, 0xFF, 0xFF}) // /P -4, little endian.
	h.Write([]byte(docIDBytes))
	fileKey := h.Sum(nil)[:5]
	user := rc4Cipher(fileKey, passwordPad)

	cipher := rc4Cipher(rc4ObjectKey(fileKey, streamNum, streamGen), []byte(plaintext))
	if bytes.Equal(cipher, []byte(plaintext)) {
		t.Fatal("fixture is not actually encrypted")
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	offs := map[int]int{}
	put := func(num, gen int, body string) {
		offs[num] = buf.Len()
		fmt.Fprintf(&buf, "%d %d obj\n%s\nendobj\n", num, gen, body)
	}
	put(1, 0, "<< /Type /Catalog /Pages 2 0 R >>")
	put(2, 0, "<< /Type /Pages /Count 1 /Kids [3 0 R] >>")
	put(3, 0, fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 50] /Contents %d %d R >>",
		streamNum, streamGen))
	put(4, 0, fmt.Sprintf("<< /Filter /Standard /V 1 /R 2 /P -4 /O <%x> /U <%x> >>", owner, user))
	offs[streamNum] = buf.Len()
	fmt.Fprintf(&buf, "%d %d obj\n<< /Length %d >>\nstream\n%s\nendstream\nendobj\n",
		streamNum, streamGen, len(cipher), cipher)

	start := buf.Len()
	buf.WriteString("xref\n0 1\n0000000000 65535 f \n")
	for _, n := range []int{1, 2, 3, 4} {
		fmt.Fprintf(&buf, "%d 1\n%010d 00000 n \n", n, offs[n])
	}
	// The generation the file gives the stream, and the only place litho can
	// learn it when a caller omits it from the reference.
	fmt.Fprintf(&buf, "%d 1\n%010d %05d n \n", streamNum, offs[streamNum], streamGen)
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R /Encrypt 4 0 R /ID [<%x> <%x>] >>\nstartxref\n%d\n%%%%EOF\n",
		streamNum+1, []byte(docIDBytes), []byte(docIDBytes), start)

	data := buf.Bytes()
	ctx := context.Background()
	doc, err := NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	if doc.Crypto == nil {
		t.Skip("the fixture's security handler was not accepted, so nothing is decrypted")
	}

	// Resolve the way the compliance cache-warming pass does: by object number,
	// with no generation in the reference.
	obj, err := ResolveObject(ctx, doc, ast.PdfIndirectRef{ObjectNum: streamNum})
	if err != nil {
		t.Fatalf("resolving object %d: %v", streamNum, err)
	}
	stream, ok := obj.(ast.PdfStream)
	if !ok {
		t.Fatalf("object %d resolved to %T, want a stream", streamNum, obj)
	}
	got, err := stream.Bytes(ctx)
	if err != nil {
		t.Fatalf("reading stream: %v", err)
	}
	if string(got) != plaintext {
		t.Errorf("decrypted to %q, want %q: the cipher used the reference's generation"+
			" instead of the one the cross-reference gives the object", got, plaintext)
	}
}
