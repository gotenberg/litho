package offcrypto

import (
	"bytes"
	"compress/flate"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // ODF 1.3 Part 2, 3.4.2 fixes SHA-1 for the legacy start key, checksum and PBKDF2
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/blowfish" //nolint:staticcheck // ODF 1.3 Part 2, 4.16.1 requires consumers to read Blowfish CFB
	"golang.org/x/text/encoding/charmap"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/limits"
)

// ODFParams holds the attributes of one encrypted file entry of an
// OpenDocument manifest (ODF 1.3 Part 2, 4.3 to 4.7 and 4.16), each copied
// verbatim from the attribute it is named after, the empty string when the
// attribute is absent. [OpenODFEntry] parses and checks them.
type ODFParams struct {
	// Size is manifest:size of the <manifest:file-entry>: the entry's
	// length once decrypted and inflated (ODF 1.3 Part 2, 4.16.13).
	Size string
	// ChecksumType and Checksum are manifest:checksum-type and
	// manifest:checksum of <manifest:encryption-data> (4.16.2, 4.16.3).
	ChecksumType, Checksum string
	// AlgorithmName and InitialisationVector are manifest:algorithm-name
	// and manifest:initialisation-vector of <manifest:algorithm> (4.16.1,
	// 4.16.5).
	AlgorithmName, InitialisationVector string
	// StartKeyGenerationName and StartKeySize are
	// manifest:start-key-generation-name and manifest:key-size of
	// <manifest:start-key-generation> (4.16.6, 4.16.7).
	StartKeyGenerationName, StartKeySize string
	// KeyDerivationName, KeySize, IterationCount and Salt are
	// manifest:key-derivation-name, manifest:key-size,
	// manifest:iteration-count and manifest:salt of
	// <manifest:key-derivation> (4.16.9, 4.16.7, 4.16.8, 4.16.12).
	KeyDerivationName, KeySize, IterationCount, Salt string
	// Argon2Iterations, Argon2Memory and Argon2Lanes are the
	// argon2-iterations, argon2-memory (in KiB) and argon2-lanes attributes
	// of <manifest:key-derivation> for Argon2id, in the manifest namespace
	// of ODF 1.5 or the loext namespace LibreOffice 24.2 and later write.
	Argon2Iterations, Argon2Memory, Argon2Lanes string
}

// The algorithm names of ODF 1.3 Part 2, 4.16.1, 4.16.3, 4.16.6 and
// 4.16.9, with the ones LibreOffice reads beyond them
// (package/source/manifest/ManifestDefines.hxx).
const (
	odfBlowfish      = "Blowfish CFB"
	odfBlowfishURI   = "urn:oasis:names:tc:opendocument:xmlns:manifest:1.0#blowfish"
	odfAESCBCPrefix  = "http://www.w3.org/2001/04/xmlenc#aes"
	odfAESGCMPrefix  = "http://www.w3.org/2009/xmlenc11#aes"
	odfSHA1          = "SHA1"
	odfSHA1URI       = "http://www.w3.org/2000/09/xmldsig#sha1"
	odfSHA256DSig    = "http://www.w3.org/2000/09/xmldsig#sha256"
	odfSHA256Enc     = "http://www.w3.org/2001/04/xmlenc#sha256"
	odfPBKDF2        = "PBKDF2"
	odfPBKDF2URI     = "urn:oasis:names:tc:opendocument:xmlns:manifest:1.0#pbkdf2"
	odfArgon2ID      = "urn:oasis:names:tc:opendocument:xmlns:manifest:1.5#argon2id"
	odfArgon2IDLO    = "urn:org:documentfoundation:names:experimental:office:manifest:argon2id"
	odfSHA11K        = "SHA1/1K"
	odfSHA11KURI     = "urn:oasis:names:tc:opendocument:xmlns:manifest:1.0#sha1-1k"
	odfSHA2561KURI   = "urn:oasis:names:tc:opendocument:xmlns:manifest:1.0#sha256-1k"
	odfChecksumBytes = 1024
)

// maxPBKDF2Iterations caps the PBKDF2 iteration count at 10,000,000.
// LibreOffice writes 100,000, OpenOffice.org 1,024; the cap is a hundred
// times LibreOffice's, about two seconds of HMAC-SHA-1 for a 32-byte key on
// an Apple M-series core, during which ctx is checked. A wrong password for
// a SHA-1 start key derives up to four keys. Anti-DoS guard, not spec-mandated: ODF 1.3 Part 2, 4.16.8 gives
// the count no bound.
const maxPBKDF2Iterations = 10_000_000

// maxArgon2MemoryKiB caps Argon2id's memory at 256 MiB, four times the
// 64 MiB LibreOffice writes: the memory is allocated for the whole
// derivation. Anti-DoS guard, not spec-mandated: RFC 9106, 3.1 allows up to
// 4 TiB.
const maxArgon2MemoryKiB = 1 << 18

// maxArgon2Work caps the product of Argon2id's passes and memory in KiB at
// 2^21, 2 GiB of block passes: about ten times LibreOffice's 3 passes over
// 64 MiB, which take some 50 ms on an Apple M-series core; the cap, which
// no ctx can interrupt, about half a second.
// Anti-DoS guard, not spec-mandated: RFC 9106, 3.1 bounds neither.
const maxArgon2Work = 2 << 20

// maxArgon2Lanes caps Argon2id's lanes, which run as that many goroutines,
// at 16; LibreOffice writes 4. Anti-DoS guard, not spec-mandated: RFC 9106,
// 3.1 allows 2^24 - 1.
const maxArgon2Lanes = 16

// maxODFEntryBytes caps one decrypted, inflated entry at the bound litho
// puts on one decoded PDF stream, 256 MiB: a content.xml past it is past
// what the converters lay out. Anti-DoS guard, not spec-mandated.
const maxODFEntryBytes = limits.DefaultMaxStreamBytes

// odfCipher is the algorithm of <manifest:algorithm>.
type odfCipher int

const (
	odfBlowfishCFB odfCipher = iota
	odfAESCBC
	odfAESGCM
)

// odfDigest is a start key or checksum digest, with StarOffice's SHA-1 as a
// variant of SHA-1.
type odfDigest int

const (
	digestSHA1 odfDigest = iota
	digestStarOfficeSHA1
	digestSHA256
)

func (d odfDigest) sum(b []byte) []byte {
	switch d {
	case digestStarOfficeSHA1:
		s := starOfficeSHA1(b)
		return s[:]
	case digestSHA256:
		s := sha256.Sum256(b)
		return s[:]
	case digestSHA1:
	}
	s := sha1.Sum(b) //nolint:gosec // ODF 1.3 Part 2, 4.16.3: the SHA1/1K checksum
	return s[:]
}

// odfEntry is a parsed and checked ODFParams.
type odfEntry struct {
	size        int64
	hasSize     bool
	cipher      odfCipher
	iv          []byte
	keyBytes    int
	startSHA256 bool
	argon2      bool
	iterations  int
	salt        []byte
	a2t, a2m    uint32
	a2p         uint8
	checksum    []byte
	checkSHA256 bool
}

func odfMalformed(format string, args ...any) error {
	return fmt.Errorf("%w: ODF encryption data: %s", office.ErrMalformed, fmt.Sprintf(format, args...))
}

func odfNotRead(format string, args ...any) error {
	return fmt.Errorf("%w: ODF encryption: %s is not read", office.ErrEncrypted, fmt.Sprintf(format, args...))
}

// parseCount parses a nonNegativeInteger attribute (ODF 1.3 Part 2, 7.2)
// at most limit.
func parseCount(attr, v string, limit int64) (int64, error) {
	n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	if err != nil || n < 0 {
		return 0, odfMalformed("%s %q is not a non-negative integer", attr, v)
	}
	if n > limit {
		return 0, odfNotRead("%s %d past the cap of %d", attr, n, limit)
	}
	return n, nil
}

func decodeBase64(attr, v string) ([]byte, error) {
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(v))
	if err != nil {
		return nil, odfMalformed("%s is not base64", attr)
	}
	return b, nil
}

// parseODFParams checks p against ODF 1.3 Part 2, 4.16 and the caps.
func parseODFParams(p ODFParams) (*odfEntry, error) {
	e := &odfEntry{}
	var err error
	if p.Size != "" {
		if e.size, err = parseCount("manifest:size", p.Size, maxODFEntryBytes); err != nil {
			if errors.Is(err, office.ErrEncrypted) {
				return nil, fmt.Errorf("%w: ODF entry of %s bytes past the cap of %d", office.ErrInputTooLarge, p.Size, maxODFEntryBytes)
			}
			return nil, err
		}
		e.hasSize = true
	}
	// ODF 1.3 Part 2, 4.16.1: Blowfish CFB and the xmlenc-core IRIs; the
	// AES key size follows from the IRI, as LibreOffice reads it.
	name := p.AlgorithmName
	var ivLen int
	switch {
	case name == odfBlowfish || name == odfBlowfishURI:
		e.cipher, ivLen, e.keyBytes = odfBlowfishCFB, blowfish.BlockSize, 16
	case strings.HasPrefix(name, odfAESCBCPrefix) && strings.HasSuffix(name, "-cbc"):
		e.cipher, ivLen = odfAESCBC, aes.BlockSize
		e.keyBytes, err = aesKeyBytes(name, odfAESCBCPrefix, "-cbc")
	case strings.HasPrefix(name, odfAESGCMPrefix) && strings.HasSuffix(name, "-gcm"):
		// LibreOffice writes the 96-bit nonce xmlenc-core1, 5.2.4 recommends.
		e.cipher, ivLen = odfAESGCM, 12
		e.keyBytes, err = aesKeyBytes(name, odfAESGCMPrefix, "-gcm")
	default:
		return nil, odfNotRead("algorithm %q", name)
	}
	if err != nil {
		return nil, err
	}
	if e.iv, err = decodeBase64("manifest:initialisation-vector", p.InitialisationVector); err != nil {
		return nil, err
	}
	if len(e.iv) != ivLen {
		return nil, odfMalformed("an initialisation vector of %d bytes for %s", len(e.iv), name)
	}
	// ODF 1.3 Part 2, 4.16.6: SHA1 by default; LibreOffice also reads the
	// xmlenc sha256 IRI it writes since 24.2.
	switch p.StartKeyGenerationName {
	case "", odfSHA1, odfSHA1URI:
	case odfSHA256DSig, odfSHA256Enc:
		e.startSHA256 = true
	default:
		return nil, odfNotRead("start key generation %q", p.StartKeyGenerationName)
	}
	if p.StartKeySize != "" {
		want := int64(sha1.Size)
		if e.startSHA256 {
			want = sha256.Size
		}
		if n, err := parseCount("start key manifest:key-size", p.StartKeySize, 1<<10); err != nil {
			return nil, err
		} else if n != want {
			return nil, odfNotRead("a start key of %d bytes from a %d-byte digest", n, want)
		}
	}
	// ODF 1.3 Part 2, 4.16.9: PBKDF2 with HMAC-SHA-1; Argon2id as ODF 1.5
	// and LibreOffice name it.
	switch p.KeyDerivationName {
	case odfPBKDF2, odfPBKDF2URI:
		n, err := parseCount("manifest:iteration-count", p.IterationCount, maxPBKDF2Iterations)
		if err != nil {
			return nil, err
		}
		if n == 0 {
			return nil, odfMalformed("PBKDF2 with no iteration")
		}
		e.iterations = int(n) // fits int: at most maxPBKDF2Iterations
	case odfArgon2ID, odfArgon2IDLO:
		e.argon2 = true
		t, err := parseCount("argon2-iterations", p.Argon2Iterations, maxArgon2Work)
		if err != nil {
			return nil, err
		}
		m, err := parseCount("argon2-memory", p.Argon2Memory, maxArgon2MemoryKiB)
		if err != nil {
			return nil, err
		}
		l, err := parseCount("argon2-lanes", p.Argon2Lanes, maxArgon2Lanes)
		if err != nil {
			return nil, err
		}
		// LibreOffice refuses zero parameters (ManifestImport.cxx).
		if t == 0 || m == 0 || l == 0 {
			return nil, odfMalformed("Argon2id with a zero parameter")
		}
		if t*m > maxArgon2Work {
			return nil, odfNotRead("Argon2id work of %d passes over %d KiB past the cap of %d", t, m, maxArgon2Work)
		}
		e.a2t, e.a2m, e.a2p = uint32(t), uint32(m), uint8(l) //nolint:gosec // bounded by the caps above
	default:
		return nil, odfNotRead("key derivation %q", p.KeyDerivationName)
	}
	if e.salt, err = decodeBase64("manifest:salt", p.Salt); err != nil {
		return nil, err
	}
	if len(e.salt) == 0 {
		return nil, odfMalformed("an empty salt")
	}
	// ODF 1.3 Part 2, 4.16.7: the derived key defaults to 16 bytes;
	// LibreOffice takes the AES IRI's size when the attribute is absent.
	if p.KeySize != "" {
		n, err := parseCount("manifest:key-size", p.KeySize, 1<<10)
		if err != nil {
			return nil, err
		}
		switch {
		case e.cipher == odfBlowfishCFB && n >= 4 && n <= 56:
			e.keyBytes = int(n) // fits int: at most 56
		case e.cipher != odfBlowfishCFB && int(n) == e.keyBytes: // fits int: at most 1024
		default:
			return nil, odfMalformed("a derived key of %d bytes for %s", n, name)
		}
	}
	// ODF 1.3 Part 2, 4.16.3: the 1K digests. LibreOffice ignores any
	// other checksum type, and an AEAD cipher needs none (ManifestImport.cxx).
	switch p.ChecksumType {
	case odfSHA11K, odfSHA11KURI:
	case odfSHA2561KURI:
		e.checkSHA256 = true
	default:
		return e, nil
	}
	if e.checksum, err = decodeBase64("manifest:checksum", p.Checksum); err != nil {
		return nil, err
	}
	return e, nil
}

// aesKeyBytes reads the key size of an xmlenc AES IRI: aes128, aes192 or
// aes256.
func aesKeyBytes(name, prefix, suffix string) (int, error) {
	switch strings.TrimSuffix(strings.TrimPrefix(name, prefix), suffix) {
	case "128":
		return 16, nil
	case "192":
		return 24, nil
	case "256":
		return 32, nil
	}
	return 0, odfNotRead("algorithm %q", name)
}

// odfAttempt is one start key LibreOffice tries, with the checksum digest it
// pairs with it (package/source/zippackage/ZipPackageStream.cxx,
// getDataStream).
type odfAttempt struct {
	startKey []byte
	check    odfDigest
}

// attempts lists the start keys to try for password, in LibreOffice's
// order. A SHA-256 start key is the SHA-256 of the UTF-8 password (ODF 1.3
// Part 2, 4.16.6). A SHA-1 one is the SHA-1 of the UTF-8 password (3.4.2);
// then StarOffice's SHA-1 of it with StarOffice's checksum (tdf#114939);
// then SHA-256, which LibreOffice 3.6 and earlier wrote without saying so
// (fdo#47482); then StarOffice's SHA-1 of the Windows-1252 password, the
// encoding of OpenOffice.org 1.x, when it has one.
func (e *odfEntry) attempts(password string) []odfAttempt {
	utf8 := []byte(password)
	sha := sha256.Sum256(utf8)
	if e.startSHA256 {
		return []odfAttempt{{sha[:], digestSHA256}}
	}
	check := digestSHA1
	so := digestStarOfficeSHA1
	if e.checkSHA256 {
		check, so = digestSHA256, digestSHA256
	}
	s1, s1so := sha1.Sum(utf8), starOfficeSHA1(utf8) //nolint:gosec // ODF 1.3 Part 2, 3.4.2: the SHA-1 start key
	out := []odfAttempt{
		{s1[:], check},
		{s1so[:], so},
		{sha[:], check},
	}
	if cp, err := charmap.Windows1252.NewEncoder().Bytes(utf8); err == nil && !bytes.Equal(cp, utf8) {
		k := starOfficeSHA1(cp)
		out = append(out, odfAttempt{k[:], so})
	}
	return out
}

// OpenODFEntry returns the content of one encrypted entry of an
// OpenDocument package: stored is the entry's bytes as the zip archive
// stores them (ODF 1.3 Part 2, 3.4.1: encrypted entries are STORED), p the
// attributes of its manifest entry.
//
// The start key is the password's SHA-1 or SHA-256 (ODF 1.3 Part 2,
// 4.16.6), the key is derived from it with PBKDF2 (HMAC-SHA-1, 4.16.9) or
// Argon2id (RFC 9106, version 0x13), and the entry is decrypted with
// Blowfish in CFB (LibreOffice's 64-bit feedback, where 3.4.2 says 8-bit),
// AES in CBC with xmlenc-core's padding, or AES in GCM with the IV before
// the ciphertext and the tag after it. The SHA-1 or SHA-256 checksum of the first 1024 bytes of
// the decrypted, still compressed entry (4.16.3) tells a wrong password;
// for a SHA-1 start key the fallbacks LibreOffice tries for files of
// StarOffice and OpenOffice.org are tried too. The result is the entry
// inflated (3.4.1: encrypted entries are deflated first) and checked
// against Size when it is given. An entry of LibreOffice's whole-package
// encryption (encrypted-package) inflates to a zip archive, which the
// caller opens as the package.
//
// It fails with office.ErrEncrypted when the password is empty or wrong,
// or when p names an algorithm, start key, key derivation or a cost past
// the caps this package does not read; with office.ErrMalformed when p or
// the decrypted entry cannot be read; with office.ErrInputTooLarge when the
// entry inflates past 256 MiB. It returns ctx's error when ctx ends first.
// stored is not modified; the result is a new slice. Each call derives its
// own key: a caller bounds the encrypted entries it opens.
func OpenODFEntry(ctx context.Context, stored []byte, p ODFParams, password string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	e, err := parseODFParams(p)
	if err != nil {
		return nil, err
	}
	if password == "" {
		return nil, fmt.Errorf("%w: the entry is protected by a password and none was given", office.ErrEncrypted)
	}
	derived := map[string][]byte{}
	for _, a := range e.attempts(password) {
		key, ok := derived[string(a.startKey)]
		if !ok {
			if key, err = e.deriveKey(ctx, a.startKey); err != nil {
				return nil, err
			}
			derived[string(a.startKey)] = key
		}
		plain, err := e.decrypt(key, stored)
		if errors.Is(err, errWrongPassword) {
			continue
		}
		if err != nil {
			return nil, err
		}
		verified := e.cipher == odfAESGCM
		if e.checksum != nil {
			sum := a.check.sum(plain[:min(len(plain), odfChecksumBytes)])
			if subtle.ConstantTimeCompare(sum, e.checksum) != 1 {
				continue
			}
			verified = true
		}
		out, err := e.inflate(ctx, plain)
		if err != nil && !verified {
			// No checksum to tell a wrong password: a stream that does
			// not inflate is one.
			continue
		}
		return out, err
	}
	return nil, fmt.Errorf("%w: the password does not open the entry", office.ErrEncrypted)
}

// deriveKey derives the entry key from a start key (ODF 1.3 Part 2,
// 3.4.2).
func (e *odfEntry) deriveKey(ctx context.Context, startKey []byte) ([]byte, error) {
	if e.argon2 {
		return argon2.IDKey(startKey, e.salt, e.a2t, e.a2m, e.a2p, uint32(e.keyBytes)), nil //nolint:gosec // keyBytes is at most 56
	}
	return pbkdf2SHA1(ctx, startKey, e.salt, e.iterations, e.keyBytes)
}

// pbkdf2SHA1 is PBKDF2 with HMAC-SHA-1 (RFC 8018, 5.2), checking ctx every
// 16,384 iterations.
func pbkdf2SHA1(ctx context.Context, password, salt []byte, iter, keyLen int) ([]byte, error) {
	prf := hmac.New(sha1.New, password)
	out := make([]byte, 0, keyLen+sha1.Size)
	u := make([]byte, 0, sha1.Size)
	t := make([]byte, sha1.Size)
	var idx [4]byte
	for block := uint32(1); len(out) < keyLen; block++ {
		prf.Reset()
		prf.Write(salt)
		binary.BigEndian.PutUint32(idx[:], block)
		prf.Write(idx[:])
		u = prf.Sum(u[:0])
		copy(t, u)
		for i := 1; i < iter; i++ {
			if i&0x3FFF == 0 {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
			}
			prf.Reset()
			prf.Write(u)
			u = prf.Sum(u[:0])
			for j := range t {
				t[j] ^= u[j]
			}
		}
		out = append(out, t...)
	}
	return out[:keyLen], nil
}

// decrypt decrypts stored with key, returning errWrongPassword when the
// result cannot be the output of the cipher under this key: a GCM tag that
// fails, a CBC padding that is not xmlenc-core's.
func (e *odfEntry) decrypt(key, stored []byte) ([]byte, error) {
	switch e.cipher {
	case odfBlowfishCFB:
		b, err := blowfish.NewCipher(key)
		if err != nil {
			return nil, odfMalformed("%v", err)
		}
		return cfbDecrypt(b, e.iv, stored), nil
	case odfAESCBC:
		return cbcUnpad(key, e.iv, stored)
	case odfAESGCM:
		b, err := aes.NewCipher(key)
		if err != nil {
			return nil, odfMalformed("%v", err)
		}
		g, err := cipher.NewGCM(b)
		if err != nil {
			return nil, odfMalformed("%v", err)
		}
		// xmlenc-core1, 5.2.4: the IV precedes the ciphertext and the
		// 128-bit tag follows it; LibreOffice also writes the IV to the
		// manifest and refuses a mismatch (xmlsecurity/source/xmlsec/nss/
		// ciphercontext.cxx).
		if len(stored) < len(e.iv)+g.Overhead() {
			return nil, odfMalformed("an AES-GCM entry of %d bytes", len(stored))
		}
		if !bytes.Equal(stored[:len(e.iv)], e.iv) {
			return nil, odfMalformed("an AES-GCM entry whose IV is not its manifest's")
		}
		out, err := g.Open(nil, e.iv, stored[len(e.iv):], nil)
		if err != nil {
			return nil, errWrongPassword
		}
		return out, nil
	}
	return nil, odfNotRead("cipher %d", e.cipher)
}

// cbcUnpad decrypts an AES-CBC entry and strips xmlenc-core's padding,
// returning errWrongPassword when the padding cannot be one.
func cbcUnpad(key, iv, stored []byte) ([]byte, error) {
	if len(stored) == 0 || len(stored)%aes.BlockSize != 0 {
		return nil, odfMalformed("an AES-CBC entry of %d bytes", len(stored))
	}
	out, err := cbcDecrypt(key, iv, stored)
	if err != nil {
		return nil, err
	}
	// xmlenc-core, 5.2: the last byte counts the padding bytes, 1 to
	// the block size; the others are arbitrary.
	n := int(out[len(out)-1])
	if n == 0 || n > aes.BlockSize {
		return nil, errWrongPassword
	}
	return out[:len(out)-n], nil
}

// cfbDecrypt decrypts src in full-block cipher feedback mode (NIST SP
// 800-38A, 6.3, with s the block size), the mode LibreOffice's Blowfish
// CFB is (sal/rtl/cipher.cxx, EVP_bf_cfb).
func cfbDecrypt(b cipher.Block, iv, src []byte) []byte {
	bs := b.BlockSize()
	out := make([]byte, len(src))
	reg := bytes.Clone(iv)
	ks := make([]byte, bs)
	for off := 0; off < len(src); off += bs {
		b.Encrypt(ks, reg)
		end := min(off+bs, len(src))
		for i := off; i < end; i++ {
			out[i] = src[i] ^ ks[i-off]
		}
		copy(reg, src[off:end])
	}
	return out
}

// inflate inflates the raw deflate stream plain (RFC 1951) and checks it
// against the entry's size.
func (e *odfEntry) inflate(ctx context.Context, plain []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	limit := int64(maxODFEntryBytes)
	if e.hasSize {
		limit = e.size
	}
	r := flate.NewReader(bytes.NewReader(plain))
	defer r.Close() //nolint:errcheck // a flate reader over memory has nothing to release
	out, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, odfMalformed("the decrypted entry does not inflate: %v", err)
	}
	if int64(len(out)) > limit {
		if e.hasSize {
			return nil, odfMalformed("the entry inflates past its manifest:size of %d", e.size)
		}
		return nil, fmt.Errorf("%w: ODF entry inflates past %d bytes", office.ErrInputTooLarge, maxODFEntryBytes)
	}
	if e.hasSize && int64(len(out)) != e.size {
		return nil, odfMalformed("the entry inflates to %d bytes, its manifest:size is %d", len(out), e.size)
	}
	return out, nil
}
