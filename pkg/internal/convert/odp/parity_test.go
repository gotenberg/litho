package odp

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/slides"
	"github.com/gotenberg/litho/pkg/internal/css"
)

// TestParity_Pitch reads Gotenberg's pitch deck, the package and its flat
// twin, from the odp corpus's sources: four slides, PITCH-1 to PITCH-4 in
// boxes of the built-in standard style.
//
// # Corpus evidence
//
//   - odp: pitch and pitch-flat print four 28 cm by 15.75 cm pages, the
//     slide's text 18 points of Liberation Sans in white on green boxes of
//     a 0.106 cm darker green border, the title at 2 cm, 1 cm, the bullet
//     list's text 0.6 cm right of its bullet; the notes of slide 2 hold
//     PITCH-NOTES-MARKER-2.
func TestParity_Pitch(t *testing.T) {
	for _, name := range []string{"pitch/pitch.odp", "pitch-flat/pitch.fodp"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "test", "integration", "corpus", "gotenberg", "odp", "sources", name))
			if os.IsNotExist(err) {
				t.Skip("the odp corpus's sources are not in this tree")
			}
			if err != nil {
				t.Fatal(err)
			}
			doc, _ := read(t, data)
			if len(doc.Slides) != 4 || !near(doc.Width, cm(28)) || !near(doc.Height, cm(15.75)) {
				t.Fatalf("slides %d, size %v x %v", len(doc.Slides), doc.Width, doc.Height)
			}
			for i, s := range doc.Slides {
				var boxes []*slides.Shape
				for _, sh := range s.Shapes {
					if sh.Fill.Kind == slides.FillSolid {
						boxes = append(boxes, sh)
					}
				}
				if len(boxes) != 2 {
					t.Fatalf("slide %d: %d filled boxes", i+1, len(boxes))
				}
				title, list := boxes[0], boxes[1]
				if !nearRect(title.Box, rect(2, 1, 24, 3)) || !nearRect(list.Box, rect(2, 5, 24, 10)) || !near(title.Line.Width, cm(0.106)) {
					t.Errorf("slide %d: boxes %+v %+v", i+1, title.Box, list.Box)
				}
				if got := texts(title.Text.Paragraphs); !slices.Equal(got, []string{"PITCH-" + string(rune('1'+i))}) {
					t.Errorf("slide %d: title %q", i+1, got)
				}
				p := list.Text.Paragraphs[0]
				if p.Bullet == nil || !near(p.MarginLeft, cm(0.6)) || !near(p.Indent, -cm(0.6)) || p.Style.Size != 18 || p.Style.Families[0] != "Liberation Sans" || p.Style.Color != (css.Color{R: 1, G: 1, B: 1, A: 1}) {
					t.Errorf("slide %d: list paragraph %+v", i+1, p)
				}
			}
			if got := texts(doc.Slides[1].Notes); !slices.Equal(got, []string{"PITCH-NOTES-MARKER-2"}) {
				t.Errorf("notes = %q", got)
			}
		})
	}
}
