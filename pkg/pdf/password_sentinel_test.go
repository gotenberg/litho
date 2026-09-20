package pdf

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

// encryptedWithUserPassword returns a three-page document encrypted with a real
// user password, which is the only shape that can exercise the rejection path.
func encryptedWithUserPassword(t *testing.T, userPw, ownerPw string) []byte {
	t.Helper()

	ctx := context.Background()
	data := threePagePDF()

	src, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer src.Close()

	w := NewWriter(src)
	w.Encrypt(EncryptConfig{
		UserPassword:  userPw,
		OwnerPassword: ownerPw,
		Algorithm:     EncryptAES128,
		Permissions:   -1,
	})

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	return out.Bytes()
}

// TestOpen_WrongPasswordReturnsTheSentinel pins the translation at the Reader
// boundary: the internal crypto.ErrInvalidPassword becomes the public
// [ErrInvalidPassword], so a caller can match it with errors.Is.
//
// Nothing asserted this before. The package's encryption tests all use an EMPTY
// user password, which needs no password to open, so every one of them takes the
// success path and none reaches the rejection branch.
//
// Both directions are checked deliberately. Asserting only the rejection would
// also pass if Open had stopped accepting the correct password, and asserting
// only the success would miss the sentinel entirely.
func TestOpen_WrongPasswordReturnsTheSentinel(t *testing.T) {
	t.Parallel()

	const (
		userPw  = "secret"
		ownerPw = "owner"
	)
	encrypted := encryptedWithUserPassword(t, userPw, ownerPw)
	ctx := context.Background()

	rejected := []struct {
		name string
		opts []OpenOption
	}{
		{"no password at all", nil},
		{"a wrong password", []OpenOption{WithPassword("wrong")}},
	}
	for _, tt := range rejected {
		t.Run(tt.name, func(t *testing.T) {
			r, err := Open(ctx, bytes.NewReader(encrypted), int64(len(encrypted)), tt.opts...)
			if err == nil {
				r.Close()
				t.Fatalf("Open with %s succeeded, want it refused", tt.name)
			}
			if !errors.Is(err, ErrInvalidPassword) {
				t.Errorf("Open with %s: err = %v, want errors.Is(err, ErrInvalidPassword)", tt.name, err)
			}
		})
	}

	accepted := []struct {
		name string
		pw   string
	}{
		{"the user password", userPw},
		{"the owner password", ownerPw},
	}
	for _, tt := range accepted {
		t.Run(tt.name, func(t *testing.T) {
			r, err := Open(ctx, bytes.NewReader(encrypted), int64(len(encrypted)), WithPassword(tt.pw))
			if err != nil {
				t.Fatalf("Open with %s: %v, want it accepted", tt.name, err)
			}
			defer r.Close()
			if !r.IsEncrypted() {
				t.Error("IsEncrypted = false, want true: the fixture is not actually encrypted")
			}
		})
	}
}
