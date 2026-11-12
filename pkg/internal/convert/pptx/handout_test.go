package pptx

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/convert/slides"
)

// handoutFiles returns the parts of a two-slide deck with a handout master
// of a red background, a decoration, and header, date, footer and slide
// number placeholders; hf is the master's header and footer flags.
func handoutFiles(hf string) map[string]string {
	files := deckFiles(testDeck{slides: []testSlide{{tree: title("One")}, {tree: title("Two")}}})
	files["ppt/handoutMasters/handoutMaster1.xml"] = `<p:handoutMaster ` + nsDecl + `><p:cSld><p:bg><p:bgPr><a:solidFill><a:srgbClr val="FF0000"/></a:solidFill><a:effectLst/></p:bgPr></p:bg><p:spTree>` + spGroup +
		phShape("Header Placeholder 1", "hdr", -1, 0, 0, 2971800, 458788, para("HM-HEADER")) +
		phShape("Date Placeholder 2", "dt", 1, 3884613, 0, 2971800, 458788, `<a:bodyPr/><a:lstStyle/><a:p><a:fld id="{1}" type="datetimeFigureOut"><a:t>1/1/2026</a:t></a:fld></a:p>`) +
		phShape("Footer Placeholder 3", "ftr", 2, 0, 8685213, 2971800, 458787, para("HM-FOOTER")) +
		phShape("Slide Number Placeholder 4", "sldNum", 3, 3884613, 8685213, 2971800, 458787, `<a:bodyPr/><a:lstStyle/><a:p><a:fld id="{2}" type="slidenum"><a:t>‹#›</a:t></a:fld></a:p>`) +
		textBox("Logo", 0, 4000000, 914400, 914400, para("HM-LOGO")) +
		`</p:spTree></p:cSld><p:clrMap bg1="lt1" tx1="dk1" bg2="lt2" tx2="dk2" accent1="accent1" accent2="accent2" accent3="accent3" accent4="accent4" accent5="accent5" accent6="accent6" hlink="hlink" folHlink="folHlink"/>` + hf + `</p:handoutMaster>`
	files["ppt/handoutMasters/_rels/handoutMaster1.xml.rels"] = `<Relationships ` + relsNS + `><Relationship Id="rId1" Type="` + tRelTheme + `" Target="../theme/theme1.xml"/></Relationships>`
	files["ppt/presentation.xml"] = strings.Replace(files["ppt/presentation.xml"], `</p:notesMasterIdLst>`, `</p:notesMasterIdLst><p:handoutMasterIdLst><p:handoutMasterId r:id="rIdH"/></p:handoutMasterIdLst>`, 1)
	files["ppt/_rels/presentation.xml.rels"] = strings.Replace(files["ppt/_rels/presentation.xml.rels"], `</Relationships>`, `<Relationship Id="rIdH" Type="`+relHandoutMaster+`" Target="handoutMasters/handoutMaster1.xml"/></Relationships>`, 1)
	files["[Content_Types].xml"] = contentTypes(files)
	return files
}

// shapeTexts returns the text of each shape that has some, in order.
func shapeTexts(shapes []*slides.Shape) []string {
	var out []string
	for _, s := range shapes {
		if s.Text == nil {
			continue
		}
		var b strings.Builder
		for _, p := range s.Text.Paragraphs {
			for _, r := range p.Runs {
				b.WriteString(r.Text)
			}
		}
		out = append(out, b.String())
	}
	return out
}

// TestHandoutMaster checks that a conversion with handouts reads the
// handout master: its background, its decoration, and the placeholders
// its header and footer flags show, the slide number a field; and that
// one without handouts leaves it out.
func TestHandoutMaster(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		hf       string
		handouts int
		want     []string
	}{
		{"every flag", "", 4, []string{"HM-LOGO", "HM-HEADER", "1/1/2026", "HM-FOOTER", "‹#›"}},
		{"header and date off", `<p:hf hdr="0" dt="0"/>`, 4, []string{"HM-LOGO", "HM-FOOTER", "‹#›"}},
		{"no handouts", "", 0, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, _, err := load(context.Background(), bytes.NewReader(zipFiles(t, handoutFiles(tc.hf))), testFonts(t), office.Options{Handouts: tc.handouts})
			if err != nil {
				t.Fatal(err)
			}
			if got := shapeTexts(doc.HandoutShapes); strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Errorf("handout shapes %q, want %q", got, tc.want)
			}
			for _, s := range doc.HandoutShapes {
				if !s.Decorative {
					t.Errorf("shape %q is not an artifact", s.Name)
				}
				if s.Name == "Slide Number Placeholder 4" && s.Text.Paragraphs[0].Runs[0].Field != slides.FieldSlideNumber {
					t.Errorf("slide number %+v", s.Text.Paragraphs[0].Runs[0])
				}
			}
			if red := (tc.handouts > 0); red != (doc.HandoutBackground.Kind == slides.FillSolid) {
				t.Errorf("background %+v", doc.HandoutBackground)
			}
		})
	}
}

// TestConvert_Handouts checks that a PPTX source with handouts prints its
// handout pages on the notes size, the handout master's footer on each.
func TestConvert_Handouts(t *testing.T) {
	t.Parallel()
	out, err := Convert(context.Background(), bytes.NewReader(zipFiles(t, handoutFiles(""))), testFonts(t), office.Options{Handouts: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Pages) != 2 {
		t.Fatalf("%d pages, want 2", len(out.Pages))
	}
	for i, pg := range out.Pages {
		if pg.Width != 540 || pg.Height != 720 {
			t.Errorf("page %d is %v by %v, want 540 by 720", i+1, pg.Width, pg.Height)
		}
	}
}

func FuzzHandoutMaster(f *testing.F) {
	master := handoutFiles("")["ppt/handoutMasters/handoutMaster1.xml"]
	f.Add([]byte(master))
	f.Add([]byte(master[:len(master)/2]))
	fonts := testFonts(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		files := handoutFiles("")
		files["ppt/handoutMasters/handoutMaster1.xml"] = string(data)
		doc, _, err := load(context.Background(), bytes.NewReader(zipFiles(t, files)), fonts, office.Options{Handouts: 6})
		if err != nil {
			return
		}
		checkShapes(t, doc.HandoutShapes)
	})
}
