package pdf

import (
	"bytes"
	"context"
	"fmt"
	"testing"
)

// TestEncryptWritesTheReservedPermissionBits pins that every encrypted output
// carries the P value ISO 32000-2, 7.6.4.2, Table 22 requires of its reserved
// bits, whatever mask [EncryptConfig.Permissions] holds: bits 1-2 "Reserved.
// Must be zero (0).", bits 7-8 "Reserved. Must be 1." and bits 13-32
// "(Security handlers of revision 3 or greater) Reserved. Must be 1.".
//
// -1, the mask the CLI writes and [EncryptConfig.Permissions] documents for
// "all permissions granted", set bits 1-2, and a mask denying everything, 0,
// cleared bits 7-8 and 13-32. The permission bits stay the caller's, and an
// output declaring PDF 2.0 still sets bit 10.
func TestEncryptWritesTheReservedPermissionBits(t *testing.T) {
	t.Parallel()

	const userPw = "user"
	pdf17 := permissionsPDF("1.7", "")
	pdf20 := permissionsPDF("2.0", "")

	type write func(ctx context.Context, r *Reader, cfg EncryptConfig, out *bytes.Buffer) error
	encryptMethod := func(ctx context.Context, r *Reader, cfg EncryptConfig, out *bytes.Buffer) error {
		w := NewWriter(r)
		defer w.Close()
		w.Encrypt(cfg)
		return w.WriteTo(ctx, out)
	}
	split := func(ctx context.Context, r *Reader, cfg EncryptConfig, out *bytes.Buffer) error {
		return Split(ctx, r, []int{1}, out, WithEncryption(cfg))
	}
	merge := func(ctx context.Context, r *Reader, cfg EncryptConfig, out *bytes.Buffer) error {
		return Merge(ctx, []*Reader{r, r}, out, WithEncryption(cfg))
	}
	pipeline := func(ctx context.Context, r *Reader, cfg EncryptConfig, out *bytes.Buffer) error {
		_, err := NewPipeline().Open(r).
			Apply(func(_ context.Context, w *Writer) error { w.Encrypt(cfg); return nil }).
			Split([]int{1}).
			WriteTo(ctx, out)
		return err
	}

	for _, tc := range []struct {
		name  string
		src   []byte
		write write
		mask  int32
		want  int32
	}{
		{name: "every permission, Writer.Encrypt", src: pdf17, write: encryptMethod, mask: -1, want: -4},
		{name: "every permission, Split", src: pdf17, write: split, mask: -1, want: -4},
		{name: "every permission, Merge", src: pdf17, write: merge, mask: -1, want: -4},
		{name: "every permission, a pipeline", src: pdf17, write: pipeline, mask: -1, want: -4},
		{name: "no permission, Writer.Encrypt", src: pdf17, write: encryptMethod, mask: 0, want: -3904},
		{name: "no permission, Split", src: pdf17, write: split, mask: 0, want: -3904},
		// ISO 32000-2, Table 22, bit 10: PDF writers set it on a 2.0 output.
		{name: "no permission, Writer.Encrypt on PDF 2.0", src: pdf20, write: encryptMethod, mask: 0, want: -3904 | 1<<9},
		{name: "every permission, Writer.Encrypt on PDF 2.0", src: pdf20, write: encryptMethod, mask: -1, want: -4},
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
				cfg := EncryptConfig{UserPassword: userPw, OwnerPassword: "owner", Algorithm: alg, Permissions: tc.mask}
				var out bytes.Buffer
				if err := tc.write(ctx, r, cfg, &out); err != nil {
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
				p := int32(got.doc.Crypto.PermissionFlags()) //nolint:gosec // P is a 32-bit field, read back as written.
				if p != tc.want {
					t.Errorf("P = %d (%#08x), want %d (%#08x)", p, uint32(p), tc.want, uint32(tc.want))
				}
				granted := tc.mask == -1
				if perms := got.Permissions(); perms.Print != granted || perms.ExtractText != granted || perms.Assemble != granted {
					t.Errorf("Permissions() = %+v, want every flag %t", perms, granted)
				}
			})
		}
	}
}
