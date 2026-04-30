package compliance

import "errors"

// ErrUnknownProfile is returned by [ValidatePDFA] and [ValidatePDFUA]
// when the supplied profile identifier does not match any known
// PDF/A or PDF/UA conformance profile. Callers can branch on this
// sentinel via [errors.Is] without inspecting the wrapped message.
var ErrUnknownProfile = errors.New("compliance: unknown profile")
