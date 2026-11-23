package office

import (
	"bufio"
	"fmt"
	"io"

	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"

	"github.com/gotenberg/litho/pkg/internal/limits"
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
// it through Read. A stream that opens with a UTF-16 byte order mark is
// decoded to UTF-8 as it is read, the mark left out (XML 1.0, 4.3.3: an
// entity in UTF-16 begins with the mark, which encoding/xml does not
// read), so a caller's charset reader takes a UTF-16 declaration as
// UTF-8. A TokenReader is used by one goroutine.
type TokenReader struct {
	r     *bufio.Reader
	since int64
	limit int64
	// sniffed marks the byte order mark looked for, on the first read.
	sniffed bool
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

// sniff looks once for a UTF-16 byte order mark at the start of the
// stream and, when it finds one, reads the rest through a UTF-16 decoder.
// The decoder buffers what it reads, so a stream that holds no mark keeps
// its plain buffered reader.
func (t *TokenReader) sniff() {
	t.sniffed = true
	// A stream shorter than the mark holds none, and the read that follows
	// asks the source again and reports its error.
	head, _ := t.r.Peek(2)
	if len(head) == 2 && (head[0] == 0xFF && head[1] == 0xFE || head[0] == 0xFE && head[1] == 0xFF) {
		t.r = bufio.NewReader(transform.NewReader(t.r, unicode.UTF16(unicode.BigEndian, unicode.ExpectBOM).NewDecoder()))
	}
}

// ReadByte returns the next byte, failing with an error wrapping
// [limits.ErrInputTooLarge] past the cap of one token.
func (t *TokenReader) ReadByte() (byte, error) {
	if !t.sniffed {
		t.sniff()
	}
	if t.since >= t.limit {
		return 0, t.tooLarge()
	}
	b, err := t.r.ReadByte()
	if err == nil {
		t.since++
	}
	return b, err // the decoder reports io.EOF as the end of the part: wrapping would hide it
}

// Read reads into p, failing with an error wrapping [limits.ErrInputTooLarge]
// past the cap of one token.
func (t *TokenReader) Read(p []byte) (int, error) {
	if !t.sniffed {
		t.sniff()
	}
	room := t.limit - t.since
	if room <= 0 {
		return 0, t.tooLarge()
	}
	if int64(len(p)) > room {
		p = p[:room]
	}
	n, err := t.r.Read(p)
	t.since += int64(n)
	return n, err // the decoder reports io.EOF as the end of the part: wrapping would hide it
}

// tooLarge is the error past the cap.
func (t *TokenReader) tooLarge() error {
	return fmt.Errorf("%w: an XML token past %d bytes", limits.ErrInputTooLarge, t.limit)
}
