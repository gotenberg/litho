package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"  //nolint:gosec // Required for PDF standard encryption
	"crypto/rand" //nolint:gosec // Required for PDF standard encryption
	"crypto/rc4"  //nolint:gosec // Required for PDF standard encryption
	"encoding/binary"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// Algorithm enumerates supported encryption algorithms.
type Algorithm int

const (
	// RC4_40 uses 40-bit RC4 (V=1, R=2). Weak, for legacy compatibility only.
	RC4_40 Algorithm = iota
	// RC4_128 uses 128-bit RC4 (V=2, R=3).
	RC4_128
	// AES_128 uses 128-bit AES-CBC (V=4, R=4).
	AES_128
	// AES_256 uses 256-bit AES-CBC (V=5, R=6). Recommended.
	AES_256
)

// AccessibilityPermission is bit 10 of the P entry of a standard security
// handler. ISO 32000-1, 7.6.3.2, Table 22 makes it the permission to extract
// text and graphics in support of accessibility, for handlers of revision 3 or
// greater. ISO 32000-2, 7.6.4.2, Table 22 retires it: readers ignore it and
// writers always set it.
const AccessibilityPermission int32 = 1 << 9

// Reserved bits of the P entry of a standard security handler, with the value
// ISO 32000-2, 7.6.4.2, Table 22 requires of each (see [permissionsEntry]).
const (
	// reservedZeroBits are bits 1-2: "Reserved. Must be zero (0)."
	reservedZeroBits int32 = 0b11
	// reservedOneBits are bits 7-8, "Reserved. Must be 1.", and bits 13-32,
	// "(Security handlers of revision 3 or greater) Reserved. Must be 1.".
	reservedOneBits int32 = 0b1100_0000 | ^int32(1<<12-1)
)

// permissionsEntry returns the P entry an /Encrypt dictionary carries for the
// permission mask p: the permission bits of p, and every reserved bit set to
// the value ISO 32000-2, 7.6.4.2, Table 22 requires of it. Bits 1-2 read
// "Reserved. Must be zero (0).", bits 7-8 "Reserved. Must be 1." and bits
// 13-32 "(Security handlers of revision 3 or greater) Reserved. Must be 1.".
//
// -1 is the usual way to grant every permission, and it sets bits 1-2; 0
// denies every permission, and it clears bits 7-8 and 13-32. ISO 32000-1,
// 7.6.3.2 lists no reserved row in its Table 22, but its NOTE reads "Since all
// the reserved high-order flag bits in the encryption dictionary's P value are
// required to be 1, the integer value P shall be specified as a negative
// integer", and gives -44 as the example for a revision 2 handler. litho sets
// bits 13-32 for that handler ([RC4_40]) too, as the example does, although
// the ISO 32000-2 row names revision 3 or greater.
//
// A reader ignores the reserved bits: "Conforming readers shall ignore all
// flags other than those at bit positions 3, 4, 5, 6, 9, 10, 11, and 12" (ISO
// 32000-1, 7.6.3.2). So the correction grants or withholds nothing. It does
// change the file key of the RC4 and AES-128 handlers, which hash P (ISO
// 32000-1, 7.6.3.3, Algorithm 2), and the /Perms of the AES-256 one (ISO
// 32000-2, 7.6.4.4.9), so it happens once, before either is computed.
func permissionsEntry(p int32) int32 {
	return p&^reservedZeroBits | reservedOneBits
}

// EncryptConfig specifies the encryption parameters for a new document.
type EncryptConfig struct {
	UserPassword  string
	OwnerPassword string
	Algorithm     Algorithm
	// Permissions is the permission mask of the P entry. [NewEncryptor]
	// writes it through [permissionsEntry], which sets the reserved bits to
	// the values ISO 32000-2, Table 22 requires whatever the mask holds.
	Permissions int32
}

// Encryptor creates per-object encryption functions for writing encrypted PDFs.
type Encryptor struct {
	handler *Handler
}

// NewEncryptor builds the /Encrypt dictionary, computes O/U/OE/UE values,
// derives the file encryption key, and returns an Encryptor ready for use.
// The documentID should be the first element of the /ID array.
func NewEncryptor(cfg EncryptConfig, documentID []byte) (*Encryptor, ast.PdfDict, error) {
	var v, r, keyLen int
	var cfm string

	switch cfg.Algorithm {
	case RC4_40:
		v, r, keyLen, cfm = 1, 2, 5, "V2"
	case RC4_128:
		v, r, keyLen, cfm = 2, 3, 16, "V2"
	case AES_128:
		v, r, keyLen, cfm = 4, 4, 16, "AESV2"
	case AES_256:
		v, r, keyLen, cfm = 5, 6, 32, "AESV3"
	default:
		return nil, nil, fmt.Errorf("unsupported encryption algorithm: %d", cfg.Algorithm)
	}

	ownerPw := cfg.OwnerPassword
	if ownerPw == "" {
		ownerPw = cfg.UserPassword
	}

	// The reserved bits of P take the values ISO 32000-2, 7.6.4.2, Table 22
	// requires (see permissionsEntry) before the file key and /Perms are
	// computed from it: every later use of P must see the value written.
	pVal := int(permissionsEntry(cfg.Permissions))

	if v == 5 {
		return newEncryptorV5(cfg.UserPassword, ownerPw, pVal, keyLen, documentID)
	}
	return newEncryptorLegacy(cfg.UserPassword, ownerPw, v, r, keyLen, cfm, pVal, documentID)
}

// EncryptFunc returns a closure that encrypts data for a specific object.
func (e *Encryptor) EncryptFunc(objNum, genNum int) func([]byte) ([]byte, error) {
	return func(data []byte) ([]byte, error) {
		if len(data) == 0 {
			return data, nil
		}

		if e.handler.V == 5 {
			// AES-256: use the file encryption key directly.
			return aesEncrypt(e.handler.EncryptionKey, data)
		}

		key := e.handler.ObjKey(objNum, genNum)

		if e.handler.V >= 4 && e.handler.CFM == "AESV2" {
			return aesEncrypt(key, data)
		}

		// RC4
		c, err := rc4.NewCipher(key) //nolint:gosec // Key length validated during construction
		if err != nil {
			return nil, fmt.Errorf("creating RC4 cipher: %w", err)
		}
		out := make([]byte, len(data))
		c.XORKeyStream(out, data)
		return out, nil
	}
}

// aesEncrypt performs AES-CBC encryption with PKCS7 padding and a random IV.
func aesEncrypt(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("creating AES cipher: %w", err)
	}

	// PKCS7 padding.
	padLen := aes.BlockSize - len(plaintext)%aes.BlockSize
	padded := make([]byte, len(plaintext)+padLen)
	copy(padded, plaintext)
	for i := len(plaintext); i < len(padded); i++ {
		padded[i] = byte(padLen)
	}

	// Random IV.
	iv := make([]byte, aes.BlockSize)
	if _, err := rand.Read(iv); err != nil {
		return nil, fmt.Errorf("generating IV: %w", err)
	}

	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(padded, padded)

	// IV prepended to ciphertext.
	result := make([]byte, aes.BlockSize+len(padded))
	copy(result, iv)
	copy(result[aes.BlockSize:], padded)
	return result, nil
}

// newEncryptorV5 implements AES-256 (V=5, R=6) encryption setup per ISO 32000-2.
func newEncryptorV5(userPw, ownerPw string, pVal, keyLen int, docID []byte) (*Encryptor, ast.PdfDict, error) {
	// Generate random file encryption key.
	fileKey := make([]byte, keyLen)
	if _, err := rand.Read(fileKey); err != nil {
		return nil, nil, fmt.Errorf("generating file key: %w", err)
	}

	// Truncate passwords to 127 bytes.
	userBytes := truncate([]byte(userPw), 127)
	ownerBytes := truncate([]byte(ownerPw), 127)

	// Generate independent 8-byte validation and key salts for the user and owner
	// entries (ISO 32000-2 §7.6.4.4.7, Algorithm 8/9).
	userValSalt, userKeySalt := make([]byte, 8), make([]byte, 8)
	ownerValSalt, ownerKeySalt := make([]byte, 8), make([]byte, 8)
	for _, s := range [][]byte{userValSalt, userKeySalt, ownerValSalt, ownerKeySalt} {
		if _, err := rand.Read(s); err != nil {
			return nil, nil, fmt.Errorf("generating salt: %w", err)
		}
	}

	// U = hash2B(userPw, validationSalt) || validationSalt || keySalt (48 bytes),
	// using the revision-6 hardened hash.
	uVal := make([]byte, 0, 48)
	uVal = append(uVal, hash2B(userBytes, userValSalt, nil)...)
	uVal = append(uVal, userValSalt...)
	uVal = append(uVal, userKeySalt...)

	// UE = AES-256-CBC(key = hash2B(userPw, keySalt), IV=0) of the file key.
	ueVal, err := aesCBCEncryptZeroIV(hash2B(userBytes, userKeySalt, nil), fileKey)
	if err != nil {
		return nil, nil, fmt.Errorf("encrypting UE: %w", err)
	}

	// O additionally salts the hash with the full 48-byte U value; OE mirrors UE.
	oVal := make([]byte, 0, 48)
	oVal = append(oVal, hash2B(ownerBytes, ownerValSalt, uVal[:48])...)
	oVal = append(oVal, ownerValSalt...)
	oVal = append(oVal, ownerKeySalt...)

	oeVal, err := aesCBCEncryptZeroIV(hash2B(ownerBytes, ownerKeySalt, uVal[:48]), fileKey)
	if err != nil {
		return nil, nil, fmt.Errorf("encrypting OE: %w", err)
	}

	// Perms (ISO 32000-2 §7.6.4.4.9, Algorithm 8 step f): AES-256 ECB of a
	// 16-byte block encoding the permissions, so a conforming reader can verify
	// /P was not tampered with. R6 requires it; omitting it makes the /Encrypt
	// dictionary invalid for strict readers (Acrobat, pikepdf) even though a
	// lenient reader still decrypts.
	permsBlock := make([]byte, 16)
	binary.LittleEndian.PutUint32(permsBlock[0:4], uint32(pVal)) //nolint:gosec // /P is a 32-bit flag field reinterpreted as-is.
	permsBlock[4], permsBlock[5], permsBlock[6], permsBlock[7] = 0xFF, 0xFF, 0xFF, 0xFF
	permsBlock[8] = 'T' // EncryptMetadata is true below.
	permsBlock[9], permsBlock[10], permsBlock[11] = 'a', 'd', 'b'
	if _, err := rand.Read(permsBlock[12:16]); err != nil {
		return nil, nil, fmt.Errorf("generating perms salt: %w", err)
	}
	permsCipher, err := aes.NewCipher(fileKey)
	if err != nil {
		return nil, nil, fmt.Errorf("creating perms cipher: %w", err)
	}
	permsVal := make([]byte, 16)
	permsCipher.Encrypt(permsVal, permsBlock)

	// Build /Encrypt dictionary.
	encDict := ast.PdfDict{
		"Filter": ast.PdfName("Standard"),
		"V":      ast.PdfInt(5),
		"R":      ast.PdfInt(6),
		"Length": ast.PdfInt(256),
		"O":      ast.PdfString(string(oVal)),
		"U":      ast.PdfString(string(uVal)),
		"OE":     ast.PdfString(string(oeVal)),
		"UE":     ast.PdfString(string(ueVal)),
		"Perms":  ast.PdfString(string(permsVal)),
		"P":      ast.PdfInt(pVal),
		"CF": ast.PdfDict{
			"StdCF": ast.PdfDict{
				"CFM":       ast.PdfName("AESV3"),
				"AuthEvent": ast.PdfName("DocOpen"),
				"Length":    ast.PdfInt(32),
			},
		},
		"StmF":            ast.PdfName("StdCF"),
		"StrF":            ast.PdfName("StdCF"),
		"EncryptMetadata": ast.PdfBool(true),
	}

	h := &Handler{
		EncryptionKey: fileKey,
		V:             5,
		Length:        256,
		CFM:           "AESV3",
		Authenticated: true,
		oVal:          oVal,
		uVal:          uVal,
		oeVal:         oeVal,
		ueVal:         ueVal,
		pVal:          pVal,
		id:            docID,
		encMeta:       true,
	}

	return &Encryptor{handler: h}, encDict, nil
}

// newEncryptorLegacy implements V1-V4 encryption setup.
func newEncryptorLegacy(userPw, ownerPw string, v, r, keyLen int, cfm string, pVal int, docID []byte) (*Encryptor, ast.PdfDict, error) {
	// Compute O value (Algorithm 3.3, encryption direction).
	oVal, err := computeOValue(ownerPw, userPw, r, keyLen)
	if err != nil {
		return nil, nil, fmt.Errorf("computing O value: %w", err)
	}

	// Compute file encryption key (Algorithm 3.1).
	fileKey := computeFileKey(userPw, oVal, pVal, docID, v, r, keyLen, true)

	// Compute U value (Algorithm 3.4/3.5).
	uVal, err := computeUValue(fileKey, docID, r)
	if err != nil {
		return nil, nil, fmt.Errorf("computing U value: %w", err)
	}

	// Build /Encrypt dictionary.
	encDict := ast.PdfDict{
		"Filter": ast.PdfName("Standard"),
		"V":      ast.PdfInt(v),
		"R":      ast.PdfInt(r),
		"Length": ast.PdfInt(keyLen * 8),
		"O":      ast.PdfString(string(oVal)),
		"U":      ast.PdfString(string(uVal)),
		"P":      ast.PdfInt(pVal),
	}

	if v >= 4 {
		encDict["CF"] = ast.PdfDict{
			"StdCF": ast.PdfDict{
				"CFM":       ast.PdfName(cfm),
				"AuthEvent": ast.PdfName("DocOpen"),
				"Length":    ast.PdfInt(keyLen),
			},
		}
		encDict["StmF"] = ast.PdfName("StdCF")
		encDict["StrF"] = ast.PdfName("StdCF")
		encDict["EncryptMetadata"] = ast.PdfBool(true)
	}

	h := &Handler{
		EncryptionKey: fileKey,
		V:             v,
		R:             r,
		Length:        keyLen * 8,
		CFM:           cfm,
		Authenticated: true,
		oVal:          oVal,
		uVal:          uVal,
		pVal:          pVal,
		id:            docID,
		encMeta:       true,
	}

	return &Encryptor{handler: h}, encDict, nil
}

// computeOValue computes the O (owner) value per Algorithm 3.3 (encryption direction).
// r is the security handler revision (drives the 50-pass MD5 and 20-pass RC4 variants).
func computeOValue(ownerPw, userPw string, r, keyLen int) ([]byte, error) {
	// Pad owner password.
	ownerPadded := padPassword([]byte(ownerPw))
	md := md5.New() //nolint:gosec // Required for PDF standard
	md.Write(ownerPadded)
	ownerKey := md.Sum(nil)

	if r >= 3 {
		for range 50 {
			md2 := md5.New() //nolint:gosec // Required for PDF standard
			md2.Write(ownerKey)
			ownerKey = md2.Sum(nil)
		}
	}
	encKey := ownerKey[:keyLen]

	// Encrypt padded user password with the owner key.
	userPadded := padPassword([]byte(userPw))
	result := make([]byte, 32)
	copy(result, userPadded)

	if r < 3 {
		c, err := rc4.NewCipher(encKey) //nolint:gosec // RC4 required by PDF spec
		if err != nil {
			return nil, fmt.Errorf("creating RC4 cipher for O value computation: %w", err)
		}
		c.XORKeyStream(result, result)
	} else {
		// 20-pass RC4, XORing key with i (0 to 19).
		for i := range 20 {
			tempKey := make([]byte, keyLen)
			for j := range keyLen {
				tempKey[j] = encKey[j] ^ byte(i)
			}
			c, err := rc4.NewCipher(tempKey) //nolint:gosec // RC4 required by PDF spec
			if err != nil {
				return nil, fmt.Errorf("creating RC4 cipher for O value computation pass %d: %w", i, err)
			}
			c.XORKeyStream(result, result)
		}
	}

	return result, nil
}

// computeFileKey derives the file encryption key per Algorithm 3.1.
// v selects the V=4 metadata-flag append; r drives the 50-pass MD5.
func computeFileKey(userPw string, oVal []byte, pVal int, docID []byte, v, r, keyLen int, encMeta bool) []byte {
	padded := padPassword([]byte(userPw))
	md := md5.New() //nolint:gosec // Required for PDF standard
	md.Write(padded)
	md.Write(oVal)
	pBytes := []byte{byte(pVal), byte(pVal >> 8), byte(pVal >> 16), byte(pVal >> 24)} //nolint:gosec // Expected truncation
	md.Write(pBytes)
	md.Write(docID)

	if v >= 4 && !encMeta {
		md.Write([]byte{0xFF, 0xFF, 0xFF, 0xFF})
	}

	key := md.Sum(nil)
	if r >= 3 {
		for range 50 {
			md2 := md5.New() //nolint:gosec // Required for PDF standard
			md2.Write(key[:keyLen])
			key = md2.Sum(nil)
		}
	}

	return key[:keyLen]
}

// computeUValue computes the U (user) value per Algorithm 3.4/3.5.
// r selects the algorithm: 3.4 for r<3, 3.5 for r>=3.
func computeUValue(fileKey, docID []byte, r int) ([]byte, error) {
	if r >= 3 {
		// Algorithm 3.5.
		md := md5.New() //nolint:gosec // Required for PDF standard
		md.Write(paddingString)
		md.Write(docID)
		baseHash := md.Sum(nil)

		c, err := rc4.NewCipher(fileKey) //nolint:gosec // RC4 required by PDF spec
		if err != nil {
			return nil, fmt.Errorf("creating RC4 cipher for U value computation: %w", err)
		}
		result := make([]byte, 16)
		c.XORKeyStream(result, baseHash)

		for i := 1; i <= 19; i++ {
			tempKey := make([]byte, len(fileKey))
			for j := range fileKey {
				tempKey[j] = fileKey[j] ^ byte(i)
			}
			c, err = rc4.NewCipher(tempKey) //nolint:gosec // RC4 required by PDF spec
			if err != nil {
				return nil, fmt.Errorf("creating RC4 cipher for U value computation pass %d: %w", i, err)
			}
			c.XORKeyStream(result, result)
		}

		// Pad to 32 bytes with arbitrary data.
		uVal := make([]byte, 32)
		copy(uVal, result)
		return uVal, nil
	}

	// Algorithm 3.4 (V < 3).
	c, err := rc4.NewCipher(fileKey) //nolint:gosec // RC4 required by PDF spec
	if err != nil {
		return nil, fmt.Errorf("creating RC4 cipher for U value computation: %w", err)
	}
	result := make([]byte, 32)
	c.XORKeyStream(result, paddingString)
	return result, nil
}

// padPassword pads or truncates a password to exactly 32 bytes using paddingString.
func padPassword(pw []byte) []byte {
	padded := make([]byte, 32)
	if len(pw) >= 32 {
		copy(padded, pw[:32])
	} else {
		copy(padded, pw)
		copy(padded[len(pw):], paddingString[:32-len(pw)])
	}
	return padded
}

// aesCBCEncryptZeroIV encrypts plaintext with AES-CBC using a zero IV.
// Used for OE/UE computation in V=5.
func aesCBCEncryptZeroIV(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	iv := make([]byte, aes.BlockSize)
	// Pad plaintext to block size if needed.
	padLen := aes.BlockSize - len(plaintext)%aes.BlockSize
	if padLen == aes.BlockSize {
		padLen = 0
	}
	data := make([]byte, len(plaintext)+padLen)
	copy(data, plaintext)
	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(data, data)
	return data, nil
}

func truncate(b []byte, max int) []byte {
	if len(b) > max {
		return b[:max]
	}
	return b
}
