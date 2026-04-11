package jpx

import (
	"testing"

	"github.com/gotenberg/litho/pkg/plugins"
)

func TestDecoder_FilterName(t *testing.T) {
	t.Parallel()
	d := New()
	if got := d.FilterName(); got != "JPXDecode" {
		t.Errorf("FilterName() = %q, want %q", got, "JPXDecode")
	}
}

func TestDecoder_ImplementsFilterDecoder(t *testing.T) {
	t.Parallel()
	var _ plugins.FilterDecoder = New()
}

func TestDecoder_Decode_EmptyInput(t *testing.T) {
	t.Parallel()
	d := New()
	_, err := d.Decode(nil, nil)
	if err == nil {
		t.Error("expected error for empty input")
	}
}

func TestDecoder_Decode_EmptySlice(t *testing.T) {
	t.Parallel()
	d := New()
	_, err := d.Decode([]byte{}, nil)
	if err == nil {
		t.Error("expected error for empty-slice input")
	}
}
