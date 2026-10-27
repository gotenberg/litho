package page

import (
	"bytes"
	"compress/zlib"
	"testing"
)

// TestUndecodableFilterBeforeAnother pins that the renderer draws nothing
// from a content stream whose chain decodes past a filter litho cannot
// decode, and the filters before one that ends it.
func TestUndecodableFilterBeforeAnother(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	if _, err := zw.Write([]byte("1 0 0 rg 0 0 100 100 re f")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		chain string
		want  rgb
	}{
		{"[/Foo /FlateDecode]", paper},
		{"[/FlateDecode /Foo]", red},
		{"/FlateDecode", red},
	} {
		f := fixture{
			objs: []string{stream("/Type /XObject /Subtype /Form /BBox [0 0 100 100] /Filter "+tc.chain, buf.String())},
			page: "/Resources << /XObject << /Fm 5 0 R >> >>", content: "/Fm Do",
		}
		img, _ := draw(t, f)
		if got := at(t, img, 50, 50); !close8(got, tc.want, 1) {
			t.Errorf("%s: (50,50) = %v, want %v", tc.chain, got, tc.want)
		}
	}
}
