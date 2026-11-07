package offcrypto

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha1" //nolint:gosec // compared against StarOffice's SHA-1
	"errors"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
)

func testXML(n int) []byte {
	var b bytes.Buffer
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><office:document-content>`)
	for i := 0; b.Len() < n; i++ {
		b.WriteString(`<text:p text:style-name="P1">Paragraph `)
		b.WriteString(strings.Repeat("x", i%50))
		b.WriteString(`</text:p>`)
	}
	return b.Bytes()
}

func TestOpenODFEntry_RoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name string
		o    odfOptions
		size int
	}{
		{"blowfish-sha1-1k", odfOptions{cipher: "blowfish", checksum: "sha1", iterations: 1024}, 5000},
		{"blowfish-short", odfOptions{cipher: "blowfish", checksum: "sha1", iterations: 10}, 30},
		{"aes256-cbc-sha256-1k", odfOptions{cipher: "aes-cbc", aesBits: 256, sha256Start: true, checksum: "sha256", iterations: 100000}, 20000},
		{"aes128-cbc", odfOptions{cipher: "aes-cbc", aesBits: 128, sha256Start: true, checksum: "sha256", iterations: 3}, 900},
		{"aes192-cbc-no-checksum", odfOptions{cipher: "aes-cbc", aesBits: 192, sha256Start: true, iterations: 3}, 2000},
		{"aes256-gcm-argon2id", odfOptions{cipher: "aes-gcm", aesBits: 256, sha256Start: true, argon2: true}, 8000},
		{"aes128-gcm-pbkdf2", odfOptions{cipher: "aes-gcm", aesBits: 128, sha256Start: true, iterations: 5}, 100},
		{"no-size", odfOptions{cipher: "aes-cbc", aesBits: 256, sha256Start: true, checksum: "sha256", iterations: 2, omitSize: true}, 3000},
		{"staroffice-sha1", odfOptions{cipher: "blowfish", checksum: "sha1", starOffice: true, iterations: 1024}, 2000},
		{"aes-cbc-sha1-start", odfOptions{cipher: "aes-cbc", aesBits: 256, checksum: "sha256", iterations: 4}, 2000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plain := testXML(tc.size)
			pw := "pässwörd"
			if tc.o.starOffice {
				// 53 bytes: the length StarOffice's SHA-1 pads wrongly.
				pw = strings.Repeat("p", 53)
			}
			stored, p := odfEncrypt(t, plain, pw, tc.o, 11)
			got, err := OpenODFEntry(context.Background(), stored, p, pw)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, plain) {
				t.Fatalf("decrypted %d bytes, want %d", len(got), len(plain))
			}
			if _, err := OpenODFEntry(context.Background(), stored, p, "wrong"); !errors.Is(err, office.ErrEncrypted) {
				t.Errorf("wrong password: %v, want ErrEncrypted", err)
			}
			if _, err := OpenODFEntry(context.Background(), stored, p, ""); !errors.Is(err, office.ErrEncrypted) {
				t.Errorf("empty password: %v, want ErrEncrypted", err)
			}
		})
	}
}

func TestOpenODFEntry_Windows1252(t *testing.T) {
	plain := testXML(500)
	stored, p := odfEncrypt(t, plain, "café", odfOptions{cipher: "blowfish", checksum: "sha1", starOffice: true, cp1252: true, iterations: 1024}, 12)
	got, err := OpenODFEntry(context.Background(), stored, p, "café")
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("OpenOffice.org 1.x start key: %v", err)
	}
}

func TestOpenODFEntry_SHA256StartKeyUndeclared(t *testing.T) {
	// fdo#47482: LibreOffice 3.6 and earlier wrote SHA-256 start keys with
	// no start-key-generation element.
	plain := testXML(500)
	stored, p := odfEncrypt(t, plain, "pw", odfOptions{cipher: "aes-cbc", aesBits: 256, sha256Start: true, checksum: "sha1", iterations: 4}, 13)
	p.StartKeyGenerationName, p.StartKeySize = "", ""
	got, err := OpenODFEntry(context.Background(), stored, p, "pw")
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("undeclared SHA-256 start key: %v", err)
	}
}

func TestOpenODFEntry_Refusals(t *testing.T) {
	plain := testXML(3000)
	stored, base := odfEncrypt(t, plain, "pw", odfOptions{cipher: "aes-cbc", aesBits: 256, sha256Start: true, checksum: "sha256", iterations: 2}, 14)
	gcmStored, gcm := odfEncrypt(t, plain, "pw", odfOptions{cipher: "aes-gcm", aesBits: 256, sha256Start: true, argon2: true}, 15)
	with := func(fn func(p *ODFParams)) ODFParams {
		p := base
		fn(&p)
		return p
	}
	withGCM := func(fn func(p *ODFParams)) ODFParams {
		p := gcm
		fn(&p)
		return p
	}
	for _, tc := range []struct {
		name   string
		stored []byte
		p      ODFParams
		want   error
		msg    string
	}{
		{"algorithm", stored, with(func(p *ODFParams) { p.AlgorithmName = "http://www.w3.org/2001/04/xmlenc#tripledes-cbc" }), office.ErrEncrypted, "algorithm"},
		{"aes size", stored, with(func(p *ODFParams) { p.AlgorithmName = "http://www.w3.org/2001/04/xmlenc#aes512-cbc" }), office.ErrEncrypted, "algorithm"},
		{"iv base64", stored, with(func(p *ODFParams) { p.InitialisationVector = "!!" }), office.ErrMalformed, "base64"},
		{"iv length", stored, with(func(p *ODFParams) { p.InitialisationVector = "AAAA" }), office.ErrMalformed, "initialisation vector"},
		{"start key", stored, with(func(p *ODFParams) { p.StartKeyGenerationName = "MD5" }), office.ErrEncrypted, "start key"},
		{"start key size", stored, with(func(p *ODFParams) { p.StartKeySize = "20" }), office.ErrEncrypted, "start key"},
		{"kdf", stored, with(func(p *ODFParams) { p.KeyDerivationName = "PGP" }), office.ErrEncrypted, "key derivation"},
		{"iterations", stored, with(func(p *ODFParams) { p.IterationCount = "10000001" }), office.ErrEncrypted, "cap"},
		{"zero iterations", stored, with(func(p *ODFParams) { p.IterationCount = "0" }), office.ErrMalformed, "iteration"},
		{"iterations syntax", stored, with(func(p *ODFParams) { p.IterationCount = "-1" }), office.ErrMalformed, "non-negative"},
		{"salt", stored, with(func(p *ODFParams) { p.Salt = "" }), office.ErrMalformed, "salt"},
		{"key size", stored, with(func(p *ODFParams) { p.KeySize = "16" }), office.ErrMalformed, "derived key"},
		{"checksum base64", stored, with(func(p *ODFParams) { p.Checksum = "%" }), office.ErrMalformed, "checksum"},
		{"size cap", stored, with(func(p *ODFParams) { p.Size = "999999999999" }), office.ErrInputTooLarge, "cap"},
		{"size mismatch", stored, with(func(p *ODFParams) { p.Size = "10" }), office.ErrMalformed, "manifest:size"},
		{"size short", stored, with(func(p *ODFParams) { p.Size = "999999" }), office.ErrMalformed, "manifest:size"},
		{"cbc blocks", stored[:len(stored)-1], base, office.ErrMalformed, "AES-CBC"},
		{"checksum", stored, with(func(p *ODFParams) { p.Checksum = "AAAA" }), office.ErrEncrypted, "password"},
		{"argon2 memory", gcmStored, withGCM(func(p *ODFParams) { p.Argon2Memory = "262145" }), office.ErrEncrypted, "cap"},
		{"argon2 lanes", gcmStored, withGCM(func(p *ODFParams) { p.Argon2Lanes = "17" }), office.ErrEncrypted, "cap"},
		{"argon2 work", gcmStored, withGCM(func(p *ODFParams) { p.Argon2Iterations, p.Argon2Memory = "64", "65536" }), office.ErrEncrypted, "work"},
		{"argon2 zero", gcmStored, withGCM(func(p *ODFParams) { p.Argon2Lanes = "0" }), office.ErrMalformed, "zero"},
		{"gcm iv", append([]byte{gcmStored[0] ^ 1}, gcmStored[1:]...), gcm, office.ErrMalformed, "IV"},
		{"gcm short", gcmStored[:20], gcm, office.ErrMalformed, "AES-GCM"},
		{"gcm tag", append(bytes.Clone(gcmStored[:len(gcmStored)-1]), gcmStored[len(gcmStored)-1]^1), gcm, office.ErrEncrypted, "password"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := OpenODFEntry(context.Background(), tc.stored, tc.p, "pw")
			if !errors.Is(err, tc.want) || !strings.Contains(err.Error(), tc.msg) {
				t.Fatalf("got %v, want %v mentioning %q", err, tc.want, tc.msg)
			}
		})
	}
}

func TestOpenODFEntry_VerifiedButNotDeflate(t *testing.T) {
	// A checksum that matches a stream that does not inflate is a damaged
	// entry, not a wrong password.
	_, p := odfEncrypt(t, testXML(100), "pw", odfOptions{cipher: "aes-gcm", aesBits: 256, sha256Start: true, iterations: 1}, 16)
	e, err := parseODFParams(p)
	if err != nil {
		t.Fatal(err)
	}
	key, err := e.deriveKey(context.Background(), e.attempts("pw")[0].startKey)
	if err != nil {
		t.Fatal(err)
	}
	stored := gcmSeal(t, key, e.iv, []byte{0xFF, 0xFF, 0xFF})
	if _, err := OpenODFEntry(context.Background(), stored, p, "pw"); !errors.Is(err, office.ErrMalformed) {
		t.Fatalf("got %v, want ErrMalformed", err)
	}
}

func TestOpenODFEntry_Context(t *testing.T) {
	stored, p := odfEncrypt(t, testXML(100), "pw", odfOptions{cipher: "blowfish", checksum: "sha1", iterations: 1}, 17)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := OpenODFEntry(ctx, stored, p, "pw"); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
	if _, err := pbkdf2SHA1(ctx, []byte("k"), []byte("s"), maxPBKDF2Iterations, 16); !errors.Is(err, context.Canceled) {
		t.Fatalf("PBKDF2: got %v, want context.Canceled", err)
	}
}

func TestPBKDF2SHA1_RFC6070(t *testing.T) {
	for _, tc := range []struct {
		password, salt string
		iter, keyLen   int
		want           string
	}{
		{"password", "salt", 1, 20, "0c60c80f961f0e71f3a9b524af6012062fe037a6"},
		{"password", "salt", 4096, 20, "4b007901b765489abead49d926f721d065a429c1"},
		{"passwordPASSWORDpassword", "saltSALTsaltSALTsaltSALTsaltSALTsalt", 4096, 25, "3d2eec4fe41c849b80c8d83662c0e44a8b291a964cf2f07038"},
	} {
		got, err := pbkdf2SHA1(context.Background(), []byte(tc.password), []byte(tc.salt), tc.iter, tc.keyLen)
		if err != nil {
			t.Fatal(err)
		}
		if h := hexString(got); h != tc.want {
			t.Errorf("PBKDF2(%q, %q, %d) = %s, want %s", tc.password, tc.salt, tc.iter, h, tc.want)
		}
	}
}

func TestStarOfficeSHA1_DiffersOnlyAt52To55(t *testing.T) {
	for n := range 200 {
		msg := bytes.Repeat([]byte{'a'}, n)
		so, std := starOfficeSHA1(msg), sha1.Sum(msg)
		buggy := n%64 >= 52 && n%64 <= 55
		if (so != std) != buggy {
			t.Errorf("length %d: StarOffice SHA-1 differs %v, want %v", n, so != std, buggy)
		}
	}
	// No library computes the buggy padding: the value is pinned from an
	// independent implementation of rtl's endSHA (sal/rtl/digest.cxx).
	if h := hexString(func() []byte { s := starOfficeSHA1(bytes.Repeat([]byte{'a'}, 53)); return s[:] }()); h != starOffice53a {
		t.Errorf("StarOffice SHA-1 of 53 'a' = %s, want %s", h, starOffice53a)
	}
}

// starOffice53a is StarOffice's SHA-1 of 53 'a' bytes.
const starOffice53a = "79feaa24cb7f13e8687859d2b1e27096e5ddba48"

func gcmSeal(t *testing.T, key, iv, plain []byte) []byte {
	t.Helper()
	b, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	g, err := cipher.NewGCM(b)
	if err != nil {
		t.Fatal(err)
	}
	return g.Seal(bytes.Clone(iv), iv, plain, nil)
}

func hexString(b []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, 2*len(b))
	for i, c := range b {
		out[2*i], out[2*i+1] = digits[c>>4], digits[c&15]
	}
	return string(out)
}
