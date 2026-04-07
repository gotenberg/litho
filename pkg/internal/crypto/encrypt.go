package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"  //nolint:gosec // Required for PDF standard encryption
	"crypto/rand" //nolint:gosec // Required for PDF standard encryption
	"crypto/rc4"  //nolint:gosec // Required for PDF standard encryption
	"crypto/sha256"
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

// EncryptConfig specifies the encryption parameters for a new document.
type EncryptConfig struct {
	UserPassword  string
	OwnerPassword string
	Algorithm     Algorithm
	Permissions   int32
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

	pVal := int(cfg.Permissions)

	if v == 5 {
		return newEncryptorV5(cfg.UserPassword, ownerPw, pVal, keyLen, documentID)
	}
	return newEncryptorLegacy(cfg.UserPassword, ownerPw, v, r, keyLen, cfm, pVal, documentID)
}

// GetEncryptFunc returns a closure that encrypts data for a specific object.
func (e *Encryptor) GetEncryptFunc(objNum, genNum int) func([]byte) ([]byte, error) {
	return func(data []byte) ([]byte, error) {
		if len(data) == 0 {
			return data, nil
		}

		if e.handler.V == 5 {
			// AES-256: use the file encryption key directly.
			return aesEncrypt(e.handler.EncryptionKey, data)
		}

		key := e.handler.GetObjKey(objNum, genNum)

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

	// Generate random salts (8 bytes each: validation salt + key salt).
	userSalt := make([]byte, 16)  // [0:8] validation, [8:16] key
	ownerSalt := make([]byte, 16) // same layout
	if _, err := rand.Read(userSalt); err != nil {
		return nil, nil, fmt.Errorf("generating user salt: %w", err)
	}
	if _, err := rand.Read(ownerSalt); err != nil {
		return nil, nil, fmt.Errorf("generating owner salt: %w", err)
	}

	// U value: SHA256(password | salt[0:16]) + salt[0:16] = 48 bytes
	// The authenticate code uses uVal[32:48] as the full salt for the hash,
	// so we include all 16 salt bytes in the hash for compatibility.
	uHash := sha256.Sum256(append(userBytes, userSalt...))
	uVal := make([]byte, 48)
	copy(uVal[:32], uHash[:])
	copy(uVal[32:48], userSalt)

	// UE value: AES-CBC(key=SHA256(password | keySalt), IV=0) encrypt fileKey
	// The key derivation uses the hash of the full U[32:48] salt too.
	ueKeyHash := sha256.Sum256(append(userBytes, userSalt...))
	ueVal, err := aesCBCEncryptZeroIV(ueKeyHash[:], fileKey)
	if err != nil {
		return nil, nil, fmt.Errorf("encrypting UE: %w", err)
	}

	// O value: SHA256(password | salt[0:16]) + salt[0:16] = 48 bytes
	// The authenticate code hashes password + oVal[32:48] without U.
	oHash := sha256.Sum256(append(ownerBytes, ownerSalt...))
	oVal := make([]byte, 48)
	copy(oVal[:32], oHash[:])
	copy(oVal[32:48], ownerSalt)

	// OE value: AES-CBC(key=ownerHash, IV=0) encrypt fileKey
	// The authenticate code uses the same ownerHash to decrypt OE.
	oeVal, err := aesCBCEncryptZeroIV(oHash[:], fileKey)
	if err != nil {
		return nil, nil, fmt.Errorf("encrypting OE: %w", err)
	}

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
	// Compute O value (Algorithm 3.3 — encryption direction).
	oVal := computeOValue(ownerPw, userPw, v, keyLen)

	// Compute file encryption key (Algorithm 3.1).
	fileKey := computeFileKey(userPw, oVal, pVal, docID, v, keyLen, true)

	// Compute U value (Algorithm 3.4/3.5).
	uVal := computeUValue(fileKey, docID, v)

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
func computeOValue(ownerPw, userPw string, v, keyLen int) []byte {
	// Pad owner password.
	ownerPadded := padPassword([]byte(ownerPw))
	md := md5.New() //nolint:gosec // Required for PDF standard
	md.Write(ownerPadded)
	ownerKey := md.Sum(nil)

	if v >= 3 {
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

	if v < 3 {
		c, _ := rc4.NewCipher(encKey) //nolint:gosec // Key length validated
		c.XORKeyStream(result, result)
	} else {
		// 20-pass RC4, XORing key with i (0 to 19).
		for i := range 20 {
			tempKey := make([]byte, keyLen)
			for j := range keyLen {
				tempKey[j] = encKey[j] ^ byte(i)
			}
			c, _ := rc4.NewCipher(tempKey) //nolint:gosec // Key length validated
			c.XORKeyStream(result, result)
		}
	}

	return result
}

// computeFileKey derives the file encryption key per Algorithm 3.1.
func computeFileKey(userPw string, oVal []byte, pVal int, docID []byte, v, keyLen int, encMeta bool) []byte {
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
	if v >= 3 {
		for range 50 {
			md2 := md5.New() //nolint:gosec // Required for PDF standard
			md2.Write(key[:keyLen])
			key = md2.Sum(nil)
		}
	}

	return key[:keyLen]
}

// computeUValue computes the U (user) value per Algorithm 3.4/3.5.
func computeUValue(fileKey, docID []byte, v int) []byte {
	if v >= 3 {
		// Algorithm 3.5.
		md := md5.New() //nolint:gosec // Required for PDF standard
		md.Write(paddingString)
		md.Write(docID)
		baseHash := md.Sum(nil)

		c, _ := rc4.NewCipher(fileKey) //nolint:gosec // Key length validated
		result := make([]byte, 16)
		c.XORKeyStream(result, baseHash)

		for i := 1; i <= 19; i++ {
			tempKey := make([]byte, len(fileKey))
			for j := range fileKey {
				tempKey[j] = fileKey[j] ^ byte(i)
			}
			c, _ = rc4.NewCipher(tempKey) //nolint:gosec // Key length validated
			c.XORKeyStream(result, result)
		}

		// Pad to 32 bytes with arbitrary data.
		uVal := make([]byte, 32)
		copy(uVal, result)
		return uVal
	}

	// Algorithm 3.4 (V < 3).
	c, _ := rc4.NewCipher(fileKey) //nolint:gosec // Key length validated
	result := make([]byte, 32)
	c.XORKeyStream(result, paddingString)
	return result
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
