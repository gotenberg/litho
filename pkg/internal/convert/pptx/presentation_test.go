package pptx

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/convert/slides"
)

// title returns a title placeholder of text s.
func title(s string) string { return phShape("Title 1", "title", -1, 0, 0, 0, 0, para(s)) }

// textBox returns a text box named name at x, y, w, h EMU with the text
// body body.
func textBox(name string, x, y, w, h int, body string) string {
	return `<p:sp><p:nvSpPr><p:cNvPr id="5" name="` + name + `"/><p:cNvSpPr txBox="1"/><p:nvPr/></p:nvSpPr><p:spPr><a:xfrm>` +
		`<a:off x="` + itoa(x) + `" y="` + itoa(y) + `"/><a:ext cx="` + itoa(w) + `" cy="` + itoa(h) + `"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></p:spPr>` +
		`<p:txBody>` + body + `</p:txBody></p:sp>`
}

// itoa formats n.
func itoa(n int) string { return strconv.Itoa(n) }

func TestPresentation_Slides(t *testing.T) {
	t.Parallel()
	d := testDeck{
		sldSz: `<p:sldSz cx="12192000" cy="6858000"/>`,
		slides: []testSlide{
			{tree: title("First")},
			{tree: title("Second"), attrs: `show="0"`, cSldAttrs: `name="Named"`},
			{tree: textBox("TB", 0, 0, 914400, 914400, para("no title")), notes: phShape("Notes", "body", 3, 0, 0, 0, 0, para("NOTE-ONE", "NOTE-TWO"))},
		},
	}
	files := deckFiles(d)
	// Reorder the slide list: the show order is sldIdLst's, not the parts'.
	files["ppt/presentation.xml"] = strings.Replace(files["ppt/presentation.xml"],
		`<p:sldId id="256" r:id="rIdS1"/><p:sldId id="257" r:id="rIdS2"/>`, `<p:sldId id="257" r:id="rIdS2"/><p:sldId id="256" r:id="rIdS1"/>`, 1)
	doc, _, err := load(context.Background(), bytes.NewReader(zipFiles(t, files)), testFonts(t), office.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Width != 960 || doc.Height != 540 || doc.NotesWidth != 540 || doc.NotesHeight != 720 {
		t.Errorf("sizes %v x %v, notes %v x %v", doc.Width, doc.Height, doc.NotesWidth, doc.NotesHeight)
	}
	if doc.Meta.Title != "Test deck" || doc.Meta.Author != "litho" {
		t.Errorf("meta %+v", doc.Meta)
	}
	var titles, names []string
	var hidden []bool
	for _, s := range doc.Slides {
		titles, names, hidden = append(titles, s.Title), append(names, s.Name), append(hidden, s.Hidden)
	}
	if want := []string{"Second", "First", ""}; !slices.Equal(titles, want) {
		t.Errorf("titles %q, want %q", titles, want)
	}
	if want := []string{"Second", "First", "Slide 3"}; !slices.Equal(names, want) {
		t.Errorf("names %q, want %q", names, want)
	}
	if want := []bool{true, false, false}; !slices.Equal(hidden, want) {
		t.Errorf("hidden %v, want %v", hidden, want)
	}
	notes := doc.Slides[2].Notes
	if len(notes) != 2 || notes[0].Runs[0].Text != "NOTE-ONE" || notes[1].Runs[0].Style.Size != 12 {
		t.Errorf("notes %+v", notes)
	}
	if doc.Slides[0].Notes != nil {
		t.Errorf("a slide without notes has %+v", doc.Slides[0].Notes)
	}
}

func TestPresentation_Errors(t *testing.T) {
	t.Parallel()
	good := deckFiles(testDeck{slides: []testSlide{{tree: title("x")}}})
	for _, tc := range []struct {
		name   string
		mutate func(map[string]string)
		want   error
	}{
		{"not a zip", nil, office.ErrMalformed},
		{"main part is a workbook", func(f map[string]string) {
			f["ppt/presentation.xml"] = `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"/>`
		}, office.ErrMalformed},
		{"no main part", func(f map[string]string) { delete(f, "ppt/presentation.xml") }, office.ErrMalformed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data := []byte("not a zip")
			if tc.mutate != nil {
				files := map[string]string{}
				for k, v := range good {
					files[k] = v
				}
				tc.mutate(files)
				data = zipFiles(t, files)
			}
			_, _, err := load(context.Background(), bytes.NewReader(data), testFonts(t), office.Options{})
			if !errors.Is(err, tc.want) {
				t.Fatalf("err %v, want %v", err, tc.want)
			}
		})
	}
}

func TestPresentation_BrokenSlideIsSkipped(t *testing.T) {
	t.Parallel()
	files := deckFiles(testDeck{slides: []testSlide{{tree: title("a")}, {tree: title("b")}}})
	files["ppt/slides/slide1.xml"] = `<p:sld ` + nsDecl + `><p:cSld>`
	doc, warnings, err := load(context.Background(), bytes.NewReader(zipFiles(t, files)), testFonts(t), office.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Slides) != 1 || doc.Slides[0].Title != "b" {
		t.Fatalf("slides %+v", doc.Slides)
	}
	if !hasWarning(warnings, "malformed-part") {
		t.Errorf("warnings %+v", warnings)
	}
}

func TestPresentation_Warnings(t *testing.T) {
	t.Parallel()
	_, warnings := readDeck(t, testDeck{slides: []testSlide{{
		tree:  title("x"),
		after: `<p:transition spd="slow"/><p:timing/>`,
	}}})
	for _, want := range []string{"slide transitions do not print", "slide animations do not print"} {
		found := false
		for _, w := range warnings {
			found = found || strings.HasPrefix(w.Message, want)
		}
		if !found {
			t.Errorf("no warning %q in %+v", want, warnings)
		}
	}
}

// hasWarning reports a warning of code.
func hasWarning(ws []compose.Warning, code string) bool {
	for _, w := range ws {
		if w.Code == code {
			return true
		}
	}
	return false
}

// shapeNames returns the names of shapes.
func shapeNames(shapes []*slides.Shape) []string {
	var out []string
	for _, s := range shapes {
		out = append(out, s.Name)
	}
	return out
}
