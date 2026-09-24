package crypto

import (
	"crypto/aes"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestNewEncryptor_AES256_Perms pins that the R6 /Encrypt dictionary carries a
// valid /Perms entry (ISO 32000-2 §7.6.4.4). Without it, strict readers
// (Acrobat, pikepdf) reject the file even though a lenient reader decrypts.
func TestNewEncryptor_AES256_Perms(t *testing.T) {
	docID := []byte("0123456789abcdef")
	cfg := EncryptConfig{UserPassword: "user123", OwnerPassword: "owner456", Algorithm: AES_256, Permissions: -4}

	_, encDict, err := NewEncryptor(cfg, docID)
	if err != nil {
		t.Fatalf("NewEncryptor: %v", err)
	}
	permsObj, ok := encDict["Perms"]
	if !ok {
		t.Fatal("/Encrypt dict missing /Perms (required for R6)")
	}
	perms, ok := permsObj.(ast.PdfString)
	if !ok || len(perms) != 16 {
		t.Fatalf("/Perms: got %T len %d, want a 16-byte string", permsObj, len(perms))
	}

	// Decrypt /Perms with the file key (AES-256 ECB) and verify the P field and
	// the "adb" magic that a validator checks.
	trailer := ast.PdfDict{"ID": ast.PdfArray{ast.PdfString(string(docID)), ast.PdfString(string(docID))}}
	h, err := NewHandler(encDict, trailer)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	if err := h.Authenticate("user123"); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	block, err := aes.NewCipher(h.EncryptionKey)
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	dec := make([]byte, 16)
	block.Decrypt(dec, []byte(perms))
	if string(dec[9:12]) != "adb" {
		t.Errorf("/Perms bytes 9-11 = %q, want \"adb\"", dec[9:12])
	}
	p := int32(uint32(dec[0]) | uint32(dec[1])<<8 | uint32(dec[2])<<16 | uint32(dec[3])<<24)
	if p != -4 {
		t.Errorf("/Perms decoded P = %d, want -4", p)
	}
}

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

	// Verify that a Handler builds from the encrypt dict and authenticates.
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
	encryptFn := enc.EncryptFunc(1, 0)
	ciphertext, err := encryptFn(plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	decryptFn := h.DecryptFunc(1, 0)
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
	encryptFn := enc.EncryptFunc(1, 0)
	ciphertext, err := encryptFn(plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	decryptFn := h.DecryptFunc(1, 0)
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
	encryptFn := enc.EncryptFunc(1, 0)
	ciphertext, err := encryptFn(plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	decryptFn := h.DecryptFunc(1, 0)
	decrypted, err := decryptFn(ciphertext)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}

	if string(decrypted) != string(plaintext) {
		t.Errorf("round-trip: got %q, want %q", decrypted, plaintext)
	}
}
