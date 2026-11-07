package offcrypto

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // MS-OFFCRYPTO 2.3.4.10 lists SHA-1 among the Agile hashes
	"crypto/sha256"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"hash"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
)

// maxAgileSpinCount caps the password hash iterations of an Agile
// descriptor at 10,000,000: MS-OFFCRYPTO, 2.3.4.10 bounds spinCount there,
// and LibreOffice refuses more (oox/source/crypto/AgileEngine.cxx). Word
// writes 100,000; the cap is a hundred times that, about a second of SHA-512
// on an Apple M-series core, during which ctx is checked. A wrong password
// costs it twice: VelvetSweatshop is tried too.
const maxAgileSpinCount = 10_000_000

// maxAgileSaltBytes caps an Agile salt at 65,536 bytes, the top of the
// saltSize range of MS-OFFCRYPTO, 2.3.4.10, which LibreOffice enforces too.
// Word writes 16.
const maxAgileSaltBytes = 65536

// agileSegment is the plaintext length of one EncryptedPackage segment
// (MS-OFFCRYPTO, 2.3.4.15).
const agileSegment = 4096

// The block keys of MS-OFFCRYPTO, 2.3.4.13 (password verifier and key) and
// 2.3.4.14 (data integrity).
var (
	blockVerifierInput = []byte{0xfe, 0xa7, 0xd2, 0x76, 0x3b, 0x4b, 0x9e, 0x79}
	blockVerifierHash  = []byte{0xd7, 0xaa, 0x0f, 0x6d, 0x30, 0x61, 0x34, 0x4e}
	blockKeyValue      = []byte{0x14, 0x6e, 0x0b, 0xe7, 0xab, 0xac, 0xd0, 0xd6}
	blockHMACKey       = []byte{0x5f, 0xb2, 0xad, 0x01, 0x0c, 0xb9, 0xe1, 0xf6}
	blockHMACValue     = []byte{0xa0, 0x67, 0x7f, 0x02, 0xb2, 0x2c, 0x84, 0x33}
)

// passwordKeyEncryptor is the keyEncryptor URI of the password key
// encryptor (MS-OFFCRYPTO, 2.3.4.10).
const passwordKeyEncryptor = "http://schemas.microsoft.com/office/2006/keyEncryptor/password" //nolint:gosec // a namespace URI, not a credential

// agileParams are the cipher attributes keyData and p:encryptedKey share
// (MS-OFFCRYPTO, 2.3.4.10, CT_KeyData and CT_PasswordKeyEncryptor).
type agileParams struct {
	SaltSize        int    `xml:"saltSize,attr"`
	BlockSize       int    `xml:"blockSize,attr"`
	KeyBits         int    `xml:"keyBits,attr"`
	HashSize        int    `xml:"hashSize,attr"`
	CipherAlgorithm string `xml:"cipherAlgorithm,attr"`
	CipherChaining  string `xml:"cipherChaining,attr"`
	HashAlgorithm   string `xml:"hashAlgorithm,attr"`
	SaltValue       string `xml:"saltValue,attr"`
}

type agileXML struct {
	KeyData       agileParams `xml:"keyData"`
	DataIntegrity struct {
		EncryptedHmacKey   string `xml:"encryptedHmacKey,attr"`
		EncryptedHmacValue string `xml:"encryptedHmacValue,attr"`
	} `xml:"dataIntegrity"`
	KeyEncryptors []struct {
		URI          string `xml:"uri,attr"`
		EncryptedKey struct {
			agileParams
			SpinCount                  int    `xml:"spinCount,attr"`
			EncryptedVerifierHashInput string `xml:"encryptedVerifierHashInput,attr"`
			EncryptedVerifierHashValue string `xml:"encryptedVerifierHashValue,attr"`
			EncryptedKeyValue          string `xml:"encryptedKeyValue,attr"`
		} `xml:"encryptedKey"`
	} `xml:"keyEncryptors>keyEncryptor"`
}

// agileCipher is a checked agileParams: the hash, the key length, the salt.
type agileCipher struct {
	newHash  func() hash.Hash
	hashSize int
	keyBytes int
	salt     []byte
}

// agileScheme is Agile encryption (MS-OFFCRYPTO, 2.3.4.10) with the
// password key encryptor.
type agileScheme struct {
	data, pw                         agileCipher
	spinCount                        int
	verifierInput, verifierHash, key []byte
	hmacKey, hmacValue               []byte
}

func (*agileScheme) name() string { return "Agile encryption" }

// parseAgile reads the XML encryption descriptor of an Agile EncryptionInfo
// (MS-OFFCRYPTO, 2.3.4.10) and checks what [agileScheme.decrypt] relies on.
func parseAgile(b []byte) (packageScheme, error) {
	var doc agileXML
	if err := xml.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("%w: Agile encryption descriptor: %w", office.ErrMalformed, err)
	}
	data, err := checkAgileParams("keyData", doc.KeyData)
	if err != nil {
		return nil, err
	}
	for _, ke := range doc.KeyEncryptors {
		if ke.URI != passwordKeyEncryptor {
			continue
		}
		ek := ke.EncryptedKey
		pw, err := checkAgileParams("encryptedKey", ek.agileParams)
		if err != nil {
			return nil, err
		}
		if ek.SpinCount < 0 || ek.SpinCount > maxAgileSpinCount {
			return nil, fmt.Errorf("%w: Agile spin count %d is past the cap of %d", office.ErrEncrypted, ek.SpinCount, maxAgileSpinCount)
		}
		s := &agileScheme{data: data, pw: pw, spinCount: ek.SpinCount}
		for _, f := range []struct {
			dst  *[]byte
			attr string
			val  string
		}{
			{&s.verifierInput, "encryptedVerifierHashInput", ek.EncryptedVerifierHashInput},
			{&s.verifierHash, "encryptedVerifierHashValue", ek.EncryptedVerifierHashValue},
			{&s.key, "encryptedKeyValue", ek.EncryptedKeyValue},
			{&s.hmacKey, "encryptedHmacKey", doc.DataIntegrity.EncryptedHmacKey},
			{&s.hmacValue, "encryptedHmacValue", doc.DataIntegrity.EncryptedHmacValue},
		} {
			v, err := base64.StdEncoding.DecodeString(f.val)
			if err != nil || len(v) == 0 || len(v)%aes.BlockSize != 0 {
				return nil, fmt.Errorf("%w: Agile %s is not a base64 run of whole blocks", office.ErrMalformed, f.attr)
			}
			*f.dst = v
		}
		return s, nil
	}
	return nil, fmt.Errorf("%w: Agile encryption without a password key encryptor (a certificate one is not read)", office.ErrEncrypted)
}

// checkAgileParams checks the cipher attributes of keyData or
// p:encryptedKey the way MS-OFFCRYPTO, 2.3.4.10 and LibreOffice constrain
// them: AES with a 16-byte block in CBC, 128, 192 or 256 key bits, SHA-1,
// SHA-256, SHA-384 or SHA-512 with its own digest size, and a salt of
// saltSize bytes.
func checkAgileParams(elem string, p agileParams) (agileCipher, error) {
	if p.CipherAlgorithm != "AES" || p.CipherChaining != "ChainingModeCBC" {
		return agileCipher{}, fmt.Errorf("%w: Agile %s cipher %s in %s is not read", office.ErrEncrypted, elem, p.CipherAlgorithm, p.CipherChaining)
	}
	if p.BlockSize != aes.BlockSize || (p.KeyBits != 128 && p.KeyBits != 192 && p.KeyBits != 256) {
		return agileCipher{}, fmt.Errorf("%w: Agile %s block size %d, key bits %d", office.ErrMalformed, elem, p.BlockSize, p.KeyBits)
	}
	var c agileCipher
	switch p.HashAlgorithm {
	case "SHA1":
		c.newHash = sha1.New
	case "SHA256":
		c.newHash = sha256.New
	case "SHA384":
		c.newHash = sha512.New384
	case "SHA512":
		c.newHash = sha512.New
	default:
		return agileCipher{}, fmt.Errorf("%w: Agile %s hash %q is not read", office.ErrEncrypted, elem, p.HashAlgorithm)
	}
	c.hashSize = c.newHash().Size()
	if p.HashSize != c.hashSize {
		return agileCipher{}, fmt.Errorf("%w: Agile %s hash size %d for %s", office.ErrMalformed, elem, p.HashSize, p.HashAlgorithm)
	}
	salt, err := base64.StdEncoding.DecodeString(p.SaltValue)
	if err != nil || p.SaltSize < 1 || p.SaltSize > maxAgileSaltBytes || len(salt) != p.SaltSize {
		return agileCipher{}, fmt.Errorf("%w: Agile %s salt of %d bytes, saltSize %d", office.ErrMalformed, elem, len(salt), p.SaltSize)
	}
	c.salt = salt
	c.keyBytes = p.KeyBits / 8
	return c, nil
}

// iteratedHash is the password hash of MS-OFFCRYPTO, 2.3.4.7 and 2.3.4.11:
// H0 hashes the salt then the UTF-16LE password, and each of spin rounds
// hashes the little-endian round number then the previous hash. It checks
// ctx every 16,384 rounds.
func iteratedHash(ctx context.Context, newHash func() hash.Hash, salt []byte, password string, spin int) ([]byte, error) {
	h := newHash()
	h.Write(salt)
	h.Write(utf16LE(password))
	cur := h.Sum(nil)
	buf := make([]byte, 4+len(cur))
	for i := range spin {
		if i&0x3FFF == 0x3FFF {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		binary.LittleEndian.PutUint32(buf, uint32(i)) //nolint:gosec // i < maxAgileSpinCount, which fits uint32
		copy(buf[4:], cur)
		h.Reset()
		h.Write(buf)
		cur = h.Sum(cur[:0])
	}
	return cur, nil
}

// fitBytes truncates b to n bytes or pads it with 0x36 to n, the rule of
// MS-OFFCRYPTO, 2.3.4.11 and 2.3.4.12 for derived keys and IVs.
func fitBytes(b []byte, n int) []byte {
	out := bytes.Repeat([]byte{0x36}, n)
	copy(out, b)
	return out
}

// hashOf returns newHash's digest of the concatenation of parts.
func hashOf(newHash func() hash.Hash, parts ...[]byte) []byte {
	h := newHash()
	for _, p := range parts {
		h.Write(p)
	}
	return h.Sum(nil)
}

// cbcDecrypt decrypts src, a whole number of blocks, with AES-CBC.
func cbcDecrypt(key, iv, src []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", office.ErrMalformed, err)
	}
	if len(src)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("%w: AES-CBC input of %d bytes is not whole blocks", office.ErrMalformed, len(src))
	}
	out := make([]byte, len(src))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(out, src)
	return out, nil
}

// decrypt runs MS-OFFCRYPTO, 2.3.4.11 to 2.3.4.15: the password hash, the
// verifier check, the secret key, the data integrity HMAC over the whole
// EncryptedPackage stream, then the segments.
func (s *agileScheme) decrypt(ctx context.Context, encrypted []byte, password string) ([]byte, error) {
	pw := s.pw
	h, err := iteratedHash(ctx, pw.newHash, pw.salt, password, s.spinCount)
	if err != nil {
		return nil, err
	}
	// MS-OFFCRYPTO, 2.3.4.12: the password key encryptor's IV is its salt,
	// fitted to the block size.
	iv := fitBytes(pw.salt, aes.BlockSize)
	derive := func(blockKey []byte) []byte {
		return fitBytes(hashOf(pw.newHash, h, blockKey), pw.keyBytes)
	}
	input, err := cbcDecrypt(derive(blockVerifierInput), iv, s.verifierInput)
	if err != nil {
		return nil, err
	}
	want, err := cbcDecrypt(derive(blockVerifierHash), iv, s.verifierHash)
	if err != nil {
		return nil, err
	}
	// MS-OFFCRYPTO, 2.3.4.13: the verifier input is saltSize bytes, its
	// hash hashSize; both were padded to whole blocks.
	if len(input) < len(pw.salt) || len(want) < pw.hashSize {
		return nil, fmt.Errorf("%w: Agile verifier shorter than its salt or hash", office.ErrMalformed)
	}
	got := hashOf(pw.newHash, input[:len(pw.salt)])
	if subtle.ConstantTimeCompare(got, want[:pw.hashSize]) != 1 {
		return nil, errWrongPassword
	}
	keyValue, err := cbcDecrypt(derive(blockKeyValue), iv, s.key)
	if err != nil {
		return nil, err
	}
	d := s.data
	if len(keyValue) < d.keyBytes {
		return nil, fmt.Errorf("%w: Agile key value of %d bytes for a %d-byte key", office.ErrMalformed, len(keyValue), d.keyBytes)
	}
	key := keyValue[:d.keyBytes]
	if err := s.checkIntegrity(key, encrypted); err != nil {
		return nil, err
	}
	return agileDecryptPackage(ctx, d, key, encrypted)
}

// checkIntegrity decrypts the HMAC key and value of dataIntegrity and checks
// the HMAC of the whole EncryptedPackage stream (MS-OFFCRYPTO, 2.3.4.14).
// LibreOffice refuses a document that fails it (oox/source/crypto,
// AgileEngine::checkDataIntegrity); so does this reader.
func (s *agileScheme) checkIntegrity(key, encrypted []byte) error {
	d := s.data
	keyIV := fitBytes(hashOf(d.newHash, d.salt, blockHMACKey), aes.BlockSize)
	hk, err := cbcDecrypt(key, keyIV, s.hmacKey)
	if err != nil {
		return err
	}
	valueIV := fitBytes(hashOf(d.newHash, d.salt, blockHMACValue), aes.BlockSize)
	hv, err := cbcDecrypt(key, valueIV, s.hmacValue)
	if err != nil {
		return err
	}
	if len(hk) < d.hashSize || len(hv) < d.hashSize {
		return fmt.Errorf("%w: Agile HMAC key or value shorter than the hash", office.ErrMalformed)
	}
	mac := hmac.New(d.newHash, hk[:d.hashSize])
	mac.Write(encrypted)
	if !hmac.Equal(mac.Sum(nil), hv[:d.hashSize]) {
		return fmt.Errorf("%w: the Agile package fails its data integrity check (MS-OFFCRYPTO 2.3.4.14)", office.ErrMalformed)
	}
	return nil
}

// agileDecryptPackage decrypts an Agile EncryptedPackage (MS-OFFCRYPTO,
// 2.3.4.15): an 8-byte little-endian size, then 4096-byte segments in
// AES-CBC, segment i's IV being the hash of keyData's salt and i as a
// little-endian 32-bit integer fitted to the block size.
func agileDecryptPackage(ctx context.Context, d agileCipher, key, encrypted []byte) ([]byte, error) {
	if len(encrypted) < 8 {
		return nil, fmt.Errorf("%w: EncryptedPackage of %d bytes", office.ErrMalformed, len(encrypted))
	}
	size := binary.LittleEndian.Uint64(encrypted)
	body := encrypted[8:]
	if size > uint64(len(body)) {
		return nil, fmt.Errorf("%w: EncryptedPackage declares %d bytes and holds %d", office.ErrMalformed, size, len(body))
	}
	if len(body)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("%w: EncryptedPackage body of %d bytes is not whole blocks", office.ErrMalformed, len(body))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", office.ErrMalformed, err)
	}
	out := make([]byte, len(body))
	var idx [4]byte
	for seg, off := 0, 0; off < len(body); seg, off = seg+1, off+agileSegment {
		if seg%256 == 255 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		end := min(off+agileSegment, len(body))
		binary.LittleEndian.PutUint32(idx[:], uint32(seg)) //nolint:gosec // seg < len(body)/4096, which fits uint32
		iv := fitBytes(hashOf(d.newHash, d.salt, idx[:]), aes.BlockSize)
		cipher.NewCBCDecrypter(block, iv).CryptBlocks(out[off:end], body[off:end])
	}
	return out[:size], nil
}
