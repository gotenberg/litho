package cmap

import (
	"bytes"
	"context"
	"maps"
	"strings"
	"testing"
)

func TestConformBfRanges(t *testing.T) {
	t.Parallel()

	const head = "/CIDInit /ProcSet findresource begin\n12 dict begin\nbegincmap\n" +
		"1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n"
	const tail = "endcmap\nCMapName currentdict /CMap defineresource pop\nend\nend\n"

	tests := []struct {
		name    string
		groups  string
		want    []string
		changed bool
	}{
		{
			// pdfjs issue5751.pdf: veraPDF credits none of these codes.
			name:    "range crossing a first-byte boundary",
			groups:  "1 beginbfrange\n<01ED> <0206> <0061>\nendbfrange\n",
			want:    []string{"2 beginbfrange", "<01ED> <01FF> <0061>", "<0200> <0206> <0074>"},
			changed: true,
		},
		{
			name:    "destination last byte kept at or below 255",
			groups:  "1 beginbfrange\n<01F0> <0210> <00FA>\nendbfrange\n",
			want:    []string{"<01F0> <01F5> <00FA>", "<01F6> <01FF> <0100>", "<0200> <0210> <010A>"},
			changed: true,
		},
		{
			name:    "array destination split with its codes",
			groups:  "1 beginbfrange\n<01FE> <0201> [<0041> <0042> <0043> <0044>]\nendbfrange\n",
			want:    []string{"<01FE> <01FF> [<0041> <0042>]", "<0200> <0201> [<0043> <0044>]"},
			changed: true,
		},
		{
			// pdfjs issue20232.pdf: libHaru's identity map, 216 first-byte blocks.
			name:    "identity range regrouped at 100 entries",
			groups:  "1 beginbfrange\n<0000> <D7FF> <0000>\nendbfrange\n",
			want:    []string{"100 beginbfrange", "16 beginbfrange", "<0000> <00FF> <0000>", "<D700> <D7FF> <D700>"},
			changed: true,
		},
		{
			name:   "conforming CMap left alone",
			groups: "2 beginbfrange\n<01ED> <01FF> <0061>\n<0200> <0206> <0074>\nendbfrange\n",
		},
		{
			name:   "group it cannot read whole left alone",
			groups: "1 beginbfrange\n<01FE> <0201> [<0041> 5 <0043> <0044>]\nendbfrange\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			in := []byte(head + tt.groups + tail)

			out, changed, err := ConformBfRanges(ctx, in)
			if err != nil {
				t.Fatalf("ConformBfRanges: %v", err)
			}
			if changed != tt.changed {
				t.Fatalf("changed = %v, want %v\n%s", changed, tt.changed, out)
			}
			if !changed {
				if !bytes.Equal(out, in) {
					t.Errorf("unchanged CMap came back different:\n%s", out)
				}
				return
			}
			for _, s := range tt.want {
				if !strings.Contains(string(out), s) {
					t.Errorf("rewrite lacks %q:\n%s", s, out)
				}
			}
			before, err := Parse(ctx, in)
			if err != nil {
				t.Fatal(err)
			}
			after, err := Parse(ctx, out)
			if err != nil {
				t.Fatal(err)
			}
			if !maps.Equal(after.Mapping, before.Mapping) {
				t.Error("rewrite changed the lenient mapping")
			}
			if !maps.Equal(after.StrictMapping(), after.Mapping) {
				t.Error("rewrite still holds a range a strict reader drops")
			}
		})
	}
}
