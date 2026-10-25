package typography

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// fuzzType3ProcBytes bounds each fuzzed CharProc so one input stays a
// small document.
const fuzzType3ProcBytes = 1 << 16

// FuzzType3PaintBounds bounds an outer Type 3 glyph whose description, the
// first input, shows in a nested Type 3 font whose one glyph's description
// is the second ([liftedStrokesPDF]), through the text extractor: the
// bounds walk of both descriptions ([type3PaintBounds]), the text pass over
// the outer one ([type3TextBoxes]) and the strokes the nested glyph hands
// up ([type3Lift]). It asserts the walk never panics.
func FuzzType3PaintBounds(f *testing.F) {
	outer, nested := liftedStrokesOuter(3), liftedStrokesProc()
	f.Add([]byte(outer), []byte(nested))
	f.Add([]byte(outer[:len(outer)/2]), []byte(nested[:len(nested)/2]))
	f.Add([]byte("1000 0 d0 q 2 0 0 2 0 0 cm 0 0 10 10 re W n BT /In 1000 Tf [(b) -500 (b)] TJ ET Q"), []byte("1000 0 d0 4 w 0 0 m 10 0 l 10 10 l s"))
	f.Fuzz(func(t *testing.T, outer, nested []byte) {
		if len(outer) > fuzzType3ProcBytes || len(nested) > fuzzType3ProcBytes {
			t.Skip()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		data := liftedStrokesPDF(string(outer), string(nested), "2000 w ")
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
		ops, err := content.ParsePageContent(ctx, doc, pages[0].Dict)
		if err != nil {
			return
		}
		// An error is an outcome the walk may report on such input: the
		// assertion is that it returns.
		_, _, _, _ = ExtractElementsWithAttribution(ctx, doc, graphics.NewMachine([4]float64{0, 0, 612, 792}), pages[0], ops) //nolint:dogsled // only the absence of a panic is asserted
	})
}
