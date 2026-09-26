package document

import (
	"context"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/ioat"
	"github.com/gotenberg/litho/pkg/internal/core/lexer"
	"github.com/gotenberg/litho/pkg/internal/core/parser"
	"github.com/gotenberg/litho/pkg/internal/core/xref"
)

// DictKeyOrder returns the keys of the dictionary object objNum holds, a
// stream's dictionary included, in the order the file writes them. A key is
// listed once, where it first appears, and a key written with the value null
// is left out, since ISO 32000-1, 7.3.7 makes such an entry equivalent to an
// absent one.
//
// A dictionary has no order of its own (ISO 32000-1, 7.3.7), and an
// [ast.PdfDict] keeps none. A reader that stores the entries in a hash table
// whose iteration order depends on the order they were inserted in still
// visits them in a way only the file's order predicts, and a caller modelling
// such a reader asks for it here.
//
// It parses the object's bytes again, through the parse [Document.Resolve]
// uses: an uncompressed object from its cross-reference offset, one held in an
// object stream from that stream's decoded bytes (see
// [Document.extractFromObjectStream]). It returns nil when the index lacks the
// object or lists it free, when the bytes at its offset do not open its
// header, when its value is no dictionary, and when a pair of the dictionary
// does not parse: the order of a damaged dictionary is not the file's.
//
// DictKeyOrder is safe for concurrent use.
func (d *Document) DictKeyOrder(ctx context.Context, objNum int) []ast.PdfName {
	if ctx.Err() != nil || (resolvesOpenedOnly(ctx) && !d.listsObject(objNum)) {
		return nil
	}
	entry, ok := d.XRefEntry(objNum)
	if !ok || entry.Free {
		return nil
	}
	var p *parser.Parser
	switch entry.Type {
	case xref.XRefTypeCompressed:
		d.mu.RLock()
		data, cached := d.objStmCache[entry.ObjStmNum]
		d.mu.RUnlock()
		if !cached {
			var err error
			data, err = d.parseObjectStream(ctx, entry.ObjStmNum, map[int]bool{})
			if err != nil {
				return nil
			}
			d.cacheObjStm(entry.ObjStmNum, data)
		}
		slot := data.slotFor(entry.ObjStmIndex, objNum)
		if slot < 0 {
			return nil
		}
		off := data.offsets[slot]
		if off < 0 || off > len(data.decoded)-data.first {
			return nil
		}
		p = data.parser(off, data.objectEnd(off))
	default:
		if !objectHeaderMatches(d.r, entry.Offset, objNum) {
			return nil
		}
		lex := lexer.NewLexer(ioat.NewReader(d.r, d.size, entry.Offset))
		p = parser.NewParser(lex).WithObjectEnd(d.objectEnd(entry.Offset))
		for _, want := range []lexer.TokenType{lexer.TokenNumber, lexer.TokenNumber, lexer.TokenKeyword} {
			if _, err := p.Expect(want); err != nil {
				return nil
			}
		}
	}
	return dictKeysInOrder(ctx, p)
}

// dictKeysInOrder reads the dictionary p is positioned at and returns its
// keys in the order they appear (see [Document.DictKeyOrder]), or nil when
// what follows is no well-formed dictionary.
func dictKeysInOrder(ctx context.Context, p *parser.Parser) []ast.PdfName {
	if t, err := p.Consume(); err != nil || t.Type != lexer.TokenDelimiter || t.Value != "<<" {
		return nil
	}
	var keys []ast.PdfName
	seen := make(map[ast.PdfName]bool)
	for i := 0; ; i++ {
		if i%100 == 0 && ctx.Err() != nil {
			return nil
		}
		t, err := p.Peek(0)
		if err != nil {
			return nil
		}
		if t.Type == lexer.TokenDelimiter && t.Value == ">>" {
			return keys
		}
		keyObj, err := p.ParseObject(ctx)
		if err != nil {
			return nil
		}
		key, ok := keyObj.(ast.PdfName)
		if !ok {
			return nil
		}
		val, err := p.ParseObject(ctx)
		if err != nil {
			return nil
		}
		if _, null := val.(ast.PdfNull); null || seen[key] {
			continue
		}
		seen[key] = true
		keys = append(keys, key)
	}
}
