package crypto

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5" //nolint:gosec // Required for PDF standard encryption
	"crypto/rc4" //nolint:gosec // Required for PDF standard encryption
	"crypto/sha256"
	"crypto/sha512"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

var paddingString = []byte{
	0x28, 0xBF, 0x4E, 0x5E, 0x4E, 0x75, 0x8A, 0x41,
	0x64, 0x00, 0x4E, 0x56, 0xFF, 0xFA, 0x01, 0x08,
	0x2E, 0x2E, 0x00, 0xB6, 0xD0, 0x68, 0x3E, 0x80,
	0x2F, 0x0C, 0xA9, 0xFE, 0x64, 0x53, 0x69, 0x7A,
}

// Handler handles PDF encryption and decryption.
type Handler struct {
	EncryptionKey []byte
	V             int
	R             int
	Length        int
	CFM           string // Crypt filter method (V4)
	Authenticated bool
	// The handler keeps the parameters to re-authenticate later.
	oVal    []byte
	uVal    []byte
	oeVal   []byte // V=5 Owner Encryption string
	ueVal   []byte // V=5 User Encryption string
	pVal    int
	id      []byte
	encMeta bool
}

func dictInt(dict ast.PdfDict, key ast.PdfName, defaultVal int) int {
	if val, ok := dict[key].(ast.PdfInt); ok {
		return int(val)
	}
	return defaultVal
}

// keyLengthBits returns the key length, in bits, that an encryption
// dictionary declares for the RC4 and AES-128 handlers.
//
// ISO 32000-1, Table 20 gives the encryption dictionary a /Length "only if V
// is 2 or 3", defaulting to 40. From V 4 the length belongs to the crypt filter
// (Table 25), so a V 4 dictionary may carry none at the top level, and taking
// the 40-bit default there cut an AESV2 key, which is 128 bits, to 5 bytes:
// even the right password then failed to open the file. The stream filter's
// /Length is read instead, defaulting to 128, and a top-level /Length is still
// honoured first because producers write both and pdf.js
// (CipherTransformFactory) reads it the same way.
//
// Table 25 notes that the standard security handler expresses a crypt
// filter's /Length in bytes ("16 means 128"), where the public-key handler
// uses bits. A value under 40 cannot be a bit length in the allowed range of
// 40 to 128, so it is read as bytes, which is the same rule pdf.js applies.
//
// Only an absent entry falls back. A declared value outside the range, zero
// among them, is kept, so the key derivation refuses it as unsupported rather
// than treating a malformed dictionary as one that said nothing.
func keyLengthBits(encryptDict ast.PdfDict, v int) int {
	if _, declared := encryptDict["Length"]; declared {
		return dictInt(encryptDict, "Length", 0)
	}
	if v < 4 {
		return 40
	}
	n := 128
	if cf, ok := encryptDict["CF"].(ast.PdfDict); ok {
		if stmf, ok := encryptDict["StmF"].(ast.PdfName); ok {
			if filter, ok := cf[stmf].(ast.PdfDict); ok {
				if _, declared := filter["Length"]; declared {
					n = dictInt(filter, "Length", 0)
				}
			}
		}
	}
	if n < 40 {
		n *= 8
	}
	return n
}

func dictString(dict ast.PdfDict, key ast.PdfName) []byte {
	if val, ok := dict[key].(ast.PdfString); ok {
		return []byte(val)
	}
	return nil
}

// NewHandler initializes a new encryption handler and attempts to authenticate
// with a blank password.
//
// It currently cannot fail: the body has a single return, and the blank-password
// attempt below is discarded on purpose, so a malformed or unsupported /Encrypt
// dictionary yields a handler with Authenticated false rather than an error. The
// caller then supplies a real password and learns the outcome from
// [Handler.Authenticate], which is where [ErrUnsupportedEncryption] and
// [ErrInvalidPassword] come from.
//
// The error result is kept, and callers should keep checking it: everything here
// reads untrusted dictionary entries, which is exactly where a failure path grows
// the first time one of them has to be rejected up front.
func NewHandler(encryptDict ast.PdfDict, trailer ast.PdfDict) (*Handler, error) {
	v := dictInt(encryptDict, "V", 0)
	// /R defaults to V's revision when /R is missing; R selects the
	// algorithm variants in ISO 32000-1 §7.6.3.3/§7.6.3.4. V=2 R=3 is
	// common (RC4 128-bit + 50-pass MD5 + Algorithm 3.5 verification).
	r := dictInt(encryptDict, "R", v)
	length := keyLengthBits(encryptDict, v)
	// /P is a required entry (ISO 32000-1 Table 22). When a malformed
	// document omits it, default to all-permissions-granted (0xFFFFFFFC,
	// i.e. -4 in two's complement: reserved bits 1-2 clear, every
	// permission bit set) rather than 0, which would deny every
	// operation and misreport permissions on a document that never
	// intended to restrict them. -4 is also the value a producer writes
	// for "grant everything", so it matches a present /P.
	p := dictInt(encryptDict, "P", -4)
	o := dictString(encryptDict, "O")
	u := dictString(encryptDict, "U")
	oe := dictString(encryptDict, "OE")
	ue := dictString(encryptDict, "UE")

	var id []byte
	if idArr, ok := trailer["ID"].(ast.PdfArray); ok && len(idArr) > 0 {
		if idStr, ok := idArr[0].(ast.PdfString); ok {
			id = []byte(idStr)
		}
	}

	encryptMeta := true
	if v >= 4 {
		if em, ok := encryptDict["EncryptMetadata"].(ast.PdfBool); ok {
			encryptMeta = bool(em)
		}
	}

	cfm := "V2"
	if v >= 4 {
		if cf, ok := encryptDict["CF"].(ast.PdfDict); ok {
			if stmf, ok := encryptDict["StmF"].(ast.PdfName); ok {
				if methodDict, ok := cf[stmf].(ast.PdfDict); ok {
					if method, ok := methodDict["CFM"].(ast.PdfName); ok {
						cfm = string(method)
					}
				}
			}
		}
	}

	h := &Handler{
		V:       v,
		R:       r,
		Length:  length,
		CFM:     cfm,
		oVal:    o,
		uVal:    u,
		oeVal:   oe,
		ueVal:   ue,
		pVal:    p,
		id:      id,
		encMeta: encryptMeta,
	}

	// Attempt to authenticate with a blank password. Error is intentionally
	// discarded: if blank auth fails, the caller must provide a real password.
	_ = h.Authenticate("") //nolint:errcheck // Deliberate: a failed blank-password attempt just means the caller must supply one, as the comment above says.

	return h, nil
}

// hashV5 computes the password hash for the AES-256 security handler. Revision 6
// uses the hardened hash of ISO 32000-2 Algorithm 2.B; the deprecated revision 5
// uses a single SHA-256. udata is the 48-byte /U value when hashing an owner
// password, empty otherwise.
func (h *Handler) hashV5(password, salt, udata []byte) []byte {
	if h.R == 5 {
		s := sha256.New() //nolint:gosec // ISO 32000-2 revision 5 hash.
		s.Write(password)
		s.Write(salt)
		s.Write(udata)
		return s.Sum(nil)
	}
	return hash2B(password, salt, udata)
}

// hash2B implements the hardened password hash of ISO 32000-2 §7.6.4.3.4
// (Algorithm 2.B) used by Standard security handler revision 6. It repeatedly
// AES-128-CBC-encrypts a 64-fold repetition of (password || K || udata) and
// re-hashes with SHA-256/384/512 selected by the first block modulo 3, until at
// least 64 rounds have run and the last byte of the cipher text is within bound.
func hash2B(password, salt, udata []byte) []byte {
	sum := sha256.Sum256(append(append(append([]byte{}, password...), salt...), udata...))
	k := sum[:]
	for round := 1; ; round++ {
		block := append(append(append([]byte{}, password...), k...), udata...)
		k1 := bytes.Repeat(block, 64)
		cipherBlock, err := aes.NewCipher(k[:16])
		if err != nil {
			return k[:32] // K is always >= 32 bytes; guards a would-be panic.
		}
		e := make([]byte, len(k1))
		cipher.NewCBCEncrypter(cipherBlock, k[16:32]).CryptBlocks(e, k1)
		mod := 0
		for _, b := range e[:16] {
			mod += int(b)
		}
		switch mod % 3 {
		case 0:
			s := sha256.Sum256(e)
			k = s[:]
		case 1:
			s := sha512.Sum384(e)
			k = s[:]
		case 2:
			s := sha512.Sum512(e)
			k = s[:]
		}
		if round >= 64 && int(e[len(e)-1]) <= round-32 {
			break
		}
	}
	return k[:32]
}

// Authenticate tries password as both the user and the owner password and,
// on success, sets h.EncryptionKey and h.Authenticated. It reports
// [ErrInvalidPassword] when neither matches, and leaves h.Authenticated false.
//
// V=5 follows ISO 32000-2, 7.6.4.3 Algorithm 2.A: the password is UTF-8
// truncated to 127 bytes, validated against /U and then /O, with the file key
// recovered from /UE or /OE. A /U, /O or /UE too short to carry the hash and
// its two salts yields [ErrUnsupportedEncryption] rather than a false
// rejection, because the dictionary is malformed rather than the password
// wrong.
//
// V<5 follows ISO 32000-1 Algorithms 3.3 then 3.1 and 3.2 (3.4/3.5 for R>=3).
// Owner recovery runs first, and both the recovered string AND the supplied
// password are tested, so an owner password authenticates a document whose
// user password is unknown.
//
// [NewHandler] already calls this with a blank password, so h.Authenticated
// may be true before any explicit call.
func (h *Handler) Authenticate(password string) error {
	pwBytes := []byte(password)

	if h.V == 5 {
		// AES-256 (ISO 32000-2 §7.6.4.3, Algorithm 2.A). The password is UTF-8,
		// truncated to 127 bytes. The 48-byte /U and /O values each split into a
		// 32-byte hash, an 8-byte validation salt (bytes 32-39), and an 8-byte key
		// salt (bytes 40-47).
		if len(pwBytes) > 127 {
			pwBytes = pwBytes[:127]
		}
		if len(h.uVal) < 48 || len(h.oVal) < 48 || len(h.ueVal) < 32 {
			return ErrUnsupportedEncryption
		}

		// deriveKey decrypts a 32-byte encryption string (/UE or /OE) with an
		// intermediate key computed from the key salt: AES-256-CBC, no padding,
		// zero IV, per Algorithm 2.A.
		deriveKey := func(intermediate, enc []byte) ([]byte, error) {
			block, err := aes.NewCipher(intermediate)
			if err != nil {
				return nil, fmt.Errorf("creating AES cipher for V5 key decryption: %w", err)
			}
			fileKey := make([]byte, len(enc))
			cipher.NewCBCDecrypter(block, make([]byte, aes.BlockSize)).CryptBlocks(fileKey, enc)
			return fileKey, nil
		}

		// User password: validate against /U, then recover the key from /UE.
		if bytes.Equal(h.hashV5(pwBytes, h.uVal[32:40], nil), h.uVal[:32]) {
			fileKey, err := deriveKey(h.hashV5(pwBytes, h.uVal[40:48], nil), h.ueVal)
			if err != nil {
				return err
			}
			h.EncryptionKey = fileKey
			h.Authenticated = true
			return nil
		}

		// Owner password: the owner hash additionally salts with the full 48-byte
		// /U value, and the key is recovered from /OE.
		if len(h.oeVal) >= 32 && bytes.Equal(h.hashV5(pwBytes, h.oVal[32:40], h.uVal[:48]), h.oVal[:32]) {
			fileKey, err := deriveKey(h.hashV5(pwBytes, h.oVal[40:48], h.uVal[:48]), h.oeVal)
			if err != nil {
				return err
			}
			h.EncryptionKey = fileKey
			h.Authenticated = true
			return nil
		}

		h.Authenticated = false
		return ErrInvalidPassword
	}

	// Legacy V<5 logic below
	// Attempt Owner Password recovery (Algorithm 3.3)
	padded := make([]byte, 32)
	if len(pwBytes) >= 32 {
		copy(padded, pwBytes[:32])
	} else {
		copy(padded, pwBytes)
		copy(padded[len(pwBytes):], paddingString[:32-len(pwBytes)])
	}

	md := md5.New() //nolint:gosec,errcheck // Key length is always >0 per PDF spec
	md.Write(padded)
	ownerKey := md.Sum(nil)

	if h.R >= 3 {
		for range 50 {
			md2 := md5.New() //nolint:gosec,errcheck // Key length is always >0 per PDF spec
			md2.Write(ownerKey)
			ownerKey = md2.Sum(nil)
		}
	}

	// /Length is read from the file and is untrusted. ISO 32000-1, Table 20
	// bounds the standard security handler's key to 40 through 128 bits in
	// multiples of 8, and every legacy key below is sliced out of a 16-byte MD5
	// digest. A document declaring /Length 256 sliced that digest to 32 and one
	// declaring a negative length sliced it to -1: both panicked, and only the
	// recover boundary in pkg/pdf kept the panic from reaching the caller.
	// Reject the dictionary instead, because no password opens a document whose
	// declared key length this handler cannot produce.
	keyLen := h.Length / 8
	if keyLen < 5 || keyLen > md5.Size {
		return ErrUnsupportedEncryption
	}
	encKey := ownerKey[:keyLen]

	decryptedO := make([]byte, len(h.oVal))
	copy(decryptedO, h.oVal)

	if h.R < 3 {
		c, err := rc4.NewCipher(encKey) //nolint:gosec // RC4 required by PDF spec
		if err != nil {
			return fmt.Errorf("creating RC4 cipher for owner password recovery: %w", err)
		}
		c.XORKeyStream(decryptedO, decryptedO)
	} else {
		// 20-pass RC4 decryption loop, XORing the key with 19 - i (so from 19 down to 0)
		for i := 19; i >= 0; i-- {
			tempKey := make([]byte, keyLen)
			for j := range keyLen {
				tempKey[j] = encKey[j] ^ byte(i)
			}
			c, err := rc4.NewCipher(tempKey) //nolint:gosec // RC4 required by PDF spec
			if err != nil {
				return fmt.Errorf("creating RC4 cipher for owner password recovery pass %d: %w", i, err)
			}
			c.XORKeyStream(decryptedO, decryptedO)
		}
	}

	// Test both the potentially recovered user password and the original input password.
	candidates := []string{string(decryptedO), password}

	for _, cand := range candidates {
		// Pad or truncate password candidate
		candBytes := []byte(cand)
		candPadded := make([]byte, 32)
		if len(candBytes) >= 32 {
			copy(candPadded, candBytes[:32])
		} else {
			copy(candPadded, candBytes)
			copy(candPadded[len(candBytes):], paddingString[:32-len(candBytes)])
		}

		// Generate the encryption key (Algorithm 3.1).
		mdC := md5.New() //nolint:gosec,errcheck // Key length is always >0 per PDF spec
		mdC.Write(candPadded)
		mdC.Write(h.oVal)
		pBytes := []byte{byte(h.pVal), byte(h.pVal >> 8), byte(h.pVal >> 16), byte(h.pVal >> 24)} //nolint:gosec // Expected truncation
		mdC.Write(pBytes)
		mdC.Write(h.id)

		if h.V >= 4 && !h.encMeta {
			mdC.Write([]byte{0xFF, 0xFF, 0xFF, 0xFF})
		}

		key := mdC.Sum(nil)
		if h.R >= 3 {
			for range 50 {
				md2 := md5.New() //nolint:gosec,errcheck // Key length is always >0 per PDF spec
				md2.Write(key[:keyLen])
				key = md2.Sum(nil)
			}
		}

		candidateKey := key[:keyLen]

		// Verify the key (Algorithm 3.2, checking against the U string).
		var expectedU []byte
		if h.R >= 3 {
			// ISO 32000-1 Algorithm 3.4 and 3.5 for V >= 3

			// Compute base hash.
			md3 := md5.New() //nolint:gosec,errcheck // Key length is always >0 per PDF spec
			md3.Write(paddingString)
			md3.Write(h.id)
			baseHash := md3.Sum(nil)

			// Initialize RC4 with EncryptionKey and encrypt the base hash.
			c, err := rc4.NewCipher(candidateKey) //nolint:gosec // RC4 required by PDF spec
			if err != nil {
				return fmt.Errorf("creating RC4 cipher for user verification: %w", err)
			}
			expectedU = make([]byte, 16)
			c.XORKeyStream(expectedU, baseHash)

			// Run the 19-pass XOR loop.
			for i := 1; i <= 19; i++ {
				tempKey := make([]byte, len(candidateKey))
				for j := range candidateKey {
					tempKey[j] = candidateKey[j] ^ byte(i)
				}
				c, err = rc4.NewCipher(tempKey) //nolint:gosec // RC4 required by PDF spec
				if err != nil {
					return fmt.Errorf("creating RC4 cipher for user verification pass %d: %w", i, err)
				}
				c.XORKeyStream(expectedU, expectedU)
			}
		} else {
			c, err := rc4.NewCipher(candidateKey) //nolint:gosec // RC4 required by PDF spec
			if err != nil {
				return fmt.Errorf("creating RC4 cipher for user verification: %w", err)
			}
			expectedU = make([]byte, 32)
			c.XORKeyStream(expectedU, paddingString)
		}

		// Check whether the generated expectedU matches the document's actual U string.
		match := false
		if len(h.uVal) >= 16 && len(expectedU) >= 16 {
			match = bytes.Equal(h.uVal[:16], expectedU[:16])
		}

		if match {
			h.EncryptionKey = candidateKey
			h.Authenticated = true
			return nil
		}
	}

	h.Authenticated = false
	return ErrInvalidPassword
}

// PermissionFlags returns the raw /P value from the encrypt dictionary.
// The returned integer encodes permission bits as defined by ISO 32000-2,
// Table 22.
func (h *Handler) PermissionFlags() int {
	return h.pVal
}

// SameParameters reports whether h and other were built from the same
// encryption dictionary entries and the same first /ID string.
//
// The file encryption key is computed from a password and those values alone
// (ISO 32000-1, 7.6.3.3, Algorithm 2; ISO 32000-2, 7.6.4.3, Algorithm 2.A for
// the AES-256 handler), so a password that authenticates one handler yields
// the other's key too. Two revisions of one file carry the same values unless
// an update rewrote the encryption dictionary or the /ID.
func (h *Handler) SameParameters(other *Handler) bool {
	return h.V == other.V && h.R == other.R && h.Length == other.Length &&
		h.CFM == other.CFM && h.pVal == other.pVal && h.encMeta == other.encMeta &&
		bytes.Equal(h.oVal, other.oVal) && bytes.Equal(h.uVal, other.uVal) &&
		bytes.Equal(h.oeVal, other.oeVal) && bytes.Equal(h.ueVal, other.ueVal) &&
		bytes.Equal(h.id, other.id)
}

// ObjKey generates the specific encryption key for an object.
// Per ISO 32000-1 §7.6.3.1 (Algorithm 1) the four-byte 'sAlT' suffix
// is only appended when the chosen cipher is AES (V=4 with /CFM AESV2,
// or V=5/AESV3); RC4 cipher methods (V<4, V=4 with /CFM V2) MUST NOT
// salt the per-object key, otherwise the resulting RC4 stream
// decrypts content streams to garbage.
func (h *Handler) ObjKey(objNum, genNum int) []byte {
	md := md5.New() //nolint:gosec,errcheck // Key length is always >0 per PDF spec
	md.Write(h.EncryptionKey)
	md.Write([]byte{byte(objNum), byte(objNum >> 8), byte(objNum >> 16)}) //nolint:gosec // Expected truncation
	md.Write([]byte{byte(genNum), byte(genNum >> 8)})                     //nolint:gosec // Expected truncation

	if h.V == 4 && (h.CFM == "AESV2" || h.CFM == "AESV3") {
		md.Write([]byte{0x73, 0x41, 0x6C, 0x54}) // 'sAlT'
	}

	key := md.Sum(nil)
	keyLen := min(len(h.EncryptionKey)+5, 16)
	return key[:keyLen]
}

// DecryptFunc returns a closure that decrypts data for a specific object.
func (h *Handler) DecryptFunc(objNum, genNum int) func([]byte) ([]byte, error) {
	return func(data []byte) ([]byte, error) {
		if len(data) == 0 {
			return data, nil
		}

		// V=5 (AES-256): use the file encryption key directly.
		if h.V == 5 {
			return aesDecrypt(h.EncryptionKey, data)
		}

		key := h.ObjKey(objNum, genNum)

		if h.V >= 4 && (h.CFM == "AESV2" || h.CFM == "AESV3") {
			return aesDecrypt(key, data)
		}

		c, err := rc4.NewCipher(key) //nolint:gosec // RC4 required by PDF spec
		if err != nil {
			return nil, fmt.Errorf("creating RC4 cipher for decryption: %w", err)
		}
		out := make([]byte, len(data))
		c.XORKeyStream(out, data)
		return out, nil
	}
}

// aesDecrypt performs AES-CBC decryption with PKCS7 unpadding.
// The IV is the first 16 bytes of data.
//
// Decryption is IN PLACE: the bytes after the IV are overwritten with their
// plaintext, so data must not be a buffer the caller intends to read again.
// Every caller reaches here through [Handler.DecryptFunc] and supplies a buffer
// it has just allocated (a freshly read stream body, or a fresh []byte from a
// string), so nothing observes the overwrite today. A caller that passed a
// shared or cached slice would.
//
// Data that cannot be an AES ciphertext comes back unchanged rather than as an
// error. ISO 32000-1, 7.6.2 encrypts with a 16 byte initialisation vector
// followed by whole cipher blocks, so bytes shorter than one block, or leaving a
// partial block after the IV, were never produced by the algorithm and no key
// decrypts them. Leniency: such bytes are returned as they are stored.
//
// The relaxation is for producers that write a stream in plaintext inside an
// encrypted file, such as a /Metadata packet under /EncryptMetadata true:
// passing the bytes through recovers what the producer meant. Erroring instead
// would lose the stream outright, since [github.com/gotenberg/litho/pkg/internal/core/parser]
// turns a decryption error into an unreadable stream.
//
// The passthrough serves strings as well as streams: [Handler.DecryptFunc] routes
// both here for /V 5 and for /V 4 with AESV2 or AESV3.
//
// # Corpus evidence
//
//   - pdfjs bug1826783.pdf: declares /EncryptMetadata true with AESV2 yet
//     stores its /Metadata packet as plaintext. Its 1544 bytes leave 8 past
//     the IV, and passing them through recovers the XMP the producer meant.
//     veraPDF, which decrypts regardless, reports the resulting bytes as a
//     malformed XMP packet.
//   - All corpora, 5044 files: 1 file depends on this passthrough.
func aesDecrypt(key, data []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("creating AES cipher for decryption: %w", err)
	}
	if len(data) < aes.BlockSize {
		return data, nil
	}
	iv := data[:aes.BlockSize]
	ciphertext := data[aes.BlockSize:]
	if len(ciphertext)%aes.BlockSize != 0 {
		return data, nil
	}
	mode := cipher.NewCBCDecrypter(block, iv)
	mode.CryptBlocks(ciphertext, ciphertext)

	// Remove PKCS7 padding.
	if len(ciphertext) > 0 {
		padLen := int(ciphertext[len(ciphertext)-1])
		if padLen > 0 && padLen <= aes.BlockSize && padLen <= len(ciphertext) {
			ciphertext = ciphertext[:len(ciphertext)-padLen]
		}
	}
	return ciphertext, nil
}
