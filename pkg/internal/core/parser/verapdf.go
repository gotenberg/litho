package parser

import (
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/lexer"
)

// WithVeraPDFContainers makes p note where veraPDF gives up on the content
// stream p reads, and returns p: at an array or a dictionary veraPDF cannot
// read (COSParser.getArray and COSParser.getDictionary, veraPDF-parser 1.30),
// or at an inline image dictionary whose key is no name
// (PDFStreamParser.parseNextToken). [Parser.Abandoned] reports it. The parse
// itself is unchanged, save that "endobj" and "stream" close no dictionary:
// p still reads the container as leniently as it reads any other, dropping
// the tokens that start no object.
//
// veraPDF throws on such a container, and the reader of its model catches the
// failure and gives the content stream no operator at all
// (GFPDContentStream.parseOperators), so no rule of ISO 19005 judges any of it,
// the operators read before the container included. An array holds objects
// alone and ends at "]" (ISO 32000-1, 7.3.6), and a dictionary holds names
// paired with objects and ends at ">>" (7.3.7). veraPDF reads:
//
//   - an array whose element is a keyword other than true, false and null, a
//     token that starts no object, such as ")", "{", "}", ">>" or a ">" alone,
//     or that the stream ends inside, as a failure;
//   - a dictionary whose key is no name, whose last key has no value, which
//     the stream ends inside, or which holds a ">" alone, as a failure;
//   - a dictionary whose value is a keyword, or a ")", "{", "}" or "]", as an
//     entry with no value, and reads on.
//
// So "[1 yy] TJ", "[1e2] TJ", "<</A>>", "<</A 1 xx>>" and "<<xx /A 1>>" make
// veraPDF give up on the stream, and "<</A xx>>" does not. veraPDF reads
// integers ahead of a value to tell a reference from integers
// (COSParser.nextObject), and hands the ones it read ahead to the values after
// it, while a key is the token after them: "<</A 1 2>>" and "<</A 1 2 /B>>"
// are read whole, and "<</A 1 2 /B 3>>" is not. [veraPDFDict] follows that
// reading.
//
// Deliberate leniency toward veraPDF, the reference validator of ISO 19005: a
// content stream holding such a container breaks ISO 32000-1, 7.3.6 or 7.3.7,
// which no rule of ISO 19005 states, and the rules on the operators around it,
// such as ISO 19005-1, 6.2.10 and ISO 19005-2, 6.2.2, go unjudged there. The
// objects of the file body are read without it.
func (p *Parser) WithVeraPDFContainers() *Parser {
	p.veraPDFContainers = true
	return p
}

// Abandoned reports whether p has read, under [Parser.WithVeraPDFContainers],
// a container veraPDF cannot read, or a caller noted it gives up on the
// stream ([Parser.Abandon]).
func (p *Parser) Abandoned() bool {
	return p.abandoned
}

// Abandon notes, under [Parser.WithVeraPDFContainers], that veraPDF gives up
// on the stream p reads for a reason a caller found in bytes the parser does
// not read itself, such as the data of an inline image.
func (p *Parser) Abandon() {
	p.abandon()
}

// abandon notes, under [Parser.WithVeraPDFContainers], a container veraPDF
// cannot read.
func (p *Parser) abandon() {
	if p.veraPDFContainers {
		p.abandoned = true
	}
}

// veraToken is the kind of a token veraPDF's COSParser tells apart where it
// reads a dictionary.
type veraToken uint8

const (
	// veraNone starts no object: a keyword other than true, false, null and
	// R, or a ")", "{", "}" or "]".
	veraNone veraToken = iota
	// veraInteger is an integer.
	veraInteger
	// veraR is the keyword R.
	veraR
	// veraName is a name.
	veraName
	// veraObject is any other object, an array or a dictionary read whole.
	veraObject
	// veraClose is the ">>" that closes a dictionary.
	veraClose
	// veraEnd is the end of the data.
	veraEnd
	// veraFail is a token veraPDF's BaseParser.nextToken throws on: a ">"
	// alone.
	veraFail
)

// maxQueuedIntegers is how many integers COSParser.nextObject (veraPDF-parser
// 1.30) holds before it hands the first one out: the three of "N G R". It
// mirrors that reader, not a resource limit.
const maxQueuedIntegers = 3

// veraPDFDict collects the tokens of a dictionary of a content stream as
// [Parser.parseDictBody] reads them, and replays veraPDF's reading of them
// (COSParser.getDictionary, veraPDF-parser 1.30) once the dictionary ends. The
// two readings part after a malformed entry: [Parser.nameRunStartsWithKey] can
// take a name for a value where veraPDF reads every token after a value as a
// key, and veraPDF hands integers it read ahead to the values after them. A
// nil veraPDFDict collects nothing.
type veraPDFDict struct {
	tokens []veraToken
}

// item notes the next item the dictionary holds: obj, or, for a token that
// starts no object, nil and the token.
func (d *veraPDFDict) item(obj ast.PdfObject, stray lexer.Token) {
	if d == nil {
		return
	}
	switch obj.(type) {
	case nil:
		switch {
		case stray.Type == lexer.TokenKeyword && stray.Value == "R":
			d.tokens = append(d.tokens, veraR)
		case stray.Type == lexer.TokenDelimiter && stray.Value == ">":
			d.tokens = append(d.tokens, veraFail)
		default:
			d.tokens = append(d.tokens, veraNone)
		}
	case ast.PdfInt:
		d.tokens = append(d.tokens, veraInteger)
	case ast.PdfIndirectRef:
		d.tokens = append(d.tokens, veraInteger, veraInteger, veraR)
	case ast.PdfName:
		d.tokens = append(d.tokens, veraName)
	default:
		d.tokens = append(d.tokens, veraObject)
	}
}

// failed reports whether veraPDF gives up on the dictionary, which the token
// t ends: a ">>", or anything else for the end of the data.
func (d *veraPDFDict) failed(t lexer.Token) bool {
	if d == nil {
		return false
	}
	end := veraEnd
	if t.Type == lexer.TokenDelimiter && t.Value == ">>" {
		end = veraClose
	}
	r := veraDictReader{tokens: append(d.tokens, end), readNext: true}
	for r.name() {
		r.value()
	}
	return r.fail || r.cur != veraClose
}

// veraDictReader replays COSParser.getDictionary and COSParser.nextObject
// (veraPDF-parser 1.30) on the tokens of a dictionary, after its "<<".
type veraDictReader struct {
	tokens []veraToken
	// pos is the index of the next token to read, and cur the one read last.
	pos int
	cur veraToken
	// readNext is COSParser.flag: whether the next read takes a new token.
	readNext bool
	// integers counts the integers read ahead, and handed counts the ones
	// set aside to hand to the next values.
	integers, handed int
	// fail reports a token veraPDF throws on.
	fail bool
}

// advance reads the next token, the end of the data past the last one.
func (r *veraDictReader) advance() {
	r.cur = veraEnd
	if r.pos < len(r.tokens) {
		r.cur = r.tokens[r.pos]
		r.pos++
	}
	if r.cur == veraFail {
		r.fail = true
	}
}

// name reads a key (COSParser.getName) and reports whether it is a name.
func (r *veraDictReader) name() bool {
	if r.readNext {
		r.advance()
	}
	r.readNext = true
	return !r.fail && r.cur == veraName
}

// value reads a value (COSParser.nextObject). Whether it is an object or
// nothing does not matter to the dictionary.
func (r *veraDictReader) value() {
	if r.handed > 0 {
		r.handed--
		return
	}
	for !r.fail {
		if r.readNext {
			r.advance()
		}
		r.readNext = true
		switch {
		case r.cur == veraInteger:
			r.integers++
			if r.integers < maxQueuedIntegers {
				continue
			}
			r.integers--
		case r.cur == veraR && r.integers == 2:
			r.integers = 0
		case r.integers > 0:
			r.handed += r.integers - 1
			r.integers = 0
			r.readNext = false
		}
		return
	}
}
