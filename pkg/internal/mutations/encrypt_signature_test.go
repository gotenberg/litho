package mutations

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/crypto"
)

// TestEncryptPendingObjects_LeavesSignatureValueInTheClear pins that the
// encryption sweep writes the /Contents of a signature dictionary as it was,
// and encrypts its other strings.
//
// ISO 32000-2, 7.6.2 lists the /Contents of a signature dictionary among the
// strings encryption does not apply to: the value is written after the digest
// is computed, into the hole the ByteRange leaves, so readers take the bytes as
// they stand. The writer encrypted it like any other string, and every reader,
// litho's included, then saw a signature value that was noise.
//
// A full rewrite now draws a signed signature into its page and leaves its
// dictionary out (see [BakeSignaturesForRewrite]), so encrypting a signed
// document through the Writer no longer carries one. The sweep is pinned here,
// where it still decides for any signature dictionary a rewrite holds.
func TestEncryptPendingObjects_LeavesSignatureValueInTheClear(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	const value = "\x30\x82\x01\x00 signature value"
	const name = "Jane Doe"
	for _, algorithm := range []crypto.Algorithm{crypto.RC4_128, crypto.AES_128, crypto.AES_256} {
		alloc := writer.NewObjAllocator(0)
		sig := alloc.Alloc(ast.PdfDict{
			"Type":      ast.PdfName("Sig"),
			"Name":      ast.PdfString(name),
			"Contents":  ast.PdfString(value),
			"ByteRange": ast.PdfArray{ast.PdfInt(0), ast.PdfInt(10), ast.PdfInt(20), ast.PdfInt(30)},
		})
		enc, _, err := crypto.NewEncryptor(crypto.EncryptConfig{
			UserPassword: "user", OwnerPassword: "owner", Algorithm: algorithm, Permissions: -1,
		}, GenerateDocumentID())
		if err != nil {
			t.Fatalf("algorithm %d: NewEncryptor: %v", algorithm, err)
		}
		if err := EncryptPendingObjects(ctx, alloc, enc); err != nil {
			t.Fatalf("algorithm %d: EncryptPendingObjects: %v", algorithm, err)
		}

		got, ok := alloc.Pending()[sig.ObjectNum].(ast.PdfDict)
		if !ok {
			t.Fatalf("algorithm %d: the signature dictionary is gone from the allocator", algorithm)
		}
		if got["Contents"] != ast.PdfString(value) {
			t.Errorf("algorithm %d: /Contents = %q, want the signature value in the clear", algorithm, got["Contents"])
		}
		// The control: a sweep that encrypted nothing would pass the check
		// above.
		if got["Name"] == ast.PdfString(name) {
			t.Errorf("algorithm %d: /Name is still in the clear, so the sweep did not run", algorithm)
		}
	}
}
