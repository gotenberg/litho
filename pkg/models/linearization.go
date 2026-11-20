package models

// LinearizationParams holds the parameters extracted from a Linearization
// Dictionary found in the first 1024 bytes of a linearized PDF.
// Concurrent reads of a value are safe; a write needs synchronization.
type LinearizationParams struct {
	// FileLength is the /L value: the length of the file in bytes.
	FileLength int64 `json:"fileLength"`
	// HintStreamOffset is the byte offset of the primary hint stream, the
	// first value of /H.
	HintStreamOffset int64 `json:"hintStreamOffset"`
	// HintStreamLength is the length of the primary hint stream in bytes, the
	// second value of /H.
	HintStreamLength int64 `json:"hintStreamLength"`
	// FirstPageObjNum is the /O value: the object number of the first page.
	FirstPageObjNum int `json:"firstPageObjNum"`
	// NumPages is the /N value: the number of pages in the document.
	NumPages int `json:"numPages"`
	// FirstPageEndOffset is the /E value: the byte offset of the end of the
	// first page.
	FirstPageEndOffset int64 `json:"firstPageEndOffset"`
	// FirstPageXRefOffset is the byte offset of the first-page cross-reference
	// section.
	FirstPageXRefOffset int64 `json:"firstPageXRefOffset"`
	// Hints is the decoded Page Offset Hint Table, one entry per page,
	// or nil when the hint stream could not be parsed.
	Hints []PageOffsetEntry `json:"hints,omitempty"`
}

// PageOffsetEntry holds the per-page data decoded from the hint stream's
// Page Offset Hint Table.
// Concurrent reads of a value are safe; a write needs synchronization.
type PageOffsetEntry struct {
	// ObjCount is the number of objects in the page's group.
	ObjCount int `json:"objCount"`
	// PageLength is the length in bytes of the page's objects.
	PageLength int64 `json:"pageLength"`
	// ContentStreamOffset is the byte offset of the page's content stream,
	// relative to the start of the page's group.
	ContentStreamOffset int64 `json:"contentStreamOffset"`
}
