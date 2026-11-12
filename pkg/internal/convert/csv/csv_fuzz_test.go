package csv

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
)

// FuzzRead reads arbitrary bytes at a fuzzed separator, with type
// detection on or off: a separator the options refuse fails with
// office.ErrInvalidOptions, anything else reads to one sheet of finite
// values.
func FuzzRead(f *testing.F) {
	seed := []byte("id,\"a \"\"b\"\"\",2026-01-10\r\n1;15%;$5\t12:30\n\"x\ny\",TRUE,1/10/2026 10:00\n")
	f.Add(seed, ',', false)
	f.Add(seed[:len(seed)/2], ';', true)
	f.Add([]byte("a b§c\n"), '§', false)
	f.Fuzz(func(t *testing.T, data []byte, sep rune, keepText bool) {
		opts := office.Options{CSVSeparators: []rune{sep, '\t'}, CSVKeepText: keepText}
		doc, _, err := Read(context.Background(), data, nil, opts)
		if errors.Is(err, office.ErrInvalidOptions) {
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range doc.Sheets[0].Rows {
			for _, c := range r.Cells {
				if c.Value.Kind == sheet.Number && (keepText || math.IsNaN(c.Value.Number) || math.IsInf(c.Value.Number, 0)) {
					t.Fatalf("value %v, keep text %v", c.Value, keepText)
				}
			}
		}
	})
}
