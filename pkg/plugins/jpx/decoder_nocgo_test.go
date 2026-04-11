//go:build !cgo

package jpx

import (
	"errors"
	"testing"

	"github.com/gotenberg/litho/pkg/plugins"
)

// TestDecoder_Decode_NoCGO_UnsupportedFilter verifies that in builds
// without CGO, Decode on non-empty input returns an error that wraps
// [plugins.ErrUnsupportedFilter]. Callers who branch on the sentinel via
// [errors.Is] must continue to work when the host lacks libopenjp2.
func TestDecoder_Decode_NoCGO_UnsupportedFilter(t *testing.T) {
	t.Parallel()
	d := New()
	_, err := d.Decode([]byte{0x00, 0x01, 0x02}, nil)
	if err == nil {
		t.Fatal("expected error from non-CGO JPX decoder")
	}
	if !errors.Is(err, plugins.ErrUnsupportedFilter) {
		t.Errorf("err = %v, want errors.Is(err, plugins.ErrUnsupportedFilter)", err)
	}
}
