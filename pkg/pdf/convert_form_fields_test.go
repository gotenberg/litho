package pdf

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// formODT is a flat ODT with a text field and a check box.
func formODT() string {
	ctl := func(id string) string {
		return `<draw:control text:anchor-type="as-char" svg:width="4cm" svg:height="0.6cm" draw:control="` + id + `"/>`
	}
	return wordODT("", `<office:forms xmlns:form="urn:oasis:names:tc:opendocument:xmlns:form:1.0"><form:form form:name="F">`+
		`<form:text form:name="name" form:id="c1" form:current-value="Ada"/>`+
		`<form:checkbox form:name="agree" form:id="c2" form:current-state="checked"/>`+
		`</form:form></office:forms><text:p>Name: `+ctl("c1")+` Agree: `+ctl("c2")+`</text:p>`)
}

// TestConvert_FormFields checks that WithFormFields writes the controls of
// a DOCX and an ODT source as form fields a reader reads back, and that
// without it a conversion holds no form.
func TestConvert_FormFields(t *testing.T) {
	t.Parallel()
	fonts := WithConvertFontDirs(convertFontDir(t))
	docx := wordDOCX(t, `<w:p><w:r><w:t xml:space="preserve">Name: </w:t></w:r><w:sdt><w:sdtPr><w:tag w:val="name"/><w:text/></w:sdtPr>`+
		`<w:sdtContent><w:r><w:t>Ada</w:t></w:r></w:sdtContent></w:sdt></w:p>`)
	for _, tc := range []struct {
		name string
		c    *Conversion
		want []models.FormField
	}{
		{"DOCX", FromDOCX(bytes.NewReader(docx), fonts, WithFormFields(true)), []models.FormField{{Name: "name", Value: "Ada", Type: models.FormFieldText}}},
		{"ODT", FromODT(strings.NewReader(formODT()), fonts, WithFormFields(true)), []models.FormField{
			{Name: "name", Value: "Ada", Type: models.FormFieldText},
			{Name: "agree", Value: "Yes", Type: models.FormFieldCheckbox},
		}},
		{"DOCX without the option", FromDOCX(bytes.NewReader(docx), fonts), nil},
		{"ODT without the option", FromODT(strings.NewReader(formODT()), fonts, WithFormFields(false)), nil},
	} {
		ctx := context.Background()
		var out bytes.Buffer
		if _, err := tc.c.WriteTo(ctx, &out); err != nil {
			t.Fatalf("%s: WriteTo: %v", tc.name, err)
		}
		r, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
		if err != nil {
			t.Fatalf("%s: Open: %v", tc.name, err)
		}
		got, err := r.FormFields(ctx)
		r.Close()
		if err != nil {
			t.Fatalf("%s: FormFields: %v", tc.name, err)
		}
		if len(got) != len(tc.want) {
			t.Fatalf("%s: %d fields, want %d: %+v", tc.name, len(got), len(tc.want), got)
		}
		for i, w := range tc.want {
			if g := got[i]; g.Name != w.Name || g.Value != w.Value || g.Type != w.Type {
				t.Errorf("%s: field %d = %+v, want %+v", tc.name, i, g, w)
			}
		}
	}
}

// TestConvert_FormFieldsRemediate checks that a conversion with form fields
// remediates to PDF/A-2b and PDF/UA-1 with nothing left.
func TestConvert_FormFieldsRemediate(t *testing.T) {
	t.Parallel()
	c := FromODT(strings.NewReader(formODT()), WithConvertFontDirs(convertFontDir(t)), WithFormFields(true), WithDefaultLanguage("en"))
	out, report, err := convertAndRemediate(context.Background(), c, "1.7", models.PDFA2b, models.PDFUA1, "en")
	if err != nil {
		t.Fatalf("remediating: %v", err)
	}
	for _, v := range report.ResidualViolations() {
		t.Errorf("residual: %s %s: %s", v.Specification, v.Clause, v.Description)
	}
	r, err := Open(context.Background(), bytes.NewReader(out), int64(len(out)))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if fields, err := r.FormFields(context.Background()); err != nil || len(fields) != 2 {
		t.Errorf("remediated output: %d fields, %v", len(fields), err)
	}
}
