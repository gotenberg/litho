package cff

import (
	"fmt"
	"math"
)

// CFF2 charstring operators Type 2 reserves (OpenType specification, CFF2
// table, CharString variation operators).
const (
	csVsindex = 15
	csBlend   = 16
)

// startCFF2 sets a machine up for a CFF2 charstring: no width operand to look
// for (CFF2 states widths in 'hmtx' alone), the 513-operand stack and the
// region count of the Private DICT's vsindex (OpenType specification, CFF2
// table, CharString concepts and PrivateDICT operator summary).
func (m *type2Machine) startCFF2(private *outlinePrivate) {
	m.widthDone = true
	m.stackLimit = maxCharstringStack
	m.regions = private.regions
}

// cff2Operator runs the one-byte operators whose meaning CFF2 changes, and
// reports whether b is one. CFF2 drops return and endchar: a subroutine ends
// with its data and a glyph with its charstring, so each is a reserved
// operator, which clears the stack as [type2Machine.pathOp] reads one. It
// adds vsindex and blend (OpenType specification, CFF2 table, CharString
// operator summary).
func (m *type2Machine) cff2Operator(b byte) (bool, error) {
	switch b {
	case csReturn, csEndchar:
		m.stack = m.stack[:0]
		return true, nil
	case csVsindex:
		return true, m.vsindex()
	case csBlend:
		return true, m.blend()
	}
	return false, nil
}

// vsindex selects the ItemVariationData whose regions the blends after it
// take, and clears the stack (OpenType specification, CFF2 table, vsindex).
func (m *type2Machine) vsindex() error {
	v, err := m.pop()
	if err != nil {
		return err
	}
	k := noVariationStore
	if v >= 0 && v <= math.MaxUint16 && v == math.Trunc(v) {
		k = m.run.font.regionCount(int(v))
	}
	if k == noVariationStore {
		return fmt.Errorf("%w: vsindex %v names no ItemVariationData", ErrInvalidCharstring, v)
	}
	m.regions = k
	m.stack = m.stack[:0]
	return nil
}

// blend resolves the n + n*k + 1 operands on the top of the stack to the n
// default values, the default instance's blend: there every region scalar is
// 0, so each delta weighs nothing (OpenType specification, CFF2 table, blend,
// and OpenType Font Variations Overview, algorithm for interpolation of
// instance values). It fails on a blend the stack does not hold the operands
// of, and on one in a font without a VariationStore, where the operator must
// not be used.
func (m *type2Machine) blend() error {
	if m.regions == noVariationStore {
		return fmt.Errorf("%w: blend without a VariationStore", ErrInvalidCharstring)
	}
	keep, ok := blendSplit(m.stack, m.regions)
	if !ok {
		return fmt.Errorf("%w: blend operands past the stack", ErrInvalidCharstring)
	}
	m.stack = m.stack[:keep]
	return nil
}

// isFlex reports the flex family of escape operators, the only ones CFF2
// keeps (OpenType specification, CFF2 table, CharString operator summary).
func isFlex(op byte) bool {
	return op == csHflex || op == csFlex || op == csHflex1 || op == csFlex1
}
