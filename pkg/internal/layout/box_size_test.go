package layout

import (
	"testing"
	"unsafe"
)

// boxClass is the size class of the Go allocator a [box] stays within, in
// bytes: the next class is 704.
const boxClass = 640

// TestBox_FitsItsSizeClass locks a box within [boxClass] bytes. The
// allocator rounds an object up to a size class: a box of 641 bytes takes
// 704, a tenth more for every box of every document. What few boxes need
// goes behind a pointer, as [boxMove] does.
func TestBox_FitsItsSizeClass(t *testing.T) {
	t.Parallel()
	if size := unsafe.Sizeof(box{}); size > boxClass {
		t.Errorf("box is %d bytes, past its size class of %d: move a field behind a pointer or shrink one", size, boxClass)
	}
}
