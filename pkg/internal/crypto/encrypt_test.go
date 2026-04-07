package crypto

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

func TestNewEncryptor_AES256_RoundTrip(t *testing.T) {
	docID := []byte("0123456789abcdef")
	cfg := EncryptConfig{
		UserPassword:  "user123",
		OwnerPassword: "owner456",
		Algorithm:     AES_256,
		Permissions:   -4,
	}

	enc, encDict, err := NewEncryptor(cfg, docID)
	if err != nil {
		t.Fatalf("NewEncryptor: %v", err)
	}

	// Verify we can create a Handler from the encrypt dict and authenticate.
	trailer := ast.PdfDict{
		"ID": ast.PdfArray{ast.PdfString(string(docID)), ast.PdfString(string(docID))},
	}
	h, err := NewHandler(encDict, trailer)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	// Try user password.
	if err := h.Authenticate("user123"); err != nil {
		t.Fatalf("Authenticate user: %v", err)
	}

	// Verify the encryption key matches.
	if len(h.EncryptionKey) != 32 {
		t.Errorf("key length: got %d, want 32", len(h.EncryptionKey))
	}

	// Test encrypt → decrypt round-trip on object data.
	plaintext := []byte("Hello, encrypted world!")
	encryptFn := enc.GetEncryptFunc(1, 0)
	ciphertext, err := encryptFn(plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	decryptFn := h.GetDecryptFunc(1, 0)
	decrypted, err := decryptFn(ciphertext)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}

	if string(decrypted) != string(plaintext) {
		t.Errorf("round-trip failed: got %q, want %q", decrypted, plaintext)
	}
}

func TestNewEncryptor_AES256_OwnerPassword(t *testing.T) {
	docID := []byte("0123456789abcdef")
	cfg := EncryptConfig{
		UserPassword:  "user",
		OwnerPassword: "owner",
		Algorithm:     AES_256,
		Permissions:   -4,
	}

	_, encDict, err := NewEncryptor(cfg, docID)
	if err != nil {
		t.Fatalf("NewEncryptor: %v", err)
	}

	trailer := ast.PdfDict{
		"ID": ast.PdfArray{ast.PdfString(string(docID)), ast.PdfString(string(docID))},
	}
	h, err := NewHandler(encDict, trailer)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	if err := h.Authenticate("owner"); err != nil {
		t.Fatalf("Authenticate owner: %v", err)
	}
}

func TestNewEncryptor_AES128_RoundTrip(t *testing.T) {
	docID := []byte("0123456789abcdef")
	cfg := EncryptConfig{
		UserPassword: "test",
		Algorithm:    AES_128,
		Permissions:  -4,
	}

	enc, encDict, err := NewEncryptor(cfg, docID)
	if err != nil {
		t.Fatalf("NewEncryptor: %v", err)
	}

	trailer := ast.PdfDict{
		"ID": ast.PdfArray{ast.PdfString(string(docID)), ast.PdfString(string(docID))},
	}
	h, err := NewHandler(encDict, trailer)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	if err := h.Authenticate("test"); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	plaintext := []byte("Test data for AES-128")
	encryptFn := enc.GetEncryptFunc(1, 0)
	ciphertext, err := encryptFn(plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	decryptFn := h.GetDecryptFunc(1, 0)
	decrypted, err := decryptFn(ciphertext)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}

	if string(decrypted) != string(plaintext) {
		t.Errorf("round-trip: got %q, want %q", decrypted, plaintext)
	}
}

func TestNewEncryptor_RC4_128_RoundTrip(t *testing.T) {
	docID := []byte("0123456789abcdef")
	cfg := EncryptConfig{
		UserPassword: "pw",
		Algorithm:    RC4_128,
		Permissions:  -4,
	}

	enc, encDict, err := NewEncryptor(cfg, docID)
	if err != nil {
		t.Fatalf("NewEncryptor: %v", err)
	}

	trailer := ast.PdfDict{
		"ID": ast.PdfArray{ast.PdfString(string(docID)), ast.PdfString(string(docID))},
	}
	h, err := NewHandler(encDict, trailer)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	if err := h.Authenticate("pw"); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	plaintext := []byte("RC4 test data")
	encryptFn := enc.GetEncryptFunc(1, 0)
	ciphertext, err := encryptFn(plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	decryptFn := h.GetDecryptFunc(1, 0)
	decrypted, err := decryptFn(ciphertext)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}

	if string(decrypted) != string(plaintext) {
		t.Errorf("round-trip: got %q, want %q", decrypted, plaintext)
	}
}
