package offcrypto

import (
	"bytes"
	"compress/flate"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // the encryptors run the schemes forward, SHA-1 included
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"hash"
	"math/rand/v2"
	"testing"
	"unicode/utf16"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/blowfish"
)

// The test encryptors run each scheme forward, so every branch of the
// decryptors is covered without a sample. Their randomness is seeded:
// every run writes the same bytes.

// testRand returns a deterministic byte source.
func testRand(seed uint64) func(n int) []byte {
	r := rand.New(rand.NewChaCha8([32]byte{byte(seed), byte(seed >> 8)})) //nolint:gosec // deterministic test bytes, not keys
	return func(n int) []byte {
		b := make([]byte, n)
		for i := range b {
			b[i] = byte(r.Uint32())
		}
		return b
	}
}

// cfbStream is one root-level stream of a test compound file.
type cfbStream struct {
	name string
	data []byte
}

// buildCFB writes a compound file (MS-CFB) of the given version holding
// streams at the root, those under 4096 bytes in the mini stream. The
// directory is a right-leaning chain, which MS-CFB, 2.6.4 allows readers to
// accept.
func buildCFB(t testing.TB, major uint16, streams []cfbStream) []byte {
	t.Helper()
	shift := uint16(9)
	if major == 4 {
		shift = 12
	}
	ss := 1 << shift
	per := ss / 4
	le := binary.LittleEndian
	type placed struct {
		start uint32
		mini  bool
	}
	var mini []byte
	var miniFAT []uint32
	pos := make([]placed, len(streams))
	for i, s := range streams {
		if len(s.data) >= 4096 || len(s.data) == 0 {
			continue
		}
		start := uint32(len(mini) / 64)
		n := (len(s.data) + 63) / 64
		for j := range n {
			next := uint32(cfbEndOfChain)
			if j < n-1 {
				next = start + uint32(j) + 1
			}
			miniFAT = append(miniFAT, next)
		}
		mini = append(mini, s.data...)
		mini = append(mini, make([]byte, n*64-len(s.data))...)
		pos[i] = placed{start, true}
	}
	var sectors [][]byte
	var fat []uint32
	addChain := func(b []byte) uint32 {
		if len(b) == 0 {
			return cfbEndOfChain
		}
		start := uint32(len(sectors))
		n := (len(b) + ss - 1) / ss
		for j := range n {
			sec := make([]byte, ss)
			copy(sec, b[j*ss:])
			sectors = append(sectors, sec)
			next := uint32(cfbEndOfChain)
			if j < n-1 {
				next = start + uint32(j) + 1
			}
			fat = append(fat, next)
		}
		return start
	}
	dir := make([]byte, cfbDirEntryBytes*(1+len(streams)))
	entry := func(i int, name string, typ byte, right, child, start uint32, size uint64) {
		e := dir[i*cfbDirEntryBytes:]
		u := utf16.Encode([]rune(name))
		for j, c := range u {
			le.PutUint16(e[2*j:], c)
		}
		le.PutUint16(e[0x40:], uint16(2*len(u)+2))
		e[0x42], e[0x43] = typ, 1
		le.PutUint32(e[0x44:], cfbNoStream)
		le.PutUint32(e[0x48:], right)
		le.PutUint32(e[0x4C:], child)
		le.PutUint32(e[0x74:], start)
		le.PutUint64(e[0x78:], size)
	}
	firstDir := addChain(dir)
	miniFATBytes := make([]byte, 4*len(miniFAT))
	for i, v := range miniFAT {
		le.PutUint32(miniFATBytes[4*i:], v)
	}
	firstMiniFAT := addChain(miniFATBytes)
	miniStart := addChain(mini)
	for i, s := range streams {
		if !pos[i].mini {
			pos[i].start = addChain(s.data)
		}
	}
	child := uint32(cfbNoStream)
	if len(streams) > 0 {
		child = 1
	}
	rootStart := miniStart
	if len(mini) == 0 {
		rootStart = cfbEndOfChain
	}
	entry(0, "Root Entry", cfbTypeRoot, cfbNoStream, child, rootStart, uint64(len(mini)))
	for i, s := range streams {
		right := uint32(cfbNoStream)
		if i < len(streams)-1 {
			right = uint32(i + 2)
		}
		start := pos[i].start
		if len(s.data) == 0 {
			start = cfbEndOfChain
		}
		entry(i+1, s.name, cfbTypeStream, right, cfbNoStream, start, uint64(len(s.data)))
	}
	// The directory was placed before its entries were written: copy them
	// into its sectors.
	for j := 0; j*ss < len(dir); j++ {
		copy(sectors[int(firstDir)+j], dir[j*ss:])
	}
	nFAT := 1
	for (len(sectors)+nFAT+per-1)/per > nFAT {
		nFAT++
	}
	if nFAT > cfbHeaderDIFAT {
		t.Fatalf("buildCFB: %d FAT sectors need a DIFAT", nFAT)
	}
	fatStart := uint32(len(sectors))
	for range nFAT {
		fat = append(fat, 0xFFFFFFFD)
	}
	for len(fat)%per != 0 {
		fat = append(fat, cfbFreeSect)
	}
	for j := range nFAT {
		sec := make([]byte, ss)
		for k := range per {
			le.PutUint32(sec[4*k:], fat[j*per+k])
		}
		sectors = append(sectors, sec)
	}
	hdr := make([]byte, ss)
	copy(hdr, cfbSignature)
	le.PutUint16(hdr[0x18:], 0x3E)
	le.PutUint16(hdr[0x1A:], major)
	le.PutUint16(hdr[0x1C:], 0xFFFE)
	le.PutUint16(hdr[0x1E:], shift)
	le.PutUint16(hdr[0x20:], 6)
	le.PutUint32(hdr[0x2C:], uint32(nFAT))
	le.PutUint32(hdr[0x30:], firstDir)
	le.PutUint32(hdr[0x38:], 4096)
	le.PutUint32(hdr[0x3C:], firstMiniFAT)
	le.PutUint32(hdr[0x40:], uint32((len(miniFATBytes)+ss-1)/ss))
	le.PutUint32(hdr[0x44:], cfbEndOfChain)
	for i := range cfbHeaderDIFAT {
		v := uint32(cfbFreeSect)
		if i < nFAT {
			v = fatStart + uint32(i)
		}
		le.PutUint32(hdr[0x4C+4*i:], v)
	}
	out := bytes.NewBuffer(hdr)
	for _, s := range sectors {
		out.Write(s)
	}
	return out.Bytes()
}

// encryptedOOXML wraps an EncryptionInfo and EncryptedPackage in a
// compound file, as Office writes them (MS-OFFCRYPTO, 2.3.4.4).
func encryptedOOXML(t testing.TB, info, pkg []byte) []byte {
	t.Helper()
	return buildCFB(t, 3, []cfbStream{{"EncryptionInfo", info}, {"EncryptedPackage", pkg}})
}

// agileOptions are the parameters of a test Agile descriptor.
type agileOptions struct {
	hash      string
	keyBits   int
	spinCount int
}

func agileHash(name string) func() hash.Hash {
	c, err := checkAgileParams("test", agileParams{
		SaltSize: 1, BlockSize: 16, KeyBits: 128, CipherAlgorithm: "AES", CipherChaining: "ChainingModeCBC",
		HashAlgorithm: name, SaltValue: "AA==", HashSize: map[string]int{"SHA1": 20, "SHA256": 32, "SHA384": 48, "SHA512": 64}[name],
	})
	if err != nil {
		panic(err)
	}
	return c.newHash
}

func cbcEncrypt(key, iv, plain []byte) []byte {
	block, err := aes.NewCipher(key)
	if err != nil {
		panic(err)
	}
	p := append(bytes.Clone(plain), make([]byte, (aes.BlockSize-len(plain)%aes.BlockSize)%aes.BlockSize)...)
	out := make([]byte, len(p))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(out, p)
	return out
}

// agileEncrypt encrypts pkg with Agile encryption (MS-OFFCRYPTO, 2.3.4.10 to
// 2.3.4.15) and returns the EncryptionInfo and EncryptedPackage streams.
func agileEncrypt(t testing.TB, pkg []byte, password string, o agileOptions, seed uint64) (info, encrypted []byte) {
	t.Helper()
	rnd := testRand(seed)
	newHash := agileHash(o.hash)
	hashSize := newHash().Size()
	keyBytes := o.keyBits / 8
	keySalt, pwSalt := rnd(16), rnd(16)
	secret := rnd(keyBytes)
	h, err := iteratedHash(context.Background(), newHash, pwSalt, password, o.spinCount)
	if err != nil {
		t.Fatal(err)
	}
	derive := func(block []byte) []byte { return fitBytes(hashOf(newHash, h, block), keyBytes) }
	iv := fitBytes(pwSalt, 16)
	verifier := rnd(16)
	encInput := cbcEncrypt(derive(blockVerifierInput), iv, verifier)
	encHash := cbcEncrypt(derive(blockVerifierHash), iv, hashOf(newHash, verifier))
	encKey := cbcEncrypt(derive(blockKeyValue), iv, secret)

	encrypted = binary.LittleEndian.AppendUint64(nil, uint64(len(pkg)))
	var idx [4]byte
	for seg, off := 0, 0; off < len(pkg); seg, off = seg+1, off+agileSegment {
		binary.LittleEndian.PutUint32(idx[:], uint32(seg))
		segIV := fitBytes(hashOf(newHash, keySalt, idx[:]), 16)
		encrypted = append(encrypted, cbcEncrypt(secret, segIV, pkg[off:min(off+agileSegment, len(pkg))])...)
	}
	hmacKey := rnd(hashSize)
	mac := hmac.New(newHash, hmacKey)
	mac.Write(encrypted)
	encHMACKey := cbcEncrypt(secret, fitBytes(hashOf(newHash, keySalt, blockHMACKey), 16), hmacKey)
	encHMACValue := cbcEncrypt(secret, fitBytes(hashOf(newHash, keySalt, blockHMACValue), 16), mac.Sum(nil))

	b64 := base64.StdEncoding.EncodeToString
	params := func(salt []byte) string {
		return fmt.Sprintf(`saltSize="16" blockSize="16" keyBits="%d" hashSize="%d" cipherAlgorithm="AES" cipherChaining="ChainingModeCBC" hashAlgorithm="%s" saltValue="%s"`,
			o.keyBits, hashSize, o.hash, b64(salt))
	}
	xmlDoc := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<encryption xmlns="http://schemas.microsoft.com/office/2006/encryption" xmlns:p="http://schemas.microsoft.com/office/2006/keyEncryptor/password"><keyData %s/><dataIntegrity encryptedHmacKey="%s" encryptedHmacValue="%s"/><keyEncryptors><keyEncryptor uri="http://schemas.microsoft.com/office/2006/keyEncryptor/password"><p:encryptedKey spinCount="%d" %s encryptedVerifierHashInput="%s" encryptedVerifierHashValue="%s" encryptedKeyValue="%s"/></keyEncryptor></keyEncryptors></encryption>`,
		params(keySalt), b64(encHMACKey), b64(encHMACValue), o.spinCount, params(pwSalt), b64(encInput), b64(encHash), b64(encKey))
	info = append([]byte{4, 0, 4, 0, 0x40, 0, 0, 0}, xmlDoc...)
	return info, encrypted
}

// standardEncrypt encrypts pkg with Standard encryption (MS-OFFCRYPTO,
// 2.3.4.5 to 2.3.4.9), version major.2, and returns the EncryptionInfo and
// EncryptedPackage streams.
func standardEncrypt(t testing.TB, pkg []byte, password string, major uint16, algID uint32, seed uint64) (info, encrypted []byte) {
	t.Helper()
	rnd := testRand(seed)
	bits := map[uint32]int{0: 128, algAES128: 128, algAES192: 192, algAES256: 256}[algID]
	salt := rnd(16)
	key, err := standardKey(context.Background(), password, salt, bits/8)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := aes.NewCipher(key)
	ecb := func(p []byte) []byte {
		p = append(bytes.Clone(p), make([]byte, (16-len(p)%16)%16)...)
		out := make([]byte, len(p))
		for i := 0; i < len(p); i += 16 {
			block.Encrypt(out[i:], p[i:])
		}
		return out
	}
	verifier := rnd(16)
	vh := sha1.Sum(verifier)
	le := binary.LittleEndian
	hdr := le.AppendUint32(nil, flagCryptoAPI|flagAES)
	hdr = le.AppendUint32(hdr, 0)
	hdr = le.AppendUint32(hdr, algID)
	hdr = le.AppendUint32(hdr, algSHA1)
	hdr = le.AppendUint32(hdr, uint32(bits))
	hdr = le.AppendUint32(hdr, 0x18)
	hdr = le.AppendUint32(hdr, 0)
	hdr = le.AppendUint32(hdr, 0)
	hdr = append(hdr, utf16LE("Microsoft Enhanced RSA and AES Cryptographic Provider\x00")...)
	info = le.AppendUint16(nil, major)
	info = le.AppendUint16(info, 2)
	info = le.AppendUint32(info, flagCryptoAPI|flagAES)
	info = le.AppendUint32(info, uint32(len(hdr)))
	info = append(info, hdr...)
	info = le.AppendUint32(info, 16)
	info = append(info, salt...)
	info = append(info, ecb(verifier)...)
	info = le.AppendUint32(info, 20)
	info = append(info, ecb(vh[:])...)
	encrypted = le.AppendUint64(nil, uint64(len(pkg)))
	encrypted = append(encrypted, ecb(pkg)...)
	return info, encrypted
}

// odfOptions are the parameters of a test encrypted ODF entry.
type odfOptions struct {
	cipher      string // "blowfish", "aes-cbc", "aes-gcm"
	aesBits     int
	argon2      bool
	sha256Start bool
	checksum    string // "", "sha1", "sha256"
	starOffice  bool   // StarOffice's SHA-1 for the start key and checksum
	cp1252      bool   // the start key from the Windows-1252 password
	iterations  int
	omitSize    bool
}

// odfEncrypt deflates and encrypts plain as LibreOffice writes an ODF entry
// (ODF 1.3 Part 2, 3.4) and returns the stored bytes and the manifest
// attributes.
func odfEncrypt(t testing.TB, plain []byte, password string, o odfOptions, seed uint64) ([]byte, ODFParams) {
	t.Helper()
	rnd := testRand(seed)
	var z bytes.Buffer
	w, _ := flate.NewWriter(&z, flate.BestCompression)
	w.Write(plain)
	w.Close()
	deflated := z.Bytes()

	b64 := base64.StdEncoding.EncodeToString
	p := ODFParams{Size: fmt.Sprint(len(plain)), Salt: b64(rnd(16))}
	if o.omitSize {
		p.Size = ""
	}
	salt, _ := base64.StdEncoding.DecodeString(p.Salt)
	pw := []byte(password)
	if o.cp1252 {
		pw = bytes.ReplaceAll(pw, []byte("é"), []byte{0xE9})
	}
	var start []byte
	switch {
	case o.sha256Start:
		s := sha256.Sum256(pw)
		start = s[:]
		p.StartKeyGenerationName, p.StartKeySize = odfSHA256DSig, "32"
	case o.starOffice:
		s := starOfficeSHA1(pw)
		start = s[:]
	default:
		s := sha1.Sum(pw)
		start = s[:]
		p.StartKeyGenerationName = odfSHA1
	}
	keyBytes := 16
	if o.cipher != "blowfish" {
		keyBytes = o.aesBits / 8
	}
	var key []byte
	if o.argon2 {
		p.KeyDerivationName = odfArgon2IDLO
		p.Argon2Iterations, p.Argon2Memory, p.Argon2Lanes = "2", "256", "2"
		key = argon2.IDKey(start, salt, 2, 256, 2, uint32(keyBytes))
	} else {
		p.KeyDerivationName = odfPBKDF2
		p.IterationCount = fmt.Sprint(o.iterations)
		var err error
		key, err = pbkdf2SHA1(context.Background(), start, salt, o.iterations, keyBytes)
		if err != nil {
			t.Fatal(err)
		}
	}
	p.KeySize = fmt.Sprint(keyBytes)
	head := deflated[:min(len(deflated), odfChecksumBytes)]
	switch o.checksum {
	case "sha1":
		p.ChecksumType = odfSHA11K
		s := sha1.Sum(head)
		if o.starOffice {
			s = starOfficeSHA1(head)
		}
		p.Checksum = b64(s[:])
	case "sha256":
		p.ChecksumType = odfSHA2561KURI
		s := sha256.Sum256(head)
		p.Checksum = b64(s[:])
	}
	var stored []byte
	switch o.cipher {
	case "blowfish":
		p.AlgorithmName = odfBlowfish
		iv := rnd(8)
		p.InitialisationVector = b64(iv)
		b, _ := blowfish.NewCipher(key)
		stored = cfbEncrypt(b, iv, deflated)
	case "aes-cbc":
		p.AlgorithmName = fmt.Sprintf("http://www.w3.org/2001/04/xmlenc#aes%d-cbc", o.aesBits)
		iv := rnd(16)
		p.InitialisationVector = b64(iv)
		// xmlenc-core, 5.2: arbitrary padding bytes, the last one counting
		// them.
		n := 16 - len(deflated)%16
		pad := rnd(n)
		pad[n-1] = byte(n)
		stored = cbcEncrypt(key, iv, append(bytes.Clone(deflated), pad...))
	case "aes-gcm":
		p.AlgorithmName = fmt.Sprintf("http://www.w3.org/2009/xmlenc11#aes%d-gcm", o.aesBits)
		iv := rnd(12)
		p.InitialisationVector = b64(iv)
		b, _ := aes.NewCipher(key)
		g, _ := cipher.NewGCM(b)
		stored = g.Seal(bytes.Clone(iv), iv, deflated, nil)
	}
	return stored, p
}

// cfbEncrypt encrypts src in full-block cipher feedback mode.
func cfbEncrypt(b cipher.Block, iv, src []byte) []byte {
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
		copy(reg, out[off:end])
	}
	return out
}
