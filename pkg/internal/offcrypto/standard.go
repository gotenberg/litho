package offcrypto

import (
	"context"
	"crypto/aes"
	"crypto/sha1" //nolint:gosec // MS-OFFCRYPTO 2.3.4.7 fixes SHA-1 for Standard encryption
	"crypto/subtle"
	"encoding/binary"
	"fmt"
	"unicode/utf16"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
)

// The EncryptionHeader flags of MS-OFFCRYPTO, 2.3.1.
const (
	flagCryptoAPI = 0x04
	flagExternal  = 0x10
	flagAES       = 0x20
)

// The algorithm identifiers of MS-OFFCRYPTO, 2.3.2.
const (
	algRC4    = 0x6801
	algAES128 = 0x660E
	algAES192 = 0x660F
	algAES256 = 0x6610
	algSHA1   = 0x8004
)

// standardSpinCount is the iteration count of the Standard key derivation
// (MS-OFFCRYPTO, 2.3.4.7).
const standardSpinCount = 50000

// standardScheme is Standard encryption (MS-OFFCRYPTO, 2.3.4.5): AES in ECB
// with a SHA-1 key derivation.
type standardScheme struct {
	keyBytes              int
	salt                  []byte
	encryptedVerifier     []byte
	encryptedVerifierHash []byte
}

func (*standardScheme) name() string { return "Standard encryption" }

// parseStandard reads the body of a Standard EncryptionInfo after its
// version (MS-OFFCRYPTO, 2.3.4.5): Flags, the header size, the
// EncryptionHeader (2.3.2) and the EncryptionVerifier (2.3.3).
func parseStandard(b []byte) (packageScheme, error) {
	le := binary.LittleEndian
	if len(b) < 8 {
		return nil, fmt.Errorf("%w: Standard EncryptionInfo of %d bytes", office.ErrMalformed, len(b))
	}
	flags, hdrSize := le.Uint32(b), le.Uint32(b[4:])
	b = b[8:]
	// MS-OFFCRYPTO, 2.3.2: the fixed fields of EncryptionHeader take 32
	// bytes, the CSP name the rest.
	if hdrSize < 32 || uint64(hdrSize) > uint64(len(b)) {
		return nil, fmt.Errorf("%w: EncryptionHeader of %d bytes in %d", office.ErrMalformed, hdrSize, len(b))
	}
	hdr, ver := b[:hdrSize], b[hdrSize:]
	algID, hashID, keyBits := le.Uint32(hdr[8:]), le.Uint32(hdr[12:]), le.Uint32(hdr[16:])
	if algID == algRC4 || (flags&flagAES == 0 && algID == 0) {
		return nil, fmt.Errorf("%w: RC4 CryptoAPI encryption (MS-OFFCRYPTO 2.3.5) is not read", office.ErrEncrypted)
	}
	if flags&flagExternal != 0 {
		return nil, fmt.Errorf("%w: external encryption (MS-OFFCRYPTO 2.3.1, fExternal) is not read", office.ErrEncrypted)
	}
	// MS-OFFCRYPTO, 2.3.4.5: fCryptoAPI and fAES are set; AlgID 0 means
	// AES-128; KeySize matches the algorithm; AlgIDHash is SHA-1, 0 meaning
	// SHA-1.
	if flags&flagCryptoAPI == 0 || flags&flagAES == 0 {
		return nil, fmt.Errorf("%w: Standard encryption flags %#x", office.ErrMalformed, flags)
	}
	want := map[uint32]uint32{0: 128, algAES128: 128, algAES192: 192, algAES256: 256}
	bits, ok := want[algID]
	if !ok {
		return nil, fmt.Errorf("%w: Standard encryption algorithm %#x is not read", office.ErrEncrypted, algID)
	}
	if keyBits != bits {
		return nil, fmt.Errorf("%w: key size %d for algorithm %#x", office.ErrMalformed, keyBits, algID)
	}
	if hashID != 0 && hashID != algSHA1 {
		return nil, fmt.Errorf("%w: Standard encryption hash %#x is not read", office.ErrEncrypted, hashID)
	}
	// MS-OFFCRYPTO, 2.3.3: SaltSize is 16, the encrypted verifier 16 bytes,
	// VerifierHashSize 20 and, for AES, the encrypted hash 32 bytes.
	if len(ver) < 4+16+16+4+32 || le.Uint32(ver) != 16 || le.Uint32(ver[36:]) != sha1.Size {
		return nil, fmt.Errorf("%w: EncryptionVerifier of %d bytes", office.ErrMalformed, len(ver))
	}
	return &standardScheme{
		keyBytes:              int(bits / 8), // fits int: at most 32
		salt:                  ver[4:20],
		encryptedVerifier:     ver[20:36],
		encryptedVerifierHash: ver[40:72],
	}, nil
}

// standardKey derives the key of MS-OFFCRYPTO, 2.3.4.7: H0 is SHA-1 of the
// salt and the UTF-16LE password, each of 50,000 rounds hashes the
// little-endian round number before the previous hash, the final hash takes
// the block number 0, and the key is the head of the two SHA-1 digests of
// that hash XORed into 64 bytes of 0x36 and of 0x5C.
func standardKey(ctx context.Context, password string, salt []byte, keyBytes int) ([]byte, error) {
	h, err := iteratedHash(ctx, sha1.New, salt, password, standardSpinCount)
	if err != nil {
		return nil, err
	}
	final := sha1.Sum(append(h, 0, 0, 0, 0)) //nolint:gosec // MS-OFFCRYPTO 2.3.4.7 fixes SHA-1
	var buf1, buf2 [64]byte
	for i := range buf1 {
		buf1[i], buf2[i] = 0x36, 0x5C
	}
	for i, c := range final {
		buf1[i] ^= c
		buf2[i] ^= c
	}
	x1, x2 := sha1.Sum(buf1[:]), sha1.Sum(buf2[:]) //nolint:gosec // MS-OFFCRYPTO 2.3.4.7 fixes SHA-1
	return append(x1[:], x2[:]...)[:keyBytes], nil
}

// decrypt checks the verifier (MS-OFFCRYPTO, 2.3.4.9) and decrypts the
// package: an 8-byte little-endian size, then the package in AES-ECB
// (2.3.4.4), truncated to the size.
func (s *standardScheme) decrypt(ctx context.Context, encrypted []byte, password string) ([]byte, error) {
	key, err := standardKey(ctx, password, s.salt, s.keyBytes)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", office.ErrMalformed, err)
	}
	verifier := make([]byte, len(s.encryptedVerifier))
	ecbDecrypt(block, verifier, s.encryptedVerifier)
	hash := make([]byte, len(s.encryptedVerifierHash))
	ecbDecrypt(block, hash, s.encryptedVerifierHash)
	sum := sha1.Sum(verifier) //nolint:gosec // MS-OFFCRYPTO 2.3.4.9 fixes SHA-1
	if subtle.ConstantTimeCompare(sum[:], hash[:sha1.Size]) != 1 {
		return nil, errWrongPassword
	}
	if len(encrypted) < 8 {
		return nil, fmt.Errorf("%w: EncryptedPackage of %d bytes", office.ErrMalformed, len(encrypted))
	}
	size := binary.LittleEndian.Uint64(encrypted)
	body := encrypted[8:]
	body = body[:len(body)-len(body)%aes.BlockSize]
	if size > uint64(len(body)) {
		return nil, fmt.Errorf("%w: EncryptedPackage declares %d bytes and holds %d", office.ErrMalformed, size, len(body))
	}
	out := make([]byte, len(body))
	for off := 0; off < len(body); off += 1 << 20 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		end := min(off+1<<20, len(body))
		ecbDecrypt(block, out[off:end], body[off:end])
	}
	return out[:size], nil
}

// ecbDecrypt decrypts src, a whole number of blocks, into dst block by
// block (electronic codebook).
func ecbDecrypt(b interface{ Decrypt(dst, src []byte) }, dst, src []byte) {
	for i := 0; i+aes.BlockSize <= len(src); i += aes.BlockSize {
		b.Decrypt(dst[i:i+aes.BlockSize], src[i:i+aes.BlockSize])
	}
}

// utf16LE encodes s as UTF-16 little-endian, the password encoding of
// MS-OFFCRYPTO, 2.3.4.7 and 2.3.4.11.
func utf16LE(s string) []byte {
	units := utf16.Encode([]rune(s))
	out := make([]byte, 2*len(units))
	for i, u := range units {
		binary.LittleEndian.PutUint16(out[2*i:], u)
	}
	return out
}
