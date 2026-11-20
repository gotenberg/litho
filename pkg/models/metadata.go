package models

// Metadata holds document information dictionary fields for
// [github.com/gotenberg/litho/pkg/pdf.Writer.SetMetadata]. Concurrent reads of
// a value are safe; a write needs synchronization.
//
// Each field is the entry of the same name in the Info dictionary (ISO
// 32000-1, Table 317). An empty field leaves the existing entry alone. A
// value is written as given, with no check of its form: CreationDate and
// ModDate belong in the PDF date form "D:YYYYMMDDHHmmSSOHH'mm" (ISO 32000-1,
// 7.9.4).
type Metadata struct {
	Title        string `json:"title"`
	Author       string `json:"author"`
	Subject      string `json:"subject"`
	Keywords     string `json:"keywords"`
	Creator      string `json:"creator"`
	Producer     string `json:"producer"`
	CreationDate string `json:"creationDate"`
	ModDate      string `json:"modDate"`
}
