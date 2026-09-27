package crypto

import (
	"crypto/aes"
	"encoding/binary"
	"fmt"
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

// TestNewEncryptorWritesTheReservedPermissionBits pins that the P entry of
// every /Encrypt dictionary NewEncryptor builds carries the values ISO
// 32000-2, 7.6.4.2, Table 22 fixes for its reserved bits, whatever mask the
// caller passes: bits 1-2 "Reserved. Must be zero (0).", bits 7-8 "Reserved.
// Must be 1." and bits 13-32 "(Security handlers of revision 3 or greater)
// Reserved. Must be 1.". The permission bits are the caller's.
//
// P enters the file key of the RC4 and AES-128 handlers (ISO 32000-1,
// 7.6.3.3, Algorithm 2) and /Perms of the AES-256 one (ISO 32000-2,
// 7.6.4.4.9), so each case also authenticates under its password and, for
// AES-256, decrypts /Perms back to the same P.
func TestNewEncryptorWritesTheReservedPermissionBits(t *testing.T) {
	t.Parallel()

	docID := []byte("0123456789abcdef")
	for _, tc := range []struct {
		name string
		mask int32
		want int32
	}{
		// -1 grants every permission: only bits 1-2 change.
		{name: "every permission", mask: -1, want: -4},
		// 0 denies every permission: bits 7-8 and 13-32 come in, which the
		// NOTE of ISO 32000-1, 7.6.3.2 shows as a negative P.
		{name: "no permission", mask: 0, want: -3904},
		// Printing and copying, the -44 of the ISO 32000-1, 7.6.3.2 NOTE with
		// its reserved low bits set and bits 7-8 cleared.
		{name: "print and copy", mask: (-44 | 0b11) &^ 0xC0, want: -44},
	} {
		for _, alg := range []Algorithm{RC4_40, RC4_128, AES_128, AES_256} {
			t.Run(fmt.Sprintf("%s/algorithm %d", tc.name, alg), func(t *testing.T) {
				t.Parallel()

				cfg := EncryptConfig{UserPassword: "user", OwnerPassword: "owner", Algorithm: alg, Permissions: tc.mask}
				_, encDict, err := NewEncryptor(cfg, docID)
				if err != nil {
					t.Fatalf("NewEncryptor: %v", err)
				}
				p, ok := encDict["P"].(ast.PdfInt)
				if !ok {
					t.Fatalf("/P = %#v, want an integer", encDict["P"])
				}
				if int32(p) != tc.want {
					t.Errorf("/P = %d (%#08x), want %d (%#08x)", p, uint32(p), tc.want, uint32(tc.want))
				}

				trailer := ast.PdfDict{"ID": ast.PdfArray{ast.PdfString(string(docID)), ast.PdfString(string(docID))}}
				h, err := NewHandler(encDict, trailer)
				if err != nil {
					t.Fatalf("NewHandler: %v", err)
				}
				if err := h.Authenticate("user"); err != nil {
					t.Fatalf("the user password does not authenticate: %v", err)
				}
				if got := int32(h.PermissionFlags()); got != tc.want { //nolint:gosec // P is a 32-bit field, read back as written.
					t.Errorf("PermissionFlags() = %d, want %d", got, tc.want)
				}
				if alg != AES_256 {
					return
				}
				perms, ok := encDict["Perms"].(ast.PdfString)
				if !ok || len(perms) != 16 {
					t.Fatalf("/Perms = %#v, want a 16-byte string", encDict["Perms"])
				}
				block, err := aes.NewCipher(h.EncryptionKey)
				if err != nil {
					t.Fatalf("cipher: %v", err)
				}
				dec := make([]byte, 16)
				block.Decrypt(dec, []byte(perms))
				if got := int32(binary.LittleEndian.Uint32(dec[0:4])); got != tc.want { //nolint:gosec // /Perms holds P as 32 bits.
					t.Errorf("/Perms decodes P = %d, want %d", got, tc.want)
				}
			})
		}
	}
}

// TestNewEncryptor_ZeroAlgorithmIsAES256 pins that a config naming no
// algorithm builds the AES-256 handler (V 5, R 6). The zero value was the
// 40-bit RC4 handler, which is weak and for legacy use only.
func TestNewEncryptor_ZeroAlgorithmIsAES256(t *testing.T) {
	t.Parallel()

	cfg := EncryptConfig{UserPassword: "user", OwnerPassword: "owner", Permissions: -4}
	_, encDict, err := NewEncryptor(cfg, []byte("0123456789abcdef"))
	if err != nil {
		t.Fatalf("NewEncryptor: %v", err)
	}
	if v, r := encDict["V"], encDict["R"]; v != ast.PdfInt(5) || r != ast.PdfInt(6) {
		t.Errorf("the zero algorithm builds V %v R %v, want V 5 R 6", v, r)
	}
}
