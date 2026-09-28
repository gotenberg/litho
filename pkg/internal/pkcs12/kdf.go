package pkcs12

import (
	"crypto/sha1" //nolint:gosec // RFC 7292, Appendix B and C name SHA-1 for the legacy key derivation and MAC; reading an old store needs it.
	"crypto/sha256"
	"crypto/sha512"
	"encoding/asn1"
	"encoding/binary"
	"fmt"
	"hash"
	"unicode/utf16"
)

// Purposes of the RFC 7292, Appendix B.3 key derivation: the ID byte that
// makes a key, an IV and a MAC key from one password and salt differ.
const (
	kdfKey byte = 1
	kdfIV  byte = 2
	kdfMAC byte = 3
)

// digest is a hash function the RFC 7292 key derivation and MAC run on.
type digest struct {
	name string
	new  func() hash.Hash
	// size and block are u and v of RFC 7292, Appendix B.2: the output and
	// the input block of the compression function, in bytes.
	size, block int
}

// Hash algorithm identifiers (RFC 3279, 2.2.1 for SHA-1; RFC 5754, 2 for the
// SHA-2 family).
var (
	oidSHA1   = asn1.ObjectIdentifier{1, 3, 14, 3, 2, 26}
	oidSHA256 = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 1}
	oidSHA384 = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 2}
	oidSHA512 = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 3}
	oidSHA224 = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 4}
)

var (
	sha1Digest   = digest{"SHA-1", sha1.New, sha1.Size, sha1.BlockSize}
	sha224Digest = digest{"SHA-224", sha256.New224, sha256.Size224, sha256.BlockSize}
	sha256Digest = digest{"SHA-256", sha256.New, sha256.Size, sha256.BlockSize}
	sha384Digest = digest{"SHA-384", sha512.New384, sha512.Size384, sha512.BlockSize}
	sha512Digest = digest{"SHA-512", sha512.New, sha512.Size, sha512.BlockSize}
)

// digestOf returns the hash function of a digest algorithm identifier.
func digestOf(oid asn1.ObjectIdentifier) (digest, error) {
	switch {
	case oid.Equal(oidSHA1):
		return sha1Digest, nil
	case oid.Equal(oidSHA224):
		return sha224Digest, nil
	case oid.Equal(oidSHA256):
		return sha256Digest, nil
	case oid.Equal(oidSHA384):
		return sha384Digest, nil
	case oid.Equal(oidSHA512):
		return sha512Digest, nil
	}
	return digest{}, fmt.Errorf("%w: digest algorithm %s", ErrUnsupported, oid)
}

// bmpPassword returns password as RFC 7292, Appendix B.1 feeds it to the
// key derivation: a BMPString, big-endian UTF-16 with a two-byte terminating
// zero. A character outside the BMP takes a surrogate pair, as OpenSSL
// encodes it.
func bmpPassword(password string) []byte {
	units := utf16.Encode([]rune(password))
	out := make([]byte, 0, 2*len(units)+2)
	for _, u := range units {
		out = binary.BigEndian.AppendUint16(out, u)
	}
	return append(out, 0, 0)
}

// deriveKey returns n bytes of the RFC 7292, Appendix B.2 key derivation of
// password (already a BMPString) and salt, for purpose id, iterating h
// iterations times. The caller bounds iterations and n.
func deriveKey(h digest, id byte, password, salt []byte, iterations, n int) []byte {
	u, v := h.size, h.block
	d := make([]byte, v)
	for i := range d {
		d[i] = id
	}
	// S and P: salt and password repeated to a whole number of v-byte
	// blocks, empty when they are.
	fill := func(src []byte) []byte {
		if len(src) == 0 {
			return nil
		}
		out := make([]byte, v*((len(src)+v-1)/v))
		for i := range out {
			out[i] = src[i%len(src)]
		}
		return out
	}
	ib := append(fill(salt), fill(password)...)

	out := make([]byte, 0, n+u)
	b := make([]byte, v)
	for len(out) < n {
		hh := h.new()
		hh.Write(d)
		hh.Write(ib)
		a := hh.Sum(nil)
		for r := 1; r < iterations; r++ {
			hh.Reset()
			hh.Write(a)
			a = hh.Sum(a[:0])
		}
		out = append(out, a...)
		if len(out) >= n {
			break
		}
		for i := range b {
			b[i] = a[i%u]
		}
		// I_j = (I_j + B + 1) mod 2^(8v), each v-byte block of I read as a
		// big-endian integer.
		for j := 0; j < len(ib); j += v {
			carry := 1
			for k := v - 1; k >= 0; k-- {
				sum := int(ib[j+k]) + int(b[k]) + carry
				ib[j+k] = byte(sum) //nolint:gosec // sum is at most 511: the low byte stays, the carry takes bit 8.
				carry = sum >> 8
			}
		}
	}
	return out[:n]
}
