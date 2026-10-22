package colorconv

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// maxFuzzComponents is how many components [FuzzForObject] hands a
// converter: past the 32 colorants of the largest DeviceN space ISO
// 32000-1, Annex C.2 has a reader handle, so a space never reads past them.
const maxFuzzComponents = 40

// FuzzForObject reads the fuzzed bytes as the definition of a page's colour
// space resource, beside profiles and functions it may name, builds its
// converter and converts the fuzzed components through it, under each
// rendering intent. Neither the reading nor the conversion panics, and every
// channel converted lands in [0, 1].
func FuzzForObject(f *testing.F) {
	for _, c := range oracleCases {
		f.Add([]byte(c.def), []byte{0, 128, 255, 64})
	}
	f.Add([]byte(sepDef[:len(sepDef)/2]), []byte{200})
	f.Add([]byte("[/Indexed /DeviceRGB 2 <FF0000 00FF"), []byte{1})
	shared := sharedObjects("")
	f.Fuzz(func(t *testing.T, def, in []byte) {
		objs := append([]string{
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 10 10] /Contents 4 0 R /Resources << /ColorSpace << /CS %s >> >> >>", def),
		}, shared...)
		var buf bytes.Buffer
		buf.WriteString("%PDF-1.7\n")
		offs := make([]int, len(objs)+1)
		for i, o := range objs {
			offs[i+1] = buf.Len()
			fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
		}
		xref := buf.Len()
		fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
		for i := 1; i <= len(objs); i++ {
			fmt.Fprintf(&buf, "%010d 00000 n\r\n", offs[i])
		}
		fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
		data := buf.Bytes()
		ctx := context.Background()
		doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
		if err != nil {
			return
		}
		catalog, err := document.ResolveCatalog(ctx, doc)
		if err != nil {
			return
		}
		pages, err := document.ResolvePages(ctx, doc, catalog)
		if err != nil || len(pages) == 0 {
			return
		}
		resources := pages[0].DrawResources
		spaces, err := document.ResolveDict(ctx, doc, resources["ColorSpace"])
		if err != nil || spaces["CS"] == nil {
			return
		}
		comps := make([]float64, maxFuzzComponents)
		for i := range comps {
			if len(in) > 0 {
				comps[i] = float64(in[i%len(in)]) / 200
			}
		}
		for _, intent := range []string{"", "Perceptual", "Saturation", "AbsoluteColorimetric"} {
			conv, err := ForObject(ctx, doc, spaces["CS"], resources, Options{Intent: intent, Page: pages[0].Dict, Cache: &Cache{}})
			if err != nil {
				continue
			}
			r, g, b := conv.RGB(comps)
			for _, v := range []float32{r, g, b} {
				if !(v >= 0 && v <= 1) {
					t.Fatalf("intent %q: channel %v outside [0, 1]", intent, v)
				}
			}
		}
	})
}
