package ast

import (
	"fmt"
	"sort"
	"strings"
)

// PdfObject is the base interface for all PDF object types.
type PdfObject interface {
	IsPdfObject()
	String() string
}

// PdfNull represents the PDF null object.
type PdfNull struct{}

// IsPdfObject marks PdfNull as a PDF object.
func (PdfNull) IsPdfObject() {}

// String returns the string representation of PdfNull.
func (PdfNull) String() string {
	return "null"
}

// PdfBool represents a PDF boolean object.
type PdfBool bool

// IsPdfObject marks PdfBool as a PDF object.
func (PdfBool) IsPdfObject() {}

// String returns the string representation of PdfBool.
func (b PdfBool) String() string {
	if b {
		return "true"
	}
	return "false"
}

// PdfInt represents a PDF integer object.
type PdfInt int64

// IsPdfObject marks PdfInt as a PDF object.
func (PdfInt) IsPdfObject() {}

// String returns the string representation of PdfInt.
func (i PdfInt) String() string {
	return fmt.Sprintf("%d", i)
}

// PdfReal represents a PDF real number object.
type PdfReal float64

// IsPdfObject marks PdfReal as a PDF object.
func (PdfReal) IsPdfObject() {}

// String returns the string representation of PdfReal.
func (r PdfReal) String() string {
	return fmt.Sprintf("%g", r)
}

// PdfName represents a PDF name object, which starts with a slash in PDF files.
type PdfName string

// IsPdfObject marks PdfName as a PDF object.
func (PdfName) IsPdfObject() {}

// String returns the string representation of PdfName.
func (n PdfName) String() string {
	return fmt.Sprintf("/%s", string(n))
}

// PdfString represents a PDF string object (literal or hexadecimal).
type PdfString string

// IsPdfObject marks PdfString as a PDF object.
func (PdfString) IsPdfObject() {}

// String returns the string representation of PdfString.
func (s PdfString) String() string {
	return fmt.Sprintf("(%s)", string(s))
}

// PdfArray represents a PDF array object.
type PdfArray []PdfObject

// IsPdfObject marks PdfArray as a PDF object.
func (PdfArray) IsPdfObject() {}

// String returns the string representation of PdfArray.
func (a PdfArray) String() string {
	parts := make([]string, 0, len(a))
	for _, obj := range a {
		if obj == nil {
			parts = append(parts, "null")
		} else {
			parts = append(parts, obj.String())
		}
	}
	return fmt.Sprintf("[%s]", strings.Join(parts, " "))
}

// PdfDict represents a PDF dictionary object.
type PdfDict map[PdfName]PdfObject

// IsPdfObject marks PdfDict as a PDF object.
func (PdfDict) IsPdfObject() {}

// String returns the string representation of PdfDict with sorted keys
// for deterministic output.
func (d PdfDict) String() string {
	keys := make([]string, 0, len(d))
	for k := range d {
		keys = append(keys, string(k))
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(d))
	for _, k := range keys {
		v := d[PdfName(k)]
		var valStr string
		if v == nil {
			valStr = "null"
		} else {
			valStr = v.String()
		}
		parts = append(parts, fmt.Sprintf("/%s %s", k, valStr))
	}
	return fmt.Sprintf("<<%s>>", strings.Join(parts, " "))
}

// PdfIndirectRef represents a PDF indirect reference object.
type PdfIndirectRef struct {
	ObjectNum int
	GenNum    int
}

// IsPdfObject marks PdfIndirectRef as a PDF object.
func (PdfIndirectRef) IsPdfObject() {}

// String returns the string representation of PdfIndirectRef.
func (r PdfIndirectRef) String() string {
	return fmt.Sprintf("%d %d R", r.ObjectNum, r.GenNum)
}

// PdfStream represents a PDF stream object. Stream data is loaded lazily —
// the raw bytes are only read from the underlying PDF file when Bytes() is
// called for the first time and cached via sync.Once for subsequent calls.
//
// PdfStream is a value type, but it is safe to copy: the lazy loader closure
// captures its caching state (sync.Once, cached bytes, cached error) as
// pointer-like variables in the enclosing scope, so all copies share the
// same cache. Do not refactor the closure construction in NewLazyStream
// without preserving this shared-state guarantee.
type PdfStream struct {
	Dict PdfDict
	open func() ([]byte, error) // lazy loader, returns raw (possibly encrypted) stream bytes
}

// NewStream creates a PdfStream with pre-loaded data. Use this in tests and
// for synthetically constructed streams (e.g., object streams, encoding).
func NewStream(dict PdfDict, data []byte) PdfStream {
	return PdfStream{Dict: dict, open: func() ([]byte, error) { return data, nil }}
}

// NewLazyStream creates a PdfStream with a lazy loader that reads the raw
// bytes on first access and caches the result. The loader function is called
// at most once; subsequent calls to Bytes() return the cached result.
func NewLazyStream(dict PdfDict, loader func() ([]byte, error)) PdfStream {
	return PdfStream{Dict: dict, open: loader}
}

// Bytes returns the raw stream bytes (after decryption, before filter
// decoding). The data is read lazily on first call and cached for subsequent
// calls if the stream was created with NewLazyStream.
func (s PdfStream) Bytes() ([]byte, error) {
	if s.open == nil {
		return nil, nil
	}
	return s.open()
}

// IsPdfObject marks PdfStream as a PDF object.
func (PdfStream) IsPdfObject() {}

// String returns the string representation of PdfStream.
func (s PdfStream) String() string {
	dictStr := s.Dict.String()
	if s.Dict == nil {
		dictStr = "<<>>"
	}
	return fmt.Sprintf("%s\nstream\n<...>\nendstream", dictStr)
}
