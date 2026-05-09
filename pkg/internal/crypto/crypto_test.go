package crypto

import (
	"crypto/md5" //nolint:gosec
	"crypto/rc4" //nolint:gosec
	"errors"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

func TestGetInt_Present(t *testing.T) {
	d := ast.PdfDict{"V": ast.PdfInt(4)}
	if got := dictInt(d, "V", 0); got != 4 {
		t.Errorf("dictInt = %d, want 4", got)
	}
}

func TestGetInt_Missing(t *testing.T) {
	d := ast.PdfDict{}
	if got := dictInt(d, "V", 99); got != 99 {
		t.Errorf("dictInt missing = %d, want 99", got)
	}
}

func TestGetString_Present(t *testing.T) {
	d := ast.PdfDict{"O": ast.PdfString("owner")}
	got := dictString(d, "O")
	if string(got) != "owner" {
		t.Errorf("dictString = %q, want %q", got, "owner")
	}
}

func TestGetString_Missing(t *testing.T) {
	d := ast.PdfDict{}
	if got := dictString(d, "O"); got != nil {
		t.Errorf("dictString missing = %v, want nil", got)
	}
}

// buildV1EncryptDict builds a minimal V=1 encryption dict and trailer
// for testing authentication with a blank password.
func buildV1EncryptDict(password string) (ast.PdfDict, ast.PdfDict) {
	// Build a deterministic encryption setup for V=1, Length=40.
	id := []byte("0123456789abcdef")
	pVal := -4 // Some permission bits

	// Pad the password.
	pwBytes := []byte(password)
	padded := make([]byte, 32)
	if len(pwBytes) >= 32 {
		copy(padded, pwBytes[:32])
	} else {
		copy(padded, pwBytes)
		copy(padded[len(pwBytes):], paddingString[:32-len(pwBytes)])
	}

	// Compute O value: encrypt padded password with MD5 of padded password.
	md := md5.New() //nolint:gosec
	md.Write(padded)
	ownerKey := md.Sum(nil)
	oVal := make([]byte, 32)
	copy(oVal, padded)
	c, _ := rc4.NewCipher(ownerKey[:5]) //nolint:gosec
	c.XORKeyStream(oVal, oVal)

	// Compute encryption key (Algorithm 3.2).
	mdC := md5.New() //nolint:gosec
	mdC.Write(padded)
	mdC.Write(oVal)
	pBytes := []byte{byte(pVal), byte(pVal >> 8), byte(pVal >> 16), byte(pVal >> 24)} //nolint:gosec
	mdC.Write(pBytes)
	mdC.Write(id)
	key := mdC.Sum(nil)[:5] // Length/8 = 40/8 = 5

	// Compute U value: RC4(paddingString, key).
	uVal := make([]byte, 32)
	c2, _ := rc4.NewCipher(key) //nolint:gosec
	c2.XORKeyStream(uVal, paddingString)

	encDict := ast.PdfDict{
		"V":      ast.PdfInt(1),
		"Length": ast.PdfInt(40),
		"P":      ast.PdfInt(pVal),
		"O":      ast.PdfString(string(oVal)),
		"U":      ast.PdfString(string(uVal)),
	}

	trailer := ast.PdfDict{
		"ID": ast.PdfArray{ast.PdfString(string(id))},
	}

	return encDict, trailer
}

func TestNewHandler_BlankPassword(t *testing.T) {
	encDict, trailer := buildV1EncryptDict("")

	h, err := NewHandler(encDict, trailer)
	if err != nil {
		t.Fatal(err)
	}

	if !h.Authenticated {
		t.Error("expected Authenticated=true for blank password")
	}
	if h.V != 1 {
		t.Errorf("V = %d, want 1", h.V)
	}
	if h.Length != 40 {
		t.Errorf("Length = %d, want 40", h.Length)
	}
}

// TestAuthenticate_V2R3_BlankUserPassword guards against the gating bug
// that keyed Algorithm 3.4/3.5 on /V instead of /R. Govdocs PDFs commonly
// declare V=2 R=3 (RC4-128 + 50-pass MD5 + 16-byte U match), so the V=2
// path must still run the R≥3 algorithms.
func TestAuthenticate_V2R3_BlankUserPassword(t *testing.T) {
	cfg := EncryptConfig{
		UserPassword:  "",
		OwnerPassword: "ownersecret",
		Algorithm:     RC4_128,
		Permissions:   -1324,
	}
	docID := []byte("0123456789abcdef")
	_, encDict, err := NewEncryptor(cfg, docID)
	if err != nil {
		t.Fatalf("NewEncryptor: %v", err)
	}
	if v, _ := encDict["V"].(ast.PdfInt); int(v) != 2 {
		t.Fatalf("V = %v, want 2", v)
	}
	if r, _ := encDict["R"].(ast.PdfInt); int(r) != 3 {
		t.Fatalf("R = %v, want 3", r)
	}

	trailer := ast.PdfDict{
		"ID": ast.PdfArray{ast.PdfString(string(docID)), ast.PdfString(string(docID))},
	}
	h, err := NewHandler(encDict, trailer)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	if !h.Authenticated {
		t.Fatal("blank user password did not authenticate against V=2 R=3 dict")
	}
	if h.R != 3 {
		t.Errorf("Handler.R = %d, want 3", h.R)
	}
}

func TestAuthenticate_InvalidPassword(t *testing.T) {
	encDict, trailer := buildV1EncryptDict("")

	h, err := NewHandler(encDict, trailer)
	if err != nil {
		t.Fatal(err)
	}

	// Re-authenticate with wrong password.
	err = h.Authenticate("wrong-password-123")
	if !errors.Is(err, ErrInvalidPassword) {
		t.Errorf("expected ErrInvalidPassword, got %v", err)
	}
}

func TestObjKey(t *testing.T) {
	h := &Handler{
		EncryptionKey: []byte{0x01, 0x02, 0x03, 0x04, 0x05},
		V:             1,
		Length:        40,
	}

	key := h.ObjKey(10, 0)
	if len(key) == 0 {
		t.Error("ObjKey returned empty key")
	}
	// Key length should be min(encKey+5, 16) = min(10, 16) = 10
	if len(key) != 10 {
		t.Errorf("key length = %d, want 10", len(key))
	}
}

func TestObjKey_V4_SaltSuffix(t *testing.T) {
	h := &Handler{
		EncryptionKey: []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10},
		V:             4,
		Length:        128,
	}

	key := h.ObjKey(1, 0)
	if len(key) != 16 {
		t.Errorf("V=4 key length = %d, want 16", len(key))
	}
}

func TestDecryptFunc_RC4(t *testing.T) {
	h := &Handler{
		EncryptionKey: []byte{0x01, 0x02, 0x03, 0x04, 0x05},
		V:             1,
		Length:        40,
	}

	decrypt := h.DecryptFunc(1, 0)

	// Encrypt some data with the same key, then decrypt.
	key := h.ObjKey(1, 0)
	plaintext := []byte("Hello, PDF!")
	c, _ := rc4.NewCipher(key) //nolint:gosec
	encrypted := make([]byte, len(plaintext))
	c.XORKeyStream(encrypted, plaintext)

	decrypted, err := decrypt(encrypted)
	if err != nil {
		t.Fatal(err)
	}
	if string(decrypted) != "Hello, PDF!" {
		t.Errorf("decrypted = %q, want %q", decrypted, "Hello, PDF!")
	}
}

func TestDecryptFunc_EmptyData(t *testing.T) {
	h := &Handler{
		EncryptionKey: []byte{0x01, 0x02, 0x03, 0x04, 0x05},
		V:             1,
		Length:        40,
	}

	decrypt := h.DecryptFunc(1, 0)
	result, err := decrypt([]byte{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty result for empty input, got %d bytes", len(result))
	}
}

func TestNewHandler_V4_CFM(t *testing.T) {
	encDict := ast.PdfDict{
		"V":      ast.PdfInt(4),
		"Length": ast.PdfInt(128),
		"P":      ast.PdfInt(0),
		"O":      ast.PdfString(""),
		"U":      ast.PdfString(""),
		"StmF":   ast.PdfName("StdCF"),
		"CF": ast.PdfDict{
			"StdCF": ast.PdfDict{
				"CFM": ast.PdfName("AESV2"),
			},
		},
	}
	trailer := ast.PdfDict{
		"ID": ast.PdfArray{ast.PdfString("testid1234567890")},
	}

	h, err := NewHandler(encDict, trailer)
	if err != nil {
		t.Fatal(err)
	}
	if h.CFM != "AESV2" {
		t.Errorf("CFM = %q, want %q", h.CFM, "AESV2")
	}
}
