package pdf

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

// TestEncryptAES256PreparesThePasswordWithSASLprep pins ISO 32000-2,
// 7.6.4.3.3 through the public API: the output opens with every spelling
// that SASLprep prepares to the same string (U+2168 ROMAN NUMERAL NINE and
// "IX", RFC 4013, 3), and a password SASLprep rejects writes nothing.
func TestEncryptAES256PreparesThePasswordWithSASLprep(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	out := splitOnePage(t, ctx, threePagePDF(), WithEncryption(EncryptConfig{
		UserPassword: "Ⅸ", OwnerPassword: "owner", Algorithm: EncryptAES256, Permissions: -1,
	}))
	assertPasswordProtected(t, ctx, out, "IX")
	assertPasswordProtected(t, ctx, out, "Ⅸ")

	r, err := Open(ctx, bytes.NewReader(threePagePDF()), int64(len(threePagePDF())))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	var buf bytes.Buffer
	err = NewWriter(r).WriteTo(ctx, &buf, WithEncryption(EncryptConfig{UserPassword: "tab\there", Algorithm: EncryptAES256}))
	if !errors.Is(err, ErrPasswordRejected) {
		t.Fatalf("WriteTo with a prohibited password = %v, want ErrPasswordRejected", err)
	}
	if buf.Len() != 0 {
		t.Errorf("WriteTo wrote %d bytes before refusing the password", buf.Len())
	}

	// The legacy handlers take the password bytes as given (ISO 32000-1,
	// 7.6.3.3, Algorithm 2), so the same password encrypts there.
	out = splitOnePage(t, ctx, threePagePDF(), WithEncryption(EncryptConfig{
		UserPassword: "tab\there", OwnerPassword: "owner", Algorithm: EncryptAES128, Permissions: -1,
	}))
	assertPasswordProtected(t, ctx, out, "tab\there")
}
