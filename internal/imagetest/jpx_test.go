package imagetest

import (
	"testing"
)

// TestJPX_IsAJP2File checks the boxes; the decoding is checked against
// litho's own decoder by the image conversion's tests.
func TestJPX_IsAJP2File(t *testing.T) {
	t.Parallel()
	data := JPX(gradient(70, 66), JPXOptions{})
	if string(data[4:8]) != "jP  " || string(data[16:20]) != "ftyp" {
		t.Fatalf("not a JP2 file: % x", data[:24])
	}
}
