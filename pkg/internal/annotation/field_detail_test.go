package annotation

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// objectsPDF opens a document whose objects are objs, by object number, with
// object 1 its catalog.
func objectsPDF(t *testing.T, objs map[int]string) *document.Document {
	t.Helper()

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	size := slices.Max(slices.Collect(maps.Keys(objs))) + 1
	offs := make([]int, size)
	for n := 1; n < size; n++ {
		if o, ok := objs[n]; ok {
			offs[n] = buf.Len()
			fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", n, o)
		}
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f\r\n", size)
	for n := 1; n < size; n++ {
		if _, ok := objs[n]; ok {
			fmt.Fprintf(&buf, "%010d 00000 n\r\n", offs[n])
		} else {
			buf.WriteString("0000000000 65535 f\r\n")
		}
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", size, xref)

	data := buf.Bytes()
	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	return doc
}

// detailedFormPDF builds a two-page form with a field of every kind:
//
//   - total: a read-only text field with a widget on each page;
//   - size: a radio group with no value, whose kids show S, M and L in that
//     order;
//   - calc: a text field no widget shows;
//   - country: an editable combo box of export and display pairs;
//   - sig: a signature field that is its own widget;
//   - tags: a multi-select list box;
//   - grp.inner: a text field taking /FT, /Ff and /MaxLen from its parent,
//     whose widget no page lists and whose /P names page 1;
//   - choices.pick: a list box taking /Opt from its parent;
//   - go: a push button;
//   - agree: a check box taking the ReadOnly flag from its parent;
//   - xform.only: a name only the XFA data gives.
func detailedFormPDF(t *testing.T) *document.Document {
	t.Helper()

	const datasets = `<xfa:datasets xmlns:xfa="http://www.xfa.org/schema/xfa-data/1.0/"><xfa:data><xform><only>xfa value</only></xform></xfa:data></xfa:datasets>`
	radio := func(state string, x int) string {
		return fmt.Sprintf("<< /Type /Annot /Subtype /Widget /Parent 8 0 R /Rect [%d 620 %d 600] /P 4 0 R /AS /Off"+
			" /AP << /N << /%s 16 0 R /Off 17 0 R >> >> >>", x+20, x, state)
	}
	return objectsPDF(t, map[int]string{
		1: "<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [5 0 R 8 0 R 12 0 R 13 0 R 14 0 R 15 0 R 18 0 R 22 0 R 25 0 R 26 0 R]" +
			" /XFA 24 0 R >> >>",
		2:  "<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		3:  "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [6 0 R 15 0 R] >>",
		4:  "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [7 0 R 9 0 R 10 0 R 11 0 R 14 0 R 27 0 R] >>",
		5:  "<< /FT /Tx /T (total) /V (42) /Ff 1 /Kids [6 0 R 7 0 R] >>",
		6:  "<< /Type /Annot /Subtype /Widget /Parent 5 0 R /Rect [72 700 272 720] /P 3 0 R >>",
		7:  "<< /Type /Annot /Subtype /Widget /Parent 5 0 R /Rect [72 50 272 70] /P 4 0 R >>",
		8:  "<< /FT /Btn /Ff 49152 /T (size) /Kids [9 0 R 10 0 R 11 0 R] >>",
		9:  radio("S", 72),
		10: radio("M", 112),
		11: radio("L", 152),
		12: "<< /FT /Tx /T (calc) /V (7) >>",
		13: "<< /FT /Ch /Ff 393216 /T (country) /Opt [[(FR) (France)] [(DE) (Germany)] [(XX) (XX)]] /V (FR) >>",
		14: "<< /Type /Annot /Subtype /Widget /FT /Sig /T (sig) /Rect [300 50 500 100] /P 4 0 R >>",
		15: "<< /Type /Annot /Subtype /Widget /FT /Ch /Ff 2097152 /T (tags) /Opt [(a) (b) (c)] /V [(c) (a)]" +
			" /Rect [72 500 272 560] /P 3 0 R >>",
		16: "<< /Type /XObject /Subtype /Form /BBox [0 0 20 20] /Length 18 >>\nstream\n0 g 5 5 10 10 re f\nendstream",
		17: "<< /Type /XObject /Subtype /Form /BBox [0 0 20 20] /Length 15 >>\nstream\n0 0 20 20 re S\nendstream",
		18: "<< /FT /Tx /Ff 2 /MaxLen 5 /T (grp) /Kids [19 0 R] >>",
		19: "<< /Type /Annot /Subtype /Widget /Parent 18 0 R /T (inner) /Rect [10 10 50 30] /P 3 0 R >>",
		22: "<< /FT /Ch /Opt [(x) (y)] /T (choices) /Kids [23 0 R] >>",
		23: "<< /Parent 22 0 R /T (pick) /V (y) >>",
		24: fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(datasets), datasets),
		25: "<< /FT /Btn /Ff 65536 /T (go) >>",
		26: "<< /FT /Btn /Ff 1 /T (agree) /Kids [27 0 R] >>",
		27: "<< /Type /Annot /Subtype /Widget /Parent 26 0 R /Rect [72 300 92 320] /P 4 0 R" +
			" /AP << /N << /Yes 16 0 R /Off 17 0 R >> >> >>",
	})
}

// TestFormFieldsDescribeTheirFields pins the description each field carries:
// its type from /FT and the field flags, the flags, options and length it
// states or inherits, the states its widgets show in widget order, and the
// pages and rectangles of its widgets.
func TestFormFieldsDescribeTheirFields(t *testing.T) {
	t.Parallel()

	fields, err := ExtractFormFields(context.Background(), detailedFormPDF(t))
	if err != nil {
		t.Fatalf("ExtractFormFields: %v", err)
	}
	got := make(map[string]models.FormField, len(fields))
	var names []string
	for _, f := range fields {
		got[f.Name] = f
		names = append(names, f.Name)
	}
	wantNames := []string{"total", "size", "calc", "country", "sig", "tags", "grp.inner", "choices.pick", "go", "agree", "xform.only"}
	if !slices.Equal(names, wantNames) {
		t.Errorf("fields %v, want %v", names, wantNames)
	}

	for _, want := range []models.FormField{
		{Name: "total", Value: "42", Type: models.FormFieldText, ReadOnly: true, Widgets: []models.FormWidget{
			{PageNumber: 1, Rect: [4]float64{72, 700, 272, 720}},
			{PageNumber: 2, Rect: [4]float64{72, 50, 272, 70}},
		}},
		{Name: "size", Value: "Off", Type: models.FormFieldRadio, States: []string{"S", "M", "L"}, Widgets: []models.FormWidget{
			{PageNumber: 2, Rect: [4]float64{72, 600, 92, 620}},
			{PageNumber: 2, Rect: [4]float64{112, 600, 132, 620}},
			{PageNumber: 2, Rect: [4]float64{152, 600, 172, 620}},
		}},
		{Name: "calc", Value: "7", Type: models.FormFieldText},
		{Name: "country", Value: "FR", Type: models.FormFieldCombo, Editable: true, Options: []models.FormFieldOption{
			{Value: "FR", Label: "France"}, {Value: "DE", Label: "Germany"}, {Value: "XX"},
		}},
		{Name: "sig", Type: models.FormFieldSignature, Widgets: []models.FormWidget{{PageNumber: 2, Rect: [4]float64{300, 50, 500, 100}}}},
		{
			Name: "tags", Value: "c, a", Type: models.FormFieldList, MultiSelect: true, Values: []string{"c", "a"},
			Options: []models.FormFieldOption{{Value: "a"}, {Value: "b"}, {Value: "c"}},
			Widgets: []models.FormWidget{{PageNumber: 1, Rect: [4]float64{72, 500, 272, 560}}},
		},
		{Name: "grp.inner", Type: models.FormFieldText, Required: true, MaxLen: 5, Widgets: []models.FormWidget{
			{PageNumber: 1, Rect: [4]float64{10, 10, 50, 30}},
		}},
		{Name: "choices.pick", Value: "y", Type: models.FormFieldList, Options: []models.FormFieldOption{{Value: "x"}, {Value: "y"}}},
		{Name: "go", Type: models.FormFieldButton},
		{Name: "agree", Value: "Off", Type: models.FormFieldCheckbox, ReadOnly: true, States: []string{"Yes"}, Widgets: []models.FormWidget{
			{PageNumber: 2, Rect: [4]float64{72, 300, 92, 320}},
		}},
		{Name: "xform.only", Value: "xfa value", Type: models.FormFieldXFA},
	} {
		if !reflect.DeepEqual(got[want.Name], want) {
			t.Errorf("%s:\n got %+v\nwant %+v", want.Name, got[want.Name], want)
		}
	}
}

// TestButtonOptionsNameStatesByPosition pins the items of a button group whose
// /Opt holds its export values (ISO 32000-1, 12.7.4.2.4): the widgets' states
// name them by position, and an item maps the state to the export value.
func TestButtonOptionsNameStatesByPosition(t *testing.T) {
	t.Parallel()

	got := buttonOptions([]string{"Yes", "No", "Yes"}, []string{"0", "1"})
	want := []models.FormFieldOption{{Value: "0", Label: "Yes"}, {Value: "1", Label: "No"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buttonOptions = %+v, want %+v", got, want)
	}
	if got := buttonOptions([]string{"Yes"}, []string{"Yes"}); !reflect.DeepEqual(got, []models.FormFieldOption{{Value: "Yes"}}) {
		t.Errorf("buttonOptions by export value = %+v, want one item Yes", got)
	}
}

// TestFormDataKeepsFilledFieldsOnly is the control on the other answer of the
// walk: a check box that states no value reports "Off" when the fields are
// listed, and is left out of the values the user filled in.
func TestFormDataKeepsFilledFieldsOnly(t *testing.T) {
	t.Parallel()

	fields, err := ExtractFormData(context.Background(), detailedFormPDF(t))
	if err != nil {
		t.Fatalf("ExtractFormData: %v", err)
	}
	for _, f := range fields {
		if f.Name == "agree" || f.Name == "size" {
			t.Errorf("ExtractFormData reports %s, which states no value", f.Name)
		}
	}
}
