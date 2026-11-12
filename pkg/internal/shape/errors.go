package shape

import "errors"

// ErrUnresolved reports a geometry whose guides do not all resolve: a
// name the reader did not know, a reference cycle, or a chain of
// references deeper than [MaxGuideDepth]. Each such value evaluates to 0.
var ErrUnresolved = errors.New("shape: unresolved geometry guides")
