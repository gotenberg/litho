// Package formfield turns the form controls of a laid-out word-processing
// document into the interactive form fields of its pages (ISO 32000-1,
// 12.7). The DOCX and ODT converters share it: a converter gives each
// control's layout node the Role Form and an ID from [NodeID], lays the
// document out, and hands the controls to [Attach], which moves what each
// control shows from its page into the field that draws it.
package formfield
