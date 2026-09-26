package pdf

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"regexp"
	"testing"
)

// signedRangePattern matches the ByteRange litho writes, which pads each value
// to ten digits.
var signedRangePattern = regexp.MustCompile(`/ByteRange \[0 (\d{10}) (\d{10}) (\d{10})\]`)

// signedDigest returns the SHA-256 of the bytes the ByteRange written in data
// covers.
func signedDigest(t *testing.T, data []byte) []byte {
	t.Helper()

	m := signedRangePattern.FindSubmatch(data)
	if m == nil {
		t.Fatal("no ByteRange in the output")
	}
	var start2, len2, len1 int
	if _, err := fmt.Sscanf(string(m[1])+" "+string(m[2])+" "+string(m[3]), "%d %d %d", &len1, &start2, &len2); err != nil {
		t.Fatalf("parsing %s: %v", m[0], err)
	}
	h := sha256.New()
	h.Write(data[:len1])
	h.Write(data[start2 : start2+len2])
	return h.Sum(nil)
}

// TestWriterSignatureDigestCoversTheWrittenByteRange pins that the digest the
// signer signs is the one of the file it ends up in.
//
// The ByteRange array sits in the signature dictionary, inside the bytes the
// signature covers (ISO 32000-1, 12.8.1). The writer used to hash the file while
// the array still held its placeholder values and fill in the real offsets
// afterwards, so every signature it wrote failed verification: poppler's
// pdfsig reported a digest mismatch on a plain one-page document.
func TestWriterSignatureDigestCoversTheWrittenByteRange(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := writerTestPDF()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	signer := &mockSigner{}
	w := NewWriter(r)
	defer w.Close()
	w.Sign(signer)
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	if got := signedDigest(t, out.Bytes()); !bytes.Equal(got, signer.signedDigest) {
		t.Errorf("the signer signed %x, the file's ByteRange hashes to %x", signer.signedDigest, got)
	}
}
