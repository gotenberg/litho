package content

import (
	"github.com/gotenberg/litho/pkg/internal/core/lexer"
	"github.com/gotenberg/litho/pkg/internal/core/parser"
)

// veraPDF reads a content stream through a buffer of veraBufferSize bytes
// (ASBufferedInFilter, veraPDF-parser 1.30): it fills the second half of the
// buffer first, and once a read moves past veraBufferFeed bytes into it, it
// drops the bytes more than veraBufferHalf behind the position and reads as
// many more. These are the reference's values, not caps of litho's.
const (
	veraBufferSize = 2048
	veraBufferHalf = veraBufferSize / 2
	veraBufferFeed = 3 * veraBufferSize / 4
)

// veraObjectWindow is how far past an EI the check of the EI reads an object
// to find where it ends, for the buffer. A read that runs this far has moved
// the buffer past the EI, unless the buffer holds the end of the stream and
// moves no more, so reading further changes nothing the buffer decides.
const veraObjectWindow = 2 * veraBufferSize

// veraBuffer follows the buffer veraPDF reads a content stream through
// (ASBufferedInFilter, see [inlineImages]), as far as the moves back of the
// checks of EI need it: which bytes it holds when a check moves back.
//
// The buffer holds the bytes from base to base plus veraBufferSize. It starts
// with the first veraBufferHalf bytes of the stream in its second half, and
// every read that moves more than veraBufferFeed bytes past base moves base
// to veraBufferHalf bytes behind it, and reads the bytes that come in, until
// a read comes back short at the end of the stream. The position moves back
// within what it holds, and a move past either end of it throws.
type veraBuffer struct {
	// n is the length of the stream.
	n int
	// base is the offset in the stream of the first byte the buffer holds,
	// and read the offset past the last byte read.
	base, read int
	// end reports that the buffer holds the end of the stream, past which it
	// reads nothing more.
	end bool
}

// newVeraBuffer returns the buffer veraPDF reads a stream of n bytes through,
// before it reads any.
func newVeraBuffer(n int) *veraBuffer {
	return &veraBuffer{n: n, base: -veraBufferHalf, end: n < veraBufferHalf}
}

// readTo notes that the reading has read every byte before to, one at a
// time from where it stands.
func (b *veraBuffer) readTo(to int) {
	if to <= b.read {
		return
	}
	for !b.end && to-b.base > veraBufferFeed {
		b.feed(b.base + veraBufferFeed + 1)
	}
	b.read = to
}

// feed drops the bytes more than veraBufferHalf behind at and reads as many,
// as the read that moves the position to at does.
func (b *veraBuffer) feed(at int) {
	b.base = at - veraBufferHalf
	b.end = b.n < b.base+veraBufferSize
}

// holds reports whether the position can move to at: whether the buffer
// holds it, or it is the end of what the buffer holds and past the end of
// the stream, where a read reads nothing.
func (b *veraBuffer) holds(at int) bool {
	i := at - b.base
	return i >= 0 && (i < veraBufferSize || i == veraBufferSize && b.end)
}

// moveTo moves the position to at, which the buffer holds, and reads on from
// there: the first byte read past what was read feeds the buffer at once when
// it moves more than veraBufferFeed bytes past base.
func (b *veraBuffer) moveTo(at int) {
	if at < b.read || at >= b.n {
		return
	}
	if !b.end && at+1-b.base > veraBufferFeed {
		b.feed(at + 1)
	}
	b.read = at + 1
}

// objectEnd returns the offset past the last byte veraPDF reads of the object
// that starts at q when it checks an EI (PDFStreamParser.parseNextToken): an
// array or a dictionary to its close, a string to its end, and a name or a
// number past the byte that ends it, which it reads and puts back. It reads
// at most to veraObjectWindow bytes past pos, the EI, and the objects the
// checks read take at most the length of the stream in all, an anti-DoS
// guard, not spec-mandated: an object after an EI is read once for each EI
// that white space and the object follow, which only a crafted stream does in
// its image data, and past the bound the check reads no more than the first
// byte of the object.
func (r *inlineImages) objectEnd(pos, q int) int {
	if r.objectBudget <= 0 {
		return q + 1
	}
	window := min(len(r.data), pos+veraObjectWindow)
	p := newParser(r.data[q:window], readOptions{}).WithOffsetTracking()
	end := window
	tok, err := p.Peek(0)
	switch {
	case err != nil || tok.Type == lexer.TokenError:
	case tok.Type == lexer.TokenName || tok.Type == lexer.TokenNumber:
		end = min(q+int(p.PeekEnd(0))+1, window)
	case tok.Type == lexer.TokenDelimiter && (tok.Value == "[" || tok.Value == "<<"):
		end = q + containerEnd(p)
	default:
		end = q + int(p.PeekEnd(0))
	}
	r.objectBudget -= max(end-q, 1)
	return max(end, q+1)
}

// containerEnd returns the offset past the close of the array or the
// dictionary whose open p stands at, as veraPDF reads it: to its close and no
// further, since it reads no token after a dictionary in a content stream to
// tell a stream from it. It returns where p stops, the end of the data it
// reads among them, when p reaches no close. p reads at most
// veraObjectWindow bytes, so the walk needs no check of a context.
func containerEnd(p *parser.Parser) int {
	depth := 0
	for {
		tok, err := p.Peek(0)
		if err != nil || tok.Type == lexer.TokenError || tok.Type == lexer.TokenEOF {
			return int(p.Position())
		}
		end := int(p.PeekEnd(0))
		if _, err := p.Consume(); err != nil {
			return end
		}
		if tok.Type != lexer.TokenDelimiter {
			continue
		}
		switch tok.Value {
		case "[", "<<":
			depth++
		case "]", ">>":
			if depth--; depth == 0 {
				return end
			}
		}
	}
}
