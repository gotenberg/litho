package pkcs12

import (
	"crypto/hmac"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"errors"
	"fmt"
)

// MaxSize caps the bytes [Decode] reads. Anti-DoS guard, not spec-mandated:
// a store holds a key and a certificate chain, a few kilobytes; 1 MiB leaves
// room for a chain of hundreds of certificates while bounding what the
// parser allocates.
const MaxSize = 1 << 20

// maxSafeContentsDepth caps the nesting of SafeContentsBag values, a bag
// holding another SafeContents (RFC 7292, 4.2.6). Anti-DoS guard, not
// spec-mandated: exporters write none, and a crafted store could otherwise
// nest them until the stack gives out.
const maxSafeContentsDepth = 8

// Content types (RFC 5652, 4 and 8) and bag types (RFC 7292, 4.2).
var (
	oidData                = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 1}
	oidEncryptedData       = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 6}
	oidKeyBag              = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 12, 10, 1, 1}
	oidPKCS8ShroudedKeyBag = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 12, 10, 1, 2}
	oidCertBag             = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 12, 10, 1, 3}
	oidSafeContentsBag     = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 12, 10, 1, 6}
	oidX509Certificate     = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 22, 1}
)

// Contents is what a PKCS #12 store holds that [Decode] reads: its private
// keys, parsed as [x509.ParsePKCS8PrivateKey] returns them, and its X.509
// certificates, each in the order the store lists them.
type Contents struct {
	Keys         []any
	Certificates []*x509.Certificate
}

// pfx is PFX of RFC 7292, 4.
type pfx struct {
	Version  int
	AuthSafe contentInfo
	MacData  macData `asn1:"optional"`
}

// contentInfo is ContentInfo of RFC 5652, 3.
type contentInfo struct {
	ContentType asn1.ObjectIdentifier
	Content     asn1.RawValue `asn1:"tag:0,explicit,optional"`
}

// macData is MacData of RFC 7292, 4.
type macData struct {
	Mac        digestInfo
	MacSalt    []byte
	Iterations int `asn1:"optional,default:1"`
}

// digestInfo is DigestInfo of RFC 8017, 9.2.
type digestInfo struct {
	Algorithm pkix.AlgorithmIdentifier
	Digest    []byte
}

// encryptedData is EncryptedData of RFC 5652, 8, with its
// EncryptedContentInfo (RFC 5652, 6.1).
type encryptedData struct {
	Version int
	Info    struct {
		ContentType asn1.ObjectIdentifier
		Algorithm   pkix.AlgorithmIdentifier
		// Content is the [0] IMPLICIT OCTET STRING encryptedContent, read
		// raw since BER may split it into chunks.
		Content asn1.RawValue `asn1:"optional"`
	}
}

// safeBag is SafeBag of RFC 7292, 4.2. Its attributes (friendlyName,
// localKeyId) are not read: a key finds its certificate by public key.
type safeBag struct {
	ID         asn1.ObjectIdentifier
	Value      asn1.RawValue `asn1:"tag:0,explicit"`
	Attributes asn1.RawValue `asn1:"optional"`
}

// certBag is CertBag of RFC 7292, 4.2.3.
type certBag struct {
	ID    asn1.ObjectIdentifier
	Value asn1.RawValue `asn1:"tag:0,explicit"`
}

// encryptedPrivateKeyInfo is EncryptedPrivateKeyInfo of RFC 5958, 3.
type encryptedPrivateKeyInfo struct {
	Algorithm pkix.AlgorithmIdentifier
	Data      []byte
}

// Decode returns the private keys and certificates of the PKCS #12 store
// data (RFC 7292) under password. The password verifies the store's MAC
// when it has one, and decrypts its encrypted SafeContents and shrouded keys.
// An empty password is tried both as the empty BMPString RFC 7292 defines and
// as no bytes at all, which some exporters write.
//
// It returns [ErrIncorrectPassword] when the password does not fit,
// [ErrUnsupported] for an algorithm or mode it does not read, [ErrLimit] for
// input past [MaxSize] or a cap, and [ErrMalformed] for anything else that
// does not parse. CRL, secret and unknown bags are skipped.
func Decode(data []byte, password string) (Contents, error) {
	if len(data) > MaxSize {
		return Contents{}, fmt.Errorf("%w: %d bytes over %d", ErrLimit, len(data), MaxSize)
	}
	der, err := toDER(data)
	if err != nil {
		return Contents{}, err
	}
	var p pfx
	if err := unmarshalExact(der, &p); err != nil {
		return Contents{}, fmt.Errorf("PFX: %w", err)
	}
	// RFC 7292, 4: version v3(3), the only one defined.
	if p.Version != 3 {
		return Contents{}, fmt.Errorf("%w: PFX version %d", ErrUnsupported, p.Version)
	}
	// RFC 7292, 4: an authSafe of type signedData is public-key integrity
	// mode, which exporters do not write.
	if !p.AuthSafe.ContentType.Equal(oidData) {
		return Contents{}, fmt.Errorf("%w: authSafe content type %s", ErrUnsupported, p.AuthSafe.ContentType)
	}
	authSafe, err := explicitOctets(p.AuthSafe.Content)
	if err != nil {
		return Contents{}, err
	}

	b := &budget{left: maxDerivations}
	candidates := []secret{{bmp: bmpPassword(password), raw: password}}
	if password == "" {
		candidates = append(candidates, secret{raw: password})
	}
	hasMAC := len(p.MacData.Mac.Algorithm.Algorithm) != 0
	if hasMAC {
		pw, err := verifyMAC(p.MacData, authSafe, candidates, b)
		if err != nil {
			return Contents{}, err
		}
		return readAuthSafe(authSafe, pw, b)
	}
	// Without a MAC only decryption tells a password apart.
	var firstErr error
	for _, pw := range candidates {
		c, err := readAuthSafe(authSafe, pw, b)
		if err == nil {
			return c, nil
		}
		if firstErr == nil {
			firstErr = err
		}
		if !errors.Is(err, ErrIncorrectPassword) {
			break
		}
	}
	return Contents{}, firstErr
}

// verifyMAC checks the password-integrity MAC of RFC 7292, 5 over authSafe
// and returns the password encoding it verifies with.
func verifyMAC(m macData, authSafe []byte, candidates []secret, b *budget) (secret, error) {
	h, err := digestOf(m.Mac.Algorithm.Algorithm)
	if err != nil {
		return secret{}, fmt.Errorf("MAC: %w", err)
	}
	if len(m.Mac.Digest) != h.size {
		return secret{}, fmt.Errorf("%w: %s MAC of %d bytes", ErrMalformed, h.name, len(m.Mac.Digest))
	}
	for _, pw := range candidates {
		if err := b.spend(m.Iterations); err != nil {
			return secret{}, fmt.Errorf("MAC: %w", err)
		}
		mac := macKey(h, pw.bmp, m.MacSalt, m.Iterations)
		mac.Write(authSafe)
		if hmac.Equal(mac.Sum(nil), m.Mac.Digest) {
			return pw, nil
		}
	}
	return secret{}, fmt.Errorf("%w: the MAC does not verify", ErrIncorrectPassword)
}

// readAuthSafe reads the AuthenticatedSafe of RFC 7292, 4.1: a sequence of
// ContentInfo, each plain (data) or password-encrypted (encryptedData)
// SafeContents.
func readAuthSafe(der []byte, pw secret, b *budget) (Contents, error) {
	der, err := toDER(der)
	if err != nil {
		return Contents{}, fmt.Errorf("AuthenticatedSafe: %w", err)
	}
	var infos []contentInfo
	if err := unmarshalExact(der, &infos); err != nil {
		return Contents{}, fmt.Errorf("AuthenticatedSafe: %w", err)
	}
	var c Contents
	for i, info := range infos {
		var safe []byte
		switch {
		case info.ContentType.Equal(oidData):
			safe, err = explicitOctets(info.Content)
		case info.ContentType.Equal(oidEncryptedData):
			safe, err = decryptData(info.Content.Bytes, pw, b)
		default:
			// RFC 7292, 4.1: envelopedData is public-key privacy mode.
			err = fmt.Errorf("%w: content type %s", ErrUnsupported, info.ContentType)
		}
		if err != nil {
			return Contents{}, fmt.Errorf("AuthenticatedSafe entry %d: %w", i+1, err)
		}
		if err := readSafeContents(safe, pw, b, &c, 0); err != nil {
			return Contents{}, fmt.Errorf("AuthenticatedSafe entry %d: %w", i+1, err)
		}
	}
	return c, nil
}

// decryptData returns the plaintext of an EncryptedData content (RFC 5652,
// 8).
func decryptData(der []byte, pw secret, b *budget) ([]byte, error) {
	var ed encryptedData
	if err := unmarshalExact(der, &ed); err != nil {
		return nil, fmt.Errorf("EncryptedData: %w", err)
	}
	ct := ed.Info.Content
	// RFC 5652, 6.1: encryptedContent [0] IMPLICIT, absent when the
	// content is carried elsewhere, which a PKCS #12 store has no place for.
	if ct.Class != asn1.ClassContextSpecific || ct.Tag != 0 {
		return nil, fmt.Errorf("%w: EncryptedData without its encrypted content", ErrMalformed)
	}
	ciphertext, err := octets(ct)
	if err != nil {
		return nil, err
	}
	plain, err := decrypt(ed.Info.Algorithm, ciphertext, pw, b)
	if err != nil {
		return nil, fmt.Errorf("EncryptedData: %w", err)
	}
	return plain, nil
}

// readSafeContents appends the keys and certificates of a SafeContents (RFC
// 7292, 4.2) to c.
func readSafeContents(data []byte, pw secret, b *budget, c *Contents, depth int) error {
	if depth > maxSafeContentsDepth {
		return fmt.Errorf("%w: SafeContents nest deeper than %d", ErrLimit, maxSafeContentsDepth)
	}
	der, err := toDER(data)
	if err != nil {
		return fmt.Errorf("SafeContents: %w", err)
	}
	var bags []safeBag
	if err := unmarshalExact(der, &bags); err != nil {
		return fmt.Errorf("SafeContents: %w", err)
	}
	for i, bag := range bags {
		if err := readBag(bag, pw, b, c, depth); err != nil {
			return fmt.Errorf("bag %d: %w", i+1, err)
		}
	}
	return nil
}

// readBag appends what one SafeBag holds to c.
func readBag(bag safeBag, pw secret, b *budget, c *Contents, depth int) error {
	switch {
	case bag.ID.Equal(oidKeyBag):
		// RFC 7292, 4.2.1: a PrivateKeyInfo in the clear.
		return appendKey(c, bag.Value.Bytes)
	case bag.ID.Equal(oidPKCS8ShroudedKeyBag):
		// RFC 7292, 4.2.2: an EncryptedPrivateKeyInfo.
		var epki encryptedPrivateKeyInfo
		if err := unmarshalExact(bag.Value.Bytes, &epki); err != nil {
			return fmt.Errorf("shrouded key: %w", err)
		}
		plain, err := decrypt(epki.Algorithm, epki.Data, pw, b)
		if err != nil {
			return fmt.Errorf("shrouded key: %w", err)
		}
		return appendKey(c, plain)
	case bag.ID.Equal(oidCertBag):
		var cb certBag
		if err := unmarshalExact(bag.Value.Bytes, &cb); err != nil {
			return fmt.Errorf("certificate bag: %w", err)
		}
		// RFC 7292, 4.2.3: an SDSI certificate is the other type, which
		// signing has no use for.
		if !cb.ID.Equal(oidX509Certificate) {
			return nil
		}
		der, err := explicitOctets(cb.Value)
		if err != nil {
			return fmt.Errorf("certificate bag: %w", err)
		}
		cert, err := x509.ParseCertificate(der)
		if err != nil {
			return fmt.Errorf("%w: certificate: %w", ErrMalformed, err)
		}
		c.Certificates = append(c.Certificates, cert)
		return nil
	case bag.ID.Equal(oidSafeContentsBag):
		return readSafeContents(bag.Value.Bytes, pw, b, c, depth+1)
	}
	// crlBag, secretBag and private bag types carry nothing a signer takes.
	return nil
}

// appendKey parses a PKCS #8 PrivateKeyInfo (RFC 5958, 2) and appends it to
// c.
func appendKey(c *Contents, der []byte) error {
	key, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return fmt.Errorf("%w: private key: %w", ErrMalformed, err)
	}
	c.Keys = append(c.Keys, key)
	return nil
}

// explicitOctets returns the string an [0] EXPLICIT field wraps (see
// [octets]). [asn1.Unmarshal] leaves such a field's RawValue on the tag, its
// Bytes the wrapped value.
func explicitOctets(v asn1.RawValue) ([]byte, error) {
	var inner asn1.RawValue
	if err := unmarshalExact(v.Bytes, &inner); err != nil {
		return nil, err
	}
	return octets(inner)
}

// unmarshalExact parses der, which must hold exactly one value, into v.
func unmarshalExact(der []byte, v any) error {
	rest, err := asn1.Unmarshal(der, v)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrMalformed, err)
	}
	if len(rest) != 0 {
		return fmt.Errorf("%w: %d bytes after the value", ErrMalformed, len(rest))
	}
	return nil
}
