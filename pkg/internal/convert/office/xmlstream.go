package office

import (
	"bufio"
	"fmt"
	"io"
)

// MaxXMLTokenBytes caps the bytes of a package part an XML decoder reads
// for one token, a start tag with its attributes, a run of character data,
// a comment: 16 MiB. Anti-DoS guard, not spec-mandated: XML 1.0 bounds no
// token, and encoding/xml holds a whole token before it returns it, so a
// part of a few hundred kilobytes that inflates to one run of white space
// would hold a hundred MiB at once. A token of character data past it
// holds more than four times the characters one paragraph may lay out.
//
// # Corpus evidence
//
//   - All Office corpora: the largest token is an 11.3 MB start tag, the
//     table:consolidation of libreoffice/ods tdf136551.ods content.xml;
//     the next largest is 58 KB (libreoffice/docx crop-roundtrip.docx).
const MaxXMLTokenBytes = 16 << 20

// TokenReader feeds an XML decoder from a stream of a part's bytes,
// failing once the decoder has read more than a cap since the last token
// its caller took. It is an [io.ByteReader], so encoding/xml reads it
// directly, without a buffer of its own; a decoder's charset reader reads
// it through Read. A TokenReader is used by one goroutine.
type TokenReader struct {
	r     *bufio.Reader
	since int64
	limit int64
}

// NewTokenReader returns a TokenReader over r that allows limit bytes a
// token, [MaxXMLTokenBytes] when limit is zero or less.
func NewTokenReader(r io.Reader, limit int64) *TokenReader {
	if limit <= 0 {
		limit = MaxXMLTokenBytes
	}
	return &TokenReader{r: bufio.NewReader(r), limit: limit}
}

// Mark starts the count of the next token: the caller calls it each time
// the decoder returns one.
func (t *TokenReader) Mark() { t.since = 0 }

// ReadByte returns the next byte, failing with an error wrapping
// [ErrInputTooLarge] past the cap of one token.
func (t *TokenReader) ReadByte() (byte, error) {
	if t.since >= t.limit {
		return 0, t.tooLarge()
	}
	b, err := t.r.ReadByte()
	if err == nil {
		t.since++
	}
	return b, err //nolint:wrapcheck // the decoder reports io.EOF as the end of the part: wrapping would hide it
}

// Read reads into p, failing with an error wrapping [ErrInputTooLarge]
// past the cap of one token.
func (t *TokenReader) Read(p []byte) (int, error) {
	room := t.limit - t.since
	if room <= 0 {
		return 0, t.tooLarge()
	}
	if int64(len(p)) > room {
		p = p[:room]
	}
	n, err := t.r.Read(p)
	t.since += int64(n)
	return n, err //nolint:wrapcheck // the decoder reports io.EOF as the end of the part: wrapping would hide it
}

// tooLarge is the error past the cap.
func (t *TokenReader) tooLarge() error {
	return fmt.Errorf("%w: an XML token past %d bytes", ErrInputTooLarge, t.limit)
}
