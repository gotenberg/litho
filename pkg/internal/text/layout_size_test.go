package text

import (
	"testing"
	"unsafe"
)

// layoutStateClass is the size class of the Go allocator a [layoutState]
// stays within, in bytes: the next class is 1152.
const layoutStateClass = 1024

// TestLayoutState_FitsItsSizeClass locks the state of one layout call
// within [layoutStateClass] bytes. The allocator rounds an object up to a
// size class: a state of 1025 bytes takes 1152, an eighth more for every
// call, which on a sheet of a million cells is a million states.
func TestLayoutState_FitsItsSizeClass(t *testing.T) {
	t.Parallel()
	if size := unsafe.Sizeof(layoutState{}); size > layoutStateClass {
		t.Errorf("layoutState is %d bytes, past its size class of %d: move a field out or shrink one", size, layoutStateClass)
	}
}
