package odp

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/convert/slides"
	"github.com/gotenberg/litho/pkg/internal/css"
)

func TestRead_Document(t *testing.T) {
	t.Run("size names hidden and meta", func(t *testing.T) {
		f := flat{
			meta: `<dc:title>Deck</dc:title><dc:creator>Ann</dc:creator><meta:keyword>a</meta:keyword><meta:keyword>b</meta:keyword><dc:language>fr-FR</dc:language>`,
			auto: `<style:page-layout style:name="pm1"><style:page-layout-properties fo:page-width="25.4cm" fo:page-height="19.05cm"/></style:page-layout>` +
				`<style:page-layout style:name="pn"><style:page-layout-properties fo:page-width="21cm" fo:page-height="29.7cm"/></style:page-layout>` +
				`<style:style style:name="dpH" style:family="drawing-page"><style:drawing-page-properties presentation:visibility="hidden"/></style:style>`,
			master: `<style:master-page style:name="M" style:page-layout-name="pm1"><presentation:notes style:page-layout-name="pn"/></style:master-page>`,
			body: `<draw:page draw:name="page1" draw:master-page-name="M"/>` +
				`<draw:page draw:name="Intro" draw:style-name="dpH" draw:master-page-name="M"/>` +
				`<draw:page/>`,
		}
		doc, _ := read(t, f.bytes())
		if !near(doc.Width, cm(25.4)) || !near(doc.Height, cm(19.05)) {
			t.Errorf("size = %v x %v", doc.Width, doc.Height)
		}
		if !near(doc.NotesWidth, cm(21)) || !near(doc.NotesHeight, cm(29.7)) {
			t.Errorf("notes size = %v x %v", doc.NotesWidth, doc.NotesHeight)
		}
		var names []string
		var hidden []bool
		for _, s := range doc.Slides {
			names = append(names, s.Name)
			hidden = append(hidden, s.Hidden)
		}
		if want := []string{"Slide 1", "Intro", "Slide 3"}; !slices.Equal(names, want) {
			t.Errorf("names = %q, want %q", names, want)
		}
		if want := []bool{false, true, false}; !slices.Equal(hidden, want) {
			t.Errorf("hidden = %v, want %v", hidden, want)
		}
		if m := doc.Meta; m.Title != "Deck" || m.Author != "Ann" || m.Keywords != "a, b" || m.Lang != "fr-FR" {
			t.Errorf("meta = %+v", m)
		}
	})
	t.Run("default size", func(t *testing.T) {
		f := flat{master: `<style:master-page style:name="M"/>`, body: `<draw:page/>`}
		doc, _ := read(t, f.bytes())
		if !near(doc.Width, cm(28)) || !near(doc.Height, cm(15.75)) || doc.NotesWidth != 0 {
			t.Errorf("size = %v x %v, notes %v", doc.Width, doc.Height, doc.NotesWidth)
		}
	})
	t.Run("zero size", func(t *testing.T) {
		f := flat{
			auto:   `<style:page-layout style:name="pm1"><style:page-layout-properties fo:page-width="0cm" fo:page-height="0cm" style:print-orientation="portrait"/></style:page-layout>`,
			master: `<style:master-page style:name="M" style:page-layout-name="pm1"/>`, body: `<draw:page/>`,
		}
		doc, _ := read(t, f.bytes())
		if !near(doc.Width, cm(21)) || !near(doc.Height, cm(29.7)) {
			t.Errorf("size = %v x %v, want A4", doc.Width, doc.Height)
		}
	})
}

func TestRead_Errors(t *testing.T) {
	text := []byte(`<office:document ` + nsDecl + ` office:mimetype="application/vnd.oasis.opendocument.text"><office:body><office:text/></office:body></office:document>`)
	nobody := []byte(`<office:document ` + nsDecl + `><office:body/></office:document>`)
	for _, tt := range []struct {
		name string
		data []byte
	}{{"text document", text}, {"no presentation body", nobody}, {"not XML", []byte("hello")}} {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, err := Read(context.Background(), tt.data, nil, ""); !errors.Is(err, office.ErrMalformed) {
				t.Errorf("err = %v, want ErrMalformed", err)
			}
		})
	}
	t.Run("package", func(t *testing.T) {
		data := zipOf(t, "mimetype", "application/vnd.oasis.opendocument.presentation",
			"content.xml", `<office:document-content `+nsDecl+`><office:body><office:presentation><draw:page/></office:presentation></office:body></office:document-content>`)
		doc, _ := read(t, data)
		if len(doc.Slides) != 1 {
			t.Errorf("slides = %d", len(doc.Slides))
		}
	})
	t.Run("cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, _, err := Read(ctx, flat{body: page("")}.bytes(), nil, ""); !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want context.Canceled", err)
		}
	})
}

func TestRead_Background(t *testing.T) {
	auto := `<style:style style:name="Mdp" style:family="drawing-page"><style:drawing-page-properties draw:fill="solid" draw:fill-color="#0000ff"/></style:style>` +
		`<style:style style:name="red" style:family="drawing-page"><style:drawing-page-properties draw:fill="solid" draw:fill-color="#ff0000"/></style:style>` +
		`<style:style style:name="none" style:family="drawing-page"><style:drawing-page-properties draw:fill="none"/></style:style>` +
		`<style:style style:name="off" style:family="drawing-page"><style:drawing-page-properties presentation:background-visible="false"/></style:style>`
	master := `<style:master-page style:name="Default" draw:style-name="Mdp"/>`
	tests := []struct {
		style string
		want  css.Color
	}{
		{"", css.Color{B: 1, A: 1}},
		{"red", css.Color{R: 1, A: 1}},
		{"none", css.Color{B: 1, A: 1}},
		{"off", css.Color{}},
	}
	for _, tt := range tests {
		t.Run(tt.style, func(t *testing.T) {
			doc, _ := read(t, flat{auto: auto, master: master, body: `<draw:page draw:style-name="` + tt.style + `" draw:master-page-name="Default"/>`}.bytes())
			if got := doc.Slides[0].Background.Color; got != tt.want {
				t.Errorf("background = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRead_MasterShapes(t *testing.T) {
	frame := func(class, text string) string {
		return `<draw:frame presentation:class="` + class + `" svg:x="1cm" svg:y="1cm" svg:width="5cm" svg:height="1cm"><draw:text-box><text:p>` + text + `</text:p></draw:text-box></draw:frame>`
	}
	master := `<style:master-page style:name="Default">` +
		frame("title", "TITLE-TEMPLATE") + frame("outline", "OUTLINE-TEMPLATE") +
		frame("footer", "<presentation:footer/>") + frame("date-time", "<presentation:date-time/>") +
		frame("page-number", "<text:page-number>&lt;number&gt;</text:page-number>") +
		`<draw:custom-shape draw:layer="hidden-layer" svg:width="1cm" svg:height="1cm"><text:p>HIDDEN</text:p><draw:enhanced-geometry draw:type="rectangle"/></draw:custom-shape>` +
		`<draw:custom-shape svg:width="1cm" svg:height="1cm"><text:p>LOGO</text:p><draw:enhanced-geometry draw:type="rectangle"/></draw:custom-shape>` +
		`</style:master-page>` +
		`<draw:layer-set><draw:layer draw:name="hidden-layer" draw:display="none"/></draw:layer-set>`
	auto := `<style:style style:name="all" style:family="drawing-page"><style:drawing-page-properties presentation:display-page-number="true"/></style:style>` +
		`<style:style style:name="off" style:family="drawing-page"><style:drawing-page-properties presentation:display-footer="false" presentation:display-date-time="false" presentation:background-objects-visible="false"/></style:style>`
	decls := `<presentation:footer-decl presentation:name="ftr1">FOOT</presentation:footer-decl>` +
		`<presentation:date-time-decl presentation:name="dtd1" presentation:source="fixed">FIXED-DATE</presentation:date-time-decl>` +
		`<presentation:date-time-decl presentation:name="dtd2" presentation:source="current">8/11/26</presentation:date-time-decl>`
	tests := []struct {
		name, page string
		want       []string
		field      slides.Field
	}{
		{
			"defaults", `<draw:page draw:master-page-name="Default" presentation:use-footer-name="ftr1" presentation:use-date-time-name="dtd1"/>`,
			[]string{"FOOT", "FIXED-DATE", "LOGO"},
			slides.FieldNone,
		},
		{
			"page number shown", `<draw:page draw:style-name="all" draw:master-page-name="Default" presentation:use-date-time-name="dtd2"/>`,
			[]string{"", "8/11/26", "<number>", "LOGO"},
			slides.FieldDate,
		},
		{"objects hidden", `<draw:page draw:style-name="off" draw:master-page-name="Default"/>`, nil, slides.FieldNone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := flat{auto: auto, master: master, body: decls + tt.page}
			f.auto += `<style:page-layout style:name="pm1"><style:page-layout-properties fo:page-width="28cm" fo:page-height="15.75cm"/></style:page-layout>`
			doc, _ := read(t, f.bytes())
			var got []string
			for _, s := range doc.Slides[0].Shapes {
				got = append(got, texts(s.Text.Paragraphs)...)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("master texts = %q, want %q", got, tt.want)
			}
			if tt.field != slides.FieldNone {
				if r := doc.Slides[0].Shapes[1].Text.Paragraphs[0].Runs[0]; r.Field != tt.field {
					t.Errorf("date run = %+v", r)
				}
			}
		})
	}
}

func TestRead_TitleAndNotes(t *testing.T) {
	body := `<draw:page draw:master-page-name="Default">` +
		`<draw:frame presentation:class="title" svg:width="5cm" svg:height="1cm"><draw:text-box><text:p>Q4 <text:span>Review</text:span></text:p><text:p>2026</text:p></draw:text-box></draw:frame>` +
		`<draw:frame presentation:class="subtitle" presentation:placeholder="true" svg:width="5cm" svg:height="1cm"><draw:text-box/></draw:frame>` +
		`<presentation:notes><draw:page-thumbnail presentation:class="page"/>` +
		`<draw:frame presentation:class="notes" svg:width="5cm" svg:height="1cm"><draw:text-box><text:p>NOTE-1</text:p><text:p>NOTE-2</text:p></draw:text-box></draw:frame>` +
		`</presentation:notes></draw:page>` +
		`<draw:page draw:master-page-name="Default"><presentation:notes><draw:frame svg:width="5cm" svg:height="1cm"><draw:text-box><text:p>LOOSE</text:p></draw:text-box></draw:frame></presentation:notes></draw:page>` +
		`<draw:page draw:master-page-name="Default"><presentation:notes/></draw:page><draw:page draw:master-page-name="Default"/>`
	doc, _ := read(t, flat{body: body}.bytes())
	for i, want := range []bool{true, true, false, false} {
		if doc.Slides[i].HasNotes != want {
			t.Errorf("slide %d has notes %v, want %v", i+1, doc.Slides[i].HasNotes, want)
		}
	}
	if got := doc.Slides[0].Title; got != "Q4 Review 2026" {
		t.Errorf("title = %q", got)
	}
	if sh := doc.Slides[0].Shapes; len(sh) != 2 || len(sh[1].Text.Paragraphs) != 0 {
		t.Errorf("shapes = %+v, want the title and the empty placeholder without text", sh)
	}
	if got := texts(doc.Slides[0].Notes); !slices.Equal(got, []string{"NOTE-1", "NOTE-2"}) {
		t.Errorf("notes = %q", got)
	}
	if got := texts(doc.Slides[1].Notes); !slices.Equal(got, []string{"LOOSE"}) {
		t.Errorf("notes of a classless frame = %q", got)
	}
}

func TestRead_Warnings(t *testing.T) {
	body := page(`<anim:par/><dr3d:scene/><draw:measure/><draw:control/><office:annotation><text:p>c</text:p></office:annotation>`) +
		`<presentation:show presentation:name="s" presentation:pages="page1"/>`
	_, w := read(t, flat{body: body}.bytes())
	for _, code := range []string{"animations", "3d-scene", "measure", "form-controls", "comments", "custom-show"} {
		if !hasWarning(w, code) {
			t.Errorf("no %q warning in %v", code, w)
		}
	}
}

func TestRead_NotesMaster(t *testing.T) {
	master := `<style:master-page style:name="Default" style:page-layout-name="pm1"><presentation:notes>` +
		`<draw:page-thumbnail presentation:class="page" svg:x="2cm" svg:y="3cm" svg:width="15cm" svg:height="8cm"/>` +
		`<draw:frame presentation:class="notes" svg:x="2cm" svg:y="12cm" svg:width="15cm" svg:height="10cm"><draw:text-box/></draw:frame>` +
		`<draw:frame presentation:class="date-time" svg:x="10cm" svg:y="0cm" svg:width="8cm" svg:height="1cm"><draw:text-box><text:p>DATE</text:p></draw:text-box></draw:frame>` +
		`<draw:rect svg:x="1cm" svg:y="1cm" svg:width="1cm" svg:height="1cm"/>` +
		`</presentation:notes></style:master-page>`
	f := flat{master: master, body: page(""), auto: `<style:page-layout style:name="pm1"><style:page-layout-properties fo:page-width="28cm" fo:page-height="15.75cm"/></style:page-layout>`}
	doc, _ := read(t, f.bytes())
	if !nearRect(doc.NotesSlideBox, rect(2, 3, 15, 8)) || !nearRect(doc.NotesTextBox, rect(2, 12, 15, 10)) {
		t.Errorf("notes boxes = %+v %+v", doc.NotesSlideBox, doc.NotesTextBox)
	}
	if len(doc.NotesShapes) != 1 || !nearRect(doc.NotesShapes[0].Box, rect(1, 1, 1, 1)) {
		t.Errorf("notes shapes = %+v, want the rectangle alone", doc.NotesShapes)
	}
}
