package signature

import (
	"testing"
	"time"
)

// A signature's /Contents is attacker-controlled. The lexer bounds it at
// maxStringAccum (10 MB) but says nothing about its shape, so every parser
// below must report a malformed blob rather than panic or read past its input.
//
// pkg/internal/compliance carries its own, independent CMS reader with
// FuzzParseCMSSignature and FuzzBERToDER beside it. This package's readers are
// a separate implementation doing full SignerInfo verification plus OCSP and
// RFC 3161 timestamps, and had no fuzz target: the earlier census enumerated
// EXPORTED []byte entry points, and all six of these are unexported.
//
// Seeds are built in process by the package's own test helpers rather than
// committed .der fixtures: openssl CMS output is not reproducible across runs,
// and a binary fixture cannot be reviewed in a diff.

// cmsSeeds returns a valid PKCS#7 blob, a valid RFC 3161 timestamp token, and
// the degenerate shapes worth starting from.
func cmsSeeds(tb testing.TB) [][]byte {
	tb.Helper()

	_, cert := testSigningCert(tb)

	pkcs7, err := buildTestPKCS7(cert)
	if err != nil {
		tb.Fatalf("buildTestPKCS7: %v", err)
	}
	tst, err := buildTestTimestampToken(cert, time.Date(2023, 12, 15, 10, 30, 45, 0, time.UTC))
	if err != nil {
		tb.Fatalf("buildTestTimestampToken: %v", err)
	}

	return [][]byte{
		pkcs7,
		pkcs7[:len(pkcs7)/2], // truncated mid-structure
		tst,
		tst[:len(tst)/2],
		{},
		{0x30, 0x80, 0x00, 0x00}, // indefinite length, which DER forbids
		{0x30, 0x82, 0xff, 0xff}, // a length header promising far more than follows
	}
}

// FuzzParsePKCS7Certificates drives the certificate extractor reached from
// VerifySignatures with arbitrary bytes.
//
// It asserts only that the parser does not panic. A nil error with zero
// certificates is NOT treated as a defect: verify.go handles that case
// explicitly, so asserting against it would encode an invariant the code never
// promised.
func FuzzParsePKCS7Certificates(f *testing.F) {
	for _, s := range cmsSeeds(f) {
		f.Add(s)
	}
	f.Fuzz(func(_ *testing.T, data []byte) {
		_, _ = parsePKCS7Certificates(data)
	})
}

// FuzzCMSDigestHash drives the digest-algorithm reader, which runs on the plain
// extraction path: ExtractSignatures calls cmsDigestHash on raw /Contents and
// immediately calls New() on the result (signature.go).
//
// crypto.Hash.New panics on a hash that is not linked in, so "the returned hash
// is available" is a requirement the production call site already has, not an
// invented one. cms.go blank-imports md5, sha1, sha256 and sha512 precisely so
// every hash hashForOID can return is registered.
func FuzzCMSDigestHash(f *testing.F) {
	for _, s := range cmsSeeds(f) {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		h := cmsDigestHash(data)
		if !h.Available() {
			t.Fatalf("cmsDigestHash returned %v, which is not linked in: "+
				"ExtractSignatures calls New() on it and would panic", h)
		}
	})
}

// FuzzParseTimestampToken drives the RFC 3161 token reader. The token arrives
// as an unsigned attribute inside a signature's own CMS blob, so it is attacker
// controlled twice over: once as the blob, once as the attribute value.
func FuzzParseTimestampToken(f *testing.F) {
	for _, s := range cmsSeeds(f) {
		f.Add(s)
	}
	f.Fuzz(func(_ *testing.T, data []byte) {
		_, _ = parseTimestampToken(data)
	})
}

// FuzzParseOCSPResponse drives the OCSP reader that ResolveDSSInfo applies to
// each /OCSP entry of a document security store. It had no direct test of any
// kind before this target.
func FuzzParseOCSPResponse(f *testing.F) {
	for _, s := range cmsSeeds(f) {
		f.Add(s)
	}
	f.Fuzz(func(_ *testing.T, data []byte) {
		_, _ = parseOCSPResponse(data)
	})
}
