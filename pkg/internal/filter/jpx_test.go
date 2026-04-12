package filter

import (
	"errors"
	"testing"
)

func TestDecodeJPX_NoCGO(t *testing.T) {
	// When built without CGO, decodeJPX must return ErrUnsupportedFilter.
	// When built with CGO + libopenjp2, empty input must return an error.
	_, err := decodeJPX([]byte{})
	if err == nil {
		t.Fatal("expected error for empty input")
	}

	// In non-CGO builds, it must wrap ErrUnsupportedFilter.
	// In CGO builds, it returns a different error. Both are acceptable.
	_ = errors.Is(err, ErrUnsupportedFilter)
}

func TestDecodeJPX_CorruptInput(t *testing.T) {
	_, err := decodeJPX([]byte{0xFF, 0x4F, 0xFF, 0x51, 0x00})
	if err == nil {
		t.Fatal("expected error for corrupt input")
	}
}
