package parser

import "github.com/gotenberg/litho/pkg/internal/core/ast"

// boxedSlots is the number of operands [Parser.WithBoxedOperands] remembers.
// A power of two, so a token's slot is its hash masked.
const boxedSlots = 256

// maxBoxedLen is the longest token text [Parser.WithBoxedOperands] remembers
// an object for: the numbers and names a content stream repeats are short.
const maxBoxedLen = 32

// boxedOperands remembers, for each of [boxedSlots] slots chosen by a hash of
// a token's text, the last number or name object read from that text.
type boxedOperands struct {
	texts   [boxedSlots]string
	objects [boxedSlots]ast.PdfObject
}

// WithBoxedOperands has the parser hand out one object for each short number
// and name token it reads again and again, rather than a new one for each:
// an [ast.PdfObject] holding a number or a name is a heap value the
// interface points at, and a content stream repeats its operands. The object
// is the one the parser would read anyway, and is never changed, so the
// operations sharing it read what they would read apart. Only a number read
// with no side effect is remembered: one that records a format violation or
// an integer overflow is read again each time. It costs a table of
// [boxedSlots] entries, which pays for itself on a stream, and not on one
// object.
func (p *Parser) WithBoxedOperands() *Parser {
	p.boxed = new(boxedOperands)
	return p
}

// boxedSlot returns the slot of text in p's table, or -1 when the parser
// remembers no object for it.
func (p *Parser) boxedSlot(text string) int {
	if p.boxed == nil || len(text) > maxBoxedLen {
		return -1
	}
	// FNV-1a.
	h := uint32(2166136261)
	for i := 0; i < len(text); i++ {
		h ^= uint32(text[i])
		h *= 16777619
	}
	return int(h & (boxedSlots - 1))
}

// rememberedObject returns the object read from text at slot, if the parser
// remembers one.
func (p *Parser) rememberedObject(slot int, text string) (ast.PdfObject, bool) {
	if slot < 0 || p.boxed.objects[slot] == nil || p.boxed.texts[slot] != text {
		return nil, false
	}
	return p.boxed.objects[slot], true
}

// remember records obj as the object read from text at slot.
func (p *Parser) remember(slot int, text string, obj ast.PdfObject) ast.PdfObject {
	if slot >= 0 {
		p.boxed.texts[slot] = text
		p.boxed.objects[slot] = obj
	}
	return obj
}
