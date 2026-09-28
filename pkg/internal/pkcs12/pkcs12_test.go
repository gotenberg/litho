package pkcs12

import (
	"bytes"
	"crypto"
	"crypto/aes"
	"crypto/x509"
	"encoding/asn1"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// fixture is a store testdata/gen.sh writes with OpenSSL, and its password.
type fixture struct {
	file, password, signer string
}

// fixtures lists every store of testdata, each exercising other algorithms
// (see gen.sh).
var fixtures = []fixture{
	{"aes256.p12", "litho-test", "litho pkcs12 ec signer"},
	{"legacy.p12", "pässwörd", "litho pkcs12 rsa signer"},
	{"mixed.p12", "litho-test", "litho pkcs12 ec signer"},
	{"plain.p12", "litho-test", "litho pkcs12 ec signer"},
	{"empty.p12", "", "litho pkcs12 ec signer"},
}

func readFixture(t testing.TB, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestDecodeReadsOpenSSLStores(t *testing.T) {
	t.Parallel()
	for _, f := range fixtures {
		t.Run(f.file, func(t *testing.T) {
			t.Parallel()
			c, err := Decode(readFixture(t, f.file), f.password)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if len(c.Keys) != 1 || len(c.Certificates) != 2 {
				t.Fatalf("%d keys and %d certificates, want 1 and 2", len(c.Keys), len(c.Certificates))
			}
			key, ok := c.Keys[0].(crypto.Signer)
			if !ok {
				t.Fatalf("key of type %T", c.Keys[0])
			}
			var leaf *x509.Certificate
			for _, cert := range c.Certificates {
				if key.Public().(interface{ Equal(crypto.PublicKey) bool }).Equal(cert.PublicKey) {
					leaf = cert
				}
			}
			if leaf == nil || leaf.Subject.CommonName != f.signer {
				t.Fatalf("no certificate %q for the key", f.signer)
			}
		})
	}
}

func TestDecodeRefusesAWrongPassword(t *testing.T) {
	t.Parallel()
	for _, f := range fixtures {
		if f.file == "plain.p12" {
			// Nothing is encrypted and no MAC checks it: any password reads it.
			continue
		}
		if _, err := Decode(readFixture(t, f.file), f.password+"x"); !errors.Is(err, ErrIncorrectPassword) {
			t.Errorf("%s: %v, want ErrIncorrectPassword", f.file, err)
		}
	}
}

// berEncode rewrites a DER PFX the way streaming BER encoders do: the PFX,
// its authSafe ContentInfo and the [0] around its content in the indefinite
// length form, and the authSafe OCTET STRING split into two constructed
// chunks.
func berEncode(t *testing.T, der []byte) []byte {
	t.Helper()
	var p struct {
		Version  asn1.RawValue
		AuthSafe struct {
			ContentType asn1.RawValue
			Content     asn1.RawValue
		}
		MacData asn1.RawValue
	}
	if _, err := asn1.Unmarshal(der, &p); err != nil {
		t.Fatal(err)
	}
	var inner asn1.RawValue
	if _, err := asn1.Unmarshal(p.AuthSafe.Content.Bytes, &inner); err != nil {
		t.Fatal(err)
	}
	half := len(inner.Bytes) / 2
	chunk := func(b []byte) []byte {
		out, err := asn1.Marshal(b)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	var out []byte
	out = append(out, 0x30, 0x80)
	out = append(out, p.Version.FullBytes...)
	out = append(out, 0x30, 0x80)
	out = append(out, p.AuthSafe.ContentType.FullBytes...)
	out = append(out, 0xa0, 0x80, 0x24, 0x80)
	out = append(out, chunk(inner.Bytes[:half])...)
	out = append(out, chunk(inner.Bytes[half:])...)
	out = append(out, 0, 0, 0, 0, 0, 0)
	out = append(out, p.MacData.FullBytes...)
	return append(out, 0, 0)
}

func TestDecodeReadsBER(t *testing.T) {
	t.Parallel()
	ber := berEncode(t, readFixture(t, "aes256.p12"))
	c, err := Decode(ber, "litho-test")
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(c.Keys) != 1 || len(c.Certificates) != 2 {
		t.Errorf("%d keys and %d certificates, want 1 and 2", len(c.Keys), len(c.Certificates))
	}
}

// withMACIterations returns the store der with its MAC iteration count set
// to n.
func withMACIterations(t *testing.T, der []byte, n int) []byte {
	t.Helper()
	var p struct {
		Version  int
		AuthSafe asn1.RawValue
		MacData  macData
	}
	if _, err := asn1.Unmarshal(der, &p); err != nil {
		t.Fatal(err)
	}
	p.MacData.Iterations = n
	out, err := asn1.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestDecodeRefuses(t *testing.T) {
	t.Parallel()
	der := readFixture(t, "aes256.p12")
	for _, tc := range []struct {
		name string
		data []byte
		want error
	}{
		{"nothing", nil, ErrMalformed},
		{"a truncated store", der[:len(der)/2], ErrMalformed},
		{"trailing bytes", append(append([]byte{}, der...), 0), ErrMalformed},
		{"a store over MaxSize", make([]byte, MaxSize+1), ErrLimit},
		{"an iteration count over the cap", withMACIterations(t, der, maxIterations+1), ErrLimit},
		{"a zero iteration count", withMACIterations(t, der, 0), ErrMalformed},
		{"a nesting over the cap", bytes.Repeat([]byte{0x30, 0x80}, maxBERDepth+2), ErrLimit},
	} {
		if _, err := Decode(tc.data, "litho-test"); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, err, tc.want)
		}
	}
}

func TestBudgetCapsDerivations(t *testing.T) {
	t.Parallel()
	b := &budget{left: maxDerivations}
	for range maxDerivations {
		if err := b.spend(1); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.spend(1); !errors.Is(err, ErrLimit) {
		t.Errorf("derivation %d: %v, want ErrLimit", maxDerivations+1, err)
	}
}

func TestBMPPassword(t *testing.T) {
	t.Parallel()
	for pw, want := range map[string]string{
		"":           "0000",
		"ab":         "006100620000",
		"é":          "00e90000",
		"\U0001F600": "d83dde000000",
	} {
		if got := hex.EncodeToString(bmpPassword(pw)); got != want {
			t.Errorf("%q: %s, want %s", pw, got, want)
		}
	}
}

func TestRC2PiTableIsAPermutation(t *testing.T) {
	t.Parallel()
	var seen [256]bool
	for _, b := range rc2PiTable {
		if seen[b] {
			t.Fatalf("0x%02x twice", b)
		}
		seen[b] = true
	}
}

// TestRC2DecryptsRFC2268Vectors decrypts the test vectors of RFC 2268, 5.
func TestRC2DecryptsRFC2268Vectors(t *testing.T) {
	t.Parallel()
	for _, v := range []struct {
		key  string
		bits int
		pt   string
		ct   string
	}{
		{"0000000000000000", 63, "0000000000000000", "ebb773f993278eff"},
		{"ffffffffffffffff", 64, "ffffffffffffffff", "278b27e42e2f0d49"},
		{"3000000000000000", 64, "1000000000000001", "30649edf9be7d2c2"},
		{"88", 64, "0000000000000000", "61a8a244adacccf0"},
		{"88bca90e90875a", 64, "0000000000000000", "6ccf4308974c267f"},
		{"88bca90e90875a7f0f79c384627bafb2", 64, "0000000000000000", "1a807d272bbe5db1"},
		{"88bca90e90875a7f0f79c384627bafb2", 128, "0000000000000000", "2269552ab0f85ca6"},
	} {
		key, _ := hex.DecodeString(v.key)
		ct, _ := hex.DecodeString(v.ct)
		c, err := newRC2Decrypter(key, v.bits)
		if err != nil {
			t.Fatal(err)
		}
		pt := make([]byte, rc2BlockSize)
		c.Decrypt(pt, ct)
		if got := hex.EncodeToString(pt); got != v.pt {
			t.Errorf("key %s, %d bits: %s, want %s", v.key, v.bits, got, v.pt)
		}
	}
}

func TestCBCDecryptRefuses(t *testing.T) {
	t.Parallel()
	key := bytes.Repeat([]byte{1}, 16)
	c, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	iv := make([]byte, aes.BlockSize)
	// A block whose plaintext ends in 0x00, a padding no PKCS #7 encoder
	// writes: what decrypting under a wrong key typically leaves.
	badPad := make([]byte, aes.BlockSize)
	c.Encrypt(badPad, make([]byte, aes.BlockSize))
	for _, tc := range []struct {
		name   string
		iv, ct []byte
		want   error
	}{
		{"no block", iv, nil, ErrMalformed},
		{"a partial block", iv, make([]byte, 5), ErrMalformed},
		{"a short IV", iv[:4], make([]byte, aes.BlockSize), ErrMalformed},
		{"a bad padding", iv, badPad, ErrIncorrectPassword},
	} {
		if _, err := cbcDecrypt(c, tc.iv, tc.ct); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, err, tc.want)
		}
	}
}
