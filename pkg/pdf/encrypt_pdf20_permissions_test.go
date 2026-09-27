package pdf

import (
	"bytes"
	"context"
	"fmt"
	"testing"
)

// permissionsPDF builds a one-page document whose header declares header and
// whose catalog carries extraCatalog.
func permissionsPDF(header, extraCatalog string) []byte {
	const content = "BT /F1 12 Tf 20 700 Td (page) Tj ET\n"
	data := buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R " + extraCatalog + " >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R" +
			" /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	})
	// Same length as the header buildPDF writes, so the offsets hold.
	return bytes.Replace(data, []byte("%PDF-1.7"), []byte("%PDF-"+header), 1)
}

// TestEncryptSetsBit10OnAPDF20Output pins that an encrypted output declaring
// PDF 2.0 sets bit 10 of its P entry whatever mask the caller gives, and that
// an output declaring an earlier version keeps the mask as given.
//
// ISO 32000-2, Table 22 marks the bit "Not used", the restriction it carried
// "deprecated in PDF 2.0", and still asks writers to set it: "PDF readers
// shall ignore this bit and PDF writers shall always set this bit to 1 to
// ensure compatibility with PDF readers following earlier specifications." ISO
// 32000-1, Table 22 still makes it the permission to extract content for
// accessibility, which a PDF 1.x file may withhold. Every other bit is the
// caller's, and the P value is part of what the file key is derived from (ISO
// 32000-1, 7.6.3.3, Algorithm 2) and what /Perms encrypts (ISO 32000-2,
// 7.6.4.4.9), so the output must open under its password with the P it
// declares.
func TestEncryptSetsBit10OnAPDF20Output(t *testing.T) {
	t.Parallel()

	const (
		userPw        = "user"
		accessibility = int32(1 << 9)
	)
	// Every permission but extraction for accessibility, reserved bits 1-2
	// clear as ISO 32000-1, Table 22 asks.
	mask := ^int32(0b11 | accessibility)
	encrypt := func(alg EncryptAlgorithm) EncryptConfig {
		return EncryptConfig{UserPassword: userPw, OwnerPassword: "owner", Algorithm: alg, Permissions: mask}
	}
	pdf20 := permissionsPDF("2.0", "")
	pdf17 := permissionsPDF("1.7", "")
	catalog20 := permissionsPDF("1.7", "/Version /2.0")

	type write func(ctx context.Context, r *Reader, cfg EncryptConfig, out *bytes.Buffer) error
	encryptMethod := func(opts ...WriteOption) write {
		return func(ctx context.Context, r *Reader, cfg EncryptConfig, out *bytes.Buffer) error {
			w := NewWriter(r)
			defer w.Close()
			w.Encrypt(cfg)
			return w.WriteTo(ctx, out, opts...)
		}
	}
	withEncryption := func(ctx context.Context, r *Reader, cfg EncryptConfig, out *bytes.Buffer) error {
		w := NewWriter(r)
		defer w.Close()
		return w.WriteTo(ctx, out, WithEncryption(cfg))
	}
	split := func(ctx context.Context, r *Reader, cfg EncryptConfig, out *bytes.Buffer) error {
		return Split(ctx, r, []int{1}, out, WithEncryption(cfg))
	}
	merge := func(ctx context.Context, r *Reader, cfg EncryptConfig, out *bytes.Buffer) error {
		return Merge(ctx, []*Reader{r, r}, out, WithEncryption(cfg), WithVersion("2.0"))
	}
	merge17 := func(ctx context.Context, r *Reader, cfg EncryptConfig, out *bytes.Buffer) error {
		return Merge(ctx, []*Reader{r, r}, out, WithEncryption(cfg))
	}

	for _, tc := range []struct {
		name  string
		src   []byte
		write write
		pdf20 bool
	}{
		{name: "Writer.Encrypt on a PDF 2.0 source", src: pdf20, write: encryptMethod(), pdf20: true},
		{name: "WithEncryption on a PDF 2.0 source", src: pdf20, write: withEncryption, pdf20: true},
		{name: "Split of a PDF 2.0 source", src: pdf20, write: split, pdf20: true},
		{name: "Merge written as PDF 2.0", src: pdf17, write: merge, pdf20: true},
		{name: "a catalog declaring 2.0 over a 1.7 header", src: catalog20, write: encryptMethod(), pdf20: true},
		{name: "WithVersion 2.0 on a PDF 1.7 source", src: pdf17, write: encryptMethod(WithVersion("2.0")), pdf20: true},
		{name: "Writer.Encrypt on a PDF 1.7 source", src: pdf17, write: encryptMethod()},
		{name: "Split of a PDF 1.7 source", src: pdf17, write: split},
		{name: "Merge of PDF 1.7 sources", src: pdf17, write: merge17},
	} {
		for _, alg := range []EncryptAlgorithm{EncryptRC4_40, EncryptRC4_128, EncryptAES128, EncryptAES256} {
			t.Run(fmt.Sprintf("%s/algorithm %d", tc.name, alg), func(t *testing.T) {
				t.Parallel()

				ctx := context.Background()
				r, err := Open(ctx, bytes.NewReader(tc.src), int64(len(tc.src)))
				if err != nil {
					t.Fatalf("Open: %v", err)
				}
				defer r.Close()
				var out bytes.Buffer
				if err := tc.write(ctx, r, encrypt(alg), &out); err != nil {
					t.Fatalf("write: %v", err)
				}

				got, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()), WithPassword(userPw))
				if err != nil {
					t.Fatalf("the output does not open under its user password: %v", err)
				}
				defer got.Close()
				if !got.IsEncrypted() {
					t.Fatal("the output is not encrypted")
				}
				if v := got.Version(); (v == "2.0") != tc.pdf20 {
					t.Fatalf("precondition: the output declares %s", v)
				}
				want := mask
				if tc.pdf20 {
					want |= accessibility
				}
				if p := int32(got.doc.Crypto.PermissionFlags()); p != want { //nolint:gosec // P is a 32-bit field, read back as written.
					t.Errorf("P = %d (bit 10 %t), want %d (bit 10 %t)", p, p&accessibility != 0, want, want&accessibility != 0)
				}
			})
		}
	}
}
