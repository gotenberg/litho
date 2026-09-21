package pdf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
)

// encryptDictPDF builds a one-page document whose fourth object is the given
// /Encrypt dictionary, referenced from the trailer.
func encryptDictPDF(encrypt string) []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		encrypt,
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R /Encrypt 4 0 R "+
		"/ID [<0123456789abcdef0123456789abcdef> <0123456789abcdef0123456789abcdef>] >>\n"+
		"startxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}

// zeroString is a 32-byte PDF string, the length the legacy /U and /O entries
// carry, so a fixture varies only in the entry under test.
const zeroString = "<0000000000000000000000000000000000000000000000000000000000000000>"

// TestOpen_TruncatedAES256DictReturnsTheUnsupportedSentinel pins the last
// boundary-translated sentinel that nothing asserted.
//
// [ErrUnsupportedEncryption] is translated at the Reader boundary from
// crypto.ErrUnsupportedEncryption, so it is one of the sentinels that can be
// severed without any internal test noticing: the internal package keeps
// returning its own value and stays green.
//
// Per ISO 32000-2, 7.6.4.3 Algorithm 2.A, /U and /O each split into a 32-byte
// hash, an 8-byte validation salt and an 8-byte key salt, and the file key is
// recovered from a 32-byte /UE. A dictionary whose strings cannot hold those is
// malformed: no password exists that would open it.
//
// The second assertion carries the contract. This sentinel exists so a caller
// can tell "malformed dictionary, no password will ever work" from "wrong
// password, ask the user again". An error matching both tells the caller
// nothing, so asserting only the first would pass on a translation that had
// stopped discriminating.
func TestOpen_TruncatedAES256DictReturnsTheUnsupportedSentinel(t *testing.T) {
	t.Parallel()

	data := encryptDictPDF("<< /Filter /Standard /V 5 /R 6 /Length 256 /P -1 " +
		"/U <00> /O <00> /UE <00> /OE <00> >>")
	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err == nil {
		r.Close()
		t.Fatal("Open succeeded, want it refused: the fixture's /U, /O and /UE are too short to drive AES-256")
	}
	if !errors.Is(err, ErrUnsupportedEncryption) {
		t.Errorf("err = %v, want errors.Is(err, ErrUnsupportedEncryption)", err)
	}
	if errors.Is(err, ErrInvalidPassword) {
		t.Errorf("err = %v, also matches ErrInvalidPassword: a caller cannot tell a "+
			"malformed encryption dictionary from a password worth retrying", err)
	}
}

// TestOpen_OutOfRangeKeyLengthIsRejectedNotPanicked pins a crafted /Length on
// the legacy security-handler path.
//
// ISO 32000-1, Table 20 bounds the key to 40 through 128 bits in multiples of
// 8. Every legacy key is sliced out of a 16-byte MD5 digest, so /Length 256
// sliced it to 32, /Length 136 to 17 and a negative length to -1. All three
// panicked. Internal code never panics, and the recover boundary in pkg/pdf is
// a safety net rather than a license, so the dictionary is now rejected before
// the slice.
//
// /Length 128 is the control and it is what makes this test able to fail for
// the right reason: it must still reach the password comparison and report
// ErrInvalidPassword. A guard that rejected everything would satisfy every
// other case here and break only this one.
func TestOpen_OutOfRangeKeyLengthIsRejectedNotPanicked(t *testing.T) {
	t.Parallel()

	legacy := func(length string) []byte {
		return encryptDictPDF("<< /Filter /Standard /V 2 /R 3 /Length " + length +
			" /P -1 /U " + zeroString + " /O " + zeroString + " >>")
	}

	rejected := []struct{ name, length string }{
		{"four times the legal maximum", "256"},
		{"one step over the legal maximum", "136"},
		{"below the legal minimum", "8"},
		{"zero", "0"},
		{"negative", "-8"},
	}
	for _, tt := range rejected {
		t.Run(tt.name, func(t *testing.T) {
			data := legacy(tt.length)
			r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
			if err == nil {
				r.Close()
				t.Fatalf("Open with /Length %s succeeded, want it refused", tt.length)
			}
			if errors.Is(err, ErrInternalPanic) {
				t.Errorf("/Length %s panicked and was recovered at the boundary: %v", tt.length, err)
			}
			if !errors.Is(err, ErrUnsupportedEncryption) {
				t.Errorf("/Length %s: err = %v, want errors.Is(err, ErrUnsupportedEncryption)", tt.length, err)
			}
		})
	}

	t.Run("the legal maximum still reaches the password check", func(t *testing.T) {
		data := legacy("128")
		r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
		if err == nil {
			r.Close()
			t.Fatal("Open with /Length 128 succeeded, want the zeroed /U to be refused as a wrong password")
		}
		if !errors.Is(err, ErrInvalidPassword) {
			t.Errorf("/Length 128: err = %v, want errors.Is(err, ErrInvalidPassword): the guard "+
				"is rejecting a key length ISO 32000-1 Table 20 allows", err)
		}
	})
}
