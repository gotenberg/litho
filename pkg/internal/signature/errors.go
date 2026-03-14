package signature

import "errors"

// ErrMDPViolation indicates that a document was modified in a way that
// violates the DocMDP or FieldMDP policy declared by a signature.
var ErrMDPViolation = errors.New("MDP policy violation: unauthorized modification after signing")
