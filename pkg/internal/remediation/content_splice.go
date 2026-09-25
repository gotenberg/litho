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

// tokenSplice replaces the bytes [start, end) of a content stream with text.
type tokenSplice struct {
	start, end int
	text       string
}

// contentTokenSplices walks the tokens of a content stream's decoded bytes and
// offers each one to respell, with the number of arrays and dictionaries open
// around it. respell returns the text to write in the token's place, or false
// to keep it. It returns the replacements in stream order, and whether the
// walk read the stream to its end.
//
// Only a token whose bytes are its value is offered, so a replacement covers
// exactly the bytes it stands for. The lexer hands a number and a hex string
// back as written. An inline image's dictionary and data are skipped whole:
// the walk cannot address its data token by token.
//
// A walk the lexer cannot finish returns the replacements it found before the
// failure, which a caller may still apply: splicing them leaves every byte
// from the failure on as it was. A done context returns none.
func contentTokenSplices(ctx context.Context, data []byte, respell func(tok lexer.Token, depth int) (string, bool)) ([]tokenSplice, bool) {
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
				if _, err := p.ParseInlineImageDict(ctx); err != nil {
					return splices, false
				}
			}
			continue
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
		start := end - len(tok.Value)
		if start < 0 || end > len(data) || string(data[start:end]) != tok.Value {
			continue
		}
		if text, ok := respell(tok, depth); ok {
			splices = append(splices, tokenSplice{start: start, end: end, text: text})
		}
	}
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
