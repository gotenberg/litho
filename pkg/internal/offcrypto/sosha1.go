package offcrypto

import (
	"crypto/sha1" //nolint:gosec // ODF 1.3 Part 2, 3.4.2 fixes SHA-1 for the legacy start key and checksum
	"encoding/binary"
	"math/bits"
)

// starOfficeSHA1 is the SHA-1 of StarOffice, OpenOffice.org and LibreOffice
// before 3.5 (sal/rtl/digest.cxx, rtl_digest_SHA1). Its padding sends the
// length to an extra block when the message's last block holds 52 to 55
// bytes, where FIPS 180-4, 5.1.1 fits it in the same block; every other
// message hashes as SHA-1 does. ODF files those versions wrote carry start
// keys and checksums made with it, which LibreOffice still tries
// (package/source/zippackage/ZipPackageStream.cxx, tdf#114939).
func starOfficeSHA1(msg []byte) [sha1.Size]byte {
	if r := len(msg) % 64; r < 52 || r > 55 {
		return sha1.Sum(msg) //nolint:gosec // the StarOffice digest is SHA-1 outside its padding bug
	}
	h := [5]uint32{0x67452301, 0xEFCDAB89, 0x98BADCFE, 0x10325476, 0xC3D2E1F0}
	full := len(msg) / 64 * 64
	for off := 0; off < full; off += 64 {
		sha1Block(&h, msg[off:off+64])
	}
	var last [128]byte
	n := copy(last[:], msg[full:])
	last[n] = 0x80
	// The buggy padding closes the first block with zeros and puts the
	// bit length at the end of a second, all-zero one.
	binary.BigEndian.PutUint64(last[120:], uint64(len(msg))*8)
	sha1Block(&h, last[:64])
	sha1Block(&h, last[64:])
	var out [sha1.Size]byte
	for i, v := range h {
		binary.BigEndian.PutUint32(out[4*i:], v)
	}
	return out
}

// sha1Block runs the SHA-1 compression function on one 64-byte block
// (FIPS 180-4, 6.1.2).
func sha1Block(h *[5]uint32, p []byte) {
	var w [80]uint32
	for i := range 16 {
		w[i] = binary.BigEndian.Uint32(p[4*i:])
	}
	for i := 16; i < 80; i++ {
		w[i] = bits.RotateLeft32(w[i-3]^w[i-8]^w[i-14]^w[i-16], 1)
	}
	a, b, c, d, e := h[0], h[1], h[2], h[3], h[4]
	for i := range 80 {
		var f, k uint32
		switch {
		case i < 20:
			f, k = (b&c)|(^b&d), 0x5A827999
		case i < 40:
			f, k = b^c^d, 0x6ED9EBA1
		case i < 60:
			f, k = (b&c)|(b&d)|(c&d), 0x8F1BBCDC
		default:
			f, k = b^c^d, 0xCA62C1D6
		}
		t := bits.RotateLeft32(a, 5) + f + e + k + w[i]
		a, b, c, d, e = t, a, bits.RotateLeft32(b, 30), c, d
	}
	h[0] += a
	h[1] += b
	h[2] += c
	h[3] += d
	h[4] += e
}
