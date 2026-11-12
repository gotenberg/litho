package docx

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/layout"
)

// TestTrailingLineBreak gives the mark of a paragraph that ends with a
// text wrapping break a line of its own, as Word prints it, and leaves a
// break inside the text as it is.
func TestTrailingLineBreak(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
		want int
	}{
		{"trailing", `<w:p><w:r><w:t>a</w:t><w:br/></w:r></w:p>`, 2},
		{"alone", `<w:p><w:r><w:br/></w:r></w:p>`, 2},
		{"inside", `<w:p><w:r><w:t>a</w:t><w:br/><w:t>b</w:t></w:r></w:p>`, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b := build(t, tt.body, nil)
			ps := role(b.root, "P")
			if len(ps) != 1 {
				t.Fatalf("paragraphs %d, want 1", len(ps))
			}
			if got := countBreaks(ps[0]); got != tt.want {
				t.Errorf("line breaks %d, want %d", got, tt.want)
			}
		})
	}
}

// countBreaks counts the line breaks under n.
func countBreaks(n *layout.Node) int {
	c := 0
	if n.Kind == layout.LineBreak {
		c++
	}
	for _, k := range n.Children {
		c += countBreaks(k)
	}
	return c
}
