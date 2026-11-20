package models

// FormField represents an interactive AcroForm field.
//
// The fields after Value describe the field rather than its value. A flag or a
// list a field does not state is its zero value, and so is every one of them
// for a field only XFA data names. The flags a field inherits from its
// ancestors (ISO 32000-1, 12.7.3.1, Table 220) count as its own.
// Concurrent reads of a value are safe; a write needs synchronization.
type FormField struct {
	// Name is the fully qualified field name, the partial names of the field
	// and its ancestors joined by periods (ISO 32000-1, 12.7.3.2).
	Name string `json:"name"`
	// Value is the field's value as text. A multi-select list box joins its
	// selections into one string, which Values lists one by one. A check box
	// or a radio button group that states no value reports "Off", the state
	// it shows (ISO 32000-1, 12.7.4.2.3).
	Value string `json:"value"`
	// Type is the kind of field, "" when the field states none.
	Type FormFieldType `json:"type,omitempty"`
	// Values lists the selections of a list box that allows several (see
	// MultiSelect), in the order its value gives them.
	Values []string `json:"values,omitempty"`
	// Options lists the items of a combo box or a list box, in the order
	// its /Opt array gives them (ISO 32000-1, 12.7.4.4, Table 231). For a
	// check box or a radio button group with an /Opt array, it lists each
	// state in States that /Opt gives an export value, with the export value
	// as Label when the two differ (12.7.4.2.4).
	Options []FormFieldOption `json:"options,omitempty"`
	// States lists the names of the states a check box or a radio button
	// group can be set to, other than Off: the appearance states of its
	// widgets, in widget order (ISO 32000-1, 12.7.4.2.3). A group with an
	// /Opt array may name them by position, "0", "1" and so on, and Options
	// then maps each to its export value (12.7.4.2.4).
	States []string `json:"states,omitempty"`
	// ReadOnly reports the ReadOnly flag: the user may not change the value
	// (ISO 32000-1, Table 221).
	ReadOnly bool `json:"readOnly,omitempty"`
	// Required reports the Required flag: the field must have a value when
	// the form is submitted (ISO 32000-1, Table 221).
	Required bool `json:"required,omitempty"`
	// MultiSelect reports a choice field that allows more than one selection
	// (ISO 32000-1, Table 230).
	MultiSelect bool `json:"multiSelect,omitempty"`
	// Editable reports a combo box whose text the user may edit, so it takes
	// a value none of its options holds (ISO 32000-1, Table 230).
	Editable bool `json:"editable,omitempty"`
	// MaxLen is the maximum length of a text field's value in characters, 0
	// when it states none (ISO 32000-1, Table 229).
	MaxLen int `json:"maxLen,omitempty"`
	// Widgets lists the widget annotations that show the field on a page, in
	// the order the field tree gives them. It is empty for a field no page
	// shows.
	Widgets []FormWidget `json:"widgets,omitempty"`
}

// FormFieldType names the kind of an interactive form field (ISO 32000-1,
// 12.7.4), from its /FT and its field flags.
type FormFieldType string

const (
	// FormFieldText is a text field (/FT /Tx).
	FormFieldText FormFieldType = "text"
	// FormFieldCheckbox is a check box (/FT /Btn, neither Radio nor
	// Pushbutton set).
	FormFieldCheckbox FormFieldType = "checkbox"
	// FormFieldRadio is a radio button group (/FT /Btn with Radio set).
	FormFieldRadio FormFieldType = "radio"
	// FormFieldButton is a push button (/FT /Btn with Pushbutton set), which
	// holds no value.
	FormFieldButton FormFieldType = "button"
	// FormFieldCombo is a combo box (/FT /Ch with Combo set).
	FormFieldCombo FormFieldType = "combo"
	// FormFieldList is a list box (/FT /Ch with Combo clear).
	FormFieldList FormFieldType = "list"
	// FormFieldSignature is a signature field (/FT /Sig).
	FormFieldSignature FormFieldType = "signature"
	// FormFieldXFA is a name only the document's XFA data gives, which no
	// AcroForm field holds, so setting it has nothing to write.
	FormFieldXFA FormFieldType = "xfa"
)

// FormFieldOption is one item of a choice field (ISO 32000-1, 12.7.4.4,
// Table 231).
// Concurrent reads of a value are safe; a write needs synchronization.
type FormFieldOption struct {
	// Value is the item's export value, the one the field's value holds when
	// the item is selected.
	Value string `json:"value"`
	// Label is the text the field shows for the item, "" when it shows Value.
	Label string `json:"label,omitempty"`
}

// FormWidget is a widget annotation that shows a form field on a page.
// Concurrent reads of a value are safe; a write needs synchronization.
type FormWidget struct {
	// PageNumber is the 1-based page the widget is on.
	PageNumber int `json:"pageNumber"`
	// Rect is the widget's rectangle in default user space, normalized so
	// the lower-left corner comes first.
	Rect [4]float64 `json:"rect"`
}
