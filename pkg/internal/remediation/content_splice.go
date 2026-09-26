package remediation

import (
	"bytes"
	"context"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/ioat"
	"github.com/gotenberg/litho/pkg/internal/core/lexer"
	"github.com/gotenberg/litho/pkg/internal/core/parser"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// contentTokenCheckInterval is how many tokens [contentTokenSplices] reads
// between two checks of its context, the interval the content parser uses.
const contentTokenCheckInterval = 500

// maxInlineImageDictTokens caps the tokens [inlineImageDictSplices] reads
// ahead of the parser to find the ID that ends an inline image's dictionary.
// Anti-DoS guard, not spec-mandated: the parser holds every token read ahead,
// and a BI with no ID would have it hold the rest of the stream. ISO 32000-1,
// 8.9.7, Table 93 gives the dictionary ten entries, whose longest values are a
// Decode array of two numbers per colour component and an Indexed lookup
// string, one token, so a real dictionary holds well under a hundred tokens. A
// longer one is left as written.
//
// # Corpus evidence
//
//   - All corpora: 134 files draw 229,692 inline images, and the longest
//     dictionary among them, in pdfjs issue14256.pdf, holds 34 tokens before
//     its ID.
const maxInlineImageDictTokens = 1024

// tokenSplice replaces the bytes [start, end) of a content stream with text.
type tokenSplice struct {
	start, end int
	text       string
}

// tokenPlace is where a token [contentTokenSplices] offers stands.
type tokenPlace struct {
	// depth counts the arrays and dictionaries open around the token, an
	// inline image's dictionary among them.
	depth int
	// inlineImage reports whether the token is in an inline image's
	// dictionary, between its BI and ID.
	inlineImage bool
	// operand is the index, among the operands read since the operator
	// before it, of the operand the token is or stands in, an array or a
	// dictionary counting as one operand. For an operator, it is the count
	// of the operands the operator takes.
	operand int
	// dict reports whether the outermost array or dictionary open around
	// the token is a dictionary.
	dict bool
	// end is the offset in the stream just past the token's last byte.
	end int
}

// contentTokenSplices walks the tokens of a content stream's decoded bytes and
// offers each one to respell, with where it stands. respell returns the text
// to write in the token's place, or false to keep it. It returns the
// replacements in stream order, and whether the walk read the stream to its
// end.
//
// Only a token whose bytes are its value is offered, so a replacement covers
// exactly the bytes it stands for. The lexer hands a number, a hex string and
// a keyword back as written. A delimiter is never offered, and neither is the
// BI that opens an inline image. The tokens of the image's dictionary are
// offered ([inlineImageDictSplices]), and its data is skipped whole, past what
// veraPDF and PDF.js both read as data ([content.InlineImageEnds]): the walk
// cannot address it token by token.
//
// A walk the lexer cannot finish returns the replacements it found before the
// failure, which a caller may still apply: splicing them leaves every byte
// from the failure on as it was. A done context returns none.
func contentTokenSplices(ctx context.Context, data []byte, respell func(tok lexer.Token, at tokenPlace) (string, bool)) ([]tokenSplice, bool) {
	return tokenSplices(ctx, data, lexer.NewLexer(ioat.NewReader(bytes.NewReader(data), int64(len(data)), 0)), respell)
}

// repairTokenSplices is [contentTokenSplices] over the tokens of the reading of
// a repair ([content.ForRepair]), where a run that starts with a digit, a sign
// or a period and spells no number of ISO 32000-1, 7.3.3 is a keyword holding
// its text: "-", "." and "1.5.5" are offered as written. The token boundaries
// of ISO 32000-1, 7.2.2 read "-" as a number of value 0 (see the lexer's
// digitless numbers), whose bytes are not its value, which
// [contentTokenSplices] never offers.
func repairTokenSplices(ctx context.Context, data []byte, respell func(tok lexer.Token, at tokenPlace) (string, bool)) ([]tokenSplice, bool) {
	lex := lexer.NewLexer(ioat.NewReader(bytes.NewReader(data), int64(len(data)), 0)).WithVeraPDFTokens().WithRepairTokens()
	return tokenSplices(ctx, data, lex, respell)
}

// visitVeraPDFTokens walks the tokens of a content stream's decoded bytes as
// [contentTokenSplices] does, over the reading of veraPDF, the one validation
// takes ([lexer.Lexer.WithVeraPDFTokens]), and hands every one it would offer
// to visit, the ones whose bytes are not their value included: "c047" is the
// operator c and the number 047, where the token boundaries of ISO 32000-1,
// 7.2.2 read one keyword, and a run of digits past the range of a double is a
// number of the value veraPDF gives it.
func visitVeraPDFTokens(ctx context.Context, data []byte, visit func(tok lexer.Token, at tokenPlace)) {
	lex := lexer.NewLexer(ioat.NewReader(bytes.NewReader(data), int64(len(data)), 0)).WithVeraPDFTokens()
	walkTokens(ctx, data, lex, func(tok lexer.Token, at tokenPlace) (tokenSplice, bool) {
		visit(tok, at)
		return tokenSplice{}, false
	})
}

// tokenOffer is what a walk of [walkTokens] does with one token and where it
// stands: the replacement it asks for, if any.
type tokenOffer func(tok lexer.Token, at tokenPlace) (tokenSplice, bool)

// tokenSplices is the walk of [contentTokenSplices] over the tokens lex reads
// in data.
func tokenSplices(ctx context.Context, data []byte, lex *lexer.Lexer, respell func(tok lexer.Token, at tokenPlace) (string, bool)) ([]tokenSplice, bool) {
	return walkTokens(ctx, data, lex, func(tok lexer.Token, at tokenPlace) (tokenSplice, bool) {
		return offerToken(data, tok, at, respell)
	})
}

// walkTokens hands offer every token lex reads in data but the delimiters and
// the BI that opens an inline image, with where it stands, and returns the
// replacements offer asks for, and whether the walk read the stream to its
// end. See [contentTokenSplices].
func walkTokens(ctx context.Context, data []byte, lex *lexer.Lexer, offer tokenOffer) ([]tokenSplice, bool) {
	var splices []tokenSplice
	// images finds where the data of the inline images ends, made on the
	// first one the walk meets.
	var images *content.InlineImageEnds
	depth := 0
	// operands counts the operands read since the last operator, and dict
	// whether the outermost array or dictionary open is a dictionary.
	operands, dict := 0, false
	p := parser.NewParser(lex).WithOffsetTracking()
	for n := 1; ; n++ {
		if n%contentTokenCheckInterval == 0 && ctx.Err() != nil {
			return nil, false
		}
		tok, err := p.Peek(0)
		if err != nil || tok.Type == lexer.TokenError {
			return splices, false
		}
		if tok.Type == lexer.TokenEOF {
			return splices, true
		}
		end := int(p.PeekEnd(0))
		if _, err := p.Consume(); err != nil {
			return splices, false
		}
		switch tok.Type {
		case lexer.TokenKeyword:
			if tok.Value == "BI" && depth == 0 {
				if images == nil {
					images = content.NewInlineImageEnds(data)
				}
				dictSplices, ok := inlineImageDictSplices(ctx, p, images, offer)
				if !ok {
					return splices, false
				}
				splices = append(splices, dictSplices...)
				// BI drops the operands before it, and the image ends
				// its operation.
				operands = 0
				continue
			}
			// Any other keyword opens no nesting: offered to respell below.
		case lexer.TokenDelimiter:
			switch tok.Value {
			case "[", "<<":
				if depth == 0 {
					dict = tok.Value == "<<"
				}
				depth++
			case "]", ">>":
				// A close with nothing open is no operand: the parse drops
				// it.
				if depth > 0 {
					depth--
					if depth == 0 {
						operands++
					}
				}
			}
			continue
		case lexer.TokenEOF, lexer.TokenError, lexer.TokenName, lexer.TokenString, lexer.TokenNumber, lexer.TokenHexString:
			// A token that opens no nesting: offered to respell below.
		}
		if s, ok := offer(tok, tokenPlace{depth: depth, operand: operands, dict: depth > 0 && dict, end: end}); ok {
			splices = append(splices, s)
		}
		if depth == 0 {
			if isOperatorToken(tok) {
				operands = 0
			} else {
				operands++
			}
		}
	}
}

// isOperatorToken reports whether tok, read where an operand or an operator
// stands, is an operator: a keyword other than the objects true, false and
// null (ISO 32000-1, 7.3.2 and 7.3.9).
func isOperatorToken(tok lexer.Token) bool {
	return tok.Type == lexer.TokenKeyword && tok.Value != "true" && tok.Value != "false" && tok.Value != "null"
}

// inlineImageDictSplices hands offer the tokens of the inline image
// dictionary p stands at, just past its BI, and moves p past the image's data
// where images ends it. It returns the replacements, and false when the parser
// cannot read the image, which ends the walk.
//
// The tokens are read ahead of the parser, from its BI to the first ID
// keyword, without consuming them, and [parser.Parser.ParseInlineImageHeader]
// then reads the dictionary, whose filters end the data, which a walk
// consuming the tokens itself would hide from it. The replacements stand only
// when the parser consumed exactly the tokens read ahead, so the ID they end
// at is the one its data starts after (ISO 32000-1, 8.9.7). A dictionary the
// parser reads another way, or one longer than [maxInlineImageDictTokens], is
// left as written.
//
// Every replacement ends before the ID and the byte that follows it, so the
// data is carried across byte for byte.
func inlineImageDictSplices(ctx context.Context, p *parser.Parser, images *content.InlineImageEnds, offer tokenOffer) ([]tokenSplice, bool) {
	var splices []tokenSplice
	// dictTokens counts the tokens read ahead through the ID, or stays 0 when
	// no ID was found.
	dictTokens := 0
	for i, depth := 0, 1; i < maxInlineImageDictTokens; i++ {
		tok, err := p.Peek(i)
		if err != nil || tok.Type == lexer.TokenError || tok.Type == lexer.TokenEOF {
			break
		}
		if tok.Type == lexer.TokenKeyword && tok.Value == "ID" {
			dictTokens = i + 1
			break
		}
		if tok.Type == lexer.TokenDelimiter {
			switch tok.Value {
			case "[", "<<":
				depth++
			case "]", ">>":
				depth = max(depth-1, 1)
			}
			continue
		}
		if s, ok := offer(tok, tokenPlace{depth: depth, inlineImage: true, end: int(p.PeekEnd(i))}); ok {
			splices = append(splices, s)
		}
	}

	before := p.Consumed()
	dict, err := p.ParseInlineImageHeader(ctx)
	if err != nil {
		return nil, false
	}
	if err := p.ResumeAt(int64(images.End(ctx, int(p.Position()), dict))); err != nil {
		return nil, false
	}
	if dictTokens == 0 || p.Consumed()-before != dictTokens {
		return nil, true
	}
	return splices, true
}

// offerToken offers respell tok and where it stands in data, and returns the
// replacement it asks for. A token whose bytes are not its value is not
// offered.
func offerToken(data []byte, tok lexer.Token, at tokenPlace, respell func(tok lexer.Token, at tokenPlace) (string, bool)) (tokenSplice, bool) {
	end := at.end
	start := end - len(tok.Value)
	if start < 0 || end > len(data) || string(data[start:end]) != tok.Value {
		return tokenSplice{}, false
	}
	text, ok := respell(tok, at)
	if !ok {
		return tokenSplice{}, false
	}
	return tokenSplice{start: start, end: end, text: text}, true
}

// applyTokenSplices returns data with every splice written in place of the
// bytes it covers. The splices are in stream order and do not overlap, as
// [contentTokenSplices] returns them.
func applyTokenSplices(data []byte, splices []tokenSplice) []byte {
	var out bytes.Buffer
	out.Grow(len(data))
	prev := 0
	for _, s := range splices {
		out.Write(data[prev:s.start])
		out.WriteString(s.text)
		prev = s.end
	}
	out.Write(data[prev:])
	return out.Bytes()
}

// replaceDecodedStream writes data at ref as the body of a stream whose
// dictionary was dict, and reports whether the allocator took the write. The
// stream stays a stream, with every key of dict but its filters.
//
// data is decoded content, so the filter chain that described the original
// body no longer applies: carrying /Filter or /DecodeParms across leaves a
// stream a reader decodes into nothing (ISO 32000-1, 7.3.8.2). /Length is
// rewritten from the data.
func replaceDecodedStream(alloc *writer.ObjAllocator, ref ast.PdfIndirectRef, dict ast.PdfDict, data []byte) bool {
	newDict := make(ast.PdfDict, len(dict)+1)
	for k, v := range dict {
		if k == "Length" || k == "Filter" || k == "DecodeParms" {
			continue
		}
		newDict[k] = v
	}
	newDict["Length"] = ast.PdfInt(len(data))
	return alloc.AllocAt(ref.ObjectNum, ast.NewStream(newDict, data))
}
