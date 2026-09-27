package pdf

import (
	"bytes"
	"context"
	"testing"
)

// Each encryption algorithm carries a minimum PDF version, and the output
// declares it. AESV2 is a PDF 1.6 crypt filter; AESV3 is ISO 32000-2, declared
// as PDF 1.7 plus an Adobe extension level rather than 2.0, which is what other
// producers write. RC4 needs nothing beyond 1.4.
//
// The engine's own tests pin this. Nothing at the public surface did: the six
// encryption tests next door assert /Encrypt presence, page counts and text,
// all of which hold just as well on a file that misdescribes its version.

func TestWriteTo_EncryptionDeclaresItsVersion(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		algorithm  EncryptAlgorithm
		wantHeader string
		wantADBE   bool
	}{
		// /V 2 /R 3 is a PDF 1.4 handler, so the source version already
		// covers it and nothing should move.
		{"RC4-128 leaves the version alone", EncryptRC4_128, "%PDF-1.4", false},
		// AESV2 arrived in PDF 1.6 (ISO 32000-1, Table 25).
		{"AES-128 raises to 1.6 bare", EncryptAES128, "%PDF-1.6", false},
		// AESV3 is ISO 32000-2, declared as an extension to 1.7.
		{"AES-256 raises to 1.7 with an extension", EncryptAES256, "%PDF-1.7", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			data := threePagePDF()

			// Without this the raises below would not be observable.
			if !bytes.HasPrefix(data, []byte("%PDF-1.4")) {
				t.Fatalf("fixture declares %q, want %%PDF-1.4", pdfHeader(data))
			}

			r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer r.Close()

			var out bytes.Buffer
			err = NewWriter(r).WriteTo(ctx, &out, WithEncryption(EncryptConfig{
				UserPassword: "u",
				Algorithm:    tc.algorithm,
			}))
			if err != nil {
				t.Fatalf("WriteTo: %v", err)
			}

			// Assert the output is actually encrypted, or the version check
			// would be made against a file that never took this path.
			if !bytes.Contains(out.Bytes(), []byte("/Encrypt")) {
				t.Fatal("output carries no /Encrypt dictionary, so this proves nothing")
			}
			if !bytes.HasPrefix(out.Bytes(), []byte(tc.wantHeader)) {
				t.Errorf("output declares %q, want %s", pdfHeader(out.Bytes()), tc.wantHeader)
			}

			// PDF 1.7 alone is only half the declaration for AES-256: /V 5 is
			// an extension to that version, not a feature of it, so the
			// catalog has to name the extension it relies on.
			if got := bytes.Contains(out.Bytes(), []byte("/ADBE")); got != tc.wantADBE {
				t.Errorf("/ADBE extension present = %v, want %v", got, tc.wantADBE)
			}
			if tc.wantADBE {
				for _, want := range []string{"/BaseVersion /1.7", "/ExtensionLevel 3"} {
					if !bytes.Contains(out.Bytes(), []byte(want)) {
						t.Errorf("output does not carry %q", want)
					}
				}
			}
		})
	}
}

// TestEncryptConfigZeroValueIsAES256 pins that an [EncryptConfig] naming no
// algorithm encrypts with AES-256 (V 5, R 6), the handler [EncryptAES256]
// recommends. The zero value selected [EncryptRC4_40], a 40-bit handler its own
// documentation calls weak and for legacy use only. Each constant maps to the
// handler it names, RC4-40 included.
func TestEncryptConfigZeroValueIsAES256(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name         string
		algorithm    EncryptAlgorithm
		wantV, wantR int
	}{
		{name: "zero value", wantV: 5, wantR: 6},
		{name: "EncryptAES256", algorithm: EncryptAES256, wantV: 5, wantR: 6},
		{name: "EncryptAES128", algorithm: EncryptAES128, wantV: 4, wantR: 4},
		{name: "EncryptRC4_128", algorithm: EncryptRC4_128, wantV: 2, wantR: 3},
		{name: "EncryptRC4_40", algorithm: EncryptRC4_40, wantV: 1, wantR: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := EncryptConfig{UserPassword: "user", OwnerPassword: "owner", Algorithm: tc.algorithm, Permissions: -1}
			if back := encryptConfigFromInternal(cfg.toInternal()); back != cfg {
				t.Errorf("the config reads back from the write engine as %+v, want %+v", back, cfg)
			}
			out, err := writeWithOptions(t, twinPagesPDF(), nil, nil, []WriteOption{WithEncryption(cfg)})
			if err != nil {
				t.Fatalf("WriteTo: %v", err)
			}
			r, err := Open(context.Background(), bytes.NewReader(out), int64(len(out)), WithPassword("user"))
			if err != nil {
				t.Fatalf("reopening the output: %v", err)
			}
			defer r.Close()
			got := r.doc.Crypto
			if got == nil {
				t.Fatal("the output reopens with no security handler")
			}
			if got.V != tc.wantV || got.R != tc.wantR {
				t.Errorf("the output's handler is V %d R %d, want V %d R %d", got.V, got.R, tc.wantV, tc.wantR)
			}
		})
	}
}
