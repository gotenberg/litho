package writer

import "errors"

// ErrUnsupportedObject is returned when the serializer encounters a PDF object
// type it cannot serialize.
var ErrUnsupportedObject = errors.New("unsupported PDF object type")

// ErrNameHoldsNull is returned, wrapped with the name, by [Serialize] when it
// meets a name holding a NUL byte, which ISO 32000-1, 7.3.5 excludes from
// every name.
var ErrNameHoldsNull = errors.New("name holds a NUL byte, which ISO 32000-1, 7.3.5 excludes from every name")
