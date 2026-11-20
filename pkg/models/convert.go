package models

// ConvertReport is what a [github.com/gotenberg/litho/pkg/pdf.Conversion]
// reports beside the PDF. Concurrent reads of a value are safe; a write needs
// synchronization.
type ConvertReport struct {
	// Pages is the number of pages written.
	Pages int `json:"pages"`
	// Warnings lists what the conversion could not render as the source
	// meant it, in the order met.
	Warnings []ConvertWarning `json:"warnings"`
}

// ConvertWarning is one construct of the source a
// [github.com/gotenberg/litho/pkg/pdf.Conversion] did not render faithfully.
// Concurrent reads of a value are safe; a write needs synchronization.
type ConvertWarning struct {
	// Code names the kind of construct: one of the Warning constants (see
	// [ConvertWarningCode]).
	Code ConvertWarningCode `json:"code"`
	// Effect says what the construct cost the PDF: content left out or
	// content printed differently (see [ConvertWarningEffect]). It is
	// never "".
	Effect ConvertWarningEffect `json:"effect"`
	// Message explains the construct and what was done instead.
	Message string `json:"message"`
	// Page is the 1-based page the construct lands on, 0 when it has none.
	Page int `json:"page"`
}
