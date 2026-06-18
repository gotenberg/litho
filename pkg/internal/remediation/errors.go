package remediation

import "errors"

// errXMPPropertyNotFound is returned by the XMP splicer helpers when a
// property lookup fails. It is internal; callers switch on it via
// [errors.Is].
var errXMPPropertyNotFound = errors.New("remediation: xmp property not found")
