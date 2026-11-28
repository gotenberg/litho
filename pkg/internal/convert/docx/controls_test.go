package docx

import (
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// controlsBody holds inline content controls of every kind a field takes,
// a picture control, and the three legacy form fields.
const controlsBody = `<w:p><w:r><w:t xml:space="preserve">Name: </w:t></w:r>` +
	`<w:sdt><w:sdtPr><w:alias w:val="Customer"/><w:tag w:val="customer"/><w:text/></w:sdtPr><w:sdtContent><w:r><w:t>ACME</w:t></w:r></w:sdtContent></w:sdt></w:p>` +
	`<w:p><w:sdt><w:sdtPr><w:dropDownList><w:listItem w:displayText="Red" w:value="r"/><w:listItem w:displayText="Green" w:value="g"/></w:dropDownList></w:sdtPr>` +
	`<w:sdtContent><w:r><w:t>Green</w:t></w:r></w:sdtContent></w:sdt></w:p>` +
	`<w:p><w:sdt><w:sdtPr><w:lock w:val="sdtContentLocked"/><w:comboBox><w:listItem w:displayText="Paris"/></w:comboBox></w:sdtPr>` +
	`<w:sdtContent><w:r><w:t>Rome</w:t></w:r></w:sdtContent></w:sdt></w:p>` +
	`<w:p><w:sdt><w:sdtPr><w14:checkbox xmlns:w14="http://schemas.microsoft.com/office/word/2010/wordml"><w14:checked w14:val="1"/></w14:checkbox></w:sdtPr>` +
	`<w:sdtContent><w:r><w:t>X</w:t></w:r></w:sdtContent></w:sdt></w:p>` +
	`<w:p><w:sdt><w:sdtPr><w:picture/></w:sdtPr><w:sdtContent><w:r><w:t>picture</w:t></w:r></w:sdtContent></w:sdt></w:p>` +
	`<w:p><w:r><w:fldChar w:fldCharType="begin"><w:ffData><w:name w:val="Text1"/><w:enabled/><w:textInput><w:maxLength w:val="10"/></w:textInput></w:ffData></w:fldChar></w:r>` +
	`<w:r><w:instrText xml:space="preserve"> FORMTEXT </w:instrText></w:r><w:r><w:fldChar w:fldCharType="separate"/></w:r><w:r><w:t>Jane</w:t></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r></w:p>` +
	`<w:p><w:r><w:fldChar w:fldCharType="begin"><w:ffData><w:name w:val="Fruit"/><w:enabled/><w:ddList><w:result w:val="1"/><w:listEntry w:val="Apple"/><w:listEntry w:val="Banana"/></w:ddList></w:ffData></w:fldChar></w:r>` +
	`<w:r><w:instrText xml:space="preserve"> FORMDROPDOWN </w:instrText></w:r><w:r><w:fldChar w:fldCharType="separate"/></w:r><w:r><w:t>Banana</w:t></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r></w:p>` +
	`<w:p><w:r><w:fldChar w:fldCharType="begin"><w:ffData><w:name w:val="Check1"/><w:checkBox><w:sizeAuto/><w:default w:val="1"/></w:checkBox></w:ffData></w:fldChar></w:r>` +
	`<w:r><w:instrText xml:space="preserve"> FORMCHECKBOX </w:instrText></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r><w:r><w:t xml:space="preserve"> Agree</w:t></w:r></w:p>`

func TestConvertContentControlsAsFields(t *testing.T) {
	t.Parallel()
	d := convertBytes(t, docx(t, controlsBody, nil), office.Options{FormFields: true})
	type got struct {
		kind    typeset.FieldKind
		name    string
		value   string
		checked bool
	}
	var have []got
	for _, f := range d.Pages[0].Fields {
		have = append(have, got{f.Kind, f.Name, f.Value, f.Checked})
		if f.Parent == nil || f.Parent.Role != "Form" || !numeric.Positive(f.Rect.Width) {
			t.Errorf("field %q: parent %v, rectangle %v", f.Name, f.Parent, f.Rect)
		}
	}
	want := []got{
		{typeset.FieldText, "customer", "ACME", false},
		{typeset.FieldCombo, "", "Green", false},
		{typeset.FieldCombo, "", "Rome", false},
		{typeset.FieldCheckBox, "", "", true},
		{typeset.FieldText, "Text1", "Jane", false},
		{typeset.FieldCombo, "Fruit", "Banana", false},
		{typeset.FieldCheckBox, "Check1", "", true},
	}
	if !slices.Equal(have, want) {
		t.Fatalf("fields = %+v\nwant %+v", have, want)
	}
	fields := d.Pages[0].Fields
	if fields[0].Description != "Customer" || !fields[2].Editable || !fields[2].ReadOnly || fields[4].MaxLen != 10 || !fields[6].ReadOnly {
		t.Errorf("a field lost a property: %+v", fields)
	}
	text := pageText(d.Pages[0])
	for _, s := range []string{"ACME", "Green", "Jane"} {
		if strings.Contains(text, s) {
			t.Errorf("the page still draws %q: %q", s, text)
		}
	}
	if !strings.Contains(text, "picture") {
		t.Errorf("the picture control's text left the page: %q", text)
	}
}

// TestConvertContentControlsOff keeps the controls' text on the page and
// writes no field without the option.
func TestConvertContentControlsOff(t *testing.T) {
	t.Parallel()
	d := convertBytes(t, docx(t, controlsBody, nil), office.Options{})
	if n := len(d.Pages[0].Fields); n != 0 {
		t.Errorf("%d fields without the option", n)
	}
	if text := pageText(d.Pages[0]); !strings.Contains(text, "ACME") || !strings.Contains(text, "Jane") {
		t.Errorf("page text = %q", text)
	}
	// The legacy check box prints as Word prints it: its square stroked
	// 0.75 point wide, its diagonals 0.5.
	var square, cross *typeset.Path
	var walk func([]typeset.Fragment)
	walk = func(frags []typeset.Fragment) {
		for _, f := range frags {
			switch v := f.(type) {
			case *typeset.Path:
				switch {
				case len(v.Ops) == 5 && v.Ops[4].Kind == typeset.ClosePath:
					square = v
				case len(v.Ops) == 4 && v.Ops[2].Kind == typeset.MoveTo:
					cross = v
				}
			case *typeset.Group:
				walk(v.Content)
			}
		}
	}
	walk(d.Pages[0].Content)
	if square == nil || square.Stroke == nil || square.Stroke.Width != 0.75 || cross == nil || cross.Stroke == nil || cross.Stroke.Width != 0.5 {
		t.Errorf("the checked legacy check box draws %+v and %+v", square, cross)
	}
}

// TestConvertLegacyCheckBoxPrintField gives the Form element of a legacy
// check box printed without its field a cb role, its state and its name:
// the box's strokes carry no character, and a Form element holding content
// needs a role (ISO 14289-1, 7.18.4).
func TestConvertLegacyCheckBoxPrintField(t *testing.T) {
	t.Parallel()
	d := convertBytes(t, docx(t, controlsBody, nil), office.Options{})
	var forms []*typeset.StructElem
	stack := []*typeset.StructElem{d.Structure}
	for len(stack) > 0 {
		e := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if e.Role == "Form" {
			forms = append(forms, e)
		}
		stack = append(stack, e.Kids...)
	}
	want := typeset.PrintField{Role: typeset.PrintCheckBox, Checked: typeset.CheckOn, Desc: "Check1"}
	if len(forms) != 1 || forms[0].PrintField != want {
		t.Fatalf("Form elements = %+v, want one with %+v", forms, want)
	}
}

// TestConvertContentControlsPrintField gives every Form element of a
// control made a field the PrintField attributes of that field, which the
// backend writes where the field does not land.
func TestConvertContentControlsPrintField(t *testing.T) {
	t.Parallel()
	d := convertBytes(t, docx(t, controlsBody, nil), office.Options{FormFields: true})
	for _, f := range d.Pages[0].Fields {
		if f.Parent.PrintField.Role == typeset.PrintRoleNone {
			t.Errorf("field %q: its Form element has no PrintField role", f.Name)
		}
	}
	if p := d.Pages[0].Fields[0].Parent.PrintField; p.Role != typeset.PrintText || p.Desc != "Customer" {
		t.Errorf("text control's PrintField = %+v", p)
	}
}
