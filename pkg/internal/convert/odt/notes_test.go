package odt

import (
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
)

// TestSectionCollectsNotes locks that a section whose properties hold a
// notes configuration sets its footnotes after its text, not at the foot
// of the page (see [builder.sectionNotes]).
func TestSectionCollectsNotes(t *testing.T) {
	auto := `<style:style style:name="S1" style:family="section"><style:section-properties><text:notes-configuration text:note-class="footnote"/></style:section-properties></style:style>`
	note := `<text:note text:note-class="footnote"><text:note-citation>1</text:note-citation><text:note-body><text:p>NOTE</text:p></text:note-body></text:note>`
	for _, collect := range []bool{false, true} {
		style := ""
		if collect {
			style = ` text:style-name="S1"`
		}
		body := `<text:section text:name="A"` + style + `><text:p>Inside` + note + `</text:p></text:section><text:p>After</text:p>`
		pg := convertDoc(t, doc{contentAuto: auto, body: body}.pack(t), office.Options{}).Pages[0]
		y := map[string]float64{}
		for _, r := range runs(pg) {
			y[strings.TrimSpace(r.text)] = r.y
		}
		if got := y["NOTE"] < y["After"]; got != collect {
			t.Errorf("collected %v: NOTE at %.2f, After at %.2f", collect, y["NOTE"], y["After"])
		}
	}
}
