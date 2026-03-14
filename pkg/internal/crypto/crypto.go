package crypto

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5" //nolint:gosec // Required for PDF standard encryption
	"crypto/rc4" //nolint:gosec // Required for PDF standard encryption
	"crypto/sha256"

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
	Length        int
	CFM           string // Crypt filter method (V4)
	Authenticated bool
	// We store the parameters so we can re-authenticate later
	oVal    []byte
	uVal    []byte
	oeVal   []byte // V=5 Owner Encryption string
	ueVal   []byte // V=5 User Encryption string
	pVal    int
	id      []byte
	encMeta bool
}

func getInt(dict ast.PdfDict, key ast.PdfName, defaultVal int) int {
	if val, ok := dict[key].(ast.PdfInt); ok {
		return int(val)
	}
	return defaultVal
}

func getString(dict ast.PdfDict, key ast.PdfName) []byte {
	if val, ok := dict[key].(ast.PdfString); ok {
		return []byte(val)
	}
	return nil
}

// NewHandler initializes a new encryption handler and attempts to authenticate with a blank password.
func NewHandler(encryptDict ast.PdfDict, trailer ast.PdfDict) (*Handler, error) {
	v := getInt(encryptDict, "V", 0)
	length := getInt(encryptDict, "Length", 40)
	p := getInt(encryptDict, "P", 0)
	o := getString(encryptDict, "O")
	u := getString(encryptDict, "U")
	oe := getString(encryptDict, "OE")
	ue := getString(encryptDict, "UE")

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
	_ = h.Authenticate("") //nolint:errcheck

	return h, nil
}

// Authenticate generates the global encryption key using the provided password
// and verifies it against the document's U string (Algorithm 3.2).
func (h *Handler) Authenticate(password string) error {
	pwBytes := []byte(password)

	if h.V == 5 {
		// ISO 32000-2 Revision 6 (AES-256)
		// Truncate to 127 bytes
		if len(pwBytes) > 127 {
			pwBytes = pwBytes[:127]
		}

		if len(h.uVal) < 48 || len(h.oVal) < 48 || len(h.ueVal) < 32 {
			return ErrUnsupportedEncryption
		}

		// Check User Password
		// The salt is the last 16 bytes of U (bytes 32-47)
		salt := h.uVal[32:48]

		h256 := sha256.New() //nolint:gosec
		h256.Write(pwBytes)
		h256.Write(salt)
		userHash := h256.Sum(nil)

		match := bytes.Equal(userHash, h.uVal[:32])

		if match {
			// Decrypt UE string to get the file encryption key
			block, err := aes.NewCipher(userHash)
			if err != nil {
				return err
			}

			// IV is zeroed for this decryption
			iv := make([]byte, aes.BlockSize)

			mode := cipher.NewCBCDecrypter(block, iv)
			fileKey := make([]byte, len(h.ueVal))
			mode.CryptBlocks(fileKey, h.ueVal)

			h.EncryptionKey = fileKey
			h.Authenticated = true
			return nil
		}

		// Owner check for V=5
		ownerSalt := h.oVal[32:48]
		h256O := sha256.New() //nolint:gosec
		h256O.Write(pwBytes)
		h256O.Write(ownerSalt)
		ownerHash := h256O.Sum(nil)

		matchO := bytes.Equal(ownerHash, h.oVal[:32])
		if matchO {
			if len(h.oeVal) >= 32 {
				block, err := aes.NewCipher(ownerHash)
				if err != nil {
					return err
				}

				iv := make([]byte, aes.BlockSize)
				mode := cipher.NewCBCDecrypter(block, iv)
				fileKey := make([]byte, len(h.oeVal))
				mode.CryptBlocks(fileKey, h.oeVal)

				h.EncryptionKey = fileKey
				h.Authenticated = true
				return nil
			}
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

	if h.V >= 3 {
		for range 50 {
			md2 := md5.New() //nolint:gosec,errcheck // Key length is always >0 per PDF spec
			md2.Write(ownerKey)
			ownerKey = md2.Sum(nil)
		}
	}

	keyLen := h.Length / 8
	encKey := ownerKey[:keyLen]

	decryptedO := make([]byte, len(h.oVal))
	copy(decryptedO, h.oVal)

	if h.V < 3 {
		c, _ := rc4.NewCipher(encKey) //nolint:gosec,errcheck // Key length is always >0 per PDF spec
		c.XORKeyStream(decryptedO, decryptedO)
	} else {
		// 20-pass RC4 decryption loop, XORing the key with 19 - i (so from 19 down to 0)
		for i := 19; i >= 0; i-- {
			tempKey := make([]byte, keyLen)
			for j := range keyLen {
				tempKey[j] = encKey[j] ^ byte(i)
			}
			c, _ := rc4.NewCipher(tempKey) //nolint:gosec,errcheck // Key length is always >0 per PDF spec
			c.XORKeyStream(decryptedO, decryptedO)
		}
	}

	// We now test both the potentially recovered user password and the original input password
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

		// 1. Generate the encryption key (Algorithm 3.1)
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
		if h.V >= 3 {
			for range 50 {
				md2 := md5.New() //nolint:gosec,errcheck // Key length is always >0 per PDF spec
				md2.Write(key[:h.Length/8])
				key = md2.Sum(nil)
			}
		}

		candidateKey := key[:h.Length/8]

		// 2. Verify the key (Algorithm 3.2 - checking against the U string)
		var expectedU []byte
		if h.V >= 3 {
			// ISO 32000-1 Algorithm 3.4 and 3.5 for V >= 3

			// Step 1: Base hash
			md3 := md5.New() //nolint:gosec,errcheck // Key length is always >0 per PDF spec
			md3.Write(paddingString)
			md3.Write(h.id)
			baseHash := md3.Sum(nil)

			// Step 2: Initialize RC4 with EncryptionKey and encrypt the base hash
			c, _ := rc4.NewCipher(candidateKey) //nolint:gosec,errcheck // Key length is always >0 per PDF spec
			expectedU = make([]byte, 16)
			c.XORKeyStream(expectedU, baseHash)

			// Step 3: The 19-Pass Loop
			for i := 1; i <= 19; i++ {
				tempKey := make([]byte, len(candidateKey))
				for j := range candidateKey {
					tempKey[j] = candidateKey[j] ^ byte(i)
				}
				c, _ = rc4.NewCipher(tempKey) //nolint:gosec,errcheck // Key length is always >0 per PDF spec
				c.XORKeyStream(expectedU, expectedU)
			}
		} else {
			c, _ := rc4.NewCipher(candidateKey) //nolint:gosec,errcheck // Key length is always >0 per PDF spec
			expectedU = make([]byte, 32)
			c.XORKeyStream(expectedU, paddingString)
		}

		// Check if our generated expectedU matches the document's actual U string
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

// GetObjKey generates the specific encryption key for an object.
func (h *Handler) GetObjKey(objNum, genNum int) []byte {
	md := md5.New() //nolint:gosec,errcheck // Key length is always >0 per PDF spec
	md.Write(h.EncryptionKey)
	md.Write([]byte{byte(objNum), byte(objNum >> 8), byte(objNum >> 16)}) //nolint:gosec // Expected truncation
	md.Write([]byte{byte(genNum), byte(genNum >> 8)})                     //nolint:gosec // Expected truncation

	if h.V == 4 {
		md.Write([]byte{0x73, 0x41, 0x6C, 0x54}) // 'sAlT'
	}

	key := md.Sum(nil)
	keyLen := min(len(h.EncryptionKey)+5, 16)
	return key[:keyLen]
}

// GetDecryptFunc returns a closure that decrypts data for a specific object.
func (h *Handler) GetDecryptFunc(objNum, genNum int) func([]byte) ([]byte, error) {
	return func(data []byte) ([]byte, error) {
		if len(data) == 0 {
			return data, nil
		}

		key := h.GetObjKey(objNum, genNum)

		if h.V >= 4 && h.CFM == "AESV2" {
			block, err := aes.NewCipher(key)
			if err != nil {
				return nil, err
			}
			if len(data) < aes.BlockSize {
				return data, nil
			}
			iv := data[:aes.BlockSize]
			ciphertext := data[aes.BlockSize:]
			if len(ciphertext)%aes.BlockSize != 0 {
				return data, nil // Malformed
			}
			mode := cipher.NewCBCDecrypter(block, iv)
			mode.CryptBlocks(ciphertext, ciphertext)

			// Remove PKCS7 padding
			if len(ciphertext) > 0 {
				padLen := int(ciphertext[len(ciphertext)-1])
				if padLen > 0 && padLen <= aes.BlockSize && padLen <= len(ciphertext) {
					ciphertext = ciphertext[:len(ciphertext)-padLen]
				}
			}
			return ciphertext, nil
		}

		c, err := rc4.NewCipher(key) //nolint:gosec,errcheck // Key length is always >0 per PDF spec
		if err != nil {
			return nil, err
		}
		out := make([]byte, len(data))
		c.XORKeyStream(out, data)
		return out, nil
	}
}
