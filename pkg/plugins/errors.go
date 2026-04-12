package plugins

import "errors"

// ErrUnsupportedFilter indicates that a filter plugin cannot decode the
// stream, either because CGO is not available or the data is invalid.
var ErrUnsupportedFilter = errors.New("unsupported filter plugin")
