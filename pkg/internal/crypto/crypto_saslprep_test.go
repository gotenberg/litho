package crypto

import (
	"errors"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// saslprepTrailer is the trailer the SASLprep tests open their dictionaries
// with.
func saslprepTrailer(docID []byte) ast.PdfDict {
	return ast.PdfDict{"ID": ast.PdfArray{ast.PdfString(string(docID)), ast.PdfString(string(docID))}}
}

// TestAES256PasswordIsSASLprepped pins ISO 32000-2, 7.6.4.3.3: the password
// is prepared with SASLprep before it is encoded and hashed, so every
// spelling that prepares to the same string opens the document, and the
// owner password is prepared too. The RFC 4013, 3 examples supply the
// spellings: U+2168 ROMAN NUMERAL NINE and I<U+00AD>X both prepare to IX.
func TestAES256PasswordIsSASLprepped(t *testing.T) {
	t.Parallel()

	docID := []byte("0123456789abcdef")
	cfg := EncryptConfig{UserPassword: "Ⅸ", OwnerPassword: "ｏｗｎ", Algorithm: AES_256, Permissions: -4}
	_, encDict, err := NewEncryptor(cfg, docID)
	if err != nil {
		t.Fatalf("NewEncryptor: %v", err)
	}
	for _, pw := range []string{"IX", "Ⅸ", "I­X", "own", "ｏｗｎ", "o‍wn"} {
		h, err := NewHandler(encDict, saslprepTrailer(docID))
		if err != nil {
			t.Fatalf("NewHandler: %v", err)
		}
		if err := h.Authenticate(pw); err != nil {
			t.Errorf("Authenticate(%q): %v", pw, err)
		}
	}
	h, err := NewHandler(encDict, saslprepTrailer(docID))
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	if err := h.Authenticate("ix"); !errors.Is(err, ErrInvalidPassword) {
		t.Errorf("Authenticate(\"ix\") = %v, want ErrInvalidPassword: SASLprep preserves case", err)
	}
}

// TestAES256PasswordFromProducerWithoutSASLprep pins the fallback to the raw
// UTF-8 bytes: qpdf and PDFium hash the password as given, so a document
// they encrypt with a password SASLprep changes still opens with that
// password.
func TestAES256PasswordFromProducerWithoutSASLprep(t *testing.T) {
	t.Parallel()

	docID := []byte("0123456789abcdef")
	for _, pw := range []string{"Ⅸ", "a­b", "ا1", "tab\there"} {
		_, encDict, err := newEncryptorV5([]byte(pw), []byte(pw+"-owner"), -4, 32, docID)
		if err != nil {
			t.Fatalf("newEncryptorV5: %v", err)
		}
		for _, try := range []string{pw, pw + "-owner"} {
			h, err := NewHandler(encDict, saslprepTrailer(docID))
			if err != nil {
				t.Fatalf("NewHandler: %v", err)
			}
			if err := h.Authenticate(try); err != nil {
				t.Errorf("Authenticate(%q) on a document hashed without SASLprep: %v", try, err)
			}
		}
	}
}

// TestAES256EncryptRefusesWhatSASLprepRejects pins that a new document is
// never keyed to a password SASLprep rejects: a reader that prepares the
// password, as ISO 32000-2, 7.6.4.3.3 requires, could never open it.
func TestAES256EncryptRefusesWhatSASLprepRejects(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct{ user, owner string }{
		{"tab\there", ""},
		{"ok", "ا1"},
		{"pw\U0001F600", "ok"},
		{"bad\xff", "ok"},
	} {
		_, _, err := NewEncryptor(EncryptConfig{UserPassword: tt.user, OwnerPassword: tt.owner, Algorithm: AES_256}, []byte("0123456789abcdef"))
		if !errors.Is(err, ErrPasswordRejected) {
			t.Errorf("NewEncryptor(%q, %q) = %v, want ErrPasswordRejected", tt.user, tt.owner, err)
		}
	}
}

// TestLegacyPasswordIsNotSASLprepped pins that SASLprep stays an AES-256
// rule: ISO 32000-1, 7.6.3.3 Algorithm 2 pads the password bytes as given.
func TestLegacyPasswordIsNotSASLprepped(t *testing.T) {
	t.Parallel()

	docID := []byte("0123456789abcdef")
	_, encDict, err := NewEncryptor(EncryptConfig{UserPassword: "tab\there", Algorithm: AES_128}, docID)
	if err != nil {
		t.Fatalf("NewEncryptor: %v", err)
	}
	h, err := NewHandler(encDict, saslprepTrailer(docID))
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	if err := h.Authenticate("tab\there"); err != nil {
		t.Errorf("Authenticate: %v", err)
	}
}
