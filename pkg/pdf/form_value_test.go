package pdf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/color"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// valueFormPDF builds a one-page form with a field of every kind a value is
// checked against:
//
//   - agree: a check box whose widget shows Yes;
//   - size: a radio group whose kids show S, M and L, each a filled square
//     in its middle when on and a thick outline at its edge when off;
//   - go: a push button;
//   - sig: a signature field;
//   - country: a combo box of export and display pairs;
//   - city: an editable combo box;
//   - single: a list box that allows one selection;
//   - tags: a list box that allows several.
func valueFormPDF() []byte {
	const on = "0 g 5 5 10 10 re f"
	const off = "0 G 4 w 2 2 16 16 re S"
	radio := func(state string, x int) string {
		return fmt.Sprintf("<< /Type /Annot /Subtype /Widget /Parent 5 0 R /Rect [%d 600 %d 620] /P 3 0 R /AS /Off"+
			" /AP << /N << /%s 14 0 R /Off 15 0 R >> >> >>", x, x+20, state)
	}
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R 5 0 R 9 0 R 10 0 R 11 0 R 12 0 R 13 0 R 16 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R 6 0 R 7 0 R 8 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Btn /T (agree) /Rect [72 700 92 720] /P 3 0 R /AS /Off" +
			" /AP << /N << /Yes 14 0 R /Off 15 0 R >> >> >>",
		"<< /FT /Btn /Ff 49152 /T (size) /Kids [6 0 R 7 0 R 8 0 R] >>",
		radio("S", 72),
		radio("M", 112),
		radio("L", 152),
		"<< /FT /Btn /Ff 65536 /T (go) >>",
		"<< /FT /Sig /T (sig) >>",
		"<< /FT /Ch /Ff 131072 /T (country) /Opt [[(FR) (France)] [(DE) (Germany)]] /V (FR) >>",
		"<< /FT /Ch /Ff 393216 /T (city) /Opt [(Paris) (Berlin)] >>",
		"<< /FT /Ch /T (single) /Opt [(x) (y)] >>",
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 20 20] /Length %d >>\nstream\n%s\nendstream", len(on), on),
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 20 20] /Length %d >>\nstream\n%s\nendstream", len(off), off),
		"<< /FT /Ch /Ff 2097152 /T (tags) /Opt [(a) (b) (c)] >>",
	})
}

// writeForm applies queue to a Writer over valueFormPDF and returns the output
// and the error of the write.
func writeForm(t *testing.T, queue func(w *Writer)) ([]byte, error) {
	t.Helper()
	w := NewWriter(openBytes(t, valueFormPDF()))
	defer w.Close()
	queue(w)
	var out bytes.Buffer
	err := w.WriteTo(context.Background(), &out)
	return out.Bytes(), err
}

// formFields reads the fields of data, by name.
func formFields(t *testing.T, data []byte) map[string]models.FormField {
	t.Helper()
	r := openBytes(t, data)
	defer r.Close()
	fields, err := r.FormFields(context.Background())
	if err != nil {
		t.Fatalf("FormFields: %v", err)
	}
	out := make(map[string]models.FormField, len(fields))
	for _, f := range fields {
		out[f.Name] = f
	}
	return out
}

// TestFormFieldValueRefusesWhatTheFieldCannotHold pins the values a field is
// refused, each before anything is written: a state no widget of a button
// shows, which drew nothing, any value for a push button or a signature
// field, a value outside the options of a choice field the user cannot type
// into, and several selections for a field that allows one.
func TestFormFieldValueRefusesWhatTheFieldCannotHold(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		method string
		queue  func(w *Writer)
	}{
		{"a state the check box does not show", "SetFormField", func(w *Writer) { w.SetFormField("agree", "true") }},
		{"a state no radio button shows", "SetFormField", func(w *Writer) { w.SetFormField("size", "XL") }},
		{"a push button", "SetFormField", func(w *Writer) { w.SetFormField("go", "pressed") }},
		{"a signature field", "SetFormField", func(w *Writer) { w.SetFormField("sig", "signed") }},
		{"an option the combo box lacks", "SetFormField", func(w *Writer) { w.SetFormField("country", "XX") }},
		{"a display text, not an export value", "SetFormField", func(w *Writer) { w.SetFormField("country", "France") }},
		{"an option the list box lacks", "SetFormFieldValues", func(w *Writer) { w.SetFormFieldValues("tags", []string{"a", "z"}) }},
		{"several selections on a single-selection list", "SetFormFieldValues", func(w *Writer) { w.SetFormFieldValues("single", []string{"x", "y"}) }},
		{"no selection", "SetFormFieldValues", func(w *Writer) { w.SetFormFieldValues("tags", nil) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := writeForm(t, tc.queue)
			if !errors.Is(err, ErrFormFieldValue) || !IsInvalidInput(err) {
				t.Fatalf("WriteTo = %v, want ErrFormFieldValue, classified as invalid input", err)
			}
			if editErr, ok := errors.AsType[*EditError](err); !ok || editErr.Method != tc.method {
				t.Errorf("WriteTo = %v, want the EditError of %s", err, tc.method)
			}
		})
	}
}

// TestFormFieldValueAcceptsWhatTheFieldHolds is the control: a state a widget
// shows, Off, an option, any text in an editable combo box, and one value on a
// list box that allows several.
func TestFormFieldValueAcceptsWhatTheFieldHolds(t *testing.T) {
	t.Parallel()

	out, err := writeForm(t, func(w *Writer) {
		w.SetFormField("agree", "Yes")
		w.SetFormField("country", "DE")
		w.SetFormField("city", "Anywhere")
		w.SetFormField("single", "y")
		w.SetFormField("tags", "b")
	})
	if err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	fields := formFields(t, out)
	for name, want := range map[string]string{"agree": "Yes", "country": "DE", "city": "Anywhere", "single": "y", "tags": "b"} {
		if got := fields[name].Value; got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	if got := fields["tags"].Values; !slices.Equal(got, []string{"b"}) {
		t.Errorf("tags selections = %q, want [b]", got)
	}

	if _, err := writeForm(t, func(w *Writer) { w.SetFormField("agree", "Off") }); err != nil {
		t.Errorf("setting a check box Off: %v", err)
	}
}

// TestFormFieldValuesRoundTrip pins the selections of a list box that allows
// several: they read back in the order given.
func TestFormFieldValuesRoundTrip(t *testing.T) {
	t.Parallel()

	out, err := writeForm(t, func(w *Writer) { w.SetFormFieldValues("tags", []string{"c", "a"}) })
	if err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	tags := formFields(t, out)["tags"]
	if !slices.Equal(tags.Values, []string{"c", "a"}) {
		t.Errorf("tags selections = %q, want [c a]", tags.Values)
	}
	if !bytes.Contains(out, []byte("/I [0 2]")) {
		t.Errorf("the output does not index the selections in /Opt with /I [0 2]")
	}
}

// TestRadioValueShowsOneButton pins the radio write: the kid whose appearance
// has the state shows it, and the others show Off. Every kid used to be set to
// the state, which none but one has, so the group drew no button at all, the
// selected one included.
func TestRadioValueShowsOneButton(t *testing.T) {
	t.Parallel()

	out, err := writeForm(t, func(w *Writer) { w.SetFormField("size", "M") })
	if err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if got := formFields(t, out)["size"].Value; got != "M" {
		t.Errorf("size = %q, want M", got)
	}

	r := openBytes(t, out)
	defer r.Close()
	img, err := r.RenderPage(context.Background(), 1, WithRenderDPI(72))
	if err != nil {
		t.Fatalf("RenderPage: %v", err)
	}
	// At 72 dpi a point is a pixel, and the page's y axis runs up: the point
	// (x, 610) is pixel (x, 182). The on appearance fills a widget's middle,
	// and the off one draws its edge, 2 points in from its left side.
	dark := func(x int) bool {
		g := color.GrayModel.Convert(img.At(x, 792-610)).(color.Gray)
		return g.Y < 128
	}
	for _, kid := range []struct {
		state string
		x     int
		on    bool
	}{{"S", 72, false}, {"M", 112, true}, {"L", 152, false}} {
		if middle := dark(kid.x + 10); middle != kid.on {
			t.Errorf("the %s button draws its on state: %v, want %v", kid.state, middle, kid.on)
		}
		if edge := dark(kid.x + 2); edge == kid.on {
			t.Errorf("the %s button draws its off state: %v, want %v", kid.state, edge, !kid.on)
		}
	}
}
