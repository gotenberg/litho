package signature

import (
	"crypto/x509"
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

// FuzzVerifyCMSSignature drives the full detached-signature verifier, which no
// target reached: a coverage profile over the four targets above reported
// verifyCMSSignature at 0.0%. Each of them parses one COMPONENT of a CMS blob;
// this one runs the composition (ContentInfo, SignedData, the signer lookup,
// the messageDigest comparison and the public-key check), and it is reachable
// from the exported VerifySignatures.
//
// Both inputs are attacker controlled and mutually independent. der is the
// signature dictionary's /Contents (verify.go:137, der := []byte(sig.Contents))
// and content is the concatenation of the two ByteRange chunks read back out of
// the file, so nothing makes one agree with the other.
//
// The certificates are parsed from the SAME blob rather than pinned to a test
// certificate, because that is what VerifySignatures does at verify.go:139. A
// fixed certificate would pair a known-good signer with arbitrary SignedData,
// a combination the production path never builds.
//
// It asserts only that the verifier does not panic. The godoc promises a signer
// certificate alongside a nil error, but the sole caller discards it
// (verify.go:266, _, verifyErr := ...), so asserting on it would test the
// comment rather than anything the code depends on.
func FuzzVerifyCMSSignature(f *testing.F) {
	for _, s := range cmsSeeds(f) {
		f.Add(s, []byte("the signed byte range"))
		f.Add(s, []byte{})
	}

	f.Fuzz(func(_ *testing.T, der, content []byte) {
		// A parse failure leaves certs nil, which is the shape
		// VerifySignatures itself handles at its len(certs) == 0 check.
		certs, _ := parsePKCS7Certificates(der)
		_, _ = verifyCMSSignature(der, content, certs)
	})
}

// FuzzVerifyCMSSignatureContent fuzzes the CONTENT half against a signature
// that actually parses, and exists because the target above cannot reach the
// verifier's body.
//
// Measured: 5.7M executions of FuzzVerifyCMSSignature covered 4 of 25 blocks:
// the two asn1.Unmarshal calls and their two error returns. h.Write(content)
// never ran once. Random mutation destroys ASN.1 framing long before it can
// produce a SignedData carrying SignerInfos, so the signer lookup, the
// messageDigest comparison and CheckSignature stayed dark.
//
// The seeds could not have helped: cmsSeeds builds with buildTestPKCS7, whose
// SignedData has no SignerInfos field at all. It is a certificate carrier
// written for parsePKCS7Certificates, and reusing it here was the mistake.
//
// So hold the signature fixed and valid and fuzz the byte range it covers.
// That is also the realistic attack: forging a signature is hard, editing the
// bytes it is supposed to cover is not.
func FuzzVerifyCMSSignatureContent(f *testing.F) {
	// Built once. Generating a P-256 key per execution would cost far more
	// than the verification under test.
	key, cert := testSigningCert(f)
	const signed = "the document bytes covered by the signature ByteRange"
	der := buildSignedCMS(f, key, cert, []byte(signed))
	// The unit tests pass the signer explicitly and are proven to reach the
	// deep paths; recovering it from the blob instead would silently strand
	// this target at the "signer certificate not found" return.
	certs := []*x509.Certificate{cert}

	f.Add([]byte(signed))                  // matches: reaches CheckSignature and the success return
	f.Add([]byte(signed[:len(signed)/2]))  // a truncated byte range
	f.Add([]byte(signed + "!"))            // one byte too many
	f.Add([]byte("tampered content!!!!!")) // the unit test's tampered case
	f.Add([]byte{})
	f.Add(make([]byte, 4096))

	f.Fuzz(func(_ *testing.T, content []byte) {
		if len(content) > 1<<20 {
			return
		}
		_, _ = verifyCMSSignature(der, content, certs)
	})
}

// TestVerifyCMSContentSeedReachesTheDigestCheck pins that the fuzzed seed
// actually enters the verifier rather than stopping at a parse or lookup
// error. Without this, a change to buildSignedCMS could strand the target at
// an early return and it would still report a clean run.
func TestVerifyCMSContentSeedReachesTheDigestCheck(t *testing.T) {
	t.Parallel()

	key, cert := testSigningCert(t)
	content := []byte("the document bytes covered by the signature ByteRange")
	der := buildSignedCMS(t, key, cert, content)
	certs := []*x509.Certificate{cert}

	if _, err := verifyCMSSignature(der, content, certs); err != nil {
		t.Fatalf("the matching seed must verify, else the target never reaches CheckSignature: %v", err)
	}
	// A different byte range must fail at the messageDigest comparison, which
	// is the block the der-fuzzing target could never reach.
	if _, err := verifyCMSSignature(der, []byte("different bytes entirely"), certs); err == nil {
		t.Fatal("a mismatched byte range verified: the digest comparison was not reached")
	}
}
