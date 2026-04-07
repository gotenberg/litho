package pdf

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/crypto"
)

func TestWriter_Encrypt_AES256(t *testing.T) {
	data := writerTestPDF()
	ctx := context.Background()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	w := NewWriter(r)
	w.Encrypt(crypto.EncryptConfig{
		UserPassword:  "user123",
		OwnerPassword: "owner456",
		Algorithm:     crypto.AES_256,
		Permissions:   -4, // allow printing
	})

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	// Re-open without password should fail.
	_, err = Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err == nil {
		t.Error("expected error opening encrypted PDF without password")
	}

	// Re-open with correct user password.
	r3, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()), WithPassword("user123"))
	if err != nil {
		t.Fatalf("Open with user password: %v", err)
	}

	meta, err := r3.GetMetadata(ctx)
	if err != nil {
		t.Fatalf("GetMetadata: %v", err)
	}
	if meta["Title"] != "Original Title" {
		t.Errorf("Title: got %q, want %q", meta["Title"], "Original Title")
	}

	// Re-open with correct owner password.
	r4, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()), WithPassword("owner456"))
	if err != nil {
		t.Fatalf("Open with owner password: %v", err)
	}

	meta2, err := r4.GetMetadata(ctx)
	if err != nil {
		t.Fatalf("GetMetadata: %v", err)
	}
	if meta2["Title"] != "Original Title" {
		t.Errorf("Title: got %q, want %q", meta2["Title"], "Original Title")
	}
}

func TestWriter_Encrypt_AES128(t *testing.T) {
	data := writerTestPDF()
	ctx := context.Background()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	w := NewWriter(r)
	w.Encrypt(crypto.EncryptConfig{
		UserPassword:  "test",
		OwnerPassword: "test",
		Algorithm:     crypto.AES_128,
		Permissions:   -4,
	})

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	// Re-open with password.
	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()), WithPassword("test"))
	if err != nil {
		t.Fatalf("Open encrypted: %v", err)
	}

	meta, err := r2.GetMetadata(ctx)
	if err != nil {
		t.Fatalf("GetMetadata: %v", err)
	}
	if meta["Title"] != "Original Title" {
		t.Errorf("Title: got %q, want %q", meta["Title"], "Original Title")
	}
}

func TestWriter_Decrypt(t *testing.T) {
	data := writerTestPDF()
	ctx := context.Background()

	// First, encrypt the document.
	r, _ := Open(ctx, bytes.NewReader(data), int64(len(data)))
	w := NewWriter(r)
	w.Encrypt(crypto.EncryptConfig{
		UserPassword: "secret",
		Algorithm:    crypto.AES_256,
		Permissions:  -4,
	})
	var encrypted bytes.Buffer
	if err := w.WriteTo(ctx, &encrypted); err != nil {
		t.Fatalf("encrypt WriteTo: %v", err)
	}

	// Now decrypt it.
	r2, err := Open(ctx, bytes.NewReader(encrypted.Bytes()), int64(encrypted.Len()), WithPassword("secret"))
	if err != nil {
		t.Fatalf("Open encrypted: %v", err)
	}

	w2 := NewWriter(r2)
	w2.Decrypt()

	var decrypted bytes.Buffer
	if err := w2.WriteTo(ctx, &decrypted); err != nil {
		t.Fatalf("decrypt WriteTo: %v", err)
	}

	// Re-open without password — should work now.
	r3, err := Open(ctx, bytes.NewReader(decrypted.Bytes()), int64(decrypted.Len()))
	if err != nil {
		t.Fatalf("Open decrypted: %v", err)
	}

	if r3.doc.IsEncrypted {
		t.Error("expected document to not be encrypted after decrypt")
	}

	meta, err := r3.GetMetadata(ctx)
	if err != nil {
		t.Fatalf("GetMetadata: %v", err)
	}
	if meta["Title"] != "Original Title" {
		t.Errorf("Title: got %q, want %q", meta["Title"], "Original Title")
	}
}

func TestWriter_EncryptAndModify(t *testing.T) {
	data := writerTestPDF()
	ctx := context.Background()

	r, _ := Open(ctx, bytes.NewReader(data), int64(len(data)))
	w := NewWriter(r)
	w.SetMetadata(ctx, Metadata{Title: "Encrypted Doc"})
	w.Encrypt(crypto.EncryptConfig{
		UserPassword: "pw",
		Algorithm:    crypto.AES_256,
		Permissions:  -4,
	})

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()), WithPassword("pw"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	meta, err := r2.GetMetadata(ctx)
	if err != nil {
		t.Fatalf("GetMetadata: %v", err)
	}
	if meta["Title"] != "Encrypted Doc" {
		t.Errorf("Title: got %q, want %q", meta["Title"], "Encrypted Doc")
	}
}
