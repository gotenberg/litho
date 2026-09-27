package writer

import "errors"

// ErrUnsupportedObject is returned when the serializer encounters a PDF object
// type it cannot serialize.
var ErrUnsupportedObject = errors.New("unsupported PDF object type")

// ErrNameHoldsNull is returned, wrapped with the name, by [Serialize] when it
// meets a name holding a NUL byte, which ISO 32000-1, 7.3.5 excludes from
// every name.
var ErrNameHoldsNull = errors.New("name holds a NUL byte, which ISO 32000-1, 7.3.5 excludes from every name")

// ErrDirectStream is returned, wrapped with where the stream sits, by
// [Serialize] when it meets a stream inside a dictionary or an array. ISO
// 32000-1, 7.3.8.1 makes every stream an indirect object, so no spelling of
// such an object conforms; [HoistDirectStreams] moves each into an object of
// its own before a write.
var ErrDirectStream = errors.New("stream inside a dictionary or an array, which ISO 32000-1, 7.3.8.1 forbids: every stream is an indirect object")
