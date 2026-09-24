package plugins

import "errors"

// ErrUnsupportedFilter is the error a [FilterDecoder] returns from Decode to
// decline a stream. litho matches it with [errors.Is], so a wrapped value
// declines too. A declined stream goes to litho's built-in decoder for the
// filter; when litho has no built-in decoder for it, decoding that stream
// fails.
var ErrUnsupportedFilter = errors.New("unsupported filter plugin")
