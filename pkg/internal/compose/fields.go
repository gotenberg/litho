package compose

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// FieldKind is the kind of an interactive form [Field]: the field type and
// flags it is written with (ISO 32000-1, 12.7.4).
type FieldKind uint8

// The kinds of field.
const (
	// FieldText is a text field (12.7.4.3, /FT /Tx), a single line unless
	// [Field.Multiline].
	FieldText FieldKind = iota
	// FieldCheckBox is a check box (12.7.4.2.3, /FT /Btn), on or off.
	FieldCheckBox
	// FieldRadio is one button of a radio button group (12.7.4.2.4, /FT
	// /Btn with the Radio flag). The radio buttons of one [Field.Name]
	// form one group, one field whose widgets they are, wherever they
	// sit in the document.
	FieldRadio
	// FieldList is a scrollable list box (12.7.4.4, /FT /Ch).
	FieldList
	// FieldCombo is a combo box (12.7.4.4, /FT /Ch with the Combo flag): a
	// drop-down list, whose text the reader may edit when
	// [Field.Editable].
	FieldCombo
)

// Field is one interactive form field on a page, written as a field and its
// widget annotation (ISO 32000-1, 12.7 and 12.5.6.19). Its widget draws
// Appearance, so a reader that does not fill forms prints what a reader
// that does shows; the converter takes the content a field shows off the
// page and hands it here, so the page and the field together paint what
// the page alone painted.
type Field struct {
	Kind FieldKind
	// Rect is the widget's rectangle in page coordinates, with a positive
	// width and height.
	Rect Rect
	// Name is the field's partial name (12.7.3.2). A period separates the
	// levels of a qualified name, so one in Name is written as an
	// underscore. Two fields of one name share one value in a reader, so
	// the backend writes a field whose name an earlier field of another
	// group took with a suffix, "_2", "_3" and so on; the radio buttons
	// of one name stay one group. Empty is written as "Field". It holds
	// at most [MaxFieldText] bytes.
	Name string
	// Description is the field's alternate name, /TU (12.7.3.1, Table
	// 220), what a reader shows as its tooltip and a screen reader speaks
	// (ISO 14289-1, 7.18.1). Empty, the name stands in.
	Description string
	// Value is the text of a text field, the selected option of a list box
	// that selects one, or the text of a combo box, which is one of
	// Options unless the box is Editable. A check box and a radio button
	// read Checked instead.
	Value string
	// Options are the items of a list box or a combo box, in order, as
	// they show (12.7.4.4, /Opt). Other kinds ignore them.
	Options []string
	// Selected are the indices into Options of the items a list box that
	// allows several selects, ascending; such a box ignores Value.
	Selected []int
	// Checked is a check box or a radio button that is on.
	Checked bool
	// OnState is the name of the on state of a check box or a radio button
	// (12.7.4.2.3), "Yes" when empty. A radio button group's value is the
	// on state of its button that is on, so its buttons name different
	// states.
	OnState string
	// ReadOnly, Required, Multiline, MultiSelect and Editable are the
	// field flags of Tables 221, 228 and 230: a value the reader may not
	// change, one a submission needs, a text field of several lines, a
	// list box that selects several items, a combo box whose text the
	// reader may edit.
	ReadOnly, Required, Multiline, MultiSelect, Editable bool
	// MaxLen is the most characters a text field takes (Table 229), 0 for
	// no limit.
	MaxLen int
	// FontSize and Color are the size and colour of the text a reader sets
	// when the value changes, the field's default appearance (12.7.3.3);
	// a size of 0 fits the text to the widget. The zero Color is black.
	FontSize float64
	Color    Color
	// Appearance is what the widget draws, in page coordinates: the value
	// of a text or choice field, the on state of a check box or a radio
	// button. Nothing outside Rect shows.
	Appearance []Fragment
	// OffAppearance is what a check box or a radio button draws when off,
	// in page coordinates. Other kinds ignore it.
	OffAppearance []Fragment
	// Parent is the Form structure element (ISO 32000-1, 14.8.4.5) the
	// widget belongs to, nil for an untagged document. Its Alt, when set,
	// describes the field when Description is empty.
	Parent *StructElem
}

// MaxFieldText bounds the bytes of a field's [Field.Name],
// [Field.Description], [Field.Value] and each of its [Field.Options].
// Anti-DoS guard, not spec-mandated: ISO 32000-1, Annex C.2 bounds a
// string at 32767 bytes, which a value written as UTF-16 takes at 16 KiB
// of UTF-8, and no form control holds more text than a page shows.
const MaxFieldText = 16 << 10

// maxFieldOptions bounds the [Field.Options] of one field. Anti-DoS guard,
// not spec-mandated: Annex C.2 bounds an array at 8191 elements, and a
// list a reader scrolls holds tens of items.
const maxFieldOptions = 8191

// maxFieldsPerPage bounds the fields of one page. Anti-DoS guard, not
// spec-mandated: each field writes a widget, one or two appearance
// streams and an entry in the page's /Annots, which Annex C.2 bounds at
// 8191 elements with the links.
const maxFieldsPerPage = 4096

// maxFields bounds the fields of a document, which /AcroForm /Fields lists
// at the end. Anti-DoS guard, not spec-mandated: the list is held until
// the file is written, a reference a field, and a long form holds a few
// thousand fields.
const maxFields = 100_000

// onState returns the name of the on state of f.
func (f *Field) onState() string {
	if f.OnState == "" {
		return "Yes"
	}
	return f.OnState
}

// fieldName returns the partial name a field of name n is written with,
// before it is made unique: a period, which separates the levels of a
// fully qualified name (ISO 32000-1, 12.7.3.2), becomes an underscore, and
// an empty name "Field".
func fieldName(n string) string {
	n = strings.ReplaceAll(n, ".", "_")
	if n == "" {
		return "Field"
	}
	return n
}

// field checks f.
func (v *validator) field(f *Field) error {
	if f.Kind > FieldCombo {
		return fmt.Errorf("%w: kind %d", ErrInvalidField, f.Kind)
	}
	if !finiteRect(f.Rect) || !(f.Rect.Width > 0) || !(f.Rect.Height > 0) || f.Rect.Width > maxPageSize || f.Rect.Height > maxPageSize {
		return fmt.Errorf("%w: rectangle %v", ErrInvalidField, f.Rect)
	}
	texts := append([]string{f.Name, f.Description, f.Value, f.OnState}, f.Options...)
	if len(f.Options) > maxFieldOptions {
		return fmt.Errorf("%w: %d options, at most %d", ErrLimitExceeded, len(f.Options), maxFieldOptions)
	}
	for _, s := range texts {
		if len(s) > MaxFieldText {
			return fmt.Errorf("%w: a text of %d bytes, at most %d", ErrLimitExceeded, len(s), MaxFieldText)
		}
		if !utf8.ValidString(s) {
			return fmt.Errorf("%w: text %q is not UTF-8", ErrInvalidField, s)
		}
	}
	// A state is a name (ISO 32000-1, 12.7.4.2.3), which holds no NUL
	// (7.3.5); Off names the off state.
	if strings.ContainsRune(f.OnState, 0) || f.OnState == "Off" {
		return fmt.Errorf("%w: on state %q", ErrInvalidField, f.OnState)
	}
	prev := -1
	for _, i := range f.Selected {
		if i <= prev || i >= len(f.Options) {
			return fmt.Errorf("%w: selection %v of %d options", ErrInvalidField, f.Selected, len(f.Options))
		}
		prev = i
	}
	if f.MaxLen < 0 {
		return fmt.Errorf("%w: maximum length %d", ErrInvalidField, f.MaxLen)
	}
	if !finite(f.FontSize) || f.FontSize < 0 {
		return fmt.Errorf("%w: font size %v", ErrInvalidField, f.FontSize)
	}
	if f.Color != (Color{}) {
		if err := checkColor(f.Color); err != nil {
			return err
		}
	}
	for _, app := range [][]Fragment{f.Appearance, f.OffAppearance} {
		if err := v.fragments(app, 0); err != nil {
			return fmt.Errorf("appearance: %w", err)
		}
	}
	return v.parent(f.Parent)
}
