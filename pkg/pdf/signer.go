package pdf

import (
	"bytes"
	"crypto"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"strings"
	"time"

	"github.com/gotenberg/litho/pkg/internal/mutations"
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
// any order; duplicates and cert itself are dropped.
//
// key is an RSA key, signed with PKCS #1 v1.5, or an ECDSA key on P-256,
// P-384 or P-521. It is a [crypto.Signer], so a key held in an HSM or a cloud
// key service works when its client implements that interface. For any other
// scheme implement [Signer] directly.
//
// The signature carries no RFC 3161 timestamp, which needs a timestamp
// authority reached over the network, and so no long-term validation data.
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
	return s.signAt(digest, time.Now())
}

// signAt returns the SignedData over digest with at as its signing-time, the
// instant the signature dictionary's /M records.
func (s *builtinSigner) signAt(digest []byte, at time.Time) ([]byte, error) {
	der, err := s.cms.Sign(digest, at)
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
// the SignedData when the [Signer] is the one of [NewSigner]. Without it both
// record the time of [Writer.WriteTo]. A fixed time, with a deterministic
// signature scheme such as RSA PKCS #1 v1.5, makes the signed output
// reproducible byte for byte. The time is the signer's claim, not a trusted
// timestamp.
func WithSigningTime(t time.Time) SignOption {
	return func(o *signOpts) { o.info.Time = t }
}

// WithSignatureField names the signature field. An unsigned signature field
// of that fully qualified name is signed in place, so its widget and the
// appearance it has on the page stay. Otherwise a new root field takes the
// name, with the invisible widget, on no page and of zero size, that signing
// adds by default. Without it,
// the signature goes in a new field named Signature1, Signature2 or the first
// such name no field holds. [Writer.WriteTo] returns
// [ErrSignatureFieldUnavailable] for a name the signature cannot take.
func WithSignatureField(name string) SignOption {
	return func(o *signOpts) { o.info.Field = name }
}
