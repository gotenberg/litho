package text

import (
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
)

// TestLayout_ClusterMatchingComposes locks CSS Fonts 4, 5.4: a base and a
// combining mark the face lacks, whose canonical equivalent the face
// holds, stay in that face and draw as the precomposed glyph, not the
// mark from a fallback face.
func TestLayout_ClusterMatchingComposes(t *testing.T) {
	precomposed := fontbuild.TrueType{
		PostScriptName: "TestComposed", Family: "Test Composed", Subfamily: "Regular", NotdefAdvance: 500,
		Glyphs: []fontbuild.Glyph{
			{Name: "space", Unicode: ' ', Advance: spaceAdvance, Empty: true},
			{Name: "A", Unicode: 'A', Advance: 600},
			{Name: "Aacute", Unicode: 'Á', Advance: 700},
		},
	}.Build()
	marks := fontbuild.TrueType{
		PostScriptName: "TestMarks", Family: "Test Marks", Subfamily: "Regular", NotdefAdvance: 500,
		Glyphs: []fontbuild.Glyph{
			{Name: "space", Unicode: ' ', Advance: spaceAdvance, Empty: true},
			{Name: "A", Unicode: 'A', Advance: 500},
			{Name: "acutecomb", Unicode: 0x301, Advance: 0},
		},
	}.Build()
	f := newTestFonts(t, fontFile{name: "c.ttf", data: precomposed}, fontFile{name: "m.ttf", data: marks})
	s := Style{Families: []string{"Test Composed", "Test Marks"}, Size: 10}
	lines, _ := layout(t, f, "Á", s, 1000, Collapse)
	if len(lines) != 1 || len(lines[0].Runs) != 1 || len(lines[0].Runs[0].Glyphs) != 1 {
		t.Fatalf("lines %+v, want one run of one glyph", lines)
	}
	if got := lines[0].Runs[0].Face.Key; !strings.Contains(got, "c.ttf") {
		t.Errorf("face %q, want the face of c.ttf", got)
	}
}
