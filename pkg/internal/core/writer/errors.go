package writer

import "errors"

// ErrUnsupportedObject is returned when the serializer encounters a PDF object
// type it cannot serialize.
var ErrUnsupportedObject = errors.New("unsupported PDF object type")
