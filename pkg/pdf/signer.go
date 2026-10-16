package pdf

import (
	"bytes"
	"crypto"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gotenberg/litho/pkg/internal/mutations"
	"github.com/gotenberg/litho/pkg/internal/pkcs12"
	"github.com/gotenberg/litho/pkg/internal/signature"
)

// builtinSigner is the [Signer] [NewSigner] returns. It is safe for concurrent
// use when its key is.
type builtinSigner struct {
	cms *signature.CMSSigner
}

// NewSigner returns a [Signer] that signs with key as the subject of cert and
// writes a detached CMS SignedData (RFC 5652), the adbe.pkcs7.detached
// signature of ISO 32000-1, 12.8.3.3: SHA-256, with the content-type,
// signing-time, message-digest and ESS signing-certificate-v2 (RFC 5035)
// signed attributes, and cert followed by chain in its certificates. chain is
// the intermediate certificates a verifier needs to reach a trusted root, in
// any order; duplicates and cert itself are dropped. [WithPAdES] makes it the
// CAdES signature of a PAdES baseline signature instead.
//
// key is an RSA key, signed with PKCS #1 v1.5, or an ECDSA key on P-256,
// P-384 or P-521. It is a [crypto.Signer], so a key held in an HSM or a cloud
// key service works when its client implements that interface. For any other
// scheme implement [Signer] directly.
//
// The signature carries an RFC 3161 timestamp only with
// [WithTimestampAuthority], which reaches the authority over the network.
//
// It returns [ErrSignerKeyUnsupported] for another key type,
// [ErrSignerKeyMismatch] when key does not match cert, and
// [ErrSignerCertificateUsage] when cert's key usage excludes signing. The
// Signer is safe for concurrent use when key is.
func NewSigner(cert *x509.Certificate, key crypto.Signer, chain ...*x509.Certificate) (Signer, error) {
	cms, err := signature.NewCMSSigner(cert, key, chain)
	if err != nil {
		return nil, fmt.Errorf("building the signer: %w", err)
	}
	return &builtinSigner{cms: cms}, nil
}

// ParseSignerPEM returns the [Signer] of [NewSigner] from PEM data: certPEM
// holds the signing certificate and, optionally, its chain, and keyPEM the
// private key, in PKCS #8 (PRIVATE KEY), PKCS #1 (RSA PRIVATE KEY) or SEC 1
// (EC PRIVATE KEY) form. The certificate whose public key matches the key
// signs, and the others form its chain. Blocks of other types are skipped, so
// one file holding the key and the certificates may be passed as both.
//
// An encrypted private key is refused with [ErrSignerKeyEncrypted]: decrypt
// it first (for instance with openssl pkey), or parse it yourself and call
// [NewSigner]. Input with no certificate, no key or more than one key, or a
// block that does not parse, returns [ErrSignerPEM]. It returns the errors of
// [NewSigner] too.
func ParseSignerPEM(certPEM, keyPEM []byte) (Signer, error) {
	var certs []*x509.Certificate
	for block, rest := pem.Decode(certPEM); block != nil; block, rest = pem.Decode(rest) {
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("%w: certificate %d: %w", ErrSignerPEM, len(certs)+1, err)
		}
		certs = append(certs, cert)
	}
	if len(certs) == 0 {
		return nil, fmt.Errorf("%w: no CERTIFICATE block", ErrSignerPEM)
	}

	key, err := parsePEMPrivateKey(keyPEM)
	if err != nil {
		return nil, err
	}
	return signerOfKey(key, certs)
}

// ParseSignerPKCS12 returns the [Signer] of [NewSigner] from a PKCS #12 key
// store (RFC 7292) under password: the .p12 or .pfx file that certificate
// authorities, browsers and operating systems export. The store holds one
// private key. The certificate whose public key matches it signs, and the
// store's other certificates form its chain.
//
// The password verifies the store's MAC (HMAC over SHA-1 or SHA-2, keyed by
// the derivation of RFC 7292, Appendix B) and decrypts its content: PBES2
// (PBKDF2 with AES-CBC or DES-EDE3-CBC, RFC 8018), which current exporters
// write, or the legacy triple DES and 40- or 128-bit RC2 schemes of RFC
// 7292, Appendix C, which older ones write. A store in public-key privacy
// or integrity mode is not read.
//
// It returns [ErrSignerPKCS12Password] when the password does not fit, and
// [ErrSignerPKCS12] for a store that does not parse, uses another algorithm,
// states an iteration count over 2^21, exceeds 1 MiB, or holds no private
// key, more than one, or no certificate. It returns [ErrSignerKeyMismatch]
// when no certificate is the key's, and the errors of [NewSigner].
func ParseSignerPKCS12(data []byte, password string) (_ Signer, err error) {
	defer recoverToError(nil, "ParseSignerPKCS12", &err)
	contents, err := pkcs12.Decode(data, password)
	if errors.Is(err, pkcs12.ErrIncorrectPassword) {
		return nil, fmt.Errorf("%w: %w", ErrSignerPKCS12Password, err)
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSignerPKCS12, err)
	}
	if n := len(contents.Keys); n != 1 {
		return nil, fmt.Errorf("%w: %d private keys, want one", ErrSignerPKCS12, n)
	}
	if len(contents.Certificates) == 0 {
		return nil, fmt.Errorf("%w: no certificate", ErrSignerPKCS12)
	}
	key, ok := contents.Keys[0].(crypto.Signer)
	if !ok {
		return nil, fmt.Errorf("%w: %T", ErrSignerKeyUnsupported, contents.Keys[0])
	}
	return signerOfKey(key, contents.Certificates)
}

// signerOfKey returns the [Signer] of [NewSigner] for key, signing as the
// certificate of certs whose public key is key's, the others its chain.
func signerOfKey(key crypto.Signer, certs []*x509.Certificate) (Signer, error) {
	leaf := -1
	for i, c := range certs {
		if pub, ok := key.Public().(interface{ Equal(crypto.PublicKey) bool }); ok && pub.Equal(c.PublicKey) {
			leaf = i
			break
		}
	}
	if leaf < 0 {
		return nil, fmt.Errorf("%w: none of the %d certificates is the key's", ErrSignerKeyMismatch, len(certs))
	}
	chain := append(append([]*x509.Certificate(nil), certs[:leaf]...), certs[leaf+1:]...)
	return NewSigner(certs[leaf], key, chain...)
}

// parsePEMPrivateKey returns the one private key of data (see
// [ParseSignerPEM]).
func parsePEMPrivateKey(data []byte) (crypto.Signer, error) {
	var key crypto.Signer
	for block, rest := pem.Decode(data); block != nil; block, rest = pem.Decode(rest) {
		if block.Type == "ENCRYPTED PRIVATE KEY" || strings.Contains(block.Headers["Proc-Type"], "ENCRYPTED") {
			return nil, fmt.Errorf("%w: %s block", ErrSignerKeyEncrypted, block.Type)
		}
		var parsed any
		var err error
		switch block.Type {
		case "PRIVATE KEY":
			parsed, err = x509.ParsePKCS8PrivateKey(block.Bytes)
		case "RSA PRIVATE KEY":
			parsed, err = x509.ParsePKCS1PrivateKey(block.Bytes)
		case "EC PRIVATE KEY":
			parsed, err = x509.ParseECPrivateKey(block.Bytes)
		default:
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("%w: %s: %w", ErrSignerPEM, block.Type, err)
		}
		if key != nil {
			return nil, fmt.Errorf("%w: more than one private key", ErrSignerPEM)
		}
		signer, ok := parsed.(crypto.Signer)
		if !ok {
			return nil, fmt.Errorf("%w: %T", ErrSignerKeyUnsupported, parsed)
		}
		key = signer
	}
	if key == nil {
		if bytes.Contains(data, []byte("-----BEGIN")) {
			return nil, fmt.Errorf("%w: no PRIVATE KEY, RSA PRIVATE KEY or EC PRIVATE KEY block", ErrSignerPEM)
		}
		return nil, fmt.Errorf("%w: no PEM block", ErrSignerPEM)
	}
	return key, nil
}

// Sign returns the SignedData over digest, signed now.
func (s *builtinSigner) Sign(digest []byte) ([]byte, error) {
	return s.signAt(digest, time.Now(), false)
}

// signAt returns the SignedData over digest with at as its signing-time, the
// instant the signature dictionary's /M records, or its CAdES form without
// one (see [WithPAdES]).
func (s *builtinSigner) signAt(digest []byte, at time.Time, cades bool) ([]byte, error) {
	der, err := s.cms.Sign(digest, at, cades)
	if err != nil {
		return nil, fmt.Errorf("building the CMS signature: %w", err)
	}
	return der, nil
}

// Certificate returns the signing certificate.
func (s *builtinSigner) Certificate() *x509.Certificate { return s.cms.Certificate() }

// EstimatedSize returns an upper bound on the bytes of the SignedData.
func (s *builtinSigner) EstimatedSize() int { return s.cms.EstimatedSize() }

// SignOption sets an entry of the signature dictionary [Writer.Sign] writes
// (ISO 32000-1, Table 252) or the field the signature goes in.
type SignOption func(*signOpts)

// signOpts holds what the [SignOption] values of [Writer.Sign] set.
type signOpts struct {
	info mutations.SignatureInfo
	// ltv asks for the signature's validation data in the document security
	// store, and validation, when set, is where it comes from (see
	// [WithLTV] and [WithValidationData]).
	ltv        bool
	validation *ValidationData
}

// WithSignatureReason sets the /Reason for signing, such as "I approve this
// document".
func WithSignatureReason(reason string) SignOption {
	return func(o *signOpts) { o.info.Reason = reason }
}

// WithSignatureLocation sets the /Location of the signing, such as a city.
func WithSignatureLocation(location string) SignOption {
	return func(o *signOpts) { o.info.Location = location }
}

// WithSignatureContactInfo sets the /ContactInfo a verifier may use to reach
// the signer, such as an email address.
func WithSignatureContactInfo(contact string) SignOption {
	return func(o *signOpts) { o.info.ContactInfo = contact }
}

// WithSignerName sets the /Name of the signer. ISO 32000-1, Table 252 has a
// verifier prefer the name in the signing certificate, so set it when the
// certificate names no person, or names one differently.
func WithSignerName(name string) SignOption {
	return func(o *signOpts) { o.info.Name = name }
}

// WithSigningTime sets the /M signing time, and the signing-time attribute of
// the SignedData when the [Signer] is the one of [NewSigner] and [WithPAdES],
// which writes none, is not given. Without it both record the time of
// [Writer.WriteTo]. A fixed time, with a deterministic
// signature scheme such as RSA PKCS #1 v1.5, makes the signed output
// reproducible byte for byte. The time is the signer's claim, not a trusted
// timestamp.
func WithSigningTime(t time.Time) SignOption {
	return func(o *signOpts) { o.info.Time = t }
}

// WithPAdES writes a PAdES baseline signature (ETSI EN 319 142-1, level B-B)
// instead of the adbe.pkcs7.detached one of ISO 32000-1, 12.8.3.3: the
// signature dictionary's /SubFilter is ETSI.CAdES.detached (ISO 32000-2,
// 12.8.3.4), and its SignedData carries the content-type, message-digest and
// ESS signing-certificate-v2 signed attributes and no signing-time, whose
// time ETSI EN 319 142-1, 6.3, Table 1 moves to the /M entry, always written.
// A document older than PDF 2.0 declares the ESIC extension of level 2 in its
// catalog (ETSI EN 319 142-1, 5.6), unless it declares one already. The signer
// of [NewSigner] builds that SignedData. The SignedData of another [Signer]
// is checked, and [Writer.WriteTo] returns [ErrSignatureNotPAdES] when it
// does not qualify.
//
// With [WithTimestampAuthority] the signature reaches level B-T, and with
// [WithLTV] as well, B-LT.
func WithPAdES() SignOption {
	return func(o *signOpts) { o.info.SubFilter = mutations.SubFilterCAdESDetached }
}

// WithTimestampAuthority timestamps the signature with the RFC 3161
// timestamp authority at url, an http or https URL, which then vouches for
// when the signature existed (PAdES baseline B-T, ETSI EN 319 142-1, 6.3).
// [Writer.WriteTo] sends the authority the SHA-256 of the SignedData's
// signature value with a random nonce, checks the token it answers (a granted
// status, the nonce, the imprint, and a signature by a certificate restricted
// to timestamping, RFC 3161, 2.3 and 2.4.2), and adds it to the SignerInfo's
// unsigned attributes as the signature-time-stamp attribute (RFC 3161,
// Appendix A). It works with any [Signer] returning a DER SignedData.
//
// Each exchange is bounded by ctx and by a one-minute timeout, and the answer
// by a size cap. An authority that cannot be reached, refuses, or answers
// with a token that does not verify fails the write with
// [ErrTimestampAuthority], and nothing is written. The token carries the
// authority's time, so the output differs from one write to the next.
func WithTimestampAuthority(url string) SignOption {
	return func(o *signOpts) { o.info.TimestampURL = url }
}

// WithSignatureField names the signature field. An unsigned signature field
// of that fully qualified name is signed in place, so its widget and the
// appearance it has on the page stay. Otherwise a new root field takes the
// name, with the invisible widget, on no page and of zero size, that signing
// adds by default. Without it,
// the signature goes in a new field named Signature1, Signature2 or the first
// such name no field holds. [Writer.WriteTo] returns
// [ErrSignatureFieldUnavailable] for a name the signature cannot take.
//
// A field signed in place is honoured as ISO 32000-1, 12.7.4.5 describes. Its
// /Lock (Table 233) turns the fields it names read-only in the signed
// revision and puts a FieldMDP transform naming them in the signature
// dictionary (12.8.2.4). Its seed values (/SV, Tables 234 and 235) choose the
// subfilter, the reason and the timestamp authority the caller left unset,
// and a requirement the signature cannot meet fails the write with
// [ErrSeedValueUnmet].
func WithSignatureField(name string) SignOption {
	return func(o *signOpts) { o.info.Field = name }
}
