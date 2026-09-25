package remediation

import (
	"bytes"
	"context"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/ioat"
	"github.com/gotenberg/litho/pkg/internal/core/lexer"
	"github.com/gotenberg/litho/pkg/internal/core/parser"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
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
// offered ([inlineImageDictSplices]), and its data is skipped whole: the walk
// cannot address it token by token.
//
// A walk the lexer cannot finish returns the replacements it found before the
// failure, which a caller may still apply: splicing them leaves every byte
// from the failure on as it was. A done context returns none.
func contentTokenSplices(ctx context.Context, data []byte, respell func(tok lexer.Token, at tokenPlace) (string, bool)) ([]tokenSplice, bool) {
	var splices []tokenSplice
	depth := 0
	p := parser.NewParser(lexer.NewLexer(ioat.NewReader(bytes.NewReader(data), int64(len(data)), 0))).WithOffsetTracking()
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
				dictSplices, ok := inlineImageDictSplices(ctx, p, data, respell)
				if !ok {
					return splices, false
				}
				splices = append(splices, dictSplices...)
				continue
			}
			// Any other keyword opens no nesting: offered to respell below.
		case lexer.TokenDelimiter:
			switch tok.Value {
			case "[", "<<":
				depth++
			case "]", ">>":
				depth = max(depth-1, 0)
			}
			continue
		case lexer.TokenEOF, lexer.TokenError, lexer.TokenName, lexer.TokenString, lexer.TokenNumber, lexer.TokenHexString:
			// A token that opens no nesting: offered to respell below.
		}
		if s, ok := offerToken(data, tok, end, tokenPlace{depth: depth}, respell); ok {
			splices = append(splices, s)
		}
	}
}

// inlineImageDictSplices offers respell the tokens of the inline image
// dictionary p stands at, just past its BI, and moves p past the image's EI.
// It returns the replacements, and false when the parser cannot read the
// image, which ends the walk.
//
// The tokens are read ahead of the parser, from its BI to the first ID
// keyword, without consuming them, and [parser.Parser.ParseInlineImageDict]
// then reads the dictionary and skips the data as it always has: it finds the
// EI by the dictionary's filter, which a walk consuming the tokens itself would
// hide from it. The replacements stand only when the parser consumed exactly
// the tokens read ahead, so the ID they end at is the one its data starts
// after (ISO 32000-1, 8.9.7). A dictionary the parser reads another way, or one
// longer than [maxInlineImageDictTokens], is left as written.
//
// Every replacement ends before the ID and the byte that follows it, so the
// data is carried across byte for byte.
func inlineImageDictSplices(ctx context.Context, p *parser.Parser, data []byte, respell func(tok lexer.Token, at tokenPlace) (string, bool)) ([]tokenSplice, bool) {
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
		if s, ok := offerToken(data, tok, int(p.PeekEnd(i)), tokenPlace{depth: depth, inlineImage: true}, respell); ok {
			splices = append(splices, s)
		}
	}

	before := p.Consumed()
	if _, err := p.ParseInlineImageDict(ctx); err != nil {
		return nil, false
	}
	if dictTokens == 0 || p.Consumed()-before != dictTokens {
		return nil, true
	}
	return splices, true
}

// offerToken offers respell tok, which ends at end in data and stands at at,
// and returns the replacement it asks for. A token whose bytes are not its
// value is not offered.
func offerToken(data []byte, tok lexer.Token, end int, at tokenPlace, respell func(tok lexer.Token, at tokenPlace) (string, bool)) (tokenSplice, bool) {
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
