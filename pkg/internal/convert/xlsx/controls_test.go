package xlsx

import (
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// controlWorkbook returns a workbook of one sheet whose legacy VML
// drawing holds the shapes vml.
func controlWorkbook(t *testing.T, vml string) []byte {
	t.Helper()
	sh := testSheet{
		name: "S",
		xml:  `<sheetData><row r="1"><c r="A1" t="inlineStr"><is><t>Red</t></is></c></row><row r="2"><c r="A2" t="inlineStr"><is><t>Green</t></is></c></row></sheetData><legacyDrawing r:id="rIdV"/>`,
		rels: `<Relationship Id="rIdV" Type="` + relVMLDrawing + `" Target="../drawings/vmlDrawing1.vml"/>`,
	}
	files := workbookFiles([]testSheet{sh}, minimalStyles, "", "", map[string]string{
		"xl/drawings/vmlDrawing1.vml": `<xml xmlns:v="urn:schemas-microsoft-com:vml" xmlns:o="urn:schemas-microsoft-com:office:office" xmlns:x="urn:schemas-microsoft-com:office:excel">` + vml + `</xml>`,
	})
	return zipFiles(t, files)
}

// TestControls_PrintedAsExcelPrintsThem locks the form controls of a
// sheet's legacy drawing (ECMA-376 Part 4, 14.4 x:ClientData): each
// control placed by its x:Anchor in cells and pixels, its caption drawn;
// a control whose PrintObject is False and a comment (Note) left out.
func TestControls_PrintedAsExcelPrintsThem(t *testing.T) {
	t.Parallel()
	vml := `<v:shape style="position:absolute"><v:textbox><div><font face="Test Sans" size="160">Check
   Box 1</font></div></v:textbox><x:ClientData ObjectType="Checkbox"><x:Anchor>1, 10, 3, 2, 3, 40, 4, 18</x:Anchor><x:Checked>1</x:Checked></x:ClientData></v:shape>` +
		`<v:shape><x:ClientData ObjectType="Drop"><x:Anchor>1, 0, 6, 0, 3, 0, 7, 0</x:Anchor><x:FmlaRange>$A$1:$A$2</x:FmlaRange><x:Sel>2</x:Sel></x:ClientData></v:shape>` +
		`<v:shape><v:textbox><div>Hidden</div></v:textbox><x:ClientData ObjectType="Button"><x:Anchor>1, 0, 8, 0, 3, 0, 9, 0</x:Anchor><x:PrintObject>False</x:PrintObject></x:ClientData></v:shape>` +
		`<v:shape><x:ClientData ObjectType="Note"><x:Anchor>1, 0, 8, 0, 3, 0, 9, 0</x:Anchor></x:ClientData></v:shape>`
	doc, warnings := readBytes(t, controlWorkbook(t, vml))
	if len(warnings) > 0 {
		t.Errorf("warnings %v", warnings)
	}
	ds := doc.Sheets[0].Drawings
	if len(ds) != 2 {
		t.Fatalf("%d drawings, want the check box and the drop-down", len(ds))
	}
	cb := ds[0]
	if cb.From.Column != 1 || cb.From.Row != 3 || cb.From.DX != 7.5 || cb.From.DY != 1.5 || cb.To == nil || cb.To.Column != 3 || cb.To.DY != 13.5 {
		t.Errorf("check box anchored %+v to %+v", cb.From, cb.To)
	}
	if cb.Alt != "Check Box 1" {
		t.Errorf("caption %q", cb.Alt)
	}
	if ds[1].Alt != "Green" {
		t.Errorf("drop-down shows %q, want its second item", ds[1].Alt)
	}
	box := typeset.Rect{Width: 100, Height: 20}
	var texts []string
	paths := 0
	var walk func([]typeset.Fragment)
	walk = func(fs []typeset.Fragment) {
		for _, f := range fs {
			switch f := f.(type) {
			case *typeset.Group:
				walk(f.Content)
			case *typeset.Text:
				for _, g := range f.Glyphs {
					texts = append(texts, g.Text)
				}
			case *typeset.Path:
				paths++
			}
		}
	}
	walk(cb.Picture.Vector(t.Context(), box))
	// The square and the check mark, then the caption.
	if paths != 2 || !strings.Contains(strings.Join(texts, ""), "Check") {
		t.Errorf("check box paints %d paths and %q", paths, texts)
	}
}

// TestClientAnchor locks x:Anchor (14.4.2.2): eight integers, the
// offsets in pixels of 0.75 point; anything else is refused.
func TestClientAnchor(t *testing.T) {
	t.Parallel()
	from, to, ok := clientAnchor(" 0, 46, 2, 8,\n 3, 38, 5, 0")
	if !ok || from.Column != 0 || from.DX != 34.5 || from.Row != 2 || from.DY != 6 || to.Column != 3 || to.Row != 5 {
		t.Errorf("anchor %+v %+v %v", from, to, ok)
	}
	for _, bad := range []string{"", "1,2,3", "a,b,c,d,e,f,g,h", "1,2,3,4,5,6,7,8,9"} {
		if _, _, ok := clientAnchor(bad); ok {
			t.Errorf("clientAnchor(%q) reads", bad)
		}
	}
}
