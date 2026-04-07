package pdf

import (
	"bytes"
	"context"
	"crypto/x509"
	"testing"
)

// mockSigner is a test Signer that produces a deterministic fake signature.
type mockSigner struct {
	signedDigest []byte
}

func (m *mockSigner) Sign(digest []byte) ([]byte, error) {
	m.signedDigest = digest
	// Return a fake PKCS#7 blob (not valid, but exercises the flow).
	return bytes.Repeat([]byte{0xDE, 0xAD}, 128), nil
}

func (m *mockSigner) Certificate() *x509.Certificate {
	return &x509.Certificate{} // empty, sufficient for tests
}

func (m *mockSigner) EstimatedSize() int {
	return 8192
}

func TestWriter_Sign(t *testing.T) {
	data := writerTestPDF()
	ctx := context.Background()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	signer := &mockSigner{}
	w := NewWriter(r)
	w.Sign(ctx, signer)

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	// Verify the signer was called with a non-empty digest.
	if len(signer.signedDigest) == 0 {
		t.Error("signer was not called")
	}
	if len(signer.signedDigest) != 32 {
		t.Errorf("digest length: got %d, want 32 (SHA-256)", len(signer.signedDigest))
	}

	// Re-open and verify the document has a signature field.
	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open signed: %v", err)
	}

	sigs, err := r2.ExtractSignatures(ctx)
	if err != nil {
		t.Fatalf("ExtractSignatures: %v", err)
	}

	if len(sigs) == 0 {
		t.Error("expected at least one signature")
	}
}

func TestWriter_SignAndModify(t *testing.T) {
	data := writerTestPDF()
	ctx := context.Background()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	w := NewWriter(r)
	w.SetMetadata(ctx, Metadata{Title: "Signed Document"})
	w.Sign(ctx, &mockSigner{})

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	meta, err := r2.GetMetadata(ctx)
	if err != nil {
		t.Fatalf("GetMetadata: %v", err)
	}
	if meta["Title"] != "Signed Document" {
		t.Errorf("Title: got %q, want %q", meta["Title"], "Signed Document")
	}

	sigs, err := r2.ExtractSignatures(ctx)
	if err != nil {
		t.Fatalf("ExtractSignatures: %v", err)
	}
	if len(sigs) == 0 {
		t.Error("expected signature")
	}
}

func TestWriter_Sign_ByteRangeValid(t *testing.T) {
	data := writerTestPDF()
	ctx := context.Background()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	w := NewWriter(r)
	w.Sign(ctx, &mockSigner{})

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	// Extract ByteRange from the signature and verify it covers the whole file.
	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open signed: %v", err)
	}

	sigs, err := r2.ExtractSignatures(ctx)
	if err != nil {
		t.Fatalf("ExtractSignatures: %v", err)
	}
	if len(sigs) == 0 {
		t.Fatal("no signatures found")
	}

	br := sigs[0].ByteRange
	// ByteRange should span the entire file: [0, start, end, total-end].
	// br[0]+br[1] should be the start of /Contents, br[2]+br[3] should be total length.
	totalLen := int64(out.Len())
	coveredBytes := br[1] + br[3]
	signatureGap := br[2] - br[1] // the /Contents hex string
	if coveredBytes+signatureGap != totalLen {
		t.Errorf("ByteRange doesn't cover full file: covered=%d gap=%d total=%d", coveredBytes, signatureGap, totalLen)
	}
}
