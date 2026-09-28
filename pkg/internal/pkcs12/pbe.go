package pkcs12

import (
	"crypto/aes"
	"crypto/des" //nolint:gosec // RFC 7292, Appendix C and RFC 8018, B.2.2 name DES-EDE3 for the legacy and PBES2 schemes old stores use; decryption only.
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/x509/pkix"
	"encoding/asn1"
	"fmt"
	"hash"
)

// maxIterations caps the iteration count of one key derivation, PBKDF2 and
// the RFC 7292 derivation alike, since a store states its own count and an
// attacker's store can state billions. Anti-DoS guard, not spec-mandated:
// RFC 8018, 4.2 asks for at least 1000, OpenSSL writes 2048, and OWASP's
// 2023 guidance for PBKDF2-HMAC-SHA256 is 600000; 2^21 stays above all of
// them and bounds one derivation to about half a second.
const maxIterations = 1 << 21

// maxDerivations caps the password-based key derivations one [Decode] runs:
// the MAC and each encrypted SafeContents and shrouded key, over every
// password encoding it tries. Anti-DoS guard, not spec-mandated: an exported
// store needs three (MAC, certificates, key), four when an empty password is
// tried both ways. One derivation at maxIterations takes about half a second
// at worst (PBKDF2-HMAC-SHA-1 for a 32-byte key, measured on an Apple M-series
// laptop), so this bounds what a crafted store can demand to about four
// seconds.
const maxDerivations = 8

// Password-based encryption scheme identifiers: PBES2 (RFC 8018, A.4) and
// the PKCS #12 schemes (RFC 7292, Appendix C).
var (
	oidPBES2                  = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 5, 13}
	oidPBKDF2                 = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 5, 12}
	oidPBEWithSHAAnd3KeyDES   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 12, 1, 3}
	oidPBEWithSHAAnd2KeyDES   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 12, 1, 4}
	oidPBEWithSHAAnd128BitRC2 = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 12, 1, 5}
	oidPBEWithSHAAnd40BitRC2  = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 12, 1, 6}
)

// PBKDF2 pseudorandom functions (RFC 8018, B.1) and PBES2 encryption schemes
// (RFC 8018, B.2; RFC 3565, 4.1 for AES).
var (
	oidHMACWithSHA1   = asn1.ObjectIdentifier{1, 2, 840, 113549, 2, 7}
	oidHMACWithSHA224 = asn1.ObjectIdentifier{1, 2, 840, 113549, 2, 8}
	oidHMACWithSHA256 = asn1.ObjectIdentifier{1, 2, 840, 113549, 2, 9}
	oidHMACWithSHA384 = asn1.ObjectIdentifier{1, 2, 840, 113549, 2, 10}
	oidHMACWithSHA512 = asn1.ObjectIdentifier{1, 2, 840, 113549, 2, 11}
	oidAES128CBC      = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 1, 2}
	oidAES192CBC      = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 1, 22}
	oidAES256CBC      = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 1, 42}
	oidDESEDE3CBC     = asn1.ObjectIdentifier{1, 2, 840, 113549, 3, 7}
)

// blockDecrypter is the half of a block cipher CBC decryption needs, which
// lets the decrypt-only RC2 stand beside the standard library's ciphers.
type blockDecrypter interface {
	BlockSize() int
	Decrypt(dst, src []byte)
}

// secret is the password in the two encodings the schemes derive keys from.
type secret struct {
	// bmp is the BMPString of RFC 7292, Appendix B.1, for the PKCS #12
	// derivation.
	bmp []byte
	// raw is the password as given, UTF-8, for PBKDF2: RFC 8018 takes an
	// octet string, and OpenSSL and every PBES2 store it reads pass UTF-8.
	raw string
}

// budget counts the key derivations of one [Decode] against maxDerivations.
type budget struct {
	left int
}

// spend takes one derivation of iterations from b.
func (b *budget) spend(iterations int) error {
	if iterations < 1 {
		return fmt.Errorf("%w: iteration count %d", ErrMalformed, iterations)
	}
	if iterations > maxIterations {
		return fmt.Errorf("%w: iteration count %d over %d", ErrLimit, iterations, maxIterations)
	}
	if b.left <= 0 {
		return fmt.Errorf("%w: more than %d key derivations", ErrLimit, maxDerivations)
	}
	b.left--
	return nil
}

// pkcs12PBEParams is pkcs-12PbeParams of RFC 7292, Appendix C.
type pkcs12PBEParams struct {
	Salt       []byte
	Iterations int
}

// pbes2Params is PBES2-params of RFC 8018, A.4.
type pbes2Params struct {
	KeyDerivationFunc pkix.AlgorithmIdentifier
	EncryptionScheme  pkix.AlgorithmIdentifier
}

// pbkdf2Params is PBKDF2-params of RFC 8018, A.2. The salt is its specified
// OCTET STRING choice; otherSource has no registered use.
type pbkdf2Params struct {
	Salt           asn1.RawValue
	IterationCount int
	KeyLength      int                      `asn1:"optional"`
	PRF            pkix.AlgorithmIdentifier `asn1:"optional"`
}

// decrypt returns the plaintext of ciphertext under the password-based
// scheme alg.
func decrypt(alg pkix.AlgorithmIdentifier, ciphertext []byte, pw secret, b *budget) ([]byte, error) {
	block, iv, err := schemeCipher(alg, pw, b)
	if err != nil {
		return nil, err
	}
	return cbcDecrypt(block, iv, ciphertext)
}

// schemeCipher returns the cipher and IV of the password-based scheme alg.
func schemeCipher(alg pkix.AlgorithmIdentifier, pw secret, b *budget) (blockDecrypter, []byte, error) {
	if alg.Algorithm.Equal(oidPBES2) {
		return pbes2Cipher(alg.Parameters.FullBytes, pw, b)
	}
	// RFC 7292, Appendix C: key and IV lengths and effective RC2 bits of
	// each PKCS #12 scheme.
	var keyLen, rc2Bits int
	switch {
	case alg.Algorithm.Equal(oidPBEWithSHAAnd3KeyDES):
		keyLen = 24
	case alg.Algorithm.Equal(oidPBEWithSHAAnd2KeyDES):
		keyLen = 16
	case alg.Algorithm.Equal(oidPBEWithSHAAnd128BitRC2):
		keyLen, rc2Bits = 16, 128
	case alg.Algorithm.Equal(oidPBEWithSHAAnd40BitRC2):
		keyLen, rc2Bits = 5, 40
	default:
		return nil, nil, fmt.Errorf("%w: encryption algorithm %s", ErrUnsupported, alg.Algorithm)
	}
	var params pkcs12PBEParams
	if err := unmarshalExact(alg.Parameters.FullBytes, &params); err != nil {
		return nil, nil, fmt.Errorf("PBE parameters: %w", err)
	}
	if err := b.spend(params.Iterations); err != nil {
		return nil, nil, err
	}
	// The key and IV are two derivations of RFC 7292, Appendix B, over
	// SHA-1 (Appendix C), counted as one against the budget since the
	// caller sees one scheme.
	key := deriveKey(sha1Digest, kdfKey, pw.bmp, params.Salt, params.Iterations, keyLen)
	iv := deriveKey(sha1Digest, kdfIV, pw.bmp, params.Salt, params.Iterations, 8)
	if rc2Bits != 0 {
		c, err := newRC2Decrypter(key, rc2Bits)
		if err != nil {
			return nil, nil, err
		}
		return c, iv, nil
	}
	if keyLen == 16 {
		// Two-key triple DES: K1, K2, K1.
		key = append(key, key[:8]...)
	}
	c, err := des.NewTripleDESCipher(key) //nolint:gosec // RFC 7292, Appendix C: the legacy scheme is triple DES; decryption only.
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrMalformed, err)
	}
	return c, iv, nil
}

// pbes2Cipher returns the cipher and IV of PBES2 parameters (RFC 8018, 6.2).
func pbes2Cipher(der []byte, pw secret, b *budget) (blockDecrypter, []byte, error) {
	var params pbes2Params
	if err := unmarshalExact(der, &params); err != nil {
		return nil, nil, fmt.Errorf("PBES2 parameters: %w", err)
	}
	if !params.KeyDerivationFunc.Algorithm.Equal(oidPBKDF2) {
		return nil, nil, fmt.Errorf("%w: PBES2 key derivation %s", ErrUnsupported, params.KeyDerivationFunc.Algorithm)
	}
	var kdf pbkdf2Params
	if err := unmarshalExact(params.KeyDerivationFunc.Parameters.FullBytes, &kdf); err != nil {
		return nil, nil, fmt.Errorf("PBKDF2 parameters: %w", err)
	}
	if kdf.Salt.Class != asn1.ClassUniversal || kdf.Salt.Tag != asn1.TagOctetString {
		return nil, nil, fmt.Errorf("%w: PBKDF2 salt from another source", ErrUnsupported)
	}
	prf, err := prfOf(kdf.PRF.Algorithm)
	if err != nil {
		return nil, nil, err
	}

	scheme := params.EncryptionScheme
	var keyLen int
	var newCipher func([]byte) (blockDecrypter, error)
	switch {
	case scheme.Algorithm.Equal(oidAES128CBC):
		keyLen, newCipher = 16, newAES
	case scheme.Algorithm.Equal(oidAES192CBC):
		keyLen, newCipher = 24, newAES
	case scheme.Algorithm.Equal(oidAES256CBC):
		keyLen, newCipher = 32, newAES
	case scheme.Algorithm.Equal(oidDESEDE3CBC):
		keyLen = 24
		newCipher = func(k []byte) (blockDecrypter, error) { return des.NewTripleDESCipher(k) } //nolint:gosec // RFC 8018, B.2.2: DES-EDE3-CBC, which older PBES2 stores use; decryption only.
	default:
		return nil, nil, fmt.Errorf("%w: PBES2 encryption scheme %s", ErrUnsupported, scheme.Algorithm)
	}
	// RFC 8018, A.2: keyLength, when present, is the length the scheme
	// takes; any other is a store that cannot decrypt.
	if kdf.KeyLength != 0 && kdf.KeyLength != keyLen {
		return nil, nil, fmt.Errorf("%w: PBKDF2 key length %d for a %d-byte key", ErrMalformed, kdf.KeyLength, keyLen)
	}
	var iv []byte
	if err := unmarshalExact(scheme.Parameters.FullBytes, &iv); err != nil {
		return nil, nil, fmt.Errorf("PBES2 IV: %w", err)
	}
	if err := b.spend(kdf.IterationCount); err != nil {
		return nil, nil, err
	}
	key, err := pbkdf2.Key(prf, pw.raw, kdf.Salt.Bytes, kdf.IterationCount, keyLen)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: PBKDF2: %w", ErrMalformed, err)
	}
	c, err := newCipher(key)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrMalformed, err)
	}
	return c, iv, nil
}

// newAES returns the AES cipher of key.
func newAES(key []byte) (blockDecrypter, error) { return aes.NewCipher(key) }

// prfOf returns the hash of a PBKDF2 pseudorandom function, HMAC-SHA-1 when
// the parameters name none (RFC 8018, A.2 DEFAULT algid-hmacWithSHA1).
func prfOf(oid asn1.ObjectIdentifier) (func() hash.Hash, error) {
	switch {
	case len(oid) == 0, oid.Equal(oidHMACWithSHA1):
		return sha1Digest.new, nil
	case oid.Equal(oidHMACWithSHA224):
		return sha224Digest.new, nil
	case oid.Equal(oidHMACWithSHA256):
		return sha256Digest.new, nil
	case oid.Equal(oidHMACWithSHA384):
		return sha384Digest.new, nil
	case oid.Equal(oidHMACWithSHA512):
		return sha512Digest.new, nil
	}
	return nil, fmt.Errorf("%w: PBKDF2 pseudorandom function %s", ErrUnsupported, oid)
}

// cbcDecrypt returns ciphertext decrypted in CBC mode under block and iv,
// its PKCS #7 padding (RFC 8018, 6.1.1 and 6.2.1; RFC 5652, 6.3) removed. A
// padding that does not check is what a wrong key leaves, so it is
// [ErrIncorrectPassword].
func cbcDecrypt(block blockDecrypter, iv, ciphertext []byte) ([]byte, error) {
	bs := block.BlockSize()
	if len(iv) != bs {
		return nil, fmt.Errorf("%w: IV of %d bytes for a %d-byte block", ErrMalformed, len(iv), bs)
	}
	if len(ciphertext) == 0 || len(ciphertext)%bs != 0 {
		return nil, fmt.Errorf("%w: ciphertext of %d bytes for a %d-byte block", ErrMalformed, len(ciphertext), bs)
	}
	out := make([]byte, len(ciphertext))
	prev := iv
	for i := 0; i < len(ciphertext); i += bs {
		block.Decrypt(out[i:i+bs], ciphertext[i:i+bs])
		for k := range bs {
			out[i+k] ^= prev[k]
		}
		prev = ciphertext[i : i+bs]
	}
	pad := int(out[len(out)-1])
	if pad < 1 || pad > bs {
		return nil, fmt.Errorf("%w: decryption padding does not check", ErrIncorrectPassword)
	}
	for _, p := range out[len(out)-pad:] {
		if int(p) != pad {
			return nil, fmt.Errorf("%w: decryption padding does not check", ErrIncorrectPassword)
		}
	}
	return out[:len(out)-pad], nil
}

// macKey returns the key of the RFC 7292, 5 password-integrity MAC and the
// HMAC over it.
func macKey(h digest, pw []byte, salt []byte, iterations int) hash.Hash {
	key := deriveKey(h, kdfMAC, pw, salt, iterations, h.size)
	return hmac.New(h.new, key)
}
