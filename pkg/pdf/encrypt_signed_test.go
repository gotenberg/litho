package pdf

import (
	"bytes"
	"context"
	"testing"
)

// TestEncryptLeavesTheSignatureValueInTheClear pins that encrypting a signed
// document writes each signature's /Contents as it was, unencrypted.
//
// ISO 32000-2, 7.6.2 lists the /Contents of a signature dictionary among the
// strings encryption does not apply to: the value is written after the digest
// is computed, into the hole the ByteRange leaves, so readers take the bytes
// as they stand. The writer encrypted it like any other string, and every
// reader, litho's included, then saw a signature value that was noise.
func TestEncryptLeavesTheSignatureValueInTheClear(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := threeTextPagesPDF()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	w := NewWriter(r)
	defer w.Close()
	w.Sign(newCMSTestSigner(t))
	var signed bytes.Buffer
	if err := w.WriteTo(ctx, &signed); err != nil {
		t.Fatalf("signing: %v", err)
	}
	signedReader, err := Open(ctx, bytes.NewReader(signed.Bytes()), int64(signed.Len()))
	if err != nil {
		t.Fatalf("reopening the signed document: %v", err)
	}
	defer signedReader.Close()
	want, err := signedReader.ExtractSignatures(ctx)
	if err != nil || len(want) != 1 {
		t.Fatalf("the signed document holds %d signatures (%v), want 1", len(want), err)
	}

	for _, algorithm := range []EncryptAlgorithm{EncryptRC4_128, EncryptAES128, EncryptAES256} {
		enc := NewWriter(signedReader)
		enc.Encrypt(EncryptConfig{UserPassword: "user", OwnerPassword: "owner", Algorithm: algorithm, Permissions: -1})
		var out bytes.Buffer
		err := enc.WriteTo(ctx, &out)
		if closeErr := enc.Close(); closeErr != nil {
			t.Errorf("Close: %v", closeErr)
		}
		if err != nil {
			t.Fatalf("algorithm %d: encrypting: %v", algorithm, err)
		}

		got, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()), WithPassword("user"))
		if err != nil {
			t.Fatalf("algorithm %d: opening the encrypted output: %v", algorithm, err)
		}
		sigs, err := got.ExtractSignatures(ctx)
		_ = got.Close()
		if err != nil || len(sigs) != 1 {
			t.Fatalf("algorithm %d: the output holds %d signatures (%v), want 1", algorithm, len(sigs), err)
		}
		if sigs[0].Contents != want[0].Contents {
			t.Errorf("algorithm %d: the signature value reads back %d bytes differing from the %d signed",
				algorithm, len(sigs[0].Contents), len(want[0].Contents))
		}
	}
}
