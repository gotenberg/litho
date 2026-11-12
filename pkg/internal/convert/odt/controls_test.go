package odt

import (
	"context"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/odf"
)

// controlsDoc holds a control of every kind the converter writes as a
// field, a button and a control that does not print.
func controlsDoc(t *testing.T) []byte {
	t.Helper()
	ctl := func(id, w, h string) string {
		return `<draw:control text:anchor-type="as-char" svg:width="` + w + `" svg:height="` + h + `" draw:control="` + id + `"/>`
	}
	forms := `<office:forms xmlns:form="urn:oasis:names:tc:opendocument:xmlns:form:1.0"><form:form form:name="F">` +
		`<form:text form:name="name" form:id="c1" form:current-value="Ada" form:title="Your name" form:max-length="20"/>` +
		`<form:textarea form:name="notes" form:id="c2" form:current-value="One&#10;Two"/>` +
		`<form:checkbox form:name="agree" form:id="c3" form:current-state="checked"/>` +
		`<form:radio form:name="color" form:id="c4" form:value="red"/>` +
		`<form:radio form:name="color" form:id="c5" form:value="blue" form:current-selected="true"/>` +
		`<form:listbox form:name="fruit" form:id="c6" form:size="3"><form:option form:label="Apple"/><form:option form:label="Banana" form:current-selected="true"/></form:listbox>` +
		`<form:listbox form:name="country" form:id="c7" form:dropdown="true"><form:option form:label="France" form:selected="true"/></form:listbox>` +
		`<form:combobox form:name="city" form:id="c8" form:current-value="Rome"><form:item form:label="Paris"/></form:combobox>` +
		`<form:date form:name="day" form:id="c9" form:current-value="2024-03-15" form:readonly="true"/>` +
		`<form:button form:name="go" form:id="c10" form:label="Go"/>` +
		`<form:text form:name="hidden" form:id="c11" form:printable="false"/>` +
		`<form:text form:name="empty" form:id="c12"/>` +
		`</form:form></office:forms>`
	body := forms + `<text:p>` + ctl("c1", "4cm", "0.6cm") + ctl("c2", "4cm", "1.5cm") + `</text:p>` +
		`<text:p>` + ctl("c3", "0.4cm", "0.4cm") + ctl("c4", "0.4cm", "0.4cm") + ctl("c5", "0.4cm", "0.4cm") + `</text:p>` +
		`<text:p>` + ctl("c6", "4cm", "1.6cm") + ctl("c7", "4cm", "0.6cm") + ctl("c8", "4cm", "0.6cm") + `</text:p>` +
		`<text:p>` + ctl("c9", "4cm", "0.6cm") + ctl("c10", "3cm", "0.8cm") + ctl("c11", "3cm", "0.6cm") + ctl("c12", "3cm", "0.6cm") + `</text:p>`
	return doc{body: body}.pack(t)
}

func TestConvertFormControlsAsFields(t *testing.T) {
	t.Parallel()
	d := convertDoc(t, controlsDoc(t), office.Options{FormFields: true})
	fields := d.Pages[0].Fields
	type got struct {
		kind    compose.FieldKind
		name    string
		value   string
		checked bool
	}
	var have []got
	for _, f := range fields {
		have = append(have, got{f.Kind, f.Name, f.Value, f.Checked})
		if f.Parent == nil || f.Parent.Role != "Form" {
			t.Errorf("field %q hangs from %v, want a Form element", f.Name, f.Parent)
		}
		if !(f.Rect.Width > 0) || !(f.Rect.Height > 0) {
			t.Errorf("field %q has rectangle %v", f.Name, f.Rect)
		}
	}
	want := []got{
		{compose.FieldText, "name", "Ada", false},
		{compose.FieldText, "notes", "One\nTwo", false},
		{compose.FieldCheckBox, "agree", "", true},
		{compose.FieldRadio, "color", "", false},
		{compose.FieldRadio, "color", "", true},
		{compose.FieldList, "fruit", "Banana", false},
		{compose.FieldCombo, "country", "France", false},
		{compose.FieldCombo, "city", "Rome", false},
		{compose.FieldText, "day", "03/15/24", false},
		{compose.FieldText, "empty", "", false},
	}
	if !slices.Equal(have, want) {
		t.Fatalf("fields = %+v\nwant %+v", have, want)
	}
	if f := fields[0]; f.Description != "Your name" || f.MaxLen != 20 || len(f.Appearance) == 0 {
		t.Errorf("name field = %+v", f)
	}
	if !fields[1].Multiline || !fields[8].ReadOnly || !fields[7].Editable {
		t.Error("the text area, the read-only date or the combo box lost a flag")
	}
	if fields[4].OnState != "blue" || len(fields[3].Appearance) == 0 || len(fields[3].OffAppearance) == 0 {
		t.Errorf("radio buttons = %+v, %+v", fields[3], fields[4])
	}
	// The values moved into the fields: the page no longer draws them.
	for _, r := range runs(d.Pages[0]) {
		if r.text == "Ada" || r.text == "Rome" {
			t.Errorf("the page still draws %q", r.text)
		}
	}
}

// TestConvertFormControlsOff prints what each control shows without the
// option, as LibreOffice prints it without its form fields, and writes no
// field; with the option, the page and its fields draw the same runs in
// the same places.
func TestConvertFormControlsOff(t *testing.T) {
	t.Parallel()
	off := convertDoc(t, controlsDoc(t), office.Options{})
	if n := len(off.Pages[0].Fields); n != 0 {
		t.Errorf("%d fields without the option", n)
	}
	printed := map[string]run{}
	for _, r := range runs(off.Pages[0]) {
		printed[r.text] = r
	}
	for _, s := range []string{"Ada", "OneTwo", "Banana", "France", "Rome", "03/15/24", "Go"} {
		if _, ok := printed[s]; !ok {
			t.Errorf("%q is not printed: %v", s, printed)
		}
	}
	if r := printed["France"]; r.size != dropFontSize {
		t.Errorf("the drop-down list box prints at %v points", r.size)
	}
	on := convertDoc(t, controlsDoc(t), office.Options{FormFields: true})
	pg := &compose.Page{Content: on.Pages[0].Content}
	for _, f := range on.Pages[0].Fields {
		pg.Content = append(pg.Content, f.Appearance...)
	}
	for _, r := range runs(pg) {
		if p, ok := printed[r.text]; !ok || !near(p.x, r.x, 0.01) || !near(p.y, r.y, 0.01) || p.size != r.size {
			t.Errorf("with the option, %q at %+v, without it at %+v", r.text, r, p)
		}
	}
}

// FuzzControls builds the layout tree of a flat document whose form
// controls become fields: every control the builder makes a field of has
// a node that holds content and an ID.
func FuzzControls(f *testing.F) {
	flat := []byte(`<office:document ` + nsDecl + ` xmlns:form="urn:oasis:names:tc:opendocument:xmlns:form:1.0"><office:body><office:text>` +
		`<office:forms><form:form><form:text form:name="a" form:id="c1" form:current-value="v" form:max-length="3"/>` +
		`<form:radio form:name="r" form:id="c2" form:current-selected="true"/>` +
		`<form:listbox form:name="l" form:id="c3" form:multiple="true"><form:option form:label="x" form:selected="true"/></form:listbox>` +
		`<form:date form:name="d" form:id="c4" form:current-value="20240315"/></form:form></office:forms>` +
		`<text:p><draw:control svg:width="2cm" svg:height="0.5cm" draw:control="c1"/><draw:control draw:control="c2"/>` +
		`<draw:control svg:width="2cm" svg:height="1cm" draw:control="c3"/><draw:control draw:control="c4"/></text:p>` +
		`</office:text></office:body></office:document>`)
	f.Add(flat)
	f.Add(flat[:len(flat)/2])
	f.Fuzz(func(t *testing.T, data []byte) {
		pkg, err := odf.Open(context.Background(), data, odf.Options{})
		if err != nil {
			return
		}
		body := odf.Section(pkg.Content, "body").Child(odf.NSOffice, "text")
		if body == nil {
			return
		}
		b := newFuzzBuilder(pkg)
		b.formFields = true
		b.document(body)
		for _, c := range b.controls {
			if c.ID == "" || c.Field.MaxLen < 0 || c.Field.MaxLen > compose.MaxFieldText {
				t.Fatalf("control %+v", c)
			}
		}
	})
}
