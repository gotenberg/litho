package models

// Bookmark represents an item in the document outline.
// Concurrent reads of a value are safe; a write needs synchronization.
type Bookmark struct {
	// Title is the text of the outline item.
	Title string `json:"title"`
	// PageNumber is the 1-based page the item leads to, or 0 when it names no
	// page of the document.
	PageNumber int `json:"pageNumber"`
	// Children are the items nested under this one, in outline order.
	Children []Bookmark `json:"children,omitempty"`
}
