package signature

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestHasSignaturesAgreesWithExtractSignatures pins that HasSignatures answers
// yes exactly when ExtractSignatures returns a signature, wherever the field
// sits, and that an empty signature field does not count.
func TestHasSignaturesAgreesWithExtractSignatures(t *testing.T) {
	t.Parallel()

	empty := buildSigPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Sig /T (empty) /Rect [0 0 10 10] >>",
	}, "")

	for _, tc := range []struct {
		name string
		data []byte
		want bool
	}{
		{"registered", unregisteredSigPDF(true), true},
		{"page only", unregisteredSigPDF(false), true},
		{"empty field", empty, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			doc, err := document.NewDocument(ctx, bytes.NewReader(tc.data), int64(len(tc.data)), nil)
			if err != nil {
				t.Fatalf("NewDocument: %v", err)
			}
			got, err := HasSignatures(ctx, doc)
			if err != nil {
				t.Fatalf("HasSignatures: %v", err)
			}
			if got != tc.want {
				t.Errorf("HasSignatures = %t, want %t", got, tc.want)
			}
			if extracted := len(sigsOf(t, tc.data)) > 0; extracted != got {
				t.Errorf("HasSignatures = %t, but ExtractSignatures found signatures: %t", got, extracted)
			}
		})
	}
}

// TestPresenceSkipsTheDigest pins that the presence walk reads no signed
// bytes, while ExtractSignatures still digests them.
func TestPresenceSkipsTheDigest(t *testing.T) {
	t.Parallel()

	data := unregisteredSigPDF(true)
	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}

	hashed, err := extractSignatures(ctx, doc, true)
	if err != nil || len(hashed) != 1 || hashed[0].DocumentHash == "" {
		t.Fatalf("extractSignatures with the digest = %+v, %v; want one signature with a DocumentHash", hashed, err)
	}

	unhashed, err := extractSignatures(ctx, doc, false)
	if err != nil || len(unhashed) != 1 || unhashed[0].DocumentHash != "" {
		t.Errorf("extractSignatures without the digest = %+v, %v; want one signature without a DocumentHash", unhashed, err)
	}
}
