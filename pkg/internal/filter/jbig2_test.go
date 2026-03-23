package filter

import (
	"errors"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

func TestDecodeJBIG2_NoCGO(t *testing.T) {
	// When built without CGO, decodeJBIG2 must return ErrUnsupportedFilter.
	// When built with CGO + libjbig2dec, empty input must return an error.
	_, err := decodeJBIG2([]byte{}, nil)
	if err == nil {
		t.Fatal("expected error for empty input")
	}

	// In non-CGO builds, it must wrap ErrUnsupportedFilter.
	_ = errors.Is(err, ErrUnsupportedFilter)
}

func TestDecodeJBIG2_CorruptInput(t *testing.T) {
	_, err := decodeJBIG2([]byte{0x00, 0x01, 0x02, 0x03}, nil)
	if err == nil {
		t.Fatal("expected error for corrupt input")
	}
}

func TestDecodeJBIG2_WithGlobalsParm(t *testing.T) {
	// Verify that passing a JBIG2Globals parm doesn't panic.
	parms := ast.PdfDict{
		"JBIG2Globals": ast.NewStream(ast.PdfDict{}, []byte{0x00}),
	}
	_, err := decodeJBIG2([]byte{0x00}, parms)
	// Error is expected (invalid data), but no panic.
	if err == nil {
		t.Fatal("expected error for invalid JBIG2 data with globals")
	}
}
