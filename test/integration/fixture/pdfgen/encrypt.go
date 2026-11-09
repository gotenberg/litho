package pdfgen

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5" //nolint:gosec // the PDF standard security handler is built on MD5 and RC4.
	"crypto/rc4" //nolint:gosec // see above.
	"encoding/binary"
	"errors"
	"sort"
	"strconv"
)

// errEncryption reports an encryption setup pdfgen does not reproduce.
var errEncryption = errors.New("encryption setup pdfgen does not reproduce")

// PikepdfDefaultP is the /P qpdf writes for pikepdf's default
// Permissions: every permission but document assembly (pikepdf's
// modify_assembly defaults to False), so bits 1, 2 and 11 are clear (ISO
// 32000-1, Table 22).
const PikepdfDefaultP = -1028

// Encryption is pikepdf's Encryption(owner=..., user=..., aes=True, R=4):
// the standard security handler, revision 4, with 128-bit AESV2 crypt
// filters for strings and streams and encrypted metadata (ISO 32000-1,
// 7.6.3). Other revisions are not supported.
//
// The file key, /O and /U follow from the passwords, /P and /ID[0] as qpdf
// computes them. qpdf draws each AES initialization vector at random;
// pdfgen derives it instead, so the output is deterministic: the IV of the
// k-th string or stream encrypted in object n (k counting from 0 in write
// order) is MD5("pdfgen AESV2 IV" || n || 0 || k), with n and k as 4-byte
// and the generation as a 2-byte little-endian integer. Any IV is valid,
// so a reader decrypts the file exactly as qpdf's own.
type Encryption struct {
	// Owner is the owner password.
	Owner string
	// User is the user password; empty opens the file without a prompt.
	User string
	// P is the permission flags; PikepdfDefaultP for pikepdf's defaults.
	P int32
}

type encryptState struct {
	key      []byte
	o, u     []byte
	p        int32
	id1, id2 String
	dictID   int
}

// passwordPadding is the 32-byte padding string of ISO 32000-1, 7.6.3.3,
// Algorithm 2, step a.
var passwordPadding = []byte{
	0x28, 0xBF, 0x4E, 0x5E, 0x4E, 0x75, 0x8A, 0x41, 0x64, 0x00, 0x4E, 0x56, 0xFF, 0xFA, 0x01, 0x08,
	0x2E, 0x2E, 0x00, 0xB6, 0xD0, 0x68, 0x3E, 0x80, 0x2F, 0x0C, 0xA9, 0xFE, 0x64, 0x53, 0x69, 0x7A,
}

func padPassword(pw string) []byte {
	out := []byte(pw)
	if len(out) > 32 {
		out = out[:32]
	}
	return append(out, passwordPadding[:32-len(out)]...)
}

// iterateMD5 hashes h's digest 50 more times over its first 16 bytes
// (Algorithm 2 step h, Algorithm 3 step c, for R >= 3).
func iterateMD5(sum [16]byte) []byte {
	for range 50 {
		sum = md5.Sum(sum[:]) //nolint:gosec // see the import.
	}
	return sum[:]
}

// iterateRC4 runs RC4 20 times, keyed with key XOR i for i = 0 to 19
// (Algorithm 3 step f, Algorithm 5 step e).
func iterateRC4(data, key []byte) {
	k := make([]byte, len(key))
	for i := range 20 {
		for j := range key {
			k[j] = key[j] ^ byte(i)
		}
		c, _ := rc4.NewCipher(k) //nolint:gosec,errcheck // see the import; a 16-byte key never errors.
		c.XORKeyStream(data, data)
	}
}

// setupEncryption is QPDFWriter::setEncryptionParameters for R4 AESV2: it
// fixes the /ID pair first, since the key depends on /ID[0].
func (w *writer) setupEncryption() error {
	if !w.opts.StaticID {
		return errors.Join(errEncryption, errors.New("qpdf refuses a deterministic /ID with encryption: set StaticID"))
	}
	if w.opts.GenerateObjectStreams || w.opts.Linearize {
		return errors.Join(errEncryption, errors.New("object streams and linearization are not supported with encryption"))
	}
	e := w.opts.Encryption
	w.setMinVersion("1.6", 0)
	id2 := staticID
	id1 := w.originalID1()
	if len(id1) == 0 {
		id1 = id2
	}

	// Algorithm 3: /O.
	okey := iterateMD5(md5.Sum(padPassword(ownerOrUser(e)))) //nolint:gosec // see the import.
	o := padPassword(e.User)
	iterateRC4(o, okey)

	// Algorithm 2: the file key.
	h := md5.New() //nolint:gosec // see the import.
	h.Write(padPassword(e.User))
	h.Write(o)
	var p [4]byte
	binary.LittleEndian.PutUint32(p[:], uint32(e.P)) //nolint:gosec // /P is a signed 32-bit field stored as its bit pattern.
	h.Write(p[:])
	h.Write(id1)
	var first [16]byte
	copy(first[:], h.Sum(nil))
	key := iterateMD5(first)

	// Algorithm 5: /U, padded as qpdf pads it.
	uh := md5.New() //nolint:gosec // see the import.
	uh.Write(passwordPadding)
	uh.Write(id1)
	u := uh.Sum(nil)
	iterateRC4(u, key)
	for i := 16; i < 32; i++ {
		u = append(u, byte((i*i)%0xff))
	}

	w.enc = &encryptState{key: key, o: o, u: u, p: e.P, id1: id1, id2: id2}
	return nil
}

func ownerOrUser(e *Encryption) string {
	if e.Owner == "" {
		return e.User
	}
	return e.Owner
}

// setDataKey is QPDFWriter::setDataKey: Algorithm 1 for AES, the file key
// extended with the object number, generation 0 and "sAlT".
func (w *writer) setDataKey(id int) {
	if w.enc == nil {
		return
	}
	b := append([]byte{}, w.enc.key...)
	b = append(b, byte(id), byte(id>>8), byte(id>>16), 0, 0) //nolint:gosec // the low three bytes, as the algorithm takes them.
	b = append(b, "sAlT"...)
	sum := md5.Sum(b) //nolint:gosec // see the import.
	w.dataKey = sum[:]
	w.curObj, w.ivCounter = id, 0
}

// encryptAES is qpdf's Pl_AES_PDF in encrypt mode: the IV, then AES-128-CBC
// over data padded to a whole block with PKCS #5 padding (always at least
// one byte), under the current object key. The IV is derived as
// [Encryption] documents.
func (w *writer) encryptAES(data []byte) []byte {
	var seed []byte
	seed = append(seed, "pdfgen AESV2 IV"...)
	seed = binary.LittleEndian.AppendUint32(seed, uint32(w.curObj))    //nolint:gosec // object numbers are positive.
	seed = binary.LittleEndian.AppendUint16(seed, 0)                   // generation
	seed = binary.LittleEndian.AppendUint32(seed, uint32(w.ivCounter)) //nolint:gosec // a small counter.
	w.ivCounter++
	iv := md5.Sum(seed) //nolint:gosec // see the import.

	pad := aes.BlockSize - len(data)%aes.BlockSize
	plain := append(append([]byte{}, data...), make([]byte, pad)...)
	for i := len(data); i < len(plain); i++ {
		plain[i] = byte(pad)
	}
	block, err := aes.NewCipher(w.dataKey)
	if err != nil && w.err == nil {
		w.err = err
		return data
	}
	out := append([]byte{}, iv[:]...)
	ct := make([]byte, len(plain))
	cipher.NewCBCEncrypter(block, iv[:]).CryptBlocks(ct, plain)
	return append(out, ct...)
}

// writeEncryptionDictionary is QPDFWriter::writeEncryptionDictionary: the
// entries in key order, written unencrypted as the next object.
func (w *writer) writeEncryptionDictionary() {
	e := w.enc
	e.dictID = w.nextObjID
	w.nextObjID++
	entries := map[string]string{
		"/Filter": "/Standard",
		"/V":      "4",
		"/Length": "128",
		"/R":      "4",
		"/P":      strconv.Itoa(int(e.p)),
		"/O":      hexString(e.o),
		"/U":      hexString(e.u),
		"/StmF":   "/StdCF",
		"/StrF":   "/StdCF",
		"/CF":     "<< /StdCF << /AuthEvent /DocOpen /CFM /AESV2 /Length 16 >> >>",
	}
	keys := make([]string, 0, len(entries))
	for k := range entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	w.openObject(e.dictID)
	w.writeString("<<")
	for _, k := range keys {
		w.writeString(" " + k + " " + entries[k])
	}
	w.writeString(" >>")
	w.closeObject()
}
