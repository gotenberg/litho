package pdf

import (
	"bytes"
	"context"
	"slices"
	"testing"
)

// TestEncryptDropsTheSourceSignature pins that encrypting a signed document
// leaves its signature out of the output and reports it.
//
// Encryption rewrites every object at a new offset and encrypts every string
// and stream, so the bytes a signature's /ByteRange names are not in the
// output (ISO 32000-1, 12.8.1): poppler's pdfsig reported the copied
// signature invalid. The /Contents of a signature dictionary a rewrite still
// holds stays in the clear (ISO 32000-2, 7.6.2), which
// TestEncryptPendingObjects_LeavesSignatureValueInTheClear pins in the
// mutations package.
func TestEncryptDropsTheSourceSignature(t *testing.T) {
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
		dropped := enc.DroppedSignatures()
		if closeErr := enc.Close(); closeErr != nil {
			t.Errorf("Close: %v", closeErr)
		}
		if err != nil {
			t.Fatalf("algorithm %d: encrypting: %v", algorithm, err)
		}
		if wantDropped := []DroppedSignature{{Field: want[0].FieldName}}; !slices.Equal(dropped, wantDropped) {
			t.Errorf("algorithm %d: DroppedSignatures = %v, want %v", algorithm, dropped, wantDropped)
		}

		got, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()), WithPassword("user"))
		if err != nil {
			t.Fatalf("algorithm %d: opening the encrypted output: %v", algorithm, err)
		}
		sigs, err := got.ExtractSignatures(ctx)
		if closeErr := got.Close(); closeErr != nil {
			t.Errorf("closing the encrypted output: %v", closeErr)
		}
		if err != nil || len(sigs) != 0 {
			t.Errorf("algorithm %d: the output holds %d signatures (%v), want none", algorithm, len(sigs), err)
		}
	}
}
